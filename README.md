<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/banner-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/banner-light.svg">
    <img alt="buildermesh: a mesh for coding agents that propose, build, verify and land changes on main, across every box you own" src="docs/assets/banner-light.svg" width="100%">
  </picture>
</p>

<p align="center">
  <a href="https://github.com/rickdiculousli/buildermesh/tags"><img alt="Latest release" src="https://img.shields.io/github/v/tag/rickdiculousli/buildermesh?sort=semver&label=release&style=flat-square&color=39c5bb&labelColor=161b22"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/rickdiculousli/buildermesh?style=flat-square&logo=go&logoColor=white&color=58a6ff&labelColor=161b22"></a>
  <a href="#several-boxes"><img alt="NATS JetStream, built in" src="https://img.shields.io/badge/NATS-JetStream%2C_built_in-a371f7?style=flat-square&logo=natsdotio&logoColor=white&labelColor=161b22"></a>
  <a href="#quickstart"><img alt="Linux, macOS and Windows" src="https://img.shields.io/badge/runs_on-Linux_%C2%B7_macOS_%C2%B7_Windows-8b949e?style=flat-square&labelColor=161b22"></a>
  <a href="LICENSE"><img alt="MIT license" src="https://img.shields.io/github/license/rickdiculousli/buildermesh?style=flat-square&color=3fb950&labelColor=161b22"></a>
</p>

<p align="center">
  <a href="#one-mesh-many-boxes"><b>Many boxes</b></a> &nbsp;·&nbsp;
  <a href="#proof-before-main"><b>Proof before main</b></a> &nbsp;·&nbsp;
  <a href="#how-a-change-reaches-main"><b>How it works</b></a> &nbsp;·&nbsp;
  <a href="#get-started"><b>Get started</b></a> &nbsp;·&nbsp;
  <a href="#the-map"><b>The map</b></a> &nbsp;·&nbsp;
  <a href="#reference"><b>Reference</b></a>
</p>

<br>

**buildermesh** (`bmesh`) is one Go binary that lets Claude Code sessions on your own machines (boxes, in bmesh's words) turn ideas into changes on `main`, while you decide what gets built.

Agents write proposals as half-page specs, and you accept or reject them in plain words. When you build an accepted spec, bmesh starts a *lead*: a session that builds it in a git worktree of its own and hands pieces to *executors*, helper sessions on branches of their own. *Routines* run the repo's checks (its tests, and an Opus review for larger changes) and record each result against the exact files of a commit. A lead lands on `main` only through `bmesh land`, which refuses unless those checks passed for what it lands.

## One mesh, many boxes

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/mesh-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/mesh-light.svg">
    <img alt="Three boxes around one NATS hub: events, jobs and replies move between them; a laptop sleeps and catches up" src="docs/assets/mesh-light.svg" width="100%">
  </picture>
</p>

- **One hub.** `bmesh hub` runs NATS, a message server, inside the binary on a box that is always on, listening on its address on a tailnet (a private network such as Tailscale). Boxes share one token, and there is nothing else to install. A single box needs no separate hub: `bmesh serve` runs one.
- **A daemon on every box.** Each box runs `bmesh serve`, whose daemon wakes leads that have gone quiet, runs routines, sends phone notifications and draws the map.
- **Shared checks.** Checks that run apart from the lead, like the review, have one queue that every box running them takes from, so they run on whichever box is free. A check that needs macOS runs only on a Mac.
- **Laptops can sleep.** A sleeping laptop reconnects when it wakes and takes the work it missed, in order. Work sent to it waits on the hub for a day.
- **Signed jobs.** Boxes that join with `bmesh init --hosts` share a roster, the list of member boxes, signed by the box you work at. Every job between boxes is signed and checked. The keys that sign the roster and approve work sit where no agent can use them (on macOS, behind the Keychain password dialog). On a box marked secure, a checkout update that would move its plugins, or the repo bmesh updates itself from, waits until you approve it there.
- **Updates.** A box set to update itself takes each bmesh release on its own; on other boxes, sessions are told to run `bmesh update`.

## Proof before main

<p align="center">
  <img alt="A lead runs bmesh land: rebase, verify, carry the review, write the verified line, push main" src="docs/assets/land.svg" width="100%">
</p>

`bmesh land` names the checks it requires, runs `verify` on the rebased tree and carries the Opus review across the rebase when the diff is unchanged. A changed diff needs a new review. The spec keeps the results as one line:

```
verified: review ok t:3f2a9c1e m:claude-opus-5-5 c:81d0b7a4; verify ok t:3f2a9c1e
```

`t:` is the landed tree, and `c:` the tree the review ran on before the rebase. `bmesh done` refuses until the spec on `origin/main` reads `done` with a merged commit and every entry matches a recorded result.

## How a change reaches main

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/lifecycle-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/lifecycle-light.svg">
    <img alt="One spec's way to main: propose, choose, build, verify, land" src="docs/assets/lifecycle-light.svg" width="100%">
  </picture>
</p>

1. **Propose.** `bmesh ideate app` starts two sessions, an engineer and a UI/UX engineer. Each lands three to five ranked half-page specs on `main`. With an [ntfy](https://ntfy.sh) topic set, the list reaches your phone.
2. **Choose.** Reply in plain words: `bmesh choose-ideas app "1 and 3 yes, 2 smaller, 4 no, 5 later"`. The verdicts land on `main`. Words that aren't a verdict, like `smaller`, mark the spec `adjusting`, with your words kept for the rework.
3. **Build.** `bmesh build app 012` confirms `main` passes its verify commands, marks the spec `building` and starts a lead. The lead hands pieces to executors on branches under its own and merges what they return.
4. **Verify.** Routines run `verify` (the repo's tests) and, for larger changes, `review` (Opus, in a session that never saw the change). Each result is stored under the commit's tree hash, the fingerprint of its files, so a check marked deterministic answers a repeat from its record.
5. **Land.** `bmesh land` rebases onto `origin/main`, requires a result for the tree it lands, writes the spec's `verified` line, marks it `done` and pushes `main` as a fast-forward.

Leads report problems outside their spec as feedback. When a batch started from a chat is done, the build skill of bmesh's Claude Code plugin dispatches the quick fixes as new builds without asking you and writes the rest up as proposed specs for you to review. It then runs `bmesh release`, which tags the repo's next version (once the repo has a first `v` tag). From a chat, you can say "proposals for app", "1 and 3 yes", "build what's accepted in app" or "what's building".

## Get started

You need Go 1.26 or later, git, Claude Code (`claude` on your `PATH`, signed in) and a repo with a `main` branch. The `claude` runner, which `init` picks when `claude` is on your `PATH`, runs Claude Code with its permission prompts off, in worktrees bmesh makes.

```sh
go install github.com/rickdiculousli/buildermesh/cmd/bmesh@latest
claude plugin marketplace add rickdiculousli/buildermesh
claude plugin install buildermesh@buildermesh
cd ~/repos/your-repo && bmesh init   # one box or several; registers this repo
bmesh serve                          # hub, daemon and map; keep it running
```

Then tell bmesh what must pass before a change is done, in this repo's row of `repos.toml` (beside `config.toml` in bmesh's config directory; `bmesh init` prints the path):

```toml
[repos.your-repo]
path = "~/repos/your-repo"
verify = ["go vet ./...", "go test ./..."]
```

and run:

```sh
bmesh scaffold your-repo             # AGENTS.md sections and checks; commit, push
bmesh ideate your-repo               # your first proposal round
```

Running `serve` as a service (`bmesh init --service`), Windows, several boxes and everything `init` asks and prints are in the [Quickstart](#quickstart) below.

## The map

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/map-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/map-light.png">
    <img alt="The bmesh map: leads with their executors, a proposal round, a held lead, the routines column and the needs-you band" src="docs/assets/map-light.png" width="100%">
  </picture>
</p>

`bmesh serve` draws the mesh at `http://127.0.0.1:3774/`, live: each lead with its executors, each proposal round with its engineers, and each kind of check with its queue. What needs you comes first, in the band at the top: a session that is blocked, asking or silent.

## What's inside

| Piece | What it is |
|---|---|
| `bmesh` | One Go binary: the CLI, the daemon, the map's server and the NATS hub. |
| `bmesh serve` | Runs on every box: the daemon, the map and, on a single box, the hub. |
| `bmesh hub` | The hub for a mesh of several boxes, or a local relay to it for a laptop that sleeps. |
| Runners | How a lead's session runs: `t3` (a T3 Code thread), `claude` (headless `claude -p` turns) or `tmux`. |
| The plugin | Twelve Claude Code skills: seven are your chat entry points, and five are what leads, executors, a round's engineers and bmesh's own fix lead follow. |
| Specs | `docs/proposals/NNN-slug.md` in each repo. A spec's frontmatter is its whole record: its status, who built it, the merged commit and the `verified` line. |

<br>

## Reference

Everything below is exact: every command, config key, bucket and event bmesh has. bmesh's own messages point into it by section name.

<table>
  <tr>
    <td valign="top" width="33%"><b>Setting up</b><br><a href="#quickstart">Quickstart</a><br><a href="#upgrading">Upgrading</a><br><a href="#several-boxes">Several boxes</a><br><a href="#runners">Runners</a><br><a href="#hosts">Hosts</a><br><a href="#config">Config</a></td>
    <td valign="top" width="33%"><b>Building</b><br><a href="#proposal-rounds">Proposal rounds</a><br><a href="#builds">Builds</a><br><a href="#feedback">Feedback</a><br><a href="#releases">Releases</a><br><a href="#the-ledger">The ledger</a><br><a href="#executors">Executors</a><br><a href="#finished-nodes">Finished nodes</a></td>
    <td valign="top" width="33%"><b>Checking and landing</b><br><a href="#routines">Routines</a><br><a href="#landing">Landing</a><br><br><b>Lookup</b><br><a href="#names">Names</a><br><a href="#exit-codes">Exit codes</a><br><br><b>Working on bmesh</b><br><a href="#build-and-test">Build and test</a><br><a href="#license">License</a></td>
  </tr>
</table>

## Quickstart

A machine with Go and Claude Code (`claude` on PATH) gets a working single-box mesh in four commands:

```
go install github.com/rickdiculousli/buildermesh/cmd/bmesh@latest   # or, from a checkout, GOBIN=~/.local/bin go install ./cmd/bmesh
cd ~/repos/your-repo && bmesh init     # asks single or multi-box, writes config.toml, registers this repo, picks the claude runner, says what it set
bmesh serve                            # the hub (embedded on a single box), the daemon and the map at http://127.0.0.1:3774/; keep it running
bmesh repos                            # the registry; then bmesh scaffold your-repo, bmesh ideate your-repo, bmesh choose-ideas, bmesh build your-repo 001, bmesh mesh-status
```

Recommended, optional: the [pstack](https://github.com/rickdiculousli/pstack-claude) and `method` Claude Code plugins. At fixed moments the lead, executor and round skills say "if `pstack:<skill>` is in this session's skill list, run it; otherwise &lt;the written default&gt;":

- poteto-mode: the working style;
- `architect`, `attack-the-premise`: the lead deciding the shape;
- `tdd`: the executor's default;
- `figure-it-out`: before any `bmesh ask`;
- `blast-radius`: before landing;
- `control-ui`: a lead's browser check;
- `exhaust-the-design-space`: a round's engineers.

They name `method:writing-plans` for an L spec the same way. The condition is the session's skill list, not the plugin's install: a plugin installed but not invocable in the session counts as absent. Without them a node works from the defaults; nothing in the binary looks for either.

#### `bmesh init`

It asks for what its flags don't say (`--role`, `--hub`, `--token-file`, `--runner`, `--no-repo`), reading answers from stdin (an empty line, or no stdin, takes the default), so a script can drive it. On a box of a multi-box mesh, pick that answer and say whether it runs the hub, a leaf of it, or is its client (see Several boxes).

**`--service`** also writes the unit that keeps `bmesh serve` running (a systemd user unit on Linux, a launchd agent on macOS), and with `[hub] role` set the one that keeps `bmesh hub` running, and says how to enable each. On Windows it writes nothing and prints the two commands that set it up, to run in `cmd` (see Windows below).

Neither unit restarts on exit 78, a config the binary refused (see Config). The systemd units carry `RestartPreventExitStatus=78`. launchd has no per-code stop, so serve's launchd agent carries `ThrottleInterval` 300: a bad config restarts it at most every 5 minutes, never in a tight loop.

A unit already there:

- with the same bytes is left alone (`unchanged <file>`);
- that differs (tuned by hand, or written by an older bmesh) is kept as it is, the new body written beside it as `<file>.new`, and no enable command printed for it: `kept <file> (it differs from what init writes); the new one is <file>.new: diff them, and move it over to take it`. `--force` replaces it.

**The setup flag** (`<config dir>/.setup-complete`) is written last. Every other command but `help`, `version`, `hosts --here` and `update --notice`, finding neither the flag nor a `config.toml`, prints one line and runs `init` first when run from a terminal, and is refused (exit 3, ``run `bmesh init` ``) when not.

**On an existing `config.toml`** it edits nothing without `--force` (a registry row it already has is left too), but still writes the flag and, with `--service`, a unit that is missing.

**The repo it registers** must have a `main` branch, since bmesh builds on `main`: on `origin` as last fetched when `origin` has been fetched, else locally (an unborn `main` in a fresh `git init -b main` counts); no network is used. Refused before anything is written (exit 3):

- a repo without one: `bmesh: <path> has no main branch; bmesh builds on main (rename the branch, or see https://github.com/rickdiculousli/buildermesh/issues/1; --no-repo sets up without it)`;
- a name whose node token another row already has (see Names).

**What it prints.** `config.toml` comments each key it writes with what it means. Last, before `Next:`, it prints every setting in effect, read back from the loaded config so the defaults it didn't write show too, each with its value, meaning and the key that changes it; on an existing config, the same block for what is there:

```
What this box is set to:
  setup: single box. This box runs everything; `bmesh init --force --hub <url>` joins a hub other boxes share instead ([mesh] nats)
  hub: embedded on 127.0.0.1:4222. bmesh serve runs the message hub itself; nothing to install ([mesh] nats, listen)
  runner: claude. Each lead and proposal round is a claude session started for you ([runner] kind)
  repo: your-repo at /home/you/repos/your-repo. Registered: bmesh ideate your-repo proposes changes to it, bmesh build your-repo <NNN> builds one (repos.toml)
  worktrees: /home/you/.bmesh/worktrees. Where bmesh makes the separate checkouts its nodes work in, so yours is never touched ([runner] worktrees)
  map: http://127.0.0.1:3774/. A page showing every running node; open it while bmesh serve runs ([serve] listen)
  proposers: claude-opus-5-5 at xhigh effort. The two sessions of a proposal round, which read the repo and write specs ([models] proposer, proposer_effort)
  leads: claude-opus-5-5 at high effort, unless the spec's judge names another. The session that builds one spec and lands it on main ([models] lead, lead_effort)
  executors: chosen by each lead. Helpers a lead starts for pieces of its spec (bmesh executor new --model)
  updates: notice every 6h, release. The daemon checks for bmesh's newest release and each session says when this binary is behind; bmesh update takes it ([update] check_every, upstream, repo, track)
  service: none. Run bmesh serve yourself and keep it running, or have the OS do it (bmesh init --service)
  routines: on. This box runs any pushed commit's routine commands for every box on the mesh ([runner] checkouts)
```

- One line per registered repo (`repo: none` with none).
- A multi-box config shows `setup: several boxes` and the hub's URL (a password in it as `xxxxx`) with its token file.
- `service` names the unit or plist when the file `--service` writes exists.

#### The hub on a single box

`[mesh] nats` stays `""`. `serve` embeds the hub on `[mesh] listen` (default `127.0.0.1:4222`), and every other command dials it there, `nats://<listen>` (an unspecified host, `0.0.0.0` or `::`, as `127.0.0.1`), in JetStream domain `hub` with the same token handling.

- With nothing listening a command fails with `bmesh: no hub at 127.0.0.1:4222: is bmesh serve running?`.
- A `listen` on port 0, where `serve` picks a free port, is refused, since the CLI can't know which.

#### Per OS

- Config dir (`os.UserConfigDir`): `~/.config/bmesh` on Linux, `~/Library/Application Support/bmesh` on macOS, `%AppData%\bmesh` on Windows; an existing `~/.config/bmesh` is kept on any OS. The embedded hub's store: the OS cache dir (`os.UserCacheDir`). See Config.
- A configured command line runs through `sh` on Unix; `cmd /C` on Windows, where every argument is quoted and one holding a quote or a `%` is refused. Nothing assumes tmux. Linux and macOS are run; Windows is built and vetted on every change (`GOOS=windows go vet ./...` is part of verify), and run by users.

**Keeping serve running.** `bmesh serve --restart` is a supervisor on any OS (a Linux box without systemd can use it too). It runs `bmesh serve` as a child, from the binary path it read at start, with its own arguments less `--restart` and `--detach` (a `--config` given before `serve` passed on) and `BMESH_SUPERVISED=1` in its environment. It starts the child again whenever it exits, whatever the exit code but 78:

- Exit 78 (the child refused the config, see Config): not started again; the supervisor logs `serve exited 78 (config check failed); not restarting: fix the config and start it again` and exits 78 itself.
- Otherwise: 5 seconds after each exit (as `RestartSec=5`), 60 seconds once five runs in a row lasted under a minute each, until a run outlasts that.

Ctrl-C or SIGTERM stops the child, then the supervisor: on Unix by SIGTERM, killed after 30 s; on Windows killed, and the jobs it held go back to the inbox when their ack wait runs out.

It logs its own lines, `<time> bmesh serve --restart: started <exe> (pid N)`, `serve exited (exit N)`, `starting again in 5s`, beside the child's output: on stderr on Unix. On Windows, which has no journal, they go to `<state dir>/serve.log` (the state dir is `$XDG_STATE_HOME/bmesh`, else `~/.local/state/bmesh`), moved to `serve.prev.log` (replacing the one there) before it passes 10 MB, and the child gets a console of its own with no window.

`--detach` (Windows only, with `--restart`; `--detach` alone is a usage error, exit 2) starts the supervisor again with `CREATE_NO_WINDOW` (out of the starting process's job where the job allows it), prints `bmesh serve: supervisor started with no window (pid N); log in <path>` and returns at once.

**Windows.** `bmesh init --service` ends with three lines for `cmd` (not PowerShell, which reads the quotes differently):

```
schtasks /Create /TN buildermesh /SC ONLOGON /RL LIMITED /F /TR "\"C:\Users\you\go\bin\bmesh.exe\" serve --restart --detach --config \"C:\Users\you\AppData\Roaming\bmesh\config.toml\""
schtasks /Run /TN buildermesh
rem in cmd: bmesh serve then starts at every logon with no window; its log is C:\Users\you\.local\state\bmesh\serve.log
```

The first makes a task that runs the supervisor at every logon with the user's rights; the second starts it now. The config path goes in `--config` because a task sets no environment.

Each path is quoted for `schtasks`, so one with spaces passes. `cmd` has no escape for the `\"` that quoting takes, so to `cmd` each path stands outside quotes. Refused:

- a path holding a character it reads there (`"`, `%`, `&`, `^`, `|`, `<` or `>`): `init --service: "<path>" cannot be passed through cmd (it holds one of " % & ^ | < >); move it and run init --service again`;
- a task command longer than the 261 characters `/TR` takes.

The task's own process returns at once (`--detach`), so it leaves no console window open and the task's run time limit never applies. To stop it, `taskkill /IM bmesh.exe /F` (which ends every bmesh on the box); `schtasks /Delete /TN buildermesh /F` removes the task.

### Updates

**What a box follows.** `[update] track` (default `release`) names the commit every update path goes to: the daemon's self-update, the notice and `bmesh update` all read the same target.

**The code head** is the newest commit on `main` that changes the repo's code (its registry row's `code`, see repos.toml): the commit `build` verifies and `release` tags.

- `release` follows the newest release, the highest `vX.Y.Z` tag in `main`'s history (see Releases; a pre-release such as `v0.2.0-rc1` is passed over). A repo with no release tag yet is followed at its code head, which the daemon logs once: `update: no release tag on main yet; following its code head`.
- `latest` follows `main`'s code head. Even `latest` never rebuilds for a commit outside the code (a dispatch, review or `done` record, and with `code` set a README, a document or art).

A binary that is the target is current; one with the target in its history (built from `main`, or by `update --latest`, past the newest release) is current too, and is never moved back to an older release. One the target doesn't contain (a rollback, or another line) is left alone.

A binary installed as `go install <module>/cmd/bmesh@vX.Y.Z` names a version and no commit: every path resolves that tag to its commit in the repo it reads (fetched with its `v*` tags) before comparing. A tag that repo doesn't have is logged once by the daemon, `update: this binary's release is not on main; left alone`, while `bmesh update` installs the target as for a binary of unknown revision.

Updates name both ends by release: `v0.1.0` for a tagged commit, `v0.1.0+2 (<rev8>)` two commits past it, `<rev8>` with no release in its history.

**`bmesh version`** prints the commit the binary was built from, `bmesh <rev8> <vcs.time> [modified]`, from the first it has:

1. its build info;
2. the revision and commit time a self-update stamped into it;
3. the main module's pseudo-version, as `go install …/cmd/bmesh@latest` builds (`v0.0.0-20261016043719-ab7e0b543677` prints `bmesh ab7e0b54 2026-10-16T04:37:19Z`: the 12-hex suffix is the revision and the timestamp the time).

With none of them, `bmesh unknown` (`(devel)` names no commit).

- A tagged module install (`go install …/cmd/bmesh@v0.2.0`, or `@latest` once the module has a release, which go resolves to its tag) names a version and no commit, and prints the release alone, `bmesh v0.2.0`; an update resolves it to its commit (above).
- A build a self-update stamped at or past a release tag (see Releases) prints the release first: `bmesh v0.1.0 (9a5e181a 2026-10-16T22:38:04Z)` on the tagged commit, `bmesh v0.1.0+3 (<rev8> <time>)` three commits past it. The stamp is `git describe --tags --long --match 'v[0-9]*.[0-9]*.[0-9]*' --exclude '*-*' --exclude '*+*'` of the commit at build time, the nearest `vX.Y.Z` tag in its history (pre-releases passed over), and goes with the stamped commit only (a vcs revision that is another commit prints none).

Revisions compare by prefix wherever the hint and the updates below compare them, so a module install's 12 hex match the full commit. `serve` publishes `bm.daemon.<host>.started` on every start, `text` the full revision (`unknown`) and `body` its commit time (RFC 3339; empty when unknown); its digest title carries the revision (`bmesh <host> <rev8>: …`).

**The daemon's self-update.** With `[update] repo = "<registry name>"` (default `""`: off) the daemon keeps itself current. On each kind rescan (once a minute; it needs `[runner] checkouts`) it compares what `[update] track` follows on that repo's main checkout, `<[runner] checkouts>/<repo directory name>/main` at `origin/main`, with the binary at its path (its `version`, so a newer binary a `bmesh update --latest` already swapped in is never built over; its own revision, or release tag resolved there, when that prints none). When the target contains that revision and is ahead, it:

1. Builds `./cmd/bmesh` at the target, the checkout moved there for the build and back after: `go build -ldflags "-X <module>/internal/selfupdate.stampRevision=<commit> -X <module>/internal/selfupdate.stampTime=<commit time, RFC 3339 UTC> [-X <module>/internal/selfupdate.stampRelease=<vX.Y.Z[+N]>]" -o <temporary file beside the binary>`, since go writes no VCS stamp in a git worktree; the release only when a tag is in the commit's history. In a systemd unit it builds in a transient `systemd-run --user --scope` of its own, so the build doesn't share the unit's `MemoryMax`; inline otherwise.
2. Runs the build's `version` and refuses it unless it names the target's commit (`update: built binary reports <x>, not <new8>; keeping this binary`, logged once per target, the running binary kept).
3. Checks the new binary's config and upgrade walk, and holds the update when either needs the operator (see Upgrading).
4. Moves the running binary to `bmesh.prev` beside it and the build to its path.
5. Publishes `bm.daemon.<host>.update` with `text` `<old> -> <new>` (`v0.1.0 -> v0.2.0`; `commit` the new revision) and logs `update: v0.1.0 -> v0.2.0`.
6. Drains: every kind's feed stops taking jobs, every running job's process group is killed, and every job it held or ran goes back to the inbox (a NAK) for the restarted daemon to take at once (see Jobs).
7. Stops serve as on SIGTERM (map, daemon, client, embedded hub) and re-execs the binary with the same arguments: `exec` on Unix, so the service manager sees the same process; on Windows, which has no exec, it exits 0 and `serve --restart` starts the new binary 5 seconds later.

Clients reconnect to an embedded hub as after any restart. A box on `release` restarts once per release, and one on `latest` once per code landing.

- A build that fails is logged once per target, and the daemon keeps running the binary it has.
- A target that does not contain the running revision (a rollback, or another line) is logged once and left alone.
- A binary with neither a revision nor a release tag never updates.
- A daemon whose newest `update` has no `started` after it and named another revision than its own (the new build crashed at start and `bmesh.prev` was put back by hand) logs it and pushes `bmesh update failed on <host>` (priority `high`) through `[notify]` before publishing `started`.

On Windows a running binary can be renamed but not removed, so the swap works as on Unix; a `bmesh.prev` that can't be removed (the supervisor's own image, which the update before moved there) is moved to `bmesh.prev.old` first. A Windows daemon not under `serve --restart` (no `BMESH_SUPERVISED=1`) refuses `[update] repo`, since its exit 0 would stop it for good, logging once at start `update: [update] repo needs bmesh serve --restart on Windows, which starts the new binary (init --service sets it up); off`; the notice still runs from the row.

**`bmesh update`.** `bmesh update [--repo R] [--latest] [--answer KEY=VALUE]... [--defaults] [--apply [--yes]] | --plan [--json]` does the same build and swap on demand, on any box and without a daemon. `R` defaults to `[update] repo`, else the registry row whose `go.mod` declares this binary's module.

It fetches the repo's main into a bmesh-owned checkout, never the operator's: the daemon's `<[runner] checkouts>/<repo directory name>/main` when checkouts are configured, else a clone (of the row's `origin`, of its path without one) under `<user cache dir>/bmesh/update/<R>`. The daemon and `update` each hold the advisory lock `<checkout>.lock` while they move that checkout and build from it, so one never builds from a tree the other is moving. On Windows the lock does nothing, so a `bmesh update` run by hand while the daemon updates can race it.

It goes to what `[update] track` follows; `--latest` to the code head this once, whatever `track` says, to get a fix before the batch's release; the next `update` follows `track` again without going back: a binary past the newest release prints `ahead of v0.2.0, up to date`.

It prints `<old> -> <new>` (`v0.1.0 -> v0.2.0`; under `--latest`, `v0.2.0 -> v0.2.0+2 (<rev8>)` when two commits have landed since the release), or `up to date`. Refused (exit 3): a target that does not contain this binary's revision, and a build whose `version` does not name the target's commit (`bmesh: update: built binary reports <x>, not <new8>; keeping this binary`, the binary kept).

**No repo row** (no `--repo`, `[update] repo` or own-module row: an install from the Quickstart): it reads the module's `main` instead.

1. Fetches `https://<module path>`'s `refs/heads/main` and its `v*` tags into the bare repo `<user cache dir>/bmesh/update/upstream` (under the same lock).
2. Refuses a target that does not contain this binary's revision, as above.
3. Runs `GOBIN=<temporary dir beside the binary> go install -ldflags "<the same -X stamp of the target's commit and its time>" <module>/cmd/bmesh@<the release tag, or the code head's commit>` (scoped as the daemon's build is; stamped because a tagged build names no revision).
4. Refuses or swaps in the result as above, keeping `bmesh.prev`.

After an upstream swap it prints `<old> -> <new>` and `update the buildermesh plugin too: claude plugin marketplace update buildermesh && claude plugin update buildermesh@buildermesh (then restart Claude Code)`, since the skills name commands only a matching binary has. A module that can't be read (a binary that doesn't say its module) is refused with `no repo to update from: pass --repo or set [update] repo` (exit 3).

Between the build and the swap, and after the swap, the new binary walks the upgrade notes of the releases in between (see Upgrading): a config it refuses, or an ask with no answer, keeps the old binary. Once the binary is current (`up to date`, `ahead of …`, or after a swap from either source) it removes the notice file below, so the next session opens without it.

**Restart line.** Then, when it reaches the hub and this host's own daemon's newest `bm.daemon.<host>.started` names another revision than the binary now at the path, it prints `this box's daemon still runs <old8>; restart it so it runs <new8>: <command>`, `<command>` restarting the unit `init --service` writes:

- Linux: `systemctl --user restart bmesh`;
- macOS: `launchctl kickstart -k gui/$(id -u)/com.buildermesh.bmesh`;
- Windows, whose supervisor runs detached from the logon task: `taskkill /F /IM bmesh.exe & schtasks /Run /TN buildermesh` (for `cmd`; it stops every `bmesh.exe`, the supervisor and its `serve` included, then starts the task again);
- another OS: `stop bmesh serve and start it again`.

No hub, no `started` for this host, or a daemon of unknown revision: no line. A daemon run by hand rather than under the unit gets a command that doesn't apply; stop it and start it again.

`update` never restarts the daemon itself: a restart kills the daemon's running jobs, and only its own drain (the self-update above) hands them back. A daemon with `[update] repo` takes the new target by itself within a couple of minutes; one without (a Mac's) runs the old code until it is restarted.

**The hint.** Every command that connects to the hub reads this host's own daemon's newest `bm.daemon.<host>.started` once (the hub's newest `bm.daemon.*.started` only when this host has none: it runs no daemon, so a Mac daemon that doesn't self-update never tells another box's leads to update). When that daemon runs another revision than this binary, it prints one line on stderr, once, after the command's own output (and after its error, if any), never changing its exit code:

- `bmesh: k16 runs 1a2b3c4d, this is 9e8f7a6b; run bmesh update`;
- or, when it is this host's own daemon and its commit is older than this binary's (by the commit time `started` carries against the one the binary is stamped with), so the daemon is what lags: `bmesh: this box's daemon runs 1a2b3c4d, this binary is 9e8f7a6b; restart the daemon: <command>`, `<command>` as `update` names it above. Another host's daemon, a newer one, or a commit time either side doesn't know (a `started` from a daemon older than this rule carries none) keeps `run bmesh update`.

No hint for a binary or daemon of unknown revision, for commands that reach no hub (`help`, `version`, `init`, `repos`, `spec list|next-id|set|template`, `scaffold`), or for `update`, which prints its own restart line instead.

**The notice.** A daemon that isn't self-updating from `[update] repo` checks what `[update] track` follows on `main` every `[update] check_every` (default `6h`; `0` turns the check off; it runs in the routine loop, so it needs `[runner] checkouts`):

1. Reads the target from `[update] repo`'s main checkout when that row is set and has a checkout here, else from a fetch of the module's `main` and its tags into `<user cache dir>/bmesh/update/upstream`, as `bmesh update` does.
2. Compares the target with the binary at its own path as the self-update does (so a `bmesh update` that already swapped in a current binary reads as current).
3. When the target contains that revision and is ahead, writes one line to `<state dir>/update.txt` (through a temporary file and a rename).

For a release the line is `bmesh: v0.2.0 is out (you run v0.1.0: 046, 047); run bmesh update, then update the buildermesh plugin`. For a code head (`track = "latest"`, or no release yet): `bmesh: 9 commits behind main (landed since: 032 The Quickstart works as written, 033 …); run bmesh update, then update the buildermesh plugin`, counting only the commits in `<own>..<code head>` that change the repo's code (the `code` of the row `bmesh update` reads, so both find one code head), each spec with the `title` of `docs/proposals/NNN-*.md` on `main` (the id alone without one). The specs are those of the `NNN: done at <sha8>` subjects in `<own>..main` whose merged commit `<sha8>` the target has and the binary hasn't (or that the repo doesn't know), oldest first.

It removes the file when the binary is current or ahead, or not in the target's history (a binary of another line, or of unknown revision); with no check (`check_every = 0`, self-updating from a row, or no checkouts root) it removes it at start. A failed fetch is logged and tried again at the next check.

`bmesh update --notice` only prints that file: it never touches the network, never runs or asks for `init`, and prints nothing (exit 0) when the file is absent. The plugin's session hook runs it after `hosts --here` (see The skills); `mesh-status` prints the line last but for the hub line (see Several boxes).

**Installing from upstream.** With `[update] upstream = true` (and no `[update] repo`) the check also installs: when the target is ahead it runs the same `go install` and swap as `bmesh update`, then publishes `update`, drains and re-execs exactly as the self-update from a row does.

- A failed or refused install is logged once per target (`update: install failed; keeping this binary`), leaves the notice written, and is tried again on the next check (the module proxy can lag a fresh commit by minutes).
- On Windows it runs only under `serve --restart`; a daemon without it logs once at start, `update: [update] upstream needs bmesh serve --restart on Windows, which starts the new binary (init --service sets it up); notice only`, and is left on the notice.
- It is also left on the notice, logged once, for a binary without a revision or without a path to replace.
- With `check_every = 0` it is off.

## Upgrading

**From before v0.1.0: start over.** v0.1.0 (tagged at `61587e32`) was cut before releases carried upgrade notes, so its notes, `docs/upgrading/v0.1.0.toml`, were written after it and are the one exception to stepping a box forward: an install from before v0.1.0 is uninstalled and set up again, NATS included. In order:

1. Check that no lead or executor is live (`bmesh mesh-status --all`), and finish or forget (`bmesh node forget <path>`) any that are.
2. Stop and remove the `bmesh` and `bmesh-hub` services and any `nats-server` service, hub or leaf: the systemd user units, the launchd agents, the Windows logon task. A `bmesh serve` or `nats-server` run by hand is stopped by hand.
3. Remove the `nats-server` installed for bmesh, with its `<config dir>/nats.conf` and the JetStream `store_dir` that file names.
4. Move the config dir aside as `<config dir>.pre-0.1.0` (its `repos.toml` is kept, to publish again) and remove the state dir and the embedded hub's store.
5. Install `go install github.com/rickdiculousli/buildermesh/cmd/bmesh@v0.1.0` (or the release you go to; `bmesh update` has already done this when it walks the notes).
6. On the hub box first (several boxes, role hub) run `bmesh init --service`, then `bmesh init --hosts --admin` there.
7. On every other box run `bmesh init --service` (leaf or client) and `bmesh init --hosts` with an invite from the admin box (`bmesh hosts invite <box>`).
8. Put the kept `repos.toml` back, and have the admin publish it (`bmesh repos publish`).

A fresh start loses the results and events in the old JetStream store, and the roster and its keys; every spec's `verified` line keeps its record in git. Every release after v0.1.0 is incremental: its notes hold only the steps an install at the release before needs.

**The notes walk.** `bmesh update` walks a box through the notes of every release between the binary it runs and the one it installs, oldest first (a box that jumps from v0.1.0 to v0.3.0 walks v0.2.0's notes, then v0.3.0's).

- Notes are read from `docs/upgrading/<tag>.toml` on the update repo's `main`, not the tag, so notes written after a tag still apply (see Releases, Upgrade notes, for the format).
- Which releases are between is read from the running binary's revision, not guessed: the release tags in the target's history that the running revision's history lacks.
- A revision without v0.1.0's commit in its history predates it, and its walk starts with v0.1.0's start-over.

The binary being replaced may predate the walk, so the new one walks: once built, before the swap, `update` runs `<new> update --walk --before-swap --from <running revision> --to <target> --dir <main's repo>`, which:

- **checks the config**: it loads this box's `config.toml` and `repos.toml` with the new binary (`bmesh config check`). A config it refuses keeps the old binary, `bmesh: config check failed: config <path>: [runner] t3_server is empty: ...` then `bmesh: keeping this binary` (exit 3), so a box never restarts into a binary that won't load its config (a start-over walk skips the check: it moves the config aside).
- **answers the asks**: at a terminal, one line each, `track: boxes now update to releases, not every landing. Follow releases or every landing? [release]/latest`; Enter takes the default and an answer outside the choices is asked again. With no terminal every ask needs `--answer <key>=<value>` (the key as the ask names it, `[update] track`, or `track`) or `--defaults`, else nothing is written and `update` refuses, `1 ask waits on an answer ([update] track): pass --answer <key>=<value> for each, or --defaults; nothing changed` (exit 3), keeping the old binary. Answers go into `config.toml` (the old file kept as `config.toml.prev`, and put back when the new binary then refuses it) or, for a registry ask, into every row of `repos.toml` (kept as `repos.toml.prev`), which the admin then publishes (`bmesh repos publish`) or another box proposes.

**The steps.** The walk starts with `<from> -> <to>: 1 ask, 1 step` (a start-over adds `<release> starts this box over; it loses ...`). After the swap and the usual `<old> -> <new>`, the new binary runs `update --walk` for the rest. Each run or check step that applies to this box (its `os`, and its `when` against the config as it now is; one whose `when` doesn't hold prints `skip <release>#<n>: ...`) is printed, as `v0.2.0#2 restart bmesh-hub: ` and its command, and run in `sh -c` (`cmd /C` on Windows) with `BMESH_CONFIG`, `BMESH_CONFIG_DIR`, `BMESH_STATE_DIR`, `BMESH_STORE` (the embedded hub's store, as the config names it), `BMESH_FROM` and `BMESH_TO` in its environment.

- With `--apply` it runs without asking.
- At a terminal without it, each asks `Run it? [Y/n]`.
- With neither, the walk stops there: `the steps from v0.2.0#2 wait: run bmesh update --apply (with --yes for the ones that ask first)` (exit 0).
- A `confirm` step asks `Run it? [y/N]` at a terminal and needs `--yes` without one (`step v0.1.0#1 asks first: pass --yes to run it`, exit 3).
- A check's output must show its `expect`.
- A step that fails stops the walk: `step v0.3.0#1 failed: ...; fix it and run bmesh update again, which goes on from there` (exit 1).

`<state dir>/upgrade.json` holds the walk (its ends and the steps done), so `bmesh update` run again, now on the new binary and `up to date`, resumes after the last step done. A finished walk writes `<state dir>/installed` and ends `at v0.2.0`.

**Driven by an agent.** `bmesh update --plan --json` prints the walk and changes nothing: `{"from","to","releases","start_over","config","asks","steps","confirm","plan"}`, `config` being `ok`, the refusal, or `start-over`, and `plan` every step for this box's OS with its `id` (`v0.2.0#1`), `release`, `kind` and fields, `done`, its `answer` once given, and `applies` (whether its `when` holds now). Without `--json`: `<from> -> <to>: 1 ask, 1 step` and a line per step. Then `bmesh update --defaults --apply --yes` (or `--answer` for each ask) walks it without a terminal.

`bmesh update --walk --from <revision or release> [--to <revision>] [--dir <repo>]` walks a range by hand with this binary (`--to` defaults to this binary's revision, `--dir` to the repo `update` reads main from); `--plan` takes the same `--from`, `--to` and `--dir`.

**Where a box stands.** `bmesh init` writes `<state dir>/installed` (`<version> <revision> <time>`, the version being this binary's release or `<rev8>`), and so does every finished walk; it is a record, not the source of truth.

A binary swapped in without a walk (by a binary up to and including v0.1.0, whose `update` and self-update know no walk, or by the daemon below) finds at its first start that notes are pending when its release is past the record's (v0.1.0 with no record):

- `serve` prints, and every command run at a terminal (and `bmesh update --notice`, which the session hook runs) ends with, `bmesh: upgrade notes pending since v0.1.0: run bmesh update`.
- The daemon logs it and pushes it once through `[notify]` (the line pushed is kept in `<state dir>/pending-pushed`).

That `update`, finding the binary current, walks the pending notes from the record's revision, else from `<state dir>/previous` (the revision of the binary a swap replaced, written by `update` and the daemon), else from v0.1.0. An `update` that finds the binary current with nothing pending and no record writes one.

A box from before v0.1.0 that updated with its own old code is walked from v0.1.0, never through the start-over, since nothing recorded where it started. That is why this section leads with the start-over, and why `serve` checks its config at start (exit 78 naming the key, which its unit doesn't restart; see Config).

**The daemon's self-update** (`[update] repo`, or `upstream`) keeps its own build and swap and never walks: before the swap it runs the new binary's `config check` and `update --plan --json --from <running> --to <target> --dir <main's checkout>`.

- A refused config is logged and pushed (`update to v0.2.0: config check failed: ...; keeping this binary`), and so is a walk with an ask or a `confirm` step, `update to v0.2.0 waits on bmesh update (2 asks)`, rather than guessing an answer. Either keeps the running binary, is written as the notice (`bmesh update --notice`), and is acted on once per target until `bmesh update` takes it.
- A walk with only plain steps is swapped in and left pending for the new binary to name.
- One with nothing to do writes `installed` for the new binary.

**The hub keeps up.** `bmesh hub` is its own process and runs the binary it started with until its service is restarted, so `bmesh update` (after its other lines), `bmesh hosts --here` and `mesh-status` name a hub on this box running an older binary than serve: `hub runs v0.1.0, serve v0.2.0: restart bmesh-hub` (see Several boxes). A release's notes can restart it with a `run` step.

## Several boxes

A mesh spans boxes through one NATS hub, embedded only for a single box (`[mesh] nats = ""`). For several, `bmesh hub` runs the hub on one box and, where used, a leaf on another: bmesh's embedded nats-server (v2.15.1, as a library), configured from `[hub]` (see Config), so nothing to install and no `nats.conf` to write. Every box runs its own `bmesh serve` daemon for its leads, routines and map.

**Why a separate process.** A daemon restarts for every update (with `[update] repo`, every release, or every code landing under `track = "latest"`); a hub inside it would drop every box each time. `bmesh hub` keeps its starting binary until its service is restarted by hand, never by an update.

**Stale hub.** So `bmesh hub` and `bmesh serve` (the daemon, not the `--restart` supervisor) each write `<state dir>/hub.json` and `<state dir>/serve.json` at start, `{"pid","revision","release","time"}` of their binary, and remove it on a clean stop. When the hub's pid is alive and its binary isn't serve's commit and is older (by commit time when both have one, else any difference), `bmesh hosts --here` and `mesh-status` print `hub runs v0.1.0, serve v0.2.0: restart bmesh-hub` (each binary its release when stamped, else its revision's first eight characters).

**Setup.** `bmesh init`'s multi-box answer asks this box's part, **hub**, **leaf** or **client** (`--role` says it), and writes `[mesh]` and `[hub]` to match. With `[hub] role` set, `bmesh init --service` then writes the hub's unit beside serve's and prints how to enable it:

| Manager | Hub unit | Enable |
|---|---|---|
| systemd | user unit `bmesh-hub.service` (`ExecStart=<bmesh> hub`, `Restart=always`, `RestartSec=5`, `RestartPreventExitStatus=78`) | `systemctl --user daemon-reload && systemctl --user enable --now bmesh-hub` |
| launchd | agent `com.buildermesh.bmesh-hub` (`KeepAlive`, its log `~/Library/Logs/bmesh-hub.log`) | `launchctl bootstrap gui/$(id -u) <plist>` |
| Windows | none | run `bmesh hub` yourself |

Units restart always, since the tailnet address may not exist yet at boot. Each is handled alone: one differing from init's is kept with `.new` beside it, as `init` says above, so a hand-tuned serve unit survives adding the hub's.

**The hub** runs on an always-on box, on its tailnet address (Tailscale, say), never a LAN or public one: the tailnet is the boundary, and a token shared by every box the only auth. init asks for:

- the address: `<tailnet ip>:4222`, defaulting to this box's address in `100.64.0.0/10` if any;
- the leaf port: `<the same ip>:7422`;
- the token file: default `<config dir>/nats.token`, made if missing (24 random bytes in hex, mode 0600).

It writes:

```toml
[mesh]
nats = "nats://<tailnet ip>:4222"
token_file = "<config dir>/nats.token"
[hub]
role = "hub"
listen = "<tailnet ip>:4222"
leaf_listen = "<tailnet ip>:7422"
```

There `bmesh hub` serves clients on `listen` with the token; JetStream in domain `[mesh] domain` (default `hub`), stored in `[hub] store` (default `<state dir>/hub/jetstream`) up to `max_file_store` and `max_memory_store`; and leaves on `leaf_listen` (user `leaf`, the token as password: a leaf port takes a user and password, not a bare token); the server is named `[host] name`, logs nats-server's lines on stderr (the journal under systemd) and stops on SIGTERM or Ctrl-C, letting JetStream flush.

**A box joins** as:

- **Client:** init asks for the hub's URL and the same token file and writes them to `[mesh]` (`--hub` and `--token-file` say them; `--hub` alone is a client).
- **Leaf:** init asks for the hub's leaf URL and writes:

```toml
[mesh]
nats = "nats://127.0.0.1:4222"
token_file = "<config dir>/nats.token"   # the hub's token, copied here (mode 0600); init says so when it isn't there yet
[hub]
role = "leaf"
listen = "127.0.0.1:4222"
remote = "nats://<hub tailnet ip>:7422"
```

There `bmesh hub` runs a server without JetStream on `127.0.0.1:4222` that dials `remote`, redialing every second, forever, whenever the link drops; JetStream requests cross to the hub's domain. A leaf suits a box that sleeps or roams: its clients always find a local server, which reconnects to the hub by itself when the box wakes or rejoins the tailnet.

**Refusals.** `bmesh hub` won't start (exit 3), naming the key, with:

- `[hub] role` unset;
- `listen` (or a hub's `leaf_listen`) unset or on every interface (`0.0.0.0`, `::` or no host);
- a leaf without `remote`;
- `[mesh] token_file` unset, missing or empty.

`bmesh serve` with `[hub] role` set and `[mesh] nats` empty is refused too (exit 3): it would embed a second server where `bmesh hub` runs this box's.

**Instead of `bmesh hub`**, your own `nats-server` works, from a `nats.conf` holding the token; bmesh only needs `[mesh] nats` and `token_file` to reach it.

- **Hub:** `listen`, `authorization { token }`, `jetstream { domain: hub, store_dir }` and `leafnodes { listen, authorization { user: leaf, password: <token> } }`.
- **Leaf:** `leafnodes { remotes = [ { url: "nats://leaf:<token>@<hub>:7422" } ] }`, no `jetstream`.

**The token** is one random value, the same on every box, in `[mesh] token_file`, owner-only. Sharing the hub lets boxes see each other's nodes; to act on each other (no SSH between them), they join the admin's roster with `bmesh init --hosts` (see Hosts).

**Joining the roster.**

1. On the operator's box, `bmesh init --hosts --admin` makes it the admin, holding the roster-signing admin key; only it invites (`bmesh hosts invite <name>`, a one-time code good for 10 minutes) and revokes.
2. On each other box, `bmesh init --hosts` prompts for that code.
3. Each box answers whether it is **secure**: a secure box gets an approve key, and other boxes' asks of it wait for the operator's approval at its screen. Mark secure the box whose checkouts and plugins matter most, typically the laptop the operator types on.
4. Restart `bmesh serve` after `init --hosts`.

**The admin and approve keys** are the two keys no agent can use; their commands run at the box's own screen, not over SSH.

- **macOS:** in the login Keychain; every use raises the macOS password dialog, announced first by `Keychain dialog follows for: <action>` (see Hosts: Keys).
- **Elsewhere:** a file sealed with a passphrase asked on the terminal.

**What secure covers.** `secure` covers roster ops: `level` holds and the approve key (see Hosts). Not routines: a box with routines on runs any pushed commit's kind command for any mesh member. Routines are on by default (`config.toml`'s `[runner] checkouts`, default `~/.bmesh/checkouts`; `""` turns them off). `bmesh init --hosts` asks whether the box runs them, defaulting to no on a secure box, so a secure Mac meant to run a repo's `os = "darwin"` kinds answers yes.

**Sleeping boxes.** A sleeping laptop drops off the mesh and returns on its own: its leaf reconnects, and its daemon tells the wake from the gap between its minute ticks, re-reads the roster and takes the jobs it missed, in order (a job lives 24h; an expired one is answered and logged, not pushed). Meanwhile `bmesh hosts` and every other box's session line show it `asleep` with how long ago it was last seen, and `bmesh hosts ping` answers for it `asleep: queued until <expires>`. Keep the hub on a box that doesn't sleep.

**Which box a session is on.** With `[hosts]` on, the daemon writes the session line each minute and the plugin's session hook prints it, so every session opens with, e.g., `this box is hub-box; secure: no; on the mesh: laptop (asleep 2h, secure, admin)`. A box's guidance for sessions goes in `<config dir>/notes.md`, which travels signed in its presence; `bmesh hosts --notes` prints every box's (see Hosts).

## Runners

`bmesh lead start --repo R --spec NNN --brief FILE [--model M] [--effort E] [--title T] [--path P]`

Starts a lead through `[runner] kind` on `--model` (default `[models] lead`) at `--effort` (`low`, `medium`, `high`, `xhigh` or `max`; default `[models] lead_effort`, `""` the runner's own), registers it as node `<repo>-<NNN>` (`<repo>` the registry name's node token, see Names; state `idle`, model and effort recorded) and prints `{"path","thread","branch","worktree"}`.

- **Existing paths:** one whose node is not `done` or `blocked` is refused (exit 3); a `done` or `blocked` one is reused, its state reset to `idle`.
- **Timeouts:** the start runs under the runner's own timeout, not the CLI's one-minute deadline. If it times out, or registration then fails, the error names the thread id (and branch and worktree when known) so the session can be found; with the `t3` runner, where T3 names the thread, the branch it would be on: `start timed out; a thread on branch bmesh/<id> may exist`.
- **Lead cap:** with `[runner] max_leads` set, `lead start` refuses with exit 3 `bmesh: N leads already working on this host` once that many leads registered on this host are not `done` or `blocked`. It counts working leads, not resident sessions: a finished lead's session (a tmux window, a T3 thread) holds its memory until the operator closes it, and a lead counts only once its runner has started it. `scripts/memcap.sh N` measures what a box can carry: N leads on the tmux runner against its own mesh, each spawning three executors; it prints the peak memory.

#### `t3`

A thread in the box's T3 Code 0.1.0 at `[runner] t3_server` (default `http://127.0.0.1:3773`; `""` is a config error naming the key).

**Protocol.** Each call reads `<t3_server>/.well-known/t3/environment` (a `2` is trusted for a minute) and proceeds only for `orchestrationProtocolVersion` 2. A 1 (T3 before 0.1.0) is refused at start, status, send and stop with `bmesh: T3 <version> at <t3_server> speaks protocol 1; bmesh needs T3 0.1.0 or later`; any other version with an error naming it. A thread id that isn't T3 0.1.0's (`mcp:<uuid>`) is "no such thread".

**Runtime mode.** Every thread starts in `[runner] t3_runtime_mode`, the per-box fallback: `auto` (default; Claude Code's `auto` permission mode, where a classifier denies risky actions instead of asking anyone) or `full-access` (`bypassPermissions`). Config load refuses other modes: `approval-required` and `auto-accept-edits` stall an unattended lead.

**Calls** are bounded at 120 s and speak T3's MCP tools at `<t3_server>/mcp` (streamable HTTP, JSON-RPC, the `mcp-session-id` header, re-initialized once if T3 forgot the session), with the sign-in below.

**`start`.**

1. Finds the T3 project whose `workspaceRoot` is the repo (`t3_project_list`), or registers it (`t3_project_create`), which only a full-access sign-in may do: with an `auto` one, add the repo as a T3 project first.
2. Calls `t3_thread_launch` with the title, the brief as first message, `runtimeMode` `t3_runtime_mode`, model `{provider: "claudeAgent", model, options: {effort}}` and workspace `{type: "worktree", baseRef: <base or main>, branch: "bmesh/<thread id>"}`, or for a checkout bmesh made `{type: "existing_worktree", worktreePath, branch}`.
3. T3 names the thread (`mcp:<uuid>`, the node's thread) and the worktree (`<T3 home>/worktrees/<repo>/bmesh-<thread id>`).

A launch has no retry key, so a lost or timed-out reply is reconciled first: the project's newest threads with that title (`t3_thread_list`) are read until one is on branch `bmesh/<thread id>`.

**`status`** is `t3_thread_read`:

- **Running:** while `activeRunId` is set or the newest run is `preparing`, `queued`, `starting`, `running` or `waiting`.
- **Reply:** the newest `assistant_message`, reading the timeline on from where the last read stopped.
- **Logged once per change:** newest run `waiting` with a pending request, `t3: <path> waits on a question or approval`; `failed` on `usage_limit`, `t3: <path> hit a usage limit; it resets at <time>`.
- **Stop:** newest run `interrupted` (no run active), as T3 ends a run the user interrupts in T3, or `stop` does; the daemon marks the node stopped (see Stopping a node).
- **Not a stop:** `cancelled` (ended by T3's restart recovery) or `failed` (a crash), so a lead whose session died is still woken.

**`send`** reads the status first and never sends to a running thread: `t3_thread_send` in `queue` mode (never steering into a turn started since the status read) with `clientRequestId` `bmesh-<sha256 of thread, text and minute>`, so a retried wake is sent once.

**`stop`** is `t3_thread_interrupt`; an idle thread's `no_active_run` is success, as is a thread T3 no longer has.

#### `tmux`

1. `git worktree add -b <thread> <worktrees>/<repo>/<thread>`.
2. `claude [--model M] [--effort E]` in window `<thread>` of session `<tmux_session>` (created when missing; the window gets `BMESH_CONFIG` when the CLI has it, so the lead reports to the same mesh).
3. Watches the window until claude's prompt is up, answering a fresh worktree's dialogs: folder trust with yes, first-run screens with Enter, an offer to try something with Escape.
4. Types the brief in (one second before the Enter, since claude takes a burst of text as a paste); after 90 s it types anyway.

It can't tell whether claude is mid-turn, nor whether the user stopped it. `stop` presses Escape in the window.

#### `claude`

No T3, no tmux.

1. `git worktree add -b <thread> <worktrees>/<repo>/<thread> <branch or main>` (or, for a round's thread, the checkout bmesh made).
2. Each turn is a detached headless `<claude> -p --output-format json --dangerously-skip-permissions` in the worktree: the first with `--session-id <thread> [--model M] [--effort E]` and the brief on stdin, later ones with `--resume <thread>` and the same `--model` and `--effort` (a resume restores neither).

The session id is the thread id, so it must be a UUID (`lead start` passes one). Turns inherit the CLI's environment (so `BMESH_CONFIG` reaches the lead). State lives in `<user cache dir>/bmesh/claude/<thread>/`: `worktree`, `model`, `effort`, `pid`, `turn`, and per turn `turn-<n>.in`, `.json`, `.err`.

- **`status`:** a turn runs while its pid is alive and its `.json` is not yet claude's result; `status` replies with the newest result.
- **`send`** mid-turn queues the text under `queue/` rather than refusing; `status` starts the oldest queued send once the turn ends (the daemon polls status every minute, so queued sends drain).
- **`stop`** drops queued sends, so no later `status` starts them, and kills the running turn's process group.

It can't tell whether the user stopped a turn.

### T3 sign-in

The `t3` runner signs in to T3 as an MCP client. Only T3's OAuth sign-in gives a session a runtime-mode ceiling above `approval-required`, picked by the operator when approving, so a person approves each box's sign-in, and again every 30 days: the token has no refresh.

`bmesh t3 login [--server URL] [--callback-port 52299] [--timeout 1h]`

1. Registers bmesh as a public client (`<server>/.well-known/oauth-authorization-server`: dynamic registration, PKCE S256, grant `authorization_code`).
2. Prints the authorize URL and how to approve it from another machine (the phone or a laptop):
   - `ssh -N -L <port>:localhost:<port> <box>` for the callback (and, when the server listens on loopback, its port too: `ssh -N -L 3773:127.0.0.1:3773 -L 52299:localhost:52299 k16`); the fixed default port keeps the command the same every month;
   - a pairing code from `t3 auth pairing create` on the box if that browser isn't paired with its T3;
   - the access level: **Auto**, or **Full access** on a box whose `t3_runtime_mode` is `full-access`.
3. Takes the redirect on `localhost:<port>` (127.0.0.1 and ::1), or the browser's final URL pasted in, refusing another login's state. T3 binds the code to the origin the browser approved it on, named as `iss`; the exchange carries that origin, so a browser that reached T3 as `localhost` through a tunnel still signs in a bmesh that asks `127.0.0.1`.
4. Writes the token, expiry and ceiling (from the token's claims) to `<config dir>/t3-token.json` (beside `config.toml`, mode 0600: `server`, `token`, `expiresAt`, `scopes`, `access`) and prints `signed in: <host>: <access>, 30 days left`. A ceiling below `t3_runtime_mode` exits 3 saying so.

`bmesh t3 login --status` prints `<host>: <access>, N days left`, then the server, scopes and expiry (exit 3 with no sign-in or an expired one).

**Refusals.** With the `t3` runner, `lead start`, `build` and `ideate` refuse (exit 3, before anything is recorded) with:

- no sign-in or an expired one: `bmesh: T3 sign-in missing or expired; run bmesh t3 login`;
- one for a server other than `t3_server`;
- one whose ceiling is below `t3_runtime_mode`: `bmesh: T3 sign-in allows auto, below [runner] t3_runtime_mode full-access; run bmesh t3 login and approve full-access`.

A lapsed sign-in stops new leads on that box, not running ones, until the next login; meanwhile the daemon's poll of a running one fails.

**Expiry warnings.** The daemon of the box whose config has the token pushes `T3 sign-in on <host> expires in N days: bmesh t3 login` (tag `key`) 7, 3 and 1 days before expiry, once each. It checks hourly, with `[notify] ntfy` or `command` set, whatever `push` lists. Pushes sent for that expiry are kept in `<state dir>/t3-login-pushes.json` (`$XDG_STATE_HOME/bmesh`, else `~/.local/state/bmesh`), so a restart doesn't repeat one, a daemon down past a mark sends only the nearest, and a new login starts over. `bmesh mesh-status` closes with `t3 sign-in on <host>: expires in N days (bmesh t3 login)` under 7 days, or `expired`.

The runner reads its token through one credential interface, so a self-renewing source could replace the login without touching the runner.

**Renamed branches.** A runner may rename a lead's branch as it works (T3 does). Each minute the daemon reads the current branch of every local lead, fixer and engineer not `done` (`git rev-parse --abbrev-ref HEAD` in the node's `worktree`, if it exists on this host); a branch differing from the node's `branch` rewrites the node and logs `branch: <path> <old> -> <new>`. A detached or missing worktree leaves the node as is. `bmesh mesh-status`, the map and `bm-nodes` follow within the minute. The lead lands the branch its worktree is on (`git branch --show-current`), never the name in its brief.

## Proposal rounds

```
bmesh ideate <repo> [--idea] [--model M] [--effort E] [--context <text|->] [--force]
```

A round is two threads on one ship repo, an engineer and a UI/UX engineer, each writing three to five ranked half-page specs into `docs/proposals/` and landing them on `main` as `proposed`.

**Nodes.** The round is node `<repo>-round-<N>` (`<repo>` the registry name's node token, see Names; role `round`; `N` one past the repo's highest round in `bm-nodes`), with children `<repo>-round-<N>.eng` and `.ux` (role `engineer`, `parent` the round), each with an inbox, a thread and a bmesh worktree. `bmesh mesh-status` lists the round with its children; the map draws it like a lead with executors.

**Refusals.** `ideate` needs the repo's registry row, which carries `host`, `ux_host`, `verify` and `surfaces`; a path without one is refused. It refuses (exit 3):

- a row whose `mode` is not `ship`;
- five or more `proposed` specs on `main`, naming them: they wait on review;
- a round still open (its node neither `done` nor `blocked`): `refused: <repo>-round-<N> is still open (eng working, ux working); wait for it, or pass --force to start another`, since a second round from the same `main` numbers its specs from the same id and runs the repo's tests beside the first; `--force` starts it anyway;
- a host it cannot reach;
- a `main` it cannot level with origin (`pull --ff-only origin main` when the main checkout is on `main`, else `fetch origin main:main`; never a reset or a stash).

Then, from `main`, it:

1. makes `<[runner] worktrees>/<repo>/<repo>-round-<N>-eng` and `-ux` on branches of those names (an existing branch or path is refused, untouched);
2. renders the two briefs (the next free spec ID for the engineer, five past it for the UI/UX engineer, so they never collide; the thread's UUID as `proposed_by`);
3. registers the round (its `started` event opens it as `working`);
4. starts both threads through the runner, the engineer on the row's `host`, the UI/UX engineer on `ux_host` (`ux_host` `""` = `host`; `host` `""` = this box), with `--model` (default `[models] proposer`) at `--effort` (default `[models] proposer_effort`), titled `[ideate] <repo> round <N>: engineer|UI/UX`;
5. prints `{"path","round","eng":{"path","thread","branch","worktree","host"},"ux":{...}}`.

Briefs are kept under `<user cache dir>/bmesh/briefs/<round>-<child>.md`.

**`--context`** carries what the operator asked of the round (`-` reads stdin): both briefs get a section `## The operator's focus` with the words as given, trimmed, before their closing; a repo's own template without `{{operator_context}}` gets it appended. No words, no section.

**`--idea`** makes the round the operator's one idea, given in `--context` (refused without it, exit 2, in the full and child forms):

- only the engineer starts (`ux_host` neither checked nor asked);
- its brief gets `## The operator's idea` instead of the focus section (write that idea's spec, one half-pager or two or three in build order, and propose nothing else), and its opening, partner and numbering lines say so (`{{round_ask}}`, `{{round_partner}}`, `{{ux_ids}}`, usable by a repo's own engineer template);
- the round node carries `"idea": true`, the `started` text reads `round <N> on <repo>: one idea, eng on <host>`, and the output has no `ux`;
- the round settles on the engineer alone, its pitch the engineer's line.

**Another host** is reached as `ssh -o BatchMode=yes <host> bash -lc 'bmesh ideate <repo> --child eng|ux --round N --next-id N --thread-id UUID --model M [--effort E] [--context <text>] [--idea]'`: the child form, which makes that one worktree, brief and node on its own box (the registry is the same everywhere) and prints its child's line.

- Both hosts are checked first (`bmesh help` over ssh), so an unreachable one is refused before anything starts.
- The child form refuses a child whose node is live or whose branch exists.
- Both children are started whatever happens to the other. One that doesn't start is marked `blocked` (`did not start: <error>`), the round reads `blocked` naming it, and the command exits 1; the started thread keeps running, its pitch reaching the operator when the daemon resettles the round.

With the `t3` runner a round's thread runs in the bmesh worktree, filed under the target repo's T3 project: T3 binds only a project's own worktrees.

**Reporting.** Each thread's node reports like any node (`started`, `progress`, `ask`, `blocked`, `done`); the daemon polls and wakes an engineer like a lead. An engineer's `done` needs only `--commit` (the commit on `main`) and carries its ranked list as `--text`.

**Settling.** Each minute the daemon settles every open round:

- **Both children `done`:** the round goes `done`, its text the merged pitch (the engineer's list, then the UI/UX engineer's, each as reported), and the daemon publishes `done` from the round.
- **One child `blocked`:** the round goes `blocked` (`<child> blocked: <text>`, then the other's pitch once done).

A blocked round is rechecked each minute: when the other child's pitch arrives it says `blocked` again with it, and if a blocked child is unblocked and both finish `done`, the round is `done` after all. A settlement's event carries a `Nats-Msg-Id` of the round, state and text, so two daemons settling the same round store and push it once.

**Pushes.** The round's `done` is pushed as `<repo> round N proposed` (priority default, tag `memo`, the pitch as body), its `blocked` as `<repo> round N blocked` (high, `no_entry`); each opens the engineer's thread on a tap when `[console]` resolves it and, like a lead's, needs the event in `[notify] push`. An engineer's `ask` pushes like a lead's (`<path> asks`, with its own thread's link); its `done` and `blocked` reach the operator through the round.

## Builds

```
bmesh build <repo> <NNN> [--model M] [--effort E] [--path P] [--family F] [--hold land|now] [--force]
bmesh build --accepted <repo> [--model M] [--effort E] [--family F] [--hold land|now] [--force]
bmesh scaffold <repo> [--check]
```

`build` dispatches accepted specs to leads. Before anything is recorded, it checks:

- **Row:** a path without a registry row is refused (exit 2), a non-ship row exits 3.
- **Spec:** it levels `main` as `ideate` does (see Proposal rounds) and reads the ledger there. The spec must read `accepted` (another status is refused, exit 3, naming it).
- **Model:** the spec's `judge` names it (`fable`, `opus`, `sonnet` as `claude-fable-5-1`, `claude-opus-5-5`, `claude-sonnet-5-5`); no judge is refused unless `--model` says. Effort is `--effort` (default `[models] lead_effort`).
- **Node:** `<repo>-<NNN>`, or `--path`. A path whose node is not `done` or `blocked` is refused (exit 3, `use --path <repo>-<NNN>-2`), as is a host at `[runner] max_leads`.

Then, past the hold and the verify of `main` (below), in this order:

1. `building` lands on `main` (`built_by` the thread id chosen for the lead, `host`, `family`, and `branch` when the runner names branches ahead, as `claude` and `tmux` do), committed as `Dispatch NNN: building on <host>` in a bmesh worktree, fast-forwarded and pushed as `choose-ideas` lands (see The ledger).
2. The build brief (the spec file whole, the row's done rule and verify commands, the node path, the standard closing) is rendered to `<user cache dir>/bmesh/briefs/<path>.md`.
3. The lead starts through the runner with that thread id, titled `[build] <repo> <NNN>: <title>`, exactly as `lead start` would.

A runner that names the branch itself (`t3`) gets it recorded in a second commit, `Dispatch NNN: branch <b>`. A start that fails moves the spec back to `accepted` (`Dispatch NNN: did not start`) and exits 1.

It prints `{"path","thread","branch","worktree","model","spec","building","family"}` (and `hold`, with `--hold`; see Holds), `building` the commit that recorded it, and `warnings` when the spec defines a command that `main` or another spec already has: the lines `choose-ideas` warned of at its accept, read again from the ledger on `main` (see The ledger). A model the runner rejects is the operator's to retry with `--model`: the binary has no fallback.

Every lead one `build` starts is in one build family, the leads a coordinator dispatched together, whose feedback it reads at the end (see Feedback).

- `--family F` joins family `F` (one subject token: letters, digits, `-` and `_`; anything else exit 2).
- Without it, each invocation mints `<repo>-build-<N>` (the repo's node token, see Names), `N` one past the highest family of that shape registered for the repo in `bm-nodes` or still holding feedback, as rounds are numbered.

The lead's node records it as `family`, and so does its spec, landed with `building`. A coordinator dispatching across several commands or repos passes the first family to the rest; over ssh it goes on the remote command line.

#### Holding and verifying `main`

Before anything is recorded, once the spec reads `accepted`, both forms read the repo's leads on every host and hold while its last build run is still going. What holds a build:

- a lead outside the build's family (`--family`, or the one just minted) not `done` or `blocked`, a lead with no family as itself;
- a family of the repo's (one a lead of the repo is in, or named `<repo>-build-<N>` or `<repo>-fix-<N>`) with feedback pending.

The build's own family never holds it. Builds run in two pipelines that don't hold each other, the fixer's families (`<repo>-fix-<N>`, see The fixer) and every other family: a lead or feedback of the other pipeline is passed over. Feedback naming bmesh's own repo (see The fixer) holds no build at all, since the fixer owns it; with no such row every feedback holds as above. `[runner] max_leads` still caps the host across both, and the `land.<repo>` lease still serializes landing.

A held single `build` is refused (exit 3) with `held: <family> is still running (<path> <state>, ...); watch it with bmesh watch --nodes <paths>` or `held: <family> has <N> feedback pending; drain it with bmesh feedback list/drain <family>`. `--accepted` prints `held <family>: <why>` (`--json`: `[{"state":"held","family","reason"}]`) once, exits 0 and starts nothing.

Then it verifies `main`:

1. The commit checked is main's code head (see Updates), so the `Dispatch`, `Review` and `done` ledger commits, and with `code` set a README, document or art commit, never ask for a verify of their own.
2. A `verify` result recorded for its tree answers (`verify main <sha8>: ok t:<tree8> (answered from the record)`).
3. With none, the repo's `verify` kind, as that commit defines it, runs inline in a bmesh worktree at that commit (`<[runner] worktrees>/<repo>/<repo>-verify-<hex8>`, removed after) as `land` runs it, and records (`verify main <sha8>: running at t:<tree8>`, then `... (recorded)`).

A failing verify refuses the build (exit 3, nothing dispatched): `main is red at <sha8> <subject>: verify fail t:<tree8> (<how>); build its fix with --force`, then the last 30 lines of its output. A `main` defining no verify kind prints `verify: <repo> defines none` and goes on. `--accepted` holds and verifies once for the batch, only when any spec is accepted. These lines go to stderr, and to stdout among `--accepted`'s lines.

`--force` gets past both, as `ideate --force` gets past an open round: to dispatch beside a running family, or the spec that fixes a red `main`. It runs no verify and says what it passed: `--force: past held: ...`, `--force: past main is red at ...` or `--force: past verify main <sha8>: no result for t:<tree8>`.

`build` holds two of the fixer's rulings itself, which `--force` doesn't pass; a single `build` checks them before a hold or a verify, `--accepted` per spec once past both. Refused (exit 3):

- a spec carrying `ruled` sized `L` or larger: `refused: spec NNN is ruled and sized L (reversible: anything sized L is proposed, never built); set it proposed`;
- a spec whose `fix_of` chain on `main` is three deep, the third fix on one premise: `refused: spec NNN is fix 3 on one premise (NNN fixes MMM fixes KKK fixes JJJ) (premise: no third fix on one premise); set it proposed with the belief the fixes assumed`.

`--accepted` prints each as `not started` and goes on. The chain follows each spec's `fix_of` (its first three-digit id) until a spec without one; a two-deep fix builds.

`--accepted` dispatches every accepted spec, lowest id first, through the same steps; one spec's refusal does not stop the next. One line per spec:

- `started NNN <title>: <path> on <model> (thread, branch, building, family F)`, then `; warning: <warning>` per warning above;
- `waits NNN <title>: on 004 (building)`: its body names a lower id (`004`, or a run `001 to 004`) whose spec is not yet `done`, `rejected` or `later` (an id above its own is a pointer, not a dependency; a rejected or later spec never reaches done, so it is not waited on);
- `waits NNN <title>: 3 leads working on <repo>`: three leads of the repo (on any host) are not `done` or `blocked`, counting those just started;
- `not started NNN <title>: <the refusal>`.

When any started, a last line names their family: `family <repo>-build-<N>`. `--json` prints the lines as objects, each with `family`.

#### From a chat

From a chat (a phone, a session with the plugin and no command typed), the operator runs this lifecycle through the plugin's operator skills, the chat entry points:

- **`buildermesh:ideate`** ("proposals for X", "run a round on X"):
  1. runs `ideate` on the row's `host` (locally, else over ssh), with whatever the operator asked beyond the repo's name as `--context`;
  2. watches the round with `bmesh watch --nodes <repo>-round-<N>` and relays an engineer's `ask`;
  3. tells the operator in the chat when the round is proposed (its ranked pitch) or blocked.

  For one specific idea of the operator's own (decided by what was asked, not the words) it starts no two-thread round:

  - it runs `ideate --idea` (a round of one engineer thread, on the row's `host`) when the operator asks for a thread, a model or an effort, or the idea needs the repo read and measured at length;
  - otherwise it writes that idea's spec in the session (one half-pager, or two or three in build order for an idea too big for one lead, `proposed_by: operator`) and lands it on `main` as `proposed`.
- **`buildermesh:choose-ideas`** (the reply to a pitch in plain words) runs `choose-ideas`.
- **`buildermesh:build`** ("build X 3", "build what's accepted in X"):
  1. runs `build` on the row's `host`; a `held` refusal it waits out with `bmesh watch` on the named nodes, draining its own family first, and runs again; one held by another coordinator's family, or a red `main`, it reports to the operator;
  2. watches the leads with `bmesh watch --nodes <paths>`, relays each `ask` to the operator, answering it with `bmesh --node operator send`, holds and releases leads with `hold` and `unhold` instead of a free-text hold ("hold X", "hold land on X", "release X"; see Holds), relays a lead reaching its gate as one line, and stops one with `node stop` instead of a free-text STOP message ("stop X"; see Stopping a node);
  3. once a family's watch is done, names the fixer the daemon started for the issues naming bmesh's own repo (see The fixer), and turns the rest into accepted and proposed specs and drains them (see Feedback).
- **`buildermesh:scaffold`** ("scaffold X") runs `scaffold --check`, shows the diff, runs `scaffold`, the row's verify, commits `Scaffold the shared definition of done and proposals` and pushes.
- **`buildermesh:mesh-status`** ("what's building", "where is X") summarises `mesh-status` (and `spec list` for a named repo) as working, asking, silent, stopped and done since the last look.

`ideate` and `mesh-status` too run "stop X" (a round, a thread) as `node stop`, never as a message, which would wake it. `propose` and `build` carry the Fable fallback the binary has not: a Fable model the runner rejects is run once more with `--model claude-opus-5-5`, telling the operator what the runner said.

Each skill ends with one line: what is started or landed, and what waits on the operator, naming each thing by what it is in plain words (a spec or issue by what it does, its number after it in brackets if at all), so the line reads on its own. The done rule `scaffold` writes and every build brief's closing end the same way (`brief.PlainWords`).

#### `scaffold`

`scaffold` gives a ship repo what a build expects of it, from the row and the binary, and changes nothing else, in the checkout the row names (given by name or path) or in a linked worktree of it given by its path (the row whose checkout shares the worktree's git common dir), written in place, so a lead scaffolds its own branch without touching the main checkout. It writes:

1. **`AGENTS.md`**: three sections between `<!-- bmesh:done -->`, `<!-- bmesh:proposals -->` and `<!-- bmesh:mesh -->` markers, in that order: the definition of done from the row's verify commands and remote; the proposals convention; the Buildermesh paragraph of `docs/agents-snippet.md`, embedded, its verify placeholder filled from the row. Only the text between the markers is rewritten; a missing block is appended at the end (a missing `AGENTS.md` is created with the blocks). A block an earlier scaffold wrote between `command-center:` markers is read the same way and rewritten in place under `bmesh:` ones, so a repo never gets a second block.
2. `docs/proposals/README.md` (this repo's, embedded) and `docs/proposals/assets/.gitkeep`, when missing.
3. A kind file for each of the row's `routines` the repo lacks, seeded from `examples/routines/` (`verify` inline and deterministic, its `command` the row's verify commands joined with ` && `). A row with no verify commands gets no verify kind and a line on stderr says so; a routine with no example is an error naming the file to write by hand.

An existing kind is never touched, but for the verify kind's `command` when the row has verify commands: one whose command (as the kind loader reads it) is not those commands joined with ` && ` gets its one `command = ...` line rewritten to them, the rest of the file kept, whether or not the row's `routines` name it. A verify kind that runs a model is left alone; one whose command spans lines is an error naming the file to set by hand. So the row's verify is the source of the verify kind and of both blocks that quote it, and a change to the verify commands is a change to the row (see Hosts: The registry), which `scaffold` then writes.

It prints `wrote <file>` per file, or `<repo> is scaffolded`. `--check` prints the unified diff of what would change and exits 1 when there is any, writing nothing. A row that is not `ship`, or a path without a row, exits 2. A second run changes nothing, so run it again after the row or the snippet changes; commit what it wrote.

### Holds

```
bmesh hold <path>|--family F [--gate land|now] --text "<why>"
bmesh unhold <path>|--family F [--text "<words>"]
bmesh held
```

A coordinator (the `build` skill's session, or the operator) holds a lead at a gate until it says so, for landings in order, a diff to read before it lands, a pause while quota is low, or a redirect before the lead builds the wrong thing.

**`hold`** writes KV `bm-holds` `<path>` to `{"gate","text","by","at"}` (`by` the sender: `--node`, else this node, else the host name) and sends the lead a message of kind `hold`, `hold from <by> (<gate>): <why>`, which wakes an idle lead as any message does. It prints `holding <path> before land: <why>` (`now: <why>` at the gate `now`) per lead, `--json` `[{"path","gate","text","by","at"}]`.

- `--gate land` is the default; another gate than `land` or `now` is a usage error (exit 2). `--text` is required. A hold replaces one already there.
- `<path>` must be a registered lead not `done` or `blocked`. An executor is refused (exit 3, `refused: <path> is an executor; executors are never held: hold its lead <lead>`), and so are a node of another role, a done or blocked lead and a path with no node.
- `--family F` holds every lead of build family `F` not `done` or `blocked`, refused (exit 3) when there is none.

Rounds and routines are never held, a hold never releases itself (a release is an act, never a timer), and there are no gates but these two. Any sender may hold, as any sender may `send`.

- `land`: `bmesh land` reads `bm-holds` before it takes the lease and again once it holds it, and stops (exit 3, nothing pushed) with `bmesh: held before land by <by> <age> ago: <why>; nothing is pushed: run bmesh held and end your turn, and run bmesh land again once the release wakes you`. A `now` hold stops it the same way (`held now by ...`).
- `now`: every `bmesh inbox` the lead runs while the hold stands prints it first, as its own line, `held now by <by> <age> ago: <why>; stop here: run bmesh held, end your turn, and wait for the release` (under `--json`, first, a message of kind `held` from `<by>`). Ages run to the minute, whole days as `1d3h`.

**`held`** is the lead at its gate: it reads its own hold, publishes `held` with the text `before land by <by>: <why>` (`now by ...`), moves the node to the state `held` and prints `held <path> <text>; end your turn: the release wakes you`. With no hold it is refused (exit 3, `refused: <path> is not held (released already?); run bmesh inbox and carry on`), so a release that came first sends the lead on.

`held` is reached from `working`, `asking` or `silent` and left by the node's own next event: the release wakes the lead, and its next `started` or `progress` makes it `working` again. The daemon never calls a `held` node silent (only `working` goes silent) and never pushes it (`held` can't be in `[notify] push`); the map's needs-you band has no row for it: the coordinator put it there. Shown by:

- `mesh-status`: the state and, after the table, one line per hold with its age, `<path> held before land by operator 3m ago: <why>` (`--json`: the row's `hold`);
- `watch`: the `held` event, `<at> <path> held <text>`, so the coordinator sees the lead reach its gate, and goes on watching;
- the hourly digest: once per whole day of its age, `<path>: held before land by operator 1d0h ago: <why>`, in the pass whose window holds that mark, so a forgotten hold is named once a day.

**`unhold`** deletes the key and sends the lead a message of kind `release`, `released by <by>` (`: <words>` with `--text`), which wakes it, printing `released <path>`. A lead not held prints `<path>: not held`, sends nothing and exits 0. `--family F` releases every held lead of the family, `<F>: nothing held` when there is none. (`bmesh release` cuts a release tag, see Releases.) `node forget` deletes a node's hold with it.

**`build --hold land|now`** (both forms), "build these, land none until I say", writes each lead's hold (`by` the sender, text `held at dispatch (build --hold <gate>)`) before the runner starts it, and sends the hold message once it has started. A message that fails is said on stderr, `build: <path> is held before land, but the hold message failed: <err>`, and the start stands. Its JSON carries `hold`, and `--accepted`'s started line ends `, held before land` (`, held now`). A start that fails deletes the hold, and a build without `--hold` deletes a hold an earlier lead left on its path.

### Briefs

The engineer, UI/UX engineer, build, fix and half-pager templates are embedded in the binary (`internal/brief/templates/`). A repo overrides any but `fix` with `.bmesh/briefs/<name>.md` (`engineer`, `ux-engineer`, `build`, `half-pager`): the fix brief carries bmesh's rulings, which no repo sets. Briefs are kept under `<user cache dir>/bmesh/briefs/<node>.md`.

A template is filled in one pass: `{{name}}` placeholders take values and braces inside a value stay as written. A placeholder with no value is an error naming the template and the placeholder, never left in a brief. Every brief gets:

- `repo`, `repo_path`;
- `verify`: the row's commands in backticks, or that none are listed;
- `surfaces`: the row's, or what to do without them;
- `standard_closing`: the closing paragraph with the verify commands inlined;
- `done_rule`: the definition of done the row implies: verify passes; Opus in a session that did not write it reviewed it when it is sized M or larger as built (its spec's `size`, or the size its scope grew to; work with no spec sized as a spec would be), at medium effort for M and high for L or larger, while an XS or S change is done on verify alone; rebased, fast-forwarded to `main`, pushed to the row's `remote`.

A round's brief also gets `node`, `round`, `round_n`, `worktree`, `branch`, `thread_id`, `next_id`, `date`, `operator_context` (the operator's focus section, or empty); the fix brief `node`, `date`, `plain_words` (the plain-words rule every closing ends with); a build brief `spec`, `spec_path`, `spec_id`, `node`, `date`, `upgrading` (empty, except in bmesh's own repo, where it is a bullet asking the lead to fill the spec's `## Upgrading` section as built, with the template's rule).

The proposal briefs carry the repo's facts and point at the `ideate-engineer` and `ideate-ux` skills for the method. The build brief carries the spec, points at the `lead` skill, and says:

- the review is sized as `done_rule` says above, queued with `--effort`;
- its findings are fixed and then landed, the second pass only after a rebase changed the code;
- a taste finding is noted in the final message, not re-reviewed.

## Feedback

```
bmesh feedback "<text>" [--repo R] [--where <file|command>] [--fixed SHA]
bmesh feedback list <family>|--repo R [--json]
bmesh feedback drain <family> [--through SEQ]
bmesh feedback drain --seq SEQ[,SEQ...]
bmesh fix [--model M] [--effort E]
```

A lead that hit friction outside its spec, which another build would hit too, leaves one `feedback` line per issue once it has decided it is done, just before `bmesh done`; its own spec's work is not feedback. Executors never write feedback: they report to their lead, which decides what affects other builds.

`feedback` reads the family, repo and spec from the node (a node with no family, one not started by `build`, is refused: exit 3, `no build family`; an unregistered one exit 1). It publishes `{"from","family","repo","spec","commit","where","fixed","text","at"}` on `bm.feedback.<family>` in `bm-mesh`, `commit` the node's worktree HEAD when it is on this host, and prints `feedback <seq> in <family>` (`--json`: the record with `seq`).

`--repo` (a registry name or path; default the node's repo) names where the friction is, `buildermesh` when it is in the mesh itself (bmesh, a skill, a brief); `--where` the file or command it was met in; `--fixed` a fix the lead already landed, so what is left is the general case.

There is no consumer: a message is pending for exactly as long as it is in the stream, until it is drained or the stream's 30 days have passed.

- **`list`** with a family prints its pending feedback, oldest first, one line each: `<seq> <from> (<repo> <spec>[, where W][, fixed F]): <text>` (`--json`: an array of the records, each with `seq`); nothing pending prints nothing.
- **`list --repo R`** (a registry name or path) prints every family's pending feedback naming `R`, in stream order, each line naming its family (`<seq> <from> (<repo> <spec>, family <F>[, where W][, fixed F]): <text>`). It leaves out an issue whose sequence a spec on `R`'s `main` names in its `from_feedback` (`#<seq>`), whatever the spec's status, so a pass that stopped between landing a spec and draining its issue never writes that spec twice.
- **`drain --seq`** deletes exactly the feedback messages at those sequences, whatever their family, and prints `drained N`, then `not pending: <seqs>` for any already gone. A sequence holding anything but feedback is refused (exit 3) and nothing is deleted.
- **`drain <family>`** deletes the family's messages from `bm-mesh` with a subject-filtered purge, up to and including `--through` (else up to the last one there when it starts), and prints `drained N from <family>`. Nothing about them is kept on the mesh, only that family's subject is touched, and a message published after `--through` stays.

`mesh-status` closes with `feedback: N pending in <family>` per family with any pending, so feedback whose coordinator has gone is seen and drained within those 30 days. The issues naming bmesh's own repo are the fixer's and show as one line instead, `fixes: N pending for <repo> (...)` (see The fixer).

The coordinator, the `build` skill, reads the queue once `watch` prints `watch-done` for a family:

1. Issues naming bmesh's own repo are the fixer's, which the daemon starts (see The fixer): the coordinator reports the fixer `mesh-status` names on its `fixes:` line and leaves them pending.
2. For the rest it lists, merges duplicates, drops what a spec on `main` already covers (any status, so rejected ideas don't come back) or a `--fixed` commit settled, and sorts the rest:
   - a quick fix (one cause, one obvious change, XS or S) becomes a spec it lands as `accepted` (`Feedback NNN: accepted from <family>`, `proposed_by` its thread, `from_feedback` the family and leads) and builds once the family is drained (pending feedback would hold the build), in a new family, with no operator review;
   - anything that needs thought it writes up itself as a `proposed` spec (`Feedback NNN: proposed from <family>`, the same `proposed_by` and `from_feedback`) that waits on the operator's review, never as a round: the issues are already the ideas;
   - an issue from a lead built from a `from_feedback` spec is always proposed, never built at once.
3. It drains the issues it ruled on with `drain --seq`, leaving the fixer's, so the specs' `from_feedback` (`<family> (<leads>) #<seq>`) is the only record left.
4. It reports one line per issue: `fixed by NNN (started)`, `proposed NNN`, `already NNN`, `dropped: <why>` or `to the fixer`.

A coordinator that dies between list and drain leaves the family pending, and `mesh-status` shows it.

### The fixer

On bmesh's own repo only (the registry row whose `go.mod` declares the module the binary was built from, the row `update` and `land` find), the feedback naming it is ruled on by a fix lead in a pipeline of its own, under written rulings, and the fixes it rules are built without the operator reading them first. Every other repo's feedback stays with the coordinator and the operator, as above; with no such row, nothing here is active.

`bmesh fix` takes no repo. It levels `main`, then starts the fixer on bmesh's own repo through the runner, as `lead start` starts a lead:

- node, family and path `<repo>-fix-<N>` (`N` one past the highest registered in `bm-nodes`, as a node or a fix lead's family, or still holding feedback), role `fixer`;
- on the row's `host` (over ssh, `bmesh fix` there, when that is another box);
- on Opus (`--model` otherwise) at `[models] lead_effort` (`--effort` otherwise).

It prints `fix: <path> started (thread <id>, <model>, on <host>)` (`--json`: `{"path","thread","branch","worktree","model","family","host"}`). Refused (exit 3): with no such row (`refused: no registry row holds the module bmesh was built from`), on a row that isn't `ship`, and while a fixer of the repo is neither `done` nor `blocked` (`refused: fixer <path> is <state>; one fixer writes the queue`), so the queue has one writer. `[runner] max_leads` doesn't count the fixer, only the fix leads it starts. The daemon wakes and watches a fixer as it does a lead.

No session has to run `fix`: the daemon of the row's `host` starts it (any daemon when the row names none, as `fix` runs where it is run), with the row `ship`. A fixer is due when:

- none is live (neither `done` nor `blocked`);
- the newest isn't `blocked` (a block, stopped included, waits on the operator and is never restarted);
- an open issue (pending feedback naming the repo that no spec on its `main` names in `from_feedback`, what `list --repo` prints) was published at or after the newest fixer's start, or there has been no fixer.

An issue a fixer had when it started and left is not started on again. The daemon decides on every feedback message published and on each minute's pass, from what exists (the issues and the fixer nodes), never from what an earlier pass meant to do, so a restart, a missed message or a fixer that ended with an issue arriving after its start all converge on one fixer.

1. It claims the start by publishing `bm.daemon.<host>.fixer` (`to` the fixer's path, `<repo>-fix-<N>`, `text` `<N> open for <repo>`) with `Nats-Msg-Id` `fixer-<path>`, as a push is claimed. A duplicate reply means another daemon has it; a start that failed is tried again once the stream's two-minute duplicate window has passed.
2. It runs `bmesh fix` with its own binary, which refuses a second fixer anyway; its output and any failure are in the daemon's log.

With no row holding bmesh's module, or a binary that doesn't say its module, the daemon starts nothing.

Only the fixer reaches the operator. A lead in a `<repo>-fix-<N>` family (a fix lead) pushes nothing: its `ask`, `blocked` and `done` go to the fixer's inbox as an executor's go to its lead (`ask from <path>: <text>`, `blocked from <path>: <text>`, `done from <path>: branch <b> at <sha8>: <text>`), and the fixer answers or relays them under the rulings and reports them. The fixer's own `ask`, `blocked` and `done` push as a lead's do (see Pushes).

`mesh-status` shows the open issues as `fixes: N pending for <repo> (<fixer> <state>)` while a fixer has them, `(starts within a minute)` when the daemon starts one on its next pass, `(<fixer> blocked)` while the newest fixer's block waits on the operator, and `(<fixer> done; they wait on you)` when it had them and left them.

Its brief, the embedded `fix` template (no repo overrides it), carries these rulings, and each spec it writes cites the ones it rests on by slug:

1. `root`: fix the first wrong decision, not the place it showed, then every other place with the same fault. A guard is a fix only when the state it handles is legitimate.
2. `failing-first`: name what fails before the fix and passes after: a test through the real command or package, or a run with its output. A rate or timing the fix rests on carries its run count. An issue that can't be made to fail is dropped, saying what was tried.
3. `smallest`: the smallest change that removes the cause, deleting before adding. A sentence added to a skill or brief every session loads names the sentence it replaces, or why none can go.
4. `structure`: a lesson met twice becomes a check. When a skill sentence already warns about the issue, the fix is a refusal, warning or test in the binary, and the sentence goes.
5. `premise`: no third fix on one premise. When two fixes of the same thing haven't held, write down the belief both assumed, test it, and propose it to the operator with the evidence.
6. `no-new-surface`: a fix adds no command, flag, config or registry key, frontmatter field, bucket, subject or event. Anything new the operator would have to learn is product direction, and is proposed.
7. `reversible`: build what can be undone inside the repo without asking, then report it. Proposed, never built: taste, product direction, anything sized L, and anything outside the repo's `main` (a step on another box, deleted records, the registry or a box's config).
8. `drop`: dropping is an outcome: what a spec on `main` already covers (any status), what a `--fixed` commit settled, and the reporting lead's own spec's work.

The code-design principles (types, boundaries, domain modelling, reader load) are left to the repo's `AGENTS.md` and its review; a migration is sized L and goes to the operator; what is proposed is designed by the ideate method. `build` holds the two rulings it can check (see Builds), and `list --repo` hides what a spec already names.

The fixer follows the `fix` skill, one pass:

1. `feedback list --repo <repo>`.
2. One ruling per issue. A fix is a spec with `status: accepted`, `ruled: [<slugs>]`, `fix_of: <the reporting lead's spec>` and `from_feedback: <family> (<lead>) #<seq>`. What `premise`, `no-new-surface` or `reversible` send on is a spec with `status: proposed` and the ruling named in its Opportunity (no `ruled` or `fix_of`, which `build` would check after the operator accepts it). A drop is a line in the report.
3. Each spec lands on `main` and its issues are drained at once (`drain --seq`).
4. Each fix is built one at a time (`bmesh build <repo> NNN --family <the fixer's>`) and watched, the fixer answering an `ask` the rulings settle and relaying the rest, and listing again after each one, so a fix lead's own feedback is ruled on in the same pass.
5. Once the queue is empty and no fix lead runs, it releases the repo when it has a release tag, then `done`, its text one line per issue (`<seq>: fixed by NNN (root, failing-first)`, `<seq>: proposed NNN (no-new-surface: ...)`, `<seq>: dropped (drop: 044 covers it)`), what waits on the operator first, then the release.

## Releases

```
bmesh release <repo> [--minor|--patch] [--dry-run]
```

A release names the point where a batch's work is all in and verified. `release` runs from the coordinator once a build family is done and its feedback drained, tags the repo's code head (see Updates) as `vX.Y.Z`, an annotated tag, and pushes the tag to `origin`.

It takes the repo's registry row (a path without one is refused, exit 2), fetches `origin` with its tags first and reads `origin/main` (`main` in a repo with no `origin`, where the tag stays local).

**The version.** A repo with no `vX.Y.Z` tag in `main`'s history gets `v0.1.0`. After that, the highest such tag's minor version goes up (patch `0`) when any spec landed since it is a `feature`, else its patch version. `--minor` or `--patch` says which instead (two at once is exit 2).

**The specs landed since** are those of the `NNN: done at <sha8>` subjects after the last tag (all of them for the first), less any whose `merged` commit is in the last release's history: a release tags the code head, so the `done` commit of a spec it carries comes after its tag.

**The tag's message.**

1. `<repo> vX.Y.Z`, a blank line;
2. one `NNN <title>` per spec (`no specs landed since <tag>` when none did);
3. in bmesh's own repo, when the release's upgrade notes exist, a blank line and the notes as text: `Upgrading to vX.Y.Z (docs/upgrading/vX.Y.Z.toml):`, the summary, what a start-over loses, one numbered line per step (see Upgrade notes).

`release` prints `released <repo> vX.Y.Z at <sha8> (NNN, NNN)` (`(no specs)`; ending `; no origin, so it is local only` without one; `--json`: `{"repo","tag","commit","specs","pushed"}`). A failed push deletes the local tag again, so the next run starts over.

**Refusals** (exit 3, tagging nothing):

- `nothing to release since vX.Y.Z: nothing under code changed` when no commit since the last tag changes the repo's code (its row's `code`). When that tag is on the code head, as a tag `release` cut is (the `done` commits after it change no code), it is reported, not cut again: `already released <repo> vX.Y.Z at <sha8>` (exit 0).
- `held: <family> is still running (...)` or `held: <family> has N feedback pending; ...` while a lead of the repo is not `done` or `blocked`, or a family of the repo's has feedback pending. It is the hold `build` reads (see Builds), with no family of its own: the coordinator's own family holds it too until its feedback is drained, and a lead of either pipeline holds it, so whichever pipeline goes quiet last cuts the release (feedback naming bmesh's own repo holds it no more than a build). A batch is released once, when it is done, never per lead.
- `main is red at <sha8> <subject>: verify fail t:<tree8> (...); release once main is green` and the tail of the output, when `verify` at the code head isn't ok. A result recorded for its tree answers, else the repo's `verify` kind runs there and records, as for `build` (a repo with no `verify` kind says so and goes on).
- In bmesh's own repo only (see The fixer; `land`'s scaffold check makes the same test; every other repo releases as above, with no key to turn it on):
  - `refused: 055, 057 have Upgrading steps and docs/upgrading/vX.Y.Z.toml is missing: distil them into it (the build skill's step 7), commit it to main and release again` when a spec landed since the last tag has steps in its `## Upgrading` section (any text, unless the section is empty or its first word is `nothing`, in any case) and the new tag's notes are not on the ref read;
  - `refused: docs/upgrading/vX.Y.Z.toml: <what doesn't parse>` when the notes exist but don't parse;
  - `refused: docs/upgrading/vX.Y.Z.toml names keys this binary doesn't know: [runner] foo` when an ask or a `when` names a key this binary's config or registry doesn't have.

  Notes are checked whenever they exist, steps or not.

**`--dry-run`** runs the same checks and prints `would release <repo> vX.Y.Z at <sha8> (...); --dry-run tags nothing`, a blank line and the tag's message, tagging and pushing nothing. A pushed tag is public: a bad release is followed by the next one, never moved.

A repo starts releasing with its first tag, which the operator or a lead cuts by hand (or with `release`). From then on the `build` skill ends each batch with `bmesh release <repo>` for every repo where the family landed code and that has a `v*` tag. A box on `[update] track = "release"` (the default) updates to the newest release (see Updates), so a finished batch reaches each box once; `track = "latest"` and `bmesh update --latest` follow the code head instead.

### Upgrade notes

In bmesh's own repo, each release's notes are `docs/upgrading/vX.Y.Z.toml`: the steps an install at the release before needs. The coordinator distils them from the `## Upgrading` sections of the specs the release carries (the `build` skill, step 7) and commits them to `main` before `release`. The file is TOML; any key not listed here is an error:

- `summary`: one line, what the release asks of an install;
- `start_over`: `true` when the steps uninstall the box and set it up again;
- `loses`: what a start-over loses;
- `[[step]]`, in order, each with a `kind` and an optional `when`:
  - `kind = "ask"`: a key to set. `file` is `config` (the default; `key` is `[section] key` in `config.toml`) or `registry` (`key` is a row key in `repos.toml`, every row); `changed`, one line on what changed; `question` (required); `choices`, a list; `default` (required, and one of `choices` when given). No `command`, `os`, `confirm` or `expect`.
  - `kind = "run"`: `command` (required), `why` (required), `os` (a list of `linux`, `darwin`, `windows`; none means every OS), `confirm = true` for anything destructive, which asks first.
  - `kind = "check"`: `command` (required), `expect` (what its output must show), `why`, `os`.
  - Run and check steps take no `key`, `question`, `choices`, `default` or `changed`.
- `when`: comparisons that must all hold for the step to apply, `[section] key == "value"` or `!=` (the value quoted, or a bare word), joined by `&&`, read from this box's `config.toml` just before the step; a key the file doesn't set reads as its default, so a step after `init` sees the new config.

```toml
summary = "Boxes follow releases; restart the hub."

[[step]]
kind = "ask"
key = "[update] track"
changed = "boxes now update to releases, not every landing."
question = "Follow releases or every landing?"
choices = ["release", "latest"]
default = "release"

[[step]]
kind = "run"
command = "systemctl --user restart bmesh-hub"
why = "the hub runs the old binary"
os = ["linux"]
when = '[hub] role != ""'
```

## The ledger

A repo's specs live in `docs/proposals/NNN-slug.md` (`[spec] glob`), each a flat `key: value` frontmatter between `---` fences (lines starting with `#` are ignored) over a half-page body. `README.md` is not a spec.

```
bmesh spec list [--repo R] [--status S] [--json]
bmesh spec next-id [--repo R]
bmesh spec set --id NNN --status S [--repo R] [--from A,B] [--field K=V]...
bmesh spec template [--repo R]
bmesh spec steps [<NNN>] [--repo R] [--tick N,N [--note W] | --check [--wait D]]
bmesh choose-ideas <repo> "<reply>"
```

The frontmatter is the whole record, so an unreadable file is an error for every command, never skipped or counted around: a missing or non-numeric `id`; an `id` that does not match the file name's; a `status` outside `proposed`, `accepted`, `adjusting`, `later`, `rejected`, `building`, `blocked`, `done`; a `judge` outside `fable`, `opus`, `sonnet`; missing fences or a line that is not `key: value`; two files sharing an id.

**Fields the mesh writes.**

- `branch`: set by dispatch, corrected by the build thread when the runner renamed it. The `spec set` that writes `done` and `merged` adds `--field branch=<the branch landed>` when it differs, so the ledger names the branch merged.
- `family`: the build family it was dispatched in, set by dispatch with `building`.
- A spec a coordinator wrote from its family's feedback carries `from_feedback: <family> (<the leads whose feedback it came from, comma-separated>) #<seq>` (several issues comma-separated, each with its stream sequence; see Feedback). A lead built from one may leave feedback, but its family's coordinator writes that up as `proposed`, never straight to a build.
- A fix the fixer accepted also carries `ruled: [<the rulings' slugs>]` and `fix_of: <the reporting lead's spec>`, which `build` checks (see Builds and The fixer).

`--repo` is a registry name or a path (default the current directory; a path that is a row's path counts as that row, as for every command taking a repo).

- `list` prints one row per spec, `NNN  status  size  kind  lens  title` (`?` for a missing field), sorted by id; `--json` prints each spec's frontmatter plus `path` as an array.
- `next-id` is one past the highest id (`001` for an empty ledger).
- `set` rewrites the spec's status and the given fields in place (an existing key keeps its line, a new one is added last, every other byte stays) and prints its row. Refused (exit 1, changing nothing): with `--from`, a spec whose status is not one of the listed ones (naming its status); a write the ledger would read back otherwise (a duplicated `status:` key); a value with a line break.
- `template` prints the half-pager (the repo's `.bmesh/briefs/half-pager.md` when it has one, else the embedded one). In bmesh's own repo (its `go.mod` declares this binary's module) it adds an `## Upgrading` section before `## Needs from the operator`, unless the text has one: the steps an install from the previous release needs, written for any fleet, or `nothing`; a new config key is written as an ask (what changed, the question, the choices and the default). `release` reads it (see Releases).

#### Steps for the operator

When `bmesh land` lands a spec (step 5 of Landing), it reads its `## Needs from the operator` section as a list and copies it into the frontmatter as `operator_steps`, so what a landed change asks of the operator stays on the mesh until done.

- Each top-level `-`, `*`, `+` or numbered (`1.`, `1)`) line is a step; the lines under it, indented or running on with no blank line between, are part of it (a nested list too).
- A step may start with a box in brackets, `[<host>]` (lowercase letters, digits and `-`). With `[hosts]` on and a roster read, a word that names no roster box nor this one stays in the step's words, so `[optional] ...` is not a box. A markdown checkbox `[ ]` or `[x]` in front is dropped.
- A step may end with `check: <command>` (backticks around the command dropped), whose exit 0 says the step is done.
- A paragraph of prose is a step of its own, except one ending in `:` that leads straight into a list (`On the Mac:`): its words go in front of each of that list's steps, and its box, if it names one, counts for the steps that name none.
- A section that is absent, empty, or whose first word is `nothing` or `none` has no steps and gets no `operator_steps` (a spec landed again with steps from before gets `operator_steps: []`, so they go).

The value is JSON on one line, `[{"n":1,"box":"<host>","text":"...","check":"...","done":"","note":"..."}]` (`check` and `note` only when set), `done` empty until the step is ticked and then `<RFC 3339 time> <box>`. A spec landed again keeps the ticks of its steps whose box and words are unchanged, each old tick at most once.

**Reading.** `steps NNN` reads the spec from `main` (`origin/main` after a fetch) and prints `NNN <title>: X of Y open`, then each step as `<n>. [<box>] <text> (check: <command>) -- done <time> <box>: <note>`, the parts it has; a spec with none prints `NNN <title>: no operator steps`. With no id it prints one line per landed spec with open steps, `operator: <repo> NNN <title>: X of Y open (bmesh spec steps NNN --repo <repo>)`, as `mesh-status` ends: in `--repo`'s ledger, else in every registered repo with a checkout on this box, read from its local `origin/main` (which is fetched and read again when the repo has open steps, so a step ticked from another box drops out).

**Ticking.** `--tick 2,3` records `done: <time> <this box>` (with `--note`, the words as `note`) on those steps and lands it on `main` as `choose-ideas` lands a review: a worktree `<repo>-steps-<hex8>` from a levelled `main`, a commit `Steps: NNN 2, 3 done`, a fast-forward, a push. It prints `landed <sha8> on main: Steps: NNN 2, 3 done; X of Y open`. A step already done is named (`step 2 already done <when>`) and left as it is; when every one named is, nothing lands. A number past the last step is refused (exit 2), as is a spec with no steps (exit 3).

**Checking.** `--check` runs the check of each open step that has one: here, in the repo's checkout, when the step names no box or this one; otherwise as a `check-step` job to the step's box (see Hosts), waiting `--wait` (default 30s).

- It prints `<n>. <box>: exit 0` or `exit <code>: <the output's last line>`, `timed out after 2m0s`, a refusal, or, for a box that hasn't answered, `asleep` or `no answer in <wait>` with `; run --check again once it answers`.
- It ticks each step whose check exited 0 with `done: <time> <the box that ran it>` and the note `check passed`, one landing per box. When asking the other boxes fails (no hub, a box not joined), the passes here still land and the failure is reported after them.
- With `[hosts]` off a step of another box is not run (`not run: hosts are off here; run bmesh spec steps NNN --check on <box>`).

Open steps block nothing: `done` and `release` never wait on them.

#### The operator's reply: `choose-ideas`

`choose-ideas` takes the operator's reply to a round, typed in plain words: `"1 and 3 yes, 2 smaller, 4 no, 5 later, 3 on opus"`.

**Clauses** are split at commas, semicolons and line breaks and, once a clause's phrase has begun, at a number that a spoken verdict follows (`3 yes and 4 no` and `11 smaller and 12 no` are two clauses each; `4 no because 5 covers it` is one). A clause is its leading numbers (joined by `and` or the like), which name specs, and the phrase after them.

**Numbers.**

- Three or more digits (`012`, as ids are written) name that spec.
- One or two digits are a position in the pitch when there is one, else a spec id: `1 and 3 yes` answering a pitch of 012 to 016 means its first and third items; `45 yes` to a five-item pitch is reported as no position, so the spec is `045`; `6` to a pitch of 001 to 003 and 006 to 010, both a pitched id and the sixth item, is reported as ambiguous.
- `all` among the numbers names every spec of the pitch (`all yes`, `all later on opus`; with no pitch it is reported).

**The pitch** is the one the operator saw last: the text of the repo's newest settled round (`done` or `blocked`) on the mesh, read whole, line by line, so a position means the same spec in every reply to it, whatever has moved since (a line whose spec has moved on is reported as left unchanged).

- A line is the id it starts with. One holding two or more items of the shape `NNN title (kind, size, judge):` after sentence ends, the title free of sentence ends (a `.;!?` before a space; `v1.2` is fine), is a list typed on one line and gives them all, so each child's pitch reads the same typed one per line or in one go. An id cited inside a why is not an item.
- An open round, even with a finished child, is not a pitch yet; a settled round whose text names no spec (one blocked before a pitch) is passed over for an older one.
- With no mesh or no settled round with a pitch only ids count, and the output's first line says so.

**The phrase's first word decides.**

| First word | Moves the spec to |
|---|---|
| `yes`, `accept`, `ok` | `accepted` |
| `no`, `reject` | `rejected`, the rest of the phrase as `reason` |
| `later` | `later` |
| `on fable\|opus\|sonnet` alone | nothing: it sets `judge` |
| any other | `adjusting`: the whole phrase is an instruction, recorded as `Adjust (<date>): <phrase>` under its Proposal heading, for the thread that reworks the body |

Reasons and instructions are kept as written, so a verdict word or number inside one is just a word (`2 make it work with no network` adjusts 002). `on <judge>` right after `yes` or `later` sets the judge as well; words after that (`yes but smaller`) make it an adjustment.

**A verdict said first** is `yes`, `accept`, `build`, `go`, `approve`, `no`, `reject` or `later` (optionally followed by `to`), or `work on`, then its numbers and, for `reject`, the reason (`accept 012`, `reject 2 too big`, `work on 1, 2 and 3`, `yes to all`).

- It carries to the clauses of bare numbers after it.
- A phrase after its numbers that is itself a verdict said first is a clause of its own (`accept 012 and reject 013`).
- After two or more numbers, a phrase that is not the verdict's own (after `reject`, one starting with a spoken verdict; after `yes` or `later`, anything but `on <judge>`) is the last number's, and the verdict covers the others (`reject 2 and 3 yes`, `accept 012 and 3 smaller`; `reject 2 and 3 too big` rejects both, `yes 1 and 3 on opus` accepts both on opus).

**What moves.**

- A clause of bare numbers that no verdict said first carries to takes the verdict of the clause right after it (`1, 2 and 3 yes`); a clause with no number in between leaves them unmatched.
- A spec named in several clauses gets the last move and every judge.
- Only `proposed`, `later` and `adjusting` specs move; another status is reported and left alone.
- A clause with no number, no phrase, or a number matching nothing is printed as `unmatched: ...`, never guessed at.

**Landing.** The command:

1. levels `main` (as `ideate` does, see Proposal rounds);
2. makes a worktree `<[runner] worktrees>/<repo>/<repo>-review-<hex8>` from it and applies the verdicts there;
3. checks the commands accepted specs define (below);
4. commits `Review: 012 accepted, 013 later`;
5. fast-forwards `main` (`merge --ff-only` in the main checkout when it is on `main`, else `fetch . <branch>:main`) and pushes to origin;
6. removes the worktree and its branch.

It prints one line per spec (`NNN title: old -> new [on judge] [(note)]`), the unmatched items, the warnings below and `landed <sha8> on main: Review: ...` (or `nothing to land`). A `main` that will not fast-forward is refused (exit 3), the review kept on its branch.

**Commands an accepted spec defines.** Before committing, `choose-ideas` checks them for each spec it turns `accepted`.

- A spec defines one where its Proposal has a bold run that is a bmesh synopsis and nothing else: ``**`bmesh release <repo> [--minor]`**``, optionally closed inside the bold by `.`, `:`, `,` or `;`. The command is the synopsis's leading bare words, so `bmesh spec verified [--commit C]` defines `spec verified`.
- A bold run that goes on in words (``**`bmesh build` repeats the warning**``) talks about a command, a plain code span mentions one, and a bold run with a backtick right outside it is quoted inside a code span: none defines it.
- A synopsis going straight to a flag (`bmesh build --hold land|now`) adds a flag; flags aren't checked.

Each is compared with `main`'s (the ones this binary's `bmesh help` lists; on an up-to-date box, the binary built from `main`) and with every other spec `accepted`, `adjusting` or `building` once the review is applied. A match prints `warning: 048 defines bmesh release, which 046 (accepted) also defines` or `warning: 046 defines bmesh version, which main already has`, after the unmatched items. Two specs accepted in one reply defining one command warn once, from the higher id. The reply still lands; the operator decides. A spec changing a command on purpose warns at every accept, naming `main`. `build` repeats the warnings when it dispatches the spec (see Builds).

## Executors

A lead fans out by making executors: subagents in worktrees of their own, on branches under the lead's, each a node under the lead's path.

```
bmesh executor new --name N [--base REF] [--model M] [--json]
bmesh executor list [--json]
bmesh executor remove <name> [--force]
```

**`new`** runs as the lead (or as an executor fanning out again; the caller is the parent) and:

1. makes `<[runner] worktrees>/<repo>/<parent path>.<name>` with `git worktree add -b <parent's branch>-<name>` from the parent's HEAD (`--base` picks another commit; the parent's branch is its worktree's current one, so a renamed branch is followed).
2. registers node `<parent path>.<name>` (`role: executor`, `parent`, branch, worktree, repo, host, model) with an inbox and state `idle`.
3. prints `{"path","branch","worktree"}` followed, unless `--json`, by the executor header, the paragraph to paste at the top of the subagent's prompt. It names the path, worktree and branch, and says to:
   - start every shell command with `cd <worktree> &&` and carry `--node <path>` on every `bmesh` command (the harness keeps neither working directory nor environment between tool calls);
   - run `bmesh started` first;
   - report the finished piece with `bmesh done --commit <sha> --text "<one line>"` alone (the mesh puts it in the lead's inbox with the branch);
   - invoke the `buildermesh:executor` skill.

A name matches `^[a-z0-9][a-z0-9-]*$`. A second `new` with a name whose node is not `done` or `blocked`, or whose branch still exists, is refused (exit 3). When `git worktree add` fails (a hook, an unwritable root), nothing is registered and the worktree and branch are removed.

**`list`** prints the lead's tree (every node under the lead): path, state, branch, head (when the worktree is on this host) and unread count.

**`remove`** refuses (exit 3) while the branch is not merged into the caller's HEAD unless `--force`, then removes the worktree (`--force` also drops uncommitted changes) and the branch (`-d`, or `-D` with `--force`), forgets the worktree in the node, drains its inbox and marks it `done`. The node and its branch name stay in `bm-nodes` until `bmesh node forget` (see Finished nodes); the name can be used again.

**What reaches the lead.** An executor's `ask`, `blocked` and `done` reach its parent's inbox as well as being published as its own events, since only leads reach the operator, and the lead acts on its executors from its inbox; `started` and `progress` are events only.

- `ask`: a message, `ask from <path>: <text>` (id `ask-<path>-<hash of path and text>`);
- `blocked --text T`: a message of kind `report`, `blocked from <path>: <T>` (id `blocked-<path>-<hash of path and text>`);
- `done --commit SHA [--text T]`: a `report`, `done from <path>: branch <b> at <sha8>: <T>` (id `done-<path>-<sha>`).

With no text, each ends before its `: <T>`.

- The branch is the executor worktree's current one (`rev-parse --abbrev-ref HEAD`, so a renamed branch is followed), the recorded one when the worktree is detached, gone or on another host; the sha is `--commit` resolved to a full commit in the worktree, else as given.
- The id is the event itself, so a retry within the stream's two-minute duplicate window is one message.
- A report carries `{"event","from","branch","commit"}` (branch and commit for `done`) in its `report` field, which `inbox --json` prints.
- The message is sent after the move is checked against the node's state and before the event, so a refused event sends nothing.
- The digest lists an executor's ask, silence, block or completion nested under its lead's line and counts only leads under "asking".
- An executor's `done` needs `--commit` and never reads the spec.

### The skills

The repo is a Claude Code plugin, `buildermesh`, with twelve skills. Seven are the operator's chat entry points: to the lifecycle, `ideate`, `choose-ideas`, `build`, `scaffold` and `mesh-status` (see Builds); across boxes, `sync` and `prune` (below). Five are followed by the nodes the lifecycle starts:

- `fix`: bmesh's fix lead: list the feedback naming the repo, rule on each issue under its brief's rulings, land each as a spec and drain it, build the fixes one at a time, release, report.
- `lead`: own one spec, decide the shape, executors, checkpoints, merge, verify with one `review` pass, land, feedback, ask.
- `executor`: your worktree and branch only, checkpoints, routines, report, ask goes to the lead, never merge or push, except pushing its own branch to queue a kind of another OS.
- `ideate-engineer` and `ideate-ux`: a round's method: what to read, the lenses and measurements, screenshots of the row's surfaces for the UI/UX engineer, three to five ranked half-pagers, landing on `main`, `done` with the ranked list, and the operator's reply through `bmesh choose-ideas`.

**`buildermesh:sync`** ("sync my repos", "sync X") shows first what `bmesh sync` would do, which has no dry run of its own: this box's checkout against a fresh fetch of origin (up to date, behind, ahead, or what will refuse it) and, from `bmesh hosts --json`, the boxes that would get a `level`, their state and which are secure. Then it runs `bmesh sync --all` or `--repo R` and reports per box what moved, what is held for approval, what was refused and what is still to answer.

**`buildermesh:prune`** ("clean up worktrees") runs `bmesh prune` dry first, shows the keep and remove rows per box, runs it with `--apply` and reports per box; on a bmesh without `prune` it says prune is not there yet. Both stop, saying so, on a box with `[hosts]` off.

The node skills name optional pstack and method skills at fixed moments, each with a written default for a session without it (see Quickstart):

- the lead opens in `poteto-mode`, plans an L spec with `method:writing-plans`, decides the shape with `architect` and `attack-the-premise`, checks a browser surface with `control-ui`, runs `figure-it-out` before an ask and `blast-radius` before landing;
- the executor builds with `tdd` and runs `figure-it-out` before an ask;
- the round's engineers run `exhaust-the-design-space` and `attack-the-premise` before ranking.

**Install** it from the repo:

```
claude plugin marketplace add rickdiculousli/buildermesh && claude plugin install buildermesh@buildermesh
```

or from a checkout: `claude plugin marketplace add <path to the checkout> && claude plugin install buildermesh@buildermesh`.

**The hook.** The plugin also carries one hook, `hooks/hooks.json`: at every session start, when `bmesh` is on PATH, it runs `bmesh hosts --here` and then `bmesh update --notice` (with its stderr dropped, so a binary older than the plugin says nothing). So on a box with `[hosts]` on a session opens with the line saying which box it is on (see Several boxes), and on a box whose binary is behind `main` with the notice (see Updates); it prints nothing else.

A lead's brief says "follow the `buildermesh:lead` skill" and adds the operator's own conventions; the executor header names the executor skill; a round's briefs name the ideate skills. The skills name no repo convention; `docs/agents-snippet.md` is the paragraph a repo's AGENTS.md gets, embedded in the binary for `bmesh scaffold` and kept in the repo for reading.

## Finished nodes

```
bmesh mesh-status [--all] [--json]
bmesh node forget <path> [--worktree [--wait 30s]]
bmesh node stop <path> [--text "<why>"] [--wait 30s]
```

**`mesh-status`** prints nodes sorted by path: path, role, state, since, runner and unread count.

- A node `done` for longer than `[status] keep_done` (default `24h`; `0` shows them all) is left out, and a closing line counts what was: `… N finished nodes older than 24h (--all shows them)`.
- A `blocked` node is never left out: it waits on the operator.
- A round and its children are one unit, left out only all together.

Then, in order:

1. each hold, `<path> held before land by <by> <age> ago: <why>` (see Holds);
2. the open issues naming bmesh's own repo, as one line: `fixes: N pending for <repo> (<fixer> working)` or `(starts within a minute)` (see The fixer);
3. per build family with other feedback not yet drained: `feedback: N pending in <family>` (see Feedback);
4. with the `t3` runner and this box's T3 sign-in under 7 days from expiry, `t3 sign-in on <host>: expires in N days (bmesh t3 login)` (see T3 sign-in);
5. the behind-main notice (see Updates);
6. the hub line when this box's `bmesh hub` runs an older binary than its `bmesh serve` (see Several boxes);
7. last, one line per landed spec with steps for the operator still open: the `operator: ...` lines `bmesh spec steps` prints with no id, read the same way (see Steps for the operator under The ledger).

`--all` lists every node; `--json` carries every node with no flag, as the map's `/api/nodes` does: an array of `{"node","state","runner","unread"}`, a held node's with `hold`.

**Routine nodes** leave the mesh by themselves; every other node stays until forgotten. A fresh run's node, state and runner sample are deleted once its run ends and its checkout is removed; its `started`, `progress` and `done` events stay in the stream for the map, and its reply's `by` still names it. A pooled instance that stops (its pool restarted or shrunk, its kind gone from its repo's main, its repo gone from the registry) is marked `done`. The daemon's kind rescan, once a minute, deletes the node, state and runner sample of every routine instance on its host that has been `done` for `[status] keep_done` (`0`: never) and is not running, a fresh run's left behind included.

**`node forget`** deletes a node's `bm-nodes` entry, state, runner sample and inbox consumer, printing `forgot <path>`; its events stay in the stream, and nodes under it aren't forgotten with it. It takes:

- a `done` or `blocked` node;
- a round whose children are all gone (forgotten before the daemon settled it, which leaves nothing to settle it from), whatever its state;
- an `idle` node that never reported and whose thread isn't running, as a lead whose session died before its first event leaves behind.

An idle node is refused (exit 3) when:

- it published an event of its own (one in `bm-mesh` whose `from` is its path): `refused: <path> is idle but published <event>; only an idle node that never reported is forgotten`;
- its `host` is not this box or its `runner.kind` is not this box's runner: `refused: <path> is idle and names no host`, `refused: <path> is idle and its thread is on <host>; run it on <host>`, `refused: <path> is idle and its runner is "<kind>", not this box's <runner>; run it on <host>`;
- its runner, asked once for `runner.thread`'s status, doesn't report it known and not running: running (`refused: <path> is idle and its thread <thread> is running; stop it first`), can't tell (tmux) or names no thread (`refused: <path> is idle and its thread may be running; stop it first`), or fails (the same, with the error in parentheses before `; stop it first`).

A lead between its runner start and its first `started` is idle with no events too; the status check keeps it. Any other state is refused (exit 3, `refused: <path> is <state>; only a done or blocked node is forgotten`), as is a node with a node under it (a lead's executors, a round's children) that is not `done` or `blocked` (`refused: <child> under <path> is <state>`).

**Worktrees.** Without `--worktree` nothing on disk is touched, and a node with a worktree prints `worktree <path> kept`. `--worktree` first removes the node's worktree (`git worktree remove` in its repo; one already gone from disk is pruned) and deletes its branch (the one checked out there, so a branch the runner renamed is followed; else the node's), printing `removed worktree <path>` and `deleted branch <b>`.

- For another box's node, with `[hosts]` on and this box joined, it sends a `forget-worktree` job to that box and waits `--wait` (default 30s), forgetting the node only when that box answers it removed the worktree (see Hosts: Another box's worktree); with `[hosts]` off it is refused (`run it there`).
- It is refused (exit 3, naming it, nothing touched) when the worktree is dirty (`git status --porcelain` prints anything) or the branch is not merged into `main` (an executor's: or into its parent's branch, while that exists).
- Merged means the branch's tip is an ancestor, or every commit of it has an equivalent there (`git cherry`), as after a rebase before landing replayed them. The merged branch is deleted with `-D`, since `-d` would also want it merged by ancestry into its upstream.

### Stopping a node

`node stop <path>` stops a node and keeps it stopped. It stops the nodes under it first, deepest first (a round's children, a lead's executors; those already `done` or `blocked` are left as they are), then the node itself; for each:

1. It is written `blocked` with `stopped by <who>: <why>` (`stopped by <who>` without `--text`), and `blocked` is published, as the node's own would be (a lead's pushes).
2. Then its thread is stopped through its runner (`stop`: T3's `t3_thread_interrupt`, claude's turn and queue, tmux's Escape; a thread the runner no longer has counts as stopped), printing `stopped <path>` for each.

Written first, the daemon's poll finds the node `blocked` and never puts its own stop over the reason; a thread that reported between the two is written again. `<who>` is `--node`, else this node, else the host name. A node with no thread (an executor) is only written.

- A failed interrupt exits 1 with the node already marked (`... (it is marked stopped; run node stop again to stop its thread)`).
- Another box's thread is stopped there by a `stop-thread` job (see Hosts: Another box's thread), all of them sent at once after the threads here and answered within one `--wait`, printing `<box>: stopped <path>`. With `[hosts]` off it is refused before anything is stopped (exit 3, `refused: <path> runs on <host>; run it on <host>`), as is a thread whose runner is not this box's (`refused: <path>'s runner is "<kind>", not this box's <runner>; run it on <host>`).
- A round is then settled from its children as the daemon settles it (see Proposal rounds), printing `<round> blocked`; one whose children are all gone is written `blocked` directly with the same text, and one whose children on an asleep box aren't stopped yet prints `<round> stays <state> until its children are stopped` (the daemon settles it once they are).
- A node already `done` or `blocked` prints `already <state>` and exits 0, stopping nothing, but for a stopped node's thread on this box, which is interrupted again (an idle thread takes it as nothing). A stopped node is `blocked`, so `node forget` takes it.

**A stopped node** is a `blocked` one whose text starts `stopped by ` (`node stop`) or `stopped in ` (an interrupt in its runner). The daemon never wakes it for silence, a report, a reply or a message from a node under it. Only a message sent with `bmesh send` from outside its tree after it was stopped (its `at` later than the stopped state's, both to the second) wakes it, which is how one is resumed, so a message waiting from before the stop (a STOP sent while the thread ran) doesn't undo it. The wake reads `bmesh: resumed by <sender>; 1 unread: message from <sender>. Run ...` (see Config: Wake text). It stays `blocked` until it reports again.

**An interrupt in the runner is a stop.** When the runner reports the newest run of a lead's (or engineer's) thread ended by the user (T3: the newest run `interrupted`; see Runners), the daemon's minute poll writes the node `blocked` with `stopped in <runner> by the operator` (from `idle` through `working`) and publishes that `blocked` once. A node already `done` or `blocked` is left as it is.

- Its id is the run, and the runner sample keeps the run (`stopped_run`), so the same run never marks the node again, while a later interrupted run does.
- It is never woken for silence meanwhile, so interrupting a thread in T3 is enough to stop it.
- A runner that can't tell (tmux, claude) keeps the old behaviour: an interrupted thread there is woken for silence; stop it with `node stop`.

## Routines

A routine runs one of a repo's checks against a commit: a shell command or a headless `claude -p` session. A kind says how; a job asks for one run; a result records it by tree.

### Kinds

A kind is `.bmesh/routines/<kind>.toml` (the directory is `[routines] dir` in the repo's `.bmesh/config.toml`), named by its file name, which matches `^[a-z0-9][a-z0-9-]*$`.

| Key | Default | Meaning |
|---|---|---|
| `mode` | `"pooled"` | `inline`, `fresh` or `pooled` (see Modes) |
| `os` | `""` | the OS a box needs, the word `bmesh hosts` prints (Go's `GOOS`): `darwin` (macOS), `linux` or `windows`; `""` is any box. Only that OS's daemons take its jobs (see Another OS) |
| `deterministic` | `false` | same tree, same result: a recorded result answers a repeat job |
| `slots` | `[]` | slots (from the repo's `[slots]`) a run leases |
| `timeout` | `"20m"` | a run's whole process group is killed after this |
| `command` | `""` | run by the platform shell in the checkout at the commit, a job's args appended as positional arguments (`"$@"` under sh) |
| `model` | `""` | a model id: the kind runs `claude -p` with it |
| `models` | `[]` | several model ids; a job's `--model` picks one, default the first |
| `prompt` | `""` | model kinds: setup prompt, sent once per session before the first job |
| `allow` | `[]` | model kinds: extra `--allowedTools` entries, e.g. `"Bash(npm test:*)"`; `Read`, `Grep`, `Glob` and `LS` are always allowed |
| `pool` | `1` | pooled: instances per model; fresh: runs at once |
| `batch_idle` | `"30s"` | pooled: start on held jobs after this long with no new one and no go |
| `recycle_uses` | `20` | pooled model: new session after this many jobs |
| `recycle_context` | `120000` | pooled model: new session when the last reply's context tokens (input + cache read) exceed this |
| `recycle_idle` | `"55m"` | pooled model: new session after this long idle (the prompt-cache TTL) |

- Exactly one of `command`, `model` and `models` is set; an unknown key is an error.
- An `os` other than the three is refused, `kind <k>: os "<v>": want darwin, linux or windows (macOS is darwin)`; so is `os` on an inline kind, which runs in the requester's own worktree, so can't move.
- `os` states a fact about the work ("this build needs macOS"), true on any fleet; no key names a box.
- A command has trailing spaces, newlines and `;` trimmed; its last line may not end in a comment (the appended `"$@"` would be commented out).
- A model id may not contain spaces or `;` (it must fit a `verified` entry); `result record`, `routine queue` and `routine run` refuse such a `--model`, exit 2.
- A recycle key of `0` is off.

**A command's environment** has `BMESH_JOB`, `BMESH_KIND`, `BMESH_COMMIT`, `BMESH_TREE`, `BMESH_FROM` (the path the job runs as: the instance, or the requester for an inline run) and `BMESH_REPLY` (the requester's path).

**A model kind** gets the job on stdin as a header line (`job: <id> commit: <sha> tree: <tree>`), the job's `--text` and its args, and runs `claude -p --model M --output-format json --session-id|--resume <uuid> [--effort E] --allowedTools <list> --max-turns 40`.

- `--effort` only when the job's `--effort` set one; each job of a pooled session runs at its own. `queue` and `run` refuse (exit 2) an effort outside `low`, `medium`, `high`, `xhigh`, `max`, and any effort for a command kind.
- A run is ok unless the reply's last `VERDICT:` line says otherwise (`VERDICT: ok` or `VERDICT: fail`), or claude errs. Markdown around it is ignored: leading `*`, `_`, `#`, `` ` ``, `>` and spaces, and the same around the value, so `**VERDICT: fail**`, `**VERDICT:** fail` and `VERDICT: **ok**` all read.
- When the kind's prompt contains `VERDICT:` (asks for one), a reply without a verdict line fails; otherwise it passes by claude's exit code.

A run's output is stdout and stderr (a command) or the reply text (a model).

`examples/routines/`:

- `verify.toml`: `mode = "inline"`, `deterministic = true`, `command = "npm test"`.
- `review.toml`: `mode = "pooled"`, `models = ["claude-opus-5-5"]` (every change is reviewed on Opus, in a session that never saw it), `allow = ["Bash(git diff:*)", "Bash(git log:*)"]`, and a prompt that:
    - reviews `git diff <base>..HEAD` (the job text carries `base: <sha>`, e.g. `--text "base: $(git merge-base origin/main HEAD)"`);
    - asks for findings as `### <severity>: <what breaks>` sections ending in `VERDICT: ok` or `VERDICT: fail`;
    - says verify already ran tests, vet and formatting at that commit, so the reviewer reads the code instead of running them.
- `surface.toml`: `mode = "fresh"`, `deterministic = true`, `slots = ["browser"]`, `command = "npx playwright screenshot"`; the job args `<url> <file>` are appended as `"$@"`; a commented `os = "darwin"` line shows how a check needing a Mac (Safari, say) says so.

### Modes

| Mode | Runs | Where |
|---|---|---|
| `inline` | `bmesh routine run` in the requester's own process; no daemon | the requester's worktree, at HEAD |
| `fresh` | the daemon starts a new instance per job, `routine.<kind>.f<id8>`, and removes its checkout and node afterwards | a checkout of its own |
| `pooled` | the daemon keeps `pool` instances per model, `routine.<kind>.<host>-<model-short>-<n>` (`<host>-cmd-<n>` for a command), that drain the kind's inbox in batches | one checkout per instance, kept |

- `id8`: the job id's first 8 characters, lowercased.
- `model-short`: the model id lowercased, a leading `claude-` stripped, every character outside `[a-z0-9]` made `-`, runs of `-` collapsed, leading and trailing `-` trimmed (`claude-opus-5-5` is `opus-5-5`, `claude-opus-5-5[1m]` is `opus-5-5-1m`).
- `host`: the box's host name (`[host] name`, else the short host name, lowercased) as a path token, `.` and `_` written `-`, so each box's instances are nodes of their own (`routine.review.k16-opus-5-5-1`, `routine.review.ricks-macbook-opus-5-5-1`).
- `n`: pooled instances are numbered from 1 per model across this box's repos defining the kind.
- An instance's checkout keeps the host-less name, `<checkouts>/<repo>/routine.<kind>.<model-short>-<n>`, as checkouts are local to the box.

**A pooled instance.** It:

1. holds the jobs it takes until `batch_idle` passes with nothing new, or a go arrives (`bmesh routine go <kind>`, or a job queued with `--go`);
2. runs them in order;
3. once started, runs any job queued meanwhile at once (no hold).

**A model instance** keeps one session across jobs (`--session-id` on its first job, `--resume` after), starting a new one when `recycle_uses`, `recycle_context` or `recycle_idle` is reached, or the kind's definition at a job's commit differs from the one its session started with. A run whose output doesn't parse as claude's JSON result (including a non-zero exit with no JSON), first or resumed, drops its session whatever the recycle settings: the next job starts a new one with the prompt.

**Instances are nodes** (`role: routine`, with repo, host and model). The daemon writes their state, `working` during a job, `idle` between, `done` when it stops (a fresh run after its job, a pooled one when its pool is stopped or restarted; see Finished nodes for when their nodes leave), and publishes on `bm.<instance path>.<event>`:

- `started`;
- `progress`: `job <id> started` before a run and `job <id> ok|fail <ms>` after it (`job <id> ok|fail <ms> ctx <tokens>` for a model instance, its context tokens), both with `to` = the requester's path; or `recycled`;
- `done`.

A node event with a `to` counts as activity of that path too, so a lead waiting on a routine isn't marked silent.

**The map** draws one box per kind (every kind a configured repo defines, and any other kind with instances), not per instance. Its second line counts `N queued · N busy · N idle · N spent` (queued the kind's depth, busy its `working` instances, idle its `idle` ones, spent its `done` ones of either mode), zero counts left out, or shows its mode (`inline`, `fresh`, `pooled`) when every count is zero. A busy kind pulses as `working`; the box links to the routines page.

**The routines page** has a card per kind, its instances under a heading per repo: each row the instance's name without `routine.<kind>.` (the full path in its tooltip), `working` first, then `idle`, the `done` ones folded into one `spent (N)` row per repo that opens on a tap.

**Reading kinds.** Every minute the daemon reads each registered repo's kinds (see repos.toml) from its main (`origin/main` after a fetch when the repo has an `origin`, else `main`) and starts, restarts or stops pools to match.

- A registered repo whose path doesn't exist on this box (a row for another box) is skipped, with no kinds here, logged once per daemon start: `routines: repo has no checkout on this box; skipped repo=<name> path=<path>`.
- A kind whose `os` on main is set and isn't the box's own is left out: that daemon starts no feed or instances for it and writes no consumer settings, so never fetches its jobs.
- A repo whose kinds can't be read (no `main`, say) keeps what it had, logged once per repo and main commit, not every minute: `routines: read kinds repo=<name> err=<error>`.
- A box defers the jobs of a kind it can't judge for a repo to a box that can (see Running).
- Routines are off when `[runner] checkouts` is empty.

### Jobs

```
bmesh routine list [--json]
bmesh routine status [<kind>] [--json]
bmesh routine queue <kind> [--commit C] [--model M] [--effort E] [--go] [--rerun] [--again] [--text T] [-- args...]
bmesh routine go <kind>
bmesh routine wait <job id> [--timeout D]
bmesh routine run <kind> [--commit HEAD] [--model M] [--effort E] [--here] [--rerun] [--text T] [-- args...]
```

`list` prints the cwd repo's kinds with their instances and queue depth (waiting plus in-flight jobs).

`status` prints a kind's (or every kind's) instances with host (the box it runs on, so it shows which box did the work), state and model, then any unanswered jobs of the kinds, in queue order under `JOB KIND STATE HOLDER FROM`:

| State | Means | Holder |
|---|---|---|
| `waiting` | in the inbox: never taken, handed back, or held by a daemon that died until its `AckWait` passes | `-` |
| `held` | taken by a daemon, not yet running | `daemon.<host>` until a pooled instance takes it into its batch, then that instance, or a fresh run's path |
| `running` | being run | the instance |

A job is answered once the kind's consumer acked it, or its requester's inbox holds a reply to it. A daemon's entry not rewritten within the kind's `AckWait` is a dead daemon's; its jobs read `waiting`. `--json` carries each kind's instances as `members`, `[{"path","host","state","model"}]`, and jobs as `jobs`, `[{"id","from","state","holder","seq"}]`.

#### Queueing

A job is an inbox message (`kind: "job"`) to `routine.<kind>` with the job id as its `Nats-Msg-Id`, so one queued twice is stored once (`queue` prints `duplicate <id>`). It names a commit (`--commit`, default `HEAD`, resolved in the cwd repo to a full SHA with its tree) and a repo, the first of:

1. the requester node's `repo`, if a registered node naming one;
2. the registry name whose path shares the cwd's git common dir (so a linked worktree, where leads live, names its main repo);
3. else `queue` refuses (exit 3), `bmesh: <repo> is not in the registry`, naming the main repo directory's base name, as it refuses a node's repo the registry lacks: no daemon would run its job.

`queue` refuses an inline kind too (exit 3, `bmesh: <kind> is inline: run it with bmesh routine run`). The daemon resolves the repo through its own registry.

**The consumer** is `inbox-routine--<kind>`, shared by every box running the kind.

- `MaxAckPending` is -1 (no limit): each daemon fetches only while fewer of the kind's jobs than its capacity are in flight on it, its instance count for the kind (`pool` per model per repo for pooled, `pool` for fresh, at least 1, so a box whose repos can only refuse still answers).
- `AckWait` is the kind's longest `timeout` + 1m on the box that wrote it last. A daemon writes it when its feed for the kind starts and when its own value changes; on every other rescan it reads the consumer and, when its `MaxAckPending` reads anything else (an old daemon's count), writes `MaxAckPending` -1 back, keeping the `AckWait` it read.
- A daemon marks a job it holds or runs in progress every 20 s, a third of the 1m every box's `AckWait` adds to a timeout, so no box's `AckWait` outruns it and the job isn't redelivered while that daemon lives.
- A stopping daemon (SIGTERM or SIGINT, and before an update's re-exec) kills every running job's process group and hands every job it holds or ran back with a NAK, unanswered, so the next daemon takes it at once (a model job killed mid-run is paid for twice). Only a daemon that dies leaves its jobs unacked until `AckWait` passes.
- KV `bm-jobs` holds each daemon's unsettled jobs of a kind under `<kind>.<host>`, rewritten on every change and every in-progress heartbeat (every 20 s), emptied when it starts; `status` reads it for `held` and `running`.

#### Running

The daemon runs a job with the kind as defined at its commit (`git show <commit>:.bmesh/routines/<kind>.toml`), falling back to main's only when the file is absent there.

- A deterministic kind's job is answered from its recorded result, ok or failed, without a run unless `--rerun`; `queue` checks the record first and prints `answered from result <key> (job <id>)`.
- A commit not in the daemon's clone (queued from another box) gets one `git fetch origin` before the job is answered `not a commit`: push the branch before queueing to a daemon elsewhere.
- A job whose slot is busy goes back to the inbox for 30 s.
- A review is one pass per branch: `queue review` for a worktree whose branch already has a `review` result (recorded for a commit that is an ancestor of HEAD but not of `main`, `origin/main` when the repo has an origin, so a landed spec's review in the history doesn't count) is refused (exit 3) naming that result, its verdict, commit and time; only `--again` queues it, for code a rebase changed.

Answered `ok=false` with the reason (`routine wait` prints it and exits 1, as for any failed job):

- a kind that a configured repo defines but not the job's repo's main (`kind <k> is not on <repo>'s main (origin/main at <sha8>)`, the commit that box last read main at, `main at` for a repo with no origin);
- an inline kind;
- an unknown model;
- a commit whose kind runs on another OS (see Another OS);
- a lost lease.

So a lead that queues a kind only its branch has is told at once by any box with the repo.

**Deferred.** A daemon defers a job it can't judge instead, one for a repo:

- its registry doesn't name (it reads `repos.toml` only when `serve` starts, so restart `serve` after changing the registry);
- with no checkout (or no usable path) on that box;
- whose main failed to read and left no definition of the kind from an earlier read;
- whose main gives the kind another OS.

It:

1. hands the job back with a NAK delayed 2 s × 2^(n-1), at most 1m, less a random part of up to a quarter so redelivery doesn't fall in step with its own next fetch (`n` the job's delivery count);
2. sends no reply;
3. logs `routines: deferred job <id>: <repo> <why> here` once per job.

A box that can run the job takes it. A deferred job holds none of the box's capacity. A job only boxes without its repo see waits until `routine wait` times out, as does one of a kind no configured repo defines on its main (it has no feed).

**A go** is one message, which one box fetches: every box holding jobs of the kind looks in the stream for a go past its oldest held job (and the last go it acted on) after each empty fetch and, while its capacity is full, every second, so a go any box fetched starts every box's held batch.

#### Another OS

A kind with an `os` runs only on a box of that OS; its jobs wait on the hub while that box sleeps, and run when it wakes. A job runs on the `os` its commit gives the kind, else main's (the definition a daemon falls back to, which decides which boxes take the kind's jobs), whatever the working copy says.

When no recorded result answers a job, `queue` checks (after the record lookup, since a recorded answer queues nothing):

1. Boxes take a kind's jobs by main, but a job runs the kind as defined at its commit, so a branch could give a kind both boxes run an `os`. `queue` reads the kind at the job's commit (`git show`, not the working copy) and, when that sets an `os`, compares it with the kind on `origin/main` (`main` without an origin), refusing (exit 3) when:
    - main's differs: `bmesh: <kind> runs on darwin at <commit8> but anywhere on main, so a job could reach a linux box: land the os change first, or give it a kind name of its own`;
    - main lacks the kind (it has no feed anywhere): `bmesh: <kind> is not on main: land it first`;
    - main can't be read: `bmesh: cannot read kind <kind> on <ref>: <why>`.

    A commit with no `os` in the kind, or no kind file, isn't checked: it runs where main sends it.
2. For another OS than this box's, it refuses (exit 3) a commit on no `origin` branch (`git branch -r --contains <commit> --list 'origin/*'` prints nothing), `bmesh: push first: <kind> runs on darwin, which fetches the commit from origin`.

A job that passes both and runs on another OS prints `queued <id> (runs on darwin)`.

A daemon whose job's commit gives the kind another OS (queued by an old CLI, or main moved between queue and fetch) answers it `ok=false` right after loading that definition, before the record lookup and slot lease, `kind <k> at <commit8> runs on darwin; this box is linux`, and acks it; it is never run or recorded there. A kind name is one inbox across every repo and box, and a box defers jobs of a repo whose main gives the kind another OS, so kinds of different OSes may share a name.

#### Replies

A reply is an inbox message (`kind: "reply"`) to the requester carrying `{"job","kind","tree","ok","output","result_key","by"}`, the output cut at 64 KiB with a trailing `[truncated]` line.

`wait` reads the reply from this node's inbox subject in the stream, not its consumer: the subject keeps every message for the stream's 30 days, acked or not. It scans that history for the reply, then follows the subject until it arrives, prints its output and exits 0 when ok, 1 when failed, 5 on `--timeout` with `bmesh: timed out waiting for <id>; the job may still be queued or running, and its reply stays in the stream for 30 days: run bmesh routine wait <id> again rather than queueing it twice`.

- A reply `inbox` already read is found the same way, so `wait` works whenever it runs.
- A reply still unread is acked once found (one no-wait batch from the consumer that acks it and returns every other message unread), so no wake follows and `inbox` doesn't print it again; one further back than 100 unread messages stays unread and wakes the node once.
- `--timeout` defaults to 20m of queueing plus the kind's `timeout` plus the daemon's 1m beyond it when the cwd repo defines the kind the job was queued to (looked up by job id in the kinds' inboxes; 41m for a 20m kind), else 20m.

`bmesh inbox` returns what the inbox holds at once, without waiting, acks everything it reads (`--peek` leaves it unread), and prints a reply as one line, `<at> <from>: reply to job <id>: <kind> ok|fail, <n> lines; bmesh routine wait <id> prints it`; `--json` prints every message whole.

`queue`, `go` and `wait` need a node identity (`--node`, `BMESH_NODE` or the registered worktree), the reply's address. `run`, `lock-lease` and `result record` use the node when one resolves, else act as the host name; `list`, `status` and `result find` need none.

### Checkouts

Fresh and pooled runs never touch a requester's worktree: each instance has a detached worktree of the registered repo's path at `<[runner] checkouts>/<repo directory name>/<instance path>`, moved before each job with `git checkout --detach <commit> && git clean -fd`; the daemon reads the repo's main in a sibling checkout named `main`.

`routine run` is the inline run, of HEAD of the cwd worktree (`--commit` must name HEAD). It refuses:

- a fresh or pooled kind, unless `--here`;
- a kind whose `os` isn't this box's, `--here` included (exit 3, `bmesh: mac-build runs on darwin; this box is linux: queue it`): run here, it would record this OS's result as the kind's for the tree, answering every later job for that tree;
- a dirty tree, since every inline run records a result for HEAD's tree: exit 3 `bmesh: worktree is dirty; commit first`.

It prints the run's output, then its verdict line, `<kind> ok|fail t:<tree8>[ m:<model>] (recorded)`, or `(answered from the record)` when a deterministic kind's recorded result answered it (its recorded output printed above); the line is a `verified` entry, not part of the recorded output.

### Leases

```
bmesh lock-lease acquire <slot> [--ttl D]
bmesh lock-lease renew <key> [--ttl D]
bmesh lock-lease release <key>
bmesh lock-lease run <slot> [--ttl D] -- <cmd...>
```

A slot is a resource only so many runs may use at once, its capacity in the repo's `.bmesh/config.toml` (default 1):

```toml
[slots]
browser = 1
[routines]
dir = ".bmesh/routines"
```

- Leases live in KV `bm-leases`, key `<slot>.<n>` (`n` below the capacity), value `{"holder","expires_at"}`; an expired entry is free.
- `--ttl` defaults to 10m, at least 3 s.
- `acquire` prints the key (exit 3 when the slot is busy); `renew` and `release` refuse a lease held by someone else (exit 3).
- A holder renews every ttl/3; a failed renew loses the lease: the run is killed and the job answered `ok=false` with `lease lost` (nothing recorded; `lock-lease run` exits 1).
- `lock-lease run` runs the command in its own process group, kills the group and releases on SIGINT or SIGTERM, and exits with the command's code.
- Kinds lease their `slots` for `timeout` + 1m (inline runs and the daemon alike).

### Results

```
bmesh result find <kind> [--commit C] [--model M] [--json] [-- args...]
bmesh result record <kind> --ok|--fail [--commit C] [--model M [--base B]] [--output FILE|-] [-- args...]
```

Results live in KV `bm-results`, key `<kind>.<tree>[.<args8>][.<model-short>]`: `tree` the commit's full tree hash, `args8` the first 8 hex of the sha256 of the args joined by NUL (only with args), `model-short` as in Modes (only for a model). The record:

```json
{"kind":"verify","tree":"<tree>","commit":"<sha>","args":[],"ok":true,"output":"...","at":"2026-01-02T03:04:05Z","by":"app-012","model":"claude-opus-5-5","patch_id":"<sha1>"}
```

- `model` is omitted for a command kind.
- `patch_id` is the `git patch-id --verbatim` (`--stable`, whitespace kept, so a re-indented hunk is another diff) of the diff a model was asked to read, from the commit the job text's `base: <sha>` names to the job's commit (read with `git diff-tree -r -p --binary --full-index --no-renames`, so no user config changes it). Omitted for a command kind, a job text naming no base and an empty diff; a base that isn't a commit there records none (the daemon logs it, `routine run` says so on stderr).
- `output` is cut at 64 KiB.

Every run naming a commit records: inline and daemon runs, deterministic or not (a kind's newest run at a tree replaces the last). Answered-from-record jobs, refused jobs, lost leases and runs cut off by a stopping daemon record nothing.

`result record` stores a result by hand for any kind name, with or without a kind file. `--commit` defaults to `HEAD`; a result for HEAD is refused while the worktree is dirty (exit 3 `bmesh: worktree is dirty; commit first`). `--base B` records the patch-id of `B..<commit>` as a review's (it needs `--model`, exit 2 without; an empty diff is refused, exit 3).

`result find` prints `<ok|fail> <tree> <at> <by>[ m:<model>]` (exit 3 when none): with `--model` that model's result, without it every result of the kind at the tree with those args, one line per model (`--json`: one object per line). A `result` subcommand's usage error prints its own usage, flags included; `-h` on any subcommand names its flags.

A result older than `[routines] keep_results` (default `30d`, the stream's age; `0` keeps them forever) is gone: `bmesh serve` provisions `bm-results` with that age. A deterministic kind then runs again, and `bmesh done` for a spec landed longer ago needs its routines rerun on the merged tree (it names the missing result); the spec's `verified` line is the record that stays.

### `verified`

A spec's frontmatter carries one line of proof, `;`-separated, one entry per check:

```
verified: review ok t:3f2a9c1e m:claude-opus-5-5; verify ok t:3f2a9c1e
```

Each entry is `<kind> <ok|fail> t:<tree first 8>[ m:<model>][ c:<tree first 8>]`. `c:` names the tree a model's result was recorded at when it carries to `t:` across a rebase that left the diff unchanged:

```
verified: review ok t:3f2a9c1e m:claude-opus-5-5 c:81d0b7a4; verify ok t:3f2a9c1e
```

`bmesh spec verified [--spec NNN] [--commit C] [--write]` composes the line from the results recorded for the tree of HEAD in the cwd worktree, or of commit `C` (`--commit`; read below as HEAD; exit 2 when not a commit):

- One entry per (kind, model), the newest result winning, whatever its args, sorted by kind then model.
- A (kind, model) with no result there takes the newest model result whose `patch_id` equals HEAD's (the diff from HEAD's merge-base with `origin/main`, `main` without an origin, to HEAD), written `t:<HEAD's tree8>` and `c:<its own tree8>`: a review of the same diff on another base. A changed diff carries nothing, however small; a command kind's result (verify) never carries, being about its tree; nothing carries when HEAD is on main.
- Prints `verified: ...`, or exits 3 with `bmesh: no results for HEAD's tree; run the routines first` (`no results for <C>'s tree t:<tree8>` under `--commit`).
- `--write` sets the line in the spec's file in this worktree, whichever commit it was composed for, so a line for a commit already merged is rebuilt from its recorded results, not typed (found by `[spec] glob`; the spec is `--spec`, else this node's), replacing a `verified:` line or adding one before the closing `---`; a line that wouldn't read back as the same entries isn't written.

`bmesh done` checks, in order, refusing with exit 3 at the first that fails:

1. The spec reads `done` on main with a `merged` commit main contains (`git merge-base --is-ancestor <merged> <ref>`, `ref` = `origin/main` when the repo has an origin, else `main`), else `bmesh: not done: merged <sha> is not on <ref>`.
2. For a lead only (other roles stop after step 1), the `verified` line is non-empty (`bmesh: not done: verified is empty`) and has no `fail` entry (`bmesh: not done: verified has a failing entry <kind>`).
3. For a lead, each entry in turn is for the tree of `merged` (`bmesh: not done: verified entry <kind> is for tree <t>, merged tree is <m>`) and is matched by a record of that kind at that tree with the same `ok` (and model, when the entry names one), whatever its args. A carried entry is matched by a model record at the tree its `c:` names (first 8 hex), with the same `ok` and model, whose `patch_id` is the diff `merged` landed: that of `<a>..<merged>` for one of the nearest 100 first-parent ancestors `<a>` of `merged`. Else `bmesh: not done: verified names <kind> but no result for tree <t>` (`<t>` the `c:` tree for a carried entry).

An executor's `done` skips the spec check.

On success `done` prints `done <path> <sha8>` (`<sha8>` the `--commit` given); `started`, `progress`, `ask` and `blocked` print `<event> <path>`. `inbox` prints one line per message, `<at> <from>: <text>` (a reply as `<at> <from>: reply to job <id>: <kind> ok|fail, N lines; bmesh routine wait <id> prints it`), or `inbox: empty` when nothing waits (nothing under `--json`).

A `done` for a node already `done` is compared with its newest `done` event's commit:

- the same commit (equal, or one an abbreviation of at least 7 characters of the other) exits 0 with `already done at <sha8>`, checking and publishing nothing;
- another is refused, exit 3 `bmesh: refused: <path> is already done at <sha8>, not <sha8>`.

A lead's `--commit` is the commit `main` points at after its push, the follow-up commit recording `done`; the spec check reads `merged` and the results, not this sha.

## Landing

```
bmesh land [--spec NNN] [--ttl D] [--wait D]
```

`land` lands a lead's branch on `main` from its worktree in one command, under the lease `land.<repo>` (`bm-leases`, key `land.<repo>.0`, one place; `<repo>` the node's repo, else the registry row or base name of the main checkout), so two leads landing at once queue instead of redoing each other's work. The spec is `--spec`, else the node's; the holder is the node, else the host name.

Before the lease, it:

1. Run from a node held in `bm-holds`, at either gate, stops first (exit 3, nothing pushed, `bmesh: held before land by <by> <age> ago: <why>; ...`; see Holds).
2. Refuses (exit 3) a dirty worktree and one not on a branch (or on `main`).
3. Run from a node, reads its inbox as the daemon's wake does (see Config: Wake text), holding and acking nothing:
    - While it holds a message sent by `operator` with `bmesh send`, it refuses (exit 3) `bmesh: refused: <n> unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh land again`, as the operator's messages are the ones likely to change what landing needs. Reading the inbox is the way past it; there is no `--force`.
    - Anything else unread (a report, a reply, another node's message) doesn't stop it and is named on one line, each item as the wake names it, `unread: done from <path> (branch <b> at <sha8>); reply to job <id> (<kind> ok|fail); message from <path>`.
    - Run outside a node, it has no inbox and skips the check.
4. Prints the kinds it will require of the landed tree and where each came from, `required: verify (the repo's verify kind), review (t:<tree8> at HEAD)`, or `review (t:<tree8> before the rebase at <sha8>)` for a result from before a rebase by hand (`required: nothing` when none).
5. While another holder has the lease, prints `waiting for land.<repo> (held by <holder>)` once per holder and retries every 2 s, giving up after `--wait` (default 30m; exit 5 `bmesh: timed out waiting for land.<repo>, held by <holder>`).

The lease lasts `--ttl` (default 10m, at least 3 s) unrenewed and is renewed every ttl/3, so a lander that dies blocks the next for at most that long. Holding it, `land` reads the hold and the inbox again, so an operator message that arrived while it waited refuses it before the fetch; one arriving after that read, during the rebase and checks, is not seen. It prints `lease land.<repo>`, then a line per step:

1. `fetch origin/main <sha8>`: `git fetch origin main`; refused (exit 3) while the main checkout's `main` has commits `origin/main` doesn't (someone's unpushed work, never pushed by `land`). Without an origin the base is the local `main`.
2. `rebase onto <sha8>: N commits` (or `rebase: on <sha8> already`).
    - A failed rebase is aborted and refused, exit 3, `bmesh: refused: rebase onto origin/main conflicts in <files>; it is aborted and nothing is pushed. Rebase yourself, resolve it, re-validate, then run bmesh land again`; resolving it is the lead's.
    - A branch with nothing beyond the base is refused (`nothing to land`), as is a spec not reading `building` in the worktree.
3. `scaffold: follows the row`, with a `ship` row for the repo whose `AGENTS.md` (in the rebased tree) has a `<!-- bmesh:` marker.
    - The scaffolded text the branch changed is compared with what `scaffold` writes from the row and the binary: each of `AGENTS.md`'s marked blocks when they differ from the base's (`origin/main`'s), and the verify kind's command when it differs from the base's.
    - A file the branch didn't change there isn't compared, so a row published mid-build, a newer snippet or another box's older binary never refuse a landing; a file `scaffold` would create isn't drift.
    - Drift prints the first 20 lines of its unified diff (`...` after them) and is refused, exit 3, `bmesh: refused: the branch's scaffolded files differ from what bmesh scaffold writes from <repo>'s row; the row's verify is the source: bmesh repos propose with the new verify, or restore the scaffolded files`; once the row is published with the new verify, `bmesh scaffold <worktree>` writes the files on the branch.
    - A repo never scaffolded prints `scaffold: not scaffolded, not checked`; one with no `ship` row prints nothing.
    - A tree whose `go.mod` declares this binary's own module renders its own blocks, which this binary may predate, so only the verify kind is compared: `scaffold: the verify kind follows the row (AGENTS.md is rendered by this tree's own bmesh, not checked)`.
4. The results for the rebased tree.
    - Required: kinds with a result at the branch's tree before the rebase, and `verify` when the repo defines it.
    - A branch rebased by hand is read as it was before, walking the branch's reflog (`git reflog show refs/heads/<branch>`):
        1. When HEAD is the commit a rebase made (the reflog has a `rebase (finish)` entry at HEAD; `rebase -i` and `pull --rebase` are the same), the head the rebase rewrote, the reflog entry just before it, counts as land's own head before the rebase.
        2. And so on back, while that head was itself made by a rebase.
        3. A commit made after the rebase ends the walk: HEAD is then read as it stands, as is a branch whose reflog has no rebase or expired (`gc.reflogExpire`, 90 days), or that was rebased on another box.

        A kind with no result at HEAD's tree is required when that head's tree has one, and judged against that head and its fork point from main, as if land had done the rebase. So a review lost to a conflict the lead resolved by hand (land stopped, the lead rebased and ran it again) is carried when the resolution left the diff unchanged, else refused, as below. A review's findings fixed in a commit then rebased land on verify alone, the fix's tree having no review.
    - An inline kind runs as `routine run` does (a deterministic one is answered from its record) and prints its verdict line; a failing one prints its output and exits 1.
    - A deterministic one (`verify`) with no result for the rebased tree first looks for an ok result at a tree differing from it only outside the repo's code (its row's `code`, see repos.toml; `git diff --quiet <old> <new> -- <code>`), in this order:
        1. the last tree it was ok at in this run;
        2. the branch's tree before this pass's rebase;
        3. the tree its `required` line named.

        The first with one is carried:

        - stored again for the rebased tree and HEAD, its output led by `carried from t:<old8>: nothing under code changed`, so `done` and `spec verified` match it as any recorded result;
        - printed `verify ok t:<new8> (carried from t:<old8>: nothing under code changed)`, in seconds.

        A change under the code, or to `.bmesh/`, runs it.
    - Another kind (a review) prints the first that applies:
        1. the entry its result for the rebased tree makes, `<entry> (recorded)`;
        2. the entry a result recorded with the rebased diff's patch-id makes (see `verified`), `<kind> ok t:<new8> m:<model> c:<old8> (carried from t:<old8>: the diff is unchanged)`;
        3. a result from before the rebase that is ok, recorded no patch-id, and whose diff the rebase left unchanged (the patch-id of the fork point to the old head equals that of the new base to the new head) carries: each model's result is stored again with that patch-id, the diff it read, and lands on the line as a carried entry, printed `<kind> ok t:<new8> m:<model> c:<old8> (carried from t:<old8>: the rebase left the diff unchanged)`, which `done` matches as any carried entry. A result with no model has nothing `done` can match, so it carries off the line, printed `<kind> ok t:<old8> carried: the rebase left the diff unchanged (not on the verified line)`.

        A failing one, or a changed diff, is refused (exit 3) naming the kind and how to re-run it.
    - Any failing entry for the tree is refused too.
5. `verified: ...` and `spec NNN done: merged <sha8>, branch <b>`: the `verified` line as `spec verified --write` composes it, and `status: done` (from `building`) with `merged` the rebased HEAD and `branch` the worktree's current branch. A spec whose `## Needs from the operator` section has steps also gets `operator_steps` (see Steps for the operator under The ledger), printed `operator steps: N (bmesh spec steps NNN)`.
6. `commit <sha8> NNN: done at <merged8>`: the spec's change, committed on the branch.
7. `push origin main <sha8>`: `git push origin HEAD:refs/heads/main`, a fast-forward or nothing.
    - A push refused because `origin/main` moved takes the record commit back off the branch, prints `main moved; again from the fetch` and restarts at step 1, once, where step 4 carries the verify it just ran when what moved `main` is outside the code (a coordinator's ledger commit, or with `code` set a README commit).
    - A second move is refused (exit 3) with nothing pushed.
    - Any other failure takes the commit back and exits 1.
8. `main <sha8>`: the main checkout's `main` fast-forwarded as `choose-ideas` does (`merge --ff-only` when it is on `main`, else `fetch . <branch>:main`); when it can't, `land` says so on stderr and still lands, `origin/main` holding the commit. Without an origin this step is the landing, and a `main` that moved is retried as a refused push is.

It ends `released land.<repo>` and `landed <sha8>`, the record commit `bmesh done --commit` names. When it stops, nothing is pushed and the branch holds no record commit, so `land` runs again as it stands.

## Hosts

Boxes sharing the hub act on each other through it, with no SSH between them: the admin's signed roster says which boxes belong, every message between boxes is signed, and a box runs only what its roster row lets it.

Nothing here runs unless `[hosts] enabled = true`, which only `bmesh init --hosts` sets; a single box never asks, and its daemon never pushes or pulls anything.

| Op | Does | See |
|---|---|---|
| `ping` | shows the mesh works | `ping`, below |
| `level` | levels the box's checkout with origin (fast-forward, or push when ahead) | Levelling |
| `propose` | sends this box's `repos.toml` to the admin as a proposal, held for approval | The registry |
| `prune` | lists, and with `apply` removes, finished worktrees and merged `bmesh/*` branches | Pruning |
| `forget-worktree` | removes a done or blocked node's worktree and branch on its own box, and forgets the node | Another box's worktree |
| `stop-thread` | stops a node's thread on its own box | Another box's thread |
| `nix` | removes a repo's checkouts (admin-signed) | Removing a repo everywhere |
| `check-step` | runs a step's check | A step's check, below |

**Joining.** `bmesh init --hosts [--admin]` asks:

- join (with an invite) or be the admin (`--admin` says admin);
- whether this box is **secure** (others' requests wait for the operator's approval);
- whether its daemon runs routines (default: no on a secure box, else yes).

Run again, its defaults are the current answers (from `config.toml`, `host.key` and `<state dir>/roster.json`). It needs a `config.toml` (`bmesh init` first) and a `[host] name` (or host name) that is one subject token: `^[a-z0-9][a-z0-9-]*$`, no `--`, at most 63 characters.

Last, after the keys and the join, it writes the `[hosts]` table (`enabled`, `admin`, `secure`) and, with routines off, `[runner] checkouts = ""` (routines back on drops a `checkouts = ""` line, back to the default). Other `config.toml` lines stay; the old file is kept as `config.toml.prev`; `repos.toml` is never touched; `--force` still means start over. Restart the daemon afterwards: it reads the config at start.

- **The admin** (`--admin`) makes the admin key (below) if missing and publishes roster v1 with its own row (its approve key is the admin key; it accepts `ping`). Run again, it keeps the roster and republishes it only when its own row changed. If the hub has a roster signed by another key it prints `a roster signed by <fingerprint> exists; becoming admin replaces it on every box you invite again`. It prints `admin: <host> <fingerprint>, roster v<N>`.
- **The invite:** on the admin, `bmesh hosts invite <name> [--secure] [--accept a,b] [--replace]` (default `--accept ping`) prints `invite for <name>: <id>-<secret>` and waits. The id (8 hex characters) isn't secret; the secret is 128 random bits in base32. The code is good for 10 minutes and once. A name already in the roster is refused unless `--replace`, and the admin's own name always (its row carries the admin key).

The join:

1. The new box's `bmesh init --hosts` asks for the code at a prompt (never argv, so it's not in `ps` or the history).
2. It sends `{name, key, approve_key, ts, salt}` on `bmhosts.join.<id>` (core NATS request/reply: nothing stored), MACed with HMAC-SHA256 under `HKDF-SHA256(secret, salt=id, info="bmesh join request v1")`.
3. The admin burns the invite on the first request whose MAC verifies; one that fails is dropped without burning, and the third such failure burns it. The name must be the invite's.
4. The admin publishes the next roster with the box's row (its approve key only when the invite says `--secure`) and replies `{roster, admin_key}` under the same MAC (`info="bmesh join reply v1"`, bound to the request's salt).
5. The new box takes the first reply whose MAC verifies (ignoring junk), checks the roster carries its key, pins the admin key and stores the roster: `joined: roster v2, admin <host> <fingerprint>`.

A fresh invite re-pins a box that pinned another admin. A joined box may leave the code empty to keep its roster.

**Keys.** Every joined box has an Ed25519 host key in `<config dir>/host.key` (the seed in hex, mode 0600). Its agents can read it, so it proves only "this box".

The admin box, and every secure box, also has one protected key agents can't use: the admin key (signs rosters, registries and `nix` jobs) or the approve key (signs approvals). Its public half is in `<config dir>/admin.pub` (or `approve.pub`), so only signing reads the key itself. `[hosts] key_store` says where it lives:

- **`keychain`** (the default on macOS): the login Keychain, service `bmesh`, account `admin` or `approve`, written through `security -i` with the key on stdin (never in `ps`) and an empty trusted-app list (`-T ""`), so every read raises the macOS login-password dialog.
  - Before each read bmesh prints `Keychain dialog follows for: <action>`. Deny any unannounced dialog; choose "Allow", never "Always Allow".
  - Before reading it checks the item's access list (`security dump-keychain -a`) and refuses when an app was added (what one "Always Allow" does) or every app is trusted: `the admin key's Keychain item trusts <app>; remove it in Keychain Access`.
  - Over SSH there is no dialog and the read fails: admin and approve commands run at the Mac's own screen.
- **`file`** (the default elsewhere): `<config dir>/admin.key` (or `approve.key`), mode 0600, the seed sealed with XChaCha20-Poly1305 under `scrypt(passphrase, salt, N=32768, r=8, p=1)`. The passphrase is asked on the terminal without echo (twice when making the key).

The admin commands also refuse when stdin isn't a terminal: a convenience; the dialog and the passphrase are the control.

**The roster** is KV `bm-roster`, key `roster` (outside `bm.>`): `{type, version, admin_key, hosts: [{name, key, secure, approve_key, accept}], issued_at, sig}`, signed by the admin key.

- A box keeps its pinned admin key and newest valid roster in `<state dir>/roster.json`, and accepts a roster only when the pinned key signed it and its `version` is higher.
- The admin takes the next version from its own `roster.json` (after taking any newer roster its key signed), never from the hub, saves it there and writes the hub with compare-and-set on the key's revision.
- Every daemon takes a newer `roster.json` first (a roster the admin saved but couldn't write to the hub, or a fresh invite's pin), never writing an older roster over it. It reads the roster key each minute and before each job, and puts its own back when the hub's copy is missing, unsigned or lower, so a roster deleted or rolled back by a token holder comes back within a minute from any awake box.

On the admin box:

```
bmesh hosts set <name> [--secure=true|false] [--accept a,b]   # change a row (secure needs the box's approve key: a --secure invite)
bmesh hosts revoke <name>                                       # the next roster without it; no other box can revoke, and the admin can't revoke itself
```

Each prints `roster v<N>: …`. A fingerprint is the first 16 hex characters of a key's SHA-256.

**Presence.** Each daemon writes KV `bm-hosts`, key `<host>`, each minute: `{type, host, rev, os, routines, repos: [{name, path, origin}], notes, ts, sig}`, signed with its host key. `origin` is the checkout's `origin` URL, else the row's `remote`. `notes` is `<config dir>/notes.md`, read each minute, invalid UTF-8 dropped, cut to 16 KB on a character boundary (logged once) and trimmed; `""` without the file. Presence is never the source of a path or a decision.

`bmesh hosts [--json]` prints `roster v<N>, admin <host> <fingerprint>`, then one row per roster box:

- its state: `awake`, `asleep` (the entry's server timestamp is over 3 minutes old), `unverified` (its signature fails: shown without notes) or `absent`;
- the `admin` and `secure` marks;
- OS, revision, routines, repo count and `notes` when it has some;
- what it accepts, and when it was seen.

`--json` prints `{"roster_version","admin","fingerprint","hosts":[{"host":{row},"state","at","presence":{…}}]}`.

`bmesh hosts --notes [--json]` prints every box's notes: `## <host> (<state>)`, a blank line and the notes, boxes in roster order with a blank line between them (`no notes` when none has any; `--json`: `{"<host>": "<notes>"}`). An unverified box's are withheld, with `bmesh: <host>: unverified, notes withheld` on stderr. Notes hold a box's session guidance: what is local on it, what it can't reach, how it differs from the others.

**The session line.** Each minute, after presence, the daemon writes `<state dir>/this-box.txt` (through a temporary file and a rename):

`this box is <host>[ (admin)]; secure: yes|no; on the mesh: <host> (<state>[ <age>][, secure][, admin]), …`

It lists every other roster box with its presence state, an asleep one with how long ago it was last seen (`asleep 2h`: minutes, hours, or days past two days); `no other box` when there is none; and ``; notes: `bmesh hosts --notes` `` when any verified box has notes.

`bmesh hosts --here` prints that file, nothing else:

- It adds `; as of <age> ago, not updated since` when the file is over 3 minutes old (its daemon stopped, or can't reach the hub, so the states and ages are the last it saw).
- It never dials the hub (a headless routine session doesn't wait on it) and never runs or asks for `init`.
- It prints nothing (exit 0) when `[hosts]` is off or the file is missing.
- After it, `[hosts]` on or off, comes the hub line when this box's `bmesh hub` runs an older binary than its `bmesh serve` (`hub runs <hub>, serve <serve>: restart bmesh-hub`, from `hub.json` and `serve.json`, see Several boxes).

The plugin's session hook runs it (see The skills).

**Jobs and replies** travel in `bm-mesh`. A job for a box goes to `bm.inbox.host.<host>`. The box's durable consumer `inbox-host--<host>` (AckWait 10m, `InProgress` every minute while a job runs, MaxAckPending 1) resumes where it stopped, so a box that was asleep gets what it missed, in order. A reply goes to `bm.hostreply.<caller>.<job id>`. Neither shows in the map, `watch`, status or the activity reader.

**Signed messages.** Every job, reply, approval, roster, registry and presence entry is signed: `sig` is the hex Ed25519 signature over the SHA-256 of the message's bytes, deterministic (RFC 8032), so one message gives one signature on every box.

The bytes are every field in a fixed order, starting with the type tag:

- a scalar is a 4-byte big-endian length then its bytes (a string as UTF-8, an integer in decimal ASCII, a boolean `0` or `1`);
- a list is a 4-byte big-endian count then each item;
- a struct is its fields in order.

Lists of names (`accept`) are sorted and deduplicated; the roster's hosts and presence's repos are sorted by name. `ts`, `expires` and `issued_at` are RFC 3339 UTC to the second. A `salt` is 16 random bytes in hex. Keys and signatures are lowercase hex. The type tags and fields:

- job (`bmesh/job/v1`): `type, id, by, to, op, repo, args, roster_version, ts, expires, salt`;
- reply (`bmesh/reply/v1`): `type, job_id, job_salt, by, to, ok, text, ts, salt`;
- approve (`bmesh/approve/v1`): `type, job_id, job_salt, job_sha256, by, decision, ts, salt`;
- roster (`bmesh/roster/v1`): `type, version, admin_key, hosts (name, key, secure, approve_key, accept), issued_at`;
- registry (`bmesh/registry/v1`): `type, version, admin_key, rows, issued_at`;
- presence (`bmesh/presence/v1`): `type, host, rev, os, routines, repos (name, path, origin), notes, ts`.

A receiving box checks a job in this order, stopping at the first failure:

1. `by` is in the roster (an op that needs the admin is checked against the admin key).
2. The signature verifies.
3. `to` is this box.
4. `roster_version` isn't ahead of this box's roster (else it naks the job for a minute, until it has caught up; a hub it can't read naks too).
5. `ts` is at most 5 minutes ahead of its clock (else a nak for a minute: a Mac just woken can be behind).
6. `expires` hasn't passed and `expires - ts` is at most 24h.
7. The op is in this box's `accept`.
8. The salt's record in `<state dir>/salts` (one file per salt, kept until the job expires, surviving a restart):
   - none: it writes `started` and runs it;
   - `done`: it acks and sends the stored reply again, never re-running, logged only (a lost ack redelivers a job too);
   - `started` (a redelivery after a crash or sleep): it resumes an idempotent op (`ping`, `level`, `prune`, `stop-thread`, `check-step`) and answers any other `interrupted`;
   - a salt another job used is refused as `replay`.

A failure is answered `refused: <check>` (`roster`, `signature`, `to`, `expired`, `accept`, `replay`) and never run. An expired job is acked, answered and logged only, so a weekend asleep doesn't flood the phone.

Roster, signature, `to` and replay failures are also published as `bm.daemon.<host>.refused` (`text` `refused <check> from <sender>: <op> job <id>`, `body` `<sender> <check>`). Any daemon with `[notify]` and `[hosts]` pushes it once (title `<host> refused <sender>: <check>`, priority `high`) through the `push-<seq>` claim, at most once per sender and check every 10 minutes.

The caller checks a reply likewise: signed by the job's `to`, naming the job's id and salt.

**`ping`** answers `<rev8>, roster v<N>`. `bmesh hosts ping [<host>] [--wait 10s]` sends it to every roster box (itself included), or the one named, living 24h, and prints each answer as it arrives (`k16: 1a2b3c4d, roster v2`). A box silent past `--wait` prints `<host>: asleep: queued until <expires>` (`no answer in <wait>` instead of `asleep` when its presence is awake).

`bmesh hosts replies [--json]` prints every reply this box's jobs got (`<ts> <host> <op> <job id>: <text>`), checked against the job in the stream and the roster. One that fails is shown as `unverified, withheld`.

**Waking.** A daemon tells it has woken from a gap of 3 minutes or more between its minute ticks, read on the wall clock (monotonic time stops while a box sleeps). It logs it, re-reads the roster and self-levels (below); it reads the roster again before each job.

**Levelling.** A repo checked out on several boxes, some of them plugins every session loads from the checkout, stays level with origin from whichever box drives it; no box puts code on another that the operator hasn't seen, where that matters.

- **One lock per repo:** a level, the daemon's routine fetch (its rescan, and a job's commit fetched once) and a landing's fast-forward of `main` in the main checkout (`bmesh land`, `build`, `choose-ideas`) all take `<state dir>/locks/<repo>.lock`, a flock that waits for its holder, so they never collide on git's `index.lock` or a ref lock. With `[hosts]` off nothing takes it.
- **`level`** is a built-in op: `repo` the registry name, `args` `[<sha>, <branch>, <until>]` (origin's head after the sender levelled, the sender's branch, `""` for a pinned row, and when the sender stops waiting). The receiving box alone judges it against its own registry row's checkout, which must exist (else `refused: no checkout of <repo> here`):
  1. It runs a plain `git fetch origin` (and, for a row with a `pin`, fetches that tag, never forced).
  2. The target is `origin/<branch>` (the job's branch, else the checkout's current one), or the pin tag's commit for a pinned row. A missing one is `refused: no origin/<branch>` or `refused: no tag <pin>`; a checkout with no origin, `refused: no origin`.
  3. The job's sha only says "at least this": it must be in the target's history, else `refused: <sha8> not on origin/<branch>` (or `not on <pin>`), so a side branch's or a fork's commit can't be named.
  4. The checkout check moves nothing; it answers:
     - `refused: in progress`: a `MERGE_HEAD`, `CHERRY_PICK_HEAD`, `REVERT_HEAD`, a `rebase-merge` or `rebase-apply` directory, or an `index.lock`;
     - `refused: dirty`: uncommitted or untracked changes;
     - `refused: other branch`: on another branch than the target's, or detached, for a row that isn't pinned;
     - `refused: diverged`: both ahead of and behind the target.
  5. Then, by where the checkout stands:
     - A pinned row, wherever its checkout stands: it moves to the tag, detached (`moved to <pin>`), unless it carries commits no remote branch or tag has (`ahead <N>, not pushed: pinned to <pin>`); a pinned row is never pushed.
     - Behind only: it fast-forwards (`merge --ff-only`): `fast-forwarded <N>`.
     - Level with the target: `up to date`.
     - Ahead only: it pushes when the caller may push (a received job or `bmesh sync`, never a self-level), the row's mode is in `[hosts] push_modes` (default `["ship", "fork"]`) and the newest local commit is at least 10 minutes old: `git push origin <branch> <tags…>`, with the tags (lightweight ones included) that point at the pushed commits, answered `pushed <N>`. Otherwise it answers `ahead <N>, not pushed: <why>` (`a self-level never pushes`, `mode <m> is not in push_modes`, `the newest commit is <age> old (10m0s before a push)`). If another box pushed first, the push is refused as not a fast-forward: `refused: diverged`.
  6. **Plugin refresh:** after the checkout moves, the box refreshes what it loads from it:
     - when it carries `.claude-plugin/marketplace.json`, `claude plugin marketplace update <name>`, then `claude plugin update <id> --scope user` for each user-scope install from that marketplace (read from a folder in the checkout) whose version lags the folder's;
     - when `codex` is on `PATH`, `codex plugin add <id>` for each installed Codex plugin whose marketplace source is the checkout (Codex keeps a copy).

     Each refresh adds a part to the answer (`fast-forwarded 3; claude pstack@pstack-claude 0.15.24 -> 0.15.25`); a failure is appended as `plugins: <error>`. A new session loads the change. bmesh runs no other command a job or the registry names.
- **The hold:** on a secure box (its `[hosts] secure`, or its roster row's), a level that would move a repo sessions load waits for the operator, whether a job, the daemon's own self-level or `bmesh sync` at the box started it. Such a repo is a checkout that is a plugin marketplace (`.claude-plugin/marketplace.json`, `.agents/plugins/marketplace.json`, or the marketplace source of a Codex install), or the `[update] repo`. A push moves nothing here, so isn't held. A box that isn't secure holds nothing.
  - The hold is `<state dir>/held/<job id>.json`: the job (a self-level's is the box's own, signed with its host key), the repo, its head and target, and `git log --stat HEAD..<target>`. The job's answer: `held for approval (+<N> commits)`.
  - A repo has one hold: a newer job's replaces it, and the older job's sender hears `hold superseded by job <id>`. A self-level leaves an existing hold as it is.
  - The box publishes `bm.daemon.<host>.held` (`text` `<host> holds: <repo> +<N> commits (<sender or self-level>)`, `body` the hold's id), which a daemon with `[notify]` pushes once through the `push-<seq>` claim (title the text, priority `default`).
  - `bmesh hosts held [--json]` lists the holds: `<id> <repo> +<N> commits to <target8>, from <sender>, expires <day hh:mm>`.
  - `bmesh hosts approve <id>`, at the box's own screen:
    1. shows the log again;
    2. reads the approve key (the admin key on the admin box) with the announced Keychain dialog (or the file store's passphrase);
    3. signs `approve/v1` over the job's hash, with `decision` `approve <target>`, the target the operator saw;
    4. sends that to the box's daemon (core NATS request/reply on `bmhosts.approve.<host>`, outside `bm.>`); with no daemon running, this is refused.

    The daemon then:

    1. checks the approval is signed by the box's roster approve key, names the hold's job (id, salt and hash) and target, and is within 5 minutes of its clock;
    2. checks the job's signature still verifies against its sender, who is still in the roster, else drops the hold;
    3. levels. When origin moved since, nothing moves: the hold takes the new target and log, and the command shows them and asks again (`Approve <repo> at <target8>? [y/N]`, signing again with the key it read).

    Approved, it prints `<repo>: fast-forwarded <N> (approved)`, which the job's sender gets as a second reply.
  - `bmesh hosts deny <id>` drops the hold; the job's sender hears `denied by the operator`.
  - A hold expires with its job (24h; a self-level's job lives 24h too) and is dropped, logged only.
- **`bmesh sync [--repo R | --all] [--wait 30s]`:**
  1. levels this box's checkout of the repo (default: the registered repo of the current directory; `--all`: every registered repo this box has), which pushes when it is ahead and allowed to;
  2. sends `level` with origin's head to every roster box whose `accept` has `level` and whose verified presence lists the repo, living 24h;
  3. prints each answer that arrives within `--wait`: `k16: fast-forwarded 3`, `ricks-macbook: held for approval (+1 commit)`, or for one that hasn't answered `ricks-macbook: asleep, queued until <day hh:mm>` (`no answer in <wait>` when its presence is awake).

  With several repos each line starts with `<repo>: `. It works the same from any box: each box judges its own checkout against origin. It needs `[hosts]` on and the box joined; it is refused for a repo the registry doesn't name or this box hasn't checked out.
- **Self-level:** each daemon with `[hosts] enabled` levels every registered repo it has a checkout of, on start and on each wake, as a fast-forward only, never a push. Refusals are logged, not pushed; holds apply as above. A box that slept catches up as soon as it opens.
- **Pushes:** a refusal or failure of a received `level` whose sender stopped waiting (its `until` has passed: a box that was asleep answering later) is published as `bm.daemon.<host>.level` (`text` `<host>: <repo> <answer> (job from <sender>)`) and pushed once through the `push-<seq>` claim (title the text, priority `high`). Two boxes ahead at once leave the second push refused as not a fast-forward, and the phone hears `diverged`.

**The registry.** With `[hosts]` on, `repos.toml` is the admin's signed document, not each box's own file. A row's `verify` is shell every lead runs and `bmesh scaffold` writes into `.bmesh/routines/verify.toml` (and `bmesh land` refuses a branch that changes it there or in `AGENTS.md` by hand), and its `path` decides what `nix` deletes, so no box but the admin can change it on another.

- It is KV `bm-roster`, key `registry`: `{type, version, admin_key, rows, issued_at, sig}`, signed by the admin key, where `rows` is the admin's `repos.toml` as written, comments and all.
- A box takes a registry only when its pinned admin key signed it, its `version` is higher than the box's own, and the box's own loader reads the rows (the same refusals as `repos.toml`, an unknown key among them). It then writes the rows as its `repos.toml` (keeping the file it replaces as `repos.toml.prev`, through a rename) and the document as `<state dir>/registry.json`. Its hosts layer (presence, levels, prune and nix) reads the new rows at once; the routine loop reads `repos.toml` at start.
- A registry it can't read leaves both files alone: the box publishes `bm.daemon.<host>.registry` (`text` `<host>: refused: registry v<N> needs a newer bmesh (<error>)`), pushed once per version through the `push-<seq>` claim (priority `high`), and leaves the newer registry on the hub for the boxes that can.
- Every daemon reads the registry key each minute (so at start and on wake), putting its own `registry.json` back when the hub's copy is missing, unsigned or lower, as it does the roster. A box re-pinned to a new admin starts again from no registry.
- **`bmesh repos publish`**, on the admin box:
  1. prints the row diff from the current registry to this box's `repos.toml`: `registry v<N> -> v<N+1>:`, then `+ <name>: <field> <value>, …`, `- <name>` and `~ <name>: <field>: <old> -> <new>` lines, or `(comments and layout only)`;
  2. reads the admin key (the announced Keychain dialog: `publish registry v<N+1>`);
  3. publishes the next version: `registry v<N+1> published`.

  The version comes from the box's own `registry.json`, never the hub's. An unchanged file prints `registry v<N>: no change`.
- **`bmesh repos propose [--wait 30s]`**, on any other box, prints the same diff against the registry it has (`proposed against registry v<N>:`) and sends the box's `repos.toml` to the admin box as a `propose` job (args `[<base version>, <rows>]`, living 24h; the admin box's row must accept `propose`).
  - The admin's daemon refuses rows it can't read and a proposal against an older version than its registry (`refused: proposed against registry v<N>; it is v<M> now: propose again`).
  - Otherwise it holds it: `<state dir>/held/<job id>.json` with the row diff, published as `held` (`<admin> holds: registry proposal from <box>, <N> row changes`), answered `held for approval as <id> (<N> row changes against registry v<base>)`. A newer proposal from the same box supersedes it.
  - Nothing changes until it is approved. `bmesh hosts held` lists it (`<id> registry proposal, <N> row changes against v<base>, from <box>, expires <day hh:mm>`). `bmesh hosts approve <id>` at the admin's screen shows the diff, reads the admin key and publishes the proposal as the next version, `registry v<N> published: proposal from <box> approved`; the proposer hears `approved: registry v<N> (<N> row changes)`.
  - A proposal the registry has moved past since, or one past its job's expiry, is dropped (its rows would undo what came since) and its proposer told so.
  - While the admin's own `repos.toml` has changes the registry doesn't, which approving would overwrite, the approval is refused and the proposal stays held.
  - `bmesh hosts deny <id>` drops it as it drops a level's hold.
- `publish`, `propose` and `remove` first take a newer registry from the hub when there is one; having just replaced this box's `repos.toml`, they stop there (`took registry v<N> from the hub as <path> (your file is kept as repos.toml.prev): bring your edits over and run this again`).
- With `[hosts]` off, `repos.toml` is a plain local file.

**Pruning.** `prune [<repo>]` is a built-in op: `repo` a registry name, or `""` for every registered repo with a checkout on the box; `args` `["apply"]` to remove, else a dry run. Each box judges its own checkouts by its own nodes:

1. Each linked worktree of the checkout (the main checkout and routine checkouts under `[runner] checkouts` aside) is a row.
   - One under `[runner] worktrees` is bmesh's. It is removed when its node (matched by worktree path among this box's nodes) is `done`, it is clean, and its HEAD is merged into `main` (as `node forget --worktree` judges it, see Finished nodes) or into its lead's branch. Otherwise it is kept, with the reason: `no bmesh node`, `node <state>` (`idle` with no state), `uncommitted changes`, `unmerged`.
   - Removing is `git worktree remove` (never forced), then `git branch -d` of its branch (`-D` when the merge was proven by `git cherry` or the lead's branch).
   - Any other worktree is kept, `not bmesh's (T3 cleans its own)`: on T3 0.1.0, T3's storage cleanup removes a thread's worktree once it has no commits beyond the default branch, keeping one with uncommitted changes.
2. Then every local `bmesh/*` branch no worktree holds (T3 leaves a lead's behind) is removed when `git branch -d` accepts it (merged into its upstream, else into the checkout's HEAD), never with `-D`; any other is kept, `unmerged`.

With `apply` it holds the repo's lock (see Levelling) while it removes.

The answer is a line per row, `<repo> <worktree path> <keep|remove|removed|failed> <reason>` or `<repo> branch <name> <keep|remove|removed> <reason>` (a failed removal adds `  (failed: <error>)`), and a summary, `<N> worktrees, <B> branches, <M> to remove, <K> kept` (applied: `, <R> removed`, and `, <F> failed`). A named repo the box has no checkout of answers `no checkout of <repo> here`.

`bmesh prune [--repo R | --all] [--apply] [--wait 30s]` (default: the registered repo of the current directory) prunes this box first, then sends `prune` (living 24h) to every other roster box whose `accept` has it, and prints each box's lines as `<box>: <line>`, or `<box>: asleep, queued until <day hh:mm>` for one that hasn't answered. With `[hosts]` off it prunes this box only. It exits 1 when a removal on this box failed.

**Another box's worktree.** `forget-worktree <node path>` runs `node forget --worktree`'s removal on the node's own host, with the same rules (see Finished nodes), and on success forgets the node there too: `removed worktree <path>; deleted branch <b>; forgot <path>`. Beyond the removal's own refusals it refuses `refused: no node <path>`, `refused: <path> is on <host>, not <box>` and `refused: <path> is <state>; only a done or blocked node is forgotten`.

`bmesh node forget <path> --worktree [--wait 30s]` of another box's node sends it (living 24h) to that box, which must accept it, waits, and forgets the node only on success, printing `<box>: <answer>` and `forgot <path>`. A refusal is printed and exits 3, forgetting nothing. A box that doesn't answer in time prints `queued; the node stays until <box> answers`; on waking that box takes the job and forgets the node itself.

**Another box's thread.** `stop-thread <node path> <who> <why>` stops a node of the box's own the way `bmesh node stop` does there (see Stopping a node):

1. the node is marked `blocked` with `stopped by <who>[: <why>]` and that `blocked` published;
2. its thread is interrupted through the box's runner;
3. it answers `stopped <path>`, or `already <state>` for a `done` or `blocked` node (a stopped one has its thread interrupted again: the sender marked it while the box was asleep).

It refuses `refused: no node <path>`, `refused: <path> is on <host>, not <box>` and `refused: <path>'s runner is "<kind>", not this box's <runner>`.

`bmesh node stop` sends it (living 24h) for each thread to stop on another box, all at once, which must accept it, and waits `--wait` (default 30s) for the answers, printing `<box>: <answer>` for each; refusals are printed and exit 3. A box that doesn't answer in time has the node marked stopped from the sender, so its daemon never wakes it, and prints `queued; <path> is marked stopped and <box> stops its thread when it answers`: when it wakes it takes the job and interrupts the thread.

**A step's check.** `check-step` (`repo` a registry name, `args` `[<spec id>, <step number>]`) runs one step's `check:` for `bmesh spec steps --check` (see Steps for the operator under The ledger). The job names the step, never a command. The box:

1. fetches its own checkout of the repo (under the repo's lock);
2. reads the spec's `operator_steps` from its `origin/main` (`main` without an origin);
3. runs that step's check through the platform shell in the checkout, for at most 2 minutes, answering ok with `exit 0`, or not ok with `exit <code>: <the output's last line>`.

It refuses `refused: no checkout of <repo> here`, a spec or step it can't find, `refused: step <n> is for "<box>", not <host>` and `refused: step <n> has no check`. The job lives 10 minutes, since its sender stops listening after `--wait`. Accepting `check-step` lets the box run the `check:` commands of specs on the repos' `main`, as a box with routines runs main's kinds, so the admin chooses per box (`bmesh hosts set <name> --accept ...`).

**Removing a repo everywhere.** `nix <repo>` is a built-in op that needs the admin's signature: its job is signed by the admin key, from the admin box. It carries the repo name and the origin of the repo's row in the admin's registry (`args` `[<origin>]`, plus `ignored-ok`), never a path.

- The path comes from the receiving box's own registry row. A repo with no row there answers `absent: <repo> is not in this box's registry`; a row whose checkout doesn't exist, `absent: no checkout at <path>` (its routine checkouts, `<[runner] checkouts>/<checkout name>`, still go).
- The box refuses, touching nothing, unless the directory (symlinks resolved) has the row's path's name, is a checkout's top level, has an `origin` (`refused: <path> has no origin`) that is the job's (`refused: <path>'s origin is <url>, not the registry's <origin>`; a GitHub URL matches its `owner/name`, and a trailing `/` or `.git` doesn't count), and the box's own row names no other remote.
- It also refuses:
  - a checkout with uncommitted or untracked changes;
  - a linked worktree (other than a routine checkout) with uncommitted or untracked changes, or whose HEAD is not in `main` or `origin/main` (`refused: worktree <path> is on <branch>, not merged into main`);
  - a commit on no remote (`git log --branches --tags --not --remotes`, so an unpushed tag too: `refused: <path> has commits on no remote (<commit>)`);
  - a stash;
  - ignored files, in the checkout or a linked worktree, unless the job says `ignored-ok` (`refused: <path> has ignored files (<paths>): nix with ignored-ok to remove them too`).
- On success it removes the linked worktrees (`git worktree remove`), `<[runner] checkouts>/<checkout name>` and the checkout, under the repo's lock, and answers `removed <path> and <checkouts dir>[, with <N> linked worktrees]`.

`bmesh repos remove <name> --checkouts [--ignored-ok] [--apply] [--wait 1m]`, on the admin box:

1. sends `nix` (living 24h) to every roster box whose verified presence lists the repo (the admin box too; each must accept `nix`) and prints each answer as it arrives;
2. only once every one of them has answered `removed` or `absent`, publishes the next registry without the row: `registry v<N>: <name> removed`.

Until then it prints `pending: <box>, …; the registry keeps <name> until each answers removed or absent (run this again to re-ask them)` and keeps the boxes and their jobs in `<state dir>/nix/<name>.json`. Run again, it counts the answers that arrived since and re-asks only the boxes still pending. One read of the admin key signs the jobs and the registry. Without `--apply` it prints the boxes it would ask and the version it would publish, asking nobody. It is refused for a row with no `remote`, and while this box's `repos.toml` has changes the registry doesn't (`publish them first`).

Without `--checkouts` it only publishes the registry without the row. With `[hosts]` off, `bmesh repos remove <name> [--apply]` takes the row out of this box's `repos.toml` (keeping `repos.toml.prev`; `--checkouts` is refused).

**Threat model.** Whoever holds the hub token can read every message, delete keys and streams and flood subjects; signatures don't stop that. They stop such a party, or any box not in the roster, from getting a box to act. Revocation is best-effort against a token holder, who can delay a box's view of the newest roster but can't forge one. A box's `host.key` is readable by its own agents, so a compromised box can act as itself until it is revoked. The admin and approve keys are the secrets no agent can use.

## Names

Path rules, and the streams, subjects and buckets built on paths.

- A path matches `^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)*$`.
- Its first token is never `inbox`, `daemon`, `feedback`, `host` or `hostreply`, the first tokens of subjects that are not a node's events (`routine` is for the routine pool: `routine.<kind>` and `routine.<kind>.<instance>`).
- No token contains `--`: the inbox consumer name maps `.` to `--`, so `a--b` and `a.b` would collide.

**Repo names.** A registry name may hold capitals, `_` and `.` (see repos.toml), which a path token can't. So every path, family and branch made from it (`<repo>-round-<N>`, its children's branches, `<repo>-<NNN>`, `<repo>-build-<N>`, and the branches `build` and `choose-ideas` record on `main` through) starts with its node token: the name lower-cased, `.` and `_` written as `-`, runs of `-` collapsed to one, a trailing `-` trimmed (`MapleMarketScanner` is `maplemarketscanner`, `my_repo.js` is `my-repo-js`). A node's `repo` and the worktree directories keep the registry name.

Two names with one token (`Foo` and `foo`, `my.app` and `my_app`) would share paths, so `bmesh init`, `repos publish` and `repos propose` refuse such a registry (exit 3, `bmesh: repos Foo and foo are both foo in node paths; rename one`).

| Kind | Name | Subjects / keys |
|---|---|---|
| stream | `bm-mesh` | `bm.>` (30 days, file store, 2m duplicate window) |
| event subject | | `bm.<path>.<event>` |
| inbox subject | | `bm.inbox.<path>` |
| daemon subject | | `bm.daemon.<host>.<event>` (`started`, `update`, `wake`, `silent`, `digest`, `push`, `push-failed`, `refused`, `held`, `level`, `registry`, `fixer`) |
| consumer per inbox | `inbox-<path, "." as "--">` | filter `bm.inbox.<path>` |
| kind inbox subject | | `bm.inbox.routine.<kind>` (jobs and gos) |
| consumer per kind | `inbox-routine--<kind>` | filter `bm.inbox.routine.<kind>`, shared by every box that runs the kind; MaxAckPending -1 (each box fetches up to its own instance count), AckWait = timeout + 1m (the last writer's) |
| routine event subject | | `bm.routine.<kind>.<instance>.<event>` (`started`, `progress`, `done`) |
| feedback subject | | `bm.feedback.<family>` (a build family's pending feedback; no consumer, drained by purge) |
| host job subject | | `bm.inbox.host.<host>` (signed jobs for a box; see Hosts) |
| consumer per host | `inbox-host--<host>` | filter `bm.inbox.host.<host>`; MaxAckPending 1, AckWait 10m |
| host reply subject | | `bm.hostreply.<caller>.<job id>` |
| join subject | | `bmhosts.join.<id>` (core NATS, outside the stream: an invite's request and reply) |
| KV | `bm-nodes` | `<path>` to Node (a build's lead carries its `family`; a node whose runner named its own thread, as T3 does, carries the id bmesh chose as `runner.ref`: the briefs' `proposed_by` and the spec's `built_by`) |
| KV | `bm-state` | `<path>` to State |
| KV | `bm-runner` | `<path>` to RunnerSample |
| KV | `bm-results` | `<kind>.<tree>[.<args8>][.<model-short>]` to Result (`[routines] keep_results`, default 30 days) |
| KV | `bm-leases` | `<slot>.<n>` to `{"holder","expires_at"}` |
| KV | `bm-roster` | `roster` to the admin's signed roster, `registry` to its signed registry (see Hosts) |
| KV | `bm-hosts` | `<host>` to that box's signed presence |
| KV | `bm-claims` | `quota.<provider>.<reset unix>` to the claiming daemon (`daemon.<host>`); age limit 8 days |
| KV | `bm-jobs` | `<kind>.<host>` to `{"kind","host","at","jobs":[{"seq","job","from","holder","state","since"}]}` (that daemon's held and running jobs of the kind) |
| KV | `bm-holds` | `<path>` to `{"gate","text","by","at"}` (a coordinator's hold on a lead, until `unhold`; see Holds) |

The map, `watch`, status, the push loop and the activity reader skip the same subjects (`mesh.IsMeshInternal`: first token `inbox`, `daemon`, `feedback`, `host` or `hostreply`), each keeping what it reads of inbox and daemon events. Host traffic (`bm.inbox.host.*`, `bm.hostreply.*`) never reaches the map or `watch`.

JetStream is addressed through the domain from `[mesh] domain` (default `hub`).

## Config

Three files in the config dir, `<os.UserConfigDir()>/bmesh` (`~/.config/bmesh` on Linux, and anywhere that directory already exists). `BMESH_CONFIG` overrides the config file's path; the other two live beside it.

| File | Holds |
|---|---|
| `config.toml` | this box's settings |
| `repos.toml` | the registry of repos, the same on every box |
| `.setup-complete` | the setup flag, written by `bmesh init` (see Quickstart) |

`bmesh config check` loads this box's `config.toml` and `repos.toml` with this binary, as every command does, and prints `config ok: <path>` or refuses (exit 3) with `config check failed: <why>`, naming the key.

`bmesh serve` (the supervisor included) and `bmesh hub` run the same check at start: on a refusal they print `bmesh: config check failed: <why>` and exit 78 (`EX_CONFIG`), which the units `init --service` writes don't restart on (see Quickstart).

### repos.toml

The registry: one `[repos.<name>]` table per repo. A name matches `^[A-Za-z0-9][A-Za-z0-9_.-]*$` (a directory name, so a checkout keeps its name); quote one holding a dot, `[repos."my.app"]`. Every key with its default:

```toml
[repos.party-buzzer]
path = "~/repos/party-buzzer"           # required; the checkout on this box (~ expands; a relative path is from the config dir)
mode = "ship"                           # ship | companion | publish | fork | dormant
remote = ""                             # owner/repo, or a URL; for people and the table
host = ""                               # the box proposal threads run on ("" = this one)
ux_host = ""                            # the box UI/UX threads run on
verify = []                             # the shell commands that must pass before a change is done
surfaces = []                           # the UI screens a UI/UX thread screenshots, with their viewports
pin = ""                                # a release tag every checkout stays on, instead of following a branch
routines = ["verify", "review"]         # the routine kinds a build runs
code = []                               # git pathspecs of the repo's code ("cmd/**", "*.go"); none = everything but the spec files
```

**Code.** `code` lists git pathspecs relative to the repo's root, read as `git log -- <pathspec>` reads them (`*` crosses `/`: `*.go` is every Go file, `cmd/**` everything under `cmd/`).

- `.bmesh/` (the routine kinds and their config, which say what verify is) is always code.
- A change is code when it touches a path one of them matches.
- Without `code` (or with it empty), code is everything but the spec files, the paths `[spec] glob` matches (`docs/proposals/NNN-*.md`), so a `README.md` beside them is code.
- One rule reads it everywhere: main's code head (see Updates) is what `build` verifies, `release` tags and `[update]` follows; `land` carries a `verify` across a rebase that changed nothing under code.
- List every file the build or tests read (the `go:embed` sources, test fixtures, documents a test checks): one left out is never verified when it alone changes.

**Errors.** One loader serves every command, so an unknown key, a row without `path` or a mode outside the five is an error for all. `[repos]` in `config.toml`, where the registry used to be, is refused with a message naming `repos.toml`. `bmesh lead start --repo`, `routine queue` and the daemon resolve repo names through this file.

```
bmesh repos [--json]               # the table: name, mode, remote, verify, host, ux_host, surfaces, pin (sorted by name)
bmesh repos show <name> [--json]   # one row, key: value lines (code: (everything but the spec files) for a row without it)
bmesh repos publish                # (admin, [hosts] on) publish this file as the next registry version
bmesh repos propose [--wait 30s]   # ([hosts] on) send this file's changes to the admin box, held for approval
bmesh repos remove <name> [--checkouts [--ignored-ok]] [--apply] [--wait 1m]   # take a row out (see Hosts)
```

- `--json` prints the rows as an array (`show`: one object) of `{"name","path","mode","remote","host","ux_host","verify":[],"surfaces":[],"pin","routines":[],"code":[]}`, defaults filled in, for other tools.
- `show` of an unknown name exits 3.
- `bmesh init` adds the current directory's checkout as a row (`path`, and its GitHub origin as `owner/repo`) unless it is there, rewriting the file whole, so comments are lost.

**With `[hosts]` on** the file is the admin's: each daemon writes it from the newest registry the admin signed, keeping the old one as `repos.toml.prev`. So a change here reaches other boxes through `bmesh repos publish` on the admin box or `bmesh repos propose` elsewhere; otherwise the next registry overwrites it (see Hosts: The registry).

### config.toml

Keys and defaults. An unknown key is ignored. `[runner] t3_dispatch` and `t3_project`, which the t3 runner read before T3 0.1.0, load with one stderr line each: `bmesh: [runner] t3_dispatch is ignored (T3 0.1.0 needs no script); delete it from <path>`.

```toml
[mesh]
nats = ""                                   # "" = the server bmesh serve embeds on listen, which every other command dials; else e.g. "nats://localhost:4222"
domain = "hub"
token_file = ""                             # "" = no token
store = "<cache dir>/bmesh/jetstream"       # embedded server's store (os.UserCacheDir: ~/.cache on Linux, ~/Library/Caches on macOS); an existing ~/.local/state/bmesh/jetstream is kept
listen = "127.0.0.1:4222"                   # embedded server's bind
[host]
name = ""                                   # "" = `hostname -s`; also the name of the server bmesh hub runs
[hub]                                       # the server bmesh hub runs on this box (see Several boxes)
role = ""                                   # "" = no hub here (one box: serve embeds it; or a client of another box's hub) | "hub" | "leaf"
listen = ""                                 # hub: "<tailnet ip>:4222"; leaf: "127.0.0.1:4222"; never every interface
leaf_listen = ""                            # hub only: "<tailnet ip>:7422", the port leaves dial; "" = none
remote = ""                                 # leaf only: "nats://<hub tailnet ip>:7422", redialed forever
store = ""                                  # hub only; "" = <state dir>/hub/jetstream
max_file_store = "8GB"                      # hub's JetStream limits, as nats-server reads sizes (GB/MB powers of 1024, G/M of 1000)
max_memory_store = "256MB"
[serve]
listen = "127.0.0.1:3774"
[runner]
kind = "t3"                                 # claude | t3 | tmux (init writes claude when claude is on PATH)
t3_server = "http://127.0.0.1:3773"         # this box's T3 0.1.0; "" is a config error
t3_runtime_mode = "auto"                    # auto | full-access: the mode t3 threads start in
tmux_session = "bmesh"
worktrees = "~/.bmesh/worktrees"            # executors' worktrees, and the claude and tmux runners'
checkouts = "~/.bmesh/checkouts"            # routine instances' checkouts; "" = routines off
claude = "claude"                           # claude command for model routines and the claude runner, split on spaces
max_leads = 0                               # leads at once on this host (lead start refuses past it); 0 = no cap
[notify]
ntfy = ""                                   # "" = no digest; else "http://k16:2586/bmesh"
command = ""                                # test hook: receives the POST body on stdin instead of HTTP
digest_every = "1h"                         # command runs through the platform shell, body on stdin, title in $BMESH_TITLE (and $NTFY_TITLE)
push = ["ask", "blocked", "done"]           # a lead's (and a round's) events pushed at once (only these three; another entry is a config error); [] = no pushes
map_url = ""                                # the map's URL for a push's click-through (<map_url>#/lead/<path>); "" = none
catch_up = "1h"                             # on start, push missed events no older than this
[liveness]
grace = "20m"
poll_every = "60s"
wake_every = "10m"                          # an idle lead (or engineer) with unread mail is woken at most this often
[spec]
glob = "docs/proposals/{id}-*.md"
[models]
lead = "claude-opus-5-5"                    # lead start's model when --model names none (build passes the spec's judge)
lead_effort = "high"                        # a lead's effort: low, medium, high, xhigh or max; "" = the runner's default
proposer = "claude-opus-5-5"                # a proposal round's two threads
proposer_effort = "xhigh"                   # their effort
[status]
keep_done = "24h"                           # mesh-status leaves out nodes done longer than this, and the daemon deletes stopped routine instances' nodes after it; "0" = never. Durations take whole days too: "30d"
[routines]
keep_results = "30d"                        # bm-results' age limit, applied by serve; an older result is gone; "0" = none
[quota]
command = ""                                # the map's quota strip; "" = no strip. Run through the platform shell at most every 5m (30s timeout); prints a JSON array of {"reading":{"provider","five_hour","week","five_reset","week_reset"}}
remind_topic = ""                           # the ntfy topic a used 5-hour window's reset reminder goes to; "" = none (see Quota reminders); needs command and [notify] ntfy
[console]
thread_url = ""                             # the map's thread link for leads on this host, {thread} = the lead's runner thread, path-escaped (mcp:x is mcp%3Ax); "" = this host's [console.hosts] entry, else no link
[console.hosts]                             # host = template, for leads on other hosts (e.g. k16 = "http://k16:3773/…/{thread}")
[update]
repo = ""                                   # registry name of the repo whose main the daemon rebuilds itself from, at what track follows; "" = off (see Updates)
check_every = "6h"                          # without repo, how often the daemon checks what track follows on the module's main and writes the notice when behind; "0" = never
upstream = false                            # without repo, also install the target when there is a newer one (go install), drain and re-exec; on Windows only under serve --restart
track = "release"                           # what every update follows on main: release = the newest vX.Y.Z tag (the code head until there is one), latest = the code head (newest commit that changes the repo's code); bmesh update --latest goes there once
[hosts]                                     # written by bmesh init --hosts (see Hosts)
enabled = false                             # nothing of the hosts layer runs unless true
admin = false                               # this box is the roster's admin
secure = false                              # what others ask of this box waits for the operator
key_store = ""                              # keychain | file: where the admin or approve key lives; "" = keychain on macOS, file elsewhere
push_modes = ["ship", "fork"]               # the registry modes whose rows a level pushes when this box is ahead (see Hosts)
```

**State dir**, what the daemon keeps across restarts (the T3 sign-in's push record, `roster.json`, the salt records, holds in `held/`, per-repo locks in `locks/`): `$XDG_STATE_HOME/bmesh` when `XDG_STATE_HOME` is set, else `~/.local/state/bmesh` on Linux, `~/Library/Application Support/bmesh/state` on macOS. Never the cache dir, which macOS purges.

**Digest.** Also published as a `digest` event on `bm.daemon.<host>.digest`, title as `text`, body as `body`.

**Wakes.** A wake is text the daemon sends, at most once per `wake_every`, to the thread of a lead (or engineer) on this host, published as a `wake` event on `bm.daemon.<host>.wake`. It goes to:

- an idle runner with unread mail; but a `done` or `blocked` one only when that mail holds a message sent with `bmesh send` (so the operator can ask a finished lead something; a late report or reply doesn't wake it), and a stopped one only for a message from outside its own tree sent after the stop (see Stopping a node);
- a silent one, whatever its mail.

Never to a thread whose runner reports it stopped by the user.

**Silence.** A `working` node is marked `silent` (`quiet for <age>`, published as `silent` on `bm.daemon.<host>.silent`) when nothing at or under its path was published within `[liveness] grace`, so a `blocked` one, stopped included, never is.

**Wake text.** Headed `bmesh: `, or `bmesh: resumed by <sender>; ` for a stopped node, it names what waits: `` bmesh: 3 unread: done from app-012.alpha (branch t3code/x-alpha at 1a2b3c4d); reply to job <id> (review fail); message from operator. Run `bmesh inbox` now, then continue. ``

| Item | Reads |
|---|---|
| report | `done from <path> (branch <b> at <sha8>)` or `blocked from <path>` |
| reply | `reply to job <id> (<kind> ok\|fail)` |
| other | `<kind> from <from>` |

Up to five items, oldest first, then `and N more`; the count alone (`bmesh: 3 unread. Run ...`) when there is nothing to list or the read fails. Items are read only once the wake is due, from the inbox subject in the stream after the consumer's ack floor, never fetched, so the daemon holds or acks none of the lead's messages. The count is the consumer's unread count; a reply `routine wait` already acked (see Jobs) can sit above the floor, so the list can be longer.

### Pushes

With `[notify] ntfy` or `command` set, a daemon also pushes a lead's events in the `push` list to the same topic at once:

| Event | Title | Priority | Tag |
|---|---|---|---|
| ask | `<path> asks` | `high` | `question` |
| blocked | `<path> is blocked` | `high` | `no_entry` |
| done | `<path> is done` | `default` | `white_check_mark` |

- **Body:** the event's text cut to 3 KB, plus `commit <sha8>` for done.
- **Tap (`Click`):** the lead's thread when `[console]` resolves it, with an `Open map` action for its map page when `map_url` is set; else the map page. The phone must reach both, so on a tailnet `map_url` and the `[console]` templates use the full MagicDNS name (`http://<host>.<tailnet>.ts.net:3773/…`), not the short host name.
- **`command`** gets the body on stdin and the headers in `$NTFY_TITLE`, `$NTFY_PRIORITY`, `$NTFY_TAGS`, `$NTFY_CLICK` and `$NTFY_ACTIONS`.

**Others.**

- A fixer (see The fixer) pushes like a lead, but its `done` is titled `<repo> fixes: N fixed, M wait on you` (`waits` for one): `N` its report's `<seq>: fixed by` lines, `M` its `<seq>: proposed` ones, the proposed lines first in the body, the rest after as written.
- A fix lead (in a `<repo>-fix-<N>` family) never pushes: its fixer reports it.
- A round's `done` and `blocked`, and an engineer's `ask`, push as Proposal rounds says.
- Executors' and routines' events never push.
- The T3 sign-in's expiry pushes: see Runners. With `[hosts] enabled`, a box's refusals push as Hosts says (even with `push = []`).

**Once only**, across restarts and daemons:

- Before posting, a daemon claims the event by publishing `bm.daemon.<host>.push` (`to` the lead, `text` the title, `seq` the event's stream sequence) with `Nats-Msg-Id` `push-<seq>`; a duplicate reply means another daemon has it.
- On start it pushes, in order, the listed events no older than `catch_up` (by the event's `at`, else its stored time) with no `push` claim yet, then follows live.

A live event stored over a minute ago (the backlog a hub reconnect delivers) is likewise dropped when older than `catch_up` or when any daemon's `push` claim names its seq.

**Failures.** A push still failing after retries at 5 s and 30 s is logged and recorded as `push-failed`; the claim stands, so it is never repeated. At shutdown the push being posted gets 10 s; any claimed push still queued or awaiting retry is logged and recorded as `push-failed` with body `shutdown`.

### Quota reminders

With `[quota] remind_topic` set, the daemon reads quota on start, then hourly, through the map strip's gate, so `[quota] command` still runs at most every 5 minutes whoever asks.

For each provider whose 5-hour window has usage (`five_hour` above 0) and whose `five_reset`, rounded to the minute (reset times jitter by fractions of a second between readings), is still ahead, it posts one message to `[notify] ntfy`'s URL with the last path segment replaced by the topic (`http://127.0.0.1:2586/bmesh` and `quota-reset` give `http://127.0.0.1:2586/quota-reset`):

- title `Claude 5-hour quota reset` (the provider's name, capitalised);
- body `Claude's 5-hour window is fresh. Weekly was 41% used.`;
- tag `hourglass`;
- an `At` header of the rounded reset, so ntfy delivers it at the reset; none when the reset is under 30 s away (ntfy refuses a shorter delay), so it posts at once.

Once per window across daemons and restarts: before posting, a daemon creates `quota.<provider>.<rounded reset unix>` in `bm-claims`; a key already there means another daemon (or this one, an hour ago) has it. A failed post deletes its claim and is logged, so the next reading retries.

### Map

#### Blocks and the shelf

The map (`/`) draws one block per lead, a tree of the lead and its executors (a round and its engineers alike), then the orphans block, then the routines column (last on the phone, where blocks stack).

Blocks run in need order: `blocked`, `asking`, `silent`, then `working`, `held`, `idle` (no state yet), `done`; ties keep `started_at` order, so a running lead keeps its place.

With at least one live lead (any state but `done`) and one `done`, done leads leave the tree for a shelf under the live blocks: chips (`buildermesh-001 ✓`, newest first) headed `done (N)`, each linking its lead page. With no live lead, done leads draw as trees. A lead's state is its own: a live lead whose executors are all done stays a tree, and a `done` event moves the lead to the shelf on the redraw the map does for `done`.

#### The strip

Under the header the strip leads with a summary row, `2 working · 1 asking · 1 silent · 2 held · 3 done`: computed from the loaded tree on every render over leads, rounds and their children (not routines), in the order `working`, `asking`, `blocked`, `silent`, `held`, `done`, zero counts dropped, each word coloured as its badge, `all idle` when none of the six has a node. A live event that restyles a node updates it at once.

The strip shows whenever the tree has nodes, not only with a quota or digest. Below the row: the quota lines, then the newest digest as a fold (`▸ digest 50m ago: <title>`) opening to the digest body, the daemon's hourly wording, not the present.

Under `?check=1` the title's `strip=` counts the strip's rows (summary included), `held=` the `held` boxes on the map, `bandHeld=` the `held` rows in the needs-you band (always 0).

#### The recent-events drawer

Each row's node is a chip in its lead tree's hue (a hash of the path's top segment over eight hues; an executor a lighter step of its lead, routines and `daemon.*` grey), labelled without the prefix of an ancestor that has rows of its own (`eng`, full path in the tooltip). An inbox row shows the sender's chip, `→`, and a small recipient chip.

Each node with rows takes a lane, in order of first appearance from the newest row, 14 px apart, with a faint line in its hue down the list and its chip at the lane's offset; past six lanes (three under 700 px) the rest share the last, its line grey.

- Hovering a chip or its line dims every row but that node's and those addressed to it, and brightens its line.
- Clicking a chip (tap on touch) pins that focus across new events until clicked again or Escape; clicking another chip moves the pin.

#### The needs-you band

Above the map, whenever a lead, round or engineer is `asking`, `blocked` or `silent`, a "needs you" band shows one row per such node: state badge, path (linking its lead page) and the state's words (the ask or block text; for a silent node the daemon's verdict, `quiet for 20m`, and how long ago). Rows run in need order (`blocked`, `asking`, `silent`); a row leaves on the node's next `started`, `progress` or `done`, live or on the next tree read.

On the map:

- An `asking` or `blocked` box's second line is the state plus as much of the words as fits, instead of its unread count.
- A `silent` box draws at full opacity with a dashed stroke in the blocked colour.
- A `held` box (a coordinator holds the lead at a gate) has a badge in its own calm slate colour, a solid stroke and, like `asking`, its words as the second line. It counts in the strip but never gets a band row, since the coordinator put it there; `node scripts/check.mjs` exits 1 when the title's `bandHeld=` is non-zero.

After the nodes' rows, one row per landed spec with operator steps still open, from every configured repo's ledger (read as the lead page reads a spec: local `origin/main`, cached a minute, fetched in the background): an `operator` badge, `<repo> NNN`, and `<title>: X of Y open · bmesh spec steps NNN --repo <repo>`, gone once every step is ticked. `/api/tree` carries these as `operator`, `[{"repo","id","title","open","total"}]`, omitted when none.

The band's words, the drawer's rows and the lead page's inbox and timeline rows clamp to three lines and open on a tap. The timeline names a child by its last segment (`eng`, not `buildermesh-round-1.eng`), full path in the tooltip.

#### The lead page

The lead page (`#/lead/<path>`, served for any node by `/api/lead/<path>`) reads the lead's spec from its registered repo's local `origin/main` (`main` without an origin), never fetching in the request. While lead pages are viewed, the daemon fetches `origin main` in the background at most once a minute per repo, so a spec shows up to a minute or two after landing.

`/api/lead/<path>` carries:

- `children`, `[{node, state, unread}]`: the node's direct children in the tree `/api/tree` draws (a lead's executors, a round's engineers);
- for a round, `specs`, `[{id, title, status, text}]`: specs on that same `origin/main` whose `proposed_by` is one of its children's runner threads (or a child's runner `ref`), by id; `[]` before any land, `null` for other nodes. The ledger is cached a minute per repo like the spec; an unparseable file is left out.

The main column opens with a `tree` card, one row per child (state badge, last name of its path, branch, `N unread`) linking its page, absent without children; then a lead's `spec` card or a round's `specs` card, one `NNN title · status` line per spec that opens its body in place (`no specs landed yet` before any land). A card with nothing to show (`no spec`, `no messages`, no events) is one muted line and, in the one-column layout (narrower than 1000px), sorts after cards with content. Under `?check=1` the title also names its `firstCard`, the card drawn highest.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | ok |
| 1 | error; a failed run (`routine run`, `routine wait`, an inline kind `land` runs); a lost lease; an interrupted `lock-lease run`; a malformed spec, or a `spec set` its `--from` refuses |
| 2 | usage; a `scaffold` of a repo without a row or whose row is not `ship` |
| 3 | refused (causes below) |
| 5 | timeout (`watch`, `routine wait`, `t3 login`, a `land` waiting for its lease) |
| 78 | config: `serve` or `hub` refused this box's config at start, naming the key (see Config); their units, and `serve --restart`, don't start it again |

`lock-lease run` exits with its command's code.

**Exit 3 (refused)**, by area:

- **Setup and updates:**
  - a command other than `init` and `help` on a box not set up, run outside a terminal;
  - `update` whose main lacks this binary's revision or whose build reports another.
- **Specs and builds:**
  - `done` the spec or its `verified` line doesn't support;
  - `ideate` on a non-ship row, with five proposed specs, with a round still open and no `--force`, an unreachable host, a live child or an existing branch;
  - `main` that `ideate`, `build` or `choose-ideas` cannot level with origin or fast-forward;
  - `build` of a non-ship row, of a spec not `accepted`, with no judge and no `--model`, of a taken path or on a host at its lead cap, of a `ruled` spec sized L or of a third fix on one premise;
  - `fix` with no registry row holding bmesh's module, on a non-ship row or beside a live fixer;
  - `feedback drain --seq` naming a sequence that isn't feedback;
  - `release` held by a working lead or pending feedback, at a red code head, or with nothing under code changed since the last release.
- **Routines and results:**
  - dirty worktree for a deterministic inline run;
  - unknown kind;
  - fresh or pooled kind run inline without `--here`;
  - `routine run` of a kind whose `os` is another OS's;
  - `routine queue` of such a kind from a commit on no `origin` branch;
  - `routine queue` whose commit gives the kind an `os` that main doesn't (or main lacks the kind or can't be read);
  - inline run of a commit other than HEAD;
  - busy slot;
  - lease held by someone else;
  - no result for `result find` or `spec verified`;
  - second `review` job for a reviewed branch without `--again`.
- **Land:** `land` from a dirty worktree or off a branch, with `main` ahead of origin, a rebase that conflicts, nothing to land, a spec not `building`, a required result missing or failing, or `main` moving twice.
- **Runners (T3):**
  - `lead start`, `build` or `ideate` with the `t3` runner without a sign-in that serves it;
  - `t3 login` whose ceiling is below `t3_runtime_mode`;
  - `t3 login --status` with no live sign-in.
- **Hub and config:**
  - `hub` whose `[hub] role`, `listen`, `leaf_listen` or `remote`, or `[mesh] token_file`, won't do (the message names the key);
  - `serve` with `[hub] role` set and `[mesh] nats` empty;
  - `config check` of a config this binary refuses.
- **Hosts and sync:**
  - `hosts` or `sync` command on a box with `[hosts]` off or not joined;
  - `sync` of a repo the registry doesn't name or this box has no checkout of;
  - `hosts approve` or `deny` of a hold this box doesn't have;
  - approval outside a terminal, without an approve key in the roster, with no daemon answering or that the daemon didn't level;
  - admin command (`hosts invite|set|revoke`) on a box that isn't the roster's admin or outside a terminal;
  - invite of the admin's name or of a name in the roster without `--replace`;
  - failed join;
  - `init --hosts` without a `config.toml`.
- **Nodes:**
  - reserved or taken path;
  - send to a running thread;
  - executor name or branch in use;
  - `executor remove` of an unmerged branch;
  - `node forget` of an unknown node, of one not `done` or `blocked` (an `idle` one that reported, on another host or runner, or whose thread may be running), of one with a node under it that is neither, or with `--worktree` of a worktree on another host with `[hosts]` off (or that host's refusal), dirty or with its branch unmerged.
- **Registry:**
  - `repos show` of an unknown name;
  - `repos publish` or `remove` on a box that isn't the admin;
  - `repos propose` on the admin box;
  - any of the three when the hub had a newer registry (taken) or one this bmesh can't read;
  - proposal the admin box refused;
  - approval of a proposal the registry has moved past;
  - `repos remove` of a row the registry doesn't have, with no `remote` under `--checkouts`, with `--checkouts` and `[hosts]` off, or while `repos.toml` has unpublished changes.

## Build and test

```
GOBIN=~/.local/bin go install ./cmd/bmesh
TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test ./...
GOOS=windows go vet ./...    # type-checks the Windows-only files a Linux go vet skips
```

A spec asking for repeated runs on a Mac (`go test -count=10`) uses the repo's `soak-darwin` kind: push the branch, then `bmesh routine queue soak-darwin --go` and `bmesh routine wait <job id>`. Only a darwin box takes it; each queue is a fresh run.

Tests never use `/tmp`: `meshtest.Start` fails when `TMPDIR` is `/tmp`.

### Testing

Runner tests drive shell fakes instead of tmux and claude, and a fake T3 (below). These variables swap what bmesh runs or reads:

| Variable | Replaces | In tests |
|---|---|---|
| `BMESH_TMUX_COMMAND` | the tmux binary, when set (split on spaces) | `sh <repo>/testdata/fake-tmux.sh` |
| `BMESH_T3_TIMEOUT` | the t3 runner's 120 s per-call timeout (a Go duration) | |
| `BMESH_CLAUDE_COMMAND` | `[runner] claude` (split on spaces), for routines and the claude runner's tests; also the `claude` a level's plugin refresh runs | `sh <repo>/testdata/fake-claude.sh`; plugin refresh: `sh <repo>/testdata/fake-claude-plugin.sh` |
| `BMESH_SSH_COMMAND` | the `ssh` that `bmesh ideate` reaches another host with | `sh <repo>/testdata/fake-ssh.sh` |
| `BMESH_GO_COMMAND` | the `go` an update builds with | `sh <repo>/testdata/fake-go.sh` |
| `BMESH_UPSTREAM_URL` | `https://<module path>` as where an update without a row and the notice read the module's `main` | a local repo |
| `BMESH_SECURITY_COMMAND` | the macOS `security` the Keychain key store runs | `sh <repo>/testdata/fake-security.sh`, keeping items under `$FAKE_SECURITY_DIR`, printing the access list `$FAKE_SECURITY_ACL` names |
| `BMESH_CODEX_COMMAND` | a level's plugin refresh's `codex`; unset, `codex` runs only when on `PATH` | `sh <repo>/testdata/fake-codex.sh` |
| `BMESH_KEY_STORE` | the key store's default (`keychain` or `file`) when `[hosts] key_store` is unset | `file` |
| `BMESH_CACHE_DIR` | an absolute path for the user cache dir (`os.UserCacheDir`: `~/.cache` or `$XDG_CACHE_HOME` on Linux, always `~/Library/Caches` on macOS), where briefs and a claude session's files go | |

The `cmd/bmesh` tests set, package-wide:

- `BMESH_UPSTREAM_URL` and `XDG_STATE_HOME`, so no test reaches the network or touches the box's notice;
- `BMESH_KEY_STORE` to `file`, and `BMESH_SECURITY_COMMAND`, `BMESH_CLAUDE_COMMAND` and `BMESH_CODEX_COMMAND` to the fakes (a test may set its own over them), so no test reaches the real Keychain and its dialog, or the box's own `claude` and `codex`;
- `BMESH_CACHE_DIR`, so they stay out of the box's cache.

Leave all ten unset outside tests; the real Keychain dialog is checked by hand on a Mac.

The t3 runner, `bmesh t3 login`, the expiry pushes and the `lead start`, `build`, `ideate` and `node forget` tests run against `internal/runner/t3mcp/t3fake`, an `httptest` T3 0.1.0 (the environment document, the MCP OAuth endpoints and the MCP tools below) answering with shapes recorded from a live 0.1.0 server, kept in `testdata/t3v2/`.

**`scripts/seed.sh [seeder args]`** starts a throwaway `bmesh serve` in the background (embedded NATS and HTTP on free loopback ports, routines and ntfy off) with a seeded mesh holding every state the map draws: each node state (`working`, `asking`, `blocked`, `silent`, `held`, `done`, `idle`), a done lead on the shelf, a round and its engineers, routines with done instances, a queued job, leases, a digest and quota lines, and a landed spec with operator steps open (a band row). It prints the map's URL, its check command and a CLI config for poking the mesh, and returns; serve keeps running.

- State lives under `~/.cache/bmesh-scratch/seed/<checkout directory name>` (`BMESH_SEED_DIR` overrides it, `BMESH_SEED_HTTP` the listen address), wiped each run, so two checkouts seed side by side. `seed.pid` there holds serve's pid; a run first stops its checkout's previous one.
- `scripts/seed.sh stop` stops it by that pidfile; never `pkill -f` its command line, which matches the caller's own shell.
- The seeded serve reads the page from the checkout's `internal/web/` on every request, so edits to `index.html`, `map.js` or `map.css` show on reload; a Go change needs a re-run. `BMESH_WEB_DIR=<dir>` does that for any `bmesh serve` (logging `bmesh serve: map page from <dir>`); unset, the page is the one embedded at build.

**`node scripts/check.mjs <url> <width> <height> [png]`** (Node 22 or later), the map's browser check:

1. loads `url` (adding `?check=1`) in headless Chromium at that viewport over the DevTools protocol;
2. waits for the self-check title to show a loaded tree on a live connection (`trees=` at least 1, `conn=` ending `live`);
3. prints the title, and writes a viewport screenshot to `png` if given.

It exits 0, or 1 when no such title showed within 20 s (the last title printed anyway), 2 on a usage or browser error. A hash picks the view (`/#/lead/<path>`, `/#/routines`). Chromium is `$CHROME`, else the newest headless shell or Chromium in the Playwright cache (`$PLAYWRIGHT_BROWSERS_PATH`, `~/.cache/ms-playwright`, `~/Library/Caches/ms-playwright`); stderr names it. `chromium --dump-dom` is no substitute: the page's `/events` stream never ends, so it never returns.

A UI change's check: `scripts/seed.sh`, then the printed `node scripts/check.mjs ... 390 844 shot.png` and the same at `1280 800`, then `scripts/seed.sh stop`.

**Go fixtures** in `internal/meshtest`:

- `Start`: an embedded server.
- `Git(t, dir, args...)`: git with a fixed identity and no global config, hooks or signing; returns its output, trimmed.
- `CommitFiles(t, repo, files)`: writes and commits them, returns the commit.
- `SyncBuffer`: a log sink goroutines and the test share.
- `ForceState(t, c, path, mesh.State{...})`: writes a node's state past the transition table, for a fixture needing `done` or `silent` in one step.
- `QueueJob(t, c, kind, from, job)`: creates the kind's consumer, then sends the job, so it counts toward the kind's depth.

## License

MIT.

<br>

<p align="center"><img alt="" src="docs/assets/logo.svg" width="56"></p>
