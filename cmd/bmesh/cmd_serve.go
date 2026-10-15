package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/daemon"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
	"github.com/rickdiculousli/buildermesh/internal/web"
)

const (
	serveHistory  = 24 * time.Hour // how far back the page's event list starts
	shutdownGrace = 5 * time.Second
)

// errReexec is serve's return once an update has drained the daemon and
// everything has stopped: the caller re-execs the binary.
var errReexec = errors.New("serve: re-exec for an update")

// cmdServe runs the daemon and the map until SIGTERM or SIGINT, or until an
// update replaced the binary, which it then re-execs.
func (a *app) cmdServe(args []string) error {
	fs := a.flags("serve")
	listen := fs.String("listen", "", "HTTP address (default [serve] listen)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "usage: bmesh serve [--listen ADDR]")
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	// The path is read before an update moves the running file aside.
	exe, err := executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		fmt.Fprintf(a.stderr, "bmesh serve: no binary path, so no [update]: %v\n", err)
		exe = ""
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	err = serve(ctx, a.cfg, *listen, a.stderr, nil, exe)
	if errors.Is(err, errReexec) {
		stop()
		fmt.Fprintf(a.stderr, "bmesh serve: restarting as %s\n", exe)
		return reexec(exe)
	}
	return err
}

// serve runs the embedded server (when [mesh] nats is empty), the daemon and
// the HTTP server until ctx ends, then stops them in order: HTTP, daemon,
// client, embedded server. listen overrides [serve] listen; ready, when not
// nil, receives the bound HTTP address. exe is the binary [update] replaces
// ("" = none); when an update asks for a restart, serve stops as it would
// on ctx and returns errReexec.
func serve(ctx context.Context, cfg *config.Config, listen string, stderr io.Writer, ready chan<- string, exe string) error {
	ctx, restart := context.WithCancel(ctx)
	defer restart()
	var restarting atomic.Bool
	host := cfg.HostName()
	url, token, domain, mode := cfg.Mesh.NATS, "", cfg.Mesh.Domain, "hub"
	if url == "" {
		emb, err := mesh.StartEmbedded(cfg.Expand(cfg.Mesh.Store), cfg.Mesh.Listen)
		if err != nil {
			return fmt.Errorf("embedded nats-server: %w", err)
		}
		defer emb.Shutdown()
		url, domain, mode = emb.URL(), "hub", "embedded" // the embedded server's domain is always hub
	} else {
		t, err := cfg.Token()
		if err != nil {
			return fmt.Errorf("token: %w", err)
		}
		token = t
	}
	c, err := mesh.Connect(ctx, url, token, domain, "bmesh/serve/"+host, true)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := mesh.Provision(ctx, c, cfg.Routines.KeepResults); err != nil {
		return err
	}
	r, err := runner.New(cfg)
	if err != nil {
		return err
	}

	if listen == "" {
		listen = cfg.Serve.Listen
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	addr := ln.Addr().String()
	fmt.Fprintf(stderr, "bmesh serve: nats %s (%s), http %s, host %s\n", url, mode, addr, host)

	claude := os.Getenv("BMESH_CLAUDE_COMMAND")
	if strings.TrimSpace(claude) == "" {
		claude = cfg.Runner.Claude
	}
	dctx, stopDaemon := context.WithCancel(ctx)
	defer stopDaemon()
	daemonDone := make(chan error, 1)
	log := slog.New(slog.NewTextHandler(stderr, nil))
	go func() {
		daemonDone <- daemon.Run(dctx, daemon.Deps{
			Client: c, Cfg: cfg, Runner: r, Host: host,
			Log:       log,
			Checkouts: cfg.Expand(cfg.Runner.Checkouts), ClaudeBin: claude,
			Revision: buildVersion().Revision, Exe: exe,
			Restart: func() { restarting.Store(true); restart() },
		})
	}()

	srv := &http.Server{
		Handler: web.Handler(c, serveHistory, web.Options{Cfg: cfg, Host: host, Log: log}), ReadHeaderTimeout: 10 * time.Second,
		// Requests end with ctx, so open event streams don't hold Shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	httpDone := make(chan error, 1)
	go func() { httpDone <- srv.Serve(ln) }()
	if ready != nil {
		ready <- addr
	}

	var httpErr error
	select {
	case <-ctx.Done():
	case httpErr = <-httpDone:
	}
	sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		srv.Close() // event streams outlive the grace period; cut them
	}
	stopDaemon()
	derr := <-daemonDone
	if httpErr != nil && !errors.Is(httpErr, http.ErrServerClosed) {
		return fmt.Errorf("http: %w", httpErr)
	}
	if derr == nil && restarting.Load() {
		return errReexec // after the deferred stops: client, embedded server
	}
	return derr
}
