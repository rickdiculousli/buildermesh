package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// thisManual is this tree's manual, whose commands an arm of it knows.
func thisManual(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "manual.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The fake's feedback queue: list prints the canned issues still pending,
// in the binary's formats; --repo leaves out what a fix commit or a spec on
// origin's main names, and both leave out what was drained; drain --seq
// deletes exactly those and says which were gone.
func TestFakeFeedback(t *testing.T) {
	manual := thisManual(t)
	dir, _ := caseCell(t, "fix", manual)
	wt := filepath.Join(dir, findCase("fix").Lead.worktree())
	expect := func(want string, args ...string) string {
		t.Helper()
		got, stdout, stderr := fake(t, dir, wt, args...)
		if got != 0 || stdout != want {
			t.Errorf("bmesh %s: exit %d, %q%s, want %q", strings.Join(args, " "), got, stdout, stderr, want)
		}
		return stdout
	}
	line := func(seqs ...int) string {
		var b strings.Builder
		for _, i := range fixIssues {
			for _, s := range seqs {
				if i.Seq == s {
					b.WriteString(i.line(true))
				}
			}
		}
		return b.String()
	}
	all := expect(line(41, 42, 43, 44), "feedback", "list", "--repo", "shop")
	if !strings.HasPrefix(all, "41 shop-013 (shop 013, family shop-build-2, where cart/cart.go): an empty cart") {
		t.Errorf("list --repo isn't the binary's format:\n%s", all)
	}
	expect(all, "feedback", "list", "--repo", ".")
	expect("41 shop-013 (shop 013, where cart/cart.go): "+fixIssues[0].Text+"\n43 shop-013 (shop 013, where store/store.go): "+fixIssues[2].Text+"\n",
		"feedback", "list", "shop-build-2")
	expect("", "feedback", "list", "--repo", "buildermesh")

	gitIn := func(args ...string) {
		t.Helper()
		if _, err := git(wt, gitEnv(dir), args...); err != nil {
			t.Fatal(err)
		}
	}
	// Titles the binary doesn't read as fixes leave every issue listed.
	for _, subject := range []string{"Fix #41,#42: no space after the comma", "Fix #41:no space after the colon"} {
		gitIn("commit", "-q", "--allow-empty", "-m", subject)
		gitIn("push", "-q", "origin", "HEAD:main")
		expect(line(41, 42, 43, 44), "feedback", "list", "--repo", "shop")
	}
	// A fix landed but not drained is left out of --repo, not the family's.
	os.WriteFile(filepath.Join(wt, "cart", "note.go"), []byte("package cart\n"), 0o644)
	gitIn("add", "-A")
	gitIn("commit", "-qm", "Fix #41, #42: an empty cart totals 0.00")
	gitIn("push", "-q", "origin", "HEAD:main")
	expect(line(43, 44), "feedback", "list", "--repo", "shop")
	expect("41 shop-013 (shop 013, where cart/cart.go): "+fixIssues[0].Text+"\n43 shop-013 (shop 013, where store/store.go): "+fixIssues[2].Text+"\n",
		"feedback", "list", "shop-build-2")

	expect("drained 1\n", "feedback", "drain", "--seq", "43")
	expect("drained 2\nnot pending: 43,99\n", "--node", fixNode, "feedback", "drain", "--seq", "41,43,99,42")
	expect(line(44), "feedback", "list", "--repo", "shop")
	expect("", "feedback", "list", "shop-build-2")

	// A spec on main whose from_feedback names it, whatever its status.
	os.WriteFile(filepath.Join(wt, "docs", "proposals", "015-refunds.md"), []byte("---\nid: 015\nstatus: proposed\nfrom_feedback: shop-build-3 (shop-014) #44\n---\n"), 0o644)
	gitIn("add", "-A")
	gitIn("commit", "-qm", "Fix 015: proposed from shop-build-3 #44")
	gitIn("push", "-q", "origin", "HEAD:main")
	expect("", "feedback", "list", "--repo", "shop")

	// A family's drain takes what is pending up to --through.
	dir, _ = caseCell(t, "drain", manual)
	sandbox := filepath.Join(dir, sandboxDir)
	if _, out, _ := fake(t, dir, sandbox, "feedback", "list", drainFamily); strings.Count(out, "\n") != 3 || !strings.Contains(out, "52 shop-013 (buildermesh 013, where bmesh land): ") {
		t.Errorf("list %s:\n%s", drainFamily, out)
	}
	// A coordinator's quick fix on main is left out of --repo too.
	os.WriteFile(filepath.Join(sandbox, "cart", "note.go"), []byte("package cart\n"), 0o644)
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "Quick fix from " + drainFamily + " #51: the receipt says Total"}, {"push", "-q", "origin", "main"}} {
		if _, err := git(sandbox, gitEnv(dir), args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, out, _ := fake(t, dir, sandbox, "feedback", "list", "--repo", "shop"); !strings.HasPrefix(out, "53 shop-013 (shop 013, family "+drainFamily+", where store/store.go): ") || strings.Count(out, "\n") != 1 {
		t.Errorf("list --repo after a quick fix:\n%s", out)
	}
	if _, out, _ := fake(t, dir, sandbox, "feedback", "drain", drainFamily, "--through", "52"); out != "drained 2 from "+drainFamily+"\n" {
		t.Errorf("drain --through: %q", out)
	}
	if _, out, _ := fake(t, dir, sandbox, "feedback", "list", drainFamily); !strings.HasPrefix(out, "53 ") || strings.Count(out, "\n") != 1 {
		t.Errorf("list after the drain:\n%s", out)
	}

	// With nothing canned, the queue is empty, as in every other case.
	dir, _ = caseCell(t, "bug", manual)
	sandbox = filepath.Join(dir, sandboxDir)
	if _, out, _ := fake(t, dir, sandbox, "feedback", "list", "--repo", "shop"); out != "" {
		t.Errorf("an empty queue lists %q", out)
	}
	if _, out, _ := fake(t, dir, sandbox, "feedback", "drain", "--seq", "1"); out != "drained 0\nnot pending: 1\n" {
		t.Errorf("an empty queue drains %q", out)
	}
}

// The fix case's cell: the session is the fix lead in its worktree, and
// its one turn is the fix brief.
func TestFixBrief(t *testing.T) {
	dir, _ := caseCell(t, "fix", thisManual(t))
	c := findCase("fix")
	if b, _ := git(filepath.Join(dir, c.Lead.worktree()), gitEnv(dir), "branch", "--show-current"); b != "bmesh/"+fixNode {
		t.Errorf("the fix lead's worktree is on %q", b)
	}
	env, _ := os.ReadFile(filepath.Join(dir, "fake.env"))
	if !strings.Contains(string(env), "node='"+fixNode+"'") {
		t.Errorf("fake.env:\n%s", env)
	}
	text, err := c.Lead.brief(Arm{}, filepath.Join(dir, sandboxDir), thisBox(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Fix: rule on the feedback naming shop", "Your node on the mesh is `" + fixNode + "`, role `fixer`",
		"`no-new-surface`", "Say what each thing is"} {
		if !strings.Contains(text, want) {
			t.Errorf("the fix brief has no %q:\n%.600s", want, text)
		}
	}
}

const fakeFixClaude = `#!/bin/sh
# A fake fix lead: it rules on the four issues, fixing the bug and its
# duplicate in one commit, dropping the covered one, proposing the M one,
# draining each, and reports, all through the fake bmesh.
set -e
pwd >"$FAKE_CLAUDE_DIR/cwd"
bmesh started --text "4 issues" >/dev/null
bmesh feedback list --repo shop >"$FAKE_CLAUDE_DIR/list"
sed 's/return c.Subtotal() - c.Discount()/return max(0, c.Subtotal()-c.Discount())/' cart/cart.go >cart/cart.go.new && mv cart/cart.go.new cart/cart.go
git commit -qam "Fix #41, #42: an empty cart with a code totals 0.00"
git push -q origin HEAD:main
bmesh feedback drain --seq 41,42 >/dev/null
bmesh feedback drain --seq 43 >/dev/null
printf -- '---\nid: 015\ntitle: Orders can be refunded\nsize: M\nstatus: proposed\nfrom_feedback: shop-build-3 (shop-014) #44\n---\n' >docs/proposals/015-orders-can-be-refunded.md
git add -A && git commit -qm "Fix 015: proposed from shop-build-3 #44"
git push -q origin HEAD:main
bmesh feedback drain --seq 44 >/dev/null
bmesh feedback list --repo shop >"$FAKE_CLAUDE_DIR/after"
bmesh done --commit "$(git rev-parse HEAD)" --text "015 waits on you
41, 42: fixed by $(git rev-parse --short=8 HEAD~1)
43: dropped (drop: 012 covers it)
44: proposed 015" >/dev/null
printf '{"type":"result","subtype":"success","is_error":false,"result":"Ruled on 41 to 44.","session_id":"fake-fix","num_turns":9,"duration_ms":1000,"total_cost_usd":0.01}\n'
`

// The fix case runs end to end against a fake claude and scores on what
// it landed, drained and reported.
func TestRunFix(t *testing.T) {
	need(t, "git", "go", "sh")
	root := repoRoot(t)
	arm, err := prepareArm("cand", root, root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	claude := filepath.Join(tools, "claude")
	if err := os.WriteFile(claude, []byte(fakeFixClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	host := findHost()
	host.Home = t.TempDir()
	r := &runner{claude: claude, model: "claude-opus-5-5", timeout: 3 * time.Minute, env: append(os.Environ(), "FAKE_CLAUDE_DIR="+tools), host: host}
	row, err := r.runCell(context.Background(), cellPlan{n: 1, k: findCase("fix"), arm: arm, run: 1}, testFixture(t), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !row.Pass {
		t.Errorf("row %+v", row)
	}
	if b, _ := os.ReadFile(filepath.Join(tools, "list")); strings.Count(string(b), "\n") != 4 {
		t.Errorf("the session listed:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(tools, "after")); len(b) != 0 {
		t.Errorf("after its pass the session listed:\n%s", b)
	}
	if cwd, _ := os.ReadFile(filepath.Join(tools, "cwd")); !strings.HasSuffix(strings.TrimSpace(string(cwd)), filepath.Join("worktrees", "shop", fixNode)) {
		t.Errorf("the fix lead ran in %s", cwd)
	}
}

// The drain case's watch lands the lead's spec first, once, after its
// build: its done line names a commit on origin's main that sets 013 done
// and carries its code and its test, as a real lead's landing.
func TestFakeWatchLands(t *testing.T) {
	dir, base := caseCell(t, "drain", thisManual(t))
	sandbox := filepath.Join(dir, sandboxDir)
	origin := func() string {
		sha, _ := git(dir, gitEnv(dir), "--git-dir", filepath.Join(dir, originDir), "rev-parse", "main")
		return sha
	}
	fake(t, dir, sandbox, "build", "shop", "013")
	code, out, errs := fake(t, dir, sandbox, "watch", "--nodes", "shop-013", "--timeout", "30m")
	landed := origin()
	if code != 0 || landed == base || !strings.Contains(out, "shop-013 done 013 landed at "+landed[:8]+"\nwatch-done; next: bmesh feedback list "+drainFamily+"\n") {
		t.Fatalf("watch: exit %d, origin's main %s (base %s):\n%s%s", code, landed, base, out, errs)
	}
	spec, _ := git(dir, gitEnv(dir), "--git-dir", filepath.Join(dir, originDir), "show", "main:docs/proposals/013-report-details-lists-each-orders-items.md")
	if !strings.Contains(spec, "\nstatus: done\n") {
		t.Errorf("013 on origin's main:\n%s", spec)
	}
	landedCode(t, dir, landed, "report/report.go", "report/report_test.go", "cmd/shop/main.go")
	if _, again, _ := fake(t, dir, sandbox, "watch", "--nodes", "shop-013"); again != out || origin() != landed {
		t.Errorf("a second watch landed again or said otherwise:\n%s", again)
	}
}

// A family's feedback list ends with the steps of draining it on stderr,
// and watch-done names the list, on an arm whose manual documents them
// (100); an older arm's fake prints neither.
func TestFakeDrainNext(t *testing.T) {
	manual := thisManual(t)
	for _, tc := range []struct {
		name, manual string
		next         bool
	}{{"this manual", manual, true}, {"a manual before 100", strings.ReplaceAll(manual, "next: ", ""), false}} {
		dir, _ := caseCell(t, "drain", tc.manual)
		sandbox := filepath.Join(dir, sandboxDir)
		_, out, _ := fake(t, dir, sandbox, "watch", "--nodes", "shop-013")
		if got := strings.Contains(out, "watch-done; next: bmesh feedback list "+drainFamily+"\n"); got != tc.next {
			t.Errorf("%s: watch says next %v:\n%s", tc.name, got, out)
		}
		code, list, errs := fake(t, dir, sandbox, "feedback", "list", drainFamily)
		if code != 0 || strings.Count(list, "\n") != len(drainIssues) {
			t.Errorf("%s: feedback list: exit %d\n%s", tc.name, code, list)
		}
		if got := strings.Count(errs, "next: "); tc.next && (got != 3 || !strings.Contains(errs, "Quick fix from "+drainFamily+" #<seq>")) || !tc.next && got != 0 {
			t.Errorf("%s: feedback list's next lines:\n%s", tc.name, errs)
		}
	}
}
