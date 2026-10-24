package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/brief"
	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/gitwork"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const buildUsage = "usage: bmesh build <repo> <NNN> [<NNN>...] [--model M] [--effort E] [--path P] [--family F] [--hold land|now] [--context <text|->] [--force]"

// buildSlots is how many leads a build of several specs keeps working on
// one repo.
const buildSlots = 3

// idRE finds the three-digit ids a spec body names; rangeRE a run of them.
var (
	idRE    = regexp.MustCompile(`\b(\d{3})\b`)
	rangeRE = regexp.MustCompile(`\b(\d{3}) (?:to|through|-) (\d{3})\b`)
	// familyNRE reads a minted family's number: <repo>-build-<N>.
	familyNRE = regexp.MustCompile(`^(.*)-build-(\d+)$`)
)

// buildStarted is what one build prints: the lead's node and session, the
// model, the spec and the build family.
type buildStarted struct {
	leadStarted
	Model  string `json:"model"`
	Spec   string `json:"spec"`
	Family string `json:"family"`
	Hold   string `json:"hold,omitempty"` // the gate build --hold held it at
	// The spec's command clashes, which build warns of at dispatch.
	Warnings []string `json:"warnings,omitempty"`
}

// buildLine is one spec's line when several are picked: started, waits,
// building or not started, with why.
type buildLine struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	Family string `json:"family"`
	*buildStarted
}

// cmdBuild builds the specs the operator picked: bmesh build <repo> <NNN>
// [<NNN>...]. Picking is the accept: any spec on main that is not done or
// rejected is built, whatever it reads, with nothing recorded on main (the
// lead's node says the spec is being built). For each spec it renders the
// build brief and starts the lead through the runner as lead start does.
// One id prints the started JSON; several print a line per spec in id
// order: building (its newest lead is live), started, waits (on a named
// lower spec that is not done, or for one of buildSlots leads on the
// repo) or not started (the refusal). Every lead one invocation starts is
// in one build family: --family, else a fresh <repo>-build-<N>. Before
// anything starts, it holds while another family of the repo's still runs
// or has feedback pending, then verifies main's code head, once, and only
// when a spec would start; --force goes past both, saying what it passed.
// --context puts the operator's words in each brief.
func (a *app) cmdBuild(args []string) error {
	fs := a.flags("build")
	model := fs.String("model", "", "model the leads run on (default each spec's judge)")
	effort := fs.String("effort", "", "the leads' effort (default [models] lead_effort)")
	pathFlag := fs.String("path", "", "node path, for one spec (default <repo>-<NNN>)")
	familyFlag := fs.String("family", "", "join this build family (default a fresh <repo>-build-<N>)")
	force := fs.Bool("force", false, "dispatch past another family still running and a red main")
	holdGate := fs.String("hold", "", "dispatch each lead held at this gate (land or now) until bmesh unhold")
	words := fs.String("context", "", "the operator's words for these leads' briefs (- reads stdin)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return fail(exitUsage, buildUsage)
	}
	var ids []int
	for _, p := range pos[1:] {
		n, err := strconv.Atoi(p)
		if err != nil {
			return fail(exitUsage, "spec id %q is not a number", p)
		}
		if !slices.Contains(ids, n) {
			ids = append(ids, n)
		}
	}
	slices.Sort(ids)
	if *pathFlag != "" && len(ids) > 1 {
		return fail(exitUsage, "--path names one lead; pick one spec with it")
	}
	if *holdGate != "" && *holdGate != mesh.GateLand && *holdGate != mesh.GateNow {
		return fail(exitUsage, "--hold %q: want land or now", *holdGate)
	}
	if err := checkModel(*model); err != nil {
		return err
	}
	if err := checkEffort(*effort); err != nil {
		return err
	}
	if *familyFlag != "" {
		if err := mesh.CheckFamily(*familyFlag); err != nil {
			return fail(exitUsage, "--family: %v", err)
		}
	}
	if *words, err = a.readWords(*words); err != nil {
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
	if err := gitwork.LevelMain(ctx, repoPath); err != nil {
		return fail(exitRefused, "refused: %v", err)
	}
	specs, err := spec.ScanRef(repoPath, a.cfg.Spec.Glob, "main")
	if err != nil {
		return landFail(err)
	}
	var picked []*spec.Spec
	for _, n := range ids {
		s, ok := spec.Find(specs, n)
		if !ok {
			return fail(exitError, "no spec %03d on main in %s", n, repoPath)
		}
		picked = append(picked, s)
	}
	if err := a.connect(ctx, "build"); err != nil {
		return err
	}
	leads, err := a.specLeads(ctx, row.Name)
	if err != nil {
		return err
	}
	b := &batch{row: row, repoPath: repoPath, specs: specs, leads: leads, model: *model, effort: *effort,
		hold: *holdGate, words: *words}
	one := len(picked) == 1
	// Its own refusal comes before a hold or a verify.
	if one {
		if err := b.refusal(picked[0]); err != nil {
			return err
		}
	}
	if b.working, err = a.repoLeads(ctx, row.Name); err != nil {
		return err
	}
	if b.family = *familyFlag; b.family == "" {
		if b.family, err = nextFamily(ctx, a.client, row.Name); err != nil {
			return err
		}
	}
	// What the gate says goes to stderr, keeping stdout the command's
	// result, except among several specs' lines.
	note := a.stderr
	if !one && !a.json {
		note = a.stdout
	}
	// A batch where nothing would start has nothing to hold or verify.
	if one || b.anyStarts(picked) {
		hold, err := a.heldBy(ctx, row.Name, b.family)
		if err != nil {
			return err
		}
		if hold != nil {
			switch {
			case *force:
				fmt.Fprintf(note, "--force: past %s\n", hold.line())
			case one:
				return fail(exitRefused, "%s", hold.line())
			case a.json:
				return a.printJSON([]map[string]string{{"state": "held", "family": hold.who, "reason": hold.why}})
			default:
				fmt.Fprintf(a.stdout, "held %s: %s\n", hold.who, hold.why)
				return nil
			}
		}
		head, err := a.codeHead(row.Name, repoPath, "main")
		if err != nil {
			return err
		}
		if err := a.verifyMain(ctx, row, repoPath, "main", head, note, *force, "build its fix with --force"); err != nil {
			return err
		}
	}
	// The dispatch gets its own ten minutes, however long the verify ran.
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if one {
		started, err := a.buildOne(ctx, b, picked[0], *pathFlag)
		if err != nil {
			return err
		}
		if err := a.printJSON(started); err != nil {
			return err
		}
	} else if err := a.buildPicked(ctx, b, picked); err != nil {
		return err
	}
	leadsNext(a.stderr, b.paths)
	return nil
}

// leadsNext is what the coordinator does with the leads a build started
// (100): the watch that follows them, then how to relay, hold, stop and
// wipe one, to stderr so stdout stays the command's result. paths are the
// started leads'; nothing started says nothing.
func leadsNext(w io.Writer, paths []string) {
	if len(paths) == 0 {
		return
	}
	fmt.Fprintf(w, "next: bmesh watch --nodes %s --timeout 30m, under a Monitor where your harness has one, else in the background (never poll the runner); on exit 5, run it again with --since <its last line's time>\n", strings.Join(paths, ","))
	fmt.Fprintln(w, `next: an ask goes to the operator in their words, and their answer back with bmesh --node operator send <path> "<answer>"; a blocked is reported with its reason, a done with its commit, a held or landing line only when asked`)
	fmt.Fprintln(w, `next: to hold one, bmesh --node operator hold <path> --text "<why>" (before it lands; --gate now at once), released with bmesh --node operator unhold <path>; to stop one for good, bmesh --node operator node stop <path> --text "<why>", never a message; to wipe one, stop it, then bmesh node forget <path> --wipe (--dry-run first)`)
}

// readWords is the operator's words a flag gives, trimmed; - reads them
// from stdin.
func (a *app) readWords(v string) (string, error) {
	if v == "-" {
		b, err := io.ReadAll(a.stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		v = string(b)
	}
	return strings.TrimSpace(v), nil
}

// batch is one build invocation: the repo, main's ledger, each spec's
// newest lead, what every lead it starts shares, and the repo's leads
// working, counting those it started.
type batch struct {
	row                        config.Repo
	repoPath                   string
	specs                      []*spec.Spec
	leads                      map[string]*specLead
	model, effort, hold, words string
	family                     string
	working                    int
	started                    []string // ids this batch started, in order
	paths                      []string // their leads' paths, in the same order
}

// refusal is why spec s is not built, whoever picked it: done, declined,
// its newest lead live, one of the fixer's rulings, or no model.
func (b *batch) refusal(s *spec.Spec) error {
	switch s.Status() {
	case "done":
		return fail(exitRefused, "refused: spec %s is done", s.IDString())
	case "rejected":
		if r := s.Fields["reason"]; r != "" {
			return fail(exitRefused, "refused: spec %s was declined: %s", s.IDString(), r)
		}
		return fail(exitRefused, "refused: spec %s was declined", s.IDString())
	}
	if lead := b.leads[s.IDString()]; lead.live() {
		return fail(exitRefused, "refused: spec %s is building: %s (%s)", s.IDString(), lead.Node.Path, dash(lead.State.State))
	}
	if err := fixRefusal(s, b.specs); err != nil {
		return err
	}
	_, err := b.modelFor(s)
	return err
}

// modelFor is the model s's lead runs on: --model, else its judge's.
func (b *batch) modelFor(s *spec.Spec) (string, error) {
	if b.model != "" {
		return b.model, nil
	}
	m, err := spec.ModelFor(s)
	if err != nil {
		return "", fail(exitRefused, "refused: spec %s has no judge; pass --model", s.IDString())
	}
	return m, nil
}

// anyStarts reports whether a spec of picked would start: one with no
// refusal and nothing to wait on, with a slot free.
func (b *batch) anyStarts(picked []*spec.Spec) bool {
	return b.working < buildSlots && slices.ContainsFunc(picked, func(s *spec.Spec) bool {
		return b.refusal(s) == nil && len(waitsOn(s, b.specs)) == 0
	})
}

// claims is the ledger as the command clashes read it: a spec whose newest
// lead is live, or that this batch started, reads building.
func (b *batch) claims() []*spec.Spec {
	out := make([]*spec.Spec, len(b.specs))
	for i, s := range b.specs {
		out[i] = s
		if b.leads[s.IDString()].live() || slices.Contains(b.started, s.IDString()) {
			c := *s
			c.Fields = maps.Clone(s.Fields)
			c.Fields["status"] = "building"
			out[i] = &c
		}
	}
	return out
}

// nextFamily is <repo>-build-<N>, N one past the highest family of that
// shape registered in bm-nodes (as rounds are numbered) or still holding
// feedback, so a family whose nodes were forgotten before its drain is not
// joined by a new build. The repo's name is written as its path token
// (mesh.RepoToken): a subject token holds no capital, dot or underscore.
func nextFamily(ctx context.Context, c *mesh.Client, repo string) (string, error) {
	base := mesh.RepoToken(repo)
	nodes, err := mesh.ListNodes(ctx, c)
	if err != nil {
		return "", err
	}
	pending, err := mesh.PendingFeedback(ctx, c)
	if err != nil {
		return "", err
	}
	families := make([]string, 0, len(nodes)+len(pending))
	for _, node := range nodes {
		families = append(families, node.Family)
	}
	for _, p := range pending {
		families = append(families, p.Family)
	}
	n := 0
	for _, f := range families {
		m := familyNRE.FindStringSubmatch(f)
		if m == nil || m[1] != base {
			continue
		}
		if k, err := strconv.Atoi(m[2]); err == nil && k > n {
			n = k
		}
	}
	return fmt.Sprintf("%s-build-%d", base, n+1), nil
}

// buildOne dispatches spec s, which b.refusal passed; its judge names the
// model unless --model does. The node path must be free (exit 3 and the
// --path <repo>-<NNN>-2 rule, as lead start) and the host under its lead
// cap before anything starts. Then the brief is rendered to <user cache
// dir>/bmesh/briefs/<path>.md, with the operator's words when there are
// any, and the lead is started with a fresh thread id; nothing is recorded
// on main. With a hold (land or now) the lead's bm-holds key is written
// before the runner starts it, and the hold message follows the start;
// without, a hold left on the path by an earlier lead is deleted. The
// started line's command clashes are read against b.claims.
func (a *app) buildOne(ctx context.Context, b *batch, s *spec.Spec, pathFlag string) (buildStarted, error) {
	row, repoPath, family, hold := b.row, b.repoPath, b.family, b.hold
	model, err := b.modelFor(s)
	if err != nil {
		return buildStarted{}, err
	}
	p, err := leadPath(row.Name, s.IDString(), pathFlag)
	if err != nil {
		return buildStarted{}, err
	}
	if _, err := a.leadFree(ctx, p, true); err != nil {
		return buildStarted{}, err
	}
	r, err := runner.New(a.cfg)
	if err != nil {
		return buildStarted{}, err
	}
	if err := a.preflight(ctx, r); err != nil {
		return buildStarted{}, err
	}
	thread, err := uuid4()
	if err != nil {
		return buildStarted{}, err
	}
	values := brief.BuildValues(row, repoPath, s.Text, s.Path, s.IDString(), p, time.Now().UTC().Format("2006-01-02"))
	values["operator_words"] = brief.OperatorWords(b.words)
	if ownModuleRepo(repoPath) {
		values["upgrading"] = brief.UpgradingBullet
	}
	text, err := brief.Render(repoPath, "build", values)
	if err != nil {
		return buildStarted{}, err
	}
	// The lead's worktree is made from main after this brief is written,
	// so its first step is the setup main names.
	rc, err := kind.LoadRepoConfigAt(repoPath, "main")
	if err != nil {
		return buildStarted{}, err
	}
	text = brief.WithSetup(text, brief.SetupStep(rc.Setup.Command, ""))
	if section := values["operator_words"]; section != "" && !strings.Contains(text, section) {
		// A repo's own template without {{operator_words}} still gets
		// the operator's words.
		text = strings.TrimRight(text, "\n") + "\n\n" + strings.TrimRight(section, "\n") + "\n"
	}
	briefPath, err := writeBrief(p, text)
	if err != nil {
		return buildStarted{}, err
	}
	h := mesh.LeadHold{Path: p, Gate: hold, Text: "held at dispatch (build --hold " + hold + ")", At: time.Now().UTC().Format(time.RFC3339)}
	if hold != "" {
		if h.By, err = a.identityOrHost(ctx, "build"); err != nil {
			return buildStarted{}, err
		}
		if err := mesh.PutLeadHold(ctx, a.client, h); err != nil {
			return buildStarted{}, err
		}
	} else if _, err := mesh.DeleteLeadHold(ctx, a.client, p); err != nil {
		return buildStarted{}, err
	}
	started, err := a.startLead(leadStart{Name: row.Name, RepoPath: repoPath, Spec: s.IDString(), BriefPath: briefPath,
		Model: model, Effort: b.effort, Title: fmt.Sprintf("[build] %s %s: %s", row.Name, s.IDString(), s.Title()), Path: p, Thread: thread, Family: family})
	if err != nil {
		if hold != "" {
			_, _ = mesh.DeleteLeadHold(ctx, a.client, p)
		}
		return buildStarted{}, err
	}
	if hold != "" {
		// The lead runs and its hold is written, which is what land reads;
		// a lost message is said, not a failed start.
		if _, err := mesh.Send(ctx, a.client, holdMessage(h)); err != nil {
			fmt.Fprintf(a.stderr, "build: %s is held %s, but the hold message failed: %v\n", p, mesh.GatePhrase(hold), err)
		}
	}
	b.working++
	b.started = append(b.started, s.IDString())
	b.paths = append(b.paths, p)
	return buildStarted{leadStarted: started, Model: model, Spec: s.IDString(), Family: family, Hold: hold,
		Warnings: spec.CommandClashes(s, b.claims(), mainCommands())}, nil
}

// specLead is a spec's newest lead: the newest bm-nodes node with role
// lead for the repo and the spec, and its state.
type specLead struct {
	Node  mesh.Node
	State mesh.State
}

// live reports a lead still on its spec: not done or blocked.
func (l *specLead) live() bool {
	return l != nil && l.State.State != "done" && l.State.State != "blocked"
}

// specLeads is repo's newest lead per spec id.
func (a *app) specLeads(ctx context.Context, repo string) (map[string]*specLead, error) {
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return nil, err
	}
	states, err := mesh.ReadAllStates(ctx, a.client)
	if err != nil {
		return nil, err
	}
	out := map[string]*specLead{}
	for _, n := range nodes {
		if n.Role != "lead" || n.Repo != repo || n.Spec == "" {
			continue
		}
		id := spec.NormID(n.Spec)
		if old, ok := out[id]; ok && !spec.Newer(n.StartedAt, old.Node.StartedAt) {
			continue
		}
		st, ok := states[n.Path]
		if !ok {
			st.State = "idle" // registered, with no state written yet
		}
		out[id] = &specLead{Node: n, State: st}
	}
	return out, nil
}

// buildPicked builds several picked specs in id order, keeping at most
// buildSlots leads working on the repo (across hosts), and prints one line
// per spec: building (its newest lead is live: its path and state),
// started, waits (on a named lower spec that is not done, or for a slot)
// or not started (with the refusal), then the family the started leads
// are in. One spec's refusal does not stop the others; a spec whose lead
// is blocked starts again.
func (a *app) buildPicked(ctx context.Context, b *batch, picked []*spec.Spec) error {
	var out []buildLine
	for _, s := range picked {
		l := buildLine{ID: s.IDString(), Title: s.Title(), Family: b.family}
		lead := b.leads[s.IDString()]
		if lead.live() {
			l.State, l.Reason = "building", fmt.Sprintf("%s (%s)", lead.Node.Path, dash(lead.State.State))
		} else if err := b.refusal(s); err != nil {
			l.State, l.Reason = "not started", err.Error()
		} else if waits := waitsOn(s, b.specs); len(waits) > 0 {
			l.State, l.Reason = "waits", "on "+strings.Join(waits, ", ")
		} else if b.working >= buildSlots {
			l.State, l.Reason = "waits", fmt.Sprintf("%d leads working on %s", b.working, b.row.Name)
		} else if started, err := a.buildOne(ctx, b, s, ""); err != nil {
			l.State, l.Reason = "not started", err.Error()
		} else {
			l.State, l.buildStarted = "started", &started
		}
		out = append(out, l)
	}
	if a.json {
		return a.printJSON(out)
	}
	for _, l := range out {
		switch l.State {
		case "started":
			held := ""
			if l.Hold != "" {
				held = ", held " + mesh.GatePhrase(l.Hold)
			}
			warn := ""
			for _, w := range l.Warnings {
				warn += "; warning: " + w
			}
			fmt.Fprintf(a.stdout, "started %s %s: %s on %s (thread %s, branch %s, family %s%s)%s\n", l.ID, l.Title, l.Path, l.Model, l.Thread, l.Branch, l.Family, held, warn)
		default:
			fmt.Fprintf(a.stdout, "%s %s %s: %s\n", l.State, l.ID, l.Title, l.Reason)
		}
	}
	if len(b.started) > 0 {
		fmt.Fprintf(a.stdout, "family %s\n", b.family)
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
