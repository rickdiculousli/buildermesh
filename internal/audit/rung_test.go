package audit

import (
	"slices"
	"testing"
)

func TestImplied(t *testing.T) {
	for _, c := range []struct {
		f    Facts
		want Rung
	}{
		{Facts{Leads: true, PushedMain: true, NewSpec: true}, Leads},
		{Facts{Round: true, EndsAsking: true}, Round},
		{Facts{PushedMain: true}, Here},
		{Facts{PushedMain: true, EndsAsking: true}, Here},
		{Facts{PushedMain: true, NewSpec: true}, Answer},
		{Facts{EndsAsking: true}, Plan},
		{Facts{}, Answer},
	} {
		if got, rule := Implied(c.f); got != c.want || rule == "" {
			t.Errorf("Implied(%+v) = %q (%s), want %q", c.f, got, rule, c.want)
		}
	}
	if !EndsAsking("Plan:\n1. x\n\nGo ahead?") || EndsAsking("Why? Because.\n\nThat's all.") {
		t.Fatal("EndsAsking reads the last paragraph")
	}
}

func TestShell(t *testing.T) {
	for _, c := range []struct {
		cmd   string
		calls []string // bmesh calls as cmd sub
		acts  bool     // the first call acts
		push  bool
	}{
		{`cd /w && bmesh --node app-1 started --text "next: bmesh build app 012"`, []string{"started"}, false, false},
		{`bmesh repos 2>/dev/null | head; bmesh mesh-status 2>&1`, []string{"repos", "mesh-status"}, false, false},
		{`bmesh build app 012 014 --context - <<'EOF'
bmesh ideate app
keep the flag
EOF
git status`, []string{"build"}, true, false},
		{`ssh -o BatchMode=yes mac "bash -lc 'bmesh lead start app 012'"`, []string{"lead start"}, true, false},
		{`TMPDIR=/x timeout 60 bmesh routine run verify`, []string{"routine run"}, true, false},
		{`bmesh spec steps 012 --check`, []string{"spec steps"}, true, false},
		{`bmesh spec list --repo app # bmesh build app 1`, []string{"spec list"}, false, false},
		{`bmesh release app --dry-run && bmesh scaffold app --check`, []string{"release", "scaffold"}, false, false},
		{`bmesh build --help`, []string{"build"}, false, false},
		{`git fetch origin && git rebase origin/main && git push origin HEAD:main`, nil, false, true},
		{`git -C /repo push origin main`, nil, false, true},
		{`git push origin HEAD`, nil, false, false},
		{`git push -u origin feature:main-fix`, nil, false, false},
		{`echo "git push origin main"`, nil, false, false},
	} {
		var got []string
		calls := bmeshCalls(c.cmd)
		for _, k := range calls {
			got = append(got, k.String())
		}
		if !slices.Equal(got, c.calls) {
			t.Errorf("%q: calls %q, want %q", c.cmd, got, c.calls)
		}
		if len(calls) > 0 && calls[0].acts() != c.acts {
			t.Errorf("%q: acts %v, want %v", c.cmd, calls[0].acts(), c.acts)
		}
		if pushesMain(c.cmd) != c.push {
			t.Errorf("%q: pushes main %v, want %v", c.cmd, !c.push, c.push)
		}
	}
	for cmd, want := range map[string]string{
		"cd /w && cat docs/x.md":      "docs/x.md whole",
		"sed -n '10,40p' a/b.go":      "a/b.go lines 10-40",
		"head -n 20 README.md":        "README.md head -n 20",
		"cat a b":                     "",
		"cat a; ls":                   "",
		"grep -n x a.go":              "",
		"bmesh spec template > docs/": "",
	} {
		f, r, ok := fileRead(cmd)
		if got := f + " " + r; ok != (want != "") || ok && got != want {
			t.Errorf("fileRead(%q) = %q %v, want %q", cmd, got, ok, want)
		}
	}
	if !newSpec("bmesh spec template > docs/proposals/015-x.md") || newSpec("cat docs/proposals/015-x.md") {
		t.Fatal("newSpec reads a write through >")
	}
}
