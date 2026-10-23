package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// hubCheckEvery is how often bmesh hub reads serve.json for a release to
// restart onto; hubReexec and hubSignals are reexec and the signals that
// stop the hub. Tests shorten and stub them.
var (
	hubCheckEvery = time.Minute
	hubReexec     = reexec
	hubSignals    = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	}
)

// cmdHub runs this box's NATS hub or leaf from [hub] until SIGTERM or
// SIGINT. It is its own process, apart from serve, so serve's restarts for
// an update never drop the mesh. Off Windows, once serve runs a release
// newer than the hub's binary, the hub stops and re-execs its executable
// (serveRelease).
func (a *app) cmdHub(args []string) error {
	fs := a.flags("hub")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "usage: bmesh hub")
	}
	if err := a.loadServiceConfig(); err != nil {
		return err
	}
	o, err := hubOptions(a.cfg)
	if err != nil {
		return err
	}
	exe, started := a.hubBinary()
	o.Logs = true
	h, err := mesh.StartHub(o)
	if err != nil {
		return fmt.Errorf("hub: %w", err)
	}
	if o.Remote == "" {
		fmt.Fprintf(a.stderr, "bmesh hub: hub %s on %s, leaves on %s, JetStream domain %s, store %s\n",
			o.Name, o.Listen, orNone(o.LeafListen), o.Domain, o.Store)
	} else {
		fmt.Fprintf(a.stderr, "bmesh hub: leaf %s on %s, dialing %s\n", o.Name, o.Listen, o.Remote)
	}
	// hub.json is how hosts --here, mesh-status and update tell a hub
	// left on an older binary than serve.
	if remove, err := selfupdate.WriteProc(config.StateDir(), "hub", buildVersion()); err != nil {
		fmt.Fprintf(a.stderr, "bmesh hub: no hub.json: %v\n", err)
	} else {
		defer remove()
	}
	ctx, stop := hubSignals()
	defer stop()
	var check <-chan time.Time
	if exe != "" {
		t := time.NewTicker(hubCheckEvery)
		defer t.Stop()
		check = t.C
	}
	self := selfupdate.RecordOf(buildVersion())
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(a.stderr, "bmesh hub: stopping")
			h.Shutdown()
			return nil
		case <-check:
			rel := serveRelease(self, exe, started)
			if rel == "" {
				continue
			}
			fmt.Fprintf(a.stderr, "bmesh hub: serve runs %s, restarting as %s\n", rel, exe)
			stop()
			h.Shutdown()
			return hubReexec(exe)
		}
	}
}

// hubBinary is the path the hub re-execs and the file there as it started,
// read before an update moves the running file aside; "" on Windows, where
// reexec only exits and no service starts the hub again, or with no path.
func (a *app) hubBinary() (string, os.FileInfo) {
	if runtime.GOOS == "windows" {
		return "", nil
	}
	exe, err := executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	var fi os.FileInfo
	if err == nil {
		fi, err = os.Stat(exe)
	}
	if err != nil {
		fmt.Fprintf(a.stderr, "bmesh hub: no binary path, so no restart onto serve's release: %v\n", err)
		return "", nil
	}
	return exe, fi
}

// serveRelease is the release this box's serve runs when the hub, at self,
// is to restart onto it: serve's record names a release (not a commit past
// one, as track = "latest" runs), the hub is older (HubBehind), and the file at exe
// is no longer the one the hub started from, since re-exec'ing that would
// only start this binary again, once a check. "" otherwise.
func serveRelease(self selfupdate.ProcRecord, exe string, started os.FileInfo) string {
	serve, ok := selfupdate.ReadProc(config.StateDir(), "serve")
	if !ok || !selfupdate.IsRelease(serve.Release) || !selfupdate.HubBehind(self, serve) {
		return ""
	}
	if now, err := os.Stat(exe); err != nil || os.SameFile(now, started) {
		return ""
	}
	return serve.Release
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// hubOptions is the server [hub] describes, or the refusal naming the key
// that keeps it from starting: role unset, a listen address on every
// interface or without a host, a leaf without remote, or no token.
func hubOptions(c *config.Config) (mesh.HubOptions, error) {
	h := c.Hub
	o := mesh.HubOptions{Name: c.HostName(), Listen: h.Listen}
	if h.Role == "" {
		return o, fail(exitRefused, "hub: [hub] role is unset: set it to hub or leaf (docs/manual.md: Several boxes); a single box needs no bmesh hub, since bmesh serve embeds it")
	}
	if err := boundAddr("listen", h.Listen); err != nil {
		return o, err
	}
	switch h.Role {
	case "hub":
		if h.LeafListen != "" {
			if err := boundAddr("leaf_listen", h.LeafListen); err != nil {
				return o, err
			}
		}
		maxStore, err := config.ParseSize(h.MaxFileStore)
		if err != nil {
			return o, fail(exitRefused, "hub: [hub] max_file_store: %v", err)
		}
		maxMem, err := config.ParseSize(h.MaxMemoryStore)
		if err != nil {
			return o, fail(exitRefused, "hub: [hub] max_memory_store: %v", err)
		}
		o.Domain, o.Store, o.MaxStore, o.MaxMemory, o.LeafListen = c.Mesh.Domain, c.HubStore(), maxStore, maxMem, h.LeafListen
	case "leaf":
		if h.Remote == "" {
			return o, fail(exitRefused, "hub: [hub] remote is unset: a leaf dials the hub's leaf port, nats://<hub tailnet ip>:7422")
		}
		o.Remote = h.Remote
	}
	if c.Mesh.TokenFile == "" {
		return o, fail(exitRefused, "hub: [mesh] token_file is unset: the hub and every box share one token (docs/manual.md: Several boxes)")
	}
	tok, err := c.Token()
	if err != nil {
		return o, fail(exitRefused, "hub: [mesh] token_file %s: %v", c.Mesh.TokenFile, err)
	}
	if tok == "" {
		return o, fail(exitRefused, "hub: [mesh] token_file %s is empty", c.Mesh.TokenFile)
	}
	o.Token = tok
	return o, nil
}

// boundAddr refuses a [hub] address that is empty, unparsable, or on every
// interface (no host, 0.0.0.0 or ::): the hub listens on its tailnet
// address, a leaf on 127.0.0.1, since the token is the only auth.
func boundAddr(key, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	switch {
	case addr == "":
		return fail(exitRefused, "hub: [hub] %s is unset: want <tailnet ip>:port on a hub, 127.0.0.1:4222 on a leaf", key)
	case err != nil:
		return fail(exitRefused, "hub: [hub] %s %q: %v", key, addr, err)
	}
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		return fail(exitRefused, "hub: [hub] %s %q listens on every interface: name the tailnet address (or 127.0.0.1 on a leaf), since the token is the only auth", key, addr)
	}
	return nil
}
