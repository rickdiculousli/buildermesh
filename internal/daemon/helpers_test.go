package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

const testHost = "testhost"

// fixture is a provisioned server, a fake runner and Deps whose clock the
// test moves.
type fixture struct {
	ctx context.Context
	c   *mesh.Client
	d   Deps
	now time.Time // what d.Now returns
	rn  *fakeRunner
}

// fakeRunner is a runner whose threads are idle unless status names them;
// it records each send it accepts as "send <thread> <text>" and refuses
// every send with sendErr while that is set.
type fakeRunner struct {
	mu      sync.Mutex
	sendErr error
	sent    []string
	status  map[string]runner.Status
}

// setStatus makes thread read as s.
func (r *fakeRunner) setStatus(thread string, s runner.Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == nil {
		r.status = map[string]runner.Status{}
	}
	r.status[thread] = s
}

func (r *fakeRunner) Start(context.Context, runner.StartSpec) (runner.Started, error) {
	return runner.Started{}, errors.New("fakeRunner: no start")
}

func (r *fakeRunner) Status(_ context.Context, thread string) (runner.Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.status[thread]; ok {
		return s, nil
	}
	return runner.Status{Known: true}, nil
}

func (r *fakeRunner) Send(_ context.Context, thread, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sendErr != nil {
		return r.sendErr
	}
	r.sent = append(r.sent, "send "+thread+" "+text)
	return nil
}

func (r *fakeRunner) Interrupt(context.Context, string) error { return nil }

func (r *fakeRunner) failSends(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendErr = err
}

func setup(t *testing.T) *fixture {
	t.Helper()
	return setupOn(t, meshtest.Start(t), false)
}

// setupOn is setup on the server at url; a daemon client reconnects to
// a server that restarts.
func setupOn(t *testing.T, url string, daemon bool) *fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	c, err := mesh.Connect(ctx, url, "", "hub", "test", daemon)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{ctx: ctx, c: c, now: time.Now(), rn: &fakeRunner{}}
	f.d = Deps{
		Client: c, Cfg: cfg, Host: testHost,
		Runner: f.rn,
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
	meshtest.ForceState(t, f.c, path, mesh.State{State: state, By: "node"})
}

func (f *fixture) send(t *testing.T, to string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := mesh.Send(f.ctx, f.c, mesh.Message{From: "rick", To: to, Text: "hi"}); err != nil {
			t.Fatal(err)
		}
	}
}

// sends returns the sends the runner accepted.
func (f *fixture) sends(t *testing.T) []string {
	t.Helper()
	f.rn.mu.Lock()
	defer f.rn.mu.Unlock()
	return append([]string{}, f.rn.sent...)
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
