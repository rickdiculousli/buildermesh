package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
)

const t3Usage = "usage: bmesh t3 login [--server URL] [--callback-port 52299] [--timeout D] | bmesh t3 login --status"

func (a *app) cmdT3(args []string) error {
	if len(args) == 0 || args[0] != "login" {
		return fail(exitUsage, t3Usage)
	}
	return a.cmdT3Login(args[1:])
}

// cmdT3Login signs bmesh in to the box's T3 0.1.0: it registers a public
// client, prints the authorize URL and how to approve it from another
// machine, takes the redirect on a local listener or pasted, exchanges the
// code with PKCE and writes the token beside the config. --status prints
// the sign-in instead.
func (a *app) cmdT3Login(args []string) error {
	fs := a.flags("t3 login")
	server := fs.String("server", "", "the T3 server (default [runner] t3_server)")
	port := fs.Int("callback-port", 52299, "the local port the browser is sent back to")
	timeout := fs.Duration("timeout", time.Hour, "how long to wait for the approval")
	status := fs.Bool("status", false, "print the sign-in: box, access, days left, server and scopes")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, t3Usage)
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	host, path := a.cfg.HostName(), a.cfg.T3TokenPath()
	if *status {
		tok, err := t3mcp.ReadToken(path)
		if errors.Is(err, t3mcp.ErrNoToken) {
			return fail(exitRefused, "no T3 sign-in on %s; run bmesh t3 login", host)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "%s: %s, %s\n", host, tok.Access, daysLeft(tok, time.Now()))
		fmt.Fprintf(a.stdout, "server %s, scopes %s, expires %s\n", tok.Server, strings.Join(tok.Scopes, " "), tok.ExpiresAt.UTC().Format(time.RFC3339))
		if tok.DaysLeft(time.Now()) == 0 {
			return fail(exitRefused, "T3 sign-in missing or expired; run bmesh t3 login")
		}
		return nil
	}
	if *server == "" {
		*server = a.cfg.Runner.T3Server
	}
	*server = strings.TrimRight(*server, "/")
	su, err := url.Parse(*server)
	if err != nil || su.Host == "" {
		return fail(exitUsage, "--server %q: want a URL like http://127.0.0.1:3773", *server)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	md, err := t3mcp.Discover(ctx, nil, *server)
	if err != nil {
		return err
	}
	// Listen first, so the redirect URI names the port really held.
	lns, err := listenCallback(*port)
	if err != nil {
		return err
	}
	defer func() {
		for _, l := range lns {
			l.Close()
		}
	}()
	*port = lns[0].Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://localhost:%d/callback", *port)
	clientID, err := t3mcp.Register(ctx, nil, md, "bmesh ("+host+")", redirect)
	if err != nil {
		return err
	}
	pkce, err := t3mcp.NewPKCE()
	if err != nil {
		return err
	}
	state, err := t3mcp.NewState()
	if err != nil {
		return err
	}
	mode := a.cfg.Runner.T3Runtime
	level := "Auto"
	if mode == "full-access" {
		level = "Full access (this box runs t3_runtime_mode full-access)"
	}
	tunnel := fmt.Sprintf("-L %d:localhost:%d", *port, *port)
	if h := su.Hostname(); h == "127.0.0.1" || h == "localhost" || h == "::1" {
		sp := su.Port()
		if sp == "" {
			sp = "80"
		}
		tunnel = fmt.Sprintf("-L %s:%s:%s ", sp, h, sp) + tunnel
	}
	fmt.Fprintf(a.stdout, `Sign bmesh on %s in to T3 at %s (t3_runtime_mode %s).

  1. From the machine with the browser, open the tunnel:
       ssh -N %s %s
  2. If that browser isn't paired with this T3, pair it first with a code from
     `+"`t3 auth pairing create`"+` on %s (a code lasts 5 minutes).
  3. Open:
       %s
  4. Choose %s and approve.

Waiting for the browser on localhost:%d, or paste the URL it ended on here.
`, host, *server, mode, tunnel, host, host, t3mcp.AuthorizeURL(md, clientID, redirect, state, pkce), level, *port)

	type redirectMsg struct {
		raw   string
		reply chan error
	}
	got := make(chan redirectMsg)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		m := redirectMsg{raw: redirect + "?" + r.URL.RawQuery, reply: make(chan error, 1)}
		select {
		case got <- m:
		case <-r.Context().Done():
			return
		}
		if err := <-m.reply; err != nil {
			http.Error(w, "bmesh: "+err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, "bmesh on %s is signed in to T3. You can close this page.\n", host)
	})}
	for _, l := range lns {
		go srv.Serve(l)
	}
	defer func() {
		// Let the callback page say how it went before the listener goes.
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	go func() {
		sc := bufio.NewScanner(a.stdin)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			m := redirectMsg{raw: line, reply: make(chan error, 1)}
			select {
			case got <- m:
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		var m redirectMsg
		select {
		case m = <-got:
		case <-ctx.Done():
			return fail(exitTimeout, "no sign-in within %s", *timeout)
		}
		code, iss, err := t3mcp.ParseRedirect(m.raw, state)
		if err != nil {
			m.reply <- err
			if errors.Is(err, t3mcp.ErrRefused) {
				return err
			}
			fmt.Fprintf(a.stderr, "bmesh: %s; still waiting\n", err)
			continue
		}
		tok, err := t3mcp.Exchange(ctx, nil, md, *server, clientID, redirect, code, pkce.Verifier, iss, time.Now())
		if err == nil {
			err = t3mcp.WriteToken(path, tok)
		}
		m.reply <- err
		if err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "signed in: %s: %s, %s (token in %s)\n", host, tok.Access, daysLeft(tok, time.Now()), path)
		if t3mcp.RuntimeRank(tok.Access) < t3mcp.RuntimeRank(mode) {
			return fail(exitRefused, "this sign-in allows %s, below [runner] t3_runtime_mode %s: leads won't start; run bmesh t3 login and approve %s",
				tok.Access, mode, mode)
		}
		return nil
	}
}

// listenCallback listens on port on 127.0.0.1 and, when it can, [::1]:
// localhost may resolve to either at the end of a tunnel.
func listenCallback(port int) ([]net.Listener, error) {
	l4, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("callback listener: %w (another login running? pick --callback-port)", err)
	}
	lns := []net.Listener{l4}
	p := l4.Addr().(*net.TCPAddr).Port
	if l6, err := net.Listen("tcp", net.JoinHostPort("::1", strconv.Itoa(p))); err == nil {
		lns = append(lns, l6)
	}
	return lns, nil
}

// daysLeft is "N days left", "1 day left" or "expired".
func daysLeft(t t3mcp.Token, now time.Time) string {
	switch n := t.DaysLeft(now); n {
	case 0:
		return "expired"
	case 1:
		return "1 day left"
	default:
		return fmt.Sprintf("%d days left", n)
	}
}
