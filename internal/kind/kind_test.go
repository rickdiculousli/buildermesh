package kind

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestParseDefaultsAndValidation(t *testing.T) {
	k, err := Parse("verify", []byte(`command = "npm test"`))
	if err != nil {
		t.Fatal(err)
	}
	if k.Name != "verify" || k.Mode != "pooled" || k.Deterministic || k.Timeout != 20*time.Minute ||
		k.Pool != 1 || k.BatchIdle != 30*time.Second || k.RecycleUses != 20 ||
		k.RecycleContext != 120000 || k.RecycleIdle != 55*time.Minute || k.IsModel() {
		t.Fatalf("defaults wrong: %+v", k)
	}
	m, err := Parse("r", []byte(`models = ["a", "b"]`))
	if err != nil || !m.IsModel() || m.ModelsList()[0] != "a" {
		t.Fatalf("models: %+v %v", m, err)
	}
	s, err := Parse("r", []byte(`model = "x"`))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.ModelsList(); len(got) != 1 || got[0] != "x" {
		t.Fatalf("ModelsList = %v", got)
	}
	ml, err := Parse("r", []byte("command = \"\"\"\necho hi;\n\n\"\"\""))
	if err != nil || ml.Command != "echo hi" {
		t.Fatalf("multi-line command: %q %v", ml.Command, err)
	}
	if c, err := Parse("r", []byte(`command = "echo a#b"`)); err != nil || c.Command != "echo a#b" {
		t.Fatalf("# inside a word: %+v %v", c, err)
	}
	bad := map[string]struct{ name, body string }{
		"command and model":  {"r", `command = "x"` + "\n" + `model = "m"`},
		"model and models":   {"r", `model = "m"` + "\n" + `models = ["n"]`},
		"command and models": {"r", `command = "x"` + "\n" + `models = ["n"]`},
		"neither":            {"r", `mode = "inline"`},
		"bad mode":           {"r", `mode = "exclusive"` + "\n" + `command = "x"`},
		"bad name":           {"Bad_Name", `command = "x"`},
		"unknown key":        {"r", `command = "x"` + "\n" + `timout = "1m"`},
		"bad duration":       {"r", `command = "x"` + "\n" + `timeout = "soon"`},
		"trailing comment":   {"r", "command = \"\"\"\nmake test # note\n\"\"\""},
	}
	for what, c := range bad {
		if _, err := Parse(c.name, []byte(c.body)); err == nil {
			t.Errorf("%s: want error", what)
		}
	}
}

func TestLoadDirReadsEveryKind(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".bmesh", "routines")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"inline", "pooled", "fresh"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "kinds", n+".toml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, n+".toml"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ks, err := LoadDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(ks) != 3 || ks[0].Name != "fresh" || ks[1].Name != "inline" || ks[2].Name != "pooled" {
		t.Fatalf("kinds: %+v", ks)
	}
	if ks[2].Pool != 2 || len(ks[2].Models) != 2 || ks[0].Slots[0] != "browser" || ks[0].Timeout != 5*time.Minute {
		t.Fatalf("fields: %+v", ks)
	}

	if ks, err := LoadDir(t.TempDir()); err != nil || len(ks) != 0 {
		t.Fatalf("empty repo: %v %v", ks, err)
	}

	b, _ := os.ReadFile(filepath.Join("..", "..", "testdata", "kinds", "invalid.toml"))
	if err := os.WriteFile(filepath.Join(dir, "invalid.toml"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(repo); err == nil {
		t.Fatal("invalid kind file: want error")
	}
}

func TestLoadRepoConfig(t *testing.T) {
	repo := t.TempDir()
	rc, err := LoadRepoConfig(repo)
	if err != nil || rc.RoutinesDir != ".bmesh/routines" || len(rc.Slots) != 0 {
		t.Fatalf("defaults: %+v %v", rc, err)
	}
	os.MkdirAll(filepath.Join(repo, ".bmesh"), 0o755)
	os.WriteFile(filepath.Join(repo, ".bmesh", "config.toml"),
		[]byte("[slots]\ngame = 2\n[routines]\ndir = \"ci/kinds\"\n"), 0o644)
	rc, err = LoadRepoConfig(repo)
	if err != nil || rc.Slots["game"] != 2 || rc.RoutinesDir != "ci/kinds" {
		t.Fatalf("read: %+v %v", rc, err)
	}
	os.MkdirAll(filepath.Join(repo, "ci", "kinds"), 0o755)
	os.WriteFile(filepath.Join(repo, "ci", "kinds", "v.toml"), []byte(`command = "x"`), 0o644)
	ks, err := LoadDir(repo)
	if err != nil || len(ks) != 1 || ks[0].Name != "v" {
		t.Fatalf("custom dir: %+v %v", ks, err)
	}
}

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestKindReadFromJobCommit(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	file := filepath.Join(repo, ".bmesh", "routines", "verify.toml")
	os.MkdirAll(filepath.Dir(file), 0o755)
	commit := func(cmd string) string {
		if err := os.WriteFile(file, []byte("command = \""+cmd+"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, repo, "add", "-A")
		git(t, repo, "commit", "-q", "-m", cmd)
		return trim(git(t, repo, "rev-parse", "HEAD"))
	}
	c1 := commit("npm test")
	c2 := commit("npm run verify")
	for commit, want := range map[string]string{c1: "npm test", c2: "npm run verify"} {
		k, err := LoadAt(repo, commit, "verify")
		if err != nil || k.Command != want || k.Name != "verify" {
			t.Fatalf("LoadAt(%s) = %+v, %v; want %q", commit[:8], k, err, want)
		}
	}
	if _, err := LoadAt(repo, c1, "absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing kind: want ErrNotFound, got %v", err)
	}

	// a custom dir named by the commit's own config.toml
	os.WriteFile(filepath.Join(repo, ".bmesh", "config.toml"), []byte("[routines]\ndir = \"kinds\"\n"), 0o644)
	os.MkdirAll(filepath.Join(repo, "kinds"), 0o755)
	os.WriteFile(filepath.Join(repo, "kinds", "lint.toml"), []byte(`command = "make lint"`), 0o644)
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "custom dir")
	k, err := LoadAt(repo, "HEAD", "lint")
	if err != nil || k.Command != "make lint" {
		t.Fatalf("custom dir: %+v %v", k, err)
	}
	if _, err := LoadAt(repo, "--output=x", "verify"); err == nil {
		t.Fatal("option-like commit: want error")
	}
	if _, err := os.Stat(filepath.Join(repo, "x")); err == nil {
		t.Fatal("option-like commit created a file")
	}
	if _, err := LoadAt(repo, "HEAD", "../x"); err == nil {
		t.Fatal("bad name: want error")
	}
}

func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

func TestExamplesParse(t *testing.T) {
	for _, n := range []string{"verify", "review", "surface"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "examples", "routines", n+".toml"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(n, b); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
}
