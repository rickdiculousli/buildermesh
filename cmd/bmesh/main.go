// Command bmesh is the buildermesh CLI: nodes report events, message each
// other and watch the mesh through it.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

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
Every command but init, help, version, hosts --here and update --notice needs the box set up (the flag
<config dir>/.setup-complete or a config.toml): a terminal run runs init first, a non-interactive one is
refused (exit 3).

commands:
  init [--service] [--force] [--role hub|leaf|client] [--hub URL] [--token-file F] [--runner K] [--no-repo]
                                           set this box up: config.toml (multi-box: [hub] and [mesh] for a hub,
                                           leaf or client; a hub makes <config dir>/nats.token), this repo in
                                           repos.toml (it needs a main branch), --service the unit for bmesh serve
                                           (on Windows, the schtasks commands for a logon task running serve
                                           --restart) and with [hub] role the one for bmesh hub; asks what the
                                           flags don't say, then prints every setting in effect and the key that
                                           changes it
  init --hosts [--admin]                   join this box to the mesh with a one-time invite code (asked at the
                                           prompt), or --admin make it the admin; writes only [hosts] and, with
                                           routines off, [runner] checkouts = "" (keeps config.toml.prev)
  hosts [--json]                           the roster joined with presence: awake, asleep, unverified or absent
  hosts --here                             this box's session line (<state dir>/this-box.txt, the daemon's);
                                           nothing when [hosts] is off or the file is missing; never dials the hub
  hosts --notes [--json]                   every box's verified notes (<config dir>/notes.md, in its presence)
  hosts invite <name> [--secure] [--accept a,b] [--replace]
                                           (admin) print a one-time code for a box to join with; waits 10m
  hosts set <name> [--secure=true|false] [--accept a,b]
                                           (admin) change a box's row in the roster
  hosts revoke <name>                      (admin) publish the next roster without the box
  hosts ping [<host>] [--wait D]           ping every roster box (or one); print each answer, or asleep: queued
  hosts replies [--json]                   the replies this box's jobs got, checked
  hosts held [--json]                      the levels this secure box holds for the operator, and on the admin
                                           box the registry proposals
  hosts approve <id>                       show a hold's log, sign the approval with the approve key (the
                                           announced Keychain dialog) and level; asks again when origin moved;
                                           a registry proposal is published as the next registry version
  hosts deny <id>                          drop a hold, answering its sender
  sync [--repo R | --all] [--wait 30s]     level this box's checkout (pushing when ahead and allowed), then send
                                           level with origin's head to every box that has the repo; print each
                                           answer within --wait, or asleep, queued until <expires>
  repos [--json]                           the registry (repos.toml): name, mode, remote, verify, host, ux_host,
                                           surfaces, pin; --json prints the rows
  repos show <name> [--json]               one row
  repos publish                            (admin) print the row diff from the current registry and publish this
                                           box's repos.toml as its next version, signed with the admin key
  repos propose [--wait 30s]               send this box's repos.toml, as a diff from its registry, to the admin
                                           box, which holds it for the operator's approval
  repos remove <name> [--checkouts [--ignored-ok]] [--apply] [--wait 1m]
                                           (admin) publish the registry without the row; --checkouts first asks
                                           every box that lists it to nix its checkout, and publishes only once
                                           each answered removed or absent; with [hosts] off, edit repos.toml
  prune [--repo R | --all] [--apply] [--wait 30s]
                                           judge this box's bmesh worktrees and bmesh/* branches (keep or remove,
                                           with the reason), then every other box that accepts prune; a dry run
                                           unless --apply
  whoami                                   print this node's path
  started|progress|blocked|ask [--text T]  report an event; print <event> <path>
  done --commit SHA [--text T]             report done (a lead's spec must have landed, verified by results); print
                                           done <path> <sha8>, or already done at <sha8> for a repeat
  send <path> <text|->                     message a node (- reads stdin)
  inbox [--peek] [--json]                  read (and ack) this node's messages; inbox: empty when none
  thread-status [<path>] [--json]          print a node's state
  mesh-status [--all] [--json]             every node's state, runner and unread count; nodes done longer than
                                           [status] keep_done are counted, not listed, unless --all (or --json)
  node forget <path> [--worktree [--wait 30s]]
                                           delete a done or blocked node (or an idle one that never reported and
                                           whose thread isn't running), its state and inbox; --worktree also
                                           removes its clean worktree and merged branch, on another box through
                                           a forget-worktree job (the node stays until that box answers)
  watch --nodes a,b [--since T] [--timeout D]
                                           follow nodes until all are done, blocked or silent
  lead start --repo R --spec NNN --brief FILE [--model M] [--effort E] [--title T] [--path P]
                                           start a lead through the runner and register it
  ideate <repo> [--model M] [--effort E] [--context <text|->] [--force]
                                           run a proposal round: an engineer and a UI/UX engineer thread, each in a
                                           bmesh worktree from main, as nodes <repo>-round-<N>.eng|ux; --context
                                           puts the operator's words in both briefs
  choose-ideas <repo> "<reply>"            apply the operator's review to the proposed specs and land it on main
  build <repo> <NNN> [--model M] [--effort E] [--path P] [--family F]
                                           dispatch an accepted spec: record building on main, render the build
                                           brief and start the lead (as lead start); the lead joins build family
                                           F, else a fresh <repo>-build-<N>
  build --accepted <repo> [--model M] [--effort E] [--family F]
                                           dispatch every accepted spec, lowest id first, a spec naming a lower id
                                           after that one is done, three leads at a time; print what waits
  release <repo> [--minor|--patch] [--dry-run]
                                           tag main's code head as the next vX.Y.Z (v0.1.0 first; minor up when a
                                           feature landed since, else patch) and push the tag; refused while a lead
                                           works, feedback is pending, verify isn't ok or only specs changed
  feedback "<text>" [--repo R] [--where <file|command>] [--fixed SHA]
                                           a lead's lesson for its build family's coordinator, before done
  feedback list <family> [--json]          the family's pending feedback, each with its stream sequence
  feedback drain <family> [--through SEQ]  delete the family's feedback from the stream, up to SEQ
  scaffold <repo> [--check]                give a ship repo its AGENTS.md sections, docs/proposals and the routine
                                           kinds its row names; --check prints the diff and exits 1 when any
  executor new --name N [--base REF] [--model M] [--json]
                                           make a worktree, branch and node for an executor under this node;
                                           print {"path","branch","worktree"} and (without --json) its header
  executor list [--json]                   this lead's tree: path, state, branch, head, unread
  executor remove <name> [--force]         remove a merged executor's worktree and branch (exit 3 unmerged)
  serve [--listen ADDR] [--restart [--detach]]
                                           run the daemon and the map (embedded NATS when [mesh] nats is empty);
                                           with [update] repo, it rebuilds itself when that repo's main moves;
                                           else every [update] check_every it writes the notice when behind;
                                           --restart runs it as a child, started again 5s after every exit;
                                           --detach (Windows) starts that with no console window and returns
  hub                                      run this box's NATS hub or leaf from [hub] (role hub: JetStream, the
                                           leaf port; role leaf: dials [hub] remote forever) until stopped; its
                                           own process apart from serve, so an update never drops the mesh;
                                           exit 3 naming the key when role, listen or the token file won't do
  t3 login [--server URL] [--callback-port 52299] [--timeout D]
                                           sign bmesh in to this box's T3 0.1.0 (default [runner] t3_server):
                                           print the authorize URL, take the redirect on localhost or pasted,
                                           write <config dir>/t3-token.json; lasts 30 days
  t3 login --status                        print the sign-in: <host>: <access>, N days left; server and scopes
  version                                  print this binary's commit: <rev8> <time> [modified], or unknown;
                                           <release> (<rev8> <time>) when built at or past a release tag
  update [--repo R]                        build R's main (default [update] repo, else the row this binary's
                                           module is from; with none, go install the module's main) over this
                                           binary, keeping it as bmesh.prev; print <old8> -> <new8>, or up to date
                                           (then the restart this box's daemon needs, when it runs another build)
  update --notice                          print the daemon's behind-main notice (<state dir>/update.txt), if any
  land [--spec NNN] [--ttl D] [--wait D]   land this worktree's branch on main under the lease land.<repo> (waits
                                           while another lead holds it): print the kinds it requires (a branch
                                           rebased by hand requires what the head before the rebase had), fetch,
                                           rebase, run or require the results, write verified, set done with merged
                                           and branch, commit, push; a push main refuses starts again from the fetch
                                           once; prints landed <sha8>
  result find <kind> [--commit C] [--model M] [--json] [-- args...]
                                           print the result of kind at the commit's tree, without --model every
                                           model's (exit 3 when none)
  result record <kind> --ok|--fail [--commit C] [--model M] [--output FILE|-] [-- args...]
                                           record a result of kind at the commit's tree
  lock-lease acquire <slot> [--ttl D]      take a place in a slot and print its key (exit 3 when busy)
  lock-lease renew <key> [--ttl D]         extend a held lease
  lock-lease release <key>                 give back a held lease
  lock-lease run <slot> [--ttl D] -- <cmd...>
                                           hold a lease while a command runs; exit with its code
  routine list [--json]                    the repo's kinds with their instances and queue depth
  routine status [<kind>] [--json]         a kind's (or every kind's) instances with state and model
  routine queue <kind> [--commit C] [--model M] [--go] [--rerun] [--again] [--text T] [-- args...]
                                           queue a job (a deterministic kind answers from a recorded result; a
                                           review for a branch that has one is refused without --again)
  routine go <kind>                        tell the kind's instances to start on waiting jobs
  routine wait <job id> [--timeout D]      wait for a job's reply; print its output, exit 0 ok / 1 fail / 5 timeout
  routine run <kind> [--commit HEAD] [--model M] [--here] [--rerun] [--text T] [-- args...]
                                           run a kind in this worktree and record its result; a deterministic
                                           kind needs a clean tree and answers from a recorded result; ends with
                                           <kind> ok|fail t:<tree8>; exit 0 ok / 1 fail
  spec list [--repo R] [--status S] [--json]
                                           the repo's specs, one row each (id, status, size, kind, lens, title)
  spec next-id [--repo R]                  the next free spec id
  spec set --id NNN --status S [--repo R] [--from A,B] [--field K=V]...
                                           set a spec's status and fields (--from refuses another status)
  spec template [--repo R]                 print the half-pager (the repo's .bmesh/briefs/half-pager.md, else embedded)
  spec verified [--spec NNN] [--commit C] [--write]
                                           print the verified line from the results for HEAD's tree, or C's (exit 3
                                           when none); --write sets it in the spec's file in this worktree
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
	in             *bufio.Reader // stdin's one buffered reader (lines)
	stdout, stderr io.Writer
	configPath     string
	node           string
	json           bool
	cfg            *config.Config
	client         *mesh.Client
	hint           string // the revision hint, printed once after the command's output
}

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr}
	defer func() {
		if a.client != nil {
			a.client.Close()
		}
	}()
	code := a.report(a.dispatch(args))
	if a.hint != "" {
		fmt.Fprintln(stderr, a.hint)
	}
	return code
}

// report prints err as "bmesh: <msg>" on stderr and returns the exit code.
func (a *app) report(err error) int {
	if err == nil {
		return exitOK
	}
	var ee *exitErr
	if errors.As(err, &ee) {
		if ee.msg != "" {
			fmt.Fprintf(a.stderr, "bmesh: %s\n", ee.msg)
		}
		return ee.code
	}
	fmt.Fprintf(a.stderr, "bmesh: %s\n", err)
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
	// hosts --here runs from a session hook: it reads one file and never
	// sets the box up or refuses, so a session on a box without bmesh set
	// up opens as quietly as one with [hosts] off.
	// So does update --notice, beside it in the same hook.
	here := cmd == "hosts" && slices.Contains(rest, "--here") || cmd == "update" && slices.Contains(rest, "--notice")
	if cmd != "init" && cmd != "help" && cmd != "version" && !here {
		if err := a.ensureSetup(rest); err != nil {
			return err
		}
	}
	switch cmd {
	case "init":
		return a.cmdInit(rest)
	case "repos":
		return a.cmdRepos(rest)
	case "whoami":
		return a.cmdWhoami(rest)
	case "started", "progress", "blocked", "ask", "done":
		return a.cmdEvent(cmd, rest)
	case "send":
		return a.cmdSend(rest)
	case "inbox":
		return a.cmdInbox(rest)
	case "thread-status":
		return a.cmdThreadStatus(rest)
	case "mesh-status":
		return a.cmdMeshStatus(rest)
	case "watch":
		return a.cmdWatch(rest)
	case "lead":
		return a.cmdLead(rest)
	case "ideate":
		return a.cmdIdeate(rest)
	case "choose-ideas":
		return a.cmdChooseIdeas(rest)
	case "build":
		return a.cmdBuild(rest)
	case "release":
		return a.cmdRelease(rest)
	case "feedback":
		return a.cmdFeedback(rest)
	case "scaffold":
		return a.cmdScaffold(rest)
	case "serve":
		return a.cmdServe(rest)
	case "hub":
		return a.cmdHub(rest)
	case "land":
		return a.cmdLand(rest)
	case "result":
		return a.cmdResult(rest)
	case "lock-lease":
		return a.cmdLease(rest)
	case "routine":
		return a.cmdRoutine(rest)
	case "spec":
		return a.cmdSpec(rest)
	case "executor":
		return a.cmdExecutor(rest)
	case "node":
		return a.cmdNode(rest)
	case "t3":
		return a.cmdT3(rest)
	case "version":
		return a.cmdVersion(rest)
	case "update":
		return a.cmdUpdate(rest)
	case "hosts":
		return a.cmdHosts(rest)
	case "sync":
		return a.cmdSync(rest)
	case "prune":
		return a.cmdPrune(rest)
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
	fs.StringVar(&a.configPath, "config", a.configPath, "config file (default $BMESH_CONFIG, else config.toml in the config dir)")
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
				return nil, fail(exitUsage, "usage: bmesh %s%s (see `bmesh help`)", fs.Name(), ownFlags(fs))
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

// ownFlags names the flags a subcommand adds to the global ones, as
// " [--a] [--b]", or "" when it has none.
func ownFlags(fs *flag.FlagSet) string {
	var b strings.Builder
	fs.VisitAll(func(f *flag.Flag) {
		if f.Name != "config" && f.Name != "node" && f.Name != "json" {
			fmt.Fprintf(&b, " [--%s]", f.Name)
		}
	})
	return b.String()
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
	for _, l := range cfg.Ignored() {
		fmt.Fprintln(a.stderr, l)
	}
	a.cfg = cfg
	return nil
}

// connect loads the config and dials the hub ([mesh] nats, else the hub
// bmesh serve embeds on [mesh] listen); name labels the connection. Once
// connected it reads the revision hint, which run prints after the
// command's output, when the daemon runs another build.
func (a *app) connect(ctx context.Context, name string) error {
	if a.client != nil {
		return nil
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	token, err := a.cfg.Token()
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	url, embedded := a.cfg.HubURL()
	domain := a.cfg.Mesh.Domain
	if embedded {
		domain = "hub" // the embedded server's domain, as serve dials it
		if _, port, err := net.SplitHostPort(a.cfg.Mesh.Listen); err == nil && port == "0" {
			return fail(exitError, "[mesh] listen %s lets bmesh serve pick a port, which the CLI can't know: give it one, or set [mesh] nats", a.cfg.Mesh.Listen)
		}
	}
	c, err := mesh.Connect(ctx, url, token, domain, "bmesh/"+name, false)
	if embedded && errors.Is(err, nats.ErrNoServers) {
		return fail(exitError, "no hub at %s: is bmesh serve running?", strings.TrimPrefix(url, "nats://"))
	}
	if err != nil {
		return err
	}
	a.client = c
	a.versionHint(ctx)
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
