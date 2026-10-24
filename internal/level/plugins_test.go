package level

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakes struct{ log string }

func setup(t *testing.T) fakes {
	t.Helper()
	log := filepath.Join(t.TempDir(), "log")
	t.Setenv("FAKE_PLUGIN_LOG", log)
	cl, _ := filepath.Abs("../../testdata/fake-claude-plugin.sh")
	cx, _ := filepath.Abs("../../testdata/fake-codex.sh")
	t.Setenv("BMESH_CLAUDE_COMMAND", "/bin/sh "+cl)
	t.Setenv("BMESH_CODEX_COMMAND", "/bin/sh "+cx)
	return fakes{log}
}

func (f fakes) lines() []string {
	b, _ := os.ReadFile(f.log)
	s := strings.TrimSpace(string(b))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func checkout(t *testing.T, claudeName string) string {
	dir := t.TempDir()
	if claudeName != "" {
		os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755)
		os.WriteFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"), []byte(`{"name":"`+claudeName+`"}`), 0o644)
	}
	return dir
}

func TestRefreshClaude(t *testing.T) {
	f := setup(t)
	dir := checkout(t, "mk")
	other := t.TempDir()
	list := fmt.Sprintf(`[
{"id":"lag@mk","version":"1","scope":"user","readFromFolder":%q,"folderVersion":"2"},
{"id":"same@mk","version":"3","scope":"user","readFromFolder":%q,"folderVersion":"3"},
{"id":"proj@mk","version":"1","scope":"project","readFromFolder":%q,"folderVersion":"2"},
{"id":"lag@elsewhere","version":"1","scope":"user","readFromFolder":%q,"folderVersion":"2"},
{"id":"far@mk","version":"1","scope":"user","readFromFolder":%q,"folderVersion":"2"}]`,
		dir+"/plugin", dir, dir, dir, other)
	t.Setenv("FAKE_CLAUDE_LIST", list)
	t.Setenv("BMESH_CODEX_COMMAND", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("BMESH_CLAUDE_COMMAND", "/bin/sh "+mustAbs(t, "../../testdata/fake-claude-plugin.sh"))
	lines, err := Refresh(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "claude lag@mk 1 -> 2" {
		t.Fatalf("lines %q", lines)
	}
	got := f.lines()
	want := []string{"plugin list --json", "plugin marketplace update mk", "plugin update lag@mk --scope user"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %q", got)
	}
}

func mustAbs(t *testing.T, p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRefreshCodex(t *testing.T) {
	f := setup(t)
	dir := checkout(t, "")
	t.Setenv("FAKE_CODEX_ADD_VERSION", "0.2")
	t.Setenv("FAKE_CODEX_LIST", fmt.Sprintf(`{"installed":[
{"pluginId":"a@x","version":"0.1","installed":true,"marketplaceSource":{"sourceType":"local","source":%q}},
{"pluginId":"b@x","version":"0.2","installed":true,"marketplaceSource":{"sourceType":"local","source":%q}},
{"pluginId":"c@y","version":"0.1","installed":true,"marketplaceSource":{"sourceType":"local","source":"/elsewhere"}}]}`, dir, dir))
	lines, err := Refresh(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "codex a@x 0.1 -> 0.2|codex b@x 0.2" {
		t.Fatalf("lines %q", lines)
	}
	got := strings.Join(f.lines(), "|")
	if got != "plugin list --json|plugin add a@x --json|plugin add b@x --json" {
		t.Fatalf("calls %q", got)
	}
}

func TestNoCodex(t *testing.T) {
	f := setup(t)
	dir := checkout(t, "")
	t.Setenv("BMESH_CODEX_COMMAND", "")
	t.Setenv("PATH", t.TempDir())
	lines, err := Refresh(context.Background(), dir)
	if lines != nil || err != nil {
		t.Fatalf("%q %v", lines, err)
	}
	if len(f.lines()) != 0 {
		t.Fatalf("calls %q", f.lines())
	}
	if LoadsPlugins(context.Background(), dir) {
		t.Fatal("loads")
	}
}

func TestNothingToRefresh(t *testing.T) {
	f := setup(t)
	dir := checkout(t, "")
	t.Setenv("FAKE_CODEX_LIST", `{"installed":[{"pluginId":"c@y","installed":true,"marketplaceSource":{"source":"/elsewhere"}}]}`)
	lines, err := Refresh(context.Background(), dir)
	if lines != nil || err != nil {
		t.Fatalf("%q %v", lines, err)
	}
	for _, c := range f.lines() {
		if !strings.HasPrefix(c, "plugin list") {
			t.Fatalf("call %q", c)
		}
	}
	if len(f.lines()) != 1 {
		t.Fatalf("calls %q", f.lines())
	}
}

func TestRefreshUpdateFails(t *testing.T) {
	setup(t)
	dir := checkout(t, "mk")
	t.Setenv("BMESH_CODEX_COMMAND", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("BMESH_CLAUDE_COMMAND", "/bin/sh "+mustAbs(t, "../../testdata/fake-claude-plugin.sh"))
	t.Setenv("FAKE_CLAUDE_LIST", fmt.Sprintf(`[{"id":"p@mk","version":"1","scope":"user","readFromFolder":%q,"folderVersion":"2"}]`, dir))
	t.Setenv("FAKE_PLUGIN_FAIL", "update")
	t.Setenv("FAKE_PLUGIN_EXIT", "3")
	_, err := Refresh(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "plugin update p@mk") {
		t.Fatalf("err %v", err)
	}
}

// A checkout sessions load is one a marketplace reads, not one that has a
// marketplace file: the same checkout levels once nothing reads it.
func TestLoadsPlugins(t *testing.T) {
	setup(t)
	ctx := context.Background()
	d := checkout(t, "mk")
	if LoadsPlugins(ctx, d) {
		t.Fatal("a marketplace file nothing reads")
	}
	t.Setenv("FAKE_CLAUDE_MARKETPLACES", fmt.Sprintf(`[{"name":"mk","source":"directory","path":%q}]`, d))
	if !LoadsPlugins(ctx, d) {
		t.Fatal("claude marketplace")
	}
	t.Setenv("FAKE_CLAUDE_MARKETPLACES", fmt.Sprintf(`[{"name":"mk","source":"directory","path":%q}]`, filepath.Join(d, "plugins")))
	if !LoadsPlugins(ctx, d) {
		t.Fatal("claude marketplace inside the checkout")
	}
	t.Setenv("FAKE_CLAUDE_MARKETPLACES", `[{"name":"buildermesh","source":"directory","path":"/elsewhere/plugin"},{"name":"o","source":"github","repo":"o/o"}]`)
	if LoadsPlugins(ctx, d) {
		t.Fatal("read from elsewhere")
	}
	t.Setenv("FAKE_CODEX_LIST", fmt.Sprintf(`{"installed":[{"pluginId":"a@x","installed":true,"marketplaceSource":{"source":%q}}]}`, d))
	if !LoadsPlugins(ctx, d) {
		t.Fatal("codex install")
	}
}

// A claude that fails to answer counts as reading the checkout, so a
// secure box holds; one that isn't on PATH reads nothing.
func TestLoadsPluginsWhenClaudeFails(t *testing.T) {
	setup(t)
	ctx := context.Background()
	d := checkout(t, "")
	t.Setenv("FAKE_PLUGIN_FAIL", "marketplace")
	t.Setenv("FAKE_PLUGIN_EXIT", "1")
	if !LoadsPlugins(ctx, d) {
		t.Fatal("a failing claude")
	}
	t.Setenv("BMESH_CLAUDE_COMMAND", "")
	t.Setenv("PATH", t.TempDir())
	if LoadsPlugins(ctx, d) {
		t.Fatal("no claude")
	}
}
