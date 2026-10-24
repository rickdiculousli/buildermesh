package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh"
	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/scaffold"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// These tests never run claude or reach the network: the run test drives a
// shell fake in its place.

func need(t *testing.T, tools ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake bmesh is a POSIX shell script")
	}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s on PATH", tool)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := git(".", nil, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Skip("not in a git checkout")
	}
	return root
}

func testFixture(t *testing.T) string {
	t.Helper()
	d, err := fixtureDir()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Every case's good references pass and its bad ones fail the expectation
// they name: the scorers are proven before any session is paid for.
func TestReferences(t *testing.T) {
	need(t, "git", "go", "sh")
	fixture := testFixture(t)
	for _, k := range cases {
		t.Run(k.Name, func(t *testing.T) {
			t.Parallel()
			good, bad := 0, 0
			for _, ref := range k.Refs {
				if ref.Fails == "" {
					good++
				} else {
					bad++
				}
				if err := checkRef(t.TempDir(), k, ref, fixture); err != nil {
					t.Errorf("%s: %v", ref.Name, err)
				}
			}
			if good == 0 || bad == 0 {
				t.Errorf("%d good and %d bad references; a case needs both", good, bad)
			}
		})
	}
}

var update = flag.Bool("update", false, "rewrite the fixture as bmesh scaffold writes it")

// The fixture is a shop as bmesh scaffold leaves a ship repo: its AGENTS.md
// blocks and routine kinds as scaffold writes them, a ledger of three
// proposed specs, and tests that pass with its bugs in. A change to what
// scaffold writes is taken in with -update.
func TestFixture(t *testing.T) {
	need(t, "go")
	fixture := testFixture(t)
	r := config.Repo{Name: repoName, Mode: "ship", Remote: "origin", Verify: []string{"go test ./..."}, Routines: []string{"verify", "review"}}
	targets, _, err := scaffold.Targets(fixture, r, buildermesh.AgentsSnippet)
	if err != nil {
		t.Fatal(err)
	}
	if plan := scaffold.Plan(targets); plan != "" && *update {
		if _, err := scaffold.Write(fixture, targets); err != nil {
			t.Fatal(err)
		}
	} else if plan != "" {
		t.Errorf("the fixture is not as bmesh scaffold writes it (go test ./scripts/skilltest -run TestFixture -update takes it in):\n%s", plan)
	}
	specs, err := spec.Scan(fixture, "docs/proposals/{id}-*.md")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range specs {
		got = append(got, s.IDString()+" "+s.Status())
	}
	if want := []string{"012 proposed", "013 proposed", "014 proposed"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ledger %v, want %v", got, want)
	}
	code, out, errs := run(fixture, 5*time.Minute, append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off", "GOWORK=off", "GOFLAGS="), "go", "test", "./...")
	if code != 0 {
		t.Errorf("the fixture's tests fail:\n%s%s", out, errs)
	}
}

// The commands an arm's fake knows are its manual's headings, and this
// repo's manual names every command its binary dispatches.
func TestCommandsFromManual(t *testing.T) {
	got := commandsFromManual("# x\n### `bmesh foo` y\n#### `bmesh sub`\n### `bmesh a`, `b`, `c-d`\ntext `bmesh e`\n### Not a command\n")
	if want := []string{"a", "b", "c-d", "foo", "help", "hosts", "version"}; !reflect.DeepEqual(got, want) {
		t.Errorf("commands %v, want %v", got, want)
	}
	root := repoRoot(t)
	manual, err := os.ReadFile(filepath.Join(root, "docs", "manual.md"))
	if err != nil {
		t.Fatal(err)
	}
	main, err := os.ReadFile(filepath.Join(root, "cmd", "bmesh", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(main)[strings.Index(string(main), "func (a *app) dispatch("):]
	body = body[:strings.Index(body, "\n}\n")]
	want := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\tcase (".*"):$`).FindAllStringSubmatch(body, -1) {
		for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(m[1], -1) {
			want[q[1]] = true
		}
	}
	var wantList []string
	for c := range want {
		wantList = append(wantList, c)
	}
	got = commandsFromManual(string(manual))
	gotSet := map[string]bool{}
	for _, c := range got {
		gotSet[c] = true
	}
	for c := range want {
		if !gotSet[c] {
			t.Errorf("bmesh dispatches %s, but the manual has no heading for it", c)
		}
	}
	for _, c := range got {
		if !want[c] {
			t.Errorf("the manual heads %s, which bmesh doesn't dispatch", c)
		}
	}
	if len(wantList) < 30 {
		t.Errorf("read only %d commands from dispatch", len(wantList))
	}
}

// An arm from a ref is the plugin extracted from that commit; a directory
// is used in place.
func TestArms(t *testing.T) {
	need(t, "git")
	root := repoRoot(t)
	out := t.TempDir()
	a, err := prepareArm("base", "HEAD", root, out)
	if err != nil {
		t.Fatal(err)
	}
	head, _ := git(root, nil, "rev-parse", "HEAD")
	if a.Commit != head || a.Dir != filepath.Join(out, "arms", "base", "plugin") {
		t.Errorf("arm %+v", a)
	}
	for _, f := range []string{".claude-plugin/plugin.json", "skills/quickfix/SKILL.md", "hooks/hooks.json", "docs/manual.md"} {
		if _, err := os.Stat(filepath.Join(a.Dir, f)); err != nil {
			t.Errorf("the extracted arm has no %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(a.Dir, "cmd")); err == nil {
		t.Error("the extracted arm holds more than the plugin and the manual")
	}
	d, err := prepareArm("cand", root, root, out)
	if err != nil {
		t.Fatal(err)
	}
	if d.Dir != root || d.Commit != "" || !reflect.DeepEqual(d.Commands, a.Commands) {
		t.Errorf("dir arm %+v", d)
	}
	if _, err := prepareArm("x", "no-such-ref-at-all", root, out); err == nil {
		t.Error("an arm from a missing ref was made")
	}
	if _, _, err := parseArm("noequals"); err == nil {
		t.Error("an --arm with no name was taken")
	}
}

// fakeCell is a cell set up for the bug case on an arm of this repo.
func fakeCell(t *testing.T) (string, string) {
	t.Helper()
	need(t, "git", "go", "sh")
	root := repoRoot(t)
	manual, err := os.ReadFile(filepath.Join(root, "docs", "manual.md"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	arm := Arm{Name: "test", Dir: root, Commands: commandsFromManual(string(manual))}
	base, err := setupCell(dir, findCase("bug"), arm, testFixture(t), thisBox(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return dir, base
}

func fake(t *testing.T, dir, cwd string, args ...string) (int, string, string) {
	t.Helper()
	env := append(os.Environ(), "SKILLTEST_CELL="+dir, "SKILLTEST_LOG="+filepath.Join(dir, logFile))
	env = append(env, gitEnv(dir)...)
	return run(cwd, 3*time.Minute, env, filepath.Join(dir, "bin", "bmesh"), args...)
}

// The fake answers what the skills ask, logs every call, starts nothing,
// and refuses a command the arm's binary lacks as that binary would.
func TestFake(t *testing.T) {
	dir, base := fakeCell(t)
	sandbox := filepath.Join(dir, sandboxDir)
	host := thisBox()
	expect := func(want string, code int, args ...string) string {
		t.Helper()
		got, stdout, stderr := fake(t, dir, sandbox, args...)
		if got != code || !strings.Contains(stdout+stderr, want) {
			t.Errorf("bmesh %s: exit %d, want %d, and %q, want it to hold %q", strings.Join(args, " "), got, code, stdout+stderr, want)
		}
		return stdout
	}
	expect("path: "+sandbox, 0, "repos", "show", "shop")
	expect("host: "+host, 0, "repos", "show", "shop")
	var r config.Repo
	if err := json.Unmarshal([]byte(expect(`"mode":"ship"`, 0, "repos", "show", "shop", "--json")), &r); err != nil || r.Path != sandbox || r.Verify[0] != "go test ./..." {
		t.Errorf("repos show --json: %+v, %v", r, err)
	}
	expect("shop", 0, "--json", "repos")
	expect(`no repo "other"`, 3, "repos", "show", "other")
	expect("015", 0, "spec", "next-id")
	expect("016", 0, "spec", "next-id", "--repo", "shop")
	expect("017", 0, "spec", "next-id", "--peek")
	expect("017-019", 0, "spec", "next-id", "--count", "3")
	expect("id: {{id}}", 0, "spec", "template")
	if out := expect("014  proposed", 0, "spec", "list", "--repo", "shop"); strings.Count(out, "\n") != 3 {
		t.Errorf("spec list:\n%s", out)
	}
	expect("013  proposed", 0, "spec", "list", "--status", "proposed")
	expect("is not one of", 2, "spec", "list", "--status", "bogus")
	expect("013  rejected   S   feature", 0, "spec", "set", "--id", "13", "--status", "rejected")
	if out := expect("landed ", 0, "spec", "set", "--id", "13,14", "--status", "later", "--land"); !strings.Contains(out, "014  later") || !strings.HasSuffix(out, " on main: Specs 013, 014: later\n") {
		t.Errorf("spec set --land:\n%s", out)
	}
	expect("no spec 099", 1, "spec", "set", "--id", "13,99", "--status", "later")
	expect("PATH  ROLE  STATE  SINCE  RUNNER  UNREAD", 0, "mesh-status")
	expect("[]", 0, "mesh-status", "--json")
	expect("idle", 0, "thread-status", "shop-012")
	expect("no node for this directory", 2, "thread-status")
	expect("inbox: empty", 0, "--node", "operator", "inbox")
	expect("this box is "+host, 0, "hosts", "--here")
	if _, out, errs := fake(t, dir, sandbox, "update", "--notice"); out+errs != "" {
		t.Errorf("update --notice printed %q", out+errs)
	}
	var b struct{ Path, Spec, Family, Building string }
	if err := json.Unmarshal([]byte(expect(`"path":"shop-012"`, 0, "build", "shop", "012", "--model", "opus")), &b); err != nil ||
		b.Spec != "012" || b.Family != "shop-build-1" || len(b.Building) != 40 {
		t.Errorf("build: %+v, %v", b, err)
	}
	expect("no accepted specs in shop", 0, "build", "--accepted", "shop")
	if out := expect(`"ux":`, 0, "ideate", "shop"); !strings.Contains(out, `"path":"shop-round-1"`) {
		t.Errorf("ideate: %s", out)
	}
	if out := expect(`"eng":`, 0, "ideate", "shop", "--idea", "--context", "-"); strings.Contains(out, `"ux"`) || !strings.Contains(out, "shop-round-2") {
		t.Errorf("ideate --idea: %s", out)
	}
	expect("released shop v0.1.0 at "+base[:8]+" (no specs)", 0, "release", "shop")
	expect("--dry-run tags nothing", 0, "release", "shop", "--dry-run")
	expect("sent ", 0, "--node", "operator", "send", "shop-012", "hi")
	expect("stopped shop-012", 0, "--node", "operator", "node", "stop", "shop-012", "--text", "why")
	// This tree's manual has no choose-ideas, so its arm's fake refuses it as the binary does.
	expect("unknown command \"choose-ideas\" (run `bmesh help`)", 2, "choose-ideas", "shop", "1 yes")
	expect("unknown command \"frobnicate\" (run `bmesh help`)", 2, "frobnicate")
	expect("unknown command \"frobnicate\"", 2, "--node", "operator", "frobnicate")

	tree, _ := git(sandbox, gitEnv(dir), "rev-parse", "HEAD^{tree}")
	expect("verify ok t:"+tree[:8]+" (recorded)", 0, "routine", "run", "verify")
	if err := os.WriteFile(filepath.Join(sandbox, "cart", "cart.go"), []byte("package cart\n\nfunc broken() int { return \"no\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	expect("worktree is dirty; commit first", 3, "routine", "run", "verify")
	if _, err := git(sandbox, gitEnv(dir), "commit", "-qam", "break it"); err != nil {
		t.Fatal(err)
	}
	tree, _ = git(sandbox, gitEnv(dir), "rev-parse", "HEAD^{tree}")
	expect("verify fail t:"+tree[:8], 1, "routine", "run", "verify")
	expect("kind review is pooled", 3, "routine", "run", "review")

	// Every call is logged with its cwd, its arguments whole.
	odd := "a\tb\nc \\ d"
	fake(t, dir, sandbox, "send", "shop-012", odd)
	calls, err := readCalls(filepath.Join(dir, logFile))
	if err != nil {
		t.Fatal(err)
	}
	last := calls[len(calls)-1]
	if last.Cwd != sandbox || !reflect.DeepEqual(last.Args, []string{"send", "shop-012", odd}) {
		t.Errorf("last call %+v", last)
	}
	if calls[0].String() != "repos show shop" || !calls[1].Is("repos", "show") {
		t.Errorf("first calls %v, %v", calls[0], calls[1])
	}

	// The stubs keep the tools that reach real things out, and say so.
	code, _, errs := run(sandbox, time.Minute, append(os.Environ(), "SKILLTEST_CELL="+dir), filepath.Join(dir, "bin", "claude"), "-p", "hi")
	if code != 127 || !strings.Contains(errs, "claude: not available in this sandbox") {
		t.Errorf("the claude stub: exit %d, %q", code, errs)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, blockedLog)); !strings.Contains(string(b), "claude\t-p hi") {
		t.Errorf("blocked.log: %q", b)
	}
}

// The shell a session runs its commands in finds the fake bmesh and the
// stubs first, whatever this box's shell profile puts on PATH: the cell's
// home has no profile, and its PATH nothing of this box's home but Go.
func TestSessionShell(t *testing.T) {
	need(t, "bash")
	dir, _ := fakeCell(t)
	if err := os.MkdirAll(filepath.Join(dir, "home"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := findHost()
	env := sessionEnv(os.Environ(), dir, h)
	bin := filepath.Join(dir, "bin")
	for _, mode := range [][]string{{"-l", "-c"}, {"-i", "-c"}, {"-c"}} {
		code, out, errs := run(dir, time.Minute, env, "bash", append(mode, `command -v bmesh claude ssh tmux go git; echo "$HOME"; echo "$PATH"`)...)
		got := strings.Fields(out)
		if code != 0 || len(got) != 8 {
			t.Fatalf("bash %v: exit %d: %s%s", mode, code, out, errs)
		}
		for i, tool := range []string{"bmesh", "claude", "ssh", "tmux"} {
			if got[i] != filepath.Join(bin, tool) {
				t.Errorf("bash %v finds %s at %s", mode, tool, got[i])
			}
		}
		if under(got[4], dir) || under(got[5], dir) {
			t.Errorf("bash %v finds go at %s and git at %s", mode, got[4], got[5])
		}
		if got[6] != filepath.Join(dir, "home") {
			t.Errorf("bash %v has HOME %s", mode, got[6])
		}
		for _, p := range filepath.SplitList(got[7]) {
			if under(p, h.Home) && !under(p, dir) && !slices.Contains(h.Tools, p) {
				t.Errorf("bash %v has %s of this box's home on PATH", mode, p)
			}
		}
	}
}

func TestAsksOperator(t *testing.T) {
	for reply, want := range map[string]bool{
		"Found it in cart.Total. Want me to fix it?":                       true,
		"This is L.\n\n1. a\n2. b\n\nShall I start on it?":                 true,
		"Plan above. Say go and I'll start it.":                            true,
		"Two ways: a spec, or here with a high-effort review; your call.":  true,
		"Fixed and pushed; origin/main contains it. Nothing waits on you.": false,
		"Is it negative? Yes.\n\nFixed and pushed.":                        false,
		"Done: 1a2b3c4d on main.":                                          false,
		"":                                                                 false,
	} {
		if got := asksOperator(reply); got != want {
			t.Errorf("asksOperator(%q) = %v, want %v", reply, got, want)
		}
	}
}

func TestVerdicts(t *testing.T) {
	acc, rej, later := "accepted", "rejected", "later"
	for _, c := range []struct {
		args []string
		want map[string]string
	}{
		{[]string{"shop", "1 and 3 yes, 2 no, it duplicates the --verbose flag"}, map[string]string{"012": acc, "013": rej, "014": acc}},
		{[]string{"shop", "accept 012 and 014, reject 013: it duplicates --verbose"}, map[string]string{"012": acc, "013": rej, "014": acc}},
		{[]string{"shop", "012 yes and 013 no", "and 014 later"}, map[string]string{"012": acc, "013": rej, "014": later}},
		{[]string{"shop", "reject 2 and 3 yes"}, map[string]string{"013": rej, "014": acc}},
		{[]string{"shop", "4 no because 5 covers it"}, map[string]string{"004": rej}},
		{[]string{"shop", "1, 2 and 3 later"}, map[string]string{"012": later, "013": later, "014": later}},
		{[]string{"shop", "all yes"}, map[string]string{"012": acc, "013": acc, "014": acc}},
		{[]string{"shop", "2 smaller"}, map[string]string{"013": "adjusting"}},
		{[]string{"shop", "--accept", "012,014", "--reject", "013=it duplicates --verbose"}, map[string]string{"012": acc, "013": rej, "014": acc}},
		{[]string{"shop", "--accept=012", "--later=14", "--adjust", "013=smaller"}, map[string]string{"012": acc, "013": "adjusting", "014": later}},
	} {
		if got := pickPitch.verdicts(c.args); !reflect.DeepEqual(got, c.want) {
			t.Errorf("verdicts(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestReadTurn(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	one := `{"type":"result","result":"hi","session_id":"s1","num_turns":3,"total_cost_usd":0.5,"usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}`
	r, init, err := readTurn(write("one.json", one))
	if sp := turnSpend(r, nil); err != nil || init != nil || r.Result != "hi" || r.SessionID != "s1" || sp.Tokens() != 10 || sp.CostUSD != 0.5 {
		t.Errorf("one object: %+v, %v, %v", r, init, err)
	}
	stream := `{"type":"system","subtype":"init","plugins":[{"name":"buildermesh","path":"/p"}],"skills":["buildermesh:quickfix","simplify"],"mcp_servers":[]}` + "\n" +
		"not json\n" + `{"type":"assistant","message":{}}` + "\n" +
		`{"type":"result","result":"last","total_cost_usd":0.3,"modelUsage":{"a":{"inputTokens":1,"outputTokens":1,"costUSD":0.1},"b":{"cacheReadInputTokens":5,"costUSD":0.2}}}` + "\n"
	r, init, err = readTurn(write("stream.json", stream))
	if err != nil || r.Result != "last" || turnSpend(r, nil).Tokens() != 7 || init == nil ||
		!reflect.DeepEqual(names(init.Plugins), []string{"buildermesh"}) || len(names(init.Skills)) != 2 || len(init.MCPServers) != 0 {
		t.Errorf("stream: %+v, %+v, %v", r, init, err)
	}
	// A resumed session's result states its own spend in a stream; in a
	// json run its totals run on from the turn before, which come off.
	prev := r
	since := `{"type":"result","total_cost_usd":0.9,"modelUsage":{"a":{"inputTokens":50}},"model_usage_since_last_result":{"a":{"inputTokens":2,"costUSD":0.25}}}`
	r, _, _ = readTurn(write("since.json", since))
	if sp := turnSpend(r, &prev); sp.Tokens() != 2 || sp.CostUSD != 0.25 {
		t.Errorf("since the last result: %+v", sp)
	}
	ranOn := `{"type":"result","total_cost_usd":0.45,"modelUsage":{"a":{"inputTokens":4,"outputTokens":1},"b":{"cacheReadInputTokens":5}}}`
	r, _, _ = readTurn(write("ranon.json", ranOn))
	if sp := turnSpend(r, &prev); sp.Tokens() != 3 || sp.CostUSD < 0.1499 || sp.CostUSD > 0.1501 {
		t.Errorf("running totals: %+v", sp)
	}
	fresh := `{"type":"result","total_cost_usd":0.05,"modelUsage":{"a":{"inputTokens":1}}}`
	r, _, _ = readTurn(write("fresh.json", fresh))
	if sp := turnSpend(r, &prev); sp.Tokens() != 1 || sp.CostUSD != 0.05 {
		t.Errorf("totals that started again: %+v", sp)
	}
	if _, _, err := readTurn(write("none.json", "")); err == nil {
		t.Error("an empty output read as a result")
	}
}

// A cell's scratch is never /tmp and never under instructions a session
// would load.
func TestScratchRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the /tmp rule is for Unix boxes")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", filepath.Join(dir, "tmp"))
	if _, err := scratchRoot("x"); err == nil || !strings.Contains(err.Error(), "AGENTS.md") {
		t.Errorf("a scratch under an AGENTS.md was taken: %v", err)
	}
	if left, _ := os.ReadDir(filepath.Join(dir, "tmp")); len(left) > 0 {
		t.Errorf("a refused scratch was left behind: %v", left)
	}
	t.Setenv("TMPDIR", "/tmp")
	if _, err := scratchRoot("x"); err == nil || !strings.Contains(err.Error(), "/tmp") {
		t.Errorf("/tmp was taken: %v", err)
	}
}

const fakeClaude = `#!/bin/sh
# A fake claude: the first turn looks the repo up, and a sandboxed tool, and
# asks; the steer makes the fix, verifies it with the fake bmesh and pushes.
n=$(( $(cat "$FAKE_CLAUDE_DIR/n" 2>/dev/null || echo 0) + 1 ))
echo "$n" >"$FAKE_CLAUDE_DIR/n"
printf '%s\n' "$*" >>"$FAKE_CLAUDE_DIR/calls"
env >"$FAKE_CLAUDE_DIR/env.$n"
pwd >"$FAKE_CLAUDE_DIR/cwd.$n"
if [ "$n" = 1 ]; then
	bmesh hosts --here >/dev/null
	bmesh repos show shop >/dev/null
	gh pr list >/dev/null 2>&1
	printf '{"type":"system","subtype":"hook_started"}\n'
	printf '{"type":"system","subtype":"init","session_id":"fake-session","plugins":[{"name":"buildermesh","path":"%s"}],"skills":["buildermesh:quickfix","buildermesh:build","simplify"],"tools":["Bash","Task","Monitor"],"mcp_servers":[]}\n' "$PWD"
	printf '{"type":"result","subtype":"success","is_error":false,"result":"The total goes below zero in cart.Total. Want me to fix it?","session_id":"fake-session","num_turns":3,"duration_ms":1000,"total_cost_usd":0.01,"modelUsage":{"claude-opus-5-5":{"inputTokens":10,"outputTokens":5,"cacheReadInputTokens":100,"cacheCreationInputTokens":20,"costUSD":0.01}}}\n'
	exit 0
fi
git worktree add -q "$FAKE_CLAUDE_DIR/wt" -b scratch HEAD
sed 's/return c.Subtotal() - c.Discount()/return max(0, c.Subtotal()-c.Discount())/' cart/cart.go >cart/cart.go.new && mv cart/cart.go.new cart/cart.go
git commit -qam "An empty cart with a code totals 0.00" && bmesh routine run verify >/dev/null && git push -q origin HEAD:main
printf '{"type":"result","subtype":"success","is_error":false,"result":"Fixed and pushed.","session_id":"fake-session","num_turns":7,"duration_ms":2000,"total_cost_usd":0.03,"modelUsage":{"claude-opus-5-5":{"inputTokens":11,"outputTokens":7,"cacheReadInputTokens":103,"cacheCreationInputTokens":24,"costUSD":0.025},"claude-haiku-4-5":{"inputTokens":10,"costUSD":0.005}},"model_usage_since_last_result":{"claude-opus-5-5":{"inputTokens":1,"outputTokens":2,"cacheReadInputTokens":3,"cacheCreationInputTokens":4,"costUSD":0.015},"claude-haiku-4-5":{"inputTokens":10,"costUSD":0.005}}}\n'
`

// A cell runs end to end against a fake claude: each turn's command line
// and environment, the steer when the session asks, the transcript-free
// keep, the archive, the move into the out dir, the row and a rescore.
func TestRun(t *testing.T) {
	need(t, "git", "go", "sh")
	root := repoRoot(t)
	fixture := testFixture(t)
	arm, err := prepareArm("cand", root, root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	claude := filepath.Join(tools, "claude")
	if err := os.WriteFile(claude, []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	parent := append(os.Environ(), "FAKE_CLAUDE_DIR="+tools, "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=parent", "CLAUDE_CONFIG_DIR=/keep/me",
		"T3_SERVICE_LAUNCHER_CONTEXT={}", "BMESH_NODE=shop-012", "GIT_DIR=/nowhere", "SSH_AUTH_SOCK=/agent", "AI_AGENT=x",
		"TMUX=/tmp/tmux-1000/default,1,0", "TMUX_PANE=%1")
	host := findHost()
	host.Home = t.TempDir()
	if err := os.Mkdir(filepath.Join(host.Home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &runner{claude: claude, model: "claude-opus-5-5", timeout: 3 * time.Minute, projects: t.TempDir(), env: parent, host: host}
	out := t.TempDir()
	row, err := r.runCell(context.Background(), cellPlan{n: 1, k: findCase("bug"), arm: arm, run: 1}, fixture, t.TempDir(), out)
	if err != nil {
		t.Fatal(err)
	}
	if !row.Pass || row.Steers != 1 || row.OperatorTurns != 2 || row.Commits != 1 || row.Turns != 10 || !row.PluginHook {
		t.Errorf("row %+v", row)
	}
	if row.Tokens != 135+20 || row.CostUSD < 0.0299 || row.CostUSD > 0.0301 || row.DurationMS != 3000 {
		t.Errorf("usage %+v", row)
	}
	if !reflect.DeepEqual(row.Plugins, []string{"buildermesh"}) || len(row.MCPServers) != 0 || len(row.Reaching) != 0 ||
		!reflect.DeepEqual(row.Available, []string{"buildermesh:quickfix", "buildermesh:build"}) {
		t.Errorf("init: plugins %v, MCP servers %v, reaching tools %v, skills %v", row.Plugins, row.MCPServers, row.Reaching, row.Available)
	}
	if !reflect.DeepEqual(row.Commands, []string{"repos show shop", "routine run verify"}) || len(row.Blocked) != 1 || row.Blocked[0] != "gh pr list" {
		t.Errorf("calls %v, blocked %v", row.Commands, row.Blocked)
	}
	calls, _ := os.ReadFile(filepath.Join(tools, "calls"))
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	words := findCase("bug").Turns[0]
	want := "-p " + words + " --model claude-opus-5-5 --output-format stream-json --verbose --permission-mode bypassPermissions --setting-sources project,local --strict-mcp-config --disallowedTools " + strings.Join(deniedTools, ",") + " --plugin-dir " + root
	if len(lines) != 2 || lines[0] != want || lines[1] != strings.Replace(want, words, goAhead+" --resume fake-session", 1) {
		t.Errorf("claude was run as:\n%s\nwant first:\n%s", calls, want)
	}
	env, _ := os.ReadFile(filepath.Join(tools, "env.1"))
	cell := filepath.Join(out, "cells", "bug__cand__1")
	for _, bad := range []string{"CLAUDECODE=", "CLAUDE_CODE_SESSION_ID=", "T3_SERVICE_LAUNCHER_CONTEXT=", "BMESH_NODE=", "GIT_DIR=", "SSH_AUTH_SOCK=", "AI_AGENT=", "TMUX=", "TMUX_PANE="} {
		if regexp.MustCompile(`(?m)^` + bad).Match(env) {
			t.Errorf("the session's environment kept %s", bad)
		}
	}
	for _, good := range []string{"CLAUDE_CONFIG_DIR=/keep/me", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1", "GOPROXY=off", "BMESH_CONFIG=", "HOME="} {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(good)).Match(env) {
			t.Errorf("the session's environment has no %s", good)
		}
	}
	if tm, tmp := regexp.MustCompile(`(?m)^TMUX_TMPDIR=(.+)$`).FindSubmatch(env), regexp.MustCompile(`(?m)^TMPDIR=(.+)$`).FindSubmatch(env); tm == nil || tmp == nil || string(tm[1]) != string(tmp[1]) {
		t.Errorf("tmux's dir isn't the session's TMPDIR: %q, %q", tm, tmp)
	}
	if m := regexp.MustCompile(`(?m)^PATH=([^:\n]*)`).FindSubmatch(env); m == nil || filepath.Base(string(m[1])) != "bin" {
		t.Errorf("PATH doesn't start with the cell's bin: %s", m)
	}
	if cwd, _ := os.ReadFile(filepath.Join(tools, "cwd.1")); filepath.Base(strings.TrimSpace(string(cwd))) != sandboxDir {
		t.Errorf("the session ran in %s", cwd)
	}
	for _, f := range []string{cellFile, logFile, workTar, "turns/01.json", "turns/02.json", originDir} {
		if _, err := os.Stat(filepath.Join(cell, f)); err != nil {
			t.Errorf("the kept cell has no %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(cell, sandboxDir)); err == nil {
		t.Error("the kept cell holds the sandbox's tree, not its archive")
	}
	if _, err := os.Stat(filepath.Join(host.Home, ".claude")); err != nil {
		t.Error("keeping the cell removed the claude config its home linked to")
	}
	if c, err := loadCell(cell); err != nil || !reflect.DeepEqual(c.Left, []string{filepath.Join(tools, "wt")}) {
		t.Errorf("the worktree made outside the cell: %v, %v", c, err)
	}
	if _, err := os.Stat(filepath.Join(tools, "wt")); err == nil {
		t.Error("the worktree made outside the cell is still there")
	}
	if err := writeResults(out, Results{Date: "test", Model: r.model, Arms: []Arm{arm}, Rows: []Row{row}}); err != nil {
		t.Fatal(err)
	}
	res, err := rescore(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || !reflect.DeepEqual(res.Rows[0], row) || !res.Rescored {
		t.Errorf("rescored %+v\nwas %+v", res.Rows, row)
	}
	md, _ := os.ReadFile(filepath.Join(out, "summary.md"))
	if !strings.Contains(string(md), "| bug | cand | 1 | 1/1 | 1 | 1 | $0.03 |") || !strings.Contains(string(md), "| cand | 1 | 100% | 1 | 1 |") {
		t.Errorf("summary.md:\n%s", md)
	}
	// A cell whose turn failed passes nothing, whatever the state it left.
	c, err := loadCell(cell)
	if err != nil {
		t.Fatal(err)
	}
	c.Error = "claude: error_during_execution usage limit reached"
	if err := c.save(cell); err != nil {
		t.Fatal(err)
	}
	if failed, err := scoreCell(cell, findCase("bug")); err != nil || failed.Pass {
		t.Errorf("a cell whose turn failed scored pass %v, %v", failed.Pass, err)
	}
}

// A turn past its timeout is killed with its whole process group.
func TestTurnTimeout(t *testing.T) {
	dir, _ := fakeCell(t)
	slow := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(slow, []byte("#!/bin/sh\necho '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"s0\"}'\nsleep 60 &\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &runner{claude: slow, model: "m", timeout: time.Second, env: os.Environ()}
	c := &Cell{Plugin: dir}
	start := time.Now()
	err := r.turn(context.Background(), dir, c, "hi", false)
	if err == nil || !strings.Contains(err.Error(), "killed after 1s") || time.Since(start) > 20*time.Second {
		t.Fatalf("turn: %v after %s", err, time.Since(start))
	}
	if len(c.Turns) != 1 || c.Turns[0].Error == "" || c.Turns[0].Session != "s0" {
		t.Errorf("turns %+v", c.Turns)
	}
}

// What a session left running in the background ends with its turn.
func TestTurnEndsItsGroup(t *testing.T) {
	dir, _ := fakeCell(t)
	tools := t.TempDir()
	pid := filepath.Join(tools, "pid")
	quick := filepath.Join(tools, "claude")
	script := "#!/bin/sh\nsleep 60 &\necho $! > " + pid + "\necho '{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"ok\",\"session_id\":\"s1\"}'\n"
	if err := os.WriteFile(quick, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &runner{claude: quick, model: "m", timeout: time.Minute, env: os.Environ()}
	if err := r.turn(context.Background(), dir, &Cell{Plugin: dir}, "hi", false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(pid)
	if err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(10 * time.Second); exec.Command("kill", "-0", strings.TrimSpace(string(b))).Run() == nil; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("the session's background sleep %s outlived its turn", b)
		}
	}
}
