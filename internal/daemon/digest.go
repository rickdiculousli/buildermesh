package daemon

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const deliverTimeout = 10 * time.Second

// Digest summarises the mesh for a notification. It takes host and now in
// addition to the plan's (states, nodes, since): the title names the host and
// the ages are measured to now. states is the source of truth (a state with
// no registered node still counts); nodes is reserved for enrichment.
//
// The body has one line per asking, silent or blocked node, then one per node
// done after since. changed is false when there are no such lines.
func Digest(host string, states map[string]mesh.State, nodes []mesh.Node, since, now time.Time) (title, body string, changed bool) {
	paths := make([]string, 0, len(states))
	for p := range states {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var working, asking, silent int
	var attention, finished []string
	for _, p := range paths {
		st := states[p]
		at, _ := time.Parse(time.RFC3339, st.At)
		switch st.State {
		case "working":
			working++
		case "asking", "silent", "blocked":
			if st.State == "asking" {
				asking++
			} else if st.State == "silent" {
				silent++
			}
			line := fmt.Sprintf("%s: %s since %s", p, st.State, age(now.Sub(at)))
			if st.Text != "" {
				line += ": " + st.Text
			}
			attention = append(attention, line)
		case "done":
			if at.After(since) {
				finished = append(finished, p+": done")
			}
		}
	}
	title = fmt.Sprintf("bmesh %s: %d working, %d asking, %d silent", host, working, asking, silent)
	lines := append(attention, finished...)
	return title, strings.Join(lines, "\n"), len(lines) > 0
}

// age renders d as 5m or 2h10m (minutes at least).
func age(d time.Duration) string {
	if d < time.Minute {
		return "0m"
	}
	return shortDuration(d)
}

// digestOnce sends one digest if anything needs attention or newly finished,
// then moves *since to now. A failed delivery leaves *since alone so the next
// pass reports the same things again.
func digestOnce(ctx context.Context, d Deps, since *time.Time) {
	states, err := mesh.ReadAllStates(ctx, d.Client)
	if err != nil {
		d.Log.Warn("digest: read states", "err", err)
		return
	}
	nodes, err := mesh.ListNodes(ctx, d.Client)
	if err != nil {
		d.Log.Warn("digest: list nodes", "err", err)
		return
	}
	now := d.Now()
	title, body, changed := Digest(d.Host, states, nodes, *since, now)
	if !changed {
		return
	}
	if err := deliver(ctx, d, title, body); err != nil {
		d.Log.Warn("digest: deliver", "err", err)
		return
	}
	ev := mesh.Event{From: d.from(), Event: "digest", Text: title, At: now.UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, d.Client, ev); err != nil {
		d.Log.Warn("digest: publish", "err", err)
	}
	*since = now
}

// deliver pipes body to Notify.Command (title in $BMESH_TITLE) when set, else
// POSTs it to Notify.Ntfy with a Title header.
func deliver(ctx context.Context, d Deps, title, body string) error {
	ctx, cancel := context.WithTimeout(ctx, deliverTimeout)
	defer cancel()
	if cmd := d.Cfg.Notify.Command; cmd != "" {
		c := exec.CommandContext(ctx, "sh", "-c", cmd)
		c.Stdin = strings.NewReader(body)
		c.Env = append(os.Environ(), "BMESH_TITLE="+title)
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("notify command: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Cfg.Notify.Ntfy, bytes.NewReader([]byte(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)
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
