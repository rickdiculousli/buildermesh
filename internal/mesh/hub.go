package mesh

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// LeafUser is the user a leaf authenticates to the hub's leaf port as, the
// token being its password: a leaf listener takes a user and password, not
// a bare token.
const LeafUser = "leaf"

// HubOptions is the server `bmesh hub` runs: a hub (JetStream on, a leaf
// port) or, with Remote set, a leaf of one (no JetStream). Every address is
// "host:port"; port 0 picks a free one.
type HubOptions struct {
	Name     string // the server name
	Token    string // client auth, and the leaf link's password
	Listen   string // the client port
	Logs     bool   // log to stderr; off in tests
	ReadyFor time.Duration

	// A hub.
	Domain     string // JetStream domain
	Store      string
	MaxStore   int64
	MaxMemory  int64
	LeafListen string // "" = no leaf port

	// A leaf.
	Remote string // the hub's leaf URL, nats://host:port; dialled forever
}

// Hub is a running hub or leaf server.
type Hub struct{ srv *server.Server }

// StartHub starts the server o describes and waits until it takes clients.
func StartHub(o HubOptions) (*Hub, error) {
	host, port, err := hostPort(o.Listen)
	if err != nil {
		return nil, err
	}
	so := &server.Options{
		ServerName: o.Name, Host: host, Port: port, Authorization: o.Token,
		NoLog: !o.Logs, NoSigs: true, Logtime: true,
	}
	if o.Remote == "" {
		so.JetStream, so.JetStreamDomain, so.StoreDir = true, o.Domain, o.Store
		so.JetStreamMaxStore, so.JetStreamMaxMemory = o.MaxStore, o.MaxMemory
		if o.LeafListen != "" {
			lh, lp, err := hostPort(o.LeafListen)
			if err != nil {
				return nil, err
			}
			so.LeafNode = server.LeafNodeOpts{Host: lh, Port: lp, Username: LeafUser, Password: o.Token}
		}
	} else {
		u, err := url.Parse(o.Remote)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("remote %q: want nats://host:port", o.Remote)
		}
		u.User = url.UserPassword(LeafUser, o.Token)
		// A solicited leaf connection is retried forever, every second.
		so.LeafNode = server.LeafNodeOpts{Remotes: []*server.RemoteLeafOpts{{URLs: []*url.URL{u}}}, ReconnectInterval: time.Second}
	}
	s, err := server.NewServer(so)
	if err != nil {
		return nil, err
	}
	s.ConfigureLogger()
	go s.Start()
	ready := o.ReadyFor
	if ready == 0 {
		ready = 10 * time.Second
	}
	if !s.ReadyForConnections(ready) {
		s.Shutdown()
		return nil, fmt.Errorf("nats-server not ready on %s within %s", o.Listen, ready)
	}
	return &Hub{srv: s}, nil
}

// hostPort splits addr, port 0 becoming the server's "pick one" (-1).
func hostPort(addr string) (string, int, error) {
	host, ps, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("listen %q: %w", addr, err)
	}
	port, err := strconv.Atoi(ps)
	if err != nil {
		return "", 0, fmt.Errorf("listen %q: %w", addr, err)
	}
	if port == 0 {
		port = -1
	}
	return host, port, nil
}

// ClientURL is the client port's URL.
func (h *Hub) ClientURL() string { return h.srv.ClientURL() }

// Leaves is the number of leaf links up.
func (h *Hub) Leaves() int { return h.srv.NumLeafNodes() }

// Shutdown stops the server, letting JetStream flush its store.
func (h *Hub) Shutdown() {
	h.srv.Shutdown()
	h.srv.WaitForShutdown()
}
