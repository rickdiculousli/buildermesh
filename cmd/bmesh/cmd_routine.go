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
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/instance"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	wsetup "github.com/rickdiculousli/buildermesh/internal/setup"
)

const routineUsage = "usage: bmesh routine list [--json] | status [<kind>] [--json] | " +
	"queue <kind> [--commit C] [--model M] [--effort E] [--go] [--rerun] [--again] [--text T] [-- args...] | go <kind> | wait <job id> [--timeout D] | " +
	"run <kind> [--commit HEAD] [--model M] [--effort E] [--here] [--rerun] [--text T] [-- args...]"

// hostOS is the OS a kind's os is matched against: this box's; tests set it.
var hostOS = runtime.GOOS

func (a *app) cmdRoutine(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "list":
			return a.cmdRoutineList(args[1:])
		case "status":
			return a.cmdRoutineStatus(args[1:])
		case "queue":
			return a.cmdRoutineQueue(args[1:])
		case "go":
			return a.cmdRoutineGo(args[1:])
		case "wait":
			return a.cmdRoutineWait(args[1:])
		case "run":
			return a.cmdRoutineRun(args[1:])
		}
	}
	return fail(exitUsage, routineUsage)
}

// routineRow is one kind as list and status print it. A kind seen only in
// the mesh has no Mode.
type routineRow struct {
	Kind          string           `json:"kind"`
	Mode          string           `json:"mode"`
	Deterministic bool             `json:"deterministic"`
	Command       string           `json:"command,omitempty"`
	Models        []string         `json:"models,omitempty"`
	Instances     int              `json:"instances"`
	Depth         int              `json:"depth"`
	Members       []routineMember  `json:"members,omitempty"`
	Jobs          []mesh.QueuedJob `json:"jobs,omitempty"`
}

type routineMember struct {
	Path  string `json:"path"`
	Host  string `json:"host"`
	State string `json:"state"`
	Model string `json:"model"`
}

func (r routineRow) run() string {
	if r.Command != "" {
		return r.Command
	}
	return dash(strings.Join(r.Models, ","))
}

func (r routineRow) deterministic() string {
	if r.Mode == "" {
		return "-"
	}
	return fmt.Sprint(r.Deterministic)
}

// cwdKinds reads the kind files of the repo holding the cwd.
func cwdKinds() (top string, kinds []kind.Kind, err error) {
	if top, err = gitRev("--show-toplevel"); err != nil {
		return "", nil, err
	}
	kinds, err = kind.LoadDir(top)
	return top, kinds, err
}

// routineRows fills a row per kind from its file (when known) and the mesh.
func (a *app) routineRows(ctx context.Context, names []string, files map[string]kind.Kind, members bool) ([]routineRow, error) {
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return nil, err
	}
	var states map[string]mesh.State
	if members {
		if states, err = mesh.ReadAllStates(ctx, a.client); err != nil {
			return nil, err
		}
	}
	rows := make([]routineRow, 0, len(names))
	for _, name := range names {
		r := routineRow{Kind: name}
		if k, ok := files[name]; ok {
			r.Mode, r.Deterministic, r.Command, r.Models = k.Mode, k.Deterministic, k.Command, k.ModelsList()
		}
		for _, n := range nodes {
			if n.Role != "routine" || !strings.HasPrefix(n.Path, "routine."+name+".") {
				continue
			}
			r.Instances++
			if members {
				st := "idle"
				if s, ok := states[n.Path]; ok {
					st = s.State
				}
				r.Members = append(r.Members, routineMember{Path: n.Path, Host: n.Host, State: st, Model: n.Model})
			}
		}
		if r.Depth, err = mesh.Depth(ctx, a.client, name); err != nil {
			return nil, err
		}
		if members {
			if r.Jobs, err = mesh.KindJobs(ctx, a.client, name); err != nil {
				return nil, err
			}
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// printRows prints one JSON object per row, or a table; status adds a line
// per instance, then a table of the kinds' unanswered jobs when there are
// any.
func (a *app) printRows(rows []routineRow, members bool) error {
	if a.json {
		for _, r := range rows {
			if err := a.printJSON(r); err != nil {
				return err
			}
		}
		return nil
	}
	tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
	if !members {
		fmt.Fprintln(tw, "KIND\tMODE\tDETERMINISTIC\tRUN\tINSTANCES\tDEPTH")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\n", r.Kind, dash(r.Mode), r.deterministic(), r.run(), r.Instances, r.Depth)
		}
		return tw.Flush()
	}
	fmt.Fprintln(tw, "KIND\tMODE\tDETERMINISTIC\tRUN\tDEPTH\tINSTANCE\tHOST\tSTATE\tMODEL")
	for _, r := range rows {
		head := fmt.Sprintf("%s\t%s\t%s\t%s\t%d", r.Kind, dash(r.Mode), r.deterministic(), r.run(), r.Depth)
		if len(r.Members) == 0 {
			fmt.Fprintf(tw, "%s\t-\t-\t-\t-\n", head)
		}
		for _, m := range r.Members {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", head, m.Path, dash(m.Host), m.State, dash(m.Model))
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	var jobs []string
	for _, r := range rows {
		for _, j := range r.Jobs {
			jobs = append(jobs, fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n", j.ID, r.Kind, j.State, dash(j.Holder), j.From))
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	fmt.Fprintln(a.stdout)
	tw = tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "JOB\tKIND\tSTATE\tHOLDER\tFROM")
	for _, l := range jobs {
		fmt.Fprint(tw, l)
	}
	return tw.Flush()
}

// cmdRoutineList prints the cwd repo's kinds with their instances and depth.
func (a *app) cmdRoutineList(args []string) error {
	pos, err := parse(a.flags("routine list"), args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, routineUsage)
	}
	_, kinds, err := cwdKinds()
	if err != nil {
		return err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "routine"); err != nil {
		return err
	}
	files := map[string]kind.Kind{}
	var names []string
	for _, k := range kinds {
		files[k.Name] = k
		names = append(names, k.Name)
	}
	rows, err := a.routineRows(ctx, names, files, false)
	if err != nil {
		return err
	}
	return a.printRows(rows, false)
}

// cmdRoutineStatus prints a kind, or every kind in the mesh, with its
// instances. Kind files from the cwd repo, when there is one, fill the mode.
func (a *app) cmdRoutineStatus(args []string) error {
	pos, err := parse(a.flags("routine status"), args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || len(pos) == 1 && !kind.NameRE.MatchString(pos[0]) {
		return fail(exitUsage, routineUsage)
	}
	files := map[string]kind.Kind{}
	if _, kinds, err := cwdKinds(); err == nil {
		for _, k := range kinds {
			files[k.Name] = k
		}
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "routine"); err != nil {
		return err
	}
	names := pos
	if len(pos) == 0 {
		if names, err = a.meshKinds(ctx); err != nil {
			return err
		}
	}
	rows, err := a.routineRows(ctx, names, files, true)
	if err != nil {
		return err
	}
	return a.printRows(rows, true)
}

// meshKinds names every kind with a registered instance or an inbox consumer.
func (a *app) meshKinds(ctx context.Context) ([]string, error) {
	seen := map[string]bool{}
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if p, err := mesh.ParsePath(n.Path); err == nil {
			if k, ok := mesh.KindFromPath(p); ok {
				seen[k] = true
			}
		}
	}
	stream, err := a.client.JS.Stream(ctx, mesh.StreamMesh)
	if err != nil {
		return nil, err
	}
	lister := stream.ConsumerNames(ctx)
	prefix := mesh.InboxConsumer(mesh.Path{"routine"}) + "--"
	for name := range lister.Name() {
		// inbox-routine--<kind>; an instance's own inbox has a further "--".
		if k, ok := strings.CutPrefix(name, prefix); ok && !strings.Contains(k, "--") {
			seen[k] = true
		}
	}
	if err := lister.Err(); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}

// cmdRoutineQueue queues a job of a kind from the cwd repo, or answers it
// from a recorded result when the kind is deterministic.
func (a *app) cmdRoutineQueue(args []string) error {
	fs := a.flags("routine queue")
	commit := fs.String("commit", "HEAD", "commit the job runs against")
	model := fs.String("model", "", "one of the kind's models (default the first)")
	effort := fs.String("effort", "", "a model kind's effort for this job (default the model's)")
	goNow := fs.Bool("go", false, "start the batch now")
	rerun := fs.Bool("rerun", false, "run even when a result is recorded")
	again := fs.Bool("again", false, "review: queue although this branch already has a review result")
	text := fs.String("text", "", "text for a model kind")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 || !kind.NameRE.MatchString(pos[0]) {
		return fail(exitUsage, routineUsage)
	}
	name, jobArgs := pos[0], pos[1:]
	top, kinds, err := cwdKinds()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(kinds, func(k kind.Kind) bool { return k.Name == name })
	if i < 0 {
		return fail(exitRefused, "no kind %s in %s", name, top)
	}
	k := kinds[i]
	if err := checkModel(*model); err != nil {
		return err
	}
	if *model != "" && !slices.Contains(k.ModelsList(), *model) {
		return fail(exitUsage, "kind %s has no model %q", name, *model)
	}
	if err := checkJobEffort(k, *effort); err != nil {
		return err
	}
	if k.Mode == "inline" {
		return fail(exitRefused, "%s is inline: run it with bmesh routine run", name)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	self, err := a.identity(ctx, "routine")
	if err != nil {
		return err
	}
	if name == reviewKind && !*again {
		if prior, ok, err := a.priorReview(ctx, top); err != nil {
			return err
		} else if ok {
			return fail(exitRefused, "this branch already has a review: %s (%s, commit %.8s, at %s). A review's findings are fixed and then landed; "+
				"the second pass only after a rebase changed the code, with --again",
				mesh.ResultKey(prior.Kind, prior.Tree, prior.Args, prior.Model), okWord(prior.OK), prior.Commit, prior.At)
		}
	}
	if k.Deterministic && !*rerun {
		tree, err := gitRev("--verify", "--end-of-options", *commit+"^{tree}")
		if err != nil {
			return err
		}
		resultModel := *model
		if resultModel == "" && k.IsModel() {
			resultModel = k.ModelsList()[0]
		}
		key := mesh.ResultKey(name, tree, jobArgs, resultModel)
		r, ok, err := mesh.GetResult(ctx, a.client, key)
		if err != nil {
			return err
		}
		if ok {
			id := newID()
			reply := mesh.Reply{Job: id, Kind: name, Tree: r.Tree, OK: r.OK, Output: r.Output, ResultKey: key, By: r.By}
			if err := mesh.SendReply(ctx, a.client, mesh.RoutinePath(name), self, reply); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "answered from result %s (job %s)\n", key, id)
			return nil
		}
	}
	runsOn, err := jobOS(ctx, top, name, *commit)
	if err != nil {
		return err
	}
	elsewhere := runsOn != "" && runsOn != hostOS
	if elsewhere {
		// The box that runs it fetches the commit from origin.
		sha, err := gitIn(ctx, top, "rev-parse", "--verify", "--end-of-options", *commit+"^{commit}")
		if err != nil {
			return err
		}
		on, err := gitIn(ctx, top, "branch", "-r", "--contains", sha, "--list", "origin/*")
		if err != nil {
			return err
		}
		if on == "" {
			return fail(exitRefused, "push first: %s runs on %s, which fetches the commit from origin", name, runsOn)
		}
	}
	repo, err := a.repoName(ctx, self, top)
	if err != nil {
		return err
	}
	// A daemon defers a repo its registry lacks to a box whose registry has
	// it, so a repo no registry names would wait for nobody.
	if _, ok := a.cfg.Repos[repo]; !ok {
		return fail(exitRefused, "%s is not in the registry", repo)
	}
	j := mesh.Job{ID: newID(), Kind: name, Repo: repo, Commit: *commit, Args: jobArgs,
		Model: *model, Effort: *effort, Text: *text, Go: *goNow, Rerun: *rerun}
	dup, err := mesh.Queue(ctx, a.client, self, j, top)
	if err != nil {
		return err
	}
	verb := "queued"
	if dup {
		verb = "duplicate"
	}
	where := ""
	if elsewhere {
		where = " (runs on " + runsOn + ")"
	}
	fmt.Fprintf(a.stdout, "%s %s%s\n", verb, j.ID, where)
	return nil
}

// jobOS is the OS a job of the kind at commit runs on ("" for any box):
// the os the commit gives the kind, else main's (origin/main when the repo
// has an origin), whose definition the daemon falls back to without one and
// which decides the boxes that take the kind's jobs. A commit that gives the
// kind an os main doesn't is refused (exit 3), since its job could reach a
// box that can't run it; so is one whose main lacks the kind or can't be
// read. A commit without an os, where main can't say, runs anywhere.
func jobOS(ctx context.Context, top, name, commit string) (string, error) {
	at, err := kind.LoadAt(top, commit, name)
	if err != nil && !errors.Is(err, kind.ErrNotFound) {
		return "", err
	}
	ref := "main"
	if hasOrigin(ctx, top) {
		ref = "origin/main"
	}
	onMain, mainErr := kind.LoadAt(top, ref, name)
	if at.OS == "" {
		if mainErr != nil {
			return "", nil
		}
		return onMain.OS, nil
	}
	switch {
	case errors.Is(mainErr, kind.ErrNotFound):
		return "", fail(exitRefused, "%s is not on main: land it first", name)
	case mainErr != nil:
		return "", fail(exitRefused, "cannot read kind %s on %s: %v", name, ref, mainErr)
	case onMain.OS == at.OS:
		return at.OS, nil
	}
	sha, err := gitIn(ctx, top, "rev-parse", "--verify", "--end-of-options", commit+"^{commit}")
	if err != nil {
		return "", err
	}
	mainSays, box := "anywhere", "a box that isn't "+at.OS
	switch {
	case onMain.OS != "":
		mainSays, box = "on "+onMain.OS, "a "+onMain.OS+" box"
	case hostOS != at.OS:
		box = "a " + hostOS + " box"
	}
	return "", fail(exitRefused, "%s runs on %s at %.8s but %s on main, so a job could reach %s: "+
		"land the os change first, or give it a kind name of its own", name, at.OS, sha, mainSays, box)
}

// repoName is the name the daemon resolves a job's repo by: the node's repo
// when self is a registered node naming one; else the [repos] key whose
// checkout shares the git common dir of top (top may be a linked worktree);
// else the base name of the main repo holding top, which queue refuses as
// not in the registry and land uses only as its lease name.
func (a *app) repoName(ctx context.Context, self mesh.Path, top string) (string, error) {
	n, ok, err := mesh.GetNode(ctx, a.client, self.String())
	if err != nil {
		return "", err
	}
	if ok && n.Repo != "" {
		return n.Repo, nil
	}
	return a.repoNameAt(top), nil
}

// repoNameAt is repoName without a node: the [repos] key whose checkout
// shares the git common dir of top, else the base name of the main repo
// holding top.
func (a *app) repoNameAt(top string) string {
	common, ok := checkout.CommonDir(top)
	if !ok {
		return filepath.Base(top)
	}
	keys := make([]string, 0, len(a.cfg.Repos))
	for key := range a.cfg.Repos {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, p, err := a.cfg.RepoPath(key); err == nil {
			if c, ok := checkout.CommonDir(p); ok && c == common {
				return key
			}
		}
	}
	return filepath.Base(filepath.Dir(common))
}

// cmdRoutineGo tells a kind's instances to start on the jobs waiting.
func (a *app) cmdRoutineGo(args []string) error {
	pos, err := parse(a.flags("routine go"), args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || !kind.NameRE.MatchString(pos[0]) {
		return fail(exitUsage, routineUsage)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	self, err := a.identity(ctx, "routine")
	if err != nil {
		return err
	}
	m := mesh.Message{ID: newID(), Kind: "message", Text: "go", From: self.String(), To: mesh.RoutinePath(pos[0]).String()}
	if _, err := mesh.Send(ctx, a.client, m); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "sent %s\n", m.ID)
	return nil
}

// cmdRoutineWait finds the reply to a job in this node's inbox history, or
// follows the inbox until it arrives, and exits by the reply's verdict. A
// reply still unread is acked, leaving every other message unread.
func (a *app) cmdRoutineWait(args []string) error {
	fs := a.flags("routine wait")
	timeout := fs.Duration("timeout", 0, "give up after this long (exit 5; default 20m, plus the kind's timeout and 1m when the cwd repo defines it)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, routineUsage)
	}
	id := pos[0]
	if *timeout <= 0 {
		lookup, cancelLookup := cmdCtx()
		err := a.connect(lookup, "routine")
		if err == nil {
			*timeout = waitTimeout(lookup, a.client, id)
		}
		cancelLookup()
		if err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	timedOut := func(err error) error {
		if ctx.Err() != nil {
			return fail(exitTimeout, "timed out waiting for %s; the job may still be queued or running, and its reply stays in the stream "+
				"for 30 days: run bmesh routine wait %s again rather than queueing it twice", id, id)
		}
		return err
	}
	self, err := a.identity(ctx, "routine")
	if err != nil {
		return timedOut(err)
	}
	m, err := mesh.WaitReply(ctx, a.client, self, id)
	if err != nil {
		return timedOut(err)
	}
	// Ack it when still pending, so no wake follows and inbox does not print
	// it again; a reply inbox already read is simply not there.
	isReply := func(m mesh.Message) bool { return m.Kind == "reply" && m.Reply != nil && m.Reply.Job == id }
	if _, _, err := mesh.FetchMatch(ctx, a.client, self, isReply); err != nil {
		return timedOut(err)
	}
	return a.printVerdict(m.Reply.OK, m.Reply.Output)
}

// waitQueueing is how long wait allows a job to queue before it runs.
const waitQueueing = 20 * time.Minute

// waitTimeout is wait's default: waitQueueing plus the job's kind's timeout
// plus the 1m the daemon allows beyond it (its kind consumer's AckWait), and
// for a command kind the repo's setup timeout when it names a setup, when
// the cwd repo defines the kind the job was queued to; else waitQueueing.
func waitTimeout(ctx context.Context, c *mesh.Client, id string) time.Duration {
	top, kinds, err := cwdKinds()
	if err != nil || len(kinds) == 0 {
		return waitQueueing
	}
	name, ok, err := mesh.JobKind(ctx, c, id)
	if err != nil || !ok {
		return waitQueueing
	}
	for _, k := range kinds {
		if k.Name == name {
			d := waitQueueing + k.Timeout + time.Minute
			if rc, err := kind.LoadRepoConfig(top); err == nil && rc.Setup.Command != "" && !k.IsModel() {
				d += rc.Setup.Timeout
			}
			return d
		}
	}
	return waitQueueing
}

// cmdRoutineRun runs a kind in this worktree: the requester's own run of
// HEAD, holding the kind's slots, and records its result (with the model for
// a model kind). It needs a clean tree; a deterministic kind answers from a
// recorded result, and verify from one carried from HEAD's code run
// (carryVerify), unless --rerun.
func (a *app) cmdRoutineRun(args []string) error {
	fs := a.flags("routine run")
	commit := fs.String("commit", "HEAD", "commit to run (must be HEAD)")
	model := fs.String("model", "", "one of the kind's models (default the first)")
	effort := fs.String("effort", "", "a model kind's effort for this job (default the model's)")
	here := fs.Bool("here", false, "run a fresh or pooled kind in this worktree")
	rerun := fs.Bool("rerun", false, "run even when a result is recorded or would carry")
	text := fs.String("text", "", "text for a model kind")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 || !kind.NameRE.MatchString(pos[0]) {
		return fail(exitUsage, routineUsage)
	}
	name, jobArgs := pos[0], pos[1:]
	top, kinds, err := cwdKinds()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(kinds, func(k kind.Kind) bool { return k.Name == name })
	if i < 0 {
		return fail(exitRefused, "no kind %s in %s", name, top)
	}
	k := kinds[i]
	// Run here, it would record this OS's result as the kind's for the tree.
	if !k.RunsOn(hostOS) {
		return fail(exitRefused, "%s runs on %s; this box is %s: queue it", name, k.OS, hostOS)
	}
	if k.Mode != "inline" && !*here {
		return fail(exitRefused, "kind %s is %s; queue it, or pass --here to run it in this worktree", name, k.Mode)
	}
	if err := checkModel(*model); err != nil {
		return err
	}
	if *model != "" && !slices.Contains(k.ModelsList(), *model) {
		return fail(exitUsage, "kind %s has no model %q", name, *model)
	}
	if err := checkJobEffort(k, *effort); err != nil {
		return err
	}
	if *model == "" && k.IsModel() {
		*model = k.ModelsList()[0]
	}
	// Every inline run records against HEAD's tree, so none may run on
	// uncommitted changes.
	if err := refuseDirty(); err != nil {
		return err
	}
	head, err := gitRev("--verify", "--end-of-options", "HEAD^{commit}")
	if err != nil {
		return err
	}
	sha, err := gitRev("--verify", "--end-of-options", *commit+"^{commit}")
	if err != nil {
		return err
	}
	if sha != head {
		return fail(exitRefused, "inline runs run HEAD; check out %s or queue the job", *commit)
	}
	tree, err := gitRev("--verify", "--end-of-options", "HEAD^{tree}")
	if err != nil {
		return err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	self, err := a.identityOrHost(ctx, "routine")
	if err != nil {
		return err
	}
	// A verify over the code of a commit verified before answers from it.
	if k.Deterministic && !*rerun {
		from, err := a.carryVerify(ctx, name, top, a.repoNameAt(top), head, tree, self, *model, jobArgs, head)
		if err != nil {
			return err
		}
		if from != "" {
			return a.printRunVerdict(name, tree, *model, true, "", carriedHow(from))
		}
	}
	ok, out, how, err := a.runInline(ctx, k, top, head, tree, self, *model, *effort, *text, jobArgs, *rerun)
	if err != nil {
		return err
	}
	return a.printRunVerdict(name, tree, *model, ok, out, how)
}

// runInline runs kind k on HEAD (head, tree) of the worktree top for self,
// holding the kind's slots, and records the result; a deterministic kind is
// answered from its record unless rerun. It returns the verdict, the output
// and how it came to be: "recorded" or "answered from the record".
func (a *app) runInline(ctx context.Context, k kind.Kind, top, head, tree, self, model, effort, text string, jobArgs []string,
	rerun bool) (bool, string, string, error) {
	name := k.Name
	key := mesh.ResultKey(name, tree, jobArgs, model)
	if k.Deterministic && !rerun {
		r, ok, err := mesh.GetResult(ctx, a.client, key)
		if err != nil {
			return false, "", "", err
		}
		if ok {
			return r.OK, r.Output, "answered from the record", nil
		}
	}

	rc, err := kind.LoadRepoConfig(top)
	if err != nil {
		return false, "", "", err
	}
	// A command kind runs against the repo's installs; a model kind reads
	// diffs and needs none.
	if !k.IsModel() {
		if err := a.runSetup(ctx, top, rc.Setup); err != nil {
			return false, "", "", err
		}
	}
	var inst instance.Instance = instance.NewCommand(k)
	if k.IsModel() {
		claude := os.Getenv("BMESH_CLAUDE_COMMAND")
		if strings.TrimSpace(claude) == "" {
			claude = a.cfg.Runner.Claude
		}
		inst = instance.NewModel(k, model, claude)
	}
	capacity := func(slot string) (int, error) { return max(rc.Slots[slot], 1), nil }
	j := instance.Job{ID: newID(), Kind: name, Commit: head, Tree: tree, From: self, Reply: self,
		Args: jobArgs, Text: text, Dir: top, Effort: effort}
	var reply instance.Reply
	err = a.withLeases(k.Slots, self, k.Timeout+time.Minute, capacity, func(ctx context.Context) error {
		reply = inst.Run(ctx, j)
		return nil
	})
	if err != nil {
		return false, "", "", err
	}
	// A model run whose claude never answered is an infrastructure error,
	// like a failed setup: refused, never a recorded result.
	if !reply.Judged {
		return false, "", "", fail(exitRefused, "%s", reply.Unjudged())
	}
	// Every inline run that judged ran HEAD, so every one records (R12); only a
	// deterministic kind is answered from its record.
	putCtx, cancel := cmdCtx() // the run may have outlasted ctx
	defer cancel()
	patch := ""
	if model != "" {
		if patch, err = checkout.ReviewedPatch(putCtx, top, text, head); err != nil {
			fmt.Fprintf(a.stderr, "bmesh: %v; recorded without a patch-id\n", err)
		}
	}
	r := mesh.Result{Kind: name, Tree: tree, Commit: head, Args: jobArgs, OK: reply.OK,
		Output: mesh.Truncate(reply.Output), By: self, Model: model, PatchID: patch}
	if err := mesh.PutResult(putCtx, a.client, r); err != nil {
		return false, "", "", err
	}
	return reply.OK, reply.Output, "recorded", nil
}

// runSetup brings the worktree top's installs current (setup.Ensure, the
// tree required clean after) before a command kind runs there and records.
// A failed setup is refused (exit 3) with the tail of its output: an
// infrastructure error, never a recorded result.
func (a *app) runSetup(ctx context.Context, top string, s kind.Setup) error {
	start := time.Now()
	// The setup's own timeout bounds it, not the caller's deadline.
	ran, out, err := wsetup.Ensure(context.WithoutCancel(ctx), top, s, wsetup.Options{Clean: true})
	var se *wsetup.Error
	if errors.As(err, &se) {
		msg := se.Msg + "; nothing recorded"
		if tail := wsetup.Tail(out, redTail); tail != "" {
			msg += "\n" + tail
		}
		return fail(exitRefused, "%s", msg)
	}
	if err != nil {
		return err
	}
	if ran {
		fmt.Fprintf(a.stderr, "bmesh: setup ran (%s) in %s\n", s.Command, time.Since(start).Round(100*time.Millisecond))
	}
	return nil
}

// checkJobEffort refuses an effort the runner can't start a session at, and
// any effort for a command kind, which runs no model.
func checkJobEffort(k kind.Kind, effort string) error {
	if err := checkEffort(effort); err != nil {
		return err
	}
	if effort != "" && !k.IsModel() {
		return fail(exitUsage, "kind %s runs a command: --effort is for a model kind", k.Name)
	}
	return nil
}

// printRunVerdict prints a run's output and then its verdict line,
// `<kind> ok|fail t:<tree8>[ m:<model>] (<how>)`, a verified entry and how
// the result came to be: recorded, or answered from the record.
func (a *app) printRunVerdict(name, tree, model string, ok bool, out, how string) error {
	line := fmt.Sprintf("%s %s t:%s", name, okWord(ok), tree[:8])
	if model != "" {
		line += " m:" + model
	}
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	fmt.Fprintf(a.stdout, "%s%s (%s)\n", out, line, how)
	if !ok {
		return fail(exitError, "")
	}
	return nil
}

// refuseDirty refuses (exit 3) when the cwd's worktree has uncommitted
// changes or untracked files: a result recorded for HEAD's tree must be of
// HEAD's tree.
func refuseDirty() error {
	st, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if len(strings.TrimSpace(string(st))) > 0 {
		return fail(exitRefused, "worktree is dirty; commit first")
	}
	return nil
}

// printVerdict prints a run's output, ending in a newline, and exits 1 when
// the run failed.
func (a *app) printVerdict(ok bool, out string) error {
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	fmt.Fprint(a.stdout, out)
	if !ok {
		return fail(exitError, "")
	}
	return nil
}

// reviewKind is the kind a branch is reviewed by once: a second review job
// for a branch that has a review result needs --again.
const reviewKind = "review"

// priorReview finds the newest review result recorded for a commit of this
// branch: one whose commit is an ancestor of the worktree's HEAD but not of
// main (origin/main when the repo has an origin), so a landed spec's review
// in the history does not count. A result whose commit is unknown here is
// skipped.
func (a *app) priorReview(ctx context.Context, top string) (mesh.Result, bool, error) {
	results, err := mesh.ListResults(ctx, a.client, reviewKind)
	if err != nil {
		return mesh.Result{}, false, err
	}
	sort.Slice(results, func(i, j int) bool { return results[i].At > results[j].At })
	mainRef := "main"
	if hasOrigin(ctx, top) {
		mainRef = "origin/main"
	}
	onMain := func(commit string) bool {
		_, err := gitIn(ctx, top, "merge-base", "--is-ancestor", commit, mainRef)
		return err == nil
	}
	for _, r := range results {
		if r.Commit == "" {
			continue
		}
		if _, err := gitIn(ctx, top, "merge-base", "--is-ancestor", r.Commit, "HEAD"); err != nil {
			continue
		}
		if onMain(r.Commit) {
			continue
		}
		return r, true, nil
	}
	return mesh.Result{}, false, nil
}

func okWord(ok bool) string {
	if ok {
		return "ok"
	}
	return "fail"
}
