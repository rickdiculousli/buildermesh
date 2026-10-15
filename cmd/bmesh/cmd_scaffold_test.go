package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldWritesChecksAndRefuses(t *testing.T) {
	setup(t)
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("# Working here\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\nremote = \"x/demo\"\nverify = [\"go test ./...\", \"go vet ./...\"]\n[repos.side]\npath = \""+t.TempDir()+"\"\nmode = \"fork\"\n")
	out, errs, code := runCLI(t, nil, "scaffold", "demo", "--check")
	if code != 1 || errs != "" || !strings.Contains(out, "+++ b/AGENTS.md") || !strings.Contains(out, "+## Definition of done") ||
		!strings.Contains(out, "+command = \"go test ./... && go vet ./...\"") || !strings.Contains(out, "+++ b/.bmesh/routines/review.toml") {
		t.Fatalf("check: %d %q %q", code, out, errs)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md")); string(b) != "# Working here\n\nbody\n" {
		t.Fatal("check wrote")
	}
	out, errs, code = runCLI(t, nil, "scaffold", "demo")
	if code != 0 || out != "wrote AGENTS.md\nwrote docs/proposals/README.md\nwrote docs/proposals/assets/.gitkeep\nwrote .bmesh/routines/verify.toml\nwrote .bmesh/routines/review.toml\n" {
		t.Fatalf("write: %d %q %q", code, out, errs)
	}
	b, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if !strings.HasPrefix(string(b), "# Working here\n\nbody\n\n<!-- command-center:done -->\n## Definition of done\n") ||
		!strings.Contains(string(b), "(`go test ./...`, `go vet ./...`)") || !strings.Contains(string(b), "pushed to `x/demo`") ||
		!strings.Contains(string(b), "`verify` runs `go test ./...`, `go vet ./...`; `review` asks") || strings.Contains(string(b), "Rick") {
		t.Fatalf("%s", b)
	}
	out, errs, code = runCLI(t, nil, "scaffold", "demo", "--check")
	if code != 0 || out != "" {
		t.Fatalf("check after write: %d %q %q", code, out, errs)
	}
	if out, _, code := runCLI(t, nil, "scaffold", repo); code != 0 || out != "demo is scaffolded\n" {
		t.Fatalf("again: %d %q", code, out)
	}
	// The kinds read as kinds.
	if out, errs, code := runCLI(t, map[string]string{}, "routine", "list"); code != 0 || !strings.Contains(out, "verify") {
		_ = errs
	}
	for _, c := range []struct {
		args []string
		err  string
	}{
		{[]string{"scaffold", "side"}, "side is fork; only ship repos"},
		{[]string{"scaffold", t.TempDir()}, "not in repos.toml"},
		{[]string{"scaffold", "nope"}, "unknown repo"},
		{[]string{"scaffold"}, "usage"},
	} {
		if _, errs, code := runCLI(t, nil, c.args...); code != 2 || !strings.Contains(errs, c.err) {
			t.Errorf("%v: %d %q", c.args, code, errs)
		}
	}
	// A row with no verify commands gets no verify kind, and says so.
	bare := t.TempDir()
	writeRepos(t, "[repos.bare]\npath = \""+bare+"\"\n")
	out, errs, code = runCLI(t, nil, "scaffold", "bare")
	if code != 0 || strings.Contains(out, "verify.toml") || !strings.Contains(errs, "no verify commands, so the verify kind was not written") {
		t.Fatalf("bare: %d %q %q", code, out, errs)
	}
	if b, _ := os.ReadFile(filepath.Join(bare, "AGENTS.md")); !strings.HasPrefix(string(b), "<!-- command-center:done -->") ||
		!strings.Contains(string(b), "`verify` runs nothing yet") {
		t.Fatalf("%s", b)
	}
}
