package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const feedbackUsage = `usage: bmesh feedback "<text>" [--repo R] [--where <file|command>] [--fixed SHA] | bmesh feedback list <family> [--json] | bmesh feedback drain <family> [--through SEQ]`

// cmdFeedback is a lead's lesson for its build family's coordinator:
// bmesh feedback "<text>", and the coordinator's list and drain.
func (a *app) cmdFeedback(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "list":
			return a.cmdFeedbackList(args[1:])
		case "drain":
			return a.cmdFeedbackDrain(args[1:])
		}
	}
	fs := a.flags("feedback")
	repo := fs.String("repo", "", "the repo the friction is in (default the node's; buildermesh for the mesh itself)")
	where := fs.String("where", "", "the file or command it was met in")
	fixed := fs.String("fixed", "", "a fix already landed for it")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return fail(exitUsage, feedbackUsage)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	p, err := a.identity(ctx, "feedback")
	if err != nil {
		return err
	}
	n, ok, err := mesh.GetNode(ctx, a.client, p.String())
	if err != nil {
		return err
	}
	if !ok {
		return fail(exitError, "node %s is not registered", p)
	}
	if n.Family == "" {
		return fail(exitRefused, "refused: %s: no build family", p)
	}
	f := mesh.Feedback{From: p.String(), Family: n.Family, Repo: n.Repo, Spec: n.Spec, Where: *where, Fixed: *fixed,
		Text: strings.TrimSpace(pos[0])}
	if *repo != "" {
		name, _, err := a.cfg.RepoPath(*repo)
		if err != nil {
			return fail(exitUsage, "--repo: %v", err)
		}
		f.Repo = name
	}
	if n.Worktree != "" && n.Host == a.cfg.HostName() {
		if out, err := exec.CommandContext(ctx, "git", "-C", n.Worktree, "rev-parse", "HEAD").Output(); err == nil {
			f.Commit = strings.TrimSpace(string(out))
		}
	}
	seq, err := mesh.PublishFeedback(ctx, a.client, f)
	if err != nil {
		return err
	}
	if a.json {
		f.Seq = seq
		return a.printJSON(f)
	}
	fmt.Fprintf(a.stdout, "feedback %d in %s\n", seq, f.Family)
	return nil
}

// cmdFeedbackList prints a family's pending feedback, one message per
// line with its stream sequence.
func (a *app) cmdFeedbackList(args []string) error {
	pos, err := parse(a.flags("feedback list"), args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, "usage: bmesh feedback list <family> [--json]")
	}
	if err := mesh.CheckFamily(pos[0]); err != nil {
		return fail(exitUsage, "%v", err)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "feedback list"); err != nil {
		return err
	}
	list, err := mesh.ListFeedback(ctx, a.client, pos[0])
	if err != nil {
		return err
	}
	if a.json {
		return a.printJSON(list)
	}
	for _, f := range list {
		fmt.Fprintln(a.stdout, feedbackLine(f))
	}
	return nil
}

// feedbackLine is "<seq> <from> (<repo> <spec>[, where W][, fixed F]): <text>",
// the text's line breaks folded to spaces.
func feedbackLine(f mesh.Feedback) string {
	about := []string{dash(f.Repo) + " " + dash(f.Spec)}
	if f.Where != "" {
		about = append(about, "where "+f.Where)
	}
	if f.Fixed != "" {
		about = append(about, "fixed "+f.Fixed)
	}
	return fmt.Sprintf("%d %s (%s): %s", f.Seq, dash(f.From), strings.Join(about, ", "), strings.Join(strings.Fields(f.Text), " "))
}

// cmdFeedbackDrain deletes a family's feedback from the stream, up to and
// including --through (else what is there when it starts).
func (a *app) cmdFeedbackDrain(args []string) error {
	fs := a.flags("feedback drain")
	through := fs.String("through", "", "the last sequence to delete (default the last one there now)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, "usage: bmesh feedback drain <family> [--through SEQ]")
	}
	if err := mesh.CheckFamily(pos[0]); err != nil {
		return fail(exitUsage, "%v", err)
	}
	var seq uint64
	if *through != "" {
		if seq, err = strconv.ParseUint(*through, 10, 64); err != nil || seq == 0 {
			return fail(exitUsage, "--through %q: want a stream sequence", *through)
		}
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "feedback drain"); err != nil {
		return err
	}
	n, err := mesh.DrainFeedback(ctx, a.client, pos[0], seq)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "drained %d from %s\n", n, pos[0])
	return nil
}
