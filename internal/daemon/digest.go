package daemon

import (
	"bytes"
	"context"
	"fmt"
	"github.com/rickdiculousli/buildermesh/internal/shell"
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
// The body has one line per lead that entered asking, silent or blocked in
// the window (since, now], then one per lead done in that window. Nodes in a
// lead's tree (executors) are nested under their lead's line, "  └ <name>:
// <state>: <text>", which is added as "<lead>: <state>" when the lead itself
// is not news; the operator reads only leads' asks, so the title's asking
// count covers leads alone. A state is listed only in the pass whose window
// holds its At, so a standing state is posted once rather than every pass;
// one stamped after now waits for the next pass, so none is lost between
// passes. The title's working and silent counts cover every node. changed is
// false when there are no such lines.
func Digest(host string, states map[string]mesh.State, nodes []mesh.Node, since, now time.Time) (title, body string, changed bool) {
	paths := make([]string, 0, len(states))
	for p := range states {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var working, asking, silent int
	type entry struct {
		attention, finished []string // the lead's own lines
		nested              []string // its tree's lines, in path order
	}
	entries := map[string]*entry{}
	var leads []string
	at := func(lead string) *entry {
		e, ok := entries[lead]
		if !ok {
			e = &entry{}
			entries[lead] = e
			leads = append(leads, lead)
		}
		return e
	}
	for _, p := range paths {
		st := states[p]
		stAt, _ := time.Parse(time.RFC3339, st.At)
		lead, name, inTree := treeMember(p)
		switch st.State {
		case "working":
			working++
		case "silent":
			silent++
		case "asking":
			if !inTree {
				asking++
			}
		}
		if !inWindow(stAt, since, now) {
			continue
		}
		switch st.State {
		case "asking", "silent", "blocked":
			if inTree {
				word := st.State
				if word == "asking" {
					word = "ask"
				}
				line := "  └ " + name + ": " + word
				if st.Text != "" {
					line += ": " + st.Text
				}
				e := at(lead)
				e.nested = append(e.nested, line)
				continue
			}
			line := fmt.Sprintf("%s: %s since %s", p, st.State, age(now.Sub(stAt)))
			if st.Text != "" {
				line += ": " + st.Text
			}
			e := at(lead)
			e.attention = append(e.attention, line)
		case "done":
			e := at(lead)
			if inTree {
				e.nested = append(e.nested, "  └ "+name+": done")
			} else {
				e.finished = append(e.finished, p+": done")
			}
		}
	}
	title = fmt.Sprintf("bmesh %s: %d working, %d asking, %d silent", host, working, asking, silent)
	var attention, finished []string
	for _, lead := range leads {
		e := entries[lead]
		switch {
		case len(e.attention) > 0:
			attention = append(attention, e.attention...)
			attention = append(attention, e.nested...)
		case len(e.finished) > 0:
			finished = append(finished, e.finished...)
			finished = append(finished, e.nested...)
		case len(e.nested) > 0:
			// The lead itself is not news: head its tree's lines with its state.
			st := states[lead].State
			if st == "" {
				st = "idle"
			}
			head := lead + ": " + st
			if stAt, err := time.Parse(time.RFC3339, states[lead].At); err == nil && !stAt.IsZero() {
				head += " since " + age(now.Sub(stAt))
			}
			attention = append(attention, head)
			attention = append(attention, e.nested...)
		}
	}
	lines := append(attention, finished...)
	return title, strings.Join(lines, "\n"), len(lines) > 0
}

// treeMember splits a path under a lead (an executor, "a.x") into the lead
// and the rest; routine instances and leads themselves are not members.
func treeMember(p string) (lead, name string, ok bool) {
	lead, name, found := strings.Cut(p, ".")
	if !found || lead == "routine" {
		return p, "", false
	}
	return lead, name, true
}

// inWindow reports whether at is in (since, now].
func inWindow(at, since, now time.Time) bool { return at.After(since) && !at.After(now) }

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
	now := d.Now() // before the read, so the window (since, now] has no gap
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
	title, body, changed := Digest(d.Host, states, nodes, *since, now)
	if !changed {
		return
	}
	if err := deliver(ctx, d, Headers{Title: title}, body); err != nil {
		d.Log.Warn("digest: deliver", "err", err)
		return
	}
	ev := mesh.Event{From: d.from(), Event: "digest", Text: title, Body: body, At: now.UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, d.Client, ev); err != nil {
		d.Log.Warn("digest: publish", "err", err)
	}
	*since = now
}

// deliver pipes body to Notify.Command when set, the headers in its
// environment ($BMESH_TITLE and $NTFY_TITLE, $NTFY_PRIORITY, $NTFY_TAGS,
// $NTFY_CLICK, $NTFY_ACTIONS, empty when unset), else POSTs it to
// Notify.Ntfy with the non-empty headers.
func deliver(ctx context.Context, d Deps, h Headers, body string) error {
	ctx, cancel := context.WithTimeout(ctx, deliverTimeout)
	defer cancel()
	if cmd := d.Cfg.Notify.Command; cmd != "" {
		c := exec.CommandContext(ctx, shell.Argv(cmd)[0], shell.Argv(cmd)[1:]...)
		c.Stdin = strings.NewReader(body)
		c.WaitDelay = 2 * time.Second // a child holding the pipes can't block Wait past the timeout
		c.Env = append(os.Environ(), "BMESH_TITLE="+h.Title, "NTFY_TITLE="+h.Title, "NTFY_PRIORITY="+h.Priority,
			"NTFY_TAGS="+h.Tags, "NTFY_CLICK="+h.Click, "NTFY_ACTIONS="+h.Actions)
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("notify command: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Cfg.Notify.Ntfy, bytes.NewReader([]byte(body)))
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
