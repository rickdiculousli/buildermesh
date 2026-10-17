package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
	"github.com/rickdiculousli/buildermesh/internal/upgrade"
)

// walkRange is a walk of the upgrade notes: from one revision to another,
// the notes read from main in dir.
type walkRange struct {
	dir      string
	from, to string // revisions; from "" is a box with no record (walked from upgrade.First)
}

// What a walk does.
const (
	phaseAll  = iota // everything: a walk run by hand, a resume, the walk after the swap (its asks done)
	phaseAsks        // the config check and the asks: before the swap
)

// walkTimeout bounds one run or check step.
const walkTimeout = 30 * time.Minute

// label names rev in r.dir: a release, a release and the commits past it,
// or <rev8>; upgrade.First for "" (no record).
func (r walkRange) label(rev string) string {
	if rev == "" {
		return upgrade.First
	}
	if selfupdate.IsRelease(rev) {
		return rev
	}
	return selfupdate.Label(r.dir, rev)
}

// notes reads the notes of every release between r's ends, oldest first:
// a release without notes asks nothing.
func (r walkRange) notes() ([]*upgrade.Notes, []string, error) {
	from := r.from
	if selfupdate.IsRelease(from) {
		if from = selfupdate.OwnCommit(r.dir, from); from == "" {
			return nil, nil, fmt.Errorf("release %s is not in %s", r.from, r.dir)
		}
	}
	tags, err := upgrade.Between(r.dir, from, r.to)
	if err != nil {
		return nil, nil, err
	}
	ref := upgrade.MainRef(r.dir)
	var out []*upgrade.Notes
	for _, t := range tags {
		n, ok, err := upgrade.Read(r.dir, ref, t)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			out = append(out, n)
		}
	}
	return out, tags, nil
}

// pendingWalk is the walk a current binary at own still owes: one stopped
// part-way to own (<state dir>/upgrade.json), else the notes pending since
// this box's record (upgrade.Pending), walked from the recorded revision,
// else from the binary a swap replaced (<state dir>/previous), else from
// upgrade.First. false when there is none.
func (a *app) pendingWalk(dir, own string) (walkRange, bool) {
	sd := config.StateDir()
	if st, err := upgrade.LoadState(sd); err == nil && st != nil && selfupdate.Same(st.To, own) {
		return walkRange{dir: dir, from: st.From, to: st.To}, true
	}
	if own == "" || upgrade.Pending(sd, buildVersion().Release) == "" {
		return walkRange{}, false
	}
	from := ""
	if rec, ok := upgrade.ReadInstalled(sd); ok {
		from = rec.Revision
	} else if p := upgrade.ReadPrevious(sd); p != "" {
		from = selfupdate.OwnCommit(dir, p)
	}
	return walkRange{dir: dir, from: from, to: own}, true
}

// recordInstalled writes this box's record when it has none: a current
// binary has nothing left to walk.
func (a *app) recordInstalled(dir, rev string) {
	if rev == "" {
		return
	}
	_ = upgrade.WriteInstalled(config.StateDir(), upgrade.Record{Version: selfupdate.Label(dir, rev), Revision: rev,
		At: time.Now().UTC().Format(time.RFC3339)})
}

// updateSrc is where update reads main.
type updateSrc struct {
	name   string // the registry row, or the module
	dir    string // its main checkout, or the bare upstream repo
	ref    string // main there
	module string // set when dir is the module's upstream
}

// updateSource is where update reads main: repo's registry row (default
// [update] repo, else the row this binary's module is from), its
// bmesh-owned main checkout, else the module's main fetched into the bare
// upstream repo; locked until unlock.
func (a *app) updateSource(repo string) (src updateSrc, unlock func(), err error) {
	if err := a.loadConfig(); err != nil {
		return src, nil, err
	}
	name := repo
	if name == "" {
		name = a.cfg.Update.Repo
	}
	if name == "" {
		name = a.ownRepo()
	}
	if name == "" {
		module := buildModule()
		if module == "" {
			return src, nil, fail(exitRefused, "no repo to update from: pass --repo or set [update] repo")
		}
		cache, err := userCacheDir()
		if err != nil {
			return src, nil, fmt.Errorf("no cache dir for %s's main: %w", module, err)
		}
		src = updateSrc{name: module, dir: selfupdate.UpstreamDir(cache), ref: "refs/heads/main", module: module}
		if _, unlock, err = selfupdate.FetchUpstream(src.dir, module); err != nil {
			return src, nil, fmt.Errorf("read %s's main: %w", module, err)
		}
		return src, unlock, nil
	}
	if _, ok := a.cfg.Repos[name]; !ok {
		return src, nil, fail(exitRefused, "update repo %q is not in the registry (bmesh repos)", name)
	}
	_, repoPath, err := a.cfg.RepoPath(name)
	if err != nil {
		return src, nil, err
	}
	clones := ""
	if cache, err := userCacheDir(); err == nil {
		clones = filepath.Join(cache, "bmesh", "update")
	} else if a.cfg.Runner.Checkouts == "" {
		return src, nil, fmt.Errorf("no checkouts root and no cache dir: %w", err)
	}
	dir, unlock, err := selfupdate.MainCheckout(repoPath, a.cfg.Expand(a.cfg.Runner.Checkouts), clones, name)
	if err != nil {
		return src, nil, err
	}
	return updateSrc{name: name, dir: dir, ref: "HEAD"}, unlock, nil
}

// walkGiven is `update --walk` and `update --plan --from`: the walk (or its
// plan) of the range the flags give, to this binary when --to is unset,
// with main read from --dir, else as update reads it.
func (a *app) walkGiven(w walkFlags) error {
	dir := w.dir
	if dir == "" {
		src, unlock, err := a.updateSource("")
		if err != nil {
			return err
		}
		defer unlock()
		dir = src.dir
	}
	r := walkRange{dir: dir, from: w.from, to: w.to}
	if !w.toSet {
		r.to = selfupdate.OwnCommit(dir, buildVersion().Own())
		if r.to == "" {
			return fail(exitRefused, "this binary does not know its revision: pass --to")
		}
	}
	if w.plan {
		return a.printPlan(r)
	}
	phase := phaseAll
	if w.preSwap {
		phase = phaseAsks
	}
	return a.walk(r, w, phase)
}

// planOut is what --plan --json prints.
type planOut struct {
	From      string        `json:"from"`
	To        string        `json:"to"`
	Releases  []string      `json:"releases"`   // every release in between, with notes or not
	StartOver bool          `json:"start_over"` // the walk starts the box over (upgrade.First's notes)
	Config    string        `json:"config"`     // ok, the failure, or start-over (the walk moves it aside)
	Asks      int           `json:"asks"`       // asks not yet answered
	Steps     int           `json:"steps"`      // run and check steps not yet done
	Confirm   int           `json:"confirm"`    // of those, the ones that ask first
	Plan      []plannedStep `json:"plan"`
}

// plannedStep is a step of the plan and whether its `when` holds now.
type plannedStep struct {
	upgrade.Planned
	Applies bool `json:"applies"`
}

// walkPlan is the plan of r on this box: the notes, the steps for its OS
// marked done as the walk under way records, and whether each step's
// `when` holds against the config now.
func (a *app) walkPlan(r walkRange) (planOut, *upgrade.State, []*upgrade.Notes, error) {
	notes, tags, err := r.notes()
	if err != nil {
		return planOut{}, nil, nil, err
	}
	st, err := upgrade.LoadState(config.StateDir())
	if err != nil {
		return planOut{}, nil, nil, err
	}
	if st == nil || st.From != r.from || st.To != r.to {
		st = &upgrade.State{From: r.from, FromAs: r.label(r.from), To: r.to, ToAs: r.label(r.to), Answers: map[string]string{}}
	}
	if st.Answers == nil {
		st.Answers = map[string]string{}
	}
	out := planOut{From: st.FromAs, To: st.ToAs, Releases: tags, Config: "ok", Plan: []plannedStep{}}
	if out.Releases == nil {
		out.Releases = []string{}
	}
	out.StartOver = slices.ContainsFunc(notes, func(n *upgrade.Notes) bool { return n.StartOver })
	if out.StartOver {
		out.Config = "start-over"
	} else if _, err := checkConfig(a.configPath); err != nil {
		out.Config = err.Error()
	}
	get, err := config.Values(a.cfgPath())
	if err != nil {
		get = func(string, string) (string, bool) { return "", false }
	}
	for _, p := range upgrade.Plan(notes, hostGOOS, st) {
		cond, _ := upgrade.ParseWhen(p.When)
		ps := plannedStep{Planned: p, Applies: cond.Holds(get)}
		out.Plan = append(out.Plan, ps)
		if p.Done {
			continue
		}
		if p.Kind == upgrade.Ask {
			out.Asks++
		} else {
			out.Steps++
			if p.Confirm {
				out.Confirm++
			}
		}
	}
	return out, st, notes, nil
}

// cfgPath is the config file this run reads.
func (a *app) cfgPath() string {
	if a.configPath != "" {
		return a.configPath
	}
	return config.Path()
}

// counts is `1 ask, 2 steps`, or `nothing to do`.
func counts(asks, steps int) string {
	var parts []string
	if asks > 0 {
		parts = append(parts, count(asks, "ask"))
	}
	if steps > 0 {
		parts = append(parts, count(steps, "step"))
	}
	if len(parts) == 0 {
		return "nothing to do"
	}
	return strings.Join(parts, ", ")
}

// count is `1 ask`, `2 asks`.
func count(n int, s string) string { return fmt.Sprintf("%d %s", n, plural(n, s)) }

// printPlan prints r's plan and changes nothing: `<from> -> <to>: 1 ask, 1
// step`, a line per step, and the config check; --json prints planOut.
func (a *app) printPlan(r walkRange) error {
	out, _, _, err := a.walkPlan(r)
	if err != nil {
		return err
	}
	if a.json {
		return a.printJSON(out)
	}
	fmt.Fprintf(a.stdout, "%s -> %s: %s\n", out.From, out.To, counts(out.Asks, out.Steps))
	for _, p := range out.Plan {
		mark := ""
		switch {
		case p.Done:
			mark = " (done)"
		case !p.Applies:
			mark = " (skipped: its when doesn't hold)"
		}
		fmt.Fprintf(a.stdout, "  %s %s%s\n", p.ID, p.Line(), mark)
	}
	if out.Config != "ok" {
		fmt.Fprintf(a.stdout, "config: %s\n", out.Config)
	}
	return nil
}

// walk walks this box through r's notes: it checks the config with this
// binary (not for a start-over, which moves it aside), answers the asks
// (--answer, --defaults, else a question at a terminal) and writes them
// into the config, keeping config.toml.prev; then, unless phase is
// phaseAsks, it runs the run and check steps that apply here, with --apply
// or after a yes at a terminal, a confirm step only after a yes or with
// --yes. Each step done is recorded in <state dir>/upgrade.json, so a walk
// stopped by a failing step resumes after the last one done; a finished
// walk writes <state dir>/installed and prints `at <to>`.
func (a *app) walk(r walkRange, w walkFlags, phase int) error {
	plan, st, notes, err := a.walkPlan(r)
	if err != nil {
		return err
	}
	sd := config.StateDir()
	if plan.Config != "ok" && plan.Config != "start-over" {
		return fail(exitRefused, "config check failed: %s", plan.Config)
	}
	if !st.Shown && (plan.Asks > 0 || plan.Steps > 0) {
		fmt.Fprintf(a.stdout, "%s -> %s: %s\n", plan.From, plan.To, counts(plan.Asks, plan.Steps))
		if plan.StartOver {
			for _, n := range notes {
				if n.StartOver && n.Loses != "" {
					fmt.Fprintf(a.stdout, "%s starts this box over; it loses %s\n", n.Release, n.Loses)
				}
			}
		}
		st.Shown = true
		if err := st.Save(sd); err != nil {
			return err
		}
	}
	if err := a.walkAsks(plan, st, w); err != nil {
		return err
	}
	if phase == phaseAsks {
		return nil
	}
	for _, p := range plan.Plan {
		if p.Kind == upgrade.Ask || slices.Contains(st.Done, p.ID) {
			continue
		}
		ran, err := a.walkStep(p, st, w, r)
		if err != nil {
			return err
		}
		if !ran {
			return nil
		}
	}
	rec := upgrade.Record{Version: st.ToAs, Revision: r.to, At: time.Now().UTC().Format(time.RFC3339)}
	if err := upgrade.WriteInstalled(sd, rec); err != nil {
		return err
	}
	if err := upgrade.ClearState(sd); err != nil {
		return err
	}
	if plan.Asks > 0 || plan.Steps > 0 || st.Shown {
		fmt.Fprintf(a.stdout, "at %s\n", st.ToAs)
	}
	return nil
}

// answerFor is an ask's answer from --answer (by its key as written, its
// key alone or section.key), else its default with --defaults.
func (w walkFlags) answerFor(s upgrade.Step) (string, bool) {
	sec, key, _ := upgrade.ParseKey(s.Key)
	for _, k := range []string{s.Key, s.Label(), sec + "." + key} {
		if v, ok := w.answers[k]; ok && k != "." {
			return v, true
		}
	}
	if w.defaults {
		return s.Default, true
	}
	return "", false
}

// walkAsks answers every ask not yet done whose `when` holds, then writes
// the answers: config asks into config.toml (keeping config.toml.prev; a
// config this binary then refuses is put back and refused), registry asks
// into every row of repos.toml. Without an answer for every one (and no
// terminal to ask at) it writes nothing and refuses, naming them.
func (a *app) walkAsks(plan planOut, st *upgrade.State, w walkFlags) error {
	term := interactive(a.stdin)
	got := map[string]string{}
	var asks []plannedStep
	var missing []string
	for _, p := range plan.Plan {
		if p.Kind != upgrade.Ask || p.Done {
			continue
		}
		if !p.Applies {
			st.Done = append(st.Done, p.ID)
			continue
		}
		asks = append(asks, p)
		v, ok := w.answerFor(p.Step)
		if !ok && term {
			v, ok = a.askLine(p.Step), true
		}
		if !ok {
			missing = append(missing, p.Key)
			continue
		}
		if len(p.Choices) > 0 && !slices.Contains(p.Choices, v) {
			return fail(exitUsage, "%s: %q is not one of %s", p.Key, v, strings.Join(p.Choices, ", "))
		}
		got[p.ID] = v
	}
	if len(missing) > 0 {
		return fail(exitRefused, "%s %s an answer (%s): pass --answer <key>=<value> for each, or --defaults; nothing changed",
			count(len(missing), "ask"), map[bool]string{true: "waits on", false: "wait on"}[len(missing) == 1], strings.Join(missing, ", "))
	}
	if len(asks) == 0 {
		return st.Save(config.StateDir())
	}
	if err := a.writeAnswers(asks, got, plan.StartOver); err != nil {
		return err
	}
	for _, p := range asks {
		st.Done = append(st.Done, p.ID)
		st.Answers[p.ID] = got[p.ID]
	}
	return st.Save(config.StateDir())
}

// askLine asks s at the terminal, one line: `track: <changed> <question>
// [release]/latest`; Enter takes the default, and an answer outside the
// choices is asked again.
func (a *app) askLine(s upgrade.Step) string {
	choices := []string{"[" + s.Default + "]"}
	if len(s.Choices) > 0 {
		choices = choices[:0]
		for _, c := range s.Choices {
			if c == s.Default {
				c = "[" + c + "]"
			}
			choices = append(choices, c)
		}
	}
	q := fmt.Sprintf("%s: %s %s", s.Label(), strings.TrimSpace(s.Changed+" "+s.Question), strings.Join(choices, "/"))
	for {
		fmt.Fprintf(a.stdout, "%s ", q)
		line, _ := a.lines().ReadString('\n')
		if line = strings.TrimSpace(line); line == "" {
			return s.Default
		}
		if len(s.Choices) == 0 || slices.Contains(s.Choices, line) {
			return line
		}
		fmt.Fprintf(a.stdout, "want one of %s\n", strings.Join(s.Choices, ", "))
	}
}

// writeAnswers writes the asks' answers (got by step id).
func (a *app) writeAnswers(asks []plannedStep, got map[string]string, startOver bool) error {
	path := a.cfgPath()
	var cfgAsks, regAsks []plannedStep
	for _, p := range asks {
		if p.File == upgrade.FileRegistry {
			regAsks = append(regAsks, p)
		} else {
			cfgAsks = append(cfgAsks, p)
		}
	}
	if len(cfgAsks) > 0 {
		old, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		var lines []string
		if len(old) > 0 {
			lines = strings.Split(strings.TrimRight(string(old), "\n"), "\n")
		}
		for _, p := range cfgAsks {
			sec, key, _ := upgrade.ParseKey(p.Key)
			lit, err := config.Literal(sec, key, got[p.ID])
			if err != nil {
				return fail(exitUsage, "%s: %v", p.Key, err)
			}
			lines = setTable(lines, sec, func(body []string) []string {
				body = slices.DeleteFunc(body, func(l string) bool { return keyRE(key).MatchString(l) })
				return append([]string{key + " = " + lit}, body...)
			}, true)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if old != nil {
			if err := os.WriteFile(path+".prev", old, 0o600); err != nil {
				return err
			}
		}
		if err := os.WriteFile(path, []byte(strings.TrimLeft(strings.Join(lines, "\n"), "\n")+"\n"), 0o600); err != nil {
			return err
		}
		if !startOver {
			if _, err := checkConfig(a.configPath); err != nil {
				if old != nil {
					_ = os.WriteFile(path, old, 0o600)
				}
				return fail(exitRefused, "config check failed with the answers, so %s is as it was: %v", path, err)
			}
		}
		fmt.Fprintf(a.stdout, "wrote %s (the old one is %s.prev)\n", path, path)
	}
	if len(regAsks) > 0 {
		rp := config.ReposPath(path)
		repos, err := config.LoadRepos(rp)
		if err != nil {
			return err
		}
		for name, row := range repos {
			for _, p := range regAsks {
				if err := config.SetRepoKey(&row, p.Key, got[p.ID]); err != nil {
					return fail(exitUsage, "repos.toml [repos.%s] %s: %v", name, p.Key, err)
				}
			}
			repos[name] = row
		}
		if b, err := os.ReadFile(rp); err == nil {
			if err := os.WriteFile(rp+".prev", b, 0o644); err != nil {
				return err
			}
		}
		if err := config.WriteRepos(rp, repos); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "wrote %s (the old one is %s.prev): publish it from the admin box (bmesh repos publish), or propose it (bmesh repos propose)\n", rp, rp)
	}
	return nil
}

// walkStep runs one run or check step, or skips it when its `when` doesn't
// hold. Without --apply it runs only after a yes at a terminal, and with
// none it lists what waits and reports false. A failing step stops the
// walk, naming it; what is done stays recorded.
func (a *app) walkStep(p plannedStep, st *upgrade.State, w walkFlags, r walkRange) (bool, error) {
	sd := config.StateDir()
	get, err := config.Values(a.cfgPath())
	if err != nil {
		get = func(string, string) (string, bool) { return "", false }
	}
	cond, _ := upgrade.ParseWhen(p.When)
	if !cond.Holds(get) {
		fmt.Fprintf(a.stdout, "skip %s: %s (its when doesn't hold)\n", p.ID, p.Line())
		st.Done = append(st.Done, p.ID)
		return true, st.Save(sd)
	}
	fmt.Fprintf(a.stdout, "%s %s\n", p.ID, p.Line())
	term := interactive(a.stdin)
	switch {
	case !w.apply && !term:
		fmt.Fprintf(a.stdout, "the steps from %s wait: run bmesh update --apply (with --yes for the ones that ask first)\n", p.ID)
		return false, st.Save(sd)
	case p.Confirm && !w.yes && !term:
		return false, fail(exitRefused, "step %s asks first: pass --yes to run it", p.ID)
	case p.Confirm && !w.yes:
		if !a.yes("Run it? [y/N] ", false) {
			return false, fail(exitRefused, "stopped before %s; run bmesh update again to go on", p.ID)
		}
	case !w.apply:
		if !a.yes("Run it? [Y/n] ", true) {
			return false, fail(exitRefused, "stopped before %s; run bmesh update again to go on", p.ID)
		}
	}
	if err := a.runStep(p, r); err != nil {
		if serr := st.Save(sd); serr != nil {
			return false, serr
		}
		return false, fail(exitError, "step %s failed: %v; fix it and run bmesh update again, which goes on from there", p.ID, err)
	}
	st.Done = append(st.Done, p.ID)
	return true, st.Save(sd)
}

// yes asks q and reads a line; Enter is def.
func (a *app) yes(q string, def bool) bool {
	fmt.Fprint(a.stdout, q)
	line, _ := a.lines().ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return def
	case "y", "yes":
		return true
	}
	return false
}

// runStep runs a step's command in the shell (sh -c, cmd /C on Windows),
// with the walk's paths in its environment: BMESH_CONFIG, BMESH_CONFIG_DIR,
// BMESH_STATE_DIR, BMESH_STORE (the embedded hub's store, as the config
// names it), BMESH_FROM and BMESH_TO. A check's output must show its
// expect.
func (a *app) runStep(p plannedStep, r walkRange) error {
	ctx, cancel := context.WithTimeout(context.Background(), walkTimeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", p.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", p.Command)
	}
	path := a.cfgPath()
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	store := config.DefaultStore()
	if get, err := config.Values(path); err == nil {
		if s, _ := get("mesh", "store"); s != "" {
			store = config.ExpandHome(s)
		}
	}
	cmd.Env = append(os.Environ(), "BMESH_CONFIG="+path, "BMESH_CONFIG_DIR="+filepath.Dir(path),
		"BMESH_STATE_DIR="+config.StateDir(), "BMESH_STORE="+store, "BMESH_FROM="+r.label(r.from), "BMESH_TO="+r.label(r.to))
	cmd.Stdin = a.stdin
	cmd.WaitDelay = 5 * time.Second
	if p.Kind == upgrade.Run {
		cmd.Stdout, cmd.Stderr = a.stdout, a.stderr
		return cmd.Run()
	}
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		fmt.Fprint(a.stdout, string(out))
		if !strings.HasSuffix(string(out), "\n") {
			fmt.Fprintln(a.stdout)
		}
	}
	if err != nil {
		return err
	}
	if p.Expect != "" && !strings.Contains(string(out), p.Expect) {
		return fmt.Errorf("its output doesn't show %q", p.Expect)
	}
	return nil
}
