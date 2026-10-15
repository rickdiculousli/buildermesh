package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const leaseUsage = "usage: bmesh lease acquire <slot> [--ttl D] | lease renew <key> [--ttl D] | " +
	"lease release <key> | lease run <slot> [--ttl D] -- <cmd...>"

const (
	defaultTTL = 10 * time.Minute
	minTTL     = 3 * time.Second // renewals run every ttl/3
)

// leaseSignals delivers the signals that interrupt `lease run`; tests replace it.
var leaseSignals = func() (<-chan os.Signal, func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	return c, func() { signal.Stop(c) }
}

func (a *app) cmdLease(args []string) error {
	if len(args) == 0 {
		return fail(exitUsage, leaseUsage)
	}
	sub := args[0]
	if sub != "acquire" && sub != "renew" && sub != "release" && sub != "run" {
		return fail(exitUsage, leaseUsage)
	}
	fs := a.flags("lease " + sub)
	var ttl *time.Duration
	if sub != "release" {
		ttl = fs.Duration("ttl", defaultTTL, "how long the lease lasts unrenewed")
	}
	pos, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	if ttl != nil && *ttl < minTTL {
		return fail(exitUsage, "lease %s: --ttl must be at least %s", sub, minTTL)
	}
	switch {
	case sub == "run" && len(pos) >= 2 && kind.NameRE.MatchString(pos[0]):
		return a.leaseRun(pos[0], pos[1:], *ttl)
	case sub == "run" || len(pos) != 1:
		return fail(exitUsage, leaseUsage)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	switch sub {
	case "acquire":
		key, _, err := a.acquire(ctx, pos[0], *ttl)
		if err != nil {
			return err
		}
		fmt.Fprintln(a.stdout, key)
		return nil
	case "renew":
		holder, err := a.identityOrHost(ctx, "lease")
		if err != nil {
			return err
		}
		return leaseErr(mesh.RenewLease(ctx, a.client, pos[0], holder, *ttl, time.Now()), pos[0])
	default:
		holder, err := a.identityOrHost(ctx, "lease")
		if err != nil {
			return err
		}
		return leaseErr(mesh.ReleaseLease(ctx, a.client, pos[0], holder), pos[0])
	}
}

func leaseErr(err error, key string) error {
	if errors.Is(err, mesh.ErrNotHolder) {
		return fail(exitRefused, "lease %s is not held by you", key)
	}
	return err
}

// acquire takes a place in slot for this node (or host) and returns the key
// and the holder.
func (a *app) acquire(ctx context.Context, slot string, ttl time.Duration) (string, string, error) {
	if !kind.NameRE.MatchString(slot) {
		return "", "", fail(exitUsage, "invalid slot %q", slot)
	}
	capacity, err := slotCapacity(slot)
	if err != nil {
		return "", "", err
	}
	holder, err := a.identityOrHost(ctx, "lease")
	if err != nil {
		return "", "", err
	}
	key, err := mesh.AcquireLease(ctx, a.client, slot, holder, ttl, capacity, time.Now())
	if errors.Is(err, mesh.ErrBusy) {
		return "", "", fail(exitRefused, "slot %s is busy", slot)
	}
	return key, holder, err
}

// slotCapacity reads the slot's capacity from the cwd's repo config; 1 when
// the cwd is not in a repo or the repo does not list the slot.
func slotCapacity(slot string) (int, error) {
	top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return 1, nil
	}
	rc, err := kind.LoadRepoConfig(strings.TrimSpace(string(top)))
	if err != nil {
		return 0, err
	}
	if n := rc.Slots[slot]; n > 0 {
		return n, nil
	}
	return 1, nil
}

// leaseRun holds a lease on slot while argv runs, renewing every ttl/3. The
// command runs in its own process group so the whole group can be killed. A
// failed renew kills the group and exits 1 "lease lost"; SIGINT or SIGTERM
// kills it, releases the lease and exits 1 "interrupted"; otherwise the lease
// is released and the command's exit code is returned.
func (a *app) leaseRun(slot string, argv []string, ttl time.Duration) error {
	ctx, cancel := cmdCtx()
	key, holder, err := a.acquire(ctx, slot, ttl)
	cancel()
	if err != nil {
		return err
	}
	sigs, stopSigs := leaseSignals()
	defer stopSigs()
	runCtx, kill := context.WithCancel(context.Background())
	defer kill()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.stdin, a.stdout, a.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second // a killed command's children may hold its output open
	var lost, interrupted atomic.Bool
	stop := make(chan struct{})
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		t := time.NewTicker(ttl / 3)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-sigs:
				interrupted.Store(true)
				kill()
				return
			case <-t.C:
				ctx, cancel := cmdCtx()
				err := mesh.RenewLease(ctx, a.client, key, holder, ttl, time.Now())
				cancel()
				if err != nil {
					lost.Store(true)
					kill()
					return
				}
			}
		}
	}()
	runErr := cmd.Run()
	close(stop)
	<-watched
	if lost.Load() {
		return fail(exitError, "lease lost")
	}
	ctx, cancel = cmdCtx()
	defer cancel()
	if err := mesh.ReleaseLease(ctx, a.client, key, holder); err != nil {
		return leaseErr(err, key)
	}
	if interrupted.Load() {
		return fail(exitError, "interrupted")
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		code := ee.ExitCode()
		if code < 0 {
			code = exitError
		}
		return &exitErr{code: code}
	}
	return runErr
}
