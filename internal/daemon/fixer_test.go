package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

const ownModule = "example.com/bm"

// starts counts the fixers the daemons started; each start registers the
// node `bmesh fix` would, <repo>-fix-<N> with N one past the highest.
type starts struct {
	mu    sync.Mutex
	paths []string
}

func (s *starts) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.paths...)
}

// fixerSetup is a fixture whose registry has "bm", bmesh's own repo (its
// go.mod declares the binary's module), on host (any when ""), and whose
// fixer starts are faked through st.
func fixerSetup(t *testing.T, host string, st *starts) *fixture {
	t.Helper()
	f := setup(t)
	repo := filepath.Join(t.TempDir(), "bm")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "init", "-q", "-b", "main")
	meshtest.CommitFiles(t, repo, map[string]string{"go.mod": "module " + ownModule + "\n"})
	f.d.Cfg.Repos = map[string]config.Repo{"bm": {Name: "bm", Path: repo, Mode: "ship", Host: host}}
	f.d.Module = ownModule
	f.d.StartFixer = fakeFix(f.c, st)
	return f
}

// fakeFix starts a fixer as `bmesh fix` does, as far as the daemon reads it:
// it refuses while one is live and registers the next.
func fakeFix(c *mesh.Client, st *starts) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		st.mu.Lock()
		defer st.mu.Unlock()
		nodes, err := mesh.ListNodes(ctx, c)
		if err != nil {
			return "", err
		}
		states, err := mesh.ReadAllStates(ctx, c)
		if err != nil {
			return "", err
		}
		fx := mesh.ReadFixers(nodes, states, nil, "bm")
		if fx.Live.Path != "" {
			return "", fmt.Errorf("refused: fixer %s is live", fx.Live.Path)
		}
		path := fmt.Sprintf("bm-fix-%d", fx.Next)
		if err := mesh.PutNode(ctx, c, mesh.Node{Path: path, Role: "fixer", Repo: "bm", Family: path, Host: testHost,
			StartedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
			return "", err
		}
		st.paths = append(st.paths, path)
		return "fix: " + path + " started", nil
	}
}

func (f *fixture) feedback(t *testing.T, repo, at string) uint64 {
	t.Helper()
	seq, err := mesh.PublishFeedback(f.ctx, f.c, mesh.Feedback{From: "bm-030", Family: "bm-build-1", Repo: repo, Text: "friction", At: at})
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

// waitStarts waits until want fixers were started, then a little longer,
// so a second start would show.
func waitStarts(t *testing.T, st *starts, want int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(st.list()) < want && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	return st.list()
}

// A feedback message naming bmesh's own repo starts one fixer at once,
// though the next pass is an hour off; two daemons hearing it start one.
func TestFeedbackStartsOneFixerAcrossDaemons(t *testing.T) {
	st := &starts{}
	a := fixerSetup(t, "", st)
	b := a.d
	b.Host = "other"
	ctx, cancel := context.WithCancel(a.ctx)
	var wg sync.WaitGroup
	for _, d := range []Deps{a.d, b} {
		wg.Add(1)
		go func() { defer wg.Done(); fixerLoop(ctx, d, time.Hour) }()
	}
	t.Cleanup(func() { cancel(); wg.Wait() })
	time.Sleep(300 * time.Millisecond) // both loops' first pass found nothing, and they follow feedback
	if got := st.list(); len(got) != 0 {
		t.Fatalf("started with nothing pending: %v", got)
	}
	a.feedback(t, "bm", "")
	if got := waitStarts(t, st, 1); len(got) != 1 || got[0] != "bm-fix-1" {
		t.Fatalf("starts %v", got)
	}
	if claims := a.daemonEvents(t, "fixer"); len(claims)+len(eventsOf(t, a, "other", "fixer")) != 1 {
		t.Fatalf("claims %+v", claims)
	}
	// More feedback while it is live starts nothing.
	a.feedback(t, "bm", "")
	time.Sleep(500 * time.Millisecond)
	if got := st.list(); len(got) != 1 {
		t.Fatalf("second start %v", got)
	}
}

func eventsOf(t *testing.T, f *fixture, host, event string) []mesh.Stored {
	t.Helper()
	hist, err := mesh.History(f.ctx, f.c, mesh.DaemonSubject(host, event), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return hist
}

// Racing passes of several daemons on one pending issue start one fixer:
// the claim lets one through.
func TestRacingPassesStartOneFixer(t *testing.T) {
	st := &starts{}
	a := fixerSetup(t, "", st)
	a.feedback(t, "bm", "")
	var wg sync.WaitGroup
	for i := range 4 {
		d := a.d
		d.Host = fmt.Sprintf("box-%d", i)
		d.StartFixer = func(context.Context) (string, error) {
			// Two fix processes can both read no live fixer before
			// either registers one: only the claim keeps this to one.
			st.mu.Lock()
			defer st.mu.Unlock()
			st.paths = append(st.paths, d.Host)
			return "", nil
		}
		wg.Add(1)
		go func() { defer wg.Done(); fixerOnce(a.ctx, d) }()
	}
	wg.Wait()
	if got := st.list(); len(got) != 1 {
		t.Fatalf("starts %v", got)
	}
}

// A pending issue with no live fixer starts one on the next pass: after a
// restart, and after a fixer ended with an issue that came in after its
// start. One a fixer had at its start and left doesn't start another.
func TestPendingIssueStartsFixerOnThePass(t *testing.T) {
	st := &starts{}
	f := fixerSetup(t, testHost, st)
	f.feedback(t, "bm", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
	fixerOnce(f.ctx, f.d)
	if got := st.list(); len(got) != 1 || got[0] != "bm-fix-1" {
		t.Fatalf("restart pass: %v", got)
	}
	fixerOnce(f.ctx, f.d)
	if got := st.list(); len(got) != 1 {
		t.Fatalf("while live: %v", got)
	}
	// It ended done and left the issue it started with: no second fixer.
	meshtest.ForceState(t, f.c, "bm-fix-1", mesh.State{State: "done", By: "node"})
	fixerOnce(f.ctx, f.d)
	if got := st.list(); len(got) != 1 {
		t.Fatalf("left issue restarted: %v", got)
	}
	// An issue that came after its start (its last list missed it) does.
	f.feedback(t, "bm", time.Now().Add(time.Minute).UTC().Format(time.RFC3339))
	fixerOnce(f.ctx, f.d)
	if got := st.list(); len(got) != 2 || got[1] != "bm-fix-2" {
		t.Fatalf("after done: %v", got)
	}
	// The fixer drained what it ruled on; an issue a spec on main already
	// names (it stopped before draining) is not open.
	meshtest.ForceState(t, f.c, "bm-fix-2", mesh.State{State: "done", By: "node"})
	all, err := mesh.ListAllFeedback(f.ctx, f.c)
	if err != nil || len(all) != 2 {
		t.Fatalf("%v %v", all, err)
	}
	if _, _, err := mesh.DeleteFeedback(f.ctx, f.c, []uint64{all[0].Seq, all[1].Seq}); err != nil {
		t.Fatal(err)
	}
	seq := f.feedback(t, "bm", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	meshtest.CommitFiles(t, f.d.Cfg.Repos["bm"].Path, map[string]string{"docs/proposals/070-x.md": fmt.Sprintf(
		"---\nid: 070\ntitle: X\nkind: refinement\nstatus: accepted\nfrom_feedback: bm-build-1 (bm-030) #%d\n---\n\n## Proposal\nA fix.\n", seq)})
	if _, open, err := readFixQueue(f.ctx, f.c, "bm", f.d.Cfg.Repos["bm"].Path, f.d.Cfg.Spec.Glob); err != nil || len(open) != 0 {
		t.Fatalf("open %v %v", open, err)
	}
	fixerOnce(f.ctx, f.d)
	if got := st.list(); len(got) != 2 {
		t.Fatalf("named issue started a fixer: %v", got)
	}
}

// A blocked fixer is never restarted; feedback naming another repo, a
// registry with no own row, another box's row and a binary that doesn't
// say its module start nothing.
func TestFixerNotStarted(t *testing.T) {
	st := &starts{}
	f := fixerSetup(t, testHost, st)
	f.feedback(t, "other", "")
	fixerOnce(f.ctx, f.d)
	if got := st.list(); len(got) != 0 {
		t.Fatalf("another repo's feedback: %v", got)
	}
	f.feedback(t, "bm", "")
	for name, d := range map[string]func(Deps) Deps{
		"no own row":   func(d Deps) Deps { d.Module = "example.com/else"; return d },
		"no module":    func(d Deps) Deps { d.Module = ""; return d },
		"another host": func(d Deps) Deps { d.Host = "elsewhere"; return d },
	} {
		fixerOnce(f.ctx, d(f.d))
		if got := st.list(); len(got) != 0 {
			t.Fatalf("%s: %v", name, got)
		}
	}
	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: "bm-fix-1", Role: "fixer", Repo: "bm", Family: "bm-fix-1",
		StartedAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	meshtest.ForceState(t, f.c, "bm-fix-1", mesh.State{State: "blocked", By: "node", Text: "needs the operator"})
	fixerOnce(f.ctx, f.d)
	if got := st.list(); len(got) != 0 {
		t.Fatalf("blocked fixer restarted: %v", got)
	}
}
