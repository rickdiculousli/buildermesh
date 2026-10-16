package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// userCacheDir and executable are os.UserCacheDir and os.Executable; tests
// point them into their temp dir.
var (
	userCacheDir = os.UserCacheDir
	executable   = os.Executable
)

// cmdUpdate builds the update repo's main over this binary, as the daemon's
// [update] does, without a daemon.
func (a *app) cmdUpdate(args []string) error {
	fs := a.flags("update")
	repoFlag := fs.String("repo", "", "registry name of the repo to build from (default [update] repo)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "usage: bmesh update [--repo R]")
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
	if name == "" {
		return fail(exitRefused, "no repo to update from: pass --repo or set [update] repo")
	}
	if _, ok := a.cfg.Repos[name]; !ok {
		return fail(exitRefused, "update repo %q is not in the registry (bmesh repos)", name)
	}
	_, repoPath, err := a.cfg.RepoPath(name)
	if err != nil {
		return err
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
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
	if head == own {
		fmt.Fprintln(a.stdout, "up to date")
		return nil
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
	return nil
}

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
