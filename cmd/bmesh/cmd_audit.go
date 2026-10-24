package main

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/audit"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/scaffold"
	"github.com/rickdiculousli/buildermesh/internal/session"
)

// cmdAudit prints what a Claude Code session's transcript says it did
// (101): per request, the rung its reply named, the rung its actions
// imply, and what it read before its first action. The session is this
// one, a node's (the id its started kept, on the box it ran on), or one
// named with --session.
func (a *app) cmdAudit(args []string) error {
	fs := a.flags("audit")
	id := fs.String("session", "", "a Claude Code session id")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || len(pos) == 1 && *id != "" {
		return fail(exitUsage, "usage: bmesh audit [<node path> | --session <id>] [--json]")
	}
	switch {
	case len(pos) == 1:
		p, err := mesh.ParsePath(pos[0])
		if err != nil {
			return fail(exitUsage, "%v", err)
		}
		ctx, cancel := cmdCtx()
		defer cancel()
		if err := a.connect(ctx, "audit"); err != nil {
			return err
		}
		n, ok, err := mesh.GetNode(ctx, a.client, p.String())
		if err != nil {
			return err
		}
		if !ok {
			return fail(exitError, "node %s is not registered", p)
		}
		if n.Session == "" {
			return fail(exitRefused, "refused: %s's record names no session (its started ran outside Claude Code, under another node, or before bmesh 101); find its transcript on %s and run bmesh audit --session <id> there", p, cmp.Or(n.Host, "its box"))
		}
		if here := a.cfg.HostName(); n.Host != "" && n.Host != here {
			return fail(exitRefused, "refused: %s ran on %s, not this box (%s); run bmesh audit %s on %s", p, n.Host, here, p, n.Host)
		}
		*id = n.Session
	case *id == "":
		var ok bool
		if *id, ok = session.ID(os.Getenv); !ok {
			fmt.Fprintln(a.stdout, "audit: no session bmesh can read (Claude Code names its session in CLAUDE_CODE_SESSION_ID)")
			return nil
		}
	}
	path, ok := session.Transcript(os.Getenv, *id)
	if !ok {
		return fail(exitError, "no transcript of session %s on this box (projects/*/%s.jsonl under CLAUDE_CONFIG_DIR, else ~/.claude)", *id, *id)
	}
	au, err := audit.File(path)
	if err != nil {
		return err
	}
	au.Rules = rulesAt(au.Cwd)
	if a.json {
		return a.printJSON(au)
	}
	printAudit(a, au)
	return nil
}

// rulesAt are the rule sets on the bmesh:rules marker of the AGENTS.md at
// the top of dir's checkout; nil when dir is gone or has none.
func rulesAt(dir string) []string {
	if dir == "" {
		return nil
	}
	top, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(top)), scaffold.Agents))
	if err != nil {
		return nil
	}
	sets, _ := scaffold.Rules(string(b))
	return sets
}

func printAudit(a *app, au *audit.Audit) {
	w := a.stdout
	fmt.Fprintf(w, "audit %s (claude, %s)\n", short8(au.Session), au.Transcript)
	rules := "none on its AGENTS.md's bmesh:rules marker"
	if len(au.Rules) > 0 {
		rules = strings.Join(au.Rules, ", ")
	} else if au.Cwd == "" {
		rules = "unknown (no cwd in the transcript)"
	}
	fmt.Fprintf(w, "ran in %s; rules: %s\n", cmp.Or(au.Cwd, "an unknown dir"), rules)
	fmt.Fprintf(w, "main thread: %d calls, context %s at the last, %s read; subagents: %d, %d calls, %s read\n",
		au.Calls, session.K(au.Tokens), session.K(int(au.Read)), au.Subagents.Count, au.Subagents.Calls, session.K(int(au.Subagents.Read)))
	fmt.Fprintf(w, "implied rung, first rule that holds: %s\n", audit.Rules)
	for _, t := range au.Turns {
		fmt.Fprintf(w, "\nturn %d %s %q\n", t.N, t.At, t.Words)
		if t.Named != "" {
			fmt.Fprintf(w, "  said: %s (%q)\n", t.Named, t.NamedLine)
		} else {
			fmt.Fprintln(w, "  said: no rung")
		}
		did := fmt.Sprintf("  did: %s (%s)", t.Implied, t.Rule)
		if t.Mismatch {
			did += fmt.Sprintf("; mismatch: said %s, did %s", t.Named, t.Implied)
		}
		fmt.Fprintln(w, did)
		if t.Acted != "" {
			fmt.Fprintf(w, "  first action: %s, at context %s; read before it:\n", t.Acted, session.K(t.Context))
		} else {
			fmt.Fprintln(w, "  no action that changes something; read:")
		}
		for _, r := range t.Read {
			what := r.Kind + " " + r.Name
			if r.Range != "" {
				what += " (" + r.Range + ")"
			}
			fmt.Fprintf(w, "    %s, %d words\n", what, r.Words)
		}
		if t.Other.Calls > 0 {
			fmt.Fprintf(w, "    other tool output: %d calls, %d words\n", t.Other.Calls, t.Other.Words)
		}
		if len(t.Read) == 0 && t.Other.Calls == 0 {
			fmt.Fprintln(w, "    nothing")
		}
	}
}
