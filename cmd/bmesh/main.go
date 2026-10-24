// Command bmesh is the buildermesh CLI: nodes report events, message each
// other and watch the mesh through it.
package main

import (
	"bufio"
	"cmp"
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
	"github.com/rickdiculousli/buildermesh/internal/gitwork"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/upgrade"
)

// Exit codes.
const (
	exitOK      = 0
	exitError   = 1
	exitUsage   = 2
	exitRefused = 3
	exitTimeout = 5
	exitConfig  = 78 // EX_CONFIG: serve or hub refused this box's config; their units don't restart on it
)

const usage = `usage: bmesh [--config FILE] [--node PATH] [--json] <command> [args]

--config, --node and --json are accepted before or after the command.
Every command but init, help, version, context, plugin, hosts --here and update --notice needs the box set up (the flag
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
                                           nothing when [hosts] is off or the file is missing; never dials the hub;
                                           then, [hosts] on or off, hub runs <old>, serve <new>: restart bmesh-hub
                                           when this box's hub runs an older binary than its serve
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
  context [--json]                         this session's context and what to do about it: context 312k (claude
                                           session ec03a02c): <advice>; every other command on a session's main
                                           thread ends with one stderr line when its context crosses a [context]
                                           nudge it hasn't heard, or drops (a compaction)
  started|progress|blocked|ask [--text T]  report an event; print <event> <path>
  done --commit SHA [--text T]             report done (a lead's commit must be its landing on main, its trailers
                                           matched by results); print
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
                                           a forget-worktree job (the node stays until that box answers); a round
                                           whose children are all gone is forgotten whatever its state
  node stop <path> [--text "<why>"] [--wait 30s]
                                           stop a node's thread (a round's children, a lead's executors first) and
                                           write it blocked, "stopped by <who>: <why>"; the daemon never wakes it
                                           again but for a bmesh send; another box's through a stop-thread job
  watch --nodes a,b [--since T] [--timeout D]
                                           follow nodes until all are done, blocked or silent
  lead start --repo R --spec NNN --brief FILE [--model M] [--effort E] [--title T] [--path P]
                                           start a lead through the runner and register it
  ideate <repo> [--idea] [--model M] [--effort E] [--context <text|->] [--force]
                                           run a proposal round: one thread ranking engineering and UI/UX ideas in
                                           one list, in a bmesh worktree from main, as node <repo>-round-<N>.eng (and
                                           .ux, the UI/UX part, when ux_host is another box); --context puts the
                                           operator's words in the briefs; --idea starts only the engineer, to
                                           write the one idea --context gives as its spec
  build <repo> <NNN> [<NNN>...] [--model M] [--effort E] [--path P] [--family F] [--hold land|now] [--context <text|->] [--force]
                                           build the specs the operator picked (any not done or rejected): render
                                           each build brief and start its lead (as lead start), recording nothing
                                           on main; several go in id order, a spec naming a lower id after that
                                           one is done, three leads at a time, printing a line each; the leads
                                           join build family F, else a fresh <repo>-build-<N>; --context puts the
                                           operator's words in each brief; --hold writes each lead's hold first
  release <repo> [--minor|--patch] [--dry-run]
                                           tag main's code head as the next vX.Y.Z (v0.1.0 first; minor up when a
                                           feature landed since, else patch) and push the tag; refused while a lead
                                           works, feedback is pending, verify isn't ok or nothing under code changed
  hold <path>|--family F [--gate land|now] --text T
                                           hold a lead (every live lead of the family) at a gate: bm-holds <path>
                                           and a hold message; land refuses bmesh land, now stops it at its next
                                           inbox; executors are never held
  unhold <path>|--family F [--text T]      release the hold and wake the lead with a release message; <path>: not
                                           held when there is none (exit 0)
  held                                     a held lead at its gate: publish held with the hold's gate and why, then
                                           end the turn; the release wakes it (refused when not held)
  feedback "<text>" [--repo R] [--where <file|command>] [--fixed SHA]
                                           a lead's lesson for its build family's coordinator, before done
  feedback list <family>|--repo R [--json]
                                           the family's pending feedback, each with its stream sequence; --repo:
                                           every family's naming R, less what a spec on R's main names (#<seq>)
  feedback drain <family> [--through SEQ]  delete the family's feedback from the stream, up to SEQ
  feedback drain --seq SEQ[,SEQ...]        delete exactly those feedback messages, whatever their family
  fix [--model M] [--effort E]             start the fixer on bmesh's own repo (<repo>-fix-<N>, role fixer, on
                                           the row's host): it rules on the feedback naming that repo and builds
                                           the fixes; refused with no own row or beside a live fixer
  scaffold <repo> [--check]                give a ship repo its AGENTS.md sections, docs/proposals and the routine
                                           kinds its row names; --check prints the diff and exits 1 when any
  executor new --name N [--base REF] [--model M] [--json]
                                           make a worktree, branch and node for an executor under this node;
                                           print {"path","branch","worktree"} and (without --json) its header
  executor list [--json]                   this lead's tree: path, state, branch, head, unread
  executor remove <name> [--force]         remove a merged executor's worktree and branch (exit 3 unmerged)
  serve [--listen ADDR] [--restart [--detach]]
                                           run the daemon and the map (embedded NATS when [mesh] nats is empty);
                                           with [update] repo, it rebuilds itself when what [update] track
                                           follows on that repo's main moves (a new release, or code head);
                                           else every [update] check_every it writes the notice when behind;
                                           --restart runs it as a child, started again 5s after every exit
                                           but 78; --detach (Windows) starts that with no console window and
                                           returns; exit 78 when config check refuses the config
  hub                                      run this box's NATS hub or leaf from [hub] (role hub: JetStream, the
                                           leaf port; role leaf: dials [hub] remote forever) until stopped; its
                                           own process apart from serve, so an update never drops the mesh;
                                           exit 3 naming the key when role, listen or the token file won't do;
                                           exit 78 when config check refuses the config
  config check                             load this box's config.toml and repos.toml with this binary: config ok:
                                           <path>, or config check failed: <the key> (exit 3)
  t3 login [--server URL] [--callback-port 52299] [--timeout D]
                                           sign bmesh in to this box's T3 0.1.0 (default [runner] t3_server):
                                           print the authorize URL, take the redirect on localhost or pasted,
                                           write <config dir>/t3-token.json; lasts 30 days
  t3 login --status                        print the sign-in: <host>: <access>, N days left; server and scopes
  version                                  print this binary's commit: <rev8> <time> [modified], or unknown;
                                           <release> (<rev8> <time>) when built at or past a release tag; a
                                           tagged module install prints its release alone
  update [--repo R] [--latest] [--answer KEY=VALUE]... [--defaults] [--apply [--yes]]
                                           build what [update] track follows on R's main (the newest vX.Y.Z tag,
                                           or the code head; --latest: the code head this once) over this binary
                                           (default [update] repo, else the row this binary's module is from;
                                           with none, go install it from the module), keeping it as bmesh.prev;
                                           the new binary first checks the config and answers the upgrade notes'
                                           asks (refused: the old binary kept), and after the swap runs their
                                           steps (--apply; --yes for the ones that ask first); print <old> -> <new>
                                           (v0.1.0 -> v0.2.0), up to date (resuming a walk left unfinished, or
                                           one pending), or ahead of <release>, up to date (never back); then the
                                           restart this box's daemon needs, and the hub's when it runs older
  update --plan [--json]                   the upgrade walk to the target (asks with their defaults, steps) and
                                           the config check; changes nothing
  update --walk --from REV [--to REV] [--dir D] [--before-swap] [...]
                                           walk the notes from REV to this binary with it (what update runs on
                                           the new binary; --before-swap: config check and asks only)
  update --notice                          print the daemon's behind-main notice (<state dir>/update.txt), and
                                           upgrade notes pending, if any
  plugin                                   print the plugin dir (<state dir>/plugin), the bmesh that wrote it and
                                           whether Claude Code reads it
  plugin --install                         write the plugin this binary carries to its dir, then point Claude
                                           Code at it: remove a buildermesh marketplace that reads another path,
                                           add the dir as the buildermesh marketplace, install
                                           buildermesh@buildermesh at user scope; print each change
  setup [--check] [--force]                run this worktree's [setup] (.bmesh/config.toml) when its installs aren't
                                           current, and mark them current; --check runs nothing and exits 1 when
                                           stale or missing; --force runs it anyway
  land [--spec NNN] [--ttl D] [--wait D]   land this worktree's branch on main under the lease land.<repo> (waits
                                           while another lead holds it): print the kinds it requires (a branch
                                           rebased by hand requires what the head before the rebase had), fetch,
                                           rebase, run or require the results, reword HEAD with the Spec and
                                           Verified trailers, push; the spec must read done; a push main refuses
                                           starts again from the fetch; prints landed <sha8>
  ready --text "<done line>" [--size S]     a lead hands its landing to its box's daemon: state landing, bm-ready
                                           <path>; the daemon runs land and done and wakes the lead only when land
                                           refuses, and once at the end; refused off this box, without a thread,
                                           dirty, with operator mail, a second time, or when the daemon is older
  ready --cancel                           withdraw the request: the daemon cancels a running step; a step that
                                           had pushed still gets done and the final wake
  result find <kind> [--commit C] [--model M] [--json] [-- args...]
                                           print the result of kind at the commit's tree, without --model every
                                           model's (exit 3 when none)
  result record <kind> --ok|--fail [--commit C] [--model M [--base B]] [--output FILE|-] [-- args...]
                                           record a result of kind at the commit's tree
  lock-lease acquire <slot> [--ttl D]      take a place in a slot and print its key (exit 3 when busy)
  lock-lease renew <key> [--ttl D]         extend a held lease
  lock-lease release <key>                 give back a held lease
  lock-lease run <slot> [--ttl D] -- <cmd...>
                                           hold a lease while a command runs; exit with its code
  routine list [--json]                    the repo's kinds with their instances and queue depth
  routine status [<kind>] [--json]         a kind's (or every kind's) instances with state and model
  routine queue <kind> [--commit C] [--model M] [--effort E] [--go] [--rerun] [--again] [--text T] [-- args...]
                                           queue a job (a deterministic kind answers from a recorded result; a
                                           review for a branch that has one is refused without --again)
  routine go <kind>                        tell the kind's instances to start on waiting jobs
  routine wait <job id> [--timeout D]      wait for a job's reply; print its output, exit 0 ok / 1 fail / 5 timeout
  routine run <kind> [--commit HEAD] [--model M] [--effort E] [--here] [--rerun] [--text T] [-- args...]
                                           run a kind on this worktree's HEAD (its tree clean) and record its
                                           result, a deterministic kind answering from a recorded one; with no
                                           hub it runs and records nothing; ends with <kind> ok|fail t:<tree8>;
                                           exit 0 ok / 1 fail
  spec list [--repo R] [--status S] [--json]
                                           the repo's specs, one row each (id, status, size, kind, lens, title)
  spec next-id [--repo R] [--count N] [--peek]
                                           claim the repo's next spec id (a block of N) on the mesh
  spec set --id NNN[,NNN...] --status S [--repo R] [--from A,B] [--field K=V]... [--land]
                                           set specs' status and fields (--from refuses another status); --land
                                           lands the edit on main in one commit, from a bmesh worktree
  spec template [--repo R]                 print the half-pager (the repo's .bmesh/briefs/half-pager.md, else embedded)
  spec steps [<NNN>] [--repo R] [--tick N,N [--note W] | --check [--wait D]]
                                           a landed spec's steps for the operator, or with no id every open one;
                                           --tick records steps done on the hub, --check runs their checks
  spec verified [--commit C]              print the verified line from the results for HEAD's tree, or C's (exit 3
                                           when none); land puts it on the landed commit
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
	pending        bool   // a command run at a terminal names upgrade notes still to walk, last
	measure        bool   // the command ends with this session's context line when it is owed one (094)
	self           string // the node identity resolved from this checkout, "" before or without one
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
	if a.pending && interactive(stdin) {
		if line := upgrade.Pending(config.StateDir(), buildVersion().Release); line != "" {
			fmt.Fprintln(stderr, "bmesh: "+line)
		}
	}
	if a.measure {
		if line := a.contextLine(); line != "" {
			fmt.Fprintln(stderr, line)
		}
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
	if errors.As(err, new(*gitwork.Refused)) {
		fmt.Fprintf(a.stderr, "bmesh: %s\n", err)
		return exitRefused
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
	// update walks them, and init, help and version stay quiet.
	a.pending = !slices.Contains([]string{"init", "help", "version", "update", "serve"}, cmd)
	// hosts --here runs from a session hook: it reads one file and never
	// sets the box up or refuses, so a session on a box without bmesh set
	// up opens as quietly as one with [hosts] off.
	// So does update --notice, beside it in the same hook.
	here := cmd == "hosts" && slices.Contains(rest, "--here") || cmd == "update" && slices.Contains(rest, "--notice")
	// Every other command tells a session it can measure its context once
	// it crosses a nudge; context prints it itself, and the daemons and
	// the session hook's commands run outside any turn.
	a.measure = !here && !slices.Contains([]string{"context", "serve", "hub"}, cmd)
	if cmd != "init" && cmd != "help" && cmd != "version" && cmd != "context" && cmd != "plugin" && !here {
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
	case "context":
		return a.cmdContext(rest)
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
	case "build":
		return a.cmdBuild(rest)
	case "release":
		return a.cmdRelease(rest)
	case "fix":
		return a.cmdFix(rest)
	case "hold":
		return a.cmdHold(rest)
	case "unhold":
		return a.cmdUnhold(rest)
	case "held":
		return a.cmdHeld(rest)
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
	case "ready":
		return a.cmdReady(rest)
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
	case "plugin":
		return a.cmdPlugin(rest)
	case "config":
		return a.cmdConfig(rest)
	case "hosts":
		return a.cmdHosts(rest)
	case "sync":
		return a.cmdSync(rest)
	case "prune":
		return a.cmdPrune(rest)
	case "setup":
		return a.cmdSetup(rest)
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

// noHubError is connect's error when no server answers at the hub's
// address (nats.ErrNoServers): bmesh serve isn't running the embedded hub,
// or nothing serves [mesh] nats. It reports as err; routine run runs on
// without a hub.
type noHubError struct {
	addr string // the hub's address, without nats://
	err  error
}

func (e *noHubError) Error() string { return e.err.Error() }
func (e *noHubError) Unwrap() error { return e.err }

// connect loads the config and dials the hub ([mesh] nats, else the hub
// bmesh serve embeds on [mesh] listen); name labels the connection. Once
// connected it reads the revision hint, which run prints after the
// command's output, when the daemon runs another build. A hub no server
// answers for is a *noHubError.
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
	if errors.Is(err, nats.ErrNoServers) {
		addr := strings.TrimPrefix(url, "nats://")
		if embedded {
			err = fail(exitError, "no hub at %s: is bmesh serve running?", addr)
		}
		return &noHubError{addr: addr, err: err}
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

// givenNode is the node path --node, else BMESH_NODE, names; "" for none.
func (a *app) givenNode() string { return cmp.Or(a.node, os.Getenv("BMESH_NODE")) }

// identity resolves this node's path: --node, else BMESH_NODE, else the
// registered node whose worktree is this checkout on this host. It connects.
func (a *app) identity(ctx context.Context, cmd string) (mesh.Path, error) {
	if given := a.givenNode(); given != "" {
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
	a.self = n.Path
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
