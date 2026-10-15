package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const specUsage = "usage: bmesh spec verified [--spec NNN] [--write]"

func (a *app) cmdSpec(args []string) error {
	if len(args) > 0 && args[0] == "verified" {
		return a.cmdSpecVerified(args[1:])
	}
	return fail(exitUsage, specUsage)
}

// cmdSpecVerified prints the verified line composed from the results for
// the cwd repo's HEAD tree; --write sets it in the spec's file in this
// worktree. The spec defaults to this node's.
func (a *app) cmdSpecVerified(args []string) error {
	fs := a.flags("spec verified")
	id := fs.String("spec", "", "spec id (default this node's spec)")
	write := fs.Bool("write", false, "write the line into the spec's frontmatter")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, specUsage)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	p, err := a.identity(ctx, "spec")
	switch {
	case errors.Is(err, errNoNode):
		p = nil
	case err != nil:
		return err
	}
	top, err := gitRev("--show-toplevel")
	if err != nil {
		return err
	}
	entries, err := spec.ComposeVerified(ctx, a.client, top)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fail(exitRefused, "%v", spec.ErrNoResults)
	}
	fmt.Fprintf(a.stdout, "verified: %s\n", spec.FormatVerified(entries))
	if !*write {
		return nil
	}
	if *id == "" && p != nil {
		n, ok, err := mesh.GetNode(ctx, a.client, p.String())
		if err != nil {
			return err
		}
		if ok {
			*id = n.Spec
		}
	}
	if *id == "" {
		return fail(exitUsage, "spec verified --write needs --spec NNN (no node spec resolves)")
	}
	pattern := filepath.Join(top, filepath.FromSlash(strings.ReplaceAll(a.cfg.Spec.Glob, "{id}", *id)))
	hits, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("bad spec glob %q: %w", a.cfg.Spec.Glob, err)
	}
	if len(hits) != 1 {
		return fail(exitRefused, "spec %s matches %d files in %s", *id, len(hits), top)
	}
	if err := spec.WriteVerified(hits[0], entries); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s\n", hits[0])
	return nil
}
