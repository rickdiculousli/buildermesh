package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

const testHost = "testhost"

// fixture is a provisioned server, a t3 runner backed by testdata/fake-t3.sh
// and Deps whose clock the test moves.
type fixture struct {
	ctx context.Context
	c   *mesh.Client
	d   Deps
	now time.Time // what d.Now returns
	log string    // the fake t3's call log
}

func setup(t *testing.T) *fixture {
	t.Helper()
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-t3.sh"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_T3_COMMAND", "sh "+fake)
	log := filepath.Join(t.TempDir(), "t3.log")
	t.Setenv("FAKE_T3_LOG", log)
	t.Setenv("FAKE_T3_STATUS", "idle")
	t.Setenv("FAKE_T3_SEND_EXIT", "0")
	cfg, err := config.Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{ctx: ctx, c: c, now: time.Now(), log: log}
	f.d = Deps{
		Client: c, Cfg: cfg, Host: testHost,
		Runner: runner.T3{Dispatch: "t3_dispatch.py", Dir: t.TempDir()},
		Now:    func() time.Time { return f.now },
	}
	f.d.defaults()
	return f
}

// lead registers a lead on the test host with the given thread and state.
func (f *fixture) lead(t *testing.T, path, thread, state string) {
	t.Helper()
	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: path, Role: "lead", Host: testHost,
		Runner: mesh.NodeRunner{Kind: "t3", Thread: thread}}); err != nil {
		t.Fatal(err)
	}
	f.setState(t, path, state)
}

func (f *fixture) setState(t *testing.T, path, state string) {
	t.Helper()
	if state == "" || state == "idle" {
		return
	}
	if state != "working" {
		if _, err := mesh.WriteState(f.ctx, f.c, path, mesh.State{State: "working", By: "node"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mesh.WriteState(f.ctx, f.c, path, mesh.State{State: state, By: "node"}); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) send(t *testing.T, to string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := mesh.Send(f.ctx, f.c, mesh.Message{From: "rick", To: to, Text: "hi"}); err != nil {
			t.Fatal(err)
		}
	}
}

// sends returns the fake t3's "send" lines.
func (f *fixture) sends(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(f.log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "send ") {
			out = append(out, l)
		}
	}
	return out
}

// daemonEvents returns the decoded daemon events of one kind.
func (f *fixture) daemonEvents(t *testing.T, event string) []mesh.Event {
	t.Helper()
	hist, err := mesh.History(f.ctx, f.c, mesh.DaemonSubject(testHost, event), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var out []mesh.Event
	for _, s := range hist {
		ev, err := decodeEvent(s.Data)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

// wakeLine is the fake t3's line for a wake of n messages sent by f.send.
func wakeLine(thread string, n int) string {
	return "send " + thread + " " + WakeText(n, fromRick(n))
}

func fromRick(n int) []mesh.Message {
	var out []mesh.Message
	for i := 0; i < n; i++ {
		out = append(out, mesh.Message{From: "rick", Kind: "message"})
	}
	return out
}
