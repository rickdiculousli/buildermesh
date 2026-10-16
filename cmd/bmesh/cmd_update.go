package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
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

// cmdUpdate builds what [update] track follows on the update repo's main
// (the newest release, or the code head) over this binary, as the daemon's
// [update] does, without a daemon; --latest goes to the code head this
// once. With no repo to build from it installs from the module's main.
// --notice prints the daemon's notice and nothing else.
func (a *app) cmdUpdate(args []string) error {
	fs := a.flags("update")
	repoFlag := fs.String("repo", "", "registry name of the repo to build from (default [update] repo)")
	latest := fs.Bool("latest", false, "go to main's code head this once, whatever [update] track says")
	notice := fs.Bool("notice", false, "print the daemon's notice (<state dir>/update.txt) and never touch the network")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || (*notice && (*repoFlag != "" || *latest)) {
		return fail(exitUsage, "usage: bmesh update [--repo R] [--latest] | update --notice")
	}
	if *notice {
		if line := selfupdate.ReadNotice(config.StateDir()); line != "" {
			fmt.Fprintln(a.stdout, line)
		}
		return nil
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	track := a.cfg.Update.Track
	if *latest {
		track = selfupdate.TrackLatest
	}
	name := *repoFlag
	if name == "" {
		name = a.cfg.Update.Repo
	}
	if name == "" {
		name = a.ownRepo()
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	if name == "" {
		return a.updateUpstream(exe, track)
	}
	if _, ok := a.cfg.Repos[name]; !ok {
		return fail(exitRefused, "update repo %q is not in the registry (bmesh repos)", name)
	}
	_, repoPath, err := a.cfg.RepoPath(name)
	if err != nil {
		return err
	}
	clones := ""
	if cache, err := userCacheDir(); err == nil {
		clones = filepath.Join(cache, "bmesh", "update")
	} else if a.cfg.Runner.Checkouts == "" {
		return fmt.Errorf("no checkouts root and no cache dir: %w", err)
	}
	dir, unlock, err := selfupdate.MainCheckout(repoPath, a.cfg.Expand(a.cfg.Runner.Checkouts), clones, name)
	if err != nil {
		return err
	}
	defer unlock()
	t, err := selfupdate.Follow(dir, "HEAD", track, a.specDir())
	if err != nil {
		return err
	}
	return a.updateTo(dir, name, t, false, func() error { return selfupdate.Install(context.Background(), dir, exe, t.Commit) })
}

// updateUpstream installs what track follows on the module's main over exe
// (selfupdate.InstallModule) when no registry row applies: main and its
// tags are fetched into the bare repo the daemon's notice reads, so a
// target without this binary's revision is refused as a row's is.
func (a *app) updateUpstream(exe, track string) error {
	module := buildModule()
	if module == "" {
		return fail(exitRefused, "no repo to update from: pass --repo or set [update] repo")
	}
	cache, err := userCacheDir()
	if err != nil {
		return fmt.Errorf("no cache dir for %s's main: %w", module, err)
	}
	dir := selfupdate.UpstreamDir(cache)
	_, unlock, err := selfupdate.FetchUpstream(dir, module)
	if err != nil {
		return fmt.Errorf("read %s's main: %w", module, err)
	}
	defer unlock()
	t, err := selfupdate.Follow(dir, "refs/heads/main", track, a.specDir())
	if err != nil {
		return err
	}
	return a.updateTo(dir, module, t, true, func() error { return selfupdate.InstallModule(context.Background(), dir, module, exe, t) })
}

// updateTo compares this binary with t in dir and runs install when it is
// behind (or of unknown commit, a tag dir doesn't know included), printing
// `<old> -> <new>` (selfupdate.Label); the binary that is t prints `up to
// date`, one with t in its history `ahead of <t>, up to date`, and one t
// doesn't contain is refused. plugin adds the plugin line after a swap.
func (a *app) updateTo(dir, from string, t selfupdate.Target, plugin bool, install func() error) error {
	tagged := buildVersion().Own()
	own := selfupdate.OwnCommit(dir, tagged)
	label := selfupdate.Label(dir, own)
	if own == "" && tagged != "" {
		label = tagged
	}
	switch selfupdate.Stand(dir, own, t.Commit) {
	case selfupdate.Current:
		fmt.Fprintln(a.stdout, "up to date")
		a.restartLine(t.Commit)
		return clearNotice()
	case selfupdate.Ahead:
		fmt.Fprintf(a.stdout, "ahead of %s, up to date\n", selfupdate.Label(dir, t.Commit))
		a.restartLine(own)
		return clearNotice()
	case selfupdate.Apart:
		if own != "" {
			return fail(exitRefused, "%s's main (%s) does not contain this binary's %s; left alone", from,
				selfupdate.Label(dir, t.Commit), label)
		}
	}
	var refused *selfupdate.Refused
	if err := install(); errors.As(err, &refused) {
		return fail(exitRefused, "update: %v", err)
	} else if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s -> %s\n", label, selfupdate.Label(dir, t.Commit))
	if plugin {
		fmt.Fprintln(a.stdout, pluginLine)
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
