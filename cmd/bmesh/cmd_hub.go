package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// cmdHub runs this box's NATS hub or leaf from [hub] until SIGTERM or
// SIGINT. It is its own process, apart from serve, so serve's restarts for
// an update never drop the mesh; it runs the binary it started with until
// its service is restarted.
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	<-ctx.Done()
	fmt.Fprintln(a.stderr, "bmesh hub: stopping")
	h.Shutdown()
	return nil
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
		return o, fail(exitRefused, "hub: [hub] role is unset: set it to hub or leaf (README: Several boxes); a single box needs no bmesh hub, since bmesh serve embeds it")
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
		return o, fail(exitRefused, "hub: [mesh] token_file is unset: the hub and every box share one token (README: Several boxes)")
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
