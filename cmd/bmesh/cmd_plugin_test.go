package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/plugin"
)

// bmesh plugin --install writes the dir and makes it Claude Code's
// buildermesh marketplace, replacing one that reads a checkout; bmesh
// plugin then says it reads it, and a second --install changes nothing.
func TestPluginInstall(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	state := t.TempDir()
	log := filepath.Join(state, "log")
	t.Setenv("FAKE_CLAUDE_STATE", state)
	t.Setenv("FAKE_PLUGIN_LOG", log)
	old := `[{"name":"buildermesh","source":"directory","path":"/home/x/repos/buildermesh"}]`
	if err := os.WriteFile(filepath.Join(state, "marketplaces.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := plugin.Dir(filepath.Join(os.Getenv("XDG_STATE_HOME"), "bmesh"))
	out, _, code := runCLI(t, nil, "plugin")
	if code != 0 || !strings.Contains(out, "dir: "+dir+"\n") || !strings.Contains(out, "written by: none yet") ||
		!strings.Contains(out, "claude: its buildermesh marketplace reads /home/x/repos/buildermesh; bmesh plugin --install moves it here") {
		t.Fatalf("before: code %d out %q", code, out)
	}
	out, errOut, code := runCLI(t, nil, "plugin", "--install")
	want := []string{"plugin: wrote " + dir + " (bmesh ", "plugin: removed marketplace buildermesh (/home/x/repos/buildermesh)\n",
		"plugin: added marketplace buildermesh (" + dir + ")\n", "plugin: installed buildermesh@buildermesh (user scope)\n"}
	for _, w := range want {
		if code != 0 || !strings.Contains(out, w) {
			t.Fatalf("install: code %d out %q err %q, want %q", code, out, errOut, w)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json")); err != nil {
		t.Fatal(err)
	}
	if out, _, _ := runCLI(t, nil, "plugin"); !strings.Contains(out, "claude: reads it (buildermesh@buildermesh, user scope)") ||
		!strings.Contains(out, "written by: bmesh ") {
		t.Fatalf("after: %q", out)
	}
	os.Remove(log)
	if out, errOut, code := runCLI(t, nil, "plugin", "--install"); code != 0 || out != "" {
		t.Fatalf("second run: code %d out %q err %q", code, out, errOut)
	}
	b, _ := os.ReadFile(log)
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if !strings.Contains(l, " list --json") {
			t.Fatalf("second run ran %q", l)
		}
	}
}

// Without Claude Code, --install writes the dir and is refused, saying
// what to install.
func TestPluginInstallWithoutClaude(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("BMESH_CLAUDE_COMMAND", "")
	t.Setenv("PATH", t.TempDir())
	_, errOut, code := runCLI(t, nil, "plugin", "--install")
	if code != exitRefused || !strings.Contains(errOut, "claude is not on PATH: install Claude Code, then run bmesh plugin --install") {
		t.Fatalf("code %d err %q", code, errOut)
	}
	if _, _, ok := plugin.Written(plugin.Dir(filepath.Join(os.Getenv("XDG_STATE_HOME"), "bmesh"))); !ok {
		t.Fatal("no dir written")
	}
}
