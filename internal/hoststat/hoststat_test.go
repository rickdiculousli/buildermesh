package hoststat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

const procStat = `cpu  1000 0 500 8000 500 0 0 0 0 0
cpu0 500 0 250 4000 250 0 0 0 0 0
cpu1 500 0 250 4000 250 0 0 0 0 0
intr 12345
`

const meminfo = `MemTotal:       32000000 kB
MemFree:         1000000 kB
MemAvailable:   20000000 kB
SwapTotal:       8000000 kB
SwapFree:        7000000 kB
`

// pidStat is a /proc/<pid>/stat line with utime..cstime and rss given.
func pidStat(pid, ppid int, comm string, ticks [4]int, start, rss int) string {
	return strconv.Itoa(pid) + " (" + comm + ") S " + strconv.Itoa(ppid) + " 1 1 0 -1 4194560 100 0 0 0 " +
		strconv.Itoa(ticks[0]) + " " + strconv.Itoa(ticks[1]) + " " + strconv.Itoa(ticks[2]) + " " + strconv.Itoa(ticks[3]) +
		" 20 0 1 0 " + strconv.Itoa(start) + " 1000000 " + strconv.Itoa(rss) + " 18446744073709551615 0 0 0 0 0\n"
}

func TestParseProcStat(t *testing.T) {
	busy, total, cores, err := parseProcStat([]byte(procStat))
	if err != nil || busy != 1500 || total != 10000 || cores != 2 {
		t.Fatalf("busy %v total %v cores %d err %v", busy, total, cores, err)
	}
	if _, _, _, err := parseProcStat([]byte("intr 1\n")); err == nil {
		t.Fatal("no cpu line: want an error")
	}
}

func TestParseMeminfoLoadavgPSI(t *testing.T) {
	used, total, swap := parseMeminfo([]byte(meminfo))
	if used != 12000000*1024 || total != 32000000*1024 || swap != 1000000*1024 {
		t.Fatalf("used %d total %d swap %d", used, total, swap)
	}
	if l := parseLoadavg([]byte("25.50 20.10 18.00 3/1200 4567\n")); l != 25.5 {
		t.Fatalf("load %v", l)
	}
	a10, a60, ok := parsePSI([]byte("some avg10=14.02 avg60=10.99 avg300=26.13 total=2814578313\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"))
	if !ok || a10 != 14.02 || a60 != 10.99 {
		t.Fatalf("psi %v %v %v", a10, a60, ok)
	}
	if _, _, ok := parsePSI([]byte("full avg10=1 avg60=2\n")); ok {
		t.Fatal("no some line: want not ok")
	}
}

func TestParsePidStatCommWithParens(t *testing.T) {
	p, err := parsePidStat([]byte(pidStat(42, 7, "a) (b", [4]int{100, 50, 30, 20}, 999, 10)), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if p.comm != "a) (b" || p.ppid != 7 || p.start != 999 || p.t != 2 || p.rss != 40960 {
		t.Fatalf("%+v", p)
	}
	if _, err := parsePidStat([]byte("42 (x) S 1 2"), 4096); err == nil {
		t.Fatal("short stat: want an error")
	}
}

// fakeProc writes a /proc under dir: the box files, each pid's stat and
// cwd (a symlink), and pressure when psi.
func fakeProc(t *testing.T, dir string, psi bool, pids map[int]string, cwds map[int]string) {
	t.Helper()
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("stat", procStat)
	write("meminfo", meminfo)
	write("loadavg", "3.00 2.00 1.00 1/100 5\n")
	if psi {
		write("pressure/cpu", "some avg10=4.00 avg60=3.00 avg300=1.00 total=1\nfull avg10=0 avg60=0 avg300=0 total=0\n")
		write("pressure/memory", "some avg10=0.00 avg60=0.50 avg300=0.00 total=1\n")
		write("pressure/io", "some avg10=1.00 avg60=2.00 avg300=0.00 total=1\n")
	}
	for pid, stat := range pids {
		write(strconv.Itoa(pid)+"/stat", stat)
		if cwd, ok := cwds[pid]; ok {
			if err := os.Symlink(cwd, filepath.Join(dir, strconv.Itoa(pid), "cwd")); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestReadLinuxMissingPressureIsNull(t *testing.T) {
	dir := t.TempDir()
	fakeProc(t, dir, false, map[int]string{1: pidStat(1, 0, "init", [4]int{}, 1, 1)}, nil)
	r, err := readLinux(dir, 4096, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r.p10 != nil || r.p60 != nil || r.load1 != 3 || r.cores != 2 || len(r.procs) != 1 {
		t.Fatalf("%+v", r)
	}
	dir = t.TempDir()
	fakeProc(t, dir, true, nil, nil)
	if r, err = readLinux(dir, 4096, time.Now()); err != nil || r.p60 == nil || *r.p60.CPU != 3 || *r.p10.IO != 1 || *r.p60.Mem != 0.5 {
		t.Fatalf("%+v %v", r, err)
	}
}

const psOut = `  PID  PPID      TIME    RSS COMM
    1     0   1:02.50  12000 /sbin/launchd
  300     1   0:10.00 204800 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome Helper (Renderer)
  400     1 1-02:03:04.00   1024 node
`

const vmStat = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               10000.
Pages active:                            200000.
Pages wired down:                         50000.
Pages purgeable:                          10000.
"Translation faults":                  99999999.
Anonymous pages:                         300000.
Pages occupied by compressor:             20000.
`

func TestParseDarwin(t *testing.T) {
	ps := parsePS([]byte(psOut))
	if len(ps) != 3 || ps[1].comm != "launchd" || ps[1].t != 62.5 || ps[1].rss != 12000*1024 {
		t.Fatalf("launchd %+v", ps[1])
	}
	if ps[300].comm != "Google Chrome Helper (Renderer)" || ps[300].ppid != 1 || ps[300].t != 10 {
		t.Fatalf("chrome %+v", ps[300])
	}
	if ps[400].t != 86400+2*3600+3*60+4 {
		t.Fatalf("node %+v", ps[400])
	}
	used, err := parseVMStat([]byte(vmStat))
	if err != nil || used != (50000+300000-10000+20000)*16384 {
		t.Fatalf("vm_stat %d %v", used, err)
	}
	sc := parseSysctl([]byte("hw.ncpu: 10\nhw.memsize: 17179869184\nvm.swapusage: total = 2048.00M  used = 1024.50M  free = 1023.50M  (encrypted)\nvm.loadavg: { 2.31 2.10 2.05 }\nkern.memorystatus_vm_pressure_level: 2\n"))
	if sc["hw.ncpu"] != "10" || parseSwapUsage(sc["vm.swapusage"]) != int64(1024.5*(1<<20)) {
		t.Fatalf("sysctl %v", sc)
	}
	if d := parseLsof([]byte("p1\nfcwd\nn/\np400\nfcwd\nn/Users/me/wt/app\np999\nfcwd\n")); d[1] != "/" || d[400] != "/Users/me/wt/app" || len(d) != 2 {
		t.Fatalf("lsof %v", d)
	}
}

// fakeRun answers the macOS commands from fixtures; lsof exits 1 for a
// pid that went, as it does.
func fakeRun(ps string) func(context.Context, string, ...string) ([]byte, error) {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch {
		case name == "ps" && args[0] == "-axS":
			return []byte(ps), nil
		case name == "ps":
			return []byte("  400 node /usr/local/lib/node_modules/@anthropic-ai/claude-code/cli.js --resume\n"), nil
		case name == "vm_stat":
			return []byte(vmStat), nil
		case name == "sysctl":
			return []byte("hw.ncpu: 10\nhw.memsize: 17179869184\nvm.swapusage: total = 0.00M  used = 0.00M  free = 0.00M\nvm.loadavg: { 5.00 2.10 2.05 }\n"), errors.New("exit status 1")
		case name == "lsof":
			return []byte("p400\nfcwd\nn/Users/me/wt/app\n"), errors.New("exit status 1")
		}
		return nil, errors.New("unknown " + name)
	}
}

func TestDarwinSampleSumsProcesses(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	s := New(Env{Host: "mac", GOOS: "darwin", Run: fakeRun(psOut), Now: clock})
	if m, err := s.Minute(context.Background()); m != nil || err != nil {
		t.Fatalf("first read: %v %v", m, err)
	}
	now = now.Add(time.Minute)
	more := strings.Replace(psOut, "1:02.50", "1:32.50", 1) // launchd: 30 s more
	s.env.Run = fakeRun(more)
	m, err := s.Minute(context.Background())
	if err != nil || m == nil {
		t.Fatal(m, err)
	}
	if m.CPU != 0.5 || m.Cores != 10 || m.Load1 != 5 || m.MacPressure != nil || m.Pressure != nil || m.OS != "darwin" {
		t.Fatalf("%+v", m)
	}
	if m.MemTotal != 17179869184 {
		t.Fatalf("mem total %d", m.MemTotal)
	}
	// node 400 runs Claude Code's CLI, with no session file and no repo: its
	// ancestor's agent, other.
	if len(m.Agents) != 1 || m.Agents[Other].CPU != 0.5 {
		t.Fatalf("agents %v", m.Agents)
	}
}

func TestProcCPUSubtractsReapedChildren(t *testing.T) {
	at := time.Unix(1000, 0)
	prev := &raw{at: at, procs: map[int]*rproc{
		10: {ppid: 1, comm: "go", start: 5, t: 10},
		11: {ppid: 10, comm: "test", start: 6, t: 4}, // exits and is reaped into 10
		12: {ppid: 1, comm: "old", start: 7, t: 100}, // its pid is reused
		13: {ppid: 2, comm: "kworker/u64:4-flush", start: 3, t: 50},
	}}
	cur := &raw{at: at.Add(10 * time.Second), procs: map[int]*rproc{
		10: {ppid: 1, comm: "go", start: 5, t: 10 + 1 + 4 + 3}, // 1 s its own, 4+3 s from 11's whole life
		12: {ppid: 1, comm: "new", start: 70, t: 2},
		13: {ppid: 2, comm: "kworker/u64:4-events", start: 3, t: 51}, // a kernel worker renames itself
	}}
	c := procCPU(prev, cur, 10)
	if c[10] != 0.4 || c[12] != 0.2 || c[13] != 0.1 {
		t.Fatalf("%v", c)
	}
}

func TestAttribution(t *testing.T) {
	root := t.TempDir()
	wt := filepath.Join(root, "wt")
	mk := func(p string) string {
		t.Helper()
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	app, app2 := mk(filepath.Join(wt, "bluke-002.app")), mk(filepath.Join(wt, "bluke-002.app2"))
	checkouts := mk(filepath.Join(root, "checkouts"))
	routineDir := mk(filepath.Join(checkouts, "app", "routine.verify.opus-1", "sub"))
	repo := mk(filepath.Join(root, "repos", "app"))
	meshtest.Git(t, repo, "init", "-q")
	claude := mk(filepath.Join(root, "claude", "sessions"))
	if err := os.WriteFile(filepath.Join(claude, "50.json"), []byte(`{"sessionId":"c158105c-6b18-4363","procStart":"500"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claude, "51.json"), []byte(`{"sessionId":"stale","procStart":"1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	nodes := []mesh.Node{
		{Path: "bluke-002", Host: "k16", Worktree: app, StartedAt: "2026-11-24T09:00:00Z"},
		{Path: "bluke-009", Host: "k16", Worktree: app2, StartedAt: "2026-11-24T08:00:00Z"},
		{Path: "bluke-003", Host: "k16", Worktree: app, StartedAt: "2026-11-24T10:00:00Z"}, // the newest in the same worktree
	}
	s := New(Env{Host: "k16", GOOS: "linux", Claude: filepath.Dir(claude), Checkouts: checkouts, Repos: map[string]string{"app": repo},
		Nodes: func(context.Context) ([]mesh.Node, error) { return nodes, nil }})
	s.nodes = nodes
	procs := map[int]*rproc{
		1:  {comm: "systemd"},
		10: {ppid: 1, comm: "bash", cwd: filepath.Join(app, "src")},
		11: {ppid: 10, comm: "java", cwd: "/"},              // an ancestor's agent
		20: {ppid: 1, comm: "make", cwd: filepath.Join(app2)}, // the sibling with the longer name
		30: {ppid: 1, comm: "go", cwd: routineDir},
		50: {ppid: 1, comm: "claude", cwd: "/home/me", start: 500},
		51: {ppid: 1, comm: "claude", cwd: repo, start: 600}, // its session file is another process's
		52: {ppid: 1, comm: "node", cwd: "/home/me", claude: false},
		60: {ppid: 1, comm: "sshd", cwd: "/"},
	}
	for _, p := range procs {
		p.claude = p.claude || IsClaude(p.comm, nil)
	}
	got := attribute(procs, s.match)
	want := map[int]string{1: Other, 10: "bluke-003", 11: "bluke-003", 20: "bluke-009", 30: "routine.verify.k16-opus-1",
		50: "session.k16.c158105c-6b18-4363", 51: "repo:app", 52: Other, 60: Other}
	for pid, w := range want {
		if got[pid] != w {
			t.Errorf("pid %d: %q, want %q", pid, got[pid], w)
		}
	}
}

func TestLiveNodesSkipsDoneAndOtherHosts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := mesh.Connect(ctx, meshtest.Start(t), "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	for _, n := range []mesh.Node{{Path: "app-001", Host: "k16", Worktree: "/wt/a"}, {Path: "app-002", Host: "k16", Worktree: "/wt/a"},
		{Path: "app-003", Host: "mac", Worktree: "/wt/a"}} {
		if err := mesh.PutNode(ctx, c, n); err != nil {
			t.Fatal(err)
		}
	}
	meshtest.ForceState(t, c, "app-001", mesh.State{State: "done"})
	meshtest.ForceState(t, c, "app-002", mesh.State{State: "working"})
	ns, err := LiveNodes(ctx, c, "k16")
	if err != nil || len(ns) != 1 || ns[0].Path != "app-002" {
		t.Fatalf("%v %v", ns, err)
	}
}

func TestLinuxSampleAndLive(t *testing.T) {
	dir := t.TempDir()
	wt := t.TempDir()
	pids := map[int]string{
		1:  pidStat(1, 0, "systemd", [4]int{0, 0, 0, 0}, 1, 100),
		10: pidStat(10, 1, "java", [4]int{100, 0, 0, 0}, 50, 1000),
	}
	fakeProc(t, dir, true, pids, map[int]string{10: wt})
	now := time.Unix(1_800_000_000, 0)
	s := New(Env{Host: "k16", GOOS: "linux", Proc: dir, PageSize: 4096, Now: func() time.Time { return now },
		Nodes: func(context.Context) ([]mesh.Node, error) {
			return []mesh.Node{{Path: "bluke-002", Host: "k16", Worktree: wt}}, nil
		}})
	ctx := context.Background()
	if l, err := s.Live(ctx); l != nil || err != nil {
		t.Fatalf("no previous read: %v %v", l, err)
	}
	if _, err := s.Minute(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	// 10 s on: java used 5 s (0.5 cores); the box 2 of 4 busy... of 2 cores.
	os.WriteFile(filepath.Join(dir, "10", "stat"), []byte(pidStat(10, 1, "java", [4]int{600, 0, 0, 0}, 50, 1000)), 0o644)
	os.WriteFile(filepath.Join(dir, "stat"), []byte(strings.Replace(procStat, "cpu  1000 0 500 8000", "cpu  2000 0 500 9000", 1)), 0o644)
	l, err := s.Live(ctx)
	if err != nil || l == nil {
		t.Fatal(l, err)
	}
	if l.CPU != 1 || l.Totals["bluke-002"].CPU != 0.5 || l.Totals["bluke-002"].RSS != 4096000 || l.Totals["bluke-002"].Procs != 1 {
		t.Fatalf("%+v", l)
	}
	if l.Agents[Other].CPU != 0.5 || l.Pressure10 == nil || *l.Pressure10.CPU != 4 || *l.Pressure.CPU != 3 || len(l.Procs) != 2 || l.Procs[0].Agent != "bluke-002" {
		t.Fatalf("%+v", l)
	}
	now = now.Add(2 * time.Second)
	if again, _ := s.Live(ctx); again != l {
		t.Fatal("a live read within 5 s: want the last answer again")
	}
	// The minute series keeps its own previous read (t=0): 12 s since.
	m, err := s.Minute(ctx)
	if err != nil || m == nil || m.Agents["bluke-002"].CPU != round2(5.0/12) {
		t.Fatalf("%+v %v", m, err)
	}
}
