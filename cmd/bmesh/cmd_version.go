package main

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// readBuildInfo is debug.ReadBuildInfo; tests give the binary a revision.
var readBuildInfo = debug.ReadBuildInfo

func buildVersion() selfupdate.Version { return selfupdate.FromBuildInfo(readBuildInfo()) }

// buildModule is the module this binary was built from, "" when it does not
// say.
func buildModule() string {
	if bi, ok := readBuildInfo(); ok && bi != nil {
		return bi.Main.Path
	}
	return ""
}

// cmdVersion prints `bmesh <rev8> <time> [modified]`, or `bmesh unknown`;
// a build stamped with a release prints `bmesh <release> (<rev8> <time>)`.
func (a *app) cmdVersion(args []string) error {
	fs := a.flags("version")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "usage: bmesh version")
	}
	fmt.Fprintln(a.stdout, "bmesh "+buildVersion().String())
	return nil
}

// hintWait bounds the read behind the revision hint.
const hintWait = 2 * time.Second

// versionHint sets the one stderr line run prints after the command's own
// output when this host's daemon (its newest started), or the hub's newest
// daemon to start when this host runs none, runs another revision than this
// binary. There is none when either side does not know its revision or the
// read fails: it is a hint, never a refusal, and never changes the exit code.
// Revisions compare by prefix, so a module install's 12 hex match the full
// commit. When this host's own daemon runs a commit older than this
// binary's, by the commit times both carry, the binary is current and the
// daemon is what needs a restart, so the hint says that instead.
func (a *app) versionHint(ctx context.Context) {
	bin := buildVersion()
	if bin.Revision == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, hintWait)
	defer cancel()
	ev, ownDaemon, err := a.ownStarted(ctx)
	if err != nil {
		return
	}
	host := a.cfg.HostName()
	if !ownDaemon {
		s, ok, err := mesh.Last(ctx, a.client, "bm.daemon.*.started")
		if err != nil || !ok || json.Unmarshal(s.Data, &ev) != nil {
			return
		}
		host = strings.TrimSuffix(strings.TrimPrefix(s.Subject, "bm.daemon."), ".started")
	}
	if ev.Text == "" || ev.Text == selfupdate.Unknown || selfupdate.Same(ev.Text, bin.Revision) {
		return
	}
	if ownDaemon && older(ev.Body, bin.Time) {
		a.hint = fmt.Sprintf("bmesh: this box's daemon runs %s, this binary is %s; restart the daemon: %s",
			selfupdate.Short(ev.Text), selfupdate.Short(bin.Revision), restartCommand(hostGOOS))
		return
	}
	a.hint = fmt.Sprintf("bmesh: %s runs %s, this is %s; run bmesh update", host,
		selfupdate.Short(ev.Text), selfupdate.Short(bin.Revision))
}

// ownStarted is this host's daemon's newest started event; ok is false when
// it has none (or one that doesn't decode).
func (a *app) ownStarted(ctx context.Context) (ev mesh.Event, ok bool, err error) {
	s, ok, err := mesh.Last(ctx, a.client, mesh.DaemonSubject(a.cfg.HostName(), "started"))
	if err != nil || !ok {
		return mesh.Event{}, false, err
	}
	if json.Unmarshal(s.Data, &ev) != nil {
		return mesh.Event{}, false, nil
	}
	return ev, true, nil
}

// older reports whether commit time a is before b, both RFC 3339; an
// unknown or unreadable time is never older.
func older(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	return errA == nil && errB == nil && ta.Before(tb)
}

// hostGOOS is the OS whose service manager the restart names; tests set
// another.
var hostGOOS = runtime.GOOS

// restartCommand restarts bmesh serve as the unit `init --service` writes
// on goos runs it: the systemd user unit, the launchd agent, or on Windows
// the logon task, whose supervisor runs detached, so every bmesh.exe is
// stopped before the task starts it again (in cmd). Another OS has no unit.
func restartCommand(goos string) string {
	switch goos {
	case "linux":
		return "systemctl --user restart bmesh"
	case "darwin":
		return "launchctl kickstart -k gui/$(id -u)/com.buildermesh.bmesh"
	case "windows":
		return "taskkill /F /IM bmesh.exe & schtasks /Run /TN buildermesh"
	}
	return "stop bmesh serve and start it again"
}

// restartLine prints, after update's own output, the restart this host's
// daemon needs when its newest started names another revision than rev, the
// binary now at the path. update never restarts it: a restart kills the
// daemon's running jobs, and only its own drain hands them back. No hub, no
// started for this host or a daemon of unknown revision: nothing.
func (a *app) restartLine(rev string) {
	ctx, cancel := context.WithTimeout(context.Background(), hintWait)
	defer cancel()
	if err := a.connect(ctx, "update"); err != nil {
		return
	}
	a.hint = "" // it compares this process's own revision, which the swap left behind
	ev, ok, err := a.ownStarted(ctx)
	if err != nil || !ok || ev.Text == "" || ev.Text == selfupdate.Unknown || selfupdate.Same(ev.Text, rev) {
		return
	}
	fmt.Fprintf(a.stdout, "this box's daemon still runs %s; restart it so it runs %s: %s\n",
		selfupdate.Short(ev.Text), selfupdate.Short(rev), restartCommand(hostGOOS))
}
