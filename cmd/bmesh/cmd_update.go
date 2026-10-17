package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
	"github.com/rickdiculousli/buildermesh/internal/upgrade"
)

// userCacheDir and executable are config.CacheDir and os.Executable; tests
// point them into their temp dir.
var (
	userCacheDir = config.CacheDir
	executable   = os.Executable
)

// pluginLine is what update prints after an upstream swap: the plugin's
// skills name commands only a matching binary has.
const pluginLine = "update the buildermesh plugin too: claude plugin marketplace update buildermesh && claude plugin update buildermesh@buildermesh (then restart Claude Code)"

const updateUsage = "usage: bmesh update [--repo R] [--latest] [--plan] [--answer KEY=VALUE]... [--defaults] [--apply [--yes]] | update --notice"

// answers is --answer, repeatable: KEY=VALUE.
type answers map[string]string

func (m answers) String() string { return "" }

func (m answers) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return fmt.Errorf("--answer %q: want KEY=VALUE", s)
	}
	m[strings.TrimSpace(k)] = v
	return nil
}

// walkFlags is how a walk answers and runs: what update passes on to the
// new binary's walk.
type walkFlags struct {
	answers        answers
	defaults       bool
	apply, yes     bool
	plan           bool
	from, to, dir  string // --walk and --plan: the range and the repo main is read from
	walk, preSwap  bool
	fromSet, toSet bool
}

// args are the flags that carry f to another binary's walk.
func (f walkFlags) args() []string {
	var out []string
	for k, v := range f.answers {
		out = append(out, "--answer", k+"="+v)
	}
	if f.defaults {
		out = append(out, "--defaults")
	}
	if f.apply {
		out = append(out, "--apply")
	}
	if f.yes {
		out = append(out, "--yes")
	}
	return out
}

// cmdUpdate builds what [update] track follows on the update repo's main
// (the newest release, or the code head) over this binary, as the daemon's
// [update] does, without a daemon; --latest goes to the code head this
// once. With no repo to build from it installs from the module's main.
// Between the build and the swap the new binary walks the upgrade notes of
// the releases in between (walk); --plan prints them. --notice prints the
// daemon's notice and nothing else.
func (a *app) cmdUpdate(args []string) error {
	fs := a.flags("update")
	repoFlag := fs.String("repo", "", "registry name of the repo to build from (default [update] repo)")
	latest := fs.Bool("latest", false, "go to main's code head this once, whatever [update] track says")
	notice := fs.Bool("notice", false, "print the daemon's notice (<state dir>/update.txt) and never touch the network")
	w := walkFlags{answers: answers{}}
	fs.Var(w.answers, "answer", "answer an upgrade ask: KEY=VALUE (the key as the ask names it, with or without its [section]); repeatable")
	fs.BoolVar(&w.defaults, "defaults", false, "take every upgrade ask's default")
	fs.BoolVar(&w.apply, "apply", false, "run the upgrade notes' run and check steps without asking")
	fs.BoolVar(&w.yes, "yes", false, "with --apply and no terminal: run the steps that ask first")
	fs.BoolVar(&w.plan, "plan", false, "print the upgrade steps between this binary and the target (--json: as JSON) and change nothing")
	fs.BoolVar(&w.walk, "walk", false, "walk the upgrade notes from --from to this binary (what update runs on the new binary)")
	fs.BoolVar(&w.preSwap, "before-swap", false, "with --walk: check the config and answer the asks only, before the swap")
	fs.StringVar(&w.from, "from", "", "with --walk or --plan: the revision (or release) walked from; \"\" is a box with no record")
	fs.StringVar(&w.to, "to", "", "with --walk or --plan: the revision walked to (default this binary's)")
	fs.StringVar(&w.dir, "dir", "", "with --walk or --plan: the repo (a checkout, or the bare upstream) main's notes are read from")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "from":
			w.fromSet = true
		case "to":
			w.toSet = true
		}
	})
	walkOnly := w.walk || w.preSwap || w.fromSet || w.toSet || w.dir != ""
	switch {
	case len(pos) != 0, *notice && (*repoFlag != "" || *latest || w.plan || w.apply || w.defaults || len(w.answers) > 0 || walkOnly),
		w.preSwap && !w.walk, w.walk && w.plan, w.yes && !w.apply,
		walkOnly && !w.walk && !w.plan, (w.walk || w.plan && walkOnly) && (*repoFlag != "" || *latest):
		return fail(exitUsage, updateUsage)
	}
	if *notice {
		if line := selfupdate.ReadNotice(config.StateDir()); line != "" {
			fmt.Fprintln(a.stdout, line)
		}
		if line := upgrade.Pending(config.StateDir(), buildVersion().Release); line != "" {
			fmt.Fprintln(a.stdout, "bmesh: "+line)
		}
		return nil
	}
	if w.walk || w.plan && walkOnly {
		return a.walkGiven(w)
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	track := a.cfg.Update.Track
	if *latest {
		track = selfupdate.TrackLatest
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	src, unlock, err := a.updateSource(*repoFlag)
	if err != nil {
		return err
	}
	defer unlock()
	t, err := selfupdate.Follow(src.dir, src.ref, track, a.specDir())
	if err != nil {
		return err
	}
	install := func(gate selfupdate.Gate) error {
		return selfupdate.Install(context.Background(), src.dir, exe, t.Commit, gate)
	}
	if src.module != "" {
		// No registry row: go install it from the module's main, whose
		// tags the bare repo has, so a target without this binary's
		// revision is refused as a row's is.
		install = func(gate selfupdate.Gate) error {
			return selfupdate.InstallModule(context.Background(), src.dir, src.module, exe, t, gate)
		}
	}
	return a.updateTo(exe, src.dir, src.name, t, src.module != "", w, install)
}

// updateTo compares this binary with t in dir and runs install when it is
// behind (or of unknown commit, a tag dir doesn't know included), printing
// `<old> -> <new>` (selfupdate.Label); the binary that is t prints `up to
// date`, one with t in its history `ahead of <t>, up to date`, and one t
// doesn't contain is refused. plugin adds the plugin line after a swap.
// The build is gated by its own walk of the notes' asks (`<new> update
// --walk --before-swap`), which checks the config with the new binary and
// keeps this one when it fails; after the swap the new binary walks the
// rest. A current binary resumes a walk left unfinished, or walks the notes
// still pending since this box's record. --plan prints the walk instead.
func (a *app) updateTo(exe, dir, from string, t selfupdate.Target, plugin bool, w walkFlags, install func(selfupdate.Gate) error) error {
	tagged := buildVersion().Own()
	own := selfupdate.OwnCommit(dir, tagged)
	label := selfupdate.Label(dir, own)
	if own == "" && tagged != "" {
		label = tagged
	}
	stand := selfupdate.Stand(dir, own, t.Commit)
	if stand == selfupdate.Apart && own != "" {
		return fail(exitRefused, "%s's main (%s) does not contain this binary's %s; left alone", from,
			selfupdate.Label(dir, t.Commit), label)
	}
	if stand == selfupdate.Current || stand == selfupdate.Ahead {
		r, ok := a.pendingWalk(dir, own)
		if w.plan {
			if !ok {
				r = walkRange{from: own, to: own}
			}
			return a.printPlan(r)
		}
		if stand == selfupdate.Current {
			fmt.Fprintln(a.stdout, "up to date")
		} else {
			fmt.Fprintf(a.stdout, "ahead of %s, up to date\n", selfupdate.Label(dir, t.Commit))
		}
		if ok {
			if err := a.walk(r, w, phaseAll); err != nil {
				return err
			}
		} else if _, have := upgrade.ReadInstalled(config.StateDir()); !have {
			a.recordInstalled(dir, own)
		}
		a.restartLine(own)
		return clearNotice()
	}
	r := walkRange{dir: dir, from: own, to: t.Commit}
	if w.plan {
		return a.printPlan(r)
	}
	gate := func(built string) error { return a.childWalk(built, r, w, true) }
	var refused *selfupdate.Refused
	if err := install(gate); errors.As(err, &refused) {
		return fail(exitRefused, "update: %v", err)
	} else if err != nil {
		return err
	}
	if tagged != "" {
		_ = upgrade.WritePrevious(config.StateDir(), tagged)
	}
	fmt.Fprintf(a.stdout, "%s -> %s\n", label, selfupdate.Label(dir, t.Commit))
	if plugin {
		fmt.Fprintln(a.stdout, pluginLine)
	}
	if err := a.childWalk(exe, r, w, false); err != nil {
		return err
	}
	a.restartLine(t.Commit)
	return clearNotice()
}

// clearNotice removes the daemon's notice once this box's binary is
// current, so the next session opens without it.
func clearNotice() error { return selfupdate.WriteNotice(config.StateDir(), "") }

// ownRepo is the registry row whose go.mod declares the module this binary
// was built from, or "" when none does (or the binary does not say).
func (a *app) ownRepo() string {
	bi, ok := readBuildInfo()
	if !ok || bi.Main.Path == "" {
		return ""
	}
	for _, n := range a.cfg.RepoNames() {
		_, p, err := a.cfg.RepoPath(n)
		if err == nil && modulePath(filepath.Join(p, "go.mod")) == bi.Main.Path {
			return n
		}
	}
	return ""
}

// modulePath is the module a go.mod declares, "" when unreadable.
func modulePath(goMod string) string {
	f, err := os.Open(goMod)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// childWalk runs bin's own walk of r (the new binary's: the one being
// replaced may predate the walk, so the binary that walks is always the
// one being installed): before the swap, `update --walk --before-swap`
// checks the config and answers the asks, and its failure keeps this
// binary; after it, `update --walk` runs the rest. Its output and its
// questions go through this process's.
func (a *app) childWalk(bin string, r walkRange, w walkFlags, preSwap bool) error {
	args := []string{}
	if a.configPath != "" {
		args = append(args, "--config", a.configPath)
	}
	args = append(args, "update", "--walk", "--from", r.from, "--to", r.to, "--dir", r.dir)
	if preSwap {
		args = append(args, "--before-swap")
	}
	args = append(args, w.args()...)
	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.stdin, a.stdout, a.stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if preSwap {
			return fail(ee.ExitCode(), "keeping this binary")
		}
		return fail(ee.ExitCode(), "")
	}
	return err
}
