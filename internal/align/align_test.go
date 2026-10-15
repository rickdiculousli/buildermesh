package align

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh"
	"github.com/rickdiculousli/buildermesh/internal/config"
)

var pb = config.Repo{Name: "pb", Mode: "ship", Remote: "x/pb", Verify: []string{"npm test"}, Routines: []string{"verify", "review"}}

const snippet = "# The buildermesh paragraph\n\nPaste this and fill the bracketed parts.\n\n> **buildermesh.** Builds here run as nodes (`verify` runs [the verify commands]; `review` asks a model).\n"

func TestApplyAppendsBlocksWhenAbsent(t *testing.T) {
	out := Apply("# A\n\nbody\n", "DONE", "PROPS", "MESH")
	if !strings.HasPrefix(out, "# A\n\nbody\n") ||
		!strings.Contains(out, "<!-- command-center:done -->\nDONE\n<!-- /command-center:done -->") ||
		!strings.Contains(out, "<!-- command-center:proposals -->\nPROPS\n<!-- /command-center:proposals -->") ||
		!strings.Contains(out, "<!-- command-center:mesh -->\nMESH\n<!-- /command-center:mesh -->") {
		t.Fatal(out)
	}
	if strings.Index(out, "command-center:done") > strings.Index(out, "command-center:proposals") ||
		strings.Index(out, "command-center:proposals") > strings.Index(out, "command-center:mesh") {
		t.Fatal(out)
	}
}

func TestApplyReplacesInPlaceAndIsIdempotent(t *testing.T) {
	once := Apply("# A\n\n<!-- command-center:done -->\nold\n<!-- /command-center:done -->\n\ntail\n", "NEW", "P", "M")
	if !strings.Contains(once, "\nNEW\n") || strings.Contains(once, "old") || !strings.Contains(once, "\ntail\n") ||
		!strings.HasSuffix(strings.TrimSpace(once), "<!-- /command-center:mesh -->") {
		t.Fatal(once)
	}
	if Apply(once, "NEW", "P", "M") != once {
		t.Fatal("not idempotent")
	}
	if !strings.Contains(Apply(once, "NEW", "P", "M2"), "\nM2\n") {
		t.Fatal("a changed body is not written")
	}
}

func TestApplyNothingOutsideMarkersChanges(t *testing.T) {
	src := "intro\n<!-- command-center:done -->\nx\n<!-- /command-center:done -->\nmid\n<!-- command-center:proposals -->\ny\n<!-- /command-center:proposals -->\nend\n"
	want := strings.Replace(strings.Replace(src, "\nx\n", "\nX2\n", 1), "\ny\n", "\nY2\n", 1) +
		"\n<!-- command-center:mesh -->\nZ\n<!-- /command-center:mesh -->\n"
	if got := Apply(src, "X2", "Y2", "Z"); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyToAFileWithoutTrailingNewlineIsIdempotent(t *testing.T) {
	once := Apply("# A\nbody", "D", "P", "M")
	if !strings.HasPrefix(once, "# A\nbody\n") || Apply(once, "D", "P", "M") != once {
		t.Fatal(once)
	}
	if empty := Apply("", "D", "P", "M"); !strings.HasPrefix(empty, "<!-- command-center:done -->") || Apply(empty, "D", "P", "M") != empty {
		t.Fatal(empty)
	}
}

func TestDoneNamesVerifyAndRemote(t *testing.T) {
	done := Done(pb)
	if !strings.HasPrefix(done, "## Definition of done\nA change is done when all of these hold: the repo's verify commands pass (`npm test`)") ||
		!strings.Contains(done, "pushed to `x/pb`") || !strings.Contains(done, "waits on the operator") || strings.Contains(done, "Rick") {
		t.Fatal(done)
	}
	none := Done(config.Repo{Name: "cc", Verify: []string{"t"}})
	if strings.Contains(none, "pushed to") || !strings.Contains(none, "merged fast-forward to `main`.") {
		t.Fatal(none)
	}
	props := Proposals()
	if !strings.HasPrefix(props, "## Proposals\n") || !strings.Contains(props, "docs/proposals/NNN-slug.md") ||
		!strings.Contains(props, "bmesh spec template") || strings.Contains(props, "command-center") || strings.Contains(props, "delegation") {
		t.Fatal(props)
	}
	for _, s := range []string{done, props} {
		if strings.Contains(s, "<") {
			t.Fatalf("a placeholder or tag in %q", s)
		}
	}
}

func TestMeshFillsTheVerifyCommandsFromTheSnippet(t *testing.T) {
	mesh, err := Mesh(pb, snippet)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(mesh, "## Buildermesh\n") || !strings.Contains(mesh, "`verify` runs `npm test`") ||
		strings.Contains(mesh, "[") || strings.Contains(mesh, "> ") || strings.Contains(mesh, "Paste this") || strings.Contains(mesh, "**buildermesh.**") {
		t.Fatal(mesh)
	}
	if _, err := Mesh(pb, "# nothing quoted\n"); err == nil {
		t.Fatal("no quoted paragraph accepted")
	}
	if _, err := Mesh(pb, "> keeps [a placeholder]\n"); err == nil {
		t.Fatal("a leftover placeholder accepted")
	}
	// The embedded snippet renders for a row with and without verify commands.
	if m, err := Mesh(pb, buildermesh.AgentsSnippet); err != nil || !strings.Contains(m, "`verify` runs `npm test`; `review` asks") {
		t.Fatal(m, err)
	}
	if m, err := Mesh(config.Repo{Name: "bare"}, buildermesh.AgentsSnippet); err != nil || !strings.Contains(m, "`verify` runs nothing yet") {
		t.Fatal(m, err)
	}
}

func TestExampleKinds(t *testing.T) {
	v, err := Example("verify", config.Repo{Verify: []string{"go test ./...", `test -z "$(gofmt -l .)"`}})
	if err != nil || !strings.Contains(v, "mode = \"inline\"\n") || !strings.Contains(v, "deterministic = true\n") ||
		!strings.Contains(v, "command = \"go test ./... && test -z \\\"$(gofmt -l .)\\\"\"\n") || strings.Contains(v, "npm test") {
		t.Fatalf("%v\n%s", err, v)
	}
	r, err := Example("review", pb)
	if err != nil || !strings.Contains(r, "models = [\"claude-opus-5-5\", \"claude-fable-5-1\"]") {
		t.Fatalf("%v\n%s", err, r)
	}
	if _, err := Example("lint", pb); err == nil || !strings.Contains(err.Error(), ".bmesh/routines/lint.toml") {
		t.Fatal(err)
	}
}

func TestTargetsCheckThenWriteIsStable(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	targets, skipped, err := Targets(repo, pb, snippet)
	if err != nil || len(skipped) != 0 {
		t.Fatal(err, skipped)
	}
	diff := Plan(targets)
	for _, want := range []string{"+<!-- command-center:done -->", "+<!-- command-center:mesh -->", "+++ b/docs/proposals/README.md",
		"new empty file docs/proposals/assets/.gitkeep", "+++ b/.bmesh/routines/verify.toml", "+command = \"npm test\"", "+++ b/.bmesh/routines/review.toml",
		"--- a/AGENTS.md", "--- /dev/null"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff lacks %q:\n%s", want, diff)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md")); string(b) != "# A\n" {
		t.Fatal("plan wrote")
	}
	if _, err := os.Stat(filepath.Join(repo, "docs")); err == nil {
		t.Fatal("plan wrote docs")
	}
	written, err := Write(repo, targets)
	if err != nil || strings.Join(written, " ") != "AGENTS.md docs/proposals/README.md docs/proposals/assets/.gitkeep .bmesh/routines/verify.toml .bmesh/routines/review.toml" {
		t.Fatal(written, err)
	}
	b, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if !strings.Contains(string(b), "`verify` runs `npm test`") {
		t.Fatal(string(b))
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "docs", "proposals", "README.md")); string(b) != buildermesh.ProposalsReadme {
		t.Fatal("README differs from the embedded one")
	}
	again, _, err := Targets(repo, pb, snippet)
	if err != nil || Plan(again) != "" {
		t.Fatalf("not idempotent: %v\n%s", err, Plan(again))
	}
	// An existing kind is left alone, even when it differs from the example.
	kindPath := filepath.Join(repo, ".bmesh", "routines", "verify.toml")
	if err := os.WriteFile(kindPath, []byte("command = \"mine\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, _, _ = Targets(repo, pb, snippet)
	if Plan(again) != "" {
		t.Fatal(Plan(again))
	}
	// A new verify text or snippet rewrites only the block.
	pb2 := pb
	pb2.Verify = []string{"make check"}
	again, _, _ = Targets(repo, pb2, snippet)
	d := Plan(again)
	if !strings.Contains(d, "-A change is done") || !strings.Contains(d, "`make check`") || strings.Contains(d, "README") {
		t.Fatal(d)
	}
}

func TestTargetsWithoutAgentsOrVerify(t *testing.T) {
	repo := t.TempDir()
	row := config.Repo{Name: "new", Routines: []string{"verify", "review"}}
	targets, skipped, err := Targets(repo, row, snippet)
	if err != nil || strings.Join(skipped, ",") != "verify" {
		t.Fatal(err, skipped)
	}
	rels := make([]string, 0, len(targets))
	for _, x := range targets {
		rels = append(rels, x.Rel)
	}
	if strings.Join(rels, " ") != "AGENTS.md docs/proposals/README.md docs/proposals/assets/.gitkeep .bmesh/routines/review.toml" {
		t.Fatal(rels)
	}
	if !strings.HasPrefix(targets[0].Want, "<!-- command-center:done -->") {
		t.Fatal(targets[0].Want)
	}
	// A routines dir from the repo's config is honoured.
	if err := os.MkdirAll(filepath.Join(repo, ".bmesh"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, ".bmesh", "config.toml"), []byte("[routines]\ndir = \"kinds\"\n"), 0o644)
	targets, _, _ = Targets(repo, row, snippet)
	if targets[len(targets)-1].Rel != "kinds/review.toml" {
		t.Fatal(targets[len(targets)-1].Rel)
	}
	if _, _, err := Targets(repo, config.Repo{Name: "x", Routines: []string{"lint"}}, snippet); err == nil {
		t.Fatal("an unknown routine passed")
	}
}

func TestUnifiedMatchesDiff(t *testing.T) {
	if _, err := exec.LookPath("diff"); err != nil {
		t.Skip("no diff")
	}
	cases := [][2]string{
		{"a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n", "a\nb\nc\nX\ne\nf\ng\nh\ni\nj\nk\n"},
		{"", "one\ntwo\n"},
		{"one\ntwo", "one\ntwo\n"},
		{"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n", "1\n2\nx\n4\n5\n6\n7\n8\n9\n10\n11\n12\ny\n14\n15\n"},
		{"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n", "1\n2\nx\n4\n5\n6\n7\n8\ny\n10\n"},
	}
	dir := t.TempDir()
	for _, c := range cases {
		a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
		os.WriteFile(a, []byte(c[0]), 0o644)
		os.WriteFile(b, []byte(c[1]), 0o644)
		out, _ := exec.Command("diff", "-u", "--label", "a/f", "--label", "b/f", a, b).Output()
		if got := Unified("a/f", "b/f", c[0], c[1]); got != string(out) {
			t.Errorf("for %q -> %q\ngot:\n%s\nwant:\n%s", c[0], c[1], got, out)
		}
	}
	if Unified("a", "b", "same\n", "same\n") != "" {
		t.Fatal("equal texts differ")
	}
}
