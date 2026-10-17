package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const (
	stepsUsage = "usage: bmesh spec steps [<NNN>] [--repo R] [--tick N,N [--note W] | --check [--wait D]]"
	// checkTTL is how long a check-step job lives: past the wait, nobody
	// is listening for its answer.
	checkTTL = 10 * time.Minute
)

// openSpec is a landed spec with steps for the operator, as read from its
// repo's main.
type openSpec struct {
	Repo  string
	Spec  *spec.Spec
	Steps []spec.Step
}

// Line is the spec's operator line, as mesh-status ends with it.
func (o openSpec) Line() string {
	return fmt.Sprintf("operator: %s %s %s: %d of %d open (bmesh spec steps %s --repo %s)", o.Repo, o.Spec.IDString(),
		o.Spec.Title(), spec.Open(o.Steps), len(o.Steps), o.Spec.IDString(), o.Repo)
}

// openSteps is every spec with open steps in repos (registry names; those
// with no checkout here are passed over), read from each repo's local
// origin/main (main without an origin). A repo that has any is fetched and
// read again, so a step ticked from another box drops out here; a repo
// whose ledger can't be read is passed over.
func (a *app) openSteps(ctx context.Context, repos []string) []openSpec {
	var out []openSpec
	for _, name := range repos {
		path, ok := a.checkoutHere(name)
		if !ok {
			continue
		}
		read := func() []openSpec {
			specs, err := spec.ScanMainLocal(ctx, path, a.cfg.Spec.Glob)
			if err != nil {
				return nil
			}
			var open []openSpec
			for _, s := range specs {
				if steps, err := s.Steps(); err == nil && spec.Open(steps) > 0 {
					open = append(open, openSpec{Repo: name, Spec: s, Steps: steps})
				}
			}
			return open
		}
		open := read()
		if len(open) > 0 {
			fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if spec.FetchMain(fctx, path) == nil {
				open = read()
			}
			cancel()
		}
		out = append(out, open...)
	}
	return out
}

// cmdSpecSteps lists, ticks and checks a landed spec's steps for the
// operator. With no id it prints the operator line of every spec with open
// steps (--repo's, else every registered repo's).
func (a *app) cmdSpecSteps(args []string) error {
	fs := a.flags("spec steps")
	repo := fs.String("repo", "", "repo name or path (default the current directory; with no id, every registered repo)")
	tick := fs.String("tick", "", "comma-separated step numbers to tick off")
	note := fs.String("note", "", "words to record with the ticks")
	check := fs.Bool("check", false, "run the open steps' checks and tick those that pass")
	wait := fs.Duration("wait", 30*time.Second, "how long to wait for another box's checks")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || *tick != "" && *check || *note != "" && *tick == "" {
		return fail(exitUsage, stepsUsage)
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if len(pos) == 0 {
		if *tick != "" || *check {
			return fail(exitUsage, stepsUsage)
		}
		repos := a.cfg.RepoNames()
		if *repo != "" {
			name, _, err := a.cfg.RepoPath(*repo)
			if err != nil {
				return fail(exitUsage, "%v", err)
			}
			repos = []string{name}
		}
		sort.Strings(repos)
		for _, o := range a.openSteps(ctx, repos) {
			fmt.Fprintln(a.stdout, o.Line())
		}
		return nil
	}
	n, err := strconv.Atoi(pos[0])
	if err != nil {
		return fail(exitUsage, "spec id %q is not a number", pos[0])
	}
	id := fmt.Sprintf("%03d", n)
	if *repo == "" {
		*repo = "."
	}
	name, repoPath, err := a.cfg.RepoPath(*repo)
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	switch {
	case *tick != "":
		var ns []int
		for _, f := range strings.Split(*tick, ",") {
			k, err := strconv.Atoi(strings.TrimSpace(f))
			if err != nil || k < 1 {
				return fail(exitUsage, "--tick %q: want step numbers, e.g. 2,3", *tick)
			}
			ns = append(ns, k)
		}
		return a.tickSteps(ctx, name, repoPath, n, ns, a.cfg.HostName(), *note)
	case *check:
		return a.checkSteps(ctx, name, repoPath, id, *wait)
	}
	s, err := mainSpec(repoPath, a.cfg.Spec.Glob, id)
	if err != nil {
		return err
	}
	steps, err := s.Steps()
	if err != nil {
		return ledgerFail(err)
	}
	if len(steps) == 0 {
		fmt.Fprintf(a.stdout, "%s %s: no operator steps\n", id, s.Title())
		return nil
	}
	fmt.Fprintf(a.stdout, "%s %s: %d of %d open\n", id, s.Title(), spec.Open(steps), len(steps))
	for _, st := range steps {
		fmt.Fprintln(a.stdout, st.String())
	}
	return nil
}

// mainSpec reads spec id from the repo's main (origin/main after a fetch).
func mainSpec(repoPath, glob, id string) (*spec.Spec, error) {
	p, text, err := spec.FindOnMain(repoPath, glob, id)
	if spec.IsNotFound(err) {
		return nil, fail(exitError, "%v", err)
	}
	if err != nil {
		return nil, err
	}
	s, err := spec.Parse(text, p)
	if err != nil {
		return nil, ledgerFail(err)
	}
	return s, nil
}

// tickSteps records steps ns of spec id done, `<now> <box>` with note, on
// main as `Steps: NNN 2, 3 done`. A step already done is named and left as
// it is; when every one named is, nothing lands.
func (a *app) tickSteps(ctx context.Context, name, repoPath string, id int, ns []int, box, note string) error {
	var msg, left string
	sha, err := a.editOnMain(ctx, name, repoPath, "steps", func(wt string) (string, error) {
		specs, err := spec.Scan(wt, a.cfg.Spec.Glob)
		if err != nil {
			return "", ledgerFail(err)
		}
		s, ok := spec.Find(specs, id)
		if !ok {
			return "", fail(exitError, "no spec %03d on main", id)
		}
		steps, err := s.Steps()
		if err != nil {
			return "", ledgerFail(err)
		}
		if len(steps) == 0 {
			return "", fail(exitRefused, "refused: spec %03d has no operator steps", id)
		}
		when := time.Now().UTC().Format(time.RFC3339) + " " + box
		var ticked []string
		for _, k := range ns {
			if k > len(steps) {
				return "", fail(exitUsage, "spec %03d has no step %d (it has %d)", id, k, len(steps))
			}
			st := &steps[k-1]
			if st.Done != "" {
				fmt.Fprintf(a.stdout, "step %d already done %s\n", k, st.Done)
				continue
			}
			st.Done, st.Note = when, note
			ticked = append(ticked, strconv.Itoa(k))
		}
		if len(ticked) == 0 {
			return "", nil
		}
		v, err := spec.EncodeSteps(steps)
		if err != nil {
			return "", err
		}
		if _, err := spec.SetStatus(s, s.Status(), nil, spec.Field{Key: spec.StepsKey, Value: v}); err != nil {
			return "", ledgerFail(err)
		}
		msg = fmt.Sprintf("Steps: %03d %s done", id, strings.Join(ticked, ", "))
		left = fmt.Sprintf("%d of %d open", spec.Open(steps), len(steps))
		return msg, nil
	})
	if err != nil || sha == "" {
		return err
	}
	fmt.Fprintf(a.stdout, "landed %s on main: %s; %s\n", sha[:8], msg, left)
	return nil
}

// checkSteps runs the check of each open step of spec id that has one: on
// this box when the step names no box or this one, else as a check-step
// job to its box, waiting up to wait. Each step whose check exits 0 is
// ticked, `<time> <the box that ran it>`, one landing per box.
func (a *app) checkSteps(ctx context.Context, name, repoPath, id string, wait time.Duration) error {
	s, err := mainSpec(repoPath, a.cfg.Spec.Glob, id)
	if err != nil {
		return err
	}
	steps, err := s.Steps()
	if err != nil {
		return ledgerFail(err)
	}
	here := a.cfg.HostName()
	passed := map[string][]int{} // by the box that ran the check
	var remote []spec.Step
	checked := 0
	for _, st := range steps {
		if st.Done != "" || st.Check == "" {
			continue
		}
		checked++
		if st.Box != "" && st.Box != here {
			remote = append(remote, st)
			continue
		}
		ok, text := spec.RunCheck(ctx, repoPath, st.Check)
		fmt.Fprintf(a.stdout, "%d. %s: %s\n", st.N, here, text)
		if ok {
			passed[here] = append(passed[here], st.N)
		}
	}
	if checked == 0 {
		fmt.Fprintf(a.stdout, "%s %s: no open step has a check\n", id, s.Title())
		return nil
	}
	if len(remote) > 0 {
		if err := a.checkRemote(ctx, name, id, remote, wait, passed); err != nil {
			return err
		}
	}
	boxes := make([]string, 0, len(passed))
	for box := range passed {
		boxes = append(boxes, box)
	}
	sort.Strings(boxes)
	for _, box := range boxes {
		if err := a.tickSteps(ctx, name, repoPath, s.ID, passed[box], box, "check passed"); err != nil {
			return err
		}
	}
	return nil
}

// checkRemote sends a check-step job for each step to its box and waits up
// to wait, printing each answer; a box that passes adds its step to passed.
func (a *app) checkRemote(ctx context.Context, name, id string, steps []spec.Step, wait time.Duration, passed map[string][]int) error {
	if !a.cfg.Hosts.Enabled {
		for _, st := range steps {
			fmt.Fprintf(a.stdout, "%d. %s: not run: hosts are off here; run bmesh spec steps %s --check on %s\n", st.N, st.Box, id, st.Box)
		}
		return nil
	}
	if err := a.connect(ctx, "spec steps"); err != nil {
		return err
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	if err := b.sync(ctx, a); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: roster: %v (using this box's copy)\n", err)
	}
	r := b.local.Roster
	var jobs []hosts.Job
	byJob := map[string]spec.Step{}
	for _, st := range steps {
		if _, ok := r.Host(st.Box); !ok {
			fmt.Fprintf(a.stdout, "%d. %s: not run: %s isn't in the roster\n", st.N, st.Box, st.Box)
			continue
		}
		j := hosts.NewJob(b.key, b.host, st.Box, "check-step", name, []string{id, strconv.Itoa(st.N)}, r.Version, time.Now(), checkTTL)
		jobs = append(jobs, j)
		byJob[j.ID] = st
	}
	if len(jobs) == 0 {
		return nil
	}
	seen := a.presence(ctx, r)
	replies, err := a.askBoxes(ctx, b, jobs, wait, nil)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		st := byJob[j.ID]
		rep, ok := replies[j.ID]
		switch {
		case !ok:
			how := "asleep"
			if seen[j.To].State == hosts.Awake {
				how = "no answer in " + wait.String()
			}
			fmt.Fprintf(a.stdout, "%d. %s: %s; run --check again once it answers\n", st.N, st.Box, how)
		default:
			fmt.Fprintf(a.stdout, "%d. %s: %s\n", st.N, st.Box, rep.Text)
			if rep.OK {
				passed[st.Box] = append(passed[st.Box], st.N)
			}
		}
	}
	return nil
}
