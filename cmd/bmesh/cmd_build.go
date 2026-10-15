package main

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/brief"
	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const buildUsage = "usage: bmesh build <repo> <NNN> [--model M] [--effort E] [--path P] | bmesh build --accepted <repo> [--model M] [--effort E]"

// buildSlots is how many leads --accepted keeps working on one repo.
const buildSlots = 3

// idRE finds the three-digit ids a spec body names; rangeRE a run of them.
var (
	idRE    = regexp.MustCompile(`\b(\d{3})\b`)
	rangeRE = regexp.MustCompile(`\b(\d{3}) (?:to|through|-) (\d{3})\b`)
)

// buildStarted is what one build prints: the lead's node and session, the
// model, the spec and the commit that recorded building on main.
type buildStarted struct {
	leadStarted
	Model    string `json:"model"`
	Spec     string `json:"spec"`
	Building string `json:"building"`
}

// cmdBuild dispatches an accepted spec: bmesh build <repo> <NNN> records
// building (thread, branch, host) in the spec on main, renders the build
// brief and starts the lead through the runner as lead start does.
// --accepted does that for every accepted spec, lowest id first, a spec
// that names a lower id after that one is done, at most buildSlots leads
// working on the repo, and prints what waits.
func (a *app) cmdBuild(args []string) error {
	fs := a.flags("build")
	accepted := fs.Bool("accepted", false, "build every accepted spec, in order, up to three at a time")
	model := fs.String("model", "", "model the lead runs on (default the spec's judge)")
	effort := fs.String("effort", "", "the lead's effort (default [models] lead_effort)")
	pathFlag := fs.String("path", "", "node path (default <repo>-<NNN>)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if (*accepted && (len(pos) != 1 || *pathFlag != "")) || (!*accepted && len(pos) != 2) {
		return fail(exitUsage, buildUsage)
	}
	if err := checkModel(*model); err != nil {
		return err
	}
	if err := checkEffort(*effort); err != nil {
		return err
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	name, repoPath, err := a.cfg.RepoPath(pos[0])
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	row, ok := a.cfg.Repos[name]
	if !ok {
		return fail(exitUsage, "%s is not in repos.toml; a build needs its row (verify, remote, routines)", name)
	}
	if row.Mode != "ship" {
		return fail(exitRefused, "refused: %s is %s; builds are for ship repos only", name, row.Mode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := levelMain(ctx, repoPath); err != nil {
		return fail(exitRefused, "refused: %v", err)
	}
	specs, err := spec.ScanRef(repoPath, a.cfg.Spec.Glob, "main")
	if err != nil {
		return ledgerFail(err)
	}
	if err := a.connect(ctx, "build"); err != nil {
		return err
	}
	if *accepted {
		return a.buildAccepted(ctx, row, repoPath, specs, *model, *effort)
	}
	n, err := strconv.Atoi(pos[1])
	if err != nil {
		return fail(exitUsage, "spec id %q is not a number", pos[1])
	}
	s, ok := spec.Find(specs, n)
	if !ok {
		return fail(exitError, "no spec %03d on main in %s", n, repoPath)
	}
	started, err := a.buildOne(ctx, row, repoPath, s, *model, *effort, *pathFlag)
	if err != nil {
		return err
	}
	return a.printJSON(started)
}

// buildOne dispatches one spec. The spec must read accepted on main; its
// judge names the model unless --model does. The node path must be free
// (exit 3 and the --path <repo>-<NNN>-2 rule, as lead start) and the host
// under its lead cap before anything is recorded. Then building lands on
// main (built_by the thread id, host, and branch when the runner names
// branches ahead), the brief is rendered to <user cache dir>/bmesh/briefs/
// <path>.md and the lead is started with that thread id; a runner that
// names the branch itself (t3) gets it recorded in a second commit. A
// start that fails moves the spec back to accepted.
func (a *app) buildOne(ctx context.Context, row config.Repo, repoPath string, s *spec.Spec, model, effort, pathFlag string) (buildStarted, error) {
	if s.Status() != "accepted" {
		return buildStarted{}, fail(exitRefused, "refused: spec %s is %s, not accepted", s.IDString(), s.Status())
	}
	if model == "" {
		m, err := spec.ModelFor(s)
		if err != nil {
			return buildStarted{}, fail(exitRefused, "refused: %v; pass --model, or `%s on <judge>` to bmesh choose-ideas", err, s.IDString())
		}
		model = m
	}
	p, err := leadPath(row.Name, s.IDString(), pathFlag)
	if err != nil {
		return buildStarted{}, err
	}
	if _, err := a.leadFree(ctx, p); err != nil {
		return buildStarted{}, err
	}
	thread, err := uuid4()
	if err != nil {
		return buildStarted{}, err
	}
	host := a.cfg.HostName()
	fields := []spec.Field{{Key: "built_by", Value: thread}, {Key: "host", Value: host}}
	branch := ""
	if k := a.cfg.Runner.Kind; k == "claude" || k == "tmux" {
		branch = thread // those runners make the worktree on a branch named for the thread
		fields = append(fields, spec.Field{Key: "branch", Value: branch})
	}
	sha, err := a.recordOnMain(ctx, row.Name, repoPath, fmt.Sprintf("Dispatch %s: building on %s", s.IDString(), host),
		s.ID, "building", []string{"accepted"}, fields...)
	if err != nil {
		return buildStarted{}, err
	}
	values := brief.RowValues(row, repoPath)
	for k, v := range map[string]string{"spec": strings.TrimRight(s.Text, "\n"), "spec_path": filepath.ToSlash(s.Path),
		"spec_id": s.IDString(), "node": p, "date": time.Now().UTC().Format("2006-01-02")} {
		values[k] = v
	}
	text, err := brief.Render(repoPath, "build", values)
	if err != nil {
		return buildStarted{}, err
	}
	briefPath, err := writeBrief(p, text)
	if err != nil {
		return buildStarted{}, err
	}
	started, err := a.startLead(leadStart{Name: row.Name, RepoPath: repoPath, Spec: s.IDString(), BriefPath: briefPath,
		Model: model, Effort: effort, Title: fmt.Sprintf("[build] %s %s: %s", row.Name, s.IDString(), s.Title()), Path: p, Thread: thread})
	if err != nil {
		if _, rerr := a.recordOnMain(ctx, row.Name, repoPath, fmt.Sprintf("Dispatch %s: did not start", s.IDString()),
			s.ID, "accepted", []string{"building"}); rerr != nil {
			return buildStarted{}, fmt.Errorf("%v; and the spec is left building on main: %v", err, rerr)
		}
		return buildStarted{}, fmt.Errorf("%v; the spec reads accepted again", err)
	}
	if started.Branch != branch && started.Branch != "" {
		if sha, err = a.recordOnMain(ctx, row.Name, repoPath, fmt.Sprintf("Dispatch %s: branch %s", s.IDString(), started.Branch),
			s.ID, "building", []string{"building"}, spec.Field{Key: "branch", Value: started.Branch}); err != nil {
			return buildStarted{}, fmt.Errorf("lead %s started (thread %s) but recording its branch on main failed: %w", p, started.Thread, err)
		}
	}
	return buildStarted{leadStarted: started, Model: model, Spec: s.IDString(), Building: sha}, nil
}

// recordOnMain sets spec id's status and fields on main: in a bmesh
// worktree made from main, committed as msg, fast-forwarded onto main and
// pushed, as choose-ideas lands. It returns the commit's sha. A main that
// will not fast-forward is refused (exit 3) with the commit kept on its
// branch.
func (a *app) recordOnMain(ctx context.Context, name, repoPath, msg string, id int, status string, from []string, fields ...spec.Field) (string, error) {
	if err := levelMain(ctx, repoPath); err != nil {
		return "", fail(exitRefused, "refused: %v", err)
	}
	tag, err := shortHex()
	if err != nil {
		return "", err
	}
	branch := name + "-build-" + tag
	wt := filepath.Join(a.cfg.Expand(a.cfg.Runner.Worktrees), name, branch)
	if err := addWorktree(ctx, repoPath, wt, branch, "main"); err != nil {
		return "", err
	}
	committed, landed := false, false
	defer func() {
		if err := removeWorktree(context.WithoutCancel(ctx), repoPath, wt, branch, committed && !landed); err != nil {
			fmt.Fprintf(a.stderr, "bmesh: %v\n", err)
		}
	}()
	specs, err := spec.Scan(wt, a.cfg.Spec.Glob)
	if err != nil {
		return "", ledgerFail(err)
	}
	s, ok := spec.Find(specs, id)
	if !ok {
		return "", fail(exitError, "no spec %03d on main in %s", id, repoPath)
	}
	if _, err := spec.SetStatus(s, status, from, fields...); err != nil {
		return "", ledgerFail(err)
	}
	if _, err := gitIn(ctx, wt, "add", "-A", "--", spec.Dir(a.cfg.Spec.Glob)); err != nil {
		return "", err
	}
	if _, err := gitIn(ctx, wt, "-c", "user.name=bmesh", "-c", "user.email=bmesh@localhost", "commit", "-q", "-m", msg); err != nil {
		return "", err
	}
	committed = true
	sha, err := gitIn(ctx, wt, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if err := landOnMain(ctx, repoPath, branch); err != nil {
		return "", fail(exitRefused, "refused: %v; the record is committed on branch %s", err, branch)
	}
	landed = true
	if err := pushMain(ctx, repoPath); err != nil {
		return "", err
	}
	return sha, nil
}

// buildAccepted dispatches the repo's accepted specs in id order, keeping
// at most buildSlots leads working on the repo (across hosts), and prints
// one line per spec: started, waits (on a named lower spec that is not
// done, or for a slot) or not started (with the refusal). One spec's
// refusal does not stop the others.
func (a *app) buildAccepted(ctx context.Context, row config.Repo, repoPath string, specs []*spec.Spec, model, effort string) error {
	working, err := a.repoLeads(ctx, row.Name)
	if err != nil {
		return err
	}
	type line struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Reason string `json:"reason,omitempty"`
		*buildStarted
	}
	var out []line
	for _, s := range specs {
		if s.Status() != "accepted" {
			continue
		}
		l := line{ID: s.IDString(), Title: s.Title()}
		if waits := waitsOn(s, specs); len(waits) > 0 {
			l.State, l.Reason = "waits", "on "+strings.Join(waits, ", ")
		} else if working >= buildSlots {
			l.State, l.Reason = "waits", fmt.Sprintf("%d leads working on %s", working, row.Name)
		} else if started, err := a.buildOne(ctx, row, repoPath, s, model, effort, ""); err != nil {
			l.State, l.Reason = "not started", err.Error()
		} else {
			l.State, l.buildStarted = "started", &started
			working++
		}
		out = append(out, l)
	}
	if a.json {
		return a.printJSON(out)
	}
	for _, l := range out {
		switch l.State {
		case "started":
			fmt.Fprintf(a.stdout, "started %s %s: %s on %s (thread %s, branch %s, building %s)\n", l.ID, l.Title, l.Path, l.Model, l.Thread, l.Branch, l.Building[:8])
		default:
			fmt.Fprintf(a.stdout, "%s %s %s: %s\n", l.State, l.ID, l.Title, l.Reason)
		}
	}
	if len(out) == 0 {
		fmt.Fprintf(a.stdout, "no accepted specs in %s\n", row.Name)
	}
	return nil
}

// waitsOn lists the lower ids s's body names (singly, or as `NNN to MMM`)
// whose specs are still to be built, as "NNN (status)". A spec depends on
// the ones before it that it names; an id above its own is a forward
// pointer, and a spec that is done, rejected or later is not waited on
// (the last two never reach done).
func waitsOn(s *spec.Spec, specs []*spec.Spec) []string {
	ids := map[int]bool{}
	for _, m := range rangeRE.FindAllStringSubmatch(s.Body, -1) {
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		for i := lo; i <= hi && i-lo < 1000; i++ {
			ids[i] = true
		}
	}
	for _, m := range idRE.FindAllStringSubmatch(s.Body, -1) {
		n, _ := strconv.Atoi(m[1])
		ids[n] = true
	}
	var waits []string
	for id := range ids {
		if id >= s.ID {
			continue
		}
		if dep, ok := spec.Find(specs, id); ok && !settled(dep.Status()) {
			waits = append(waits, fmt.Sprintf("%03d (%s)", id, dep.Status()))
		}
	}
	sort.Strings(waits)
	return waits
}

// settled reports whether a named spec is nothing to wait for: built, or
// never to be.
func settled(status string) bool {
	return status == "done" || status == "rejected" || status == "later"
}

// repoLeads counts the leads of repo (on any host) that are not done or
// blocked.
func (a *app) repoLeads(ctx context.Context, repo string) (int, error) {
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return 0, err
	}
	states, err := mesh.ReadAllStates(ctx, a.client)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, node := range nodes {
		if node.Role != "lead" || node.Repo != repo {
			continue
		}
		if s := states[node.Path].State; s == "done" || s == "blocked" {
			continue
		}
		n++
	}
	return n, nil
}
