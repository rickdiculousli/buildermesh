package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// fixStartTimeout bounds one `bmesh fix`, as fix bounds itself.
const fixStartTimeout = 10 * time.Minute

// fixerLoop starts bmesh's own fixer when its feedback waits: it runs
// fixerOnce on every feedback message published (bm.feedback.>) and on
// each tick, one at a time, so a start never stalls the other loops and a
// burst of feedback is one decision.
func fixerLoop(ctx context.Context, d Deps, interval time.Duration) {
	kick := make(chan struct{}, 1)
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			ch, err := mesh.Follow(ctx, d.Client, mesh.FeedbackSubject(">"))
			if err != nil {
				d.Log.Warn("fixer: follow feedback", "err", err)
				select {
				case <-ctx.Done():
				case <-time.After(followRetry):
				}
				continue
			}
			for range ch {
				select {
				case kick <- struct{}{}:
				default:
				}
			}
		}
	}()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		fixerOnce(ctx, d)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-kick:
		}
	}
}

// fixerOnce starts bmesh's own fixer when one is due, deciding from what
// exists, never from what an earlier pass meant to do. Bmesh's own repo is
// the registry row whose go.mod declares this binary's module (with none,
// nothing happens), a ship row, and only the daemon of its host acts (any
// daemon when the row names none, as fix runs where it is run). A fixer is
// due when none is live (neither done nor blocked), the newest isn't
// blocked (its issues wait on the operator), and an open issue naming the
// repo (spec.OpenIssues on its main) is newer than the newest fixer's
// start: an issue a fixer had at its start and left is not started on
// again. The start is claimed by a fixer daemon event with Nats-Msg-Id
// fixer-<the next fixer's path>, so of two daemons one starts it; a start
// that failed is tried again once the stream's duplicate window has passed.
func fixerOnce(ctx context.Context, d Deps) {
	own := d.Cfg.ModuleRepo(d.Module)
	if own == "" {
		return
	}
	row := d.Cfg.Repos[own]
	if row.Mode != "ship" || (row.Host != "" && row.Host != d.Host) {
		return
	}
	_, repoPath, err := d.Cfg.RepoPath(own)
	if err != nil {
		d.Log.Warn("fixer: repo", "repo", own, "err", err)
		return
	}
	fx, open, err := readFixQueue(ctx, d.Client, own, repoPath, d.Cfg.Spec.Glob)
	if err != nil {
		d.Log.Warn("fixer: read", "repo", own, "err", err)
		return
	}
	if !FixerDue(fx, open) {
		return
	}
	path := fmt.Sprintf("%s-fix-%d", mesh.RepoToken(own), fx.Next)
	claim := mesh.Event{From: d.from(), Event: "fixer", To: path, Text: fmt.Sprintf("%d open for %s", len(open), own),
		At: d.Now().UTC().Format(time.RFC3339)}
	dup, err := mesh.PublishEventID(ctx, d.Client, claim, "fixer-"+path)
	if err != nil {
		d.Log.Warn("fixer: claim", "path", path, "err", err)
		return
	}
	if dup {
		return
	}
	start := d.StartFixer
	if start == nil {
		start = func(ctx context.Context) (string, error) { return execFix(ctx, d.Exe, d.Cfg.Path()) }
	}
	sctx, cancel := context.WithTimeout(ctx, fixStartTimeout)
	defer cancel()
	out, err := start(sctx)
	if err != nil {
		d.Log.Warn("fixer: start", "path", path, "err", err, "out", strings.TrimSpace(out))
		return
	}
	d.Log.Info("fixer: started", "out", strings.TrimSpace(out))
}

// readFixQueue reads what decides bmesh's own fixer for repo: its fixers
// (mesh.ReadFixers) and the open issues naming it, the feedback no spec or
// fix commit on repoPath's main names (spec.OpenIssues).
func readFixQueue(ctx context.Context, c *mesh.Client, repo, repoPath, glob string) (mesh.Fixers, []mesh.Feedback, error) {
	nodes, err := mesh.ListNodes(ctx, c)
	if err != nil {
		return mesh.Fixers{}, nil, err
	}
	states, err := mesh.ReadAllStates(ctx, c)
	if err != nil {
		return mesh.Fixers{}, nil, err
	}
	pending, err := mesh.PendingFeedback(ctx, c)
	if err != nil {
		return mesh.Fixers{}, nil, err
	}
	all, err := mesh.ListAllFeedback(ctx, c)
	if err != nil {
		return mesh.Fixers{}, nil, err
	}
	specs, err := spec.ScanRef(repoPath, glob, "main")
	if err != nil {
		return mesh.Fixers{}, nil, err
	}
	fixed, err := spec.FixedSeqs(repoPath, "main")
	if err != nil {
		return mesh.Fixers{}, nil, err
	}
	return mesh.ReadFixers(nodes, states, pending, repo), spec.OpenIssues(all, repo, specs, fixed), nil
}

// FixerDue reports whether the daemon starts a fixer for open, the open
// issues of fx's repo: none is live, the newest isn't blocked, and an
// issue is newer than the newest fixer's start (or there was none).
func FixerDue(fx mesh.Fixers, open []mesh.Feedback) bool {
	if fx.Live.Path != "" || fx.LastState.State == "blocked" {
		return false
	}
	return len(unseen(fx, open)) > 0
}

// unseen is the issues in open no fixer has had: published at or after the
// newest fixer's start, or all of them when there was none or a time
// doesn't parse.
func unseen(fx mesh.Fixers, open []mesh.Feedback) []mesh.Feedback {
	started, err := time.Parse(time.RFC3339, fx.Last.StartedAt)
	if fx.Last.Path == "" || err != nil {
		return open
	}
	var out []mesh.Feedback
	for _, f := range open {
		if at, err := time.Parse(time.RFC3339, f.At); err != nil || !at.Before(started) {
			out = append(out, f)
		}
	}
	return out
}

// execFix runs `bmesh fix` with exe, this binary, as the operator would,
// on the daemon's own config file (config, when it read one).
func execFix(ctx context.Context, exe, config string) (string, error) {
	if exe == "" {
		return "", fmt.Errorf("no bmesh binary path")
	}
	var args []string
	if config != "" {
		args = append(args, "--config", config)
	}
	out, err := exec.CommandContext(ctx, exe, append(args, "fix")...).CombinedOutput()
	return string(out), err
}
