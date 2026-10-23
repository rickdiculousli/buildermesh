package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/daemon"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

// sendRunner is a runner whose threads are idle and which records sends.
type sendRunner struct {
	mu   sync.Mutex
	sent []string
}

func (r *sendRunner) Start(context.Context, runner.StartSpec) (runner.Started, error) {
	return runner.Started{}, nil
}
func (r *sendRunner) Status(context.Context, string) (runner.Status, error) {
	return runner.Status{Known: true}, nil
}
func (r *sendRunner) Send(_ context.Context, thread, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, thread+": "+text)
	return nil
}
func (r *sendRunner) Interrupt(context.Context, string) error { return nil }

// readyLead registers app-012 for newLandRepo's worktree, on this box with
// a thread, started, and this box's daemon as one that lands.
func readyLead(t *testing.T, c *mesh.Client, ctx context.Context, r landRepo) {
	t.Helper()
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Worktree: r.wt, Spec: "012",
		Runner: mesh.NodeRunner{Kind: "t3", Thread: "th-1"}})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	daemonStarted(t, c, ctx, testHost, daemon.CanLand)
}

func daemonStarted(t *testing.T, c *mesh.Client, ctx context.Context, host string, can ...string) {
	t.Helper()
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "daemon." + host, Event: "started", Text: "unknown", Can: can}); err != nil {
		t.Fatal(err)
	}
}

// The daemon's deps for this test's mesh, its steps run in-process with
// run, in the step's directory (the worktree, the test's cwd).
func cliDaemon(t *testing.T, c *mesh.Client, rn runner.Runner) daemon.Deps {
	t.Helper()
	cfg, err := config.Load(os.Getenv("BMESH_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	return daemon.Deps{Client: c, Cfg: cfg, Runner: rn, Host: testHost,
		RunStep: func(ctx context.Context, dir string, args ...string) (string, int, error) {
			if wd, _ := os.Getwd(); !mesh.SameDir(wd, dir) {
				t.Errorf("step in %s, cwd %s", dir, wd)
			}
			var out bytes.Buffer
			code := run(args, strings.NewReader(""), &out, &out)
			return out.String(), code, nil
		}}
}

// ready, then the daemon's pass: land lands the change, done records it
// with the lead's line and origin/main, and the lead gets one final wake.
func TestReadyThenTheDaemonLands(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	readyLead(t, c, ctx, r)
	head := gitOut(t, "rev-parse", "HEAD")
	out, errs, code := runCLI(t, as("app-012"), "ready", "--text", "built the thing")
	if code != 0 || out != "ready: testhost's daemon lands "+head[:8]+"; it wakes you only if land needs you\n" {
		t.Fatalf("ready: %d %q %q", code, out, errs)
	}
	if s := stateOf(t, c, ctx, "app-012"); s != "landing" {
		t.Fatalf("state %s", s)
	}
	if req, _, ok, err := mesh.GetReady(ctx, c, "app-012"); err != nil || !ok || req.Head != head || req.Text != "built the thing" || req.Phase != mesh.PhaseReady {
		t.Fatalf("request %+v %v %v", req, ok, err)
	}
	rn := &sendRunner{}
	daemon.LandPass(ctx, cliDaemon(t, c, rn), daemon.NewActivity())
	rec := gitOut(t, "rev-parse", "HEAD")
	if r.originMain(t) != rec || !strings.Contains(r.specOnOrigin(t), "status: done\n") {
		t.Fatalf("not landed: origin %s, HEAD %s\n%s", r.originMain(t), rec, r.specOnOrigin(t))
	}
	if s := stateOf(t, c, ctx, "app-012"); s != "done" {
		t.Fatalf("state %s", s)
	}
	p, _ := mesh.ParsePath("app-012")
	last, ok, err := mesh.Last(ctx, c, mesh.EventSubject(p, "done"))
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if want := `"text":"built the thing; landed ` + rec[:8] + `, origin/main at ` + rec[:8] + ` contains it"`; !strings.Contains(string(last.Data), want) {
		t.Fatalf("done event %s, want %s", last.Data, want)
	}
	if len(rn.sent) != 1 || rn.sent[0] != "th-1: "+daemon.LandedText(rec, "012", rec) {
		t.Fatalf("sends %q", rn.sent)
	}
	if _, _, ok, _ := mesh.GetReady(ctx, c, "app-012"); ok {
		t.Fatal("request left")
	}
	assertNoLeases(t, c, ctx)
}

// A land the daemon's step refuses (the spec doesn't read building) wakes
// the lead with the refusal and leaves it working.
func TestReadyRefusedLandWakesTheLead(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	readyLead(t, c, ctx, r)
	if _, errs, code := runCLI(t, as("app-012"), "ready", "--text", "x"); code != 0 {
		t.Fatal(code, errs)
	}
	writeFile(t, os.Getenv("HOOK"), "exit 1\n") // verify fails
	rn := &sendRunner{}
	daemon.LandPass(ctx, cliDaemon(t, c, rn), daemon.NewActivity())
	if len(rn.sent) != 1 || !strings.HasPrefix(rn.sent[0], "th-1: bmesh: land refused and needs you: verify failed at t:") {
		t.Fatalf("sends %q", rn.sent)
	}
	if s := stateOf(t, c, ctx, "app-012"); s != "working" {
		t.Fatalf("state %s", s)
	}
	if _, _, ok, _ := mesh.GetReady(ctx, c, "app-012"); ok {
		t.Fatal("request left")
	}
	// Fixed, ready is accepted again.
	writeFile(t, os.Getenv("HOOK"), "true\n")
	if _, errs, code := runCLI(t, as("app-012"), "ready", "--text", "x"); code != 0 {
		t.Fatal(code, errs)
	}
}

func TestReadyRefusals(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	refused := func(node, want string) {
		t.Helper()
		_, errs, code := runCLI(t, as(node), "ready", "--text", "x")
		if code != 3 || !strings.Contains(errs, want) {
			t.Fatalf("%s: %d %q, want %q", node, code, errs, want)
		}
	}
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Worktree: r.wt, Spec: "012",
		Runner: mesh.NodeRunner{Kind: "t3", Thread: "th-1"}})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	// No daemon has said it lands, then an older one.
	refused("app-012", "this box's daemon doesn't land for leads; run bmesh land")
	daemonStarted(t, c, ctx, testHost)
	refused("app-012", "this box's daemon doesn't land for leads; run bmesh land")
	daemonStarted(t, c, ctx, testHost, daemon.CanLand)

	putNode(t, c, ctx, mesh.Node{Path: "app-012.web", Role: "executor", Worktree: r.wt, Runner: mesh.NodeRunner{Thread: "x"}})
	refused("app-012.web", "is a executor; only a lead's daemon lands it")
	putNode(t, c, ctx, mesh.Node{Path: "app-013", Host: "other", Worktree: r.wt, Spec: "012", Runner: mesh.NodeRunner{Thread: "x"}})
	refused("app-013", "runs on other, not this box (testhost)")
	putNode(t, c, ctx, mesh.Node{Path: "app-014", Worktree: r.wt, Spec: "012"})
	refused("app-014", "has no thread for the daemon to wake")
	putNode(t, c, ctx, mesh.Node{Path: "app-015", Worktree: r.repo, Spec: "012", Runner: mesh.NodeRunner{Thread: "x"}})
	refused("app-015", "not this checkout")

	writeFile(t, r.wt+"/dirty.txt", "x\n")
	refused("app-012", "the worktree has changes not committed")
	if err := os.Remove(r.wt + "/dirty.txt"); err != nil {
		t.Fatal(err)
	}
	sendTo(t, c, ctx, "app-012", mesh.Message{From: "operator", Kind: "message", Text: "wait"})
	refused("app-012", "1 unread message(s) from the operator")
	if _, _, code := runCLI(t, as("app-012"), "inbox"); code != 0 {
		t.Fatal(code)
	}

	// Held: accepted, and the daemon waits for the release.
	if err := mesh.PutLeadHold(ctx, c, mesh.LeadHold{Path: "app-012", Gate: mesh.GateLand, By: "operator", At: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if out, errs, code := runCLI(t, as("app-012"), "ready", "--text", "x"); code != 0 || !strings.HasPrefix(out, "ready: ") {
		t.Fatalf("ready while held: %d %q %q", code, out, errs)
	}
	_, errs, code := runCLI(t, as("app-012"), "ready", "--text", "x")
	if code != 3 || errs != "bmesh: already ready; bmesh ready --cancel first\n" {
		t.Fatalf("second ready: %d %q", code, errs)
	}
}

// --cancel with no daemon working the request ends it at once and moves
// the lead to working; after the push it says it is too late.
func TestReadyCancel(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	readyLead(t, c, ctx, r)
	if _, errs, code := runCLI(t, as("app-012"), "ready", "--text", "x"); code != 0 {
		t.Fatal(code, errs)
	}
	out, errs, code := runCLI(t, as("app-012"), "ready", "--cancel")
	if code != 0 || out != "cancelled: nothing is pushed; land in your own turn with bmesh land, or bmesh ready again\n" {
		t.Fatalf("cancel: %d %q %q", code, out, errs)
	}
	if s := stateOf(t, c, ctx, "app-012"); s != "working" {
		t.Fatalf("state %s", s)
	}
	if _, _, ok, _ := mesh.GetReady(ctx, c, "app-012"); ok {
		t.Fatal("request left")
	}
	if _, errs, code := runCLI(t, as("app-012"), "ready", "--cancel"); code != 3 || !strings.Contains(errs, "has no ready request") {
		t.Fatalf("second cancel: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, as("app-012"), "ready", "--text", "x"); code != 0 {
		t.Fatal(code, errs)
	}
	req, rev, _, _ := mesh.GetReady(ctx, c, "app-012")
	req.Phase, req.Commit = mesh.PhaseLanded, "0123456789abcdef"
	if _, err := mesh.UpdateReady(ctx, c, req, rev); err != nil {
		t.Fatal(err)
	}
	if out, _, code := runCLI(t, as("app-012"), "ready", "--cancel"); code != 0 || !strings.HasPrefix(out, "too late: landed 01234567;") {
		t.Fatalf("cancel after the push: %d %q", code, out)
	}
}
