// Command bmesh is the buildermesh CLI: nodes report events, message each
// other and watch the mesh through it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// Exit codes.
const (
	exitOK      = 0
	exitError   = 1
	exitUsage   = 2
	exitRefused = 3
	exitTimeout = 5
)

const usage = `usage: bmesh [--config FILE] [--node PATH] [--json] <command> [args]

--config, --node and --json are accepted before or after the command.

commands:
  whoami                                   print this node's path
  started|progress|blocked|ask [--text T]  report an event
  done --commit SHA [--text T]             report done (the spec must have landed)
  send <path> <text|->                     message a node (- reads stdin)
  inbox [--peek] [--json]                  read (and ack) this node's messages
  state [<path>] [--json]                  print a node's state
  status [--json]                          every node's state, runner and unread count
  watch --nodes a,b [--since T] [--timeout D]
                                           follow nodes until all are done, blocked or silent
  lead start --repo R --spec NNN --brief FILE [--model M] [--title T] [--path P]
                                           start a lead through the runner and register it
  serve [--listen ADDR]                    run the daemon and the map (embedded NATS when [mesh] nats is empty)
  result find <kind> [--commit C] [--model M] [--json] [-- args...]
                                           print the result of kind at the commit's tree (exit 3 when none)
  result record <kind> --ok|--fail [--commit C] [--model M] [--output FILE|-] [-- args...]
                                           record a result of kind at the commit's tree
  lease acquire <slot> [--ttl D]           take a place in a slot and print its key (exit 3 when busy)
  lease renew <key> [--ttl D]              extend a held lease
  lease release <key>                      give back a held lease
  lease run <slot> [--ttl D] -- <cmd...>   hold a lease while a command runs; exit with its code
  routine list [--json]                    the repo's kinds with their instances and queue depth
  routine status [<kind>] [--json]         a kind's (or every kind's) instances with state and model
  routine queue <kind> [--commit C] [--model M] [--go] [--rerun] [--text T] [-- args...]
                                           queue a job (a deterministic kind answers from a recorded result)
  routine go <kind>                        tell the kind's instances to start on waiting jobs
  routine wait <job id> [--timeout D]      wait for a job's reply; print its output, exit 0 ok / 1 fail / 5 timeout
  routine run <kind> [--commit HEAD] [--model M] [--here] [--rerun] [--text T] [-- args...]
                                           run a kind in this worktree and record its result; a deterministic
                                           kind needs a clean tree and answers from a recorded result; exit 0 ok / 1 fail
`

// exitErr carries an exit code and the message printed as "bmesh: <msg>".
type exitErr struct {
	code int
	msg  string
}

func (e *exitErr) Error() string { return e.msg }

func fail(code int, format string, a ...any) error {
	return &exitErr{code: code, msg: fmt.Sprintf(format, a...)}
}

// app is one invocation's shared state.
type app struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	configPath     string
	node           string
	json           bool
	cfg            *config.Config
	client         *mesh.Client
}

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr}
	defer func() {
		if a.client != nil {
			a.client.Close()
		}
	}()
	err := a.dispatch(args)
	if err == nil {
		return exitOK
	}
	var ee *exitErr
	if errors.As(err, &ee) {
		if ee.msg != "" {
			fmt.Fprintf(stderr, "bmesh: %s\n", ee.msg)
		}
		return ee.code
	}
	fmt.Fprintf(stderr, "bmesh: %s\n", err)
	return exitError
}

func (a *app) dispatch(args []string) error {
	fs := a.flags("bmesh")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(a.stdout, usage)
			return nil
		}
		return fail(exitUsage, "%v", err)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprint(a.stderr, usage)
		return fail(exitUsage, "")
	}
	cmd, rest := rest[0], rest[1:]
	switch cmd {
	case "whoami":
		return a.cmdWhoami(rest)
	case "started", "progress", "blocked", "ask", "done":
		return a.cmdEvent(cmd, rest)
	case "send":
		return a.cmdSend(rest)
	case "inbox":
		return a.cmdInbox(rest)
	case "state":
		return a.cmdState(rest)
	case "status":
		return a.cmdStatus(rest)
	case "watch":
		return a.cmdWatch(rest)
	case "lead":
		return a.cmdLead(rest)
	case "serve":
		return a.cmdServe(rest)
	case "result":
		return a.cmdResult(rest)
	case "lease":
		return a.cmdLease(rest)
	case "routine":
		return a.cmdRoutine(rest)
	case "help":
		fmt.Fprint(a.stdout, usage)
		return nil
	}
	return fail(exitUsage, "unknown command %q (run `bmesh help`)", cmd)
}

// flags returns a FlagSet that reports errors through exit code 2 and has
// the global flags (--config, --node, --json) bound, so they are accepted
// before or after the subcommand. The config is loaded lazily, after the
// subcommand's flags are parsed, so a --config given late still counts.
func (a *app) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&a.configPath, "config", a.configPath, "config file (default $BMESH_CONFIG or ~/.config/bmesh/config.toml)")
	fs.StringVar(&a.node, "node", a.node, "act as this node path")
	fs.BoolVar(&a.json, "json", a.json, "print JSON")
	return fs
}

// parse parses flags interleaved with positional arguments and returns the
// positionals; any parse error is a usage error.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, fail(exitUsage, "usage: bmesh %s (see `bmesh help`)", fs.Name())
			}
			return nil, fail(exitUsage, "%s: %v", fs.Name(), err)
		}
		rest := fs.Args()
		// fs.Parse drops a "--" that ends the flags; what follows it is
		// positional even when it looks like a flag.
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" {
			return append(pos, rest...), nil
		}
		args = rest
		if len(args) == 0 {
			return pos, nil
		}
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func (a *app) loadConfig() error {
	if a.cfg != nil {
		return nil
	}
	p := a.configPath
	if p == "" {
		p = config.Path()
	}
	cfg, err := config.Load(p)
	if err != nil {
		return err
	}
	a.cfg = cfg
	return nil
}

// connect loads the config and dials the hub; name labels the connection.
func (a *app) connect(ctx context.Context, name string) error {
	if a.client != nil {
		return nil
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	if a.cfg.Mesh.NATS == "" {
		return fail(exitError, "[mesh] nats is empty; the CLI needs a hub URL")
	}
	token, err := a.cfg.Token()
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	c, err := mesh.Connect(ctx, a.cfg.Mesh.NATS, token, a.cfg.Mesh.Domain, "bmesh/"+name, false)
	if err != nil {
		return err
	}
	a.client = c
	return nil
}

// errNoNode is identity's error when nothing names this node.
var errNoNode = fail(exitUsage, "no node for this directory; pass --node or set BMESH_NODE")

// identity resolves this node's path: --node, else BMESH_NODE, else the
// registered node whose worktree is this checkout on this host. It connects.
func (a *app) identity(ctx context.Context, cmd string) (mesh.Path, error) {
	given := a.node
	if given == "" {
		given = os.Getenv("BMESH_NODE")
	}
	if given != "" {
		p, err := mesh.ParsePath(given)
		if err != nil {
			return nil, fail(exitUsage, "node %q: %v", given, err)
		}
		return p, a.connect(ctx, p.String())
	}
	if err := a.connect(ctx, cmd); err != nil {
		return nil, err
	}
	top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, errNoNode
	}
	n, ok, err := mesh.NodeForWorktree(ctx, a.client, strings.TrimSpace(string(top)), a.cfg.HostName())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNoNode
	}
	return mesh.ParsePath(n.Path)
}

// identityOrHost is identity for commands a person may run outside any node
// (result, lease): when no node resolves it acts as this host's name.
func (a *app) identityOrHost(ctx context.Context, cmd string) (string, error) {
	p, err := a.identity(ctx, cmd)
	if errors.Is(err, errNoNode) {
		return a.cfg.HostName(), nil
	}
	if err != nil {
		return "", err
	}
	return p.String(), nil
}

// cmdCtx bounds a one-shot command.
func cmdCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Minute)
}
