package daemon

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
	"github.com/rickdiculousli/buildermesh/internal/upgrade"
)

// CanRestart is the word a daemon's started event carries in can when it
// restarts itself onto a newer binary `bmesh update` swapped in at its
// path (restartsItself), so update says so instead of naming the restart.
const CanRestart = "restarts"

// historyRepo is the registry row whose main the swap check reads history
// in, as `bmesh update` builds from it: [update] repo, else the row of
// this binary's own module; "" for neither, when the bare upstream under
// the cache dir is read instead.
func historyRepo(d Deps) string {
	return cmp.Or(d.Cfg.Update.Repo, d.Cfg.ModuleRepo(d.Module))
}

// restartsItself reports whether the routine loop's swap check
// (checkSwapped) can run: routines run, with a state dir for the lock and
// the walk record, a binary of known revision to restart, a restart that
// starts the new binary (an exec, or `serve --restart` on Windows) and
// history to judge newer by.
func restartsItself(d Deps) bool {
	d.defaults()
	switch {
	case len(d.Cfg.Repos) == 0, d.Checkouts == "", d.StateDir == "", d.Exe == "", d.Restart == nil,
		d.Revision == "" && d.Tag == "", d.GOOS == "windows" && !d.Supervised:
		return false
	}
	if repo := historyRepo(d); repo != "" {
		_, path, err := d.Cfg.RepoPath(repo)
		if err != nil {
			return false
		}
		_, err = os.Stat(path)
		return err == nil
	}
	_, err := config.CacheDir()
	return d.Module != "" && err == nil
}

// fileStamp is what the swap check compares a file by: its modification
// time and size; the zero stamp is a file that isn't there.
type fileStamp struct {
	mod  time.Time
	size int64
}

func stampOf(path string) fileStamp {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{fi.ModTime(), fi.Size()}
}

// swapCheck is what the swap check keeps between rescans.
type swapCheck struct {
	exe      fileStamp // the binary at Exe when its version was last read
	rev      string    // what that version named (selfupdate.Installed)
	fetched  string    // the revision the upstream was last fetched for
	heldAt   string    // the binary, walk record and config a refusal was said for
	heldLine string    // that refusal, kept as the notice while it stands
}

// configStamp is the stamps of the files `config check` reads: config.toml
// and repos.toml beside it; none without a config file.
func (d *Deps) configStamp() [2]fileStamp {
	p := d.Cfg.Path()
	if p == "" {
		return [2]fileStamp{}
	}
	return [2]fileStamp{stampOf(p), stampOf(filepath.Join(filepath.Dir(p), "repos.toml"))}
}

// checkSwapped restarts this daemon onto a binary `bmesh update` swapped
// in at Exe: one whose revision, in the history of dir (historyRepo's main
// checkout, else the upstream, fetched once per binary that it lacks),
// contains this daemon's and is another commit. Its version is read only
// when the file changed. It waits while `bmesh update` holds update.lock,
// and refuses a binary whose walk is pending or whose config check fails,
// saying so once (hold) and again only when the binary, the walk record
// or the config changes. A restart drains first, as the self-update does; it reports
// whether it restarted.
func (r *routineSet) checkSwapped(dir string) bool {
	d, s := r.d, &r.swap
	if at := stampOf(d.Exe); at != s.exe {
		s.exe, s.rev = at, selfupdate.Installed(r.ctx, d.Exe)
	}
	if s.rev == "" || selfupdate.Same(s.rev, d.Revision) || s.rev == d.Tag {
		return false
	}
	if r.histRepo == "" {
		cache, err := config.CacheDir()
		if err != nil {
			return false
		}
		dir = selfupdate.UpstreamDir(cache)
		if next := selfupdate.OwnCommit(dir, s.rev); !selfupdate.Contains(dir, next, next) && s.fetched != s.rev {
			s.fetched = s.rev
			_, unlock, err := selfupdate.FetchUpstream(dir, d.Module)
			if err != nil {
				d.Log.Warn("update: read main", "err", err)
				return false
			}
			unlock()
		}
	}
	if dir == "" {
		return false
	}
	own, next := selfupdate.OwnCommit(dir, cmp.Or(d.Revision, d.Tag)), selfupdate.OwnCommit(dir, s.rev)
	if next == "" || selfupdate.Stand(dir, own, next) != selfupdate.Behind {
		return false // unknown here, older, or another line: left as it is
	}
	unlock, ok, err := selfupdate.TryLockUpdate(d.StateDir)
	if err != nil {
		d.Log.Warn("update: lock", "err", err)
		return false
	}
	if !ok {
		return false // bmesh update is still swapping or walking
	}
	defer unlock()
	label := selfupdate.Label(dir, next)
	key := fmt.Sprint(s.exe, stampOf(upgrade.InstalledPath(d.StateDir)), d.configStamp())
	if key == s.heldAt {
		d.writeNotice("bmesh: " + s.heldLine)
		return false
	}
	why := ""
	if line := upgrade.Pending(d.StateDir, selfupdate.Describe(dir, next)); line != "" {
		why = "its walk is unfinished (" + line + ")"
	} else {
		pre := []string{}
		if p := d.Cfg.Path(); p != "" {
			pre = append(pre, "--config", p)
		}
		if out, err := r.runBuilt(d.Exe, append(pre, "config", "check")...); err != nil {
			why = "its config check failed: " + lastLine(out, err)
		}
	}
	if why != "" {
		s.heldAt, s.heldLine = key, fmt.Sprintf("%s is at %s but %s; not restarting onto it", label, d.Exe, why)
		r.hold(&held{s.heldLine})
		return false
	}
	if s.heldLine != "" {
		d.writeNotice("")
	}
	d.Log.Info("update: "+selfupdate.Label(dir, own)+" -> "+label+" (swapped in by bmesh update); restarting", "exe", d.Exe)
	r.drain()
	d.Log.Info("update: drained; restarting")
	d.Restart()
	return true
}
