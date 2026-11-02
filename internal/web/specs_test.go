package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

func specFile(id, status string) string {
	return "---\nid: " + id + "\ntitle: T\nstatus: " + status + "\n---\nbody\n"
}

// commitAt commits files on the day offset from usageNow with message msg.
func commitAt(t *testing.T, repo string, day int, msg string, files map[string]string) {
	t.Helper()
	d := at(day, 9, 0).Format(time.RFC3339)
	t.Setenv("GIT_COMMITTER_DATE", d)
	t.Setenv("GIT_AUTHOR_DATE", d)
	names := []string{}
	for n, c := range files {
		p := filepath.Join(repo, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	meshtest.Git(t, repo, append([]string{"add", "--"}, names...)...)
	meshtest.Git(t, repo, "commit", "-q", "-m", msg)
}

func TestSpecsBurndownAndFamilies(t *testing.T) {
	ctx, c := connect(t)
	repo := filepath.Join(t.TempDir(), "buildermesh")
	meshtest.Git(t, filepath.Dir(repo), "init", "-q", "-b", "main", "buildermesh")
	p := func(n string) string { return "docs/proposals/" + n }
	commitAt(t, repo, -5, "propose", map[string]string{p("010-old.md"): specFile("10", "accepted"), p("011-rej.md"): specFile("11", "proposed"), p("README.md"): "x\n"})
	commitAt(t, repo, -4, "propose", map[string]string{p("096-new.md"): specFile("96", "accepted"), p("098-pre.md"): specFile("98", "accepted"), p("099-open.md"): specFile("99", "proposed"), p("012-shelved.md"): specFile("12", "accepted")})
	commitAt(t, repo, -3, "reject and shelve", map[string]string{p("011-rej.md"): specFile("11", "rejected"), p("012-shelved.md"): specFile("12", "later")})
	commitAt(t, repo, -2, "old landing", map[string]string{p("098-pre.md"): specFile("98", "done")})
	commitAt(t, repo, -1, "land\n\nSpec: 96\nVerified: ok", map[string]string{p("096-new.md"): specFile("96", "done")})

	nodes := []mesh.Node{
		{Path: "buildermesh-096", Role: "lead", Repo: "buildermesh", Spec: "096", Family: "fam-a", StartedAt: at(-1, 8, 0).Format(time.RFC3339)},
		{Path: "buildermesh-097", Role: "lead", Repo: "buildermesh", Spec: "097", Family: "fam-a", StartedAt: at(-1, 9, 0).Format(time.RFC3339)},
		{Path: "buildermesh-050", Role: "lead", Repo: "buildermesh", Spec: "050", Family: "fam-old", StartedAt: at(-40, 9, 0).Format(time.RFC3339)},
		{Path: "buildermesh-096.x", Role: "executor", Repo: "buildermesh", Family: "fam-a", StartedAt: at(-1, 8, 0).Format(time.RFC3339)},
	}
	for _, n := range nodes {
		if err := mesh.PutNode(ctx, c, n); err != nil {
			t.Fatal(err)
		}
	}
	doneAt := at(-1, 11, 0)
	meshtest.ForceState(t, c, "buildermesh-096", mesh.State{State: "done", At: doneAt.Format(time.RFC3339)})
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "daemon.k16", Event: "land-refused", To: "buildermesh-096", Text: "verify failed", At: at(-1, 10, 0).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Repos: map[string]config.Repo{"buildermesh": {Path: repo}}, Spec: config.SpecConfig{Glob: "docs/proposals/{id}-*.md"}}
	srv := httptest.NewServer(Handler(c, time.Hour, Options{Cfg: cfg, Host: "k16", Now: func() time.Time { return usageNow }}))
	t.Cleanup(srv.Close)

	var v SpecsView
	getJSON(t, srv, "/api/specs", 200, &v)
	if v.From != at(-29, 0, 0).Unix() || v.To != usageNow.Unix() || len(v.Repos) != 1 || len(v.Repos[0].Days) != 30 {
		t.Fatalf("window/repos: %+v", v)
	}
	days := v.Repos[0].Days
	if days[29].Day != "2026-10-24" || days[0].Day != "2026-09-25" {
		t.Fatalf("days: %s..%s", days[0].Day, days[29].Day)
	}
	// index 29+d is the day offset d: open = proposed and not landed by then (011 rejected and 012 shelved are never counted).
	for d, want := range map[int]struct {
		open   int
		landed []int
	}{-6: {0, []int{}}, -5: {1, []int{}}, -4: {4, []int{}}, -3: {4, []int{}}, -2: {3, []int{98}}, -1: {2, []int{96}}, 0: {2, []int{}}} {
		got := days[29+d]
		if got.Open != want.open || !reflect.DeepEqual(got.Landed, want.landed) {
			t.Errorf("day %d: %+v, want %+v", d, got, want)
		}
	}

	if len(v.Families) != 1 || v.Families[0].Family != "fam-a" {
		t.Fatalf("families: %+v", v.Families)
	}
	f := v.Families[0]
	if !f.Live || f.Repo != "buildermesh" || f.Start != at(-1, 8, 0).Unix() || len(f.Leads) != 2 || f.Leads[0].Done != doneAt.Unix() || f.Leads[1].Done != 0 {
		t.Errorf("family: %+v", f)
	}
	if want := [][2]int64{{at(-1, 8, 0).Unix(), 2}, {doneAt.Unix(), 1}}; !reflect.DeepEqual(f.Steps, want) {
		t.Errorf("steps %v, want %v", f.Steps, want)
	}
	if want := [][3]any{{float64(at(-1, 10, 0).Unix()), "buildermesh-096", "verify failed"}}; !reflect.DeepEqual(f.Refused, want) {
		t.Errorf("refused %v, want %v", f.Refused, want)
	}

	getJSON(t, srv, "/api/specs?repo=buildermesh", 200, &v)
	getJSON(t, srv, "/api/specs?repo=nope", 404, nil)
}
