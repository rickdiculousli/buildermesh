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
	// At #2 while the landing's waiter record outlives its wait, then #1.
	line := `preempted by app-012 \(land of ` + regexp.QuoteMeta(r.wt) + `\); waiting again at #`
	preempted := regexp.MustCompile(`^(?:` + line + `2\n)?` + line + `[12]\n$`)
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

// multiSlotRepo is a repo whose kind p needs x and y and q needs y and x
// (both running $SLOW), r needs x alone, and $MARKS is a dir of the test's.
func multiSlotRepo(t *testing.T) (marks string) {
	t.Helper()
	slotWaitFast(t)
	kind := func(slots string) string {
		return "mode = \"inline\"\nslots = [" + slots + "]\ncommand = \"sh \\\"$SLOW\\\"\"\n"
	}
	kindRepo(t, map[string]string{"p": kind(`"x", "y"`), "q": kind(`"y", "x"`), "r": kind(`"x"`)})
	marks = t.TempDir()
	t.Setenv("MARKS", marks)
	slow := filepath.Join(t.TempDir(), "slow.sh")
	writeFile(t, slow, "touch \"$MARKS/$BMESH_FROM\"; while [ ! -e \"$MARKS/go\" ]; do sleep 0.05; done; echo ran\n")
	t.Setenv("SLOW", slow)
	return marks
}

// heldOf reads slot's one place.
func heldOf(t *testing.T, c *mesh.Client, ctx context.Context, slot string) []mesh.Lease {
	t.Helper()
	line, err := mesh.SlotQueue(ctx, c, slot, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return line.Held
}

// ends waits for a background run to exit code with out ending in suffix.
func ends(t *testing.T, res chan int, code int, out, errb *meshtest.SyncBuffer, suffix string) {
	t.Helper()
	select {
	case got := <-res:
		if got != code || !strings.HasSuffix(out.String(), suffix) {
			t.Fatalf("run: %d %q %q", got, out.String(), errb.String())
		}
	case <-time.After(meshtest.Within(t)):
		t.Fatalf("run never ended: %q", out.String())
	}
}

// A run waiting for its second slot keeps the first renewed, and two kinds
// naming the same slots in opposite orders wait in one order: never each
// holding what the other waits for.
func TestMultiSlotWaitRenewsAndNeverDeadlocks(t *testing.T) {
	c, ctx := setup(t)
	marks := multiSlotRepo(t)
	key, err := mesh.AcquireLease(ctx, c, "y", "someone", time.Minute, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	aOut, aErr, aRes := runBG("--node", "app-051", "routine", "run", "p")
	meshtest.Until(t, "A to wait for y", func() bool { return aOut.String() == "waiting for y (held by someone); #1\n" })
	x := heldOf(t, c, ctx, "x")
	if len(x) != 1 || x[0].Holder != "app-051" {
		t.Fatalf("x while A waits: %+v", x)
	}
	meshtest.Until(t, "A to renew x", func() bool {
		h := heldOf(t, c, ctx, "x")
		return len(h) == 1 && h[0].ExpiresAt.After(x[0].ExpiresAt)
	})
	bOut, bErr, bRes := runBG("--node", "app-052", "routine", "run", "q")
	meshtest.Until(t, "B to wait for x", func() bool { return bOut.String() == "waiting for x (held by app-051, best effort); #1\n" })
	if y := heldOf(t, c, ctx, "y"); len(y) != 1 || y[0].Holder != "someone" {
		t.Fatalf("B took y: %+v", y)
	}
	if err := mesh.ReleaseLease(ctx, c, key, "someone"); err != nil {
		t.Fatal(err)
	}
	meshtest.Until(t, "A to run", func() bool { return exists(filepath.Join(marks, "app-051")) })
	writeFile(t, filepath.Join(marks, "go"), "")
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")[:8]
	ends(t, aRes, 0, aOut, aErr, "ran\np ok t:"+tree+" (recorded)\n")
	ends(t, bRes, 0, bOut, bErr, "ran\nq ok t:"+tree+" (recorded)\n")
	assertNoLeases(t, c, ctx)
}

// A best-effort run asked to yield a place it took while it waits for
// another gives it up and waits again; one interrupted while it waits
// gives back what it took.
func TestMultiSlotWaitYieldsAndReleases(t *testing.T) {
	c, ctx := setup(t)
	marks := multiSlotRepo(t)
	key, err := mesh.AcquireLease(ctx, c, "y", "someone", time.Minute, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	aOut, aErr, aRes := runBG("--node", "app-051", "routine", "run", "p")
	meshtest.Until(t, "A to wait for y", func() bool { return aOut.String() == "waiting for y (held by someone); #1\n" })
	oOut, oErr, oRes := runBG("--node", "operator", "routine", "run", "r")
	meshtest.Until(t, "the operator's run to start", func() bool { return exists(filepath.Join(marks, "operator")) })
	meshtest.Until(t, "A to wait again", func() bool {
		return strings.HasSuffix(aOut.String(), "preempted by operator (the operator's run); waiting again at #1\n")
	})
	writeFile(t, filepath.Join(marks, "go"), "")
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")[:8]
	ends(t, oRes, 0, oOut, oErr, "ran\nr ok t:"+tree+" (recorded)\n")
	meshtest.Until(t, "A to take x again", func() bool {
		h := heldOf(t, c, ctx, "x")
		return len(h) == 1 && h[0].Holder == "app-051" && h[0].Priority == mesh.Urgent
	})

	sigs := make(chan os.Signal, 1)
	orig := leaseSignals
	leaseSignals = func() (<-chan os.Signal, func()) { return sigs, func() {} }
	t.Cleanup(func() { leaseSignals = orig })
	// A took its signals before the swap; the next run takes the fake.
	if err := mesh.ReleaseLease(ctx, c, key, "someone"); err != nil {
		t.Fatal(err)
	}
	ends(t, aRes, 0, aOut, aErr, "ran\np ok t:"+tree+" (recorded)\n")
	if _, err := mesh.AcquireLease(ctx, c, "y", "someone", time.Minute, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	iOut, iErr, iRes := runBG("--node", "app-053", "routine", "run", "p", "--rerun")
	meshtest.Until(t, "the run to wait for y", func() bool { return strings.HasPrefix(iOut.String(), "waiting for y (held by someone); #1\n") })
	sigs <- os.Interrupt
	select {
	case code := <-iRes:
		if code != 1 || iErr.String() != "bmesh: interrupted\n" {
			t.Fatalf("interrupted: %d %q", code, iErr.String())
		}
	case <-time.After(meshtest.Within(t)):
		t.Fatal("the run never ended")
	}
	if x := heldOf(t, c, ctx, "x"); len(x) != 0 {
		t.Fatalf("x kept after an interrupt: %+v", x)
	}
}
