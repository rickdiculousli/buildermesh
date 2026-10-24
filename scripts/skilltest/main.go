// Command skilltest measures buildermesh's skills: it runs scripted
// operator asks through headless Claude Code sessions, one arm per version
// of the plugin, against a sandbox repo with a local origin and a fake
// bmesh, and scores each cell on what it left. README.md beside this file
// says how to run it and read it.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() { os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr)) }

type armFlags []string

func (a *armFlags) String() string     { return strings.Join(*a, ",") }
func (a *armFlags) Set(v string) error { *a = append(*a, v); return nil }

const usageLine = `usage: go run ./scripts/skilltest --arm name=<git ref or dir> [--arm ...] [--cases a,b] [--runs N] [--model M] [--out DIR] [--jobs N] [--turn-timeout D]
       go run ./scripts/skilltest --rescore DIR`

func cli(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("skilltest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var armArgs armFlags
	fs.Var(&armArgs, "arm", "a version of the plugin, `name=<git ref or dir>`; repeatable")
	caseArg := fs.String("cases", "", "the cases to run, comma-separated (default every case)")
	runs := fs.Int("runs", 2, "runs of each case on each arm")
	model := fs.String("model", "opus", "the sessions' model: opus, sonnet, haiku, fable or a full id")
	out := fs.String("out", "", "where the cells, results.json and summary.md go (default cache/skilltest/<stamp> in this repo)")
	jobs := fs.Int("jobs", 2, "cells run at once")
	turnTimeout := fs.Duration("turn-timeout", 20*time.Minute, "one turn's limit; past it the turn's whole process group is killed")
	rescoreDir := fs.String("rescore", "", "recompute results.json and summary.md from `DIR`'s kept cells, with no API call")
	fs.Usage = func() {
		fmt.Fprintln(stderr, usageLine)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 || (*rescoreDir == "" && len(armArgs) == 0) || *runs < 1 || *jobs < 1 {
		fs.Usage()
		return 2
	}
	if *rescoreDir != "" {
		res, err := rescore(*rescoreDir)
		if err != nil {
			fmt.Fprintln(stderr, "skilltest:", err)
			return 1
		}
		fmt.Fprint(stdout, summary(res))
		return 0
	}
	selected, err := pickCases(*caseArg)
	if err != nil {
		fmt.Fprintln(stderr, "skilltest:", err)
		return 2
	}
	fixture, err := fixtureDir()
	if err != nil {
		fmt.Fprintln(stderr, "skilltest:", err)
		return 1
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(stderr, "skilltest: no claude on PATH")
		return 1
	}
	if claude, err = filepath.Abs(claude); err != nil {
		fmt.Fprintln(stderr, "skilltest:", err)
		return 1
	}
	stamp := time.Now().Format("20060102-150405")
	root, _ := git(".", nil, "rev-parse", "--show-toplevel")
	outDir := *out
	if outDir == "" {
		outDir = filepath.Join(root, "cache", "skilltest", stamp)
	}
	if outDir, err = filepath.Abs(outDir); err != nil {
		fmt.Fprintln(stderr, "skilltest:", err)
		return 1
	}
	var arms []Arm
	seen := map[string]bool{}
	for _, v := range armArgs {
		name, source, err := parseArm(v)
		if err == nil && seen[name] {
			err = fmt.Errorf("--arm %s is named twice", name)
		}
		if err != nil {
			fmt.Fprintln(stderr, "skilltest:", err)
			return 2
		}
		seen[name] = true
		a, err := prepareArm(name, source, root, outDir)
		if err != nil {
			fmt.Fprintln(stderr, "skilltest:", err)
			return 1
		}
		arms = append(arms, a)
	}
	// The instruments first: every reference must score as it should
	// before a session is paid for.
	if err := checkRefs(selected, fixture); err != nil {
		fmt.Fprintf(stderr, "skilltest: the scorers are broken, so no session runs:\n%v\n", err)
		return 1
	}
	scratch, err := scratchRoot(stamp)
	if err != nil {
		fmt.Fprintln(stderr, "skilltest:", err)
		return 1
	}
	defer os.RemoveAll(scratch)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := &runner{claude: claude, model: modelID(*model), timeout: *turnTimeout, projects: claudeProjects(), env: os.Environ(), host: findHost()}
	res := Results{Date: stamp, Model: r.model, Claude: claudeVersion(claude), Host: thisBox(), Arms: arms, Runs: *runs}
	for _, k := range selected {
		res.Cases = append(res.Cases, k.Name)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(stderr, "skilltest:", err)
		return 1
	}
	fmt.Fprintf(stdout, "skilltest: %d cells, %d at a time, into %s\n", len(selected)*len(arms)**runs, *jobs, outDir)
	res.Rows = r.runAll(ctx, plan(selected, arms, *runs), fixture, scratch, outDir, *jobs, func(row Row, done, total int) {
		defects := ""
		if named, missed := planted(row); len(named)+len(missed) > 0 {
			defects = fmt.Sprintf(", named [%s], missed [%s]", strings.Join(named, " "), strings.Join(missed, " "))
		}
		fmt.Fprintf(stdout, "  [%d/%d] %s / %s #%d: %s, %d steers, %d commits, $%.2f, %.0fs%s%s\n", done, total, row.Case, row.Arm, row.Run,
			map[bool]string{true: "pass", false: "fail"}[row.Pass], row.Steers, row.Commits, row.CostUSD, float64(row.DurationMS)/1000, defects,
			map[bool]string{true: ", error: " + row.Error, false: ""}[row.Error != ""])
		if err := writeResults(outDir, res); err != nil {
			fmt.Fprintln(stderr, "skilltest:", err)
		}
	}, &res)
	if err := writeResults(outDir, res); err != nil {
		fmt.Fprintln(stderr, "skilltest:", err)
		return 1
	}
	fmt.Fprint(stdout, flips(res))
	fmt.Fprintf(stdout, "skilltest: wrote %s and %s\n", filepath.Join(outDir, "results.json"), filepath.Join(outDir, "summary.md"))
	if ctx.Err() != nil {
		return 1
	}
	return 0
}

func pickCases(arg string) ([]*Case, error) {
	if arg == "" {
		return cases, nil
	}
	var out []*Case
	for _, name := range strings.Split(arg, ",") {
		k := findCase(strings.TrimSpace(name))
		if k == nil {
			var names []string
			for _, c := range cases {
				names = append(names, c.Name)
			}
			return nil, fmt.Errorf("no case %q; the cases are %s", name, strings.Join(names, ", "))
		}
		out = append(out, k)
	}
	return out, nil
}

// fixtureDir is the fixture beside this file's source, else in the repo.
func fixtureDir() (string, error) {
	if _, file, _, ok := runtime.Caller(0); ok {
		if d := filepath.Join(filepath.Dir(file), "fixture"); isDir(d) {
			return d, nil
		}
	}
	root, err := git(".", nil, "rev-parse", "--show-toplevel")
	if err == nil {
		if d := filepath.Join(root, "scripts", "skilltest", "fixture"); isDir(d) {
			return d, nil
		}
	}
	return "", errors.New("no fixture: run skilltest from the buildermesh repo")
}

// memoryFiles are what claude loads as project instructions from the
// session's cwd and every directory above it.
var memoryFiles = []string{"CLAUDE.md", "CLAUDE.local.md", "AGENTS.md", ".claude/CLAUDE.md", ".claude/AGENTS.md", ".claude/rules"}

// scratchRoot is where the cells run until they are done: under TMPDIR,
// never /tmp, and with no instructions above it for a session to load (in
// the repo's cache/ every session would load buildermesh's AGENTS.md).
func scratchRoot(stamp string) (string, error) {
	tmp := os.TempDir()
	if tmp == "/tmp" || strings.HasPrefix(tmp, "/tmp/") {
		return "", errors.New("TMPDIR is /tmp, which uses RAM here: set TMPDIR (/var/tmp on k16)")
	}
	dir, err := os.MkdirTemp(tmp, "skilltest-"+stamp+"-")
	if err != nil {
		return "", err
	}
	for p := filepath.Dir(dir); ; p = filepath.Dir(p) {
		for _, f := range memoryFiles {
			if _, err := os.Stat(filepath.Join(p, f)); err == nil {
				os.RemoveAll(dir)
				return "", fmt.Errorf("a session under %s would load %s: set TMPDIR to a directory with no instructions above it", tmp, filepath.Join(p, f))
			}
		}
		if p == filepath.Dir(p) {
			return dir, nil
		}
	}
}

// cellPlan is one cell to run.
type cellPlan struct {
	n   int
	k   *Case
	arm Arm
	run int
}

func plan(ks []*Case, arms []Arm, runs int) []cellPlan {
	var out []cellPlan
	for _, k := range ks {
		for run := 1; run <= runs; run++ {
			for _, a := range arms {
				out = append(out, cellPlan{n: len(out) + 1, k: k, arm: a, run: run})
			}
		}
	}
	return out
}

// runAll runs the cells, jobs at a time, adding each row to res as it is
// scored and calling progress, and returns the rows.
func (r *runner) runAll(ctx context.Context, cells []cellPlan, fixture, scratch, outDir string, jobs int, progress func(Row, int, int), res *Results) []Row {
	var mu sync.Mutex
	work := make(chan cellPlan)
	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range work {
				if ctx.Err() != nil {
					continue
				}
				row, err := r.runCell(ctx, p, fixture, scratch, outDir)
				if err != nil {
					row = Row{Case: p.k.Name, Arm: p.arm.Name, Run: p.run, Error: err.Error()}
				}
				mu.Lock()
				res.Rows = append(res.Rows, row)
				progress(row, len(res.Rows), len(cells))
				mu.Unlock()
			}
		}()
	}
	for _, p := range cells {
		if ctx.Err() != nil {
			break
		}
		work <- p
	}
	close(work)
	wg.Wait()
	return res.Rows
}

// runCell runs one cell: its turns, then a steer while the expectations
// don't hold and the session asks; then it keeps the transcript, archives
// the session's work, moves the cell into the out dir and scores it there.
func (r *runner) runCell(ctx context.Context, p cellPlan, fixture, scratch, outDir string) (Row, error) {
	dir := filepath.Join(scratch, fmt.Sprintf("%03d", p.n))
	now := time.Now()
	host := thisBox()
	cs, err := setupCell(dir, p.k, p.arm, fixture, host, now)
	if err != nil {
		return Row{}, err
	}
	if err := makeHome(dir, r.host); err != nil {
		return Row{}, err
	}
	c := &Cell{Case: p.k.Name, Arm: p.arm.Name, Run: p.run, Model: r.model, Plugin: p.arm.Dir, Host: host, Base: cs.Base, Cwd: cs.Cwd, Routine: cs.Routine, Started: now}
	if err := c.save(dir); err != nil {
		return Row{}, err
	}
	var turnErr error
	for _, words := range cs.Words {
		if turnErr = r.turn(ctx, dir, c, words, false); turnErr != nil {
			break
		}
	}
	for turnErr == nil && p.k.Steer != "" && c.Steers < maxSteers {
		pass, reply, err := scoreNow(dir, c, p.k)
		if err != nil {
			turnErr = err
			break
		}
		if pass || !asksOperator(reply) {
			break
		}
		c.Steers++
		turnErr = r.turn(ctx, dir, c, p.k.Steer, true)
	}
	if turnErr != nil {
		c.Error = turnErr.Error()
	}
	c.Ended = time.Now()
	if err := keepLeadBranch(dir, c); err != nil && turnErr == nil {
		c.Error = "keeping the lead's branch: " + err.Error()
	}
	r.keepTranscripts(dir, c)
	unlinkHome(dir)
	c.Left = pruneWorktrees(dir)
	if err := c.save(dir); err != nil {
		return Row{}, err
	}
	names, err := leftovers(dir)
	if err != nil {
		return Row{}, err
	}
	if err := archive(dir, names, filepath.Join(dir, workTar)); err != nil {
		return Row{}, err
	}
	final := filepath.Join(outDir, "cells", fmt.Sprintf("%s__%s__%d", p.k.Name, p.arm.Name, p.run))
	if err := move(dir, final); err != nil {
		return Row{}, err
	}
	return scoreCell(final, p.k)
}

// scoreNow scores the running cell: whether it passes, and its last reply.
func scoreNow(dir string, c *Cell, k *Case) (bool, string, error) {
	if err := c.save(dir); err != nil {
		return false, "", err
	}
	if err := keepLeadBranch(dir, c); err != nil {
		return false, "", err
	}
	s, err := loadState(dir)
	if err != nil {
		return false, "", err
	}
	defer s.Close()
	return score(s, k).Pass, s.Reply(), nil
}

// rescore scores every kept cell under dir again and rewrites its
// results.json and summary.md; no session runs.
func rescore(dir string) (Results, error) {
	var res Results
	dir, err := filepath.Abs(dir) // scoring runs git and the shop from other cwds
	if err != nil {
		return res, err
	}
	if b, err := os.ReadFile(filepath.Join(dir, "results.json")); err == nil {
		if err := json.Unmarshal(b, &res); err != nil {
			return res, fmt.Errorf("%s: %w", filepath.Join(dir, "results.json"), err)
		}
	}
	res.Rows, res.Rescored = nil, true
	cells, err := filepath.Glob(filepath.Join(dir, "cells", "*", cellFile))
	if err != nil {
		return res, err
	}
	if len(cells) == 0 {
		return res, fmt.Errorf("no cells under %s", filepath.Join(dir, "cells"))
	}
	for _, f := range cells {
		c, err := loadCell(filepath.Dir(f))
		if err != nil {
			return res, err
		}
		k := findCase(c.Case)
		if k == nil {
			return res, fmt.Errorf("%s: no case %q any more", f, c.Case)
		}
		row, err := scoreCell(filepath.Dir(f), k)
		if err != nil {
			return res, err
		}
		res.Rows = append(res.Rows, row)
	}
	return res, writeResults(dir, res)
}
