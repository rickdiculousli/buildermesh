package main

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/brief"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const fixUsage = "usage: bmesh fix [--model M] [--effort E]"

// fixStarted is what fix prints with --json: the fixer's node and session,
// its model and family, and the host it runs on.
type fixStarted struct {
	leadStarted
	Model  string `json:"model"`
	Family string `json:"family"`
	Host   string `json:"host"`
}

// cmdFix starts the fix lead on bmesh's own repo (ownRepo): node, family
// and path <repo>-fix-<N>, role fixer, on the row's host (over ssh, this
// command's own form there, when that is another box), on Opus (or
// --model) at [models] lead_effort (or --effort). Its brief, the embedded
// fix template with no repo override, carries the rulings and points at
// the fix skill. It refuses (exit 3) with no row holding bmesh's module and
// while a fixer of that repo is live (neither done nor blocked), so the
// queue has one writer.
func (a *app) cmdFix(args []string) error {
	fs := a.flags("fix")
	model := fs.String("model", "", "model the fixer runs on (default Opus)")
	effort := fs.String("effort", "", "the fixer's effort (default [models] lead_effort)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, fixUsage)
	}
	if err := checkModel(*model); err != nil {
		return err
	}
	if err := checkEffort(*effort); err != nil {
		return err
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	own := a.ownRepo()
	if own == "" {
		return fail(exitRefused, "refused: no registry row holds the module bmesh was built from")
	}
	row := a.cfg.Repos[own]
	if row.Mode != "ship" {
		return fail(exitRefused, "refused: %s is %s; builds are for ship repos only", own, row.Mode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if me := a.cfg.HostName(); row.Host != "" && row.Host != me {
		return a.fixRemote(ctx, row.Host, *model, *effort)
	}
	_, repoPath, err := a.cfg.RepoPath(own)
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	if err := a.connect(ctx, "fix"); err != nil {
		return err
	}
	n, err := a.nextFixer(ctx, own)
	if err != nil {
		return err
	}
	if err := levelMain(ctx, repoPath); err != nil {
		return fail(exitRefused, "refused: %v", err)
	}
	if *model == "" {
		*model = spec.Judges["opus"]
	}
	path := fmt.Sprintf("%s-fix-%d", mesh.RepoToken(own), n)
	values := brief.RowValues(row, repoPath)
	values["node"], values["date"], values["plain_words"] = path, time.Now().UTC().Format("2006-01-02"), brief.PlainWords
	text, err := brief.Render("", "fix", values) // the rulings are bmesh's: no repo overrides them
	if err != nil {
		return err
	}
	briefPath, err := writeBrief(path, text)
	if err != nil {
		return err
	}
	started, err := a.startLead(leadStart{Name: own, RepoPath: repoPath, BriefPath: briefPath, Model: *model, Effort: *effort,
		Title: fmt.Sprintf("[fix] %s: feedback", own), Path: path, Family: path, Role: "fixer"})
	if err != nil {
		return err
	}
	out := fixStarted{leadStarted: started, Model: *model, Family: path, Host: a.cfg.HostName()}
	if a.json {
		return a.printJSON(out)
	}
	fmt.Fprintf(a.stdout, "fix: %s started (thread %s, %s, on %s)\n", path, started.Thread, *model, out.Host)
	return nil
}

// nextFixer refuses while a fixer of repo is live and otherwise returns
// one past the highest <repo>-fix-<N> registered in bm-nodes (as a path or
// a fix lead's family) or still holding feedback, so a fixer's family is
// never joined by the next.
func (a *app) nextFixer(ctx context.Context, repo string) (int, error) {
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return 0, err
	}
	states, err := mesh.ReadAllStates(ctx, a.client)
	if err != nil {
		return 0, err
	}
	pending, err := mesh.PendingFeedback(ctx, a.client)
	if err != nil {
		return 0, err
	}
	f := mesh.ReadFixers(nodes, states, pending, repo)
	if f.Live.Path != "" {
		return 0, fail(exitRefused, "refused: fixer %s is %s; one fixer writes the queue", f.Live.Path, cmp.Or(f.LiveState.State, "idle"))
	}
	return f.Next, nil
}

// fixRemote runs fix on host, the row's, and prints what it printed.
func (a *app) fixRemote(ctx context.Context, host, model, effort string) error {
	parts := []string{"bmesh", "fix"}
	if a.json {
		parts = append(parts, "--json")
	}
	if model != "" {
		parts = append(parts, "--model", model)
	}
	if effort != "" {
		parts = append(parts, "--effort", effort)
	}
	for i, p := range parts {
		parts[i] = shellQuote(p)
	}
	out, err := ssh(ctx, host, strings.Join(parts, " "))
	if err != nil {
		return fmt.Errorf("fix on %s: %w", host, err)
	}
	_, err = a.stdout.Write(out)
	return err
}
