package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/nats-io/nuid"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// newID names a message up front so `sent <id>` can print it and a retry
// with the same id is deduplicated by the stream.
func newID() string { return nuid.Next() }

// cmdSend messages a node: bmesh send <path> <text|->.
func (a *app) cmdSend(args []string) error {
	fs := a.flags("send")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return fail(exitUsage, "usage: bmesh send <path> <text|->")
	}
	to, err := mesh.ParsePath(pos[0])
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	text := pos[1]
	if text == "-" {
		b, err := io.ReadAll(a.stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		text = strings.TrimRight(string(b), "\n")
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	from, err := a.identity(ctx, "send")
	if err != nil {
		return err
	}
	m := mesh.Message{From: from.String(), To: to.String(), Kind: "message", Text: text}
	m.ID = newID()
	dup, err := mesh.Send(ctx, a.client, m)
	if err != nil {
		return err
	}
	verb := "sent"
	if dup {
		verb = "duplicate"
	}
	fmt.Fprintf(a.stdout, "%s %s\n", verb, m.ID)
	return nil
}

// cmdInbox prints this node's messages, acking them unless --peek.
func (a *app) cmdInbox(args []string) error {
	fs := a.flags("inbox")
	peek := fs.Bool("peek", false, "leave the messages unread")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "inbox takes no arguments")
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	p, err := a.identity(ctx, "inbox")
	if err != nil {
		return err
	}
	msgs, err := mesh.Fetch(ctx, a.client, p, !*peek)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		if a.json {
			b, err := json.Marshal(m)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "%s\n", b)
			continue
		}
		if m.Kind == "reply" && m.Reply != nil {
			// A reply read here is gone for `routine wait`, so show it whole.
			verdict := "ok"
			if !m.Reply.OK {
				verdict = "fail"
			}
			fmt.Fprintf(a.stdout, "%s %s: reply to job %s: %s %s\n%s", m.At, m.From, m.Reply.Job, m.Reply.Kind, verdict,
				withNewline(m.Reply.Output))
			continue
		}
		fmt.Fprintf(a.stdout, "%s %s: %s\n", m.At, m.From, m.Text)
	}
	return nil
}

func withNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
