// Package notify posts a notification: to an ntfy URL over HTTP, or through
// a command (the test hook config.toml's [notify] command names).
package notify

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/shell"
)

const timeout = 10 * time.Second

// Headers are a notification's ntfy headers; empty ones are not sent.
type Headers struct {
	Title, Priority, Tags, Click, Actions string
}

// Target is where a notification goes: Command when set, else Ntfy.
type Target struct{ Ntfy, Command string }

// Post pipes body to t.Command when set, the headers in its environment
// ($BMESH_TITLE and $NTFY_TITLE, $NTFY_PRIORITY, $NTFY_TAGS, $NTFY_CLICK,
// $NTFY_ACTIONS, empty when unset), else POSTs it to t.Ntfy with the
// non-empty headers. Either gets 10 s.
func (t Target) Post(ctx context.Context, h Headers, body string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if t.Command != "" {
		c := shell.Command(ctx, t.Command)
		c.Stdin = strings.NewReader(body)
		c.WaitDelay = 2 * time.Second // a child holding the pipes can't block Wait past the timeout
		c.Env = append(os.Environ(), "BMESH_TITLE="+h.Title, "NTFY_TITLE="+h.Title, "NTFY_PRIORITY="+h.Priority,
			"NTFY_TAGS="+h.Tags, "NTFY_CLICK="+h.Click, "NTFY_ACTIONS="+h.Actions)
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("notify command: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.Ntfy, bytes.NewReader([]byte(body)))
	if err != nil {
		return err
	}
	for k, v := range map[string]string{"Title": h.Title, "Priority": h.Priority, "Tags": h.Tags,
		"Click": h.Click, "Actions": h.Actions} {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("ntfy: %s", resp.Status)
	}
	return nil
}
