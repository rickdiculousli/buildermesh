package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// A registry name may hold capitals, _ and . (a directory name); every node
// path, family and branch made from it uses its token, and node.Repo and
// worktree directories keep the name.
func TestIdeateAndBuildTakeACapitalisedName(t *testing.T) {
	c, repo, _ := buildSetup(t)
	ctx := t.Context()
	const name = "Maple_Market.App"
	writeRepos(t, "[repos.\""+name+"\"]\npath = \""+repo+"\"\nremote = \"example/demo\"\nverify = [\"npm test\"]\nsurfaces = [\"phone / at 390x844\"]\n")

	out, errs, code := runCLI(t, nil, "ideate", name)
	if code != 0 {
		t.Fatalf("ideate: %d %q %q", code, out, errs)
	}
	var round struct {
		Path    string
		Eng, UX childStarted
	}
	if err := json.Unmarshal([]byte(out), &round); err != nil {
		t.Fatalf("%q: %v", out, err)
	}
	if round.Path != "maple-market-app-round-1" || round.Eng.Path != "maple-market-app-round-1.eng" || round.Eng.Branch != "maple-market-app-round-1-eng" {
		t.Fatalf("%+v", round)
	}
	n, ok, err := mesh.GetNode(ctx, c, round.Eng.Path)
	if err != nil || !ok || n.Repo != name || filepath.Base(filepath.Dir(n.Worktree)) != name {
		t.Fatalf("eng node %+v %v %v", n, ok, err)
	}
	if out, errs, code := runCLI(t, nil, "ideate", name, "--force"); code != 0 || !strings.Contains(out, `"path":"maple-market-app-round-2"`) {
		t.Fatalf("second round: %d %q %q", code, out, errs)
	}

	out, errs, code = runCLI(t, nil, "build", name, "002")
	if code != 0 {
		t.Fatalf("build: %d %q %q", code, out, errs)
	}
	var lead buildStarted
	if err := json.Unmarshal([]byte(out), &lead); err != nil {
		t.Fatalf("%q: %v", out, err)
	}
	if lead.Path != "maple-market-app-002" || lead.Family != "maple-market-app-build-1" {
		t.Fatalf("%+v", lead)
	}
	if n, ok, _ := mesh.GetNode(ctx, c, lead.Path); !ok || n.Repo != name {
		t.Fatalf("lead node %+v", n)
	}
	for _, p := range []string{round.Path, round.Eng.Path, round.UX.Path, lead.Path, lead.Family} {
		if _, err := mesh.ParsePath(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestTwoNamesOfOneTokenAreRefused(t *testing.T) {
	rows := map[string]config.Repo{"Foo": {Name: "Foo"}, "bar": {Name: "bar"}}
	if err := tokenClash(rows); err != nil {
		t.Fatal(err)
	}
	rows["foo"] = config.Repo{Name: "foo"}
	err := tokenClash(rows)
	if err == nil || !strings.Contains(err.Error(), "repos Foo and foo are both foo in node paths") {
		t.Fatalf("%v", err)
	}
}
