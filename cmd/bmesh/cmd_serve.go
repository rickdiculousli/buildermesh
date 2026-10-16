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
	"runtime"
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
// serveHostsEvery is the daemon's hosts tick (0 = its default, a minute);
// tests shorten it.
var serveHostsEvery time.Duration

func (a *app) cmdServe(args []string) error {
	fs := a.flags("serve")
	listen := fs.String("listen", "", "HTTP address (default [serve] listen)")
	restart := fs.Bool("restart", false, "run serve as a child and start it again whenever it exits")
	detachF := fs.Bool("detach", false, "with --restart, on Windows: start the supervisor with no console window and return")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *detachF && !*restart {
		return fail(exitUsage, "usage: bmesh serve [--listen ADDR] [--restart [--detach]]")
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	// The path is read before an update moves the running file aside.
	exe, err := executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if *restart {
		if err != nil {
			return fmt.Errorf("serve --restart: no binary path to start: %w", err)
		}
		return a.superviseServe(exe, args, *detachF)
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

// superviseServe is serve --restart: the supervisor, with serve's own
// arguments for the child. With detach (Windows) it starts itself again
// with no console window and returns. On Windows the supervisor and the
// child write to serve.log in the state dir, there being no journal there.
func (a *app) superviseServe(exe string, args []string, detachIt bool) error {
	logPath := filepath.Join(config.StateDir(), "serve.log")
	if detachIt {
		pid, err := detach(exe, append([]string{"serve", "--restart"}, childArgs(a.configPath, args)[1:]...))
		if err != nil {
			return fail(exitUsage, "serve --detach: %v", err)
		}
		fmt.Fprintf(a.stderr, "bmesh serve: supervisor started with no window (pid %d); log in %s\n", pid, logPath)
		return nil
	}
	var out io.Writer = a.stderr
	if runtime.GOOS == "windows" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return err
		}
		l, err := openRotLog(logPath, serveLogMax)
		if err != nil {
			return err
		}
		defer l.Close()
		fmt.Fprintf(a.stderr, "bmesh serve --restart: log in %s\n", logPath)
		out = l
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	return newSupervisor(exe, childArgs(a.configPath, args), out).run(ctx)
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
	if url == "" && cfg.Hub.Role != "" {
		return fail(exitRefused, "serve: [hub] role is %s, so bmesh hub runs this box's server: set [mesh] nats to it (nats://<[hub] listen>) rather than embed a second", cfg.Hub.Role)
	}
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
	webDir := os.Getenv("BMESH_WEB_DIR")
	if webDir != "" {
		fmt.Fprintf(stderr, "bmesh serve: map page from %s\n", webDir)
	}

	claude := os.Getenv("BMESH_CLAUDE_COMMAND")
	if strings.TrimSpace(claude) == "" {
		claude = cfg.Runner.Claude
	}
	dctx, stopDaemon := context.WithCancel(ctx)
	defer stopDaemon()
	daemonDone := make(chan error, 1)
	log := slog.New(slog.NewTextHandler(stderr, nil))
	quota := &web.Quota{Command: cfg.Quota.Command} // one gate for the map's strip and the daemon's reminders
	go func() {
		t3Token := ""
		if cfg.Runner.Kind == "t3" {
			t3Token = cfg.T3TokenPath()
		}
		daemonDone <- daemon.Run(dctx, daemon.Deps{
			Client: c, Cfg: cfg, Runner: r, Host: host,
			T3Token: t3Token, StateDir: config.StateDir(), ConfigDir: cfg.ConfigDir(), HostsEvery: serveHostsEvery,
			Log:       log,
			Checkouts: cfg.Expand(cfg.Runner.Checkouts), ClaudeBin: claude,
			Quota:    quota,
			Revision: buildVersion().Revision, Tag: buildTag(), RevTime: buildVersion().Time, Module: buildModule(), Exe: exe,
			Restart:    func() { restarting.Store(true); restart() },
			Supervised: os.Getenv("BMESH_SUPERVISED") == "1",
		})
	}()

	srv := &http.Server{
		Handler: web.Handler(c, serveHistory, web.Options{Cfg: cfg, Host: host, Log: log, Quota: quota, Dir: webDir}), ReadHeaderTimeout: 10 * time.Second,
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
