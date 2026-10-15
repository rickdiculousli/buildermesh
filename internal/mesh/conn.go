package mesh

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Client is a connected JetStream handle.
type Client struct {
	NC *nats.Conn
	JS jetstream.JetStream
}

// Connect dials url with the token (empty for none) and addresses JetStream
// through domain. The daemon reconnects forever; every other client fails fast.
func Connect(ctx context.Context, url, token, domain, clientName string, daemon bool) (*Client, error) {
	opts := []nats.Option{nats.Name(clientName), nats.Timeout(5 * time.Second)}
	if token != "" {
		opts = append(opts, nats.Token(token))
	}
	if daemon {
		opts = append(opts, nats.MaxReconnects(-1))
	} else {
		opts = append(opts, nats.RetryOnFailedConnect(false), nats.MaxReconnects(0))
	}
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("can't reach NATS at %s: %w", url, err)
	}
	js, err := jetstream.NewWithDomain(nc, domain)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream domain %q: %w", domain, err)
	}
	return &Client{NC: nc, JS: js}, nil
}

// Close closes the connection.
func (c *Client) Close() { c.NC.Close() }
