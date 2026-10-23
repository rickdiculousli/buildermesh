package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// The multi-box answer hub writes [hub] and [mesh] nats to match and makes
// the token, owner-only; a second run keeps the token there.
func TestInitHubRoleWritesHubAndToken(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config.toml")
	// multi, hub, its address, the leaf port's default, the token file's default.
	out, errs, code := runInit(t, cfg, root, "multi\nhub\n100.64.1.2:4222\n\n\n", "init", "--runner", "claude", "--no-repo")
	if code != 0 {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	c, err := config.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tok := filepath.Join(root, "nats.token")
	if c.Hub.Role != "hub" || c.Hub.Listen != "100.64.1.2:4222" || c.Hub.LeafListen != "100.64.1.2:7422" ||
		c.Mesh.NATS != "nats://100.64.1.2:4222" || c.Mesh.TokenFile != tok {
		t.Fatalf("%+v %+v", c.Hub, c.Mesh)
	}
	st, err := os.Stat(tok)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("token mode %v", st.Mode())
	}
	first, _ := os.ReadFile(tok)
	if len(strings.TrimSpace(string(first))) != 48 || !strings.Contains(out, "wrote "+tok) ||
		!strings.Contains(out, "server: the mesh's hub on 100.64.1.2:4222") || !strings.Contains(out, "bmesh hub ") {
		t.Fatalf("token %q out %q", first, out)
	}
	if _, _, code := runInit(t, cfg, root, "multi\nhub\n100.64.1.2:4222\n\n\n", "init", "--force", "--runner", "claude", "--no-repo"); code != 0 {
		t.Fatal(code)
	}
	if again, _ := os.ReadFile(tok); string(again) != string(first) {
		t.Fatal("a second init replaced the token")
	}
	// A hub on every interface is refused, naming the key.
	_, errs, code = runInit(t, filepath.Join(root, "b", "config.toml"), root, "0.0.0.0:4222\n", "init", "--role", "hub", "--runner", "claude", "--no-repo")
	if code != exitRefused || !strings.Contains(errs, "[hub] listen") {
		t.Fatalf("%d %q", code, errs)
	}
}

// The answer leaf listens on loopback, dials the hub's leaf URL, and says
// to copy the token when it isn't there.
func TestInitLeafRole(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config.toml")
	out, errs, code := runInit(t, cfg, root, "nats://100.64.1.2:7422\n\n", "init", "--role", "leaf", "--runner", "claude", "--no-repo")
	if code != 0 {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	c, err := config.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hub.Role != "leaf" || c.Hub.Listen != "127.0.0.1:4222" || c.Hub.Remote != "nats://100.64.1.2:7422" ||
		c.Mesh.NATS != "nats://127.0.0.1:4222" || c.Mesh.TokenFile != filepath.Join(root, "nats.token") {
		t.Fatalf("%+v %+v", c.Hub, c.Mesh)
	}
	if !strings.Contains(out, "copy the hub's there") {
		t.Fatalf("out %q", out)
	}
	// A token file given with ~ is made (hub) and found (leaf) in the home
	// directory, and written to the config as given.
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	hubCfg := filepath.Join(root, "h", "config.toml")
	if _, errs, code := runInit(t, hubCfg, root, "100.64.1.2:4222\n\n~/tok\n", "init", "--role", "hub", "--runner", "claude", "--no-repo"); code != 0 {
		t.Fatalf("%d %q", code, errs)
	}
	if _, err := os.Stat(filepath.Join(home, "tok")); err != nil {
		t.Fatal(err)
	}
	if c, _ := config.Load(hubCfg); c.Mesh.TokenFile != "~/tok" {
		t.Fatalf("%+v", c.Mesh)
	}
	if _, err := os.Stat(filepath.Join(root, "~")); err == nil {
		t.Fatal("made a directory named ~")
	}
	out, _, code = runInit(t, filepath.Join(root, "l", "config.toml"), root, "nats://100.64.1.2:7422\n~/tok\n", "init", "--role", "leaf", "--runner", "claude", "--no-repo")
	if code != 0 || strings.Contains(out, "copy the hub's there") {
		t.Fatalf("%d %q", code, out)
	}
	// No remote answered: refused.
	if _, _, code := runInit(t, filepath.Join(root, "b", "config.toml"), root, "", "init", "--role", "leaf", "--no-repo"); code != exitUsage {
		t.Fatal(code)
	}
}

func TestInitServiceWritesHubUnit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd")
	}
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	cfg := filepath.Join(root, "config.toml")
	os.WriteFile(cfg, []byte("[hub]\nrole = \"leaf\"\nlisten = \"127.0.0.1:4222\"\nremote = \"nats://h:7422\"\n"), 0o600)
	out, _, code := runInit(t, cfg, root, "", "init", "--service", "--no-repo")
	if code != 0 {
		t.Fatalf("%d %q", code, out)
	}
	unit := filepath.Join(root, "xdg", "systemd", "user", "bmesh-hub.service")
	b, err := os.ReadFile(unit)
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	if !strings.Contains(string(b), "ExecStart="+exe+" hub\n") || !strings.Contains(string(b), "Restart=always\n") ||
		!strings.Contains(string(b), "RestartPreventExitStatus=78\n") || strings.Contains(string(b), "KillMode") || !strings.Contains(string(b), "BMESH_CONFIG="+cfg) {
		t.Fatalf("unit:\n%s", b)
	}
	if !strings.Contains(out, "systemctl --user enable --now bmesh-hub") || !strings.Contains(out, "wrote "+unit) ||
		!strings.Contains(out, "Keeps bmesh serve and bmesh hub running") {
		t.Fatalf("out %q", out)
	}
	// Without a role, only serve's unit.
	os.Remove(unit)
	os.WriteFile(cfg, []byte("[runner]\nkind = \"claude\"\n"), 0o600)
	if _, _, code := runInit(t, cfg, root, "", "init", "--service", "--no-repo"); code != 0 {
		t.Fatal(code)
	}
	if _, err := os.Stat(unit); err == nil {
		t.Fatal("a box without [hub] role got the hub's unit")
	}
}

// bmesh hub refuses to start, naming the key, on each thing that won't do.
func TestHubRefusesNamingTheKey(t *testing.T) {
	root := t.TempDir()
	tok := filepath.Join(root, "nats.token")
	os.WriteFile(tok, []byte("tok\n"), 0o600)
	for _, tc := range []struct{ conf, want string }{
		{"", "[hub] role is unset"},
		{"[hub]\nrole = \"hub\"\nlisten = \"0.0.0.0:4222\"\n", "[hub] listen \"0.0.0.0:4222\" listens on every interface"},
		{"[hub]\nrole = \"hub\"\nlisten = \"[::]:4222\"\n", "[hub] listen \"[::]:4222\" listens on every interface"},
		{"[hub]\nrole = \"hub\"\nlisten = \":4222\"\n", "[hub] listen \":4222\" listens on every interface"},
		{"[hub]\nrole = \"hub\"\n", "[hub] listen is unset"},
		{"[hub]\nrole = \"hub\"\nlisten = \"127.0.0.1:0\"\nleaf_listen = \"0.0.0.0:7422\"\n", "[hub] leaf_listen"},
		{"[hub]\nrole = \"leaf\"\nlisten = \"127.0.0.1:0\"\n", "[hub] remote is unset"},
		{"[hub]\nrole = \"hub\"\nlisten = \"127.0.0.1:0\"\n", "[mesh] token_file is unset"},
		{"[mesh]\ntoken_file = \"" + filepath.Join(root, "none") + "\"\n[hub]\nrole = \"hub\"\nlisten = \"127.0.0.1:0\"\n", "[mesh] token_file " + filepath.Join(root, "none")},
	} {
		cfg := filepath.Join(root, "config.toml")
		os.WriteFile(cfg, []byte(tc.conf), 0o600)
		_, errs, code := runInit(t, cfg, root, "", "hub")
		if code != exitRefused || !strings.Contains(errs, tc.want) {
			t.Errorf("%q: %d %q, want %q", tc.conf, code, errs, tc.want)
		}
	}
	// A role that isn't one is a config error.
	cfg := filepath.Join(root, "config.toml")
	os.WriteFile(cfg, []byte("[hub]\nrole = \"server\"\n"), 0o600)
	if _, errs, code := runInit(t, cfg, root, "", "hub"); code == 0 || !strings.Contains(errs, "[hub] role \"server\"") {
		t.Fatalf("%d %q", code, errs)
	}
	// serve with a role and no [mesh] nats would embed a second server.
	os.WriteFile(cfg, []byte("[hub]\nrole = \"leaf\"\n"), 0o600)
	if _, errs, code := runInit(t, cfg, root, "", "serve"); code != exitRefused || !strings.Contains(errs, "set [mesh] nats") {
		t.Fatalf("%d %q", code, errs)
	}
}

// [hub] maps onto the server: the host's name, the mesh's domain, the
// default store under the state dir, the sizes as nats-server reads them.
func TestHubOptionsFromConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	tok := filepath.Join(root, "nats.token")
	os.WriteFile(tok, []byte("tok\n"), 0o600)
	cfg := filepath.Join(root, "config.toml")
	os.WriteFile(cfg, []byte("[host]\nname = \"Box\"\n[mesh]\ndomain = \"hub\"\ntoken_file = \""+tok+"\"\n"+
		"[hub]\nrole = \"hub\"\nlisten = \"100.64.1.2:4222\"\nleaf_listen = \"100.64.1.2:7422\"\nmax_memory_store = \"1G\"\n"), 0o600)
	c, err := config.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	o, err := hubOptions(c)
	if err != nil {
		t.Fatal(err)
	}
	if o.Name != "box" || o.Domain != "hub" || o.Token != "tok" || o.Store != filepath.Join(root, "state", "bmesh", "hub", "jetstream") ||
		o.MaxStore != 8<<30 || o.MaxMemory != 1e9 || o.LeafListen != "100.64.1.2:7422" || o.Remote != "" {
		t.Fatalf("%+v", o)
	}
	// A leaf has no store and dials remote.
	os.WriteFile(cfg, []byte("[mesh]\ntoken_file = \""+tok+"\"\n[hub]\nrole = \"leaf\"\nlisten = \"127.0.0.1:4222\"\nremote = \"nats://100.64.1.2:7422\"\n"), 0o600)
	if c, err = config.Load(cfg); err != nil {
		t.Fatal(err)
	}
	if o, err = hubOptions(c); err != nil || o.Remote != "nats://100.64.1.2:7422" || o.Store != "" || o.LeafListen != "" {
		t.Fatalf("%v %+v", err, o)
	}
	// A size that isn't one is a config error naming the key.
	os.WriteFile(cfg, []byte("[hub]\nmax_file_store = \"8 parsecs\"\n"), 0o600)
	if _, err := config.Load(cfg); err == nil || !strings.Contains(err.Error(), "[hub] max_file_store") {
		t.Fatal(err)
	}
}

// serve and hub refuse a config this binary doesn't take with exit 78,
// naming the key, which their units don't restart on.
func TestServeAndHubExitConfigOnABadConfig(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config.toml")
	os.WriteFile(cfg, []byte("[runner]\nt3_server = \"\"\n"), 0o600)
	for _, cmd := range [][]string{{"serve"}, {"serve", "--restart"}, {"hub"}} {
		_, errs, code := runInit(t, cfg, root, "", cmd...)
		if code != exitConfig || !strings.Contains(errs, "bmesh: config check failed: ") || !strings.Contains(errs, "[runner] t3_server is empty") {
			t.Errorf("%v: %d %q", cmd, code, errs)
		}
	}
}

// fakeHubRecords writes, in state, hub.json for a live process on an older
// binary and serve.json for this one on a newer.
func fakeHubRecords(t *testing.T, state string) (hubPid int) {
	t.Helper()
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep command")
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	writeProcRecord(t, state, "hub", selfupdate.ProcRecord{Pid: cmd.Process.Pid, Revision: "aaaaaaaaaaaa", Release: "v0.1.0", Time: "2026-10-01T00:00:00Z"})
	writeProcRecord(t, state, "serve", selfupdate.ProcRecord{Pid: os.Getpid(), Revision: "bbbbbbbbbbbb", Release: "v0.2.0", Time: "2026-10-10T00:00:00Z"})
	return cmd.Process.Pid
}

func writeProcRecord(t *testing.T, state, name string, r selfupdate.ProcRecord) {
	t.Helper()
	b, _ := json.Marshal(r)
	os.MkdirAll(state, 0o755)
	if err := os.WriteFile(selfupdate.ProcPath(state, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

const hubLine = "hub runs v0.1.0, serve v0.2.0: restart bmesh-hub\n"

// hosts --here names a hub left on an older binary than serve, [hosts] on
// or off, from the two records alone; a hub that isn't running gives none.
func TestHostsHereNamesAnOlderHub(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	state := filepath.Join(dir, "bmesh")
	cfg := filepath.Join(dir, "config.toml")
	here := func(body string) (string, string, int) {
		os.WriteFile(cfg, []byte("[mesh]\nnats = \"nats://127.0.0.1:1\"\n"+body), 0o600)
		var out, errb bytes.Buffer
		code := run([]string{"--config", cfg, "hosts", "--here"}, strings.NewReader(""), &out, &errb)
		return out.String(), errb.String(), code
	}
	fakeHubRecords(t, state)
	if out, errs, code := here(""); code != 0 || out != hubLine || errs != "" {
		t.Fatalf("[hosts] off: %d %q %q", code, out, errs)
	}
	os.WriteFile(filepath.Join(state, "this-box.txt"), []byte("this box is k16\n"), 0o644)
	if out, _, code := here("[hosts]\nenabled = true\n"); code != 0 || out != "this box is k16\n"+hubLine {
		t.Fatalf("[hosts] on: %d %q", code, out)
	}
	// The hub stopped: its record's pid is gone.
	dead := exec.Command("sleep", "0")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	writeProcRecord(t, state, "hub", selfupdate.ProcRecord{Pid: dead.Process.Pid, Revision: "aaaaaaaaaaaa", Release: "v0.1.0", Time: "2026-10-01T00:00:00Z"})
	if out, _, code := here(""); code != 0 || out != "" {
		t.Fatalf("dead hub: %d %q", code, out)
	}
}

func TestMeshStatusNamesAnOlderHub(t *testing.T) {
	setup(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeHubRecords(t, config.StateDir())
	out, _, code := runCLI(t, nil, "mesh-status")
	if code != 0 || !strings.HasSuffix(out, "\n"+hubLine) {
		t.Fatalf("code %d out %q", code, out)
	}
}

// hubBox is a box running bmesh hub in this process on loopback ports, with
// the re-exec stubbed: each call reports whether the hub's client port was
// already shut, and returns as an exec that failed to replace us would not.
type hubBox struct {
	t       *testing.T
	addr    string
	exe     string
	reexecs chan bool
}

func newHubBox(t *testing.T) *hubBox {
	if runtime.GOOS == "windows" {
		t.Skip("the hub restarts itself only where reexec replaces the process")
	}
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	tok := filepath.Join(root, "nats.token")
	os.WriteFile(tok, []byte("tok\n"), 0o600)
	cfg := filepath.Join(root, "config.toml")
	os.WriteFile(cfg, []byte("[host]\nname = \"box\"\n[mesh]\ndomain = \"hub\"\ntoken_file = \""+tok+"\"\n"+
		"[hub]\nrole = \"hub\"\nlisten = \""+addr+"\"\n"), 0o600)
	t.Setenv("BMESH_CONFIG", cfg)
	t.Setenv("BMESH_NODE", "")
	b := &hubBox{t: t, addr: addr, exe: filepath.Join(root, "bin", "bmesh"), reexecs: make(chan bool, 4)}
	b.install("old")
	savedExe, savedEvery, savedReexec, savedSignals := executable, hubCheckEvery, hubReexec, hubSignals
	t.Cleanup(func() {
		executable, hubCheckEvery, hubReexec, hubSignals = savedExe, savedEvery, savedReexec, savedSignals
	})
	executable = func() (string, error) { return b.exe, nil }
	hubCheckEvery = 20 * time.Millisecond
	hubReexec = func(exe string) error {
		if exe != b.exe {
			t.Errorf("re-exec %q, want %q", exe, b.exe)
		}
		c, err := net.Dial("tcp", addr)
		if err == nil {
			c.Close()
		}
		b.reexecs <- err != nil
		return nil
	}
	return b
}

// install puts a new file at the box's binary path, as an update's swap does.
func (b *hubBox) install(body string) {
	os.MkdirAll(filepath.Dir(b.exe), 0o755)
	if err := os.WriteFile(b.exe+".new", []byte(body), 0o755); err != nil {
		b.t.Fatal(err)
	}
	if err := os.Rename(b.exe+".new", b.exe); err != nil {
		b.t.Fatal(err)
	}
}

// start runs bmesh hub until stop is called or it returns; it waits until
// the hub has written hub.json.
func (b *hubBox) start() (stderr *meshtest.SyncBuffer, exit <-chan int, stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	hubSignals = func() (context.Context, context.CancelFunc) { return context.WithCancel(ctx) }
	stderr = &meshtest.SyncBuffer{}
	codes, done := make(chan int, 1), make(chan struct{})
	go func() {
		defer close(done)
		codes <- run([]string{"hub"}, strings.NewReader(""), io.Discard, stderr)
	}()
	b.t.Cleanup(func() { cancel(); <-done })
	meshtest.Until(b.t, "hub.json", func() bool {
		_, ok := selfupdate.ReadProc(config.StateDir(), "hub")
		return ok || len(codes) > 0
	})
	return stderr, codes, cancel
}

func (b *hubBox) connect() (*mesh.Client, context.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	b.t.Cleanup(cancel)
	c, err := mesh.Connect(ctx, "nats://"+b.addr, "tok", "hub", "test", false)
	if err != nil {
		b.t.Fatal(err)
	}
	b.t.Cleanup(c.Close)
	return c, ctx
}

const (
	hubOldRev = "1a2b3c4d5e6f7a8b9c0d1a2b3c4d5e6f7a8b9c0d" // withRevision's time, 2026-10-15
	hubNewRev = "2b3c4d5e6f7a8b9c0d1a2b3c4d5e6f7a8b9c0d1a"
)

// Once serve runs a newer release and the binary at the hub's path is
// another file, the hub logs it, shuts down and re-execs that path within
// a check; a hub started again on the same store reads back a key written
// before.
func TestHubRestartsOntoServesRelease(t *testing.T) {
	b := newHubBox(t)
	withRevision(t, hubOldRev)
	stderr, exit, _ := b.start()
	c, ctx := b.connect()
	kv, err := c.JS.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "before"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kv.PutString(ctx, "k", "kept"); err != nil {
		t.Fatal(err)
	}
	c.Close()
	b.install("new")
	writeProcRecord(t, config.StateDir(), "serve", selfupdate.ProcRecord{Pid: os.Getpid(), Revision: hubNewRev, Release: "v0.3.3", Time: "2026-10-20T00:00:00Z"})
	select {
	case down := <-b.reexecs:
		if !down {
			t.Fatal("re-exec'd with the hub still serving")
		}
	case <-time.After(meshtest.Within(t)):
		t.Fatal("no re-exec")
	}
	if code := <-exit; code != 0 || !strings.Contains(stderr.String(), "bmesh hub: serve runs v0.3.3, restarting as "+b.exe+"\n") {
		t.Fatalf("%d %q", code, stderr.String())
	}
	// The new binary: serve's own, so it stays.
	withRevision(t, hubNewRev)
	b.start()
	if hub, ok := selfupdate.ReadProc(config.StateDir(), "hub"); !ok || hub.Revision != hubNewRev {
		t.Fatalf("hub.json %+v %v", hub, ok)
	}
	c, ctx = b.connect()
	kv, err = c.JS.KeyValue(ctx, "before")
	if err != nil {
		t.Fatal(err)
	}
	if e, err := kv.Get(ctx, "k"); err != nil || string(e.Value()) != "kept" {
		t.Fatalf("%v %v", e, err)
	}
	time.Sleep(10 * hubCheckEvery)
	if len(b.reexecs) != 0 {
		t.Fatal("a hub on serve's binary re-exec'd")
	}
}

// Only a release moves the hub, and only onto another file: serve on a
// revision with no release, on a commit past a release (track = "latest"),
// or on a release whose binary isn't at the hub's path, gets no re-exec.
func TestHubStaysWithoutAReleaseToGoTo(t *testing.T) {
	for _, tc := range []struct {
		name    string
		release string
		install bool
	}{
		{"no release", "", true},
		{"past a release", "v0.3.2+3", true},
		{"same file", "v0.3.3", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newHubBox(t)
			withRevision(t, hubOldRev)
			stderr, exit, stop := b.start()
			if tc.install {
				b.install("new")
			}
			writeProcRecord(t, config.StateDir(), "serve", selfupdate.ProcRecord{Pid: os.Getpid(), Revision: hubNewRev, Release: tc.release, Time: "2026-10-20T00:00:00Z"})
			time.Sleep(10 * hubCheckEvery)
			stop()
			if code := <-exit; code != 0 || len(b.reexecs) != 0 || !strings.HasSuffix(stderr.String(), "bmesh hub: stopping\n") {
				t.Fatalf("%d re-execs %d %q", code, len(b.reexecs), stderr.String())
			}
		})
	}
}

// update's hub line compares the hub with the serve running beside it, not
// with the binary update just swapped in: none while serve still runs the
// hub's binary, since the hub follows serve once the daemon restarts.
func TestUpdateHubLineReadsTheRunningServe(t *testing.T) {
	_, shas := updateBox(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	state := config.StateDir()
	fakeHubRecords(t, state)
	hub, _ := selfupdate.ReadProc(state, "hub")
	writeProcRecord(t, state, "serve", selfupdate.ProcRecord{Pid: os.Getpid(), Revision: hub.Revision, Release: hub.Release, Time: hub.Time})
	withRevision(t, shas[0])
	if out, errOut, code := runCLI(t, nil, "update"); code != 0 || out != shas[0][:8]+" -> "+shas[1][:8]+"\n" {
		t.Fatalf("serve on the hub's binary: code %d out %q err %q", code, out, errOut)
	}
	fakeHubRecords(t, state) // serve now runs v0.2.0, the hub v0.1.0
	withRevision(t, shas[1])
	if out, errOut, code := runCLI(t, nil, "update"); code != 0 || out != "up to date\n"+hubLine {
		t.Fatalf("hub behind serve: code %d out %q err %q", code, out, errOut)
	}
}
