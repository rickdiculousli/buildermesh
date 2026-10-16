package main

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// readBuildInfo is debug.ReadBuildInfo; tests give the binary a revision.
var readBuildInfo = debug.ReadBuildInfo

func buildVersion() selfupdate.Version { return selfupdate.FromBuildInfo(readBuildInfo()) }

// cmdVersion prints `bmesh <rev8> <time> [modified]`, or `bmesh unknown`.
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
func (a *app) versionHint(ctx context.Context) {
	own := buildVersion().Revision
	if own == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, hintWait)
	defer cancel()
	s, ok, err := mesh.Last(ctx, a.client, mesh.DaemonSubject(a.cfg.HostName(), "started"))
	if err != nil {
		return
	}
	if !ok {
		s, ok, err = mesh.Last(ctx, a.client, "bm.daemon.*.started")
		if err != nil || !ok {
			return
		}
	}
	var ev mesh.Event
	if json.Unmarshal(s.Data, &ev) != nil || ev.Text == "" || ev.Text == selfupdate.Unknown || ev.Text == own {
		return
	}
	host := strings.TrimSuffix(strings.TrimPrefix(s.Subject, "bm.daemon."), ".started")
	a.hint = fmt.Sprintf("bmesh: %s runs %s, this is %s; run bmesh update", host,
		selfupdate.Short(ev.Text), selfupdate.Short(own))
}
