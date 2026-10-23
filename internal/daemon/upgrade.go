package daemon

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/notify"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
	"github.com/rickdiculousli/buildermesh/internal/upgrade"
)

// gateTimeout bounds each of the new binary's own checks before a swap.
const gateTimeout = 2 * time.Minute

// held is a self-update the new binary's checks stopped before its swap:
// its config check failed, or the notes it would walk need an answer or a
// yes. The daemon never guesses either; line says what waits.
type held struct{ line string }

func (h *held) Error() string { return h.line }

// gatePlan is the counts of `update --plan --json` the gate reads.
type gatePlan struct {
	Asks    int `json:"asks"`
	Steps   int `json:"steps"`
	Confirm int `json:"confirm"`
}

// walkedTo is the revision this box last walked the notes to, which a
// swap from own plans from: own, unless notes are pending since this box's
// record (upgrade.Pending), then the recorded revision, else the binary a
// swap replaced (<state dir>/previous), as bmesh update's pending walk
// reads them; own again when that one isn't in own's history.
func (r *routineSet) walkedTo(dir, own string) string {
	sd := r.d.StateDir
	if sd == "" || own == "" || upgrade.Pending(sd, selfupdate.Describe(dir, own)) == "" {
		return own
	}
	from := ""
	if rec, ok := upgrade.ReadInstalled(sd); ok {
		from = rec.Revision
	} else if p := upgrade.ReadPrevious(sd); p != "" {
		from = selfupdate.OwnCommit(dir, p)
	}
	if from == "" || selfupdate.Stand(dir, from, own) != selfupdate.Behind {
		return own
	}
	return from
}

// gate is the self-update's check of the binary built for t, before the
// swap: the new binary loads this box's config (`config check`), and plans
// the walk from where this box last walked to (walkedTo, own unless notes
// are still pending) with main read from dir (`update --plan --json`). A
// rejected config, or a walk with an ask or a step that asks first, holds
// the update (held); steps reports whether the walk has anything at all,
// which the new binary's first start then names as pending.
func (r *routineSet) gate(dir, own string, t selfupdate.Target, steps *bool) selfupdate.Gate {
	from := r.walkedTo(dir, own)
	return func(built string) error {
		next := selfupdate.Label(dir, t.Commit)
		pre := []string{}
		if p := r.d.Cfg.Path(); p != "" {
			pre = append(pre, "--config", p)
		}
		out, err := r.runBuilt(built, append(pre, "config", "check")...)
		if err != nil {
			return &held{fmt.Sprintf("update to %s: config check failed: %s; keeping this binary", next, lastLine(out, err))}
		}
		out, err = r.runBuilt(built, append(pre, "--json", "update", "--plan", "--from", from, "--to", t.Commit, "--dir", dir)...)
		var p gatePlan
		if err != nil || strings.TrimSpace(out) == "" || json.Unmarshal([]byte(out), &p) != nil {
			// A build that can't plan (none can't, since this one) walks
			// nothing the daemon would have to answer.
			return nil
		}
		*steps = p.Asks+p.Steps > 0
		var waits []string
		if p.Asks > 0 {
			waits = append(waits, fmt.Sprintf("%d %s", p.Asks, plural(p.Asks, "ask")))
		}
		if p.Confirm > 0 {
			waits = append(waits, fmt.Sprintf("%d %s that %s first", p.Confirm, plural(p.Confirm, "step"), map[bool]string{true: "asks", false: "ask"}[p.Confirm == 1]))
		}
		if len(waits) > 0 {
			return &held{fmt.Sprintf("update to %s waits on bmesh update (%s)", next, strings.Join(waits, ", "))}
		}
		return nil
	}
}

// runBuilt runs the built binary with args, its stdout returned.
func (r *routineSet) runBuilt(bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(r.ctx, gateTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.WaitDelay = 5 * time.Second
	var errb strings.Builder
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		return errb.String(), err
	}
	return string(out), nil
}

// lastLine is the last line of a failed run's stderr, without bmesh's
// prefix, else the error.
func lastLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return strings.TrimPrefix(strings.TrimPrefix(l, "bmesh: "), "config check failed: ")
	}
	return err.Error()
}

// hold logs and pushes a held update once per target, and leaves it as
// the notice for this box's sessions.
func (r *routineSet) hold(h *held) {
	d := r.d
	d.Log.Warn("update: " + h.line)
	d.writeNotice("bmesh: " + h.line)
	if d.Cfg.Notify.Ntfy != "" || d.Cfg.Notify.Command != "" {
		ctx, cancel := r.settleCtx()
		defer cancel()
		if err := deliver(ctx, d, notify.Headers{Title: "bmesh update waits on " + d.Host, Tags: "warning"}, h.line); err != nil {
			d.Log.Warn("update: push held update", "err", err)
		}
	}
}

// swapped records a swap the daemon made: the revision it replaced as
// <state dir>/previous, for the walk the new binary asks for, unless notes
// from before it are still pending (walkedTo), whose walk starts earlier;
// and, when the walk the gate planned had nothing to do, the new binary as
// installed.
func (r *routineSet) swapped(dir, own string, t selfupdate.Target, steps bool) {
	d := r.d
	if d.StateDir == "" {
		return
	}
	prev := cmp.Or(d.Revision, d.Tag, own)
	if prev != "" && r.walkedTo(dir, own) == own {
		if err := upgrade.WritePrevious(d.StateDir, prev); err != nil {
			d.Log.Warn("update: write previous", "err", err)
		}
	}
	if !steps {
		rec := upgrade.Record{Version: selfupdate.Label(dir, t.Commit), Revision: t.Commit, At: d.Now().UTC().Format(time.RFC3339)}
		if err := upgrade.WriteInstalled(d.StateDir, rec); err != nil {
			d.Log.Warn("update: write installed", "err", err)
		}
	}
}

// pending logs, and pushes once per line, that this binary has upgrade
// notes to walk (upgrade.Pending): a binary that an older one swapped in
// without walking them. The line pushed last is kept in
// <state dir>/pending-pushed.
func pending(ctx context.Context, d Deps) {
	if d.StateDir == "" {
		return
	}
	line := upgrade.Pending(d.StateDir, d.Release)
	if line == "" {
		return
	}
	d.Log.Warn("update: " + line)
	mark := d.StateDir + string(os.PathSeparator) + "pending-pushed"
	if b, err := os.ReadFile(mark); err == nil && strings.TrimSpace(string(b)) == line {
		return
	}
	if d.Cfg.Notify.Ntfy == "" && d.Cfg.Notify.Command == "" {
		return
	}
	if err := deliver(ctx, d, notify.Headers{Title: "bmesh on " + d.Host + ": upgrade notes pending", Tags: "warning"}, line); err != nil {
		d.Log.Warn("update: push pending notes", "err", err)
		return
	}
	if err := os.WriteFile(mark, []byte(line+"\n"), 0o600); err != nil {
		d.Log.Warn("update: write pending-pushed", "err", err)
	}
}
