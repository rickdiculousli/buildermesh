package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	tokenuse "github.com/rickdiculousli/buildermesh/internal/usage"
)

const usageUsage = "usage: bmesh usage [<path> | --session <id>] [--since D] [--repo R] [--calls] [--json]"

// maxSince is the widest window: the stream keeps 30 days.
const maxSince = 720 * time.Hour

// cmdUsage prints the token use every box put on the mesh (105): per
// agent, or one agent's in full, with --calls each of its model calls.
func (a *app) cmdUsage(args []string) error {
	fs := a.flags("usage")
	sess := fs.String("session", "", "a coordinator session: its id, or its first 8 characters")
	sinceText := fs.String("since", "24h", "how far back: a duration (90m, 24h, 7d), at most 720h")
	repo := fs.String("repo", "", "only this repo's agents")
	calls := fs.Bool("calls", false, "one line per model call (needs an agent)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || len(pos) == 1 && *sess != "" || *calls && len(pos) == 0 && *sess == "" {
		return fail(exitUsage, usageUsage)
	}
	window, err := sinceDuration(*sinceText)
	if err != nil {
		return fail(exitUsage, "--since %q: %v", *sinceText, err)
	}
	if window > maxSince {
		return fail(exitUsage, "--since %s exceeds 720h: the usage stream keeps 30 days", *sinceText)
	}
	f := tokenuse.Filter{Repo: *repo}
	switch {
	case *sess != "":
		if len(*sess) < 8 {
			return fail(exitUsage, "--session %q: give the whole id or its first 8 characters", *sess)
		}
		f.Agent = "session." + *sess
	case len(pos) == 1 && strings.HasPrefix(pos[0], "session."):
		f.Agent = pos[0]
	case len(pos) == 1:
		p, err := mesh.ParsePath(pos[0])
		if err != nil {
			return fail(exitUsage, "node %q: %v", pos[0], err)
		}
		f.Agent = p.String()
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "usage"); err != nil {
		return err
	}
	since := time.Now().Add(-window)
	rep, err := tokenuse.Read(ctx, a.client, since, f)
	if errors.Is(err, tokenuse.ErrNoStream) {
		return fail(exitError, "%v", err)
	}
	if err != nil {
		return err
	}
	if !*calls {
		rep.Calls, rep.Marks = nil, nil
	}
	if f.Agent != "" && len(rep.Agents) == 0 {
		if !strings.HasPrefix(f.Agent, "session.") {
			if _, ok, err := mesh.GetNode(ctx, a.client, f.Agent); err != nil {
				return err
			} else if !ok {
				return fail(exitRefused, "refused: no node %s", f.Agent)
			}
		}
		if a.json {
			return a.printJSON(rep)
		}
		fmt.Fprintf(a.stdout, "no usage for %s since %s\n", f.Agent, dayMinute(since))
		return nil
	}
	if a.json {
		return a.printJSON(rep)
	}
	if f.Agent == "" {
		printUsageTable(a, rep)
		return nil
	}
	for _, ag := range rep.Agents {
		printUsageAgent(a, ag)
	}
	if *calls {
		printUsageCalls(a, rep)
	}
	return nil
}

// sinceDuration reads a Go duration, or a whole number of days (7d).
func sinceDuration(s string) (time.Duration, error) {
	if n, ok := strings.CutSuffix(s, "d"); ok {
		d, err := strconv.Atoi(n)
		if err != nil || d <= 0 {
			return 0, errors.New("want a duration such as 90m, 24h or 7d")
		}
		return time.Duration(d) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, errors.New("want a duration such as 90m, 24h or 7d")
	}
	return d, nil
}

// dayMinute is a time as usage prints a day and minute: 10-23 21:00, local.
func dayMinute(t time.Time) string { return t.Local().Format("01-02 15:04") }

func stampOf(at string) string {
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return at
	}
	return dayMinute(t)
}

// share is part's share of whole, as a whole percent.
func share(part, whole int64) string {
	if whole == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", float64(part)*100/float64(whole))
}

func printUsageTable(a *app, rep tokenuse.Report) {
	tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "AGENT\tROLE\tHOST\tREAD\tOUT\tSUB\tPEAK\tDESIGN\tEXEC")
	var read, out int64
	for _, ag := range rep.Agents {
		design, exec := "-", "-"
		if ag.Design != nil && ag.Exec != nil {
			design, exec = share(ag.Design.Read(), ag.Read()), share(ag.Exec.Read(), ag.Read())
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", tokenuse.Short(ag.Agent), ag.Role, ag.Host,
			tokenuse.Tokens(ag.Read()), tokenuse.Tokens(ag.Main.Out+ag.Sub.Out), tokenuse.Tokens(ag.Sub.Read()), tokenuse.Tokens(ag.Peak), design, exec)
		read += ag.Read()
		out += ag.Main.Out + ag.Sub.Out
	}
	tw.Flush()
	fmt.Fprintf(a.stdout, "total %s read, %s out, %d agents since %s\n", tokenuse.Tokens(read), tokenuse.Tokens(out), len(rep.Agents), dayMinute(rep.Since))
}

func printUsageAgent(a *app, ag tokenuse.Agent) {
	w := a.stdout
	all := tokenuse.Kinds{In: ag.Main.In + ag.Sub.In, CR: ag.Main.CR + ag.Sub.CR, CW: ag.Main.CW + ag.Sub.CW, Out: ag.Main.Out + ag.Sub.Out}
	head := ag.Role + " on " + ag.Host
	if ag.Repo != "" {
		head += ", repo " + ag.Repo
	}
	if len(ag.Models) > 0 {
		head += ", " + strings.Join(ag.Models, " ")
	}
	fmt.Fprintf(w, "%s  %s, %s to %s, %d calls\n", tokenuse.Short(ag.Agent), head, stampOf(ag.First), stampOf(ag.Last), ag.Calls)
	fmt.Fprintf(w, "read     %s: input %s, cache read %s, cache write %s\n", tokenuse.Tokens(all.Read()), tokenuse.Tokens(all.In), tokenuse.Tokens(all.CR), tokenuse.Tokens(all.CW))
	fmt.Fprintf(w, "out      %s\n", tokenuse.Tokens(all.Out))
	subCalls := 0
	for _, s := range ag.Subs {
		subCalls += s.Calls
	}
	fmt.Fprintf(w, "main     %s read, %s out, %d calls\n", tokenuse.Tokens(ag.Main.Read()), tokenuse.Tokens(ag.Main.Out), ag.Calls-subCalls)
	fmt.Fprintf(w, "sub      %s read, %s out, %d calls in %d subagents\n", tokenuse.Tokens(ag.Sub.Read()), tokenuse.Tokens(ag.Sub.Out), subCalls, len(ag.Subs))
	for _, s := range ag.Subs {
		desc := s.ID
		if s.Desc != "" {
			desc += " " + s.Desc
		}
		fmt.Fprintf(w, "  %s: %s read, %s out, %d calls\n", desc, tokenuse.Tokens(s.Read()), tokenuse.Tokens(s.Out), s.Calls)
	}
	if ag.Design != nil && ag.Exec != nil {
		fmt.Fprintf(w, "design   %s: %s read, %s out\n", share(ag.Design.Read(), ag.Read()), tokenuse.Tokens(ag.Design.Read()), tokenuse.Tokens(ag.Design.Out))
		fmt.Fprintf(w, "exec     %s: %s read, %s out\n", share(ag.Exec.Read(), ag.Read()), tokenuse.Tokens(ag.Exec.Read()), tokenuse.Tokens(ag.Exec.Out))
	}
	fmt.Fprintf(w, "context  peak %s, last %s, %d compactions\n", tokenuse.Tokens(ag.Peak), tokenuse.Tokens(ag.LastCtx), ag.Compactions)
}

// printUsageCalls prints each call, a subagent's after its id, with the
// context it read, its growth over the thread's previous call, its output
// and its cause; a compaction is its own line.
func printUsageCalls(a *app, rep tokenuse.Report) {
	w := a.stdout
	fmt.Fprintln(w)
	prev := map[string]int64{}
	marks := rep.Marks
	clock := func(at string) string {
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
			return t.Local().Format("15:04:05")
		}
		return at
	}
	who := func(sub string) string {
		if sub == "" {
			return ""
		}
		return sub + "  "
	}
	for _, c := range rep.Calls {
		for len(marks) > 0 && marks[0].At <= c.At {
			fmt.Fprintf(w, "%s%s  compact\n", who(marks[0].Sub), clock(marks[0].At))
			marks = marks[1:]
		}
		growth := c.Ctx - prev[c.Sub]
		prev[c.Sub] = c.Ctx
		sign := "+"
		if growth < 0 {
			sign, growth = "-", -growth
		}
		line := fmt.Sprintf("%s%s  ctx %s  %s%s  out %s", who(c.Sub), clock(c.At), tokenuse.Tokens(c.Ctx), sign, tokenuse.Tokens(growth), tokenuse.Tokens(c.Out))
		if c.Phase != "" {
			line += "  " + c.Phase
		}
		if c.Cause != "" {
			line += "  " + c.Cause
		}
		fmt.Fprintln(w, line)
	}
	for _, m := range marks {
		fmt.Fprintf(w, "%s%s  compact\n", who(m.Sub), clock(m.At))
	}
}
