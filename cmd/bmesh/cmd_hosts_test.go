package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// k16Config is a hand-written config in the shape of a real box's: comments,
// sub-tables and keys init must not touch.
const k16Config = `# bmesh config for k16. Keys not set here keep bmesh's defaults.

[mesh]
nats = "NATS_URL"
token_file = "~/.config/command-center/nats.token"   # the hub's token

[runner]
kind = "t3"
# the V1 path's keys
t3_dispatch = "~/repos/command-center/tools/t3_dispatch.py"   # V1 only
max_leads = 6   # from memcap

[notify]
ntfy = "http://127.0.0.1:2586/bmesh"
push = ["ask", "blocked", "done"]

[console]
thread_url = "t3code://threads/env/{thread}"
[console.hosts]
ricks-macbook = "t3code://threads/mac/{thread}"   # leads on the Mac

[models]
lead = "claude-opus-5-5"
`

// diffLines is the lines of b not in a and of a not in b.
func diffLines(a, b string) (gone, added []string) {
	in := func(s string) map[string]int {
		m := map[string]int{}
		for _, l := range strings.Split(s, "\n") {
			m[l]++
		}
		return m
	}
	ma, mb := in(a), in(b)
	for l, n := range ma {
		if mb[l] < n {
			gone = append(gone, l)
		}
	}
	for l, n := range mb {
		if ma[l] < n {
			added = append(added, l)
		}
	}
	return gone, added
}

func TestWriteHostsConfigChangesOnlyHostsAndCheckouts(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	body := strings.Replace(k16Config, "NATS_URL", "nats://127.0.0.1:4222", 1)
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	prev, err := writeHostsConfig(cfg, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(prev); string(b) != body {
		t.Fatal("config.toml.prev isn't the old file")
	}
	after, _ := os.ReadFile(cfg)
	gone, added := diffLines(body, string(after))
	if len(gone) != 0 {
		t.Errorf("lines removed: %q", gone)
	}
	want := map[string]bool{"[hosts]": true, "enabled = true": true, "admin = false": true, "secure = true": true,
		`checkouts = ""   # routines off on this box (bmesh init --hosts)`: true, "": true}
	for _, l := range added {
		if !want[l] {
			t.Errorf("unexpected line added: %q", l)
		}
	}
	// checkouts sits in [runner], not after the [console.hosts] sub-table.
	if !regexp.MustCompile(`(?s)\[runner\]\ncheckouts = ""[^\[]*\[notify\]`).Match(after) {
		t.Errorf("checkouts isn't in [runner]:\n%s", after)
	}
	// Run again with routines on: the checkouts line goes and [hosts] is
	// rewritten in place.
	if _, err := writeHostsConfig(cfg, true, false, true); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(cfg)
	gone, added = diffLines(body, string(again))
	if len(gone) != 0 {
		t.Errorf("second run removed: %q", gone)
	}
	if strings.Count(string(again), "[hosts]") != 1 || strings.Contains(string(again), "checkouts") ||
		!strings.Contains(string(again), "admin = true\nsecure = false") {
		t.Errorf("second run:\n%s", again)
	}
}

// syncBuf is a buffer a goroutine writes while the test reads.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// box is one box's config dir and state dir in a hosts test.
type box struct {
	name, cfg, state string
}

func newBox(t *testing.T, name, url string) box {
	t.Helper()
	dir := t.TempDir()
	b := box{name: name, cfg: filepath.Join(dir, "config.toml"), state: filepath.Join(dir, "state")}
	body := "[mesh]\nnats = \"" + url + "\"\n[host]\nname = \"" + name + "\"\n[runner]\nkind = \"tmux\"\n"
	if err := os.WriteFile(b.cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return b
}

// run runs the CLI as this box: its config, its state dir.
func (b box) run(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", b.state)
	t.Setenv("BMESH_NODE", "")
	var out, errb bytes.Buffer
	code := run(append([]string{"--config", b.cfg}, args...), strings.NewReader(stdin), &out, &errb)
	return out.String(), errb.String(), code
}

func atScreenForTest(t *testing.T) {
	old, oldWait := atScreen, joinWait
	atScreen = func(io.Reader) bool { return true }
	joinWait = 2 * time.Second
	t.Cleanup(func() { atScreen, joinWait = old, oldWait })
}

var codeRE = regexp.MustCompile(`invite for \S+: ([0-9a-f]{8}-[A-Z2-7]{26})`)

// invite runs `hosts invite` on the admin in the background and returns
// the code it prints and a wait for its result.
func invite(t *testing.T, admin box, stdin string, args ...string) (code string, wait func() (string, int)) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", admin.state)
	out := &syncBuf{}
	var errb bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run(append([]string{"--config", admin.cfg, "hosts", "invite"}, args...), strings.NewReader(stdin), out, &errb)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m := codeRE.FindStringSubmatch(out.String()); m != nil {
			return m[1], func() (string, int) {
				select {
				case c := <-done:
					return out.String() + errb.String(), c
				case <-time.After(20 * time.Second):
					t.Fatal("invite never returned")
					return "", -1
				}
			}
		}
		select {
		case c := <-done:
			t.Fatalf("invite exited %d: %s %s", c, out.String(), errb.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("no code printed: %s", out.String())
	return "", nil
}

// answerPings answers ping jobs to b the way its daemon would, until the
// test ends.
func answerPings(t *testing.T, ctx context.Context, c *mesh.Client, b box) {
	t.Helper()
	key, err := hosts.LoadHostKey(hosts.HostKeyPath(filepath.Dir(b.cfg)))
	if err != nil {
		t.Fatal(err)
	}
	cons, err := hosts.HostInbox(ctx, c.JS, b.name)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for ctx.Err() == nil {
			batch, err := cons.Fetch(1, jetstream.FetchMaxWait(time.Second))
			if err != nil {
				return
			}
			for m := range batch.Messages() {
				var j hosts.Job
				if json.Unmarshal(m.Data(), &j) == nil {
					hosts.SendReply(ctx, c.JS, hosts.NewReply(key, &j, b.name, true, "pong from "+b.name, time.Now()))
				}
				m.Ack()
			}
		}
	}()
}

func TestHostsAdminInviteJoinPingSetRevoke(t *testing.T) {
	atScreenForTest(t)
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	mac, k16 := newBox(t, "mac", url), newBox(t, "k16", url)

	// The admin: secure, no routines, a new passphrase for admin.key.
	out, errs, code := mac.run(t, "y\nn\npw\npw\n", "init", "--hosts", "--admin")
	if code != 0 || !strings.Contains(out, "admin: mac ") || !strings.Contains(out, "roster v1") {
		t.Fatalf("admin init: %d %s %s", code, out, errs)
	}
	if b, _ := os.ReadFile(mac.cfg); !strings.Contains(string(b), "[hosts]\nenabled = true\nadmin = true\nsecure = true") ||
		!strings.Contains(string(b), `checkouts = ""`) {
		t.Fatalf("mac config:\n%s", b)
	}

	// A wrong code first: refused, and the invite stays good.
	codeStr, wait := invite(t, mac, "pw\n", "k16", "--accept", "ping,level")
	id, _, _ := strings.Cut(codeStr, "-")
	if out, _, code := k16.run(t, "\nn\ny\n"+id+"-AAAAAAAAAAAAAAAAAAAAAAAAAA\n", "init", "--hosts"); code == 0 {
		t.Fatalf("a wrong code joined: %s", out)
	}
	out, errs, code = k16.run(t, "\nn\ny\n"+codeStr+"\n", "init", "--hosts")
	if code != 0 || !strings.Contains(out, "joined: roster v2, admin mac ") {
		t.Fatalf("join: %d %s %s", code, out, errs)
	}
	if res, c := wait(); c != 0 || !strings.Contains(res, "joined: k16, roster v2") {
		t.Fatalf("invite: %d %s", c, res)
	}
	// The used code is gone.
	if out, _, code := k16.run(t, "\nn\ny\n"+codeStr+"\n", "init", "--hosts"); code == 0 {
		t.Fatalf("a used code joined again: %s", out)
	}
	// A taken name is refused unless the invite says --replace.
	if _, errs, code := mac.run(t, "pw\n", "hosts", "invite", "k16"); code != exitRefused || !strings.Contains(errs, "--replace") {
		t.Fatalf("invite a taken name: %d %s", code, errs)
	}

	out, errs, code = mac.run(t, "", "hosts", "--json")
	if code != 0 {
		t.Fatalf("hosts: %d %s", code, errs)
	}
	var listed struct {
		RosterVersion int64        `json:"roster_version"`
		Admin         string       `json:"admin"`
		Hosts         []hosts.Seen `json:"hosts"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.RosterVersion != 2 || listed.Admin != "mac" || len(listed.Hosts) != 2 {
		t.Fatalf("hosts: %+v", listed)
	}

	// Ping: k16 answers (a stand-in for its daemon); mac has no daemon, so
	// its ping waits.
	answerPings(t, ctx, c, k16)
	out, errs, code = mac.run(t, "", "hosts", "ping", "--wait", "3s")
	if code != 0 || !strings.Contains(out, "k16: pong from k16") || !regexp.MustCompile(`mac: asleep: queued until \d{4}-`).MatchString(out) {
		t.Fatalf("ping: %d %s %s", code, out, errs)
	}
	out, _, _ = mac.run(t, "", "hosts", "replies")
	if !strings.Contains(out, "k16 ping") || !strings.Contains(out, "pong from k16") {
		t.Fatalf("replies: %s", out)
	}

	// Only the admin changes the roster.
	if _, errs, code := k16.run(t, "pw\n", "hosts", "revoke", "mac"); code != exitRefused || !strings.Contains(errs, "only the admin box") {
		t.Fatalf("k16 revoke: %d %s", code, errs)
	}
	out, errs, code = mac.run(t, "pw\n", "hosts", "set", "k16", "--accept", "ping,prune")
	if code != 0 || !strings.Contains(out, "roster v3: k16 not secure, accept ping,prune") {
		t.Fatalf("set: %d %s %s", code, out, errs)
	}
	if _, errs, code := mac.run(t, "pw\n", "hosts", "set", "k16", "--secure"); code != exitRefused || !strings.Contains(errs, "no approve key") {
		t.Fatalf("set secure without an approve key: %d %s", code, errs)
	}
	if _, errs, code := mac.run(t, "wrong\n", "hosts", "revoke", "k16"); code == 0 || !strings.Contains(errs, "wrong passphrase") {
		t.Fatalf("revoke with a wrong passphrase: %d %s", code, errs)
	}
	out, errs, code = mac.run(t, "pw\n", "hosts", "revoke", "k16")
	if code != 0 || !strings.Contains(out, "roster v4: k16 revoked") {
		t.Fatalf("revoke: %d %s %s", code, out, errs)
	}
	// k16 takes the newer roster from the hub, and isn't in it.
	t.Setenv("XDG_STATE_HOME", k16.state)
	kv, _ := hosts.Bucket(ctx, c.JS, hosts.BucketRoster)
	k16State := filepath.Join(k16.state, "bmesh")
	l, _, err := hosts.LoadLocal(k16State)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hosts.Sync(ctx, kv, k16State, &l); err != nil || l.Roster.Version != 4 {
		t.Fatalf("k16 roster: v%d %v", l.Roster.Version, err)
	}

	// No invite replaces the admin's own row: it carries the admin key.
	if _, errs, code := mac.run(t, "pw\n", "hosts", "invite", "mac", "--replace"); code != exitRefused || !strings.Contains(errs, "is the admin box") {
		t.Fatalf("invite the admin's name: %d %s", code, errs)
	}
	// k16 rejoins under a fresh invite.
	codeStr, wait = invite(t, mac, "pw\n", "k16")
	if out, errs, code := k16.run(t, "\nn\ny\n"+codeStr+"\n", "init", "--hosts"); code != 0 || !strings.Contains(out, "this box pinned admin") ||
		!strings.Contains(out, "joined: roster v5") {
		t.Fatalf("rejoin: %d %s %s", code, out, errs)
	}
	if res, c := wait(); c != 0 {
		t.Fatalf("invite: %d %s", c, res)
	}

	// Run again, init keeps the answers as defaults and the roster as is.
	out, errs, code = mac.run(t, "\n\n", "init", "--hosts")
	if code != 0 || !strings.Contains(out, "roster v5") || strings.Contains(out, "Keychain") {
		t.Fatalf("admin re-run: %d %s %s", code, out, errs)
	}
}

func TestInitHostsAdminWarnsOfAnotherRoster(t *testing.T) {
	atScreenForTest(t)
	url := meshtest.Start(t)
	a, b := newBox(t, "one", url), newBox(t, "two", url)
	if out, errs, code := a.run(t, "y\nn\npw\npw\n", "init", "--hosts", "--admin"); code != 0 {
		t.Fatalf("first admin: %d %s %s", code, out, errs)
	}
	out, errs, code := b.run(t, "y\nn\nqq\nqq\n", "init", "--hosts", "--admin")
	if code != 0 || !regexp.MustCompile(`a roster signed by [0-9a-f]{16} exists; becoming admin replaces it on every box you invite again`).MatchString(out) {
		t.Fatalf("second admin: %d %s %s", code, out, errs)
	}
}

func TestInitHostsNeedsAConfig(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	var out, errb bytes.Buffer
	if code := run([]string{"--config", cfg, "init", "--hosts"}, strings.NewReader(""), &out, &errb); code != exitRefused ||
		!strings.Contains(errb.String(), "run `bmesh init` first") {
		t.Fatalf("%d %s", code, errb.String())
	}
	if code := run([]string{"--config", cfg, "init", "--admin"}, strings.NewReader(""), &out, &errb); code != exitUsage {
		t.Fatalf("--admin without --hosts: %d", code)
	}
}

func TestHostsOffIsRefused(t *testing.T) {
	setup(t)
	_, errs, code := runCLI(t, nil, "hosts")
	if code != exitRefused || !strings.Contains(errs, "hosts are off") {
		t.Fatalf("%d %s", code, errs)
	}
}

func TestHostsHereReadsOnlyTheFile(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	t.Setenv("XDG_STATE_HOME", state)
	cfg := filepath.Join(dir, "config.toml")
	// The hub named here doesn't exist: --here must never dial it.
	write := func(body string) {
		if err := os.WriteFile(cfg, []byte("[mesh]\nnats = \"nats://127.0.0.1:1\"\n"+body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	here := func() (string, string, int) {
		var out, errb bytes.Buffer
		code := run([]string{"--config", cfg, "hosts", "--here"}, strings.NewReader(""), &out, &errb)
		return out.String(), errb.String(), code
	}
	write("[hosts]\nenabled = true\n")
	if out, errs, code := here(); code != 0 || out != "" || errs != "" {
		t.Fatalf("no file: %d %q %q", code, out, errs)
	}
	line := "this box is k16; secure: no; on the mesh: ricks-macbook (asleep 2h, secure, admin)\n"
	if err := hosts.WriteHere(filepath.Join(state, "bmesh"), strings.TrimSuffix(line, "\n")); err != nil {
		t.Fatal(err)
	}
	if out, errs, code := here(); code != 0 || out != line || errs != "" {
		t.Fatalf("with the file: %d %q %q", code, out, errs)
	}
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(hosts.HerePath(filepath.Join(state, "bmesh")), old, old); err != nil {
		t.Fatal(err)
	}
	if out, _, code := here(); code != 0 || out != strings.TrimSuffix(line, "\n")+"; as of 3h ago, not updated since\n" {
		t.Fatalf("stale file: %d %q", code, out)
	}
	write("")
	if out, errs, code := here(); code != 0 || out != "" || errs != "" {
		t.Fatalf("[hosts] off: %d %q %q", code, out, errs)
	}
	// A box never set up (no config.toml, no setup flag) is quiet too, not refused.
	var out, errb bytes.Buffer
	if code := run([]string{"--config", filepath.Join(dir, "absent", "config.toml"), "hosts", "--here"}, strings.NewReader(""), &out, &errb); code != 0 || out.Len() != 0 || errb.Len() != 0 {
		t.Fatalf("not set up: %d %q %q", code, out.String(), errb.String())
	}
}

func TestHostsNotesPrintsVerifiedNotes(t *testing.T) {
	var out, errb bytes.Buffer
	a := &app{stdout: &out, stderr: &errb}
	seen := []hosts.Seen{
		{Host: hosts.Host{Name: "k16"}, State: hosts.Awake, Presence: hosts.Presence{Notes: "k16 notes"}},
		{Host: hosts.Host{Name: "pi"}, State: hosts.Absent},
		{Host: hosts.Host{Name: "mac"}, State: hosts.Asleep, Presence: hosts.Presence{Notes: "mac notes"}},
		{Host: hosts.Host{Name: "evil"}, State: hosts.Unverified},
	}
	if err := a.printNotes(seen); err != nil {
		t.Fatal(err)
	}
	if want := "## k16 (awake)\n\nk16 notes\n\n## mac (asleep)\n\nmac notes\n"; out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
	if errb.String() != "bmesh: evil: unverified, notes withheld\n" {
		t.Fatalf("stderr %q", errb.String())
	}
}
