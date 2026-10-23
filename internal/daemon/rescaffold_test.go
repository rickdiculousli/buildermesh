package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/level"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

const (
	scafAgents = "# App\n\n<!-- bmesh:done -->\n## Definition of done\nold words\n<!-- /bmesh:done -->\n"
	scafVerify = "mode = \"inline\"\ndeterministic = true\ncommand = \"go test ./...\"\n"
	scafConfig = "# the repo's config\n[setup]\ncommand = \"npm ci\"\n\n[slots]\nmax = 2\n"
)

// scaffoldFixture is the test host joined, its rows naming app (checked
// out at box, a clone of origin) as this box's ship repo, with the hosts
// loop not yet started.
type scaffoldFixture struct {
	*hostsFixture
	origin, box, peer string
}

func scaffoldSetup(t *testing.T, files map[string]string) *scaffoldFixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	f := setup(t)
	h := hostsOn(t, f, 100*time.Millisecond, "ping")
	h.stop = func() {}
	s := &scaffoldFixture{hostsFixture: h}
	root := t.TempDir()
	s.origin, s.box, s.peer = filepath.Join(root, "origin.git"), filepath.Join(root, "app"), filepath.Join(root, "peer")
	meshtest.Git(t, root, "init", "-q", "--bare", "-b", "main", s.origin)
	meshtest.Git(t, root, "clone", "-q", s.origin, s.peer)
	if files == nil {
		files = map[string]string{"AGENTS.md": scafAgents, ".bmesh/routines/verify.toml": scafVerify, ".bmesh/config.toml": scafConfig}
	}
	meshtest.CommitFiles(t, s.peer, files)
	meshtest.Git(t, s.peer, "push", "-q", "origin", "HEAD:main")
	meshtest.Git(t, root, "clone", "-q", s.origin, s.box)
	f.d.Cfg.Runner.Worktrees = filepath.Join(root, "worktrees")
	rows, err := config.ParseRepos(s.rows("npm ci", "go test ./...", ""))
	if err != nil {
		t.Fatal(err)
	}
	f.d.Cfg.Repos = rows
	return s
}

// rows is the registry's rows: app on this box with setup and verify,
// other on mac, with extra appended.
func (s *scaffoldFixture) rows(setup, verify, extra string) string {
	return fmt.Sprintf("[repos.app]\npath = %q\nhost = %q\nsetup = %q\nverify = [%q]\n\n"+
		"[repos.other]\npath = \"/nowhere/other\"\nhost = \"mac\"\nsetup = \"make deps\"\n%s", s.box, testHost, setup, verify, extra)
}

// originLog is origin main's subjects, newest first.
func (s *scaffoldFixture) originLog(t *testing.T) []string {
	t.Helper()
	return strings.Split(meshtest.Git(t, s.origin, "log", "--format=%s", "main"), "\n")
}

func (s *scaffoldFixture) show(t *testing.T, rel string) string {
	t.Helper()
	return meshtest.Git(t, s.origin, "show", "main:"+rel)
}

func (s *scaffoldFixture) records(t *testing.T) map[string]scaffoldRec {
	t.Helper()
	f, err := loadScaffolded(s.d.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	return f.Repos
}

func TestRescaffoldWritesSetupOnlyAndOnce(t *testing.T) {
	s := scaffoldSetup(t, nil)
	s.start(t)
	s.putRegistry(t, adminRegistry(2, s.rows("npm ci && npm --prefix api ci", "go test ./...", "")))
	waitTrue(t, "the scaffold event", func() bool { return len(s.claims(t, "scaffold")) > 0 })
	time.Sleep(500 * time.Millisecond) // several ticks: nothing more
	log := s.originLog(t)
	if len(log) != 2 || log[0] != "Scaffold: app follows registry v2" {
		t.Fatalf("origin log %q", log)
	}
	body := meshtest.Git(t, s.origin, "log", "-1", "--format=%an <%ae>%n%b", "main")
	if body != "bmesh <bmesh@localhost>\nsetup = \"npm ci && npm --prefix api ci\"" {
		t.Fatalf("commit %q", body)
	}
	if got := meshtest.Git(t, s.origin, "show", "--name-only", "--format=", "main"); got != ".bmesh/config.toml" {
		t.Fatalf("files %q", got)
	}
	if got := s.show(t, ".bmesh/config.toml"); got != "# the repo's config\n[setup]\ncommand = \"npm ci && npm --prefix api ci\"\n\n[slots]\nmax = 2" {
		t.Fatalf("config.toml %q", got)
	}
	// The main checkout followed.
	if b, _ := os.ReadFile(filepath.Join(s.box, ".bmesh/config.toml")); !strings.Contains(string(b), "npm --prefix api ci") {
		t.Fatalf("box config.toml %q", b)
	}
	evs := s.claims(t, "scaffold")
	sha := meshtest.Git(t, s.origin, "rev-parse", "main")
	want := fmt.Sprintf("%s: scaffolded app for registry v2 at %s (.bmesh/config.toml)", testHost, sha[:8])
	if len(evs) != 1 || evs[0].Text != want || evs[0].Body != "ok v2" {
		t.Fatalf("events %+v, want %q", evs, want)
	}
	if r := s.records(t)["app"]; !r.Landed || r.Commit != sha || r.Version != 2 {
		t.Fatalf("record %+v", r)
	}
	if n := strings.Count(meshtest.Git(t, s.box, "branch", "--list"), "scaffold"); n != 0 {
		t.Fatalf("%d scaffold branches left", n)
	}
}

func TestRescaffoldVerifyLeavesAgentsAndNamesTheDoneBlock(t *testing.T) {
	s := scaffoldSetup(t, nil)
	s.start(t)
	s.putRegistry(t, adminRegistry(2, s.rows("npm ci", "go test ./... && go vet ./...", "")))
	waitTrue(t, "the scaffold event", func() bool { return len(s.claims(t, "scaffold")) > 0 })
	if got := s.show(t, ".bmesh/routines/verify.toml"); !strings.Contains(got, `command = "go test ./... && go vet ./..."`) ||
		!strings.HasPrefix(got, "mode = \"inline\"\ndeterministic = true\n") {
		t.Fatalf("verify.toml %q", got)
	}
	if got := s.show(t, "AGENTS.md") + "\n"; got != scafAgents {
		t.Fatalf("AGENTS.md changed: %q", got)
	}
	if got := meshtest.Git(t, s.origin, "show", "--name-only", "--format=", "main"); got != ".bmesh/routines/verify.toml" {
		t.Fatalf("files %q", got)
	}
	ev := s.claims(t, "scaffold")[0]
	if !strings.HasSuffix(ev.Text, "(.bmesh/routines/verify.toml); AGENTS.md's done block still names the old verify; "+
		"run bmesh scaffold app on a box with the current bmesh") {
		t.Fatalf("event %q", ev.Text)
	}
}

// A registry that changes another row, or this row's other fields, or a
// row on another box, writes nothing: a deliberate edit on main stays.
func TestRescaffoldLeavesWhatTheRowDidNotChange(t *testing.T) {
	s := scaffoldSetup(t, nil)
	// main's verify kind differs from the row on purpose.
	meshtest.CommitFiles(t, s.peer, map[string]string{".bmesh/routines/verify.toml": strings.Replace(scafVerify, "go test ./...", "go test -race ./...", 1)})
	meshtest.Git(t, s.peer, "push", "-q", "origin", "HEAD:main")
	s.start(t)
	v2 := strings.Replace(s.rows("npm ci", "go test ./...", "remote = \"me/other\"\n"), "make deps", "make deps all", 1)
	v2 = strings.Replace(v2, "[repos.app]\n", "[repos.app]\nremote = \"me/app\"\n", 1)
	s.putRegistry(t, adminRegistry(2, v2))
	waitTrue(t, "registry v2 taken", func() bool {
		b, _ := os.ReadFile(config.ReposPath(s.d.Cfg.Path()))
		return strings.Contains(string(b), "me/app")
	})
	time.Sleep(500 * time.Millisecond)
	if log := s.originLog(t); len(log) != 2 {
		t.Fatalf("origin log %q", log)
	}
	if evs := s.claims(t, "scaffold"); len(evs) != 0 {
		t.Fatalf("events %+v", evs)
	}
	if recs := s.records(t); len(recs) != 0 {
		t.Fatalf("records %+v", recs)
	}
}

func TestRescaffoldPendingSurvivesARestart(t *testing.T) {
	s := scaffoldSetup(t, nil)
	rows, err := config.ParseRepos(s.rows("npm ci --ignore-scripts", "go test ./...", ""))
	if err != nil {
		t.Fatal(err)
	}
	s.d.Cfg.Repos = rows // repos.toml at start is v3's, taken before the restart
	if err := hosts.SaveRegistry(s.d.StateDir, adminRegistry(3, s.rows("npm ci --ignore-scripts", "go test ./...", ""))); err != nil {
		t.Fatal(err)
	}
	if err := saveScaffolded(s.d.StateDir, scaffolded{Version: 3, Rows: rowsOf(rows),
		Repos: map[string]scaffoldRec{"app": {Version: 3, Fields: []string{"setup"}, Told: "failed"}}}); err != nil {
		t.Fatal(err)
	}
	s.start(t)
	waitTrue(t, "the pending version lands", func() bool { return s.records(t)["app"].Landed })
	if log := s.originLog(t); log[0] != "Scaffold: app follows registry v3" {
		t.Fatalf("origin log %q", log)
	}
}

// A registry another process took while the daemon was down (repos.toml
// already the new rows at start) is still compared with the rows the
// daemon recorded last.
func TestRescaffoldSeesARegistryTakenWhileDown(t *testing.T) {
	s := scaffoldSetup(t, nil)
	if err := saveScaffolded(s.d.StateDir, scaffolded{Version: 1, Rows: rowsOf(s.d.Cfg.Repos), Repos: map[string]scaffoldRec{}}); err != nil {
		t.Fatal(err)
	}
	v2 := s.rows("npm ci && make", "go test ./...", "")
	rows, err := config.ParseRepos(v2)
	if err != nil {
		t.Fatal(err)
	}
	s.d.Cfg.Repos = rows
	if err := hosts.SaveRegistry(s.d.StateDir, adminRegistry(2, v2)); err != nil {
		t.Fatal(err)
	}
	s.start(t)
	waitTrue(t, "v2 lands", func() bool { return s.records(t)["app"].Landed })
	if log := s.originLog(t); log[0] != "Scaffold: app follows registry v2" {
		t.Fatalf("origin log %q", log)
	}
}

func TestRescaffoldWaitsForTheRepoLock(t *testing.T) {
	s := scaffoldSetup(t, nil)
	unlock, err := level.Lock(s.d.StateDir, "app") // a lead's land holds it
	if err != nil {
		t.Fatal(err)
	}
	s.start(t)
	s.putRegistry(t, adminRegistry(2, s.rows("npm ci && make", "go test ./...", "")))
	time.Sleep(700 * time.Millisecond)
	if log := s.originLog(t); len(log) != 1 {
		unlock()
		t.Fatalf("landed under the lock: %q", log)
	}
	unlock()
	waitTrue(t, "lands once the lock is free", func() bool { return len(s.originLog(t)) == 2 })
}

func TestRescaffoldWritesNothingUnscaffoldedForkOrElsewhere(t *testing.T) {
	t.Run("no marker", func(t *testing.T) {
		s := scaffoldSetup(t, map[string]string{"AGENTS.md": "# App\n", ".bmesh/config.toml": scafConfig})
		s.start(t)
		s.putRegistry(t, adminRegistry(2, s.rows("npm ci && make", "go test ./...", "")))
		waitTrue(t, "the record settles", func() bool { return s.records(t)["app"].Landed })
		if log := s.originLog(t); len(log) != 1 {
			t.Fatalf("origin log %q", log)
		}
		if evs := s.claims(t, "scaffold"); len(evs) != 0 {
			t.Fatalf("events %+v", evs)
		}
	})
	for name, extra := range map[string]string{"fork": "mode = \"fork\"\n", "another host": "host = \"mac\"\n"} {
		t.Run(name, func(t *testing.T) {
			s := scaffoldSetup(t, nil)
			rows := strings.Replace(s.rows("npm ci", "go test ./...", ""), fmt.Sprintf("host = %q\n", testHost), "", 1)
			rows0, err := config.ParseRepos(strings.Replace(rows, "[repos.app]\n", "[repos.app]\n"+extra, 1))
			if err != nil {
				t.Fatal(err)
			}
			s.d.Cfg.Repos = rows0
			s.start(t)
			v2 := strings.Replace(s.rows("npm ci && make", "go test ./...", ""), fmt.Sprintf("host = %q\n", testHost), "", 1)
			s.putRegistry(t, adminRegistry(2, strings.Replace(v2, "[repos.app]\n", "[repos.app]\n"+extra, 1)))
			waitTrue(t, "registry v2 taken", func() bool {
				b, _ := os.ReadFile(config.ReposPath(s.d.Cfg.Path()))
				return strings.Contains(string(b), "npm ci && make")
			})
			time.Sleep(500 * time.Millisecond)
			if log := s.originLog(t); len(log) != 1 {
				t.Fatalf("origin log %q", log)
			}
			if recs := s.records(t); len(recs) != 0 {
				t.Fatalf("records %+v", recs)
			}
		})
	}
}

func TestRescaffoldRetriesARefusedPush(t *testing.T) {
	s := scaffoldSetup(t, nil)
	// origin refuses pushes while the gate file exists.
	gate := filepath.Join(t.TempDir(), "gate")
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(s.origin, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntest ! -e '"+gate+"' || { echo moved >&2; exit 1; }\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.start(t)
	s.putRegistry(t, adminRegistry(2, s.rows("npm ci && make", "go test ./...", "")))
	waitTrue(t, "a retry", func() bool { return len(s.claims(t, "scaffold")) >= 2 })
	if err := os.Remove(gate); err != nil {
		t.Fatal(err)
	}
	waitTrue(t, "lands", func() bool { return s.records(t)["app"].Landed })
	evs := s.claims(t, "scaffold")
	last := evs[len(evs)-1]
	if evs[0].Body != "failed v2" || !strings.HasPrefix(evs[0].Text, testHost+": scaffold app for registry v2 failed: refused: push main: git push: remote: moved; To ") ||
		evs[1].Body != "failed v2 again" || last.Body != "ok v2" {
		t.Fatalf("events %+v", evs)
	}
	if n := strings.Count(meshtest.Git(t, s.box, "branch", "--list"), "scaffold"); n != 0 {
		t.Fatalf("%d scaffold branches left", n)
	}
}

func TestRescaffoldNoCheckoutTellsOncePerVersion(t *testing.T) {
	s := scaffoldSetup(t, nil)
	if err := os.RemoveAll(s.box); err != nil {
		t.Fatal(err)
	}
	s.start(t)
	s.putRegistry(t, adminRegistry(2, s.rows("npm ci && make", "go test ./...", "")))
	waitTrue(t, "the event", func() bool { return len(s.claims(t, "scaffold")) > 0 })
	time.Sleep(500 * time.Millisecond)
	evs := s.claims(t, "scaffold")
	want := fmt.Sprintf("%s: registry v2 changes app's scaffold but %s has no checkout; run bmesh scaffold app where it has one", testHost, s.box)
	if len(evs) != 1 || evs[0].Text != want || evs[0].Body != "nocheckout v2" {
		t.Fatalf("events %+v", evs)
	}
	// The checkout comes, and its first failure is told, not taken as a retry.
	meshtest.Git(t, filepath.Dir(s.box), "clone", "-q", s.origin, s.box)
	if err := os.WriteFile(filepath.Join(s.origin, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	waitTrue(t, "a failure", func() bool { return len(s.claims(t, "scaffold")) > 1 })
	if evs := s.claims(t, "scaffold"); evs[1].Body != "failed v2" {
		t.Fatalf("events %+v", evs)
	}
}

func TestPushScaffoldFailureOncePerVersion(t *testing.T) {
	f, n := pushSetup(t)
	f.d.Cfg.Notify.Push = []string{}
	f.d.Cfg.Hosts.Enabled = true
	startPush(t, f.d)
	time.Sleep(200 * time.Millisecond)
	f.event(t, mesh.Event{From: "daemon.k16", Event: "scaffold", Body: "failed v2", Text: "k16: scaffold app for registry v2 failed: x"})
	f.event(t, mesh.Event{From: "daemon.k16", Event: "scaffold", Body: "failed v2 again", Text: "k16: scaffold app for registry v2 failed: x"})
	f.event(t, mesh.Event{From: "daemon.k16", Event: "scaffold", Body: "ok v2", Text: "k16: scaffolded app for registry v2 at 1234abcd (.bmesh/config.toml)"})
	got := n.waitRequests(t, 2)
	if got[0].Title != "k16: scaffold app for registry v2 failed: x" || got[0].Priority != "high" ||
		got[1].Title != "k16: scaffolded app for registry v2 at 1234abcd (.bmesh/config.toml)" || got[1].Priority != "default" {
		t.Fatalf("pushes %+v", got)
	}
}
