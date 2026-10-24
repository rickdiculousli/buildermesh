package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/session"
)

// contextConfig is [context] from the config the command loaded, else from
// the config file, else false: a config that won't load says nothing.
func (a *app) contextConfig() (config.ContextConfig, bool) {
	if a.cfg != nil {
		return a.cfg.Context, true
	}
	p := a.configPath
	if p == "" {
		p = config.Path()
	}
	cfg, err := config.Load(p)
	if err != nil {
		return config.ContextConfig{}, false
	}
	return cfg.Context, true
}

// childNode is the node this command acts as when it has a parent (an
// executor, a round's engineer, a routine instance), else "": those never
// hear a context, which is their lead's to act on.
func (a *app) childNode() string {
	if p := cmp.Or(a.self, a.givenNode()); strings.Contains(p, ".") {
		return p
	}
	return ""
}

// contextLine is the line run prints last when this session's context has
// crossed a nudge, or dropped, since it last heard (094), else "".
func (a *app) contextLine() string {
	if a.childNode() != "" {
		return ""
	}
	r, ok := session.Measure(os.Getenv)
	if !ok {
		return ""
	}
	c, ok := a.contextConfig()
	if !ok {
		return ""
	}
	line, _ := session.Heard(config.StateDir(), c, r, time.Now())
	return line
}

// cmdContext prints this session's context with the advice for it:
// `context 312k (claude session ec03a02c): <advice>`, or that no session
// it can measure runs it (exit 0). The session has then heard it, so the
// next command's line doesn't repeat it; a compaction's line, which the
// advice doesn't carry, goes to stderr.
func (a *app) cmdContext(args []string) error {
	fs := a.flags("context")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fail(exitUsage, "usage: bmesh context [--json]")
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	c := a.cfg.Context
	child := a.childNode()
	var r session.Reading
	ok := false
	if child == "" {
		r, ok = session.Measure(os.Getenv)
	}
	advice := ""
	if ok && r.Main {
		if line, compacted := session.Heard(config.StateDir(), c, r, time.Now()); compacted {
			fmt.Fprintln(a.stderr, line)
		}
		advice = contextAdvice(c, r.Tokens)
	}
	if a.json {
		out := struct {
			Measured bool   `json:"measured"`
			Harness  string `json:"harness,omitempty"`
			Session  string `json:"session,omitempty"`
			Tokens   int    `json:"tokens,omitempty"`
			Main     bool   `json:"main,omitempty"`
			Advice   string `json:"advice,omitempty"`
		}{ok, r.Harness, r.ID, r.Tokens, r.Main, advice}
		return json.NewEncoder(a.stdout).Encode(out)
	}
	switch {
	case child != "":
		fmt.Fprintf(a.stdout, "context: none for %s, a node under another: its lead's session acts on the context\n", child)
	case !ok:
		fmt.Fprintln(a.stdout, "context: no session bmesh can measure (Claude Code names its session in CLAUDE_CODE_SESSION_ID)")
	case !r.Main:
		fmt.Fprintf(a.stdout, "context %s (%s session %s, not on its own thread: its main thread acts on it)\n", session.K(r.Tokens), r.Harness, r.Short())
	case advice != "":
		fmt.Fprintf(a.stdout, "context %s (%s session %s): %s\n", session.K(r.Tokens), r.Harness, r.Short(), advice)
	default:
		fmt.Fprintf(a.stdout, "context %s (%s session %s)\n", session.K(r.Tokens), r.Harness, r.Short())
	}
	return nil
}

// contextAdvice is the advice at tokens, or below nudge_from when the
// first nudge is still ahead; "" with nudges off.
func contextAdvice(c config.ContextConfig, tokens int) string {
	if c.NudgeFrom <= 0 {
		return ""
	}
	if level := session.Level(c, tokens); level > 0 {
		return session.Advice(c, level)
	}
	return "Nothing to do before " + session.K(c.NudgeFrom) + "."
}
