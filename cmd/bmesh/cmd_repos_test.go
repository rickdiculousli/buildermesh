package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reposSetup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	os.WriteFile(cfg, []byte(""), 0o600)
	os.WriteFile(filepath.Join(dir, "repos.toml"), []byte(`[repos.zeta]
path = "~/repos/zeta"
[repos.app]
path = "~/repos/app"
mode = "fork"
remote = "o/app"
host = "k16"
ux_host = "mac"
verify = ["npm test", "npm run lint"]
surfaces = ["desk / at 1280x800", "phone / at 390x844"]
pin = "v2"
routines = ["verify"]
`), 0o644)
	t.Setenv("BMESH_CONFIG", cfg)
	return cfg
}

func TestReposTableAndJSON(t *testing.T) {
	reposSetup(t)
	out, errs, code := runCLI(t, nil, "repos")
	if code != 0 {
		t.Fatalf("%d %q", code, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "NAME") || !strings.HasPrefix(lines[1], "app") || !strings.HasPrefix(lines[2], "zeta") {
		t.Fatalf("table:\n%s", out)
	}
	for _, want := range []string{"fork", "o/app", "npm test; npm run lint", "k16", "mac", "desk / at 1280x800, phone / at 390x844", "v2"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("row lacks %q: %s", want, lines[1])
		}
	}
	out, _, code = runCLI(t, nil, "repos", "--json")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || code != 0 || len(rows) != 2 {
		t.Fatalf("%v %d %q", err, code, out)
	}
	r := rows[0]
	if r["name"] != "app" || r["path"] != "~/repos/app" || r["mode"] != "fork" || r["pin"] != "v2" || r["ux_host"] != "mac" ||
		len(r["verify"].([]any)) != 2 || len(r["routines"].([]any)) != 1 || len(r["surfaces"].([]any)) != 2 {
		t.Fatalf("row %v", r)
	}
	if z := rows[1]; z["mode"] != "ship" || len(z["routines"].([]any)) != 2 || len(z["verify"].([]any)) != 0 {
		t.Fatalf("defaults %v", z)
	}
}

func TestReposShow(t *testing.T) {
	reposSetup(t)
	out, _, code := runCLI(t, nil, "repos", "show", "app")
	if code != 0 || !strings.Contains(out, "name: app\n") || !strings.Contains(out, "verify: npm test; npm run lint\n") ||
		!strings.Contains(out, "routines: verify\n") {
		t.Fatalf("%d %q", code, out)
	}
	out, _, code = runCLI(t, nil, "repos", "show", "app", "--json")
	var r map[string]any
	if err := json.Unmarshal([]byte(out), &r); err != nil || code != 0 || r["remote"] != "o/app" {
		t.Fatalf("%v %d %q", err, code, out)
	}
	_, errs, code := runCLI(t, nil, "repos", "show", "nope")
	if code != exitRefused || !strings.Contains(errs, `no repo "nope"`) {
		t.Fatalf("%d %q", code, errs)
	}
	if _, _, code := runCLI(t, nil, "repos", "list"); code != exitUsage {
		t.Fatalf("usage %d", code)
	}
}
