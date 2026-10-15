package spec

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func specText(status, merged string) string {
	return "---\nid: 12\nstatus: " + status + "\nmerged: " + merged + "\n---\nbody\n"
}

func commitSpec(t *testing.T, repo, status, merged string) {
	t.Helper()
	p := filepath.Join(repo, "docs", "proposals")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "012-thing.md"), []byte(specText(status, merged)), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "spec "+status)
}

func revParse(t *testing.T, repo, rev string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", rev).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	repo := filepath.Join(root, "repo")
	runGit(t, root, "init", "--bare", origin)
	runGit(t, root, "init", "-b", "main", repo)
	runGit(t, repo, "remote", "add", "origin", origin)
	return repo
}

const glob = "docs/proposals/{id}-*.md"

func TestFrontmatterParsesLikeProposalsPy(t *testing.T) {
	m, err := Frontmatter("---\n# comment\n\nid: 12\ntitle: a: b\nmerged:\n---\nbody")
	if err != nil {
		t.Fatal(err)
	}
	if m["id"] != "12" || m["title"] != "a: b" || m["merged"] != "" {
		t.Fatalf("got %v", m)
	}
	if _, ok := m["merged"]; !ok {
		t.Fatal("merged key missing")
	}
	for _, bad := range []string{"---\nid: 1\nbare\n---\n", "---\nid: 1\n", "no fence\n"} {
		if _, err := Frontmatter(bad); err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Fatalf("%q: err %v", bad, err)
		}
	}
	if _, err := Frontmatter("---\nid: 1\nbare\n---\n"); err == nil || !strings.Contains(err.Error(), "line 3: bare") {
		t.Fatalf("err %v", err)
	}
}

func TestCheckDoneTrueOnlyWhenLanded(t *testing.T) {
	repo := newRepo(t)
	commitSpec(t, repo, "building", "")
	runGit(t, repo, "push", "origin", "main")
	d, err := CheckDone(repo, glob, "012")
	if err != nil {
		t.Fatal(err)
	}
	if d.OK || d.Why != "status is building" {
		t.Fatalf("%+v", d)
	}
	commitSpec(t, repo, "done", "abc")
	runGit(t, repo, "push", "origin", "main")
	d, _ = CheckDone(repo, glob, "012")
	if d.OK || d.Why != "merged abc is not on origin/main" {
		t.Fatalf("merged not a commit: %+v", d)
	}
	landed := revParse(t, repo, "HEAD")
	commitSpec(t, repo, "done", landed)
	runGit(t, repo, "push", "origin", "main")
	d, err = CheckDone(repo, glob, "012")
	if err != nil || !d.OK || d.Path != "docs/proposals/012-thing.md" || d.Merged != landed {
		t.Fatalf("%+v %v", d, err)
	}
	// A commit on a branch never merged is not on origin/main.
	runGit(t, repo, "checkout", "-q", "-b", "side")
	runGit(t, repo, "commit", "--allow-empty", "-m", "side")
	local := revParse(t, repo, "HEAD")
	runGit(t, repo, "checkout", "-q", "main")
	commitSpec(t, repo, "done", local)
	runGit(t, repo, "push", "origin", "main")
	d, _ = CheckDone(repo, glob, "012")
	if d.OK || d.Why != "merged "+local+" is not on origin/main" {
		t.Fatalf("unpushed merged: %+v", d)
	}
	commitSpec(t, repo, "done", "")
	runGit(t, repo, "push", "origin", "main")
	d, _ = CheckDone(repo, glob, "012")
	if d.OK || d.Why != "merged is empty" {
		t.Fatalf("%+v", d)
	}
	d, err = CheckDone(repo, glob, "099")
	if err != nil || d.OK || d.Why != "no spec 099 on origin/main" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestCheckDoneReadsOriginNotLocal(t *testing.T) {
	repo := newRepo(t)
	commitSpec(t, repo, "building", "")
	runGit(t, repo, "push", "origin", "main")
	commitSpec(t, repo, "done", "abc")
	d, err := CheckDone(repo, glob, "012")
	if err != nil {
		t.Fatal(err)
	}
	if d.OK {
		t.Fatalf("local-only done must not count: %+v", d)
	}
}

func TestFindOnMainWithoutRemote(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "r")
	runGit(t, filepath.Dir(repo), "init", "-b", "main", repo)
	commitSpec(t, repo, "done", "abc")
	p, text, err := FindOnMain(repo, glob, "012")
	if err != nil || p != "docs/proposals/012-thing.md" || !strings.Contains(text, "done") {
		t.Fatalf("%q %v", p, err)
	}
	d, _ := CheckDone(repo, glob, "077")
	if d.Why != "no spec 077 on main" {
		t.Fatalf("%+v", d)
	}
}
