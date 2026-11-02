package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

// publishUse puts calls of agent on the bm-usage stream, one message.
func (f *fixture) publishUse(t *testing.T, agent, role string, calls ...usage.Call) {
	t.Helper()
	data, err := json.Marshal(usage.Message{Agent: agent, Role: role, Host: testHost, Repo: "app", Session: "s1", File: agent + ".jsonl", Calls: calls})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.JS.Publish(f.ctx, "bmusage.node."+agent, data); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) runawayEvents(t *testing.T) []string {
	t.Helper()
	hist, err := mesh.History(f.ctx, f.c, "bm.daemon.*.runaway", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range hist {
		var ev mesh.Event
		_ = json.Unmarshal(m.Data, &ev)
		out = append(out, m.Subject+" to "+ev.To+": "+ev.Text)
	}
	return out
}

// A node over the burn and context rules is announced once per rule and
// level, across a restart (a fresh flagger reading flags.json); a node
// done is forgotten, and another box's node is never this box's to judge.
func TestRunawayAnnouncedOncePerNodeRuleAndLevel(t *testing.T) {
	f := setup(t)
	f.d.StateDir = t.TempDir()
	at := func(ago time.Duration) string { return f.now.Add(-ago).UTC().Format(time.RFC3339Nano) }
	for _, n := range []mesh.Node{
		{Path: "app-001", Role: "lead", Repo: "app", Host: testHost, StartedAt: f.now.Add(-2 * time.Hour).UTC().Format(time.RFC3339)},
		{Path: "app-002", Role: "lead", Repo: "app", Host: "otherbox", StartedAt: f.now.Add(-2 * time.Hour).UTC().Format(time.RFC3339)},
	} {
		if err := mesh.PutNode(f.ctx, f.c, n); err != nil {
			t.Fatal(err)
		}
		f.setState(t, n.Path, "working")
	}
	for _, agent := range []string{"app-001", "app-002"} {
		f.publishUse(t, agent, "lead",
			usage.Call{At: at(90 * time.Minute), ID: "m0", CR: 80_000_000, Ctx: 100_000},
			usage.Call{At: at(30 * time.Minute), ID: "m1", CR: 60_000_000, Ctx: 300_000},
			usage.Call{At: at(10 * time.Minute), ID: "m2", CR: 50_000_000, Ctx: 450_000})
	}
	fl := &flagger{}
	if err := fl.check(f.ctx, f.d); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"bm.daemon.testhost.runaway to app-001: runaway app-001: context 450k, past compact_soon (400k)",
		"bm.daemon.testhost.runaway to app-001: runaway app-001: read 110M in the last hour",
	}
	if got := f.runawayEvents(t); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("events\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	b, err := os.ReadFile(filepath.Join(f.d.StateDir, "usage", "flags.json"))
	if err != nil || string(b) != `{"app-001":["context runaway","burn runaway"]}` {
		t.Fatalf("flags.json %s, %v", b, err)
	}
	// A restart: a fresh flagger, an hour later's baselines, the same flags.
	if err := (&flagger{}).check(f.ctx, f.d); err != nil {
		t.Fatal(err)
	}
	if got := f.runawayEvents(t); len(got) != 2 {
		t.Fatalf("after a restart, events %q", got)
	}
	// Done: its flags are forgotten.
	f.setState(t, "app-001", "done")
	if err := fl.check(f.ctx, f.d); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.d.StateDir, "usage", "flags.json")); string(b) != `{}` {
		t.Fatalf("flags.json after done: %s", b)
	}
}
