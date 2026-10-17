package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const (
	holdUsage        = `usage: bmesh hold <path>|--family F [--gate land|now] --text "<why>"`
	releaseHoldUsage = `usage: bmesh release <path>|--family F [--text "<words>"]`
)

// cmdHold holds a lead (or every live lead of a build family) at a gate:
// it writes bm-holds <path> and sends the lead a message of kind hold,
// which wakes an idle lead as any message does. Executors are never held.
func (a *app) cmdHold(args []string) error {
	fs := a.flags("hold")
	family := fs.String("family", "", "hold every live lead of this build family")
	gate := fs.String("gate", mesh.GateLand, "where the lead stops: land (bmesh land refuses) or now (its next inbox)")
	text := fs.String("text", "", "why, as the lead reads it")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if (len(pos) == 1) == (*family != "") || len(pos) > 1 || *text == "" {
		return fail(exitUsage, holdUsage)
	}
	if *gate != mesh.GateLand && *gate != mesh.GateNow {
		return fail(exitUsage, "--gate %q: want land or now", *gate)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	by, err := a.identityOrHost(ctx, "hold")
	if err != nil {
		return err
	}
	var leads []mesh.Node
	if *family != "" {
		if leads, err = a.familyLeads(ctx, *family); err != nil {
			return err
		}
	} else {
		n, err := a.holdable(ctx, pos[0])
		if err != nil {
			return err
		}
		leads = []mesh.Node{n}
	}
	var held []mesh.LeadHold
	for _, n := range leads {
		h := mesh.LeadHold{Path: n.Path, Gate: *gate, Text: *text, By: by, At: time.Now().UTC().Format(time.RFC3339)}
		if err := a.hold(ctx, h); err != nil {
			return err
		}
		held = append(held, h)
		if !a.json {
			fmt.Fprintf(a.stdout, "holding %s %s: %s\n", h.Path, mesh.GatePhrase(h.Gate), h.Text)
		}
	}
	if a.json {
		return a.printJSON(holdRows(held))
	}
	return nil
}

// hold writes h and tells its lead: hold from <by> (<gate>): <why>.
func (a *app) hold(ctx context.Context, h mesh.LeadHold) error {
	if err := mesh.PutLeadHold(ctx, a.client, h); err != nil {
		return err
	}
	_, err := mesh.Send(ctx, a.client, mesh.Message{ID: newID(), From: h.By, To: h.Path, Kind: "hold",
		Text: fmt.Sprintf("hold from %s (%s): %s", h.By, h.Gate, h.Text)})
	return err
}

// holdable is path's node when it is a lead that can be held: registered,
// not an executor or a round's, and not done or blocked.
func (a *app) holdable(ctx context.Context, path string) (mesh.Node, error) {
	p, err := mesh.ParsePath(path)
	if err != nil {
		return mesh.Node{}, fail(exitUsage, "%v", err)
	}
	if err := a.connect(ctx, "hold"); err != nil {
		return mesh.Node{}, err
	}
	n, ok, err := mesh.GetNode(ctx, a.client, p.String())
	if err != nil {
		return mesh.Node{}, err
	}
	switch {
	case !ok:
		return mesh.Node{}, fail(exitRefused, "refused: no node %s", p)
	case n.Role == "executor":
		return mesh.Node{}, fail(exitRefused, "refused: %s is an executor; executors are never held: hold its lead %s", p, p.Lead())
	case n.Role != "lead" && n.Role != "":
		return mesh.Node{}, fail(exitRefused, "refused: %s is a %s; only a lead is held", p, n.Role)
	}
	st, _, err := mesh.ReadState(ctx, a.client, n.Path)
	if err != nil {
		return mesh.Node{}, err
	}
	if st.State == "done" || st.State == "blocked" {
		return mesh.Node{}, fail(exitRefused, "refused: %s is %s; there is nothing to hold", p, st.State)
	}
	return n, nil
}

// familyLeads are family's live leads: not done or blocked, by path; none
// is refused.
func (a *app) familyLeads(ctx context.Context, family string) ([]mesh.Node, error) {
	if err := mesh.CheckFamily(family); err != nil {
		return nil, fail(exitUsage, "--family: %v", err)
	}
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return nil, err
	}
	states, err := mesh.ReadAllStates(ctx, a.client)
	if err != nil {
		return nil, err
	}
	var out []mesh.Node
	for _, n := range nodes {
		if s := states[n.Path].State; n.Role == "lead" && n.Family == family && s != "done" && s != "blocked" {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, fail(exitRefused, "refused: no live lead in family %s", family)
	}
	return out, nil
}

// releasesHold reports whether a release's arguments name a hold rather
// than a repo to tag: --family or --text, or a positional that is neither
// a repos.toml name nor an existing directory.
func (a *app) releasesHold(args []string) bool {
	fs := a.flags("release")
	fs.Bool("minor", false, "")
	fs.Bool("patch", false, "")
	fs.Bool("dry-run", false, "")
	family := fs.String("family", "", "")
	text := fs.String("text", "", "")
	pos, err := parse(fs, args)
	if err != nil {
		return false
	}
	if *family != "" || *text != "" {
		return true
	}
	if len(pos) != 1 || a.loadConfig() != nil {
		return false
	}
	_, _, err = a.cfg.RepoPath(pos[0])
	return err != nil
}

// cmdReleaseHold releases a lead's hold (or every hold of a build
// family's leads): it deletes the key and sends the lead a message of kind
// release, which wakes it. A lead not held prints <path>: not held.
func (a *app) cmdReleaseHold(args []string) error {
	fs := a.flags("release")
	family := fs.String("family", "", "release every held lead of this build family")
	text := fs.String("text", "", "what the lead reads with the release")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if (len(pos) == 1) == (*family != "") || len(pos) > 1 {
		return fail(exitUsage, releaseHoldUsage)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	by, err := a.identityOrHost(ctx, "release")
	if err != nil {
		return err
	}
	var paths []string
	if *family != "" {
		if err := mesh.CheckFamily(*family); err != nil {
			return fail(exitUsage, "--family: %v", err)
		}
		holds, err := mesh.ListLeadHolds(ctx, a.client)
		if err != nil {
			return err
		}
		for _, h := range holds {
			if n, ok, err := mesh.GetNode(ctx, a.client, h.Path); err != nil {
				return err
			} else if ok && n.Family == *family {
				paths = append(paths, h.Path)
			}
		}
		if len(paths) == 0 && !a.json {
			fmt.Fprintf(a.stdout, "%s: nothing held\n", *family)
		}
	} else {
		p, err := mesh.ParsePath(pos[0])
		if err != nil {
			return fail(exitUsage, "release: %s is neither a repo (repos.toml) nor a node path: %v", pos[0], err)
		}
		paths = []string{p.String()}
	}
	type row struct {
		Path     string `json:"path"`
		Released bool   `json:"released"`
	}
	rows := []row{}
	for _, path := range paths {
		was, err := mesh.DeleteLeadHold(ctx, a.client, path)
		if err != nil {
			return err
		}
		rows = append(rows, row{path, was})
		if !was {
			if !a.json {
				fmt.Fprintf(a.stdout, "%s: not held\n", path)
			}
			continue
		}
		msg := "released by " + by
		if *text != "" {
			msg += ": " + *text
		}
		if _, err := mesh.Send(ctx, a.client, mesh.Message{ID: newID(), From: by, To: path, Kind: "release", Text: msg}); err != nil {
			return err
		}
		if !a.json {
			fmt.Fprintf(a.stdout, "released %s\n", path)
		}
	}
	if a.json {
		return a.printJSON(rows)
	}
	return nil
}

// cmdHeld is a held lead reaching its gate: it publishes held with the
// gate and why and moves the node to held, which the daemon never calls
// silent; the lead then ends its turn, and the release wakes it. A lead
// not held is refused, so a release that came first sends it on.
func (a *app) cmdHeld(args []string) error {
	pos, err := parse(a.flags("held"), args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "held takes no arguments: the hold says why")
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	p, err := a.identity(ctx, "held")
	if err != nil {
		return err
	}
	path := p.String()
	h, ok, err := mesh.GetLeadHold(ctx, a.client, path)
	if err != nil {
		return err
	}
	if !ok {
		return fail(exitRefused, "refused: %s is not held (released already?); run bmesh inbox and carry on", path)
	}
	cur, _, err := mesh.ReadState(ctx, a.client, path)
	if err != nil {
		return err
	}
	if !mesh.Legal(cur.State, "held") {
		return fail(exitRefused, "refused: %v", mesh.ErrIllegalTransition{From: cur.State, To: "held"})
	}
	text := mesh.GatePhrase(h.Gate) + " by " + h.By
	if h.Text != "" {
		text += ": " + h.Text
	}
	if err := mesh.PublishEvent(ctx, a.client, mesh.Event{From: path, Event: "held", Text: text}); err != nil {
		return err
	}
	if _, err := mesh.WriteState(ctx, a.client, path, mesh.State{State: "held", Text: text, By: "node"}); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "held %s %s; end your turn: the release wakes you\n", path, text)
	return nil
}

// holdRow is a hold as --json prints it.
type holdRow struct {
	Path string `json:"path"`
	Gate string `json:"gate"`
	Text string `json:"text"`
	By   string `json:"by"`
	At   string `json:"at"`
}

func holdRows(hs []mesh.LeadHold) []holdRow {
	out := []holdRow{}
	for _, h := range hs {
		out = append(out, holdRow{h.Path, h.Gate, h.Text, h.By, h.At})
	}
	return out
}
