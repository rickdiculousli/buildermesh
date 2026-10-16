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

// cmdUpdate builds the update repo's main over this binary, as the daemon's
// [update] does, without a daemon; with no repo to build from it installs
// the module's main. --notice prints the daemon's notice and nothing else.
func (a *app) cmdUpdate(args []string) error {
	fs := a.flags("update")
	repoFlag := fs.String("repo", "", "registry name of the repo to build from (default [update] repo)")
	notice := fs.Bool("notice", false, "print the daemon's notice (<state dir>/update.txt) and never touch the network")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || (*notice && *repoFlag != "") {
		return fail(exitUsage, "usage: bmesh update [--repo R] | update --notice")
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
		return a.updateUpstream(exe)
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
	head, err := selfupdate.Head(dir)
	if err != nil {
		return err
	}
	own := buildVersion().Revision
	if selfupdate.Same(head, own) {
		fmt.Fprintln(a.stdout, "up to date")
		return clearNotice()
	}
	if own != "" && !selfupdate.Contains(dir, own, head) {
		return fail(exitRefused, "%s's main (%s) does not contain this binary's %s; left alone", name,
			selfupdate.Short(head), selfupdate.Short(own))
	}
	var refused *selfupdate.Refused
	if err := selfupdate.Install(context.Background(), dir, exe, head); errors.As(err, &refused) {
		return fail(exitRefused, "update: %v", err)
	} else if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s -> %s\n", selfupdate.Short(own), selfupdate.Short(head))
	return clearNotice()
}

// updateUpstream installs the module's main over exe (selfupdate.InstallModule)
// when no registry row applies: main is fetched into the bare repo the
// daemon's notice reads, so a main without this binary's revision is
// refused as a row's is.
func (a *app) updateUpstream(exe string) error {
	module := buildModule()
	if module == "" {
		return fail(exitRefused, "no repo to update from: pass --repo or set [update] repo")
	}
	cache, err := userCacheDir()
	if err != nil {
		return fmt.Errorf("no cache dir for %s's main: %w", module, err)
	}
	dir := selfupdate.UpstreamDir(cache)
	head, unlock, err := selfupdate.FetchUpstream(dir, module)
	if err != nil {
		return fmt.Errorf("read %s's main: %w", module, err)
	}
	defer unlock()
	own := buildVersion().Revision
	if selfupdate.Same(head, own) {
		fmt.Fprintln(a.stdout, "up to date")
		return clearNotice()
	}
	if own != "" && !selfupdate.Contains(dir, own, head) {
		return fail(exitRefused, "%s's main (%s) does not contain this binary's %s; left alone", module,
			selfupdate.Short(head), selfupdate.Short(own))
	}
	var refused *selfupdate.Refused
	if err := selfupdate.InstallModule(context.Background(), module, exe, head); errors.As(err, &refused) {
		return fail(exitRefused, "update: %v", err)
	} else if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s -> %s\n", selfupdate.Short(own), selfupdate.Short(head))
	fmt.Fprintln(a.stdout, pluginLine)
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
