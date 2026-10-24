package main

import (
	"encoding/json"
	"fmt"
	"os"
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

// contextLine is the line run prints last when this session's context has
// crossed a nudge since it last heard (094), else "".
func (a *app) contextLine() string {
	r, ok := session.Measure(os.Getenv)
	if !ok {
		return ""
	}
	c, ok := a.contextConfig()
	if !ok {
		return ""
	}
	return session.Heard(config.StateDir(), c, r, time.Now())
}

// cmdContext prints this session's context with the advice for it:
// `context 312k (claude session ec03a02c): <advice>`, or that no session
// it can measure runs it (exit 0). The session has then heard it, so the
// next command's line doesn't repeat it.
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
	r, ok := session.Measure(os.Getenv)
	advice := ""
	if ok {
		session.Heard(config.StateDir(), c, r, time.Now())
		advice = contextAdvice(c, r.Tokens)
	}
	if a.json {
		out := struct {
			Measured bool   `json:"measured"`
			Harness  string `json:"harness,omitempty"`
			Session  string `json:"session,omitempty"`
			Tokens   int    `json:"tokens,omitempty"`
			Advice   string `json:"advice,omitempty"`
		}{ok, r.Harness, r.ID, r.Tokens, advice}
		return json.NewEncoder(a.stdout).Encode(out)
	}
	if !ok {
		fmt.Fprintln(a.stdout, "context: no session bmesh can measure (Claude Code names its session in CLAUDE_CODE_SESSION_ID)")
		return nil
	}
	line := fmt.Sprintf("context %s (%s session %s)", session.K(r.Tokens), r.Harness, r.Short())
	if advice != "" {
		line += ": " + advice
	}
	fmt.Fprintln(a.stdout, line)
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
