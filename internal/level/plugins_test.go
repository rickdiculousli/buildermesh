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
	ag, _ := filepath.Abs("../../testdata/fake-agy.sh")
	t.Setenv("BMESH_CLAUDE_COMMAND", "/bin/sh "+cl)
	t.Setenv("BMESH_CODEX_COMMAND", "/bin/sh "+cx)
	t.Setenv("BMESH_ANTIGRAVITY_COMMAND", "/bin/sh "+ag)
	t.Setenv("BMESH_ANTIGRAVITY_CONFIG_DIR", filepath.Join(t.TempDir(), "gemini-config"))
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

// What it can't ask counts as read, so a secure box holds: a claude that
// fails to answer reads the checkout, and with no claude on PATH a
// checkout with a marketplace file is read and one without isn't.
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
	t.Setenv("BMESH_CODEX_COMMAND", "")
	t.Setenv("PATH", t.TempDir())
	if LoadsPlugins(ctx, d) {
		t.Fatal("no claude, no marketplace file")
	}
	if !LoadsPlugins(ctx, checkout(t, "mk")) {
		t.Fatal("no claude, a marketplace file")
	}
}

func TestLoadsPluginsAntigravity(t *testing.T) {
	setup(t)
	ctx := context.Background()
	parent := t.TempDir()
	d := filepath.Join(parent, "myrepo")
	pluginsPath := filepath.Join(d, "plugins")
	if err := os.MkdirAll(pluginsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgDir := os.Getenv("BMESH_ANTIGRAVITY_CONFIG_DIR")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		file string
		body string
		want bool
	}{
		{"inside plugins.json", "plugins.json", fmt.Sprintf(`{"entries":[{"path":%q,"include_only":["quota"]}]}`, pluginsPath), true},
		{"inside skills.json", "skills.json", fmt.Sprintf(`{"entries":[{"path":%q}]}`, filepath.Join(d, "skills")), true},
		{"parent include_only matches", "plugins.json", fmt.Sprintf(`{"entries":[{"path":%q,"include_only":["myrepo"]}]}`, parent), true},
		{"parent include_only other", "plugins.json", fmt.Sprintf(`{"entries":[{"path":%q,"include_only":["other"]}]}`, parent), false},
		{"parent exclude matches", "plugins.json", fmt.Sprintf(`{"entries":[{"path":%q,"exclude":["myrepo"]}]}`, parent), false},
		{"bare parent", "plugins.json", fmt.Sprintf(`{"entries":[{"path":%q}]}`, parent), true},
		{"elsewhere", "plugins.json", `{"entries":[{"path":"/elsewhere/plugins"}]}`, false},
		{"malformed fails closed", "plugins.json", `{bad`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Remove(filepath.Join(cfgDir, "plugins.json"))
			os.Remove(filepath.Join(cfgDir, "skills.json"))
			if err := os.WriteFile(filepath.Join(cfgDir, tc.file), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := LoadsPlugins(ctx, d); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	os.Remove(filepath.Join(cfgDir, "plugins.json"))
	os.Remove(filepath.Join(cfgDir, "skills.json"))

	// Snapshot detection reads disk directly and works without `agy` on PATH.
	t.Setenv("BMESH_ANTIGRAVITY_COMMAND", "")
	t.Setenv("BMESH_CLAUDE_COMMAND", "")
	t.Setenv("BMESH_CODEX_COMMAND", "")
	t.Setenv("PATH", t.TempDir())

	qdir := filepath.Join(pluginsPath, "quota")
	if err := os.MkdirAll(qdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qdir, "plugin.json"), []byte(`{"name":"quota","version":"0.2.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	snapDir := filepath.Join(cfgDir, "plugins", "quota")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, "plugin.json"), []byte(`{"name":"quota","version":"0.1.5"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !LoadsPlugins(ctx, d) {
		t.Fatal("enabled snapshot on disk without agy on PATH must hold")
	}
	// Disabled in config.json does not hold.
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"plugins":{"quota":{"enabled":false}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if LoadsPlugins(ctx, d) {
		t.Fatal("disabled snapshot should not hold")
	}
	// Malformed config.json fails closed.
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{bad`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !LoadsPlugins(ctx, d) {
		t.Fatal("malformed config.json must hold")
	}
}

func TestRefreshAntigravitySnapshot(t *testing.T) {
	f := setup(t)
	d := checkout(t, "")
	for name, ver := range map[string]string{"quota": "0.2.0", "same": "1.0.0", "off": "2.0.0"} {
		pdir := filepath.Join(d, "plugins", name)
		if err := os.MkdirAll(pdir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pdir, "plugin.json"), []byte(fmt.Sprintf(`{"name":%q,"version":%q}`, name, ver)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Root buildermesh plugin.json must never be copied back into ~/.gemini/config/plugins/buildermesh.
	if err := os.WriteFile(filepath.Join(d, "plugin.json"), []byte(`{"name":"buildermesh","version":"0.9.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_CODEX_COMMAND", "")
	t.Setenv("BMESH_CLAUDE_COMMAND", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("FAKE_AGY_LIST", `[
		{"name":"quota","version":"0.1.5","enabled":true},
		{"name":"same","version":"1.0.0","enabled":true},
		{"name":"off","version":"1.0.0","enabled":false},
		{"name":"buildermesh","version":"0.8.0","enabled":true},
		{"name":"other","version":"1.0.0","enabled":true}
	]`)
	lines, err := Refresh(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "antigravity quota 0.1.5 -> 0.2.0" {
		t.Fatalf("lines %q", lines)
	}
	got := strings.Join(f.lines(), "|")
	if got != "plugin list --json|plugin install "+filepath.Join(d, "plugins", "quota") {
		t.Fatalf("calls %q", got)
	}
}
