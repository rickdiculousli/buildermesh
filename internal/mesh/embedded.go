package mesh

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// Embedded is a nats-server with JetStream domain "hub" running in-process.
type Embedded struct{ srv *server.Server }

// StartEmbedded starts the server on listen ("host:port"; port 0 or -1 picks a free one).
func StartEmbedded(store, listen string) (*Embedded, error) {
	host, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return nil, fmt.Errorf("listen %q: %w", listen, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("listen %q: %w", listen, err)
	}
	if port == 0 {
		port = -1
	}
	s, err := server.NewServer(&server.Options{
		Host: host, Port: port, JetStream: true, JetStreamDomain: "hub",
		StoreDir: store, NoLog: true, NoSigs: true,
	})
	if err != nil {
		return nil, err
	}
	go s.Start()
	if !s.ReadyForConnections(5 * time.Second) {
		s.Shutdown()
		return nil, fmt.Errorf("embedded nats-server not ready on %s", listen)
	}
	return &Embedded{srv: s}, nil
}

func (e *Embedded) URL() string { return e.srv.ClientURL() }

func (e *Embedded) Shutdown() {
	e.srv.Shutdown()
	e.srv.WaitForShutdown()
}
