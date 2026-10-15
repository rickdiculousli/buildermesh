package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false",
		"-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestTmuxStartMakesWorktreeAndWindow(t *testing.T) {
	log := useFake(t, "BMESH_TMUX_COMMAND", "fake-tmux.sh", "FAKE_TMUX_LOG")
	t.Setenv("FAKE_TMUX_NO_SESSION", "1")
	root := t.TempDir()
	repo := filepath.Join(root, "demo")
	git(t, root, "init", "-b", "main", repo)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "one")
	brief := filepath.Join(root, "brief.md")
	if err := os.WriteFile(brief, []byte("do the thing"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Tmux{Session: "mesh", Worktrees: filepath.Join(root, "wt")}
	got, err := r.Start(context.Background(), StartSpec{RepoPath: repo, BriefPath: brief, ThreadID: "th-9"})
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(root, "wt", "demo", "th-9")
	if got != (Started{Thread: "th-9", Branch: "th-9", Worktree: wt}) {
		t.Fatalf("started %+v", got)
	}
	if b := git(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); b != "th-9" {
		t.Fatalf("worktree branch %q", b)
	}
	want := "has-session -t mesh\n" +
		"new-session -d -s mesh -c " + wt + "\n" +
		"new-window -t mesh -n th-9 -c " + wt + " claude\n" +
		"capture-pane -p -t mesh:th-9\n" +
		"send-keys -t mesh:th-9 -l do the thing\n" +
		"send-keys -t mesh:th-9 Enter\n"
	if l := readLog(t, log); l != want {
		t.Fatalf("log %q\nwant %q", l, want)
	}
	if st, err := r.Status(context.Background(), "th-9"); err != nil || st.Known {
		t.Fatalf("status %+v %v", st, err)
	}
	if err := r.Send(context.Background(), "th-9", "wake up"); err != nil {
		t.Fatal(err)
	}
	if err := r.Interrupt(context.Background(), "th-9"); err != nil {
		t.Fatal(err)
	}
	if l := readLog(t, log); !strings.HasSuffix(l, "send-keys -t mesh:th-9 -l wake up\nsend-keys -t mesh:th-9 Enter\nsend-keys -t mesh:th-9 Escape\n") {
		t.Fatalf("log %q", l)
	}
}

func TestTmuxStartAnswersDialogsBeforeTyping(t *testing.T) {
	log := useFake(t, "BMESH_TMUX_COMMAND", "fake-tmux.sh", "FAKE_TMUX_LOG")
	root := t.TempDir()
	repo := filepath.Join(root, "demo")
	git(t, root, "init", "-b", "main", repo)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", "one")
	brief := filepath.Join(root, "brief.md")
	if err := os.WriteFile(brief, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
	panes := filepath.Join(root, "panes")
	if err := os.Mkdir(panes, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, pane := range []string{
		"", // claude not up yet
		"Quick safety check ... Do you trust this folder?\n ❯ No, exit\n   Yes, I trust this folder\n Enter to confirm",
		"Try the new fullscreen renderer?\n Enter to confirm · Esc to cancel",
		"❯ Try \"create a util\"\n auto mode on (shift+tab to cycle)",
	} {
		if err := os.WriteFile(filepath.Join(panes, fmt.Sprint(i+1)), []byte(pane), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FAKE_TMUX_PANES", panes)
	t.Setenv("BMESH_CONFIG", "/cfg/bm.toml")
	r := Tmux{Session: "mesh", Worktrees: filepath.Join(root, "wt")}
	if _, err := r.Start(context.Background(), StartSpec{RepoPath: repo, BriefPath: brief, ThreadID: "th-1"}); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(root, "wt", "demo", "th-1")
	want := "has-session -t mesh\n" +
		"new-window -t mesh -n th-1 -c " + wt + " -e BMESH_CONFIG=/cfg/bm.toml claude\n" +
		"capture-pane -p -t mesh:th-1\n" + // nothing yet
		"capture-pane -p -t mesh:th-1\n" + // the trust question
		"send-keys -t mesh:th-1 Down\nsend-keys -t mesh:th-1 Enter\n" +
		"capture-pane -p -t mesh:th-1\n" + // the renderer offer
		"send-keys -t mesh:th-1 Escape\n" +
		"capture-pane -p -t mesh:th-1\n" + // the prompt
		"send-keys -t mesh:th-1 -l go\nsend-keys -t mesh:th-1 Enter\n"
	if l := readLog(t, log); l != want {
		t.Fatalf("log %q\nwant %q", l, want)
	}
}

func TestDialogKeys(t *testing.T) {
	for _, c := range []struct {
		pane string
		want []string
	}{
		{"", []string{""}},
		{"Welcome\n Choose the text style that looks best", []string{"Enter"}},
		{"Security notes\n Press Enter to continue…", []string{"Enter"}},
		{"Is this a project you created or one you trust this folder", []string{"Down", "Enter"}},
		{"Try the new fullscreen renderer?\n Enter to confirm", []string{"Escape"}},
		{"Something else\n Enter to confirm · Esc to cancel", []string{"Enter"}},
		{"❯ \n ? for shortcuts", nil},
		{"auto mode on (shift+tab to cycle)", nil},
	} {
		got := dialogKeys(c.pane)
		if strings.Join(got, ",") != strings.Join(c.want, ",") || (got == nil) != (c.want == nil) {
			t.Errorf("dialogKeys(%q) = %v, want %v", c.pane, got, c.want)
		}
	}
}
