package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Call is one call of the fake bmesh, from its log.
type Call struct {
	Exit int // the status it exited with; 0 in a log from before the fake wrote it
	Cwd  string
	Args []string
}

// OK reports whether the call succeeded: a call the fake refused (an
// unknown command or flag, a refusal the binary makes) did nothing.
func (c Call) OK() bool { return c.Exit == 0 }

// valueFlags are the global flags that take a value; --json takes none.
var valueFlags = map[string]bool{"config": true, "node": true}

// Cmd is the call's command and the arguments after it, the global flags
// before it left out.
func (c Call) Cmd() (string, []string) {
	for i := 0; i < len(c.Args); i++ {
		a := c.Args[i]
		if !strings.HasPrefix(a, "-") {
			return a, c.Args[i+1:]
		}
		name, _, inline := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if valueFlags[name] && !inline {
			i++
		}
	}
	return "", nil
}

// Is reports whether the call is the command words name: "build", or a
// command and its subcommand, "lead start".
func (c Call) Is(words ...string) bool {
	cmd, rest := c.Cmd()
	if cmd != words[0] {
		return false
	}
	pos := positionals(rest)
	for i, w := range words[1:] {
		if i >= len(pos) || pos[i] != w {
			return false
		}
	}
	return true
}

// Has reports whether the call names the flag (--name or --name=value).
func (c Call) Has(name string) bool {
	_, rest := c.Cmd()
	for _, a := range rest {
		n, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if strings.HasPrefix(a, "-") && n == name {
			return true
		}
	}
	return false
}

// String is the call as a command line, without the global flags.
func (c Call) String() string {
	cmd, rest := c.Cmd()
	return strings.TrimSpace(cmd + " " + strings.Join(rest, " "))
}

// boolFlags are the flags of bmesh commands that take no value, as the fake
// reads them; every other flag takes the next argument unless written
// --flag=value.
var boolFlags = map[string]bool{}

func init() {
	for _, f := range strings.Fields("accepted force json idea peek here rerun dry-run minor patch all check write cancel apply yes defaults latest worktree wipe secure admin service no-repo restart detach plan walk before-swap notice notes checkouts ignored-ok") {
		boolFlags[f] = true
	}
}

// positionals are the arguments that are neither a flag nor a flag's value.
func positionals(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(out, args[i+1:]...)
		case a == "-" || !strings.HasPrefix(a, "-"):
			out = append(out, a)
		case strings.Contains(a, "="):
		case !boolFlags[strings.TrimLeft(a, "-")]:
			i++
		}
	}
	return out
}

// escapeField and unescapeField are the log's field encoding: \, a tab
// and a newline written \\, \t and \n.
func escapeField(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`).Replace(s)
}

func unescapeField(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			case 't':
				b.WriteByte('\t')
				i++
				continue
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// logLine is one call that succeeded, as the fake logs it.
func logLine(cwd string, args ...string) string {
	f := []string{"0", escapeField(cwd)}
	for _, a := range args {
		f = append(f, escapeField(a))
	}
	return strings.Join(f, "\t") + "\n"
}

func readCalls(file string) ([]Call, error) {
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var calls []Call
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		c := Call{}
		// The status leads the line; a cwd is never all digits, so a log
		// from before the fake wrote it reads as calls that succeeded.
		if n, err := strconv.Atoi(f[0]); err == nil && len(f) > 1 {
			c.Exit, f = n, f[1:]
		}
		c.Cwd = unescapeField(f[0])
		for _, a := range f[1:] {
			c.Args = append(c.Args, unescapeField(a))
		}
		calls = append(calls, c)
	}
	return calls, nil
}

// hookCall reports whether a call is one the plugin's session hook makes.
func hookCall(c Call) bool {
	return c.Is("hosts") && c.Has("here") || c.Is("update") && c.Has("notice")
}

//go:embed testdata/hidden
var hiddenFS embed.FS

// seedOrders is the store the hidden checks run the shop against.
const seedOrders = `[
  {"id": 1, "at": "2026-10-20T09:00:00Z", "items": [{"name": "apple", "price": 150, "qty": 2}], "total": 300},
  {"id": 2, "at": "2026-10-21T09:00:00Z", "items": [{"name": "bread", "price": 350, "qty": 1}, {"name": "pear", "price": 120, "qty": 1}], "total": 470},
  {"id": 3, "at": "2026-10-22T09:00:00Z", "items": [{"name": "cheese", "price": 675, "qty": 2}], "code": "SAVE5", "total": 850}
]
`

// State is what a cell left, as its case's predicates read it: origin's
// main, the fake's log, the replies and the snapshots after each turn.
// Checks that build or test origin's main run in a scratch checkout, once.
type State struct {
	Dir     string
	Cell    *Cell
	Calls   []Call
	Replies []string
	Results []Result
	Inits   []*Init // each turn's init event, nil where its output had none

	scratch string
	memo    map[string]check
}

type check struct {
	ok  bool
	why string
}

// loadState reads the cell in dir; Close removes its scratch.
func loadState(dir string) (*State, error) {
	c, err := loadCell(dir)
	if err != nil {
		return nil, err
	}
	s := &State{Dir: dir, Cell: c, memo: map[string]check{}}
	if s.Calls, err = readCalls(filepath.Join(dir, logFile)); err != nil {
		return nil, err
	}
	for _, t := range c.Turns {
		r, init, _ := readTurn(filepath.Join(dir, t.File))
		s.Results = append(s.Results, r)
		s.Inits = append(s.Inits, init)
		s.Replies = append(s.Replies, r.Result)
	}
	return s, nil
}

func (s *State) Close() {
	if s.scratch != "" {
		os.RemoveAll(s.scratch)
	}
}

// Reply is the last turn's reply.
func (s *State) Reply() string {
	if len(s.Replies) == 0 {
		return ""
	}
	return s.Replies[len(s.Replies)-1]
}

// AgentCalls are the calls the session made, the hook's left out.
func (s *State) AgentCalls() []Call {
	var out []Call
	for _, c := range s.Calls {
		if !hookCall(c) {
			out = append(out, c)
		}
	}
	return out
}

func (s *State) git(args ...string) (string, error) {
	return git(s.Dir, gitEnv(s.Dir), append([]string{"--git-dir", filepath.Join(s.Dir, originDir)}, args...)...)
}

// NewCommits are origin's commits on main that the base lacks, newest first.
func (s *State) NewCommits() []string {
	out, err := s.git("rev-list", s.Cell.Base+"..refs/heads/main")
	if err != nil {
		return nil
	}
	return strings.Fields(out)
}

// Changed are the files origin's main changed since the base.
func (s *State) Changed() []string {
	out, err := s.git("diff", "--name-only", s.Cell.Base, "refs/heads/main")
	if err != nil {
		return nil
	}
	return strings.Fields(out)
}

const mainRef = "refs/heads/main"

// Show is a file on origin's main, or "" when it has none.
func (s *State) Show(path string) string { return s.ShowAt(mainRef, path) }

// ShowAt is a file at a ref of origin, or "" when it has none.
func (s *State) ShowAt(ref, path string) string {
	out, _ := s.git("show", ref+":"+path)
	return out
}

// scratchDir is this scoring's scratch, made on first use outside the cell,
// so the kept cell never holds a checkout.
func (s *State) scratchDir() (string, error) {
	if s.scratch == "" {
		d, err := os.MkdirTemp("", "skilltest-score-")
		if err != nil {
			return "", err
		}
		s.scratch = d
	}
	return s.scratch, nil
}

// once runs a check under key the first time and remembers its answer.
func (s *State) once(key string, f func() (bool, string)) (bool, string) {
	if c, ok := s.memo[key]; ok {
		return c.ok, c.why
	}
	ok, why := f()
	s.memo[key] = check{ok, why}
	return ok, why
}

// checkout is a clone of origin's main in the scratch.
func (s *State) checkout() (string, error) { return s.checkoutAt(mainRef) }

// checkoutAt is a clone of origin in the scratch, checked out at ref.
func (s *State) checkoutAt(ref string) (string, error) {
	key := "checkout " + ref
	if c, ok := s.memo[key]; ok {
		if !c.ok {
			return "", fmt.Errorf("%s", c.why)
		}
		return c.why, nil
	}
	dir, err := func() (string, error) {
		sha, err := s.git("rev-parse", "--verify", "-q", ref+"^{commit}")
		if err != nil {
			return "", fmt.Errorf("origin has no %s", ref)
		}
		root, err := s.scratchDir()
		if err != nil {
			return "", err
		}
		dir := filepath.Join(root, strings.ReplaceAll(strings.TrimPrefix(ref, "refs/"), "/", "-"))
		if _, err := git(root, gitEnv(s.Dir), "clone", "-q", "--no-checkout", filepath.Join(s.Dir, originDir), dir); err != nil {
			return "", err
		}
		_, err = git(dir, gitEnv(s.Dir), "-c", "advice.detachedHead=false", "checkout", "-q", sha)
		return dir, err
	}()
	if err != nil {
		s.memo[key] = check{false, err.Error()}
		return "", err
	}
	s.memo[key] = check{true, dir}
	return dir, nil
}

// host is this box's, read once for every scoring.
var host = sync.OnceValue(findHost)

// goEnv is the environment scoring runs origin's main's code in, code a
// session wrote: a session's, as sessionEnv makes it (the cell's fake bmesh
// and stubs first on PATH, nothing of this box's home, offline), with its
// home, tmp and the fake's log in this scoring's scratch, so the code
// reaches nothing real and leaves nothing in the kept cell.
func (s *State) goEnv() ([]string, error) {
	root, err := s.scratchDir()
	if err != nil {
		return nil, err
	}
	home, tmp := filepath.Join(root, "home"), filepath.Join(root, "tmp")
	for _, d := range []string{home, tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return append(sessionEnv(os.Environ(), s.Dir, host()),
		"HOME="+home, "TMPDIR="+tmp, "TMUX_TMPDIR="+tmp, "GOPATH="+filepath.Join(home, "go"),
		"SKILLTEST_LOG="+filepath.Join(root, logFile), "GOFLAGS="), nil
}

func run(dir string, timeout time.Duration, env []string, name string, args ...string) (int, string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		if ctx.Err() != nil {
			stderr.WriteString("\n(timed out after " + timeout.String() + ")")
		}
	}
	return code, stdout.String(), stderr.String()
}

// tail is the last lines of out, for a check's why.
func tail(out string, n int) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.ReplaceAll(strings.Join(lines, " | "), "\t", " ")
}

// TestsPass runs the fixture's own tests on origin's main.
func (s *State) TestsPass() (bool, string) { return s.TestsPassAt(mainRef, "origin's main") }

// TestsPassAt runs the fixture's own tests at a ref of origin, which where
// names.
func (s *State) TestsPassAt(ref, where string) (bool, string) {
	return s.once("tests "+ref, func() (bool, string) {
		dir, err := s.checkoutAt(ref)
		if err != nil {
			return false, err.Error()
		}
		env, err := s.goEnv()
		if err != nil {
			return false, err.Error()
		}
		code, out, errs := run(dir, 5*time.Minute, env, "go", "test", "./...")
		if code != 0 {
			return false, "go test ./... on " + where + ": " + tail(out+errs, 6)
		}
		return true, "go test ./... passes on " + where
	})
}

// Hidden runs one hidden test against origin's main.
func (s *State) Hidden(pkg, test string) (bool, string) {
	return s.HiddenAt(mainRef, "origin's main", pkg, test)
}

// HiddenAt runs one hidden test at a ref of origin, which where names: the
// files under testdata/hidden/<pkg> are copied into the package for the
// run and taken out after it, so the shared checkout holds only the ref.
func (s *State) HiddenAt(ref, where, pkg, test string) (bool, string) {
	return s.once("hidden "+ref+" "+test, func() (bool, string) {
		dir, err := s.checkoutAt(ref)
		if err != nil {
			return false, err.Error()
		}
		env, err := s.goEnv()
		if err != nil {
			return false, err.Error()
		}
		files, err := fs.ReadDir(hiddenFS, "testdata/hidden/"+pkg)
		if err != nil {
			return false, err.Error()
		}
		for _, f := range files {
			b, err := hiddenFS.ReadFile("testdata/hidden/" + pkg + "/" + f.Name())
			if err != nil {
				return false, err.Error()
			}
			p := filepath.Join(dir, pkg, f.Name())
			if err := writeFile(p, string(b)); err != nil {
				return false, err.Error()
			}
			defer os.Remove(p)
		}
		code, out, errs := run(dir, 5*time.Minute, env, "go", "test", "-count=1", "-run", "^"+test+"$", "./"+pkg)
		if code != 0 {
			return false, test + ": " + tail(out+errs, 4)
		}
		return true, test + " passes on " + where
	})
}

// binary is origin's main's shop command, built once.
func (s *State) binary() (string, error) {
	ok, why := s.once("build", func() (bool, string) {
		dir, err := s.checkout()
		if err != nil {
			return false, err.Error()
		}
		env, err := s.goEnv()
		if err != nil {
			return false, err.Error()
		}
		bin := filepath.Join(filepath.Dir(dir), "shop-bin")
		code, out, errs := run(dir, 5*time.Minute, env, "go", "build", "-o", bin, "./cmd/shop")
		if code != 0 {
			return false, "go build ./cmd/shop: " + tail(out+errs, 4)
		}
		return true, bin
	})
	if !ok {
		return "", fmt.Errorf("%s", why)
	}
	return why, nil
}

// ShopRun is one run of the shop command against the seeded store.
type ShopRun struct {
	Exit           int
	Stdout, Stderr string
	Files          map[string]string // the .csv files it wrote, by name
}

// Shop runs origin's main's shop command with args, in a fresh directory
// holding the seeded store (SHOP_STORE names it too).
func (s *State) Shop(args ...string) (ShopRun, error) {
	bin, err := s.binary()
	if err != nil {
		return ShopRun{}, err
	}
	root, err := s.scratchDir()
	if err != nil {
		return ShopRun{}, err
	}
	dir, err := os.MkdirTemp(root, "run-")
	if err != nil {
		return ShopRun{}, err
	}
	store := filepath.Join(dir, "orders.json")
	if err := writeFile(store, seedOrders); err != nil {
		return ShopRun{}, err
	}
	env, err := s.goEnv()
	if err != nil {
		return ShopRun{}, err
	}
	var r ShopRun
	r.Exit, r.Stdout, r.Stderr = run(dir, 30*time.Second, append(env, "SHOP_STORE="+store), bin, args...)
	r.Files = map[string]string{}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.csv"))
	for _, m := range matches {
		if b, err := os.ReadFile(m); err == nil {
			r.Files[filepath.Base(m)] = string(b)
		}
	}
	return r, nil
}

// Diff is origin's main's change since the base, limited to paths.
func (s *State) Diff(paths ...string) string {
	out, _ := s.git(append([]string{"diff", s.Cell.Base, "refs/heads/main", "--"}, paths...)...)
	return out
}

// Transcript is the cell's session transcripts, joined.
func (s *State) Transcript() string {
	var b strings.Builder
	_ = filepath.WalkDir(filepath.Join(s.Dir, "claude-project"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
			if t, err := os.ReadFile(p); err == nil {
				b.Write(t)
			}
		}
		return nil
	})
	return b.String()
}

var skillUseRE = regexp.MustCompile(`"name":"Skill","input":\{"skill":"([^"]+)"`)

// Skills are the skills the session invoked, in order, once each.
func (s *State) Skills() []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range skillUseRE.FindAllStringSubmatch(s.Transcript(), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}
