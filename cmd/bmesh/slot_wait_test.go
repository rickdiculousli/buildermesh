package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// slotWaitFast shortens the waits' poll for the test.
func slotWaitFast(t *testing.T) {
	t.Helper()
	poll := landPoll
	landPoll = 20 * time.Millisecond
	t.Cleanup(func() { landPoll = poll })
}

// slotLandRepo is newLandRepo whose verify needs slot x (capacity 1),
// beside a kind slow on x running $SLOW; both scripts see $MARKS, a dir of
// the test's.
func slotLandRepo(t *testing.T) (landRepo, string) {
	t.Helper()
	r := newLandRepo(t)
	writeFile(t, filepath.Join(r.wt, ".bmesh", "routines", "verify.toml"),
		"mode = \"inline\"\ndeterministic = true\nslots = [\"x\"]\ncommand = \"sh \\\"$HOOK\\\"\"\n")
	writeFile(t, filepath.Join(r.wt, ".bmesh", "routines", "slow.toml"), "mode = \"inline\"\nslots = [\"x\"]\ncommand = \"sh \\\"$SLOW\\\"\"\n")
	writeFile(t, filepath.Join(r.wt, ".bmesh", "config.toml"), "[slots]\nx = 1\n")
	meshtest.Git(t, r.wt, "add", "-A")
	meshtest.Git(t, r.wt, "commit", "-q", "--amend", "--no-edit")
	marks := t.TempDir()
	t.Setenv("MARKS", marks)
	slow := filepath.Join(r.root, "slow.sh")
	t.Setenv("SLOW", slow)
	return r, slow
}

// runBG runs bmesh with args in the background; its exit code arrives on
// the channel.
func runBG(args ...string) (out, errb *meshtest.SyncBuffer, res chan int) {
	out, errb, res = &meshtest.SyncBuffer{}, &meshtest.SyncBuffer{}, make(chan int, 1)
	go func() { res <- run(args, strings.NewReader(""), out, errb) }()
	return out, errb, res
}

// slotHolder reads x's one place.
func slotHolder(t *testing.T, c *mesh.Client, ctx context.Context) mesh.Lease {
	t.Helper()
	line, err := mesh.SlotQueue(ctx, c, "x", 1, time.Now())
	if err != nil || len(line.Held) != 1 {
		t.Fatalf("x: %+v %v", line, err)
	}
	return line.Held[0]
}

// Best-effort waiters A and B, then a landing's verify C: C takes x next,
// then A, then B. A routine run from a lead is best effort whatever its
// flags.
func TestLandingWaiterGoesFirst(t *testing.T) {
	c, ctx := setup(t)
	r, slow := slotLandRepo(t)
	marks := os.Getenv("MARKS")
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, slow, "touch \"$MARKS/$BMESH_FROM\"; while [ ! -e \"$MARKS/go\" ]; do sleep 0.05; done; echo ran\n")
	// The landing's verify passes only when neither A nor B ran before it.
	writeFile(t, os.Getenv("HOOK"), "for f in \"$MARKS\"/app-*; do [ -e \"$f\" ] && exit 1; done; exit 0\n")
	key, err := mesh.AcquireLease(ctx, c, "x", "someone", time.Minute, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	aOut, aErr, aRes := runBG("--node", "app-021", "routine", "run", "slow", "--rerun", "--here", "--commit", "HEAD")
	meshtest.Until(t, "A to wait", func() bool { return aOut.String() == "waiting for x (held by someone); #1\n" })
	bOut, bErr, bRes := runBG("--node", "app-022", "routine", "run", "slow")
	meshtest.Until(t, "B to wait", func() bool { return bOut.String() == "waiting for x (held by someone); #2\n" })
	out, errb, res := landBG(t, "app-012")
	until(t, out, "waiting for x (held by someone); #1\n")
	meshtest.Until(t, "A to move back", func() bool { return strings.Contains(aOut.String(), "waiting for x (held by someone); #2\n") })

	if err := mesh.ReleaseLease(ctx, c, key, "someone"); err != nil {
		t.Fatal(err)
	}
	landEnds(t, res, 0, out, errb)
	meshtest.Until(t, "A to run", func() bool { return exists(filepath.Join(marks, "app-021")) })
	if l := slotHolder(t, c, ctx); l.Holder != "app-021" || l.Priority != mesh.BestEffort {
		t.Fatalf("A's lease: %+v", l)
	}
	if exists(filepath.Join(marks, "app-022")) {
		t.Fatal("B ran beside A")
	}
	writeFile(t, filepath.Join(marks, "go"), "")
	for _, r := range []struct {
		out, errb *meshtest.SyncBuffer
		res       chan int
	}{{aOut, aErr, aRes}, {bOut, bErr, bRes}} {
		select {
		case code := <-r.res:
			if code != 0 || !strings.HasSuffix(r.out.String(), "ran\nslow ok t:"+gitOut(t, "rev-parse", "HEAD^{tree}")[:8]+" (recorded)\n") {
				t.Fatalf("run: %d %q %q", code, r.out.String(), r.errb.String())
			}
		case <-time.After(meshtest.Within(t)):
			t.Fatalf("run never ended: %q", r.out.String())
		}
	}
	assertNoLeases(t, c, ctx)
}

// A landing whose verify needs x, held by a best-effort run, takes it: the
// run's child is killed, its lease released, and it says so and waits
// again, as urgent; it runs again after the landing, and an urgent waiter
// then waits behind it instead of preempting it a second time.
func TestLandingPreemptsBestEffortRunOnce(t *testing.T) {
	c, ctx := setup(t)
	r, slow := slotLandRepo(t)
	marks := os.Getenv("MARKS")
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, slow, "if [ -e \"$MARKS/first\" ]; then touch \"$MARKS/second\"; while [ ! -e \"$MARKS/go\" ]; do sleep 0.05; done; echo second; "+
		"else touch \"$MARKS/first\"; while :; do sleep 0.05; done; fi\n")

	pOut, pErr, pRes := runBG("--node", "app-011", "routine", "run", "slow")
	meshtest.Until(t, "the run to start", func() bool { return exists(filepath.Join(marks, "first")) })
	if l := slotHolder(t, c, ctx); l.Holder != "app-011" || l.Priority != mesh.BestEffort {
		t.Fatalf("the run's lease: %+v", l)
	}

	out, errb, res := landBG(t, "app-012")
	landEnds(t, res, 0, out, errb)
	if !strings.Contains(out.String(), "waiting for x (held by app-011, best effort); #1\n") {
		t.Fatalf("land:\n%s", out.String())
	}
	preempted := regexp.MustCompile(`^preempted by app-012 \(land of ` + regexp.QuoteMeta(r.wt) + `\); waiting again at #[12]\n$`)
	meshtest.Until(t, "the run to run again", func() bool { return exists(filepath.Join(marks, "second")) })
	if !preempted.MatchString(pOut.String()) {
		t.Fatalf("the run's output %q", pOut.String())
	}
	if l := slotHolder(t, c, ctx); l.Holder != "app-011" || l.Priority != mesh.Urgent {
		t.Fatalf("the run's lease again: %+v", l)
	}

	// The operator's run is urgent, but waits behind the run that yielded once.
	oOut, oErr, oRes := runBG("--node", "operator", "routine", "run", "slow", "--rerun")
	meshtest.Until(t, "the operator's run to wait", func() bool { return oOut.String() == "waiting for x (held by app-011, urgent); #1\n" })
	time.Sleep(10 * landPoll)
	if l := slotHolder(t, c, ctx); l.Yield != "" || !preempted.MatchString(pOut.String()) {
		t.Fatalf("asked to yield twice: %+v %q", l, pOut.String())
	}
	writeFile(t, filepath.Join(marks, "go"), "")
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")[:8]
	for _, r := range []struct {
		out, errb *meshtest.SyncBuffer
		res       chan int
	}{{pOut, pErr, pRes}, {oOut, oErr, oRes}} {
		select {
		case code := <-r.res:
			if code != 0 || !strings.HasSuffix(r.out.String(), "second\nslow ok t:"+tree+" (recorded)\n") {
				t.Fatalf("run: %d %q %q", code, r.out.String(), r.errb.String())
			}
		case <-time.After(meshtest.Within(t)):
			t.Fatalf("run never ended: %q", r.out.String())
		}
	}
	assertNoLeases(t, c, ctx)
}

// A working node's run puts its wait on the node's status line, and gives
// the line back once it holds the slot.
func TestWaitShowsOnStatusLine(t *testing.T) {
	c, ctx := setup(t)
	slotWaitFast(t)
	kindRepo(t, map[string]string{"game": "mode = \"inline\"\nslots = [\"game\"]\ncommand = \"echo played\"\n"})
	if _, err := mesh.WriteState(ctx, c, "app-041", mesh.State{State: "working", Text: "building", By: "node"}); err != nil {
		t.Fatal(err)
	}
	key, err := mesh.AcquireRanked(ctx, c, "game", "app-040", mesh.BestEffort, time.Minute, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	out, errb, res := runBG("--node", "app-041", "routine", "run", "game")
	text := func() string {
		s, _, err := mesh.ReadState(ctx, c, "app-041")
		if err != nil {
			t.Fatal(err)
		}
		return s.Text
	}
	meshtest.Until(t, "the wait on the status line", func() bool { return text() == "waiting for game (held by app-040, best effort); #1" })
	if err := mesh.ReleaseLease(ctx, c, key, "app-040"); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-res:
		if code != 0 || text() != "building" {
			t.Fatalf("run: %d %q %q; state %q", code, out.String(), errb.String(), text())
		}
	case <-time.After(meshtest.Within(t)):
		t.Fatal("the run never ended")
	}
}
