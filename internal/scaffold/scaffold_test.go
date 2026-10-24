package scaffold

import (
	"github.com/rickdiculousli/buildermesh/internal/brief"
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
		!strings.Contains(out, "<!-- bmesh:done -->\nDONE\n<!-- /bmesh:done -->") ||
		!strings.Contains(out, "<!-- bmesh:proposals -->\nPROPS\n<!-- /bmesh:proposals -->") ||
		!strings.Contains(out, "<!-- bmesh:mesh -->\nMESH\n<!-- /bmesh:mesh -->") {
		t.Fatal(out)
	}
	if strings.Index(out, "bmesh:done") > strings.Index(out, "bmesh:proposals") ||
		strings.Index(out, "bmesh:proposals") > strings.Index(out, "bmesh:mesh") {
		t.Fatal(out)
	}
}

func TestApplyReplacesInPlaceAndIsIdempotent(t *testing.T) {
	once := Apply("# A\n\n<!-- bmesh:done -->\nold\n<!-- /bmesh:done -->\n\ntail\n", "NEW", "P", "M")
	if !strings.Contains(once, "\nNEW\n") || strings.Contains(once, "old") || !strings.Contains(once, "\ntail\n") ||
		!strings.HasSuffix(strings.TrimSpace(once), "<!-- /bmesh:mesh -->") {
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
	src := "intro\n<!-- bmesh:done -->\nx\n<!-- /bmesh:done -->\nmid\n<!-- bmesh:proposals -->\ny\n<!-- /bmesh:proposals -->\nend\n"
	want := strings.Replace(strings.Replace(src, "\nx\n", "\nX2\n", 1), "\ny\n", "\nY2\n", 1) +
		"\n<!-- bmesh:mesh -->\nZ\n<!-- /bmesh:mesh -->\n"
	if got := Apply(src, "X2", "Y2", "Z"); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApplyToAFileWithoutTrailingNewlineIsIdempotent(t *testing.T) {
	once := Apply("# A\nbody", "D", "P", "M")
	if !strings.HasPrefix(once, "# A\nbody\n") || Apply(once, "D", "P", "M") != once {
		t.Fatal(once)
	}
	if empty := Apply("", "D", "P", "M"); !strings.HasPrefix(empty, "<!-- bmesh:done -->") || Apply(empty, "D", "P", "M") != empty {
		t.Fatal(empty)
	}
}

func TestDoneNamesVerifyAndRemote(t *testing.T) {
	done := Done(pb)
	if !strings.HasPrefix(done, "## Definition of done\nA change is done when all of these hold: the repo's verify commands pass (`npm test`)") ||
		!strings.Contains(done, "pushed to `x/pb`") || !strings.Contains(done, "what wasn't checked, and what, if anything, waits on the operator") || !strings.Contains(done, brief.PlainWords) || strings.Contains(done, "Rick") ||
		!strings.Contains(done, "its push state from a fresh fetch") {
		t.Fatal(done)
	}
	none := Done(config.Repo{Name: "cc", Verify: []string{"t"}})
	if strings.Contains(none, "pushed to") || !strings.Contains(none, "merged fast-forward to `main`.") || !strings.Contains(none, "local only") {
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
	// A bracket in the row's own verify commands is not a placeholder.
	bracket := config.Repo{Name: "b", Verify: []string{`[ -z "$(gofmt -l .)" ]`, "go test -run 'Test[A-Z]' ./..."}}
	if m, err := Mesh(bracket, snippet); err != nil || !strings.Contains(m, "`[ -z \"$(gofmt -l .)\" ]`, `go test -run 'Test[A-Z]' ./...`") {
		t.Fatal(m, err)
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
	if err != nil || !strings.Contains(r, "models = [\"claude-opus-5-5\"]") {
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
	for _, want := range []string{"+<!-- bmesh:done -->", "+<!-- bmesh:mesh -->", "+++ b/docs/proposals/README.md",
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
	// An existing kind is left alone, even when it differs from the example,
	// but for the verify kind's command, which follows the row.
	if err := os.WriteFile(filepath.Join(repo, ".bmesh", "routines", "review.toml"), []byte("model = \"mine\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kindPath := filepath.Join(repo, ".bmesh", "routines", "verify.toml")
	if err := os.WriteFile(kindPath, []byte("# mine\nmode = \"inline\"\ncommand = \"mine\"\ntimeout = \"5m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, _, _ = Targets(repo, pb, snippet)
	if d := Plan(again); d != "--- a/.bmesh/routines/verify.toml\n+++ b/.bmesh/routines/verify.toml\n@@ -1,4 +1,4 @@\n"+
		" # mine\n mode = \"inline\"\n-command = \"mine\"\n+command = \"npm test\"\n timeout = \"5m\"\n" {
		t.Fatal(d)
	}
	if _, err := Write(repo, again); err != nil {
		t.Fatal(err)
	}
	if again, _, _ = Targets(repo, pb, snippet); Plan(again) != "" {
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
	if !strings.HasPrefix(targets[0].Want, "<!-- bmesh:done -->") {
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

func TestApplyMigratesLegacyBlocksInPlace(t *testing.T) {
	src := "intro\n<!-- command-center:done -->\nold done\n<!-- /command-center:done -->\nmid\n" +
		"<!-- command-center:proposals -->\nold props\n<!-- /command-center:proposals -->\n" +
		"<!-- bmesh:mesh -->\nold mesh\n<!-- /bmesh:mesh -->\nend\n"
	want := "intro\n<!-- bmesh:done -->\nD\n<!-- /bmesh:done -->\nmid\n" +
		"<!-- bmesh:proposals -->\nP\n<!-- /bmesh:proposals -->\n" +
		"<!-- bmesh:mesh -->\nM\n<!-- /bmesh:mesh -->\nend\n"
	got := Apply(src, "D", "P", "M")
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(got, "command-center") || strings.Count(got, "<!-- bmesh:done -->") != 1 || Apply(got, "D", "P", "M") != got {
		t.Fatal("a legacy block left behind, duplicated, or a second run changes it")
	}
	// Markers of the two prefixes never pair up across a block.
	mixed := "<!-- command-center:done -->\nx\n<!-- /bmesh:done -->\n"
	if out := Apply(mixed, "D", "P", "M"); !strings.HasPrefix(out, mixed) {
		t.Fatalf("a mismatched pair was rewritten:\n%s", out)
	}
}

func TestFollowVerify(t *testing.T) {
	for _, c := range []struct{ name, text, want, err string }{
		{"follows already", "mode = \"inline\"\ncommand = 'npm test'\n", "mode = \"inline\"\ncommand = 'npm test'\n", ""},
		{"rewritten", "command   = \"old\" # was\nmode = \"inline\"\n", "command = \"npm test\"\nmode = \"inline\"\n", ""},
		{"a model kind", "model = \"opus\"\n", "model = \"opus\"\n", ""},
		{"multi-line", "command = \"\"\"\nold\n\"\"\"\n", "", "not on one line"},
		{"not a kind", "command = \n", "", "verify.toml"},
	} {
		got, err := FollowVerify("verify.toml", c.text, pb)
		if got != c.want || (c.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s: %q, %v", c.name, got, err)
		}
	}
}

func TestDriftIsTheBranchsOwn(t *testing.T) {
	repo := t.TempDir()
	row := config.Repo{Name: "pb", Mode: "ship", Verify: []string{"npm test"}}
	targets, _, err := Targets(repo, row, snippet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(repo, targets); err != nil {
		t.Fatal(err)
	}
	kindPath := filepath.Join(repo, ".bmesh", "routines", "verify.toml")
	verify := "mode = \"inline\"\ncommand = \"npm test\"\n"
	writeTest(t, kindPath, verify)
	agents, _ := os.ReadFile(filepath.Join(repo, Agents))
	base := map[string]string{Agents: string(agents), ".bmesh/routines/verify.toml": verify}
	at := func(rel string) (string, bool, error) { s, ok := base[rel]; return s, ok, nil }

	if d, ok, err := Drift(repo, row, snippet, true, at); d != "" || !ok || err != nil {
		t.Fatalf("a clean branch: %q %v %v", d, ok, err)
	}
	// The row moved; the branch touched nothing: not its drift.
	moved := row
	moved.Verify = []string{"npm test", "npm run lint"}
	if d, _, _ := Drift(repo, moved, snippet, true, at); d != "" {
		t.Fatalf("untouched files drifted: %s", d)
	}
	// Text outside the blocks is the repo's own.
	writeTest(t, filepath.Join(repo, Agents), "# Mine\n\n"+string(agents))
	if d, _, _ := Drift(repo, row, snippet, true, at); d != "" {
		t.Fatalf("outside the blocks: %s", d)
	}
	// The branch's own edit of a block or of the verify command is drift.
	writeTest(t, filepath.Join(repo, Agents), strings.ReplaceAll(string(agents), "`npm test`", "`npm test`, `npm run lint`"))
	writeTest(t, kindPath, "mode = \"inline\"\ncommand = \"npm test && npm run lint\"\n")
	d, _, _ := Drift(repo, row, snippet, true, at)
	for _, w := range []string{"--- a/AGENTS.md", "--- a/.bmesh/routines/verify.toml", "+command = \"npm test\""} {
		if !strings.Contains(d, w) {
			t.Errorf("drift lacks %q:\n%s", w, d)
		}
	}
	// Unless the row says the same.
	if d, _, _ := Drift(repo, moved, snippet, true, at); d != "" {
		t.Fatalf("following the row: %s", d)
	}
	// Without agents only the verify kind is compared.
	if d, _, _ := Drift(repo, row, snippet, false, at); strings.Contains(d, "AGENTS.md") || !strings.Contains(d, "verify.toml") {
		t.Fatalf("without agents: %s", d)
	}
	// A repo with no bmesh: marker was never scaffolded.
	writeTest(t, filepath.Join(repo, Agents), "# Mine\n")
	if d, ok, err := Drift(repo, row, snippet, true, at); d != "" || ok || err != nil {
		t.Fatalf("unscaffolded: %q %v %v", d, ok, err)
	}
}

func writeTest(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFollowSetup(t *testing.T) {
	row := config.Repo{Name: "pb", Setup: "npm ci", SetupInputs: []string{":(glob)**/package-lock.json"}}
	block := "[setup]\ncommand = \"npm ci\"\ninputs = [\":(glob)**/package-lock.json\"]\n"
	for _, c := range []struct{ name, text, want, err string }{
		{"appended", "", block, ""},
		{"kept beside the rest", "[slots]\ngame = 2\n\n[routines]\ndir = \"ci\"\n",
			"[slots]\ngame = 2\n\n[routines]\ndir = \"ci\"\n\n" + block, ""},
		{"replaced in place", "[slots]\ngame = 2\n\n[setup]\ncommand = \"npm install\" # old\n\n# kinds\n[routines]\ndir = \"ci\"\n",
			"[slots]\ngame = 2\n\n" + block + "\n# kinds\n[routines]\ndir = \"ci\"\n", ""},
		{"follows already", "# mine\n[setup]\ncommand = 'npm ci'\ninputs = [':(glob)**/package-lock.json']\ntimeout = \"10m\"\n",
			"# mine\n[setup]\ncommand = 'npm ci'\ninputs = [':(glob)**/package-lock.json']\ntimeout = \"10m\"\n", ""},
		{"not a config", "[setup\n", "", "toml"},
		{"a multi-line command", "[setup]\ncommand = \"\"\"\n[ -d node_modules ] || npm ci\n\"\"\"\n\n[slots]\ngame = 2\n",
			block + "\n[slots]\ngame = 2\n", ""},
		{"a header-like line in a literal string", "[setup]\ncommand = '''\n[ -d x ]\n'''\n", block, ""},
	} {
		got, err := FollowSetup(c.text, row)
		if got != c.want || (c.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s: %q, %v", c.name, got, err)
		}
	}
	row.SetupTimeout = "20m"
	if got, _ := FollowSetup(block, row); got != block+"timeout = \"20m\"\n" {
		t.Errorf("timeout: %q", got)
	}
}

func TestTargetsWriteTheRowsSetup(t *testing.T) {
	repo := t.TempDir()
	writeTest(t, filepath.Join(repo, ".bmesh", "config.toml"), "[slots]\ngame = 2\n")
	row := config.Repo{Name: "pb", Mode: "ship", Verify: []string{"npm test"}, Setup: "npm ci"}
	targets, _, err := Targets(repo, row, snippet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(repo, targets); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ".bmesh", "config.toml")); string(b) != "[slots]\ngame = 2\n\n[setup]\ncommand = \"npm ci\"\n" {
		t.Fatalf("config %q", b)
	}
	if targets, _, _ = Targets(repo, row, snippet); Plan(targets) != "" {
		t.Fatalf("second run: %s", Plan(targets))
	}
	// A row with no setup leaves the file alone.
	row.Setup = ""
	targets, _, _ = Targets(repo, row, snippet)
	for _, tg := range targets {
		if tg.Rel == ".bmesh/config.toml" {
			t.Fatalf("a row without setup wrote the config: %q", tg.Want)
		}
	}
}

func TestDriftCoversTheBranchsSetup(t *testing.T) {
	repo := t.TempDir()
	row := config.Repo{Name: "pb", Mode: "ship", Verify: []string{"npm test"}, Setup: "npm ci"}
	targets, _, err := Targets(repo, row, snippet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(repo, targets); err != nil {
		t.Fatal(err)
	}
	agents, _ := os.ReadFile(filepath.Join(repo, Agents))
	cfgPath := filepath.Join(repo, ".bmesh", "config.toml")
	cfg, _ := os.ReadFile(cfgPath)
	base := map[string]string{Agents: string(agents), ".bmesh/config.toml": string(cfg)}
	at := func(rel string) (string, bool, error) { s, ok := base[rel]; return s, ok, nil }
	if d, _, err := Drift(repo, row, snippet, true, at); d != "" || err != nil {
		t.Fatalf("clean: %q %v", d, err)
	}
	// The row moved under an untouched file: not the branch's drift.
	moved := row
	moved.Setup = "pnpm install --frozen-lockfile"
	if d, _, _ := Drift(repo, moved, snippet, true, at); d != "" {
		t.Fatalf("untouched: %s", d)
	}
	// A [slots] edit is the repo's own.
	writeTest(t, cfgPath, string(cfg)+"\n[slots]\ngame = 2\n")
	if d, _, _ := Drift(repo, row, snippet, true, at); d != "" {
		t.Fatalf("slots: %s", d)
	}
	// The branch's own edit of [setup] is drift, unless the row says the same.
	writeTest(t, cfgPath, "[setup]\ncommand = \"pnpm install --frozen-lockfile\"\n")
	if d, _, _ := Drift(repo, row, snippet, true, at); !strings.Contains(d, "--- a/.bmesh/config.toml") || !strings.Contains(d, "+command = \"npm ci\"") {
		t.Fatalf("setup drift: %s", d)
	}
	if d, _, _ := Drift(repo, moved, snippet, true, at); d != "" {
		t.Fatalf("following the row: %s", d)
	}
}
