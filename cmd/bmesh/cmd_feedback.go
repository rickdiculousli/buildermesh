package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const feedbackUsage = `usage: bmesh feedback "<text>" [--repo R] [--where <file|command>] [--fixed SHA] | bmesh feedback list <family>|--repo R [--json] | bmesh feedback drain <family> [--through SEQ] | bmesh feedback drain --seq SEQ[,SEQ...]`

const (
	listUsage  = "usage: bmesh feedback list <family>|--repo R [--json]"
	drainUsage = "usage: bmesh feedback drain <family> [--through SEQ] | bmesh feedback drain --seq SEQ[,SEQ...]"
)

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

// cmdFeedbackList prints a family's pending feedback, or with --repo every
// family's pending feedback naming that repo, less any whose sequence a
// spec on the repo's main names in from_feedback (#<seq>): one message per
// line with its stream sequence.
func (a *app) cmdFeedbackList(args []string) error {
	fs := a.flags("feedback list")
	repo := fs.String("repo", "", "list every family's feedback naming this repo")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if (*repo == "") != (len(pos) == 1) || len(pos) > 1 {
		return fail(exitUsage, listUsage)
	}
	var name, repoPath string
	if *repo != "" {
		if err := a.loadConfig(); err != nil {
			return err
		}
		if name, repoPath, err = a.cfg.RepoPath(*repo); err != nil {
			return fail(exitUsage, "--repo: %v", err)
		}
	} else if err := mesh.CheckFamily(pos[0]); err != nil {
		return fail(exitUsage, "%v", err)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "feedback list"); err != nil {
		return err
	}
	var list []mesh.Feedback
	if *repo == "" {
		if list, err = mesh.ListFeedback(ctx, a.client, pos[0]); err != nil {
			return err
		}
	} else if list, err = a.repoFeedback(ctx, name, repoPath); err != nil {
		return err
	}
	if a.json {
		if list == nil {
			list = []mesh.Feedback{}
		}
		return a.printJSON(list)
	}
	for _, f := range list {
		fmt.Fprintln(a.stdout, feedbackLine(f, *repo != ""))
	}
	return nil
}

// repoFeedback is every family's pending feedback naming repo, less what a
// spec on its main already names (from_feedback's #<seq>): a fixer that
// stopped between landing a spec and draining its issue never writes that
// spec twice.
func (a *app) repoFeedback(ctx context.Context, repo, repoPath string) ([]mesh.Feedback, error) {
	specs, err := spec.ScanRef(repoPath, a.cfg.Spec.Glob, "main")
	if err != nil {
		return nil, ledgerFail(err)
	}
	all, err := mesh.ListAllFeedback(ctx, a.client)
	if err != nil {
		return nil, err
	}
	return spec.OpenIssues(all, repo, specs), nil
}

// feedbackLine is "<seq> <from> (<repo> <spec>[, family F][, where W][, fixed F]): <text>",
// the text's line breaks folded to spaces; the family is named when the
// line was read across families.
func feedbackLine(f mesh.Feedback, family bool) string {
	about := []string{dash(f.Repo) + " " + dash(f.Spec)}
	if family {
		about = append(about, "family "+f.Family)
	}
	if f.Where != "" {
		about = append(about, "where "+f.Where)
	}
	if f.Fixed != "" {
		about = append(about, "fixed "+f.Fixed)
	}
	return fmt.Sprintf("%d %s (%s): %s", f.Seq, dash(f.From), strings.Join(about, ", "), strings.Join(strings.Fields(f.Text), " "))
}

// cmdFeedbackDrain deletes a family's feedback from the stream, up to and
// including --through (else what is there when it starts), or with --seq
// exactly the messages named.
func (a *app) cmdFeedbackDrain(args []string) error {
	fs := a.flags("feedback drain")
	through := fs.String("through", "", "the last sequence to delete (default the last one there now)")
	seqFlag := fs.String("seq", "", "delete exactly these sequences, comma-separated, whatever their family")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if *seqFlag != "" {
		if len(pos) != 0 || *through != "" {
			return fail(exitUsage, drainUsage)
		}
		return a.drainSeqs(*seqFlag)
	}
	if len(pos) != 1 {
		return fail(exitUsage, drainUsage)
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

// drainSeqs deletes the feedback at each sequence in list and prints
// `drained N`, then `not pending: <seqs>` for those already gone. A
// sequence that is not feedback is refused (exit 3) and nothing is deleted.
func (a *app) drainSeqs(list string) error {
	var seqs []uint64
	for _, f := range strings.Split(list, ",") {
		n, err := strconv.ParseUint(strings.TrimSpace(f), 10, 64)
		if err != nil || n == 0 {
			return fail(exitUsage, "--seq %q: want stream sequences, comma-separated", list)
		}
		seqs = append(seqs, n)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "feedback drain"); err != nil {
		return err
	}
	deleted, missing, err := mesh.DeleteFeedback(ctx, a.client, seqs)
	if err != nil {
		return fail(exitRefused, "refused: %v", err)
	}
	fmt.Fprintf(a.stdout, "drained %d\n", len(deleted))
	if len(missing) > 0 {
		gone := make([]string, len(missing))
		for i, n := range missing {
			gone[i] = strconv.FormatUint(n, 10)
		}
		fmt.Fprintf(a.stdout, "not pending: %s\n", strings.Join(gone, ", "))
	}
	return nil
}
