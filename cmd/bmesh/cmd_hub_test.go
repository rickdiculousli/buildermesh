package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/config"
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
