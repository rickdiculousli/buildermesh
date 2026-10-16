package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/instance"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const routineUsage = "usage: bmesh routine list [--json] | status [<kind>] [--json] | " +
	"queue <kind> [--commit C] [--model M] [--go] [--rerun] [--again] [--text T] [-- args...] | go <kind> | wait <job id> [--timeout D] | " +
	"run <kind> [--commit HEAD] [--model M] [--here] [--rerun] [--text T] [-- args...]"

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
	Kind          string          `json:"kind"`
	Mode          string          `json:"mode"`
	Deterministic bool            `json:"deterministic"`
	Command       string          `json:"command,omitempty"`
	Models        []string        `json:"models,omitempty"`
	Instances     int             `json:"instances"`
	Depth         int             `json:"depth"`
	Members       []routineMember `json:"members,omitempty"`
}

type routineMember struct {
	Path  string `json:"path"`
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
				r.Members = append(r.Members, routineMember{Path: n.Path, State: st, Model: n.Model})
			}
		}
		if r.Depth, err = mesh.Depth(ctx, a.client, name); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// printRows prints one JSON object per row, or a table; status adds a line
// per instance.
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
	fmt.Fprintln(tw, "KIND\tMODE\tDETERMINISTIC\tRUN\tDEPTH\tINSTANCE\tSTATE\tMODEL")
	for _, r := range rows {
		head := fmt.Sprintf("%s\t%s\t%s\t%s\t%d", r.Kind, dash(r.Mode), r.deterministic(), r.run(), r.Depth)
		if len(r.Members) == 0 {
			fmt.Fprintf(tw, "%s\t-\t-\t-\n", head)
		}
		for _, m := range r.Members {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", head, m.Path, m.State, dash(m.Model))
		}
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
	repo, err := a.repoName(ctx, self, top)
	if err != nil {
		return err
	}
	j := mesh.Job{ID: newID(), Kind: name, Repo: repo, Commit: *commit, Args: jobArgs,
		Model: *model, Text: *text, Go: *goNow, Rerun: *rerun}
	dup, err := mesh.Queue(ctx, a.client, self, j, top)
	if err != nil {
		return err
	}
	verb := "queued"
	if dup {
		verb = "duplicate"
	}
	fmt.Fprintf(a.stdout, "%s %s\n", verb, j.ID)
	return nil
}

// repoName is the name the daemon resolves a job's repo by: the node's repo
// when self is a registered node naming one; else the [repos] key whose
// checkout shares the git common dir of top (top may be a linked worktree);
// else the base name of the main repo holding top.
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
			return fail(exitTimeout, "timed out waiting for %s", id)
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
// plus the 1m the daemon allows beyond it (its kind consumer's AckWait), when
// the cwd repo defines the kind the job was queued to; else waitQueueing.
func waitTimeout(ctx context.Context, c *mesh.Client, id string) time.Duration {
	_, kinds, err := cwdKinds()
	if err != nil || len(kinds) == 0 {
		return waitQueueing
	}
	name, ok, err := mesh.JobKind(ctx, c, id)
	if err != nil || !ok {
		return waitQueueing
	}
	for _, k := range kinds {
		if k.Name == name {
			return waitQueueing + k.Timeout + time.Minute
		}
	}
	return waitQueueing
}

// cmdRoutineRun runs a kind in this worktree: the requester's own run of
// HEAD, holding the kind's slots, and records its result (with the model for
// a model kind). It needs a clean tree; a deterministic kind answers from a
// recorded result unless --rerun.
func (a *app) cmdRoutineRun(args []string) error {
	fs := a.flags("routine run")
	commit := fs.String("commit", "HEAD", "commit to run (must be HEAD)")
	model := fs.String("model", "", "one of the kind's models (default the first)")
	here := fs.Bool("here", false, "run a fresh or pooled kind in this worktree")
	rerun := fs.Bool("rerun", false, "run even when a result is recorded")
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
	if k.Mode != "inline" && !*here {
		return fail(exitRefused, "kind %s is %s; queue it, or pass --here to run it in this worktree", name, k.Mode)
	}
	if err := checkModel(*model); err != nil {
		return err
	}
	if *model != "" && !slices.Contains(k.ModelsList(), *model) {
		return fail(exitUsage, "kind %s has no model %q", name, *model)
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
	ok, out, how, err := a.runInline(ctx, k, top, head, tree, self, *model, *text, jobArgs, *rerun)
	if err != nil {
		return err
	}
	return a.printRunVerdict(name, tree, *model, ok, out, how)
}

// runInline runs kind k on HEAD (head, tree) of the worktree top for self,
// holding the kind's slots, and records the result; a deterministic kind is
// answered from its record unless rerun. It returns the verdict, the output
// and how it came to be: "recorded" or "answered from the record".
func (a *app) runInline(ctx context.Context, k kind.Kind, top, head, tree, self, model, text string, jobArgs []string,
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

	var inst instance.Instance = instance.NewCommand(k)
	if k.IsModel() {
		claude := os.Getenv("BMESH_CLAUDE_COMMAND")
		if strings.TrimSpace(claude) == "" {
			claude = a.cfg.Runner.Claude
		}
		inst = instance.NewModel(k, model, claude)
	}
	rc, err := kind.LoadRepoConfig(top)
	if err != nil {
		return false, "", "", err
	}
	capacity := func(slot string) (int, error) { return max(rc.Slots[slot], 1), nil }
	j := instance.Job{ID: newID(), Kind: name, Commit: head, Tree: tree, From: self, Reply: self,
		Args: jobArgs, Text: text, Dir: top}
	var reply instance.Reply
	err = a.withLeases(k.Slots, self, k.Timeout+time.Minute, capacity, func(ctx context.Context) error {
		reply = inst.Run(ctx, j)
		return nil
	})
	if err != nil {
		return false, "", "", err
	}
	// Every inline run ran HEAD, so every one records (R12); only a
	// deterministic kind is answered from its record.
	putCtx, cancel := cmdCtx() // the run may have outlasted ctx
	defer cancel()
	r := mesh.Result{Kind: name, Tree: tree, Commit: head, Args: jobArgs, OK: reply.OK,
		Output: mesh.Truncate(reply.Output), By: self, Model: model}
	if err := mesh.PutResult(putCtx, a.client, r); err != nil {
		return false, "", "", err
	}
	return reply.OK, reply.Output, "recorded", nil
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
