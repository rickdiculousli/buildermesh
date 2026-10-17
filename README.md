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

**buildermesh** (`bmesh`) is one Go binary that runs Claude Code sessions on your own machines (boxes) and turns ideas into changes on `main`. You decide what gets built.

Agents write half-page specs; you accept or reject them in plain words. A *lead* session builds an accepted spec in its own git worktree, with *executor* sessions on branches under it. *Routines* run the repo's checks and record each result against a commit's exact files, and `bmesh land` refuses to land a tree those checks haven't passed.

## One mesh, many boxes

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/mesh-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/mesh-light.svg">
    <img alt="Three boxes around one NATS hub: events, jobs and replies move between them; a laptop sleeps and catches up" src="docs/assets/mesh-light.svg" width="100%">
  </picture>
</p>

- **One hub.** `bmesh hub` runs NATS, a message server, inside the binary on an always-on box, over a tailnet such as Tailscale. Boxes share one token; nothing else is installed. A single box needs no hub: `bmesh serve` embeds one.
- **A daemon on every box.** `bmesh serve` wakes quiet leads, runs routines, sends phone notifications and draws the map. A box set to update itself installs each bmesh release; on others, each session opens with a line to run `bmesh update`.
- **Shared checks.** A check that runs apart from the lead, like the review, is queued once for every box that runs it, so a free box takes it. A check that needs macOS runs only on a Mac.
- **Laptops can sleep.** A laptop that wakes takes the jobs it missed, in order; a job waits on the hub for a day.
- **Signed jobs.** Boxes joined with `bmesh init --hosts` share a signed roster, and every job between them is signed and checked. The signing and approval keys sit where no agent can read them (on macOS, behind the Keychain password dialog). A secure box holds an update of its plugins, or of the repo bmesh updates itself from, until you approve it there.

## Proof before main

<p align="center">
  <img alt="A lead runs bmesh land: rebase, verify, carry the review, write the verified line, push main" src="docs/assets/land.svg" width="100%">
</p>

`bmesh land` runs `verify` on the rebased tree and carries the Opus review across the rebase when the diff is unchanged (a changed diff needs a new review), then writes the results into the spec as one line:

```
verified: review ok t:3f2a9c1e m:claude-opus-5-5 c:81d0b7a4; verify ok t:3f2a9c1e
```

`t:` is the landed tree, `c:` the tree the review ran on before the rebase. `bmesh done` refuses until the spec on `origin/main` reads `done` with a merged commit and a recorded result for every entry.

## How a change reaches main

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/lifecycle-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/lifecycle-light.svg">
    <img alt="One spec's way to main: propose, choose, build, verify, land" src="docs/assets/lifecycle-light.svg" width="100%">
  </picture>
</p>

1. **Propose.** `bmesh ideate app` starts an engineer and a UI/UX engineer; each lands three to five ranked specs on `main`, and with an [ntfy](https://ntfy.sh) topic set the list reaches your phone.
2. **Choose.** Reply in plain words: `bmesh choose-ideas app "1 and 3 yes, 2 smaller, 4 no, 5 later"`. The verdicts land on `main`; a word that isn't a verdict, like `smaller`, marks the spec `adjusting`, with your words kept for the rework.
3. **Build.** `bmesh build app 012` checks that `main` passes its verify commands, marks the spec `building` and starts a lead.
4. **Verify.** Routines run `verify` (the repo's tests) and, for larger changes, `review` (Opus, in a session that never saw the change).
5. **Land.** `bmesh land` rebases onto `origin/main`, requires a result for the tree it lands, writes the `verified` line, marks the spec `done` and fast-forwards `main`.

A lead reports problems outside its spec as feedback. When a batch started from a chat is done, the plugin's `build` skill starts builds for the quick fixes without asking you, writes the rest up as proposed specs for your review and tags the next version with `bmesh release`. In a chat, say "proposals for app", "1 and 3 yes", "build what's accepted in app" or "what's building".

## Get started

You need Go 1.26 or later, git, Claude Code (`claude` on your `PATH`, signed in) and a repo with a `main` branch. The `claude` runner, which `init` picks, runs Claude Code with its permission prompts off in worktrees bmesh makes.

```sh
go install github.com/rickdiculousli/buildermesh/cmd/bmesh@latest
claude plugin marketplace add rickdiculousli/buildermesh
claude plugin install buildermesh@buildermesh
cd ~/repos/your-repo && bmesh init   # one box or several; registers this repo
bmesh serve                          # hub, daemon and map; keep it running
```

Then put the repo's verify commands in its row of `repos.toml` (`bmesh init` prints the path):

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

`serve` as a service (`bmesh init --service`), Windows, several boxes and every prompt and line of `init` are in the [Quickstart](#quickstart).

## The map

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/map-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/map-light.png">
    <img alt="The bmesh map: leads with their executors, a proposal round, a held lead, the routines column and the needs-you band" src="docs/assets/map-light.png" width="100%">
  </picture>
</p>

`bmesh serve` draws the mesh live at `http://127.0.0.1:3774/`: leads with their executors, rounds with their engineers, each check with its queue, and at the top what needs you: a session that is blocked, asking or silent.

## What's inside

- **`bmesh`**: the CLI, daemon, map server and NATS hub in one binary. `bmesh serve` runs on every box; `bmesh hub` is the hub of a multi-box mesh, or a local relay to it for a laptop that sleeps.
- **Runners**: `t3` (a T3 Code thread), `claude` (headless `claude -p` turns) or `tmux` runs a lead's session.
- **The plugin**: twelve Claude Code skills, seven for your chat and five that leads, executors, round engineers and bmesh's fix lead follow.
- **Specs**: `docs/proposals/NNN-slug.md` in each repo; the frontmatter holds its status, builder, merged commit and `verified` line.

<br>

## Reference

Below is every command, flag, config key, output and refusal. How the mesh works inside (subjects, buckets, record shapes, signing) is in [docs/internals.md](docs/internals.md). bmesh's messages cite this reference by section name.

<table>
  <tr>
    <td valign="top" width="33%"><b>Setting up</b><br><a href="#quickstart">Quickstart</a><br><a href="#upgrading">Upgrading</a><br><a href="#several-boxes">Several boxes</a><br><a href="#runners">Runners</a><br><a href="#hosts">Hosts</a><br><a href="#config">Config</a></td>
    <td valign="top" width="33%"><b>Building</b><br><a href="#proposal-rounds">Proposal rounds</a><br><a href="#builds">Builds</a><br><a href="#feedback">Feedback</a><br><a href="#releases">Releases</a><br><a href="#the-ledger">The ledger</a><br><a href="#executors">Executors</a><br><a href="#finished-nodes">Finished nodes</a></td>
    <td valign="top" width="33%"><b>Checking and landing</b><br><a href="#routines">Routines</a><br><a href="#landing">Landing</a><br><br><b>Lookup</b><br><a href="#names">Names</a><br><a href="#exit-codes">Exit codes</a><br><br><b>Working on bmesh</b><br><a href="#build-and-test">Build and test</a><br><a href="#license">License</a></td>
  </tr>
</table>

## Quickstart

A box with Go and Claude Code (`claude` on PATH) gets a working single-box mesh in four commands:

```
go install github.com/rickdiculousli/buildermesh/cmd/bmesh@latest   # or, from a checkout, GOBIN=~/.local/bin go install ./cmd/bmesh
cd ~/repos/your-repo && bmesh init     # asks single or multi-box, writes config.toml, registers this repo, picks the claude runner, says what it set
bmesh serve                            # the hub (embedded on a single box), the daemon and the map at http://127.0.0.1:3774/; keep it running
bmesh repos                            # the registry; then bmesh scaffold your-repo, bmesh ideate your-repo, bmesh choose-ideas, bmesh build your-repo 001, bmesh mesh-status
```

Recommended, optional: the [pstack](https://github.com/rickdiculousli/pstack-claude) and `method` Claude Code plugins. The node skills name `architect`, `attack-the-premise`, `tdd`, `figure-it-out` (before any `bmesh ask`), `blast-radius`, `control-ui`, `exhaust-the-design-space` and `method:writing-plans` at fixed moments (see The skills) as "if `pstack:<skill>` is in this session's skill list, run it; otherwise &lt;the written default&gt;". A plugin installed but not invocable in the session counts as absent; the binary reads neither.

#### `bmesh init`

It prompts for each setting not passed as a flag (`--role`, `--hub`, `--token-file`, `--runner`, `--no-repo`), reading stdin (an empty line, or no stdin, takes the default) so a script can drive it; the multi-box answer adds a prompt for this box's part, hub, leaf or client (see Several boxes).

**`--service`** also writes the unit that keeps `bmesh serve` running (a systemd user unit on Linux, a launchd agent on macOS; with `[hub] role` set, one for `bmesh hub` too) and prints how to enable each; on Windows it writes nothing and prints two commands for `cmd` (below). Neither unit restarts on exit 78, a refused config (see Config); launchd has no per-code stop, so a bad config restarts serve at most every 5 minutes.

A unit already there with the same bytes is left alone (`unchanged <file>`); one that differs (tuned by hand, or by an older bmesh) is kept, gets no enable command, and the new body goes beside it as `<file>.new`: `kept <file> (it differs from what init writes); the new one is <file>.new: diff them, and move it over to take it`. `--force` replaces it.

**The setup flag** `<config dir>/.setup-complete` is written last. Any other command but `help`, `version`, `hosts --here` and `update --notice` finding neither it nor a `config.toml` prints one line and runs `init` first at a terminal; outside one it is refused (exit 3, ``run `bmesh init` ``).

**On an existing `config.toml`**, `init` edits nothing (nor a registry row it has) without `--force`, but still writes the flag and, with `--service`, a missing unit.

**The repo it registers** must have a `main` branch: `origin`'s as last fetched, else the local one (an unborn `main` from `git init -b main` counts); nothing is fetched. Refused (exit 3) before anything is written:

- a repo without one: `bmesh: <path> has no main branch; bmesh builds on main (rename the branch, or see https://github.com/rickdiculousli/buildermesh/issues/1; --no-repo sets up without it)`;
- a name whose node token another row already has (see Names).

**What it prints.** `config.toml` comments each key it writes. Last, before `Next:`, every setting in effect (defaults included) with its value, meaning and key; on an existing config, the same block for what is there:

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

`[mesh] nats` stays `""`: `serve` embeds the hub on `[mesh] listen` (default `127.0.0.1:4222`), and every other command connects to it there. With nothing listening, a command fails with `bmesh: no hub at 127.0.0.1:4222: is bmesh serve running?`. A `listen` on port 0 (`serve` picks a free port) is refused, since the other commands can't know which port.

#### Per OS

- Config dir (`os.UserConfigDir`): `~/.config/bmesh` on Linux, `~/Library/Application Support/bmesh` on macOS, `%AppData%\bmesh` on Windows; an existing `~/.config/bmesh` is kept on any OS. The embedded hub's store: the OS cache dir (`os.UserCacheDir`). See Config.
- A configured command line runs through `sh` on Unix, `cmd /C` on Windows (every argument quoted; one holding a quote or `%` refused). Nothing assumes tmux. Linux and macOS are run; Windows is built and vetted on every change (`GOOS=windows go vet ./...` is in verify) and run by users.

**Keeping serve running.** `bmesh serve --restart` supervises `bmesh serve` on any OS (a Linux box without systemd too): it runs serve as a child and starts it again after every exit but 78:

- Exit 78 (a refused config): it logs `serve exited 78 (config check failed); not restarting: fix the config and start it again` and exits 78 itself.
- Otherwise after 5 seconds, or 60 once five runs in a row lasted under a minute each, until a run outlasts that.

Ctrl-C or SIGTERM stops the child, then the supervisor; on Windows the jobs the child held go back to the inbox when their ack wait runs out.

Its own lines (`<time> bmesh serve --restart: started <exe> (pid N)`, `serve exited (exit N)`, `starting again in 5s`) go beside the child's output: stderr on Unix; on Windows (no journal) `<state dir>/serve.log` (state dir: see Config), moved to `serve.prev.log` (replacing it) before it passes 10 MB.

`--detach` (Windows only, with `--restart`; alone a usage error, exit 2) starts the supervisor again with no window, prints `bmesh serve: supervisor started with no window (pid N); log in <path>` and returns at once.

**Windows.** `bmesh init --service` ends with three lines for `cmd` (not PowerShell, which reads the quotes differently):

```
schtasks /Create /TN buildermesh /SC ONLOGON /RL LIMITED /F /TR "\"C:\Users\you\go\bin\bmesh.exe\" serve --restart --detach --config \"C:\Users\you\AppData\Roaming\bmesh\config.toml\""
schtasks /Run /TN buildermesh
rem in cmd: bmesh serve then starts at every logon with no window; its log is C:\Users\you\.local\state\bmesh\serve.log
```

The first line makes a task that runs the supervisor at every logon with your rights; the second starts it now. The config path goes in `--config`: a task sets no environment. A path with spaces passes. Refused:

- a path holding a character `cmd` reads there (`"`, `%`, `&`, `^`, `|`, `<` or `>`): `init --service: "<path>" cannot be passed through cmd (it holds one of " % & ^ | < >); move it and run init --service again`;
- a task command over the 261 characters `/TR` takes.

The task's process returns at once (`--detach`), so no console window stays open and the task's run time limit never applies. `taskkill /IM bmesh.exe /F` stops it (and every other bmesh on the box); `schtasks /Delete /TN buildermesh /F` removes the task.

Internals: [docs/internals.md](docs/internals.md#quickstart)

### Updates

**What a box follows.** `[update] track` (default `release`) names the commit every update path (the daemon's self-update, the notice, `bmesh update`) goes to: the target. The code head is the newest commit on `main` that changes the repo's code (its row's `code`, see repos.toml), the commit `build` verifies and `release` tags.

- `release`: the newest release, the highest `vX.Y.Z` tag in `main`'s history (see Releases; a pre-release such as `v0.2.0-rc1` is passed over); with no release tag yet, the code head, logged once by the daemon: `update: no release tag on main yet; following its code head`.
- `latest`: the code head. A commit outside the code (a dispatch, review or `done` record; with `code` set, a README, a document or art) never causes a rebuild.

A binary at the target, or with it in its history (built from `main`, or by `update --latest`, past the newest release), is current and never moved back. One the target doesn't contain (a rollback, or another line) is left alone: the daemon logs it once; `bmesh update` refuses it (exit 3).

A binary installed as `go install <module>/cmd/bmesh@vX.Y.Z` names a release, which every path first resolves to its commit on `main`. A release `main` lacks: the daemon logs once `update: this binary's release is not on main; left alone`; `bmesh update` installs the target as for an unknown revision.

Updates name both ends by release: `v0.1.0` for a tagged commit, `v0.1.0+2 (<rev8>)` two commits past it, `<rev8>` with no release in its history.

**`bmesh version`** prints the commit the binary was built from, `bmesh <rev8> <vcs.time> [modified]`; `bmesh unknown` when it has none (a `(devel)` build).

- A tagged module install (`go install …/cmd/bmesh@v0.2.0`, or `@latest` once the module has a release, which go resolves to its tag) prints the release alone, `bmesh v0.2.0`.
- A self-update at or past a release tag (see Releases) prints the release first: `bmesh v0.1.0 (9a5e181a 2026-10-16T22:38:04Z)` on the tagged commit, `bmesh v0.1.0+3 (<rev8> <time>)` three past it.

Each daemon's digest title carries its revision (`bmesh <host> <rev8>: …`).

**The daemon's self-update.** With `[update] repo = "<registry name>"` (default `""`: off) the daemon keeps itself current: once a minute (it needs `[runner] checkouts`) it compares the target on that repo's main checkout with the binary at its path. When the target is ahead, it:

1. Builds `./cmd/bmesh` at the target and runs the build's `version`; one that doesn't name the target's commit is refused, `update: built binary reports <x>, not <new8>; keeping this binary`, logged once per target.
2. Checks the new binary's config and upgrade walk; when either needs the operator, holds the update (see Upgrading).
3. Moves the running binary to `bmesh.prev` beside it and the build to its path, and logs `update: v0.1.0 -> v0.2.0`.
4. Drains: stops taking jobs, kills every running job, and hands every job it held or ran back to the inbox for the restarted daemon (see Jobs).
5. Stops serve and starts the new binary with the same arguments; on Windows `serve --restart` starts it 5 seconds later.

A box on `release` restarts once per release, on `latest` once per code landing; clients reconnect to an embedded hub as after any restart.

- A failed build is logged once per target; the daemon runs on.
- On Windows a `bmesh.prev` that can't be removed (the supervisor's own image) is moved to `bmesh.prev.old` first, so you may see that file beside the binary.
- A binary with neither a revision nor a release tag never updates.
- A daemon that finds its last update never started (the new build crashed at start; `bmesh.prev` was put back by hand) logs it and pushes `bmesh update failed on <host>` (priority `high`) through `[notify]`.
- A Windows daemon not under `serve --restart` refuses `[update] repo` (its exit would stop it for good), logging once at start `update: [update] repo needs bmesh serve --restart on Windows, which starts the new binary (init --service sets it up); off`; the notice still runs from the row.

**`bmesh update`.** `bmesh update [--repo R] [--latest] [--answer KEY=VALUE]... [--defaults] [--apply [--yes]] | --plan [--json]` does the same build and swap on demand, on any box, daemon or not. `R` defaults to `[update] repo`, else to the registry row whose `go.mod` declares this binary's module. It builds from a bmesh-owned checkout of the repo's main, never the operator's; on Windows a `bmesh update` run by hand can race the daemon's own update.

It goes to the target; `--latest` goes to the code head this once, whatever `track` says, to take a fix before the batch's release. The next `update` follows `track` again without going back: past the newest release it prints `ahead of v0.2.0, up to date`.

It prints `<old> -> <new>` (`v0.1.0 -> v0.2.0`; under `--latest`, `v0.2.0 -> v0.2.0+2 (<rev8>)` two commits past the release) or `up to date`. A build whose `version` doesn't name the target's commit is refused (exit 3): `bmesh: update: built binary reports <x>, not <new8>; keeping this binary`.

**No repo row** (no `--repo`, `[update] repo` or own-module row: a Quickstart install): it installs from the module's `main` with `go install` instead, keeping `bmesh.prev` as above. After that swap it prints `<old> -> <new>` then `update the buildermesh plugin too: claude plugin marketplace update buildermesh && claude plugin update buildermesh@buildermesh (then restart Claude Code)`, since the skills name commands only a matching binary has. A binary that doesn't say its module is refused: `no repo to update from: pass --repo or set [update] repo` (exit 3).

The new binary walks the upgrade notes before and after the swap (see Upgrading); a refused config or an unanswered ask keeps the old binary. Once the binary is current (`up to date`, `ahead of …`, or after a swap from either source) `update` removes the notice file (below).

**Restart line.** Then, when this box's daemon runs another revision than the binary now at the path, `update` prints `this box's daemon still runs <old8>; restart it so it runs <new8>: <command>`, `<command>` restarting the unit `init --service` writes:

- Linux: `systemctl --user restart bmesh`;
- macOS: `launchctl kickstart -k gui/$(id -u)/com.buildermesh.bmesh`;
- Windows, whose supervisor runs detached from the logon task: `taskkill /F /IM bmesh.exe & schtasks /Run /TN buildermesh` (for `cmd`; it stops every `bmesh.exe`, supervisor included, then starts the task again);
- another OS: `stop bmesh serve and start it again`.

No hub, no daemon on this box, or a daemon of unknown revision: no line. A daemon run by hand gets a command that doesn't apply; stop and start it yourself.

`update` never restarts the daemon: a restart kills its running jobs; only its own drain (above) hands them back. A daemon with `[update] repo` takes the target by itself within a couple of minutes; one without (a Mac's) runs the old code until restarted.

**The hint.** When this box's daemon (another box's, if this box runs none) runs a revision other than this binary's, every command that connects to the hub ends with one stderr line (after its output and error; exit code unchanged):

- `bmesh: k16 runs 1a2b3c4d, this is 9e8f7a6b; run bmesh update`;
- or, when this box's own daemon is the older of the two: `bmesh: this box's daemon runs 1a2b3c4d, this binary is 9e8f7a6b; restart the daemon: <command>`, `<command>` as in the restart line.

No hint for a binary or daemon of unknown revision, for commands that reach no hub (`help`, `version`, `init`, `repos`, `spec list|next-id|set|template`, `scaffold`), or for `update`, which prints its restart line instead.

**The notice.** A daemon not self-updating from `[update] repo` checks the target every `[update] check_every` (default `6h`; `0` is off; it needs `[runner] checkouts`) and, when the target is ahead of the binary at its path, writes one line to `<state dir>/update.txt`:

- for a release: `bmesh: v0.2.0 is out (you run v0.1.0: 046, 047); run bmesh update, then update the buildermesh plugin`;
- for a code head (`track = "latest"`, or no release yet): `bmesh: 9 commits behind main (landed since: 032 The Quickstart works as written, 033 …); run bmesh update, then update the buildermesh plugin`, counting only commits that change the repo's code, and listing the specs landed since, oldest first, each with its title (the id alone without one).

It removes the file when the binary is current, ahead, or outside the target's history (another line, or unknown revision); with no check (`check_every = 0`, self-updating from a row, or no checkouts root), at start. A failed fetch is logged and tried again at the next check.

`bmesh update --notice` only prints that file: no network, no `init`, nothing (exit 0) when it is absent. The plugin's session hook runs it after `hosts --here` (see The skills); `mesh-status` prints the line last but for the hub line (see Several boxes).

**Installing from upstream.** With `[update] upstream = true` (and no `[update] repo`) the check also installs: when the target is ahead it runs the same `go install` and swap as `bmesh update`, then drains and restarts as the self-update from a row does.

- A failed or refused install is logged once per target (`update: install failed; keeping this binary`), leaves the notice written, and is tried again at the next check (the module proxy can lag a fresh commit by minutes).
- On Windows it runs only under `serve --restart`; without it the daemon logs once at start `update: [update] upstream needs bmesh serve --restart on Windows, which starts the new binary (init --service sets it up); notice only` and stays on the notice.
- A binary without a revision or without a path to replace stays on the notice too, logged once.
- `check_every = 0` turns it off.

Internals: [docs/internals.md](docs/internals.md#updates)

## Upgrading

**From before v0.1.0: start over.** v0.1.0 (tagged at `61587e32`) predates upgrade notes; its notes, `docs/upgrading/v0.1.0.toml`, written after it, are the one exception to stepping a box forward: an install from before v0.1.0 is uninstalled and set up again, NATS included. In order:

1. Check that no lead or executor is live (`bmesh mesh-status --all`); finish or forget (`bmesh node forget <path>`) any that is.
2. Stop and remove the `bmesh` and `bmesh-hub` services and any `nats-server` service, hub or leaf (systemd user units, launchd agents, the Windows logon task); stop a `bmesh serve` or `nats-server` run by hand yourself.
3. Remove the `nats-server` installed for bmesh, its `<config dir>/nats.conf` and the JetStream `store_dir` that file names.
4. Move the config dir aside as `<config dir>.pre-0.1.0` (keep its `repos.toml`); remove the state dir and the embedded hub's store.
5. Run `go install github.com/rickdiculousli/buildermesh/cmd/bmesh@v0.1.0` (or the release you go to; `bmesh update` has already done this when it walks the notes).
6. On the hub box first (several boxes, role hub) run `bmesh init --service`, then `bmesh init --hosts --admin`.
7. On every other box run `bmesh init --service` (leaf or client) and `bmesh init --hosts` with an invite from the admin box (`bmesh hosts invite <box>`).
8. Put the kept `repos.toml` back and publish it from the admin box (`bmesh repos publish`).

A fresh start loses the old JetStream store's results and events, and the roster and its keys; every spec's `verified` line keeps its record in git. Every later release is incremental: its notes hold only the steps an install at the release before needs.

**The notes walk.** `bmesh update` walks a box through the notes of every release between the binary it runs and the one it installs, oldest first (from v0.1.0 to v0.3.0: v0.2.0's notes, then v0.3.0's). Notes come from `docs/upgrading/<tag>.toml` on the update repo's `main`, not the tag, so notes written after a tag still apply (the format: Upgrade notes, under Releases). A binary without v0.1.0's commit in its history predates it; its walk starts with v0.1.0's start-over.

The new binary does the walking, since the old one may predate walks. Once built, before the swap, it:

- **checks the config**: loads this box's `config.toml` and `repos.toml` with the new binary (`bmesh config check`). A refused config keeps the old binary, `bmesh: config check failed: config <path>: [runner] t3_server is empty: ...` then `bmesh: keeping this binary` (exit 3), so a box never restarts into a binary that won't load its config. A start-over walk skips the check: it moves the config aside.
- **answers the asks**: at a terminal, one line each, `track: boxes now update to releases, not every landing. Follow releases or every landing? [release]/latest`; Enter takes the default, an answer outside the choices is asked again. Without a terminal every ask needs `--answer <key>=<value>` (the key as the ask names it, `[update] track`, or `track`) or `--defaults`, else nothing is written and `update` refuses, `1 ask waits on an answer ([update] track): pass --answer <key>=<value> for each, or --defaults; nothing changed` (exit 3), keeping the old binary. Answers go into `config.toml` (the old file kept as `config.toml.prev` and put back when the new binary then refuses it) or, for a registry ask, into every row of `repos.toml` (kept as `repos.toml.prev`), which the admin publishes (`bmesh repos publish`) or another box proposes.

**The steps.** The walk opens with `<from> -> <to>: 1 ask, 1 step` (a start-over adds `<release> starts this box over; it loses ...`). After the swap and the usual `<old> -> <new>`, the new binary runs the steps. Each run or check step for this box (by its `os`, and its `when` against the config as it now is; a `when` that doesn't hold prints `skip <release>#<n>: ...`) is printed as `v0.2.0#2 restart bmesh-hub: ` plus its command and run in `sh -c` (`cmd /C` on Windows) with `BMESH_CONFIG`, `BMESH_CONFIG_DIR`, `BMESH_STATE_DIR`, `BMESH_STORE` (the embedded hub's store, as the config names it), `BMESH_FROM` and `BMESH_TO` set.

- With `--apply` it runs without asking.
- At a terminal without `--apply`, each step asks `Run it? [Y/n]`.
- With neither, the walk stops there: `the steps from v0.2.0#2 wait: run bmesh update --apply (with --yes for the ones that ask first)` (exit 0).
- A `confirm` step asks `Run it? [y/N]` at a terminal and needs `--yes` without one (`step v0.1.0#1 asks first: pass --yes to run it`, exit 3).
- A check step's output must show its `expect`.
- A failed step stops the walk: `step v0.3.0#1 failed: ...; fix it and run bmesh update again, which goes on from there` (exit 1).

`bmesh update` run again, now on the new binary and `up to date`, resumes after the last step done. A finished walk ends `at v0.2.0`.

**Driven by an agent.** `bmesh update --plan --json` prints the walk and changes nothing: `{"from","to","releases","start_over","config","asks","steps","confirm","plan"}`, `config` being `ok`, the refusal, or `start-over`, and `plan` every step for this box's OS with its `id` (`v0.2.0#1`), `release`, `kind` and fields, `done`, its `answer` once given, and `applies` (whether its `when` holds now). Without `--json`: `<from> -> <to>: 1 ask, 1 step` and a line per step. Then `bmesh update --defaults --apply --yes` (or `--answer` for each ask) walks it without a terminal.

`bmesh update --walk --from <revision or release> [--to <revision>] [--dir <repo>]` walks a range by hand with this binary (`--to` defaults to this binary's revision, `--dir` to the repo `update` reads main from); `--plan` takes the same `--from`, `--to` and `--dir`.

**Where a box stands.** `bmesh init` and every finished walk record the box's release. A binary swapped in without a walk (by a binary up to and including v0.1.0, whose `update` and self-update know no walk, or by the daemon below) finds at its first start that notes are pending when its release is past the recorded one (v0.1.0 with no record):

- `serve` prints, and every command run at a terminal (and `bmesh update --notice`, which the session hook runs) ends with, `bmesh: upgrade notes pending since v0.1.0: run bmesh update`.
- The daemon logs it and pushes it once through `[notify]`.

That `update`, finding the binary current, walks the pending notes from the recorded release, else from the revision the swap replaced, else from v0.1.0. A box from before v0.1.0 that updated with its own old code is walked from v0.1.0, never through the start-over, since nothing recorded where it started; hence the start-over above, and `serve`'s config check at start (exit 78, see Config).

**The daemon's self-update** (`[update] repo`, or `upstream`) never walks: before the swap it runs the new binary's config check and plans the walk.

- The daemon logs and pushes a refused config (`update to v0.2.0: config check failed: ...; keeping this binary`) and a walk with an ask or `confirm` step (`update to v0.2.0 waits on bmesh update (2 asks)`), rather than guessing an answer. Either keeps the running binary, is written as the notice (`bmesh update --notice`), and is acted on once per target until `bmesh update` takes it.
- A walk with only plain steps is swapped in and left pending for the new binary to report.
- One with nothing to do records the new binary's release.

**The hub keeps up.** `bmesh hub` keeps its starting binary until its service is restarted; `bmesh update` (after its other lines), `bmesh hosts --here` and `mesh-status` then print `hub runs v0.1.0, serve v0.2.0: restart bmesh-hub` (see Several boxes). A release's notes can restart it with a `run` step.

Internals: [docs/internals.md](docs/internals.md#upgrading)

## Several boxes

A mesh spans boxes through one NATS hub. A single box embeds it in `bmesh serve` (`[mesh] nats = ""`). With several, `bmesh hub` runs the hub on one box and a leaf on any other that wants one: nats-server v2.15.1 as a library, configured from `[hub]` (see Config); no `nats.conf` to write. Every box still runs its own `bmesh serve`.

`bmesh hub` is a separate process because the daemon restarts on every update (see Updates) and would drop every box each time; the hub restarts only when you restart its service.

**Stale hub.** When `bmesh hub` runs an older binary than `bmesh serve`, `bmesh hosts --here` and `mesh-status` print `hub runs v0.1.0, serve v0.2.0: restart bmesh-hub` (a release when stamped, else eight characters of revision).

**Setup.** With the multi-box answer, `bmesh init` prompts for this box's part, **hub**, **leaf** or **client** (`--role`), and writes `[mesh]` and `[hub]`. With `[hub] role` set, `bmesh init --service` also writes the hub's unit and prints how to enable it:

| Manager | Hub unit | Enable |
|---|---|---|
| systemd | user unit `bmesh-hub.service` (`ExecStart=<bmesh> hub`, `Restart=always`, `RestartSec=5`, `RestartPreventExitStatus=78`) | `systemctl --user daemon-reload && systemctl --user enable --now bmesh-hub` |
| launchd | agent `com.buildermesh.bmesh-hub` (`KeepAlive`, its log `~/Library/Logs/bmesh-hub.log`) | `launchctl bootstrap gui/$(id -u) <plist>` |
| Windows | none | run `bmesh hub` yourself |

Units restart always, since the tailnet address may not exist at boot; a differing unit is kept with a `.new` beside it (see `bmesh init`).

**The hub** runs on an always-on box, on its tailnet address, never a LAN or public one: the tailnet is the boundary, the shared token the only auth. init prompts for the address (`<tailnet ip>:4222`, default this box's in `100.64.0.0/10`), the leaf port (`<the same ip>:7422`) and the token file (default `<config dir>/nats.token`, made if missing, mode 0600), and writes:

```toml
[mesh]
nats = "nats://<tailnet ip>:4222"
token_file = "<config dir>/nats.token"
[hub]
role = "hub"
listen = "<tailnet ip>:4222"
leaf_listen = "<tailnet ip>:7422"
```

`bmesh hub` serves clients on `listen` with the token and leaves on `leaf_listen` (user `leaf`, the token as password). It stores JetStream in `[hub] store` within `max_file_store` and `max_memory_store` (defaults in Config), is named `[host] name`, logs on stderr (the journal under systemd) and stops on SIGTERM or Ctrl-C.

**A box joins** as:

- **Client:** init prompts for the hub's URL and the same token file (`--hub`, `--token-file`; `--hub` alone makes a client) and writes them to `[mesh]`.
- **Leaf:** init prompts for the hub's leaf URL and writes:

```toml
[mesh]
nats = "nats://127.0.0.1:4222"
token_file = "<config dir>/nats.token"   # the hub's token, copied here (mode 0600); init says so when it isn't there yet
[hub]
role = "leaf"
listen = "127.0.0.1:4222"
remote = "nats://<hub tailnet ip>:7422"
```

A leaf is a local server on `127.0.0.1:4222` that relays to the hub at `remote`, so a box that sleeps or roams always has a server to reach, and it reconnects to the hub by itself.

**Refusals.** `bmesh hub` refuses to start (exit 3), naming the key, with:

- `[hub] role` unset;
- `listen` (or a hub's `leaf_listen`) unset or on every interface (`0.0.0.0`, `::` or no host);
- a leaf without `remote`;
- `[mesh] token_file` unset, missing or empty.

`bmesh serve` with `[hub] role` set and `[mesh] nats` empty is refused too (exit 3): it would embed a second server beside `bmesh hub`.

**Your own `nats-server`** works instead of `bmesh hub`, from a `nats.conf` holding the token; bmesh needs only `[mesh] nats` and `token_file`.

- **Hub:** `listen`, `authorization { token }`, `jetstream { domain: hub, store_dir }` and `leafnodes { listen, authorization { user: leaf, password: <token> } }`.
- **Leaf:** `leafnodes { remotes = [ { url: "nats://leaf:<token>@<hub>:7422" } ] }`, no `jetstream`.

**The token** is one random value, the same on every box, in `[mesh] token_file`, owner-only. Boxes sharing the hub see each other's nodes; to act on each other (no SSH between them) they join the admin's roster (see Hosts): `bmesh init --hosts --admin` on your box, `bmesh init --hosts` with the admin's one-time code (`bmesh hosts invite <name>`) on each other box, then restart `bmesh serve`. A **secure** box gets an approve key: what other boxes ask of it waits for your approval at its screen. Mark secure the box whose checkouts and plugins matter most, usually the laptop you type on.

**The admin and approve keys** are the two keys no agent can use; their commands run at the box's own screen, never over SSH (see Hosts: Keys).

**`secure` covers** roster ops (`level` holds and the approve key, see Hosts), not routines: a box with routines on runs any pushed commit's kind command for any mesh member. Routines are on by default (`[runner] checkouts`, default `~/.bmesh/checkouts`; `""` turns them off); `init --hosts` prompts for them, default no on a secure box. A secure Mac meant to run `os = "darwin"` kinds answers yes.

**Sleeping boxes.** A sleeping laptop drops off the mesh and returns on its own: its leaf reconnects, its daemon detects the wake, re-reads the roster and takes the jobs it missed, in order (an expired one is answered and logged, not pushed; see Hosts). Until then `bmesh hosts`, every box's session line and `bmesh hosts ping` show it `asleep`. Keep the hub on a box that doesn't sleep.

**Which box a session is on.** With `[hosts]` on, the plugin's session hook opens every session with the line the daemon writes each minute, e.g. `this box is hub-box; secure: no; on the mesh: laptop (asleep 2h, secure, admin)`. A box's notes for sessions, `<config dir>/notes.md`, travel signed in its presence; `bmesh hosts --notes` prints every box's (see Hosts).

Internals: [docs/internals.md](docs/internals.md#several-boxes)

## Runners

`bmesh lead start --repo R --spec NNN --brief FILE [--model M] [--effort E] [--title T] [--path P]`

Starts a lead through `[runner] kind` on `--model` (default `[models] lead`) at `--effort` (`low`, `medium`, `high`, `xhigh` or `max`; default `[models] lead_effort`, `""` the runner's own), registers node `<repo>-<NNN>` (`<repo>` the registry name's node token, see Names) as `idle`, with its model and effort, and prints `{"path","thread","branch","worktree"}`.

- **Paths:** a live node (neither `done` nor `blocked`) at the path is refused (exit 3, `refused: path <p> is taken (state <s>); use --path <p>-2`); a finished one is reused, reset to `idle`. A `--path` whose first token is reserved (see Names) is a usage error (exit 2); one of the form `routine.<kind>` is refused (exit 3, `refused: path <p>: the first token routine is for the routine pool`); a bare `routine` is a usage error like the other reserved tokens.
- **Timeouts:** the start runs under the runner's timeout, not the CLI's one-minute deadline; a timeout, or a registration failure after it, names the thread id (and branch and worktree when known), or with `t3`, whose threads T3 names, the branch: `start timed out; a thread on branch bmesh/<id> may exist`.
- **Lead cap:** with `[runner] max_leads` set, `lead start` refuses (exit 3, `bmesh: N leads already working on this host`) once that many live leads are registered here; a lead counts once its runner has started it, and a finished lead's session (a tmux window, a T3 thread) never counts though it holds memory until you close it. `scripts/memcap.sh N` prints the peak memory of N tmux leads, each with three executors.
- **Messages:** `bmesh send <path> <text>` always publishes to the node's inbox and prints `sent <id>` (`duplicate <id>` for a retried id); it never checks the thread. Only the daemon's wake checks: with the `t3` runner, a thread mid-turn gets no wake that minute, logged `wake: not delivered, thread is running`, and the next poll tries again (the `claude` runner queues it instead).

Each runner starts a lead's thread, reports whether it is running, delivers wakes and stops it. Pick one with `[runner] kind`:

#### `t3`

A thread in the box's T3 Code 0.1.0 at `[runner] t3_server` (default `http://127.0.0.1:3773`; `""` is a config error naming the key), signed in as T3 sign-in says. A T3 before 0.1.0 is refused at start, status, send and stop with `bmesh: T3 <version> at <t3_server> speaks protocol 1; bmesh needs T3 0.1.0 or later`; a thread id not of the form `mcp:<uuid>` is "no such thread".

**Runtime mode.** Every thread starts in `[runner] t3_runtime_mode`: `auto` (default; Claude Code's `auto` permission mode, where a classifier denies risky actions instead of asking anyone) or `full-access` (`bypassPermissions`). `approval-required` and `auto-accept-edits` are config errors: they stall an unattended lead.

**What you see.** The thread is in the T3 project whose `workspaceRoot` is the repo; registering a missing project needs a full-access sign-in, so with `auto`, add the repo as a T3 project first. T3 names the thread (`mcp:<uuid>`) and the worktree (`<T3 home>/worktrees/<repo>/bmesh-<thread id>`), on branch `bmesh/<thread id>`. The daemon logs, once per change, a thread waiting on a question or approval (`t3: <path> waits on a question or approval`) and one that hit a usage limit (`t3: <path> hit a usage limit; it resets at <time>`). Interrupting the thread in T3 stops the node (see Stopping a node); a run T3 cancelled on restart or that crashed is not a stop, so a dead session's lead is still woken.

#### `tmux`

A `claude [--model M] [--effort E]` session in window `<thread>` of tmux session `<tmux_session>` (created when missing), in a worktree bmesh adds at `<worktrees>/<repo>/<thread>` on branch `<thread>`, with the CLI's `BMESH_CONFIG`. It answers a fresh worktree's first dialogs and types the brief. It can't tell whether claude is mid-turn or you stopped it. `stop` presses Escape in the window.

#### `claude`

No T3, no tmux: headless `claude -p` turns in a worktree bmesh adds at `<worktrees>/<repo>/<thread>` on branch `<thread>` (a round's thread: the checkout bmesh made), with `--dangerously-skip-permissions`, `--model` and `--effort` on every turn. Its state, including each turn's `turn-<n>.err`, is in `<user cache dir>/bmesh/claude/<thread>/`; read it to debug a lead. The session id is the thread id, so a UUID (`lead start` passes one). Turns inherit the CLI's environment, so `BMESH_CONFIG` reaches the lead. A wake sent mid-turn is queued and delivered as the next turn once the current one ends; `stop` drops queued wakes and kills the turn. It can't tell whether you stopped a turn.

Internals: [docs/internals.md](docs/internals.md#runners)

### T3 sign-in

The `t3` runner signs in to T3 as an MCP client. Only T3's OAuth sign-in gives a session a runtime-mode ceiling above `approval-required`, picked by the operator when approving; so a person approves each box's sign-in, and again every 30 days (the token has no refresh).

`bmesh t3 login [--server URL] [--callback-port 52299] [--timeout 1h]`

1. Registers bmesh as a public OAuth client with T3.
2. Prints the authorize URL and how to approve it from a phone or laptop: `ssh -N -L <port>:localhost:<port> <box>` for the callback, plus the server's port when it listens on loopback (`ssh -N -L 3773:127.0.0.1:3773 -L 52299:localhost:52299 k16`; the fixed port keeps the command the same every month); a pairing code from `t3 auth pairing create` on the box if that browser isn't paired with its T3; and the access level, **Auto**, or **Full access** on a box whose `t3_runtime_mode` is `full-access`.
3. Takes the redirect on `localhost:<port>` (127.0.0.1 and ::1), or the browser's final URL pasted in, refusing another login's state. A tunnel that shows T3 as `localhost` still signs in a bmesh on `127.0.0.1`.
4. Writes `<config dir>/t3-token.json` beside `config.toml` (mode 0600) and prints `signed in: <host>: <access>, 30 days left`. A ceiling below `t3_runtime_mode` exits 3 saying so.

`bmesh t3 login --status` prints `<host>: <access>, N days left`, then the server, scopes and expiry (exit 3 with no sign-in or an expired one).

**Refusals.** With the `t3` runner, `lead start`, `build` and `ideate` refuse (exit 3, before anything is recorded) a sign-in that is:

- missing or expired: `bmesh: T3 sign-in missing or expired; run bmesh t3 login`;
- for a server other than `t3_server`;
- below `t3_runtime_mode`: `bmesh: T3 sign-in allows auto, below [runner] t3_runtime_mode full-access; run bmesh t3 login and approve full-access`.

A lapsed sign-in stops new leads on that box, not running ones, though the daemon's poll of them fails until the next login.

**Expiry warnings.** The daemon of the box holding the token pushes `T3 sign-in on <host> expires in N days: bmesh t3 login` (tag `key`) 7, 3 and 1 days before expiry, once each across restarts, checking hourly with `[notify] ntfy` or `command` set, whatever `push` lists; a daemon down past a mark sends only the nearest, and a new login starts over. `bmesh mesh-status` closes with a line for it under 7 days, or `expired` (see Finished nodes).

**Renamed branches.** A runner may rename a lead's branch (T3 does). Each minute the daemon reads the branch of every lead, fixer and engineer not `done` whose worktree is on this host; a change rewrites the node's `branch` and logs `branch: <path> <old> -> <new>`; a detached or missing worktree changes nothing. `bmesh mesh-status`, the map and `bm-nodes` follow within the minute. The lead lands the branch its worktree is on (`git branch --show-current`), never the name in its brief.

Internals: [docs/internals.md](docs/internals.md#t3-sign-in)

## Proposal rounds

```
bmesh ideate <repo> [--idea] [--model M] [--effort E] [--context <text|->] [--force]
```

A round is two threads on one ship repo, an engineer and a UI/UX engineer. Each writes three to five ranked half-page specs into `docs/proposals/` and lands them on `main` as `proposed`.

**Nodes.** The round is node `<repo>-round-<N>` (`<repo>` the registry name's node token, see Names; `N` one past the repo's highest round), its children `<repo>-round-<N>.eng` and `.ux`, each with an inbox, a thread and a bmesh worktree.

**Refusals.** `ideate` needs the repo's registry row (`host`, `ux_host`, `verify`, `surfaces`); a path without one is refused. It refuses (exit 3):

- a row whose `mode` is not `ship`;
- five or more `proposed` specs on `main`, naming them: they wait on review;
- an open round (a live node): `refused: <repo>-round-<N> is still open (eng working, ux working); wait for it, or pass --force to start another`; `--force` starts one anyway, numbering its specs from the same id and running the repo's tests beside the first;
- a host it cannot reach (both hosts are checked before anything starts);
- a `main` it cannot level with origin (`pull --ff-only origin main` with the main checkout on `main`, else `fetch origin main:main`; never a reset or a stash).

Then, from `main`, it:

1. makes `<[runner] worktrees>/<repo>/<repo>-round-<N>-eng` and `-ux` on branches of those names (an existing branch or path is refused, untouched);
2. renders the two briefs, the engineer's with the next free spec ID and the UI/UX engineer's five past it, so they never collide;
3. starts both threads through the runner, the engineer on the row's `host`, the UI/UX engineer on `ux_host` (`""` = `host`; `host` `""` = this box), with `--model` (default `[models] proposer`) at `--effort` (default `[models] proposer_effort`), titled `[ideate] <repo> round <N>: engineer|UI/UX`;
4. prints `{"path","round","eng":{"path","thread","branch","worktree","host"},"ux":{...}}`.

Briefs are kept under `<user cache dir>/bmesh/briefs/<round>-<child>.md`. On another host, the thread, worktree and brief are made there over ssh; a child whose node is live or whose branch exists is refused there too. Each child starts whatever happens to the other; one that doesn't is marked `blocked` (`did not start: <error>`), the round reads `blocked` naming it and the command exits 1, while the other thread runs on and its pitch arrives when the daemon resettles the round. With the `t3` runner a round's thread runs in the bmesh worktree, filed under the target repo's T3 project: T3 binds only a project's own worktrees.

**`--context`** carries what you asked of the round (`-` reads stdin): both briefs get `## The operator's focus` with the words as given, trimmed, before their closing (a repo's own template without `{{operator_context}}` gets it appended). No words, no section.

**`--idea`** makes the round your one idea, given in `--context` (refused without it, exit 2):

- only the engineer starts (`ux_host` neither checked nor prompted for);
- its brief gets `## The operator's idea` instead of the focus section (write that idea's spec, one half-pager or two or three in build order, and nothing else), its opening, partner and numbering lines matching (`{{round_ask}}`, `{{round_partner}}`, `{{ux_ids}}` for a repo's own engineer template);
- the `started` text reads `round <N> on <repo>: one idea, eng on <host>`, the output has no `ux`;
- the round settles on the engineer alone; its pitch is the engineer's line.

**Reporting.** An engineer's node reports (`started`, `progress`, `ask`, `blocked`, `done`); the daemon polls and wakes it like a lead. Its `done` needs only `--commit` (the commit on `main`) and carries its ranked list as `--text`.

**Settling.** Each minute the daemon settles every open round:

- **Both children `done`:** the round goes `done`, its text the merged pitch (the engineer's list, then the UI/UX engineer's), and the daemon publishes `done` from the round.
- **One child `blocked`:** the round goes `blocked` (`<child> blocked: <text>`, then the other's pitch once done).

A blocked round is rechecked each minute: the other child's pitch, when it arrives, is published as `blocked` again with it; a blocked child unblocked and both `done` make the round `done` after all.

**Pushes.** The round's `done` pushes as `<repo> round N proposed` (priority default, tag `memo`, the pitch as body), its `blocked` as `<repo> round N blocked` (high, `no_entry`); each opens the engineer's thread on a tap when `[console]` resolves it and, like a lead's, needs the event in `[notify] push`. An engineer's `ask` pushes like a lead's (`<path> asks`, with its thread's link); its `done` and `blocked` reach you through the round.

Internals: [docs/internals.md](docs/internals.md#proposal-rounds)

## Builds

```
bmesh build <repo> <NNN> [--model M] [--effort E] [--path P] [--family F] [--hold land|now] [--force]
bmesh build --accepted <repo> [--model M] [--effort E] [--family F] [--hold land|now] [--force]
bmesh scaffold <repo> [--check]
```

`build` dispatches accepted specs to leads. Before it records anything it checks:

- **Row:** no registry row, exit 2; a row not `ship`, exit 3.
- **Spec:** it levels `main` as `ideate` does (see Proposal rounds) and reads the ledger there. A spec not `accepted` is refused (exit 3) naming its status.
- **Model:** the spec's `judge` (`fable`, `opus`, `sonnet` as `claude-fable-5-1`, `claude-opus-5-5`, `claude-sonnet-5-5`) or `--model`; neither is refused. Effort is `--effort`, default `[models] lead_effort`.
- **Node:** `<repo>-<NNN>`, or `--path`. A node neither `done` nor `blocked` (live, from here on) is refused (exit 3, `use --path <repo>-<NNN>-2`); so is a box at `[runner] max_leads`.

Then, past the hold and the verify below, in order:

1. `building` lands on `main` with `built_by` (the lead's thread id), `host`, `family`, and `branch` when the runner names branches ahead (`claude` and `tmux` do): `Dispatch NNN: building on <host>`, committed in a bmesh worktree, fast-forwarded and pushed as `choose-ideas` lands (see The ledger).
2. The brief (the spec file, the row's done rule and verify commands, the node path, the standard closing) is written to `<user cache dir>/bmesh/briefs/<path>.md`.
3. The lead starts through the runner with that thread id, titled `[build] <repo> <NNN>: <title>`, as `lead start` would.

A runner that names the branch itself (`t3`) gets a second commit, `Dispatch NNN: branch <b>`. A failed start puts the spec back to `accepted` (`Dispatch NNN: did not start`), exit 1.

It prints `{"path","thread","branch","worktree","model","spec","building","family"}` (plus `hold` under `--hold`, see Holds); `building` is the commit above, `warnings` what `choose-ideas` warned at accept (a command `main` or another spec already defines), read again from the ledger. The binary has no model fallback: a model the runner rejects you retry with `--model`.

**Families.** Every lead `build` starts is in a build family, the leads a coordinator dispatched together (their feedback is read at the end, see Feedback). `--family F` joins `F` (one subject token: letters, digits, `-`, `_`; else exit 2); without `--family` each invocation mints `<repo>-build-<N>` (the repo's node token, see Names), `N` one past the highest such family in `bm-nodes` or still holding feedback. The lead's node and spec record it as `family`. A coordinator dispatching several commands or repos passes the first family to the rest (over ssh, on the command line).

#### Holding and verifying `main`

Once the spec reads `accepted`, both forms read the repo's leads on every box and hold while its last build run is still going: a live lead outside the build's family (a lead with no family is a family of its own), or a family of the repo's (a lead of the repo is in it, or it is named `<repo>-build-<N>` or `<repo>-fix-<N>`) with feedback pending. The build's own family never holds it. The fixer's families (`<repo>-fix-<N>`, see The fixer) and all others are two pipelines that don't hold each other, and feedback naming bmesh's own repo holds no build (with no such row, every feedback holds). `[runner] max_leads` still caps the box across both; the `land.<repo>` lease still serializes landing.

A held single `build` is refused (exit 3): `held: <family> is still running (<path> <state>, ...); watch it with bmesh watch --nodes <paths>` or `held: <family> has <N> feedback pending; drain it with bmesh feedback list/drain <family>`. `--accepted` prints `held <family>: <why>` once (`--json`: `[{"state":"held","family","reason"}]`), exits 0 and starts nothing.

Then it verifies `main`:

1. The commit checked is main's code head (see Updates): a `Dispatch`, `Review` or `done` ledger commit, or with `code` set a README, document or art commit, gets no verify of its own.
2. A `verify` result recorded for its tree answers: `verify main <sha8>: ok t:<tree8> (answered from the record)`.
3. Otherwise the repo's `verify` kind, as that commit defines it, runs inline in a bmesh worktree at that commit (`<[runner] worktrees>/<repo>/<repo>-verify-<hex8>`, removed after), as `land` runs it, and records: `verify main <sha8>: running at t:<tree8>`, then `... (recorded)`.

A failing verify refuses the build (exit 3, nothing dispatched): `main is red at <sha8> <subject>: verify fail t:<tree8> (<how>); build its fix with --force`, then the last 30 lines of output. A `main` with no verify kind prints `verify: <repo> defines none` and goes on. `--accepted` holds and verifies once per batch, only when a spec is accepted. These lines go to stderr; under `--accepted`, to stdout among its lines.

`--force` passes both (as `ideate --force` passes an open round), runs no verify and prints what it passed: `--force: past held: ...`, `--force: past main is red at ...` or `--force: past verify main <sha8>: no result for t:<tree8>`.

`build` also checks two of the fixer's rulings, which `--force` doesn't pass (a single `build` before the hold and verify, `--accepted` per spec after them). Refused (exit 3):

- a spec carrying `ruled` sized `L` or larger: `refused: spec NNN is ruled and sized L (reversible: anything sized L is proposed, never built); set it proposed`;
- a spec whose `fix_of` chain (each spec's `fix_of`, its first three-digit id, followed until a spec without one) is three deep: `refused: spec NNN is fix 3 on one premise (NNN fixes MMM fixes KKK fixes JJJ) (premise: no third fix on one premise); set it proposed with the belief the fixes assumed`. Two deep builds.

`--accepted` dispatches every accepted spec, lowest id first, through the same steps; one refusal doesn't stop the next. One line per spec:

- `started NNN <title>: <path> on <model> (thread, branch, building, family F)`, then `; warning: <warning>` per warning;
- `waits NNN <title>: on 004 (building)`: its body names a lower id (`004`, or a run `001 to 004`) whose spec is not yet `done`, `rejected` or `later`; a higher id is a pointer, not a dependency;
- `waits NNN <title>: 3 leads working on <repo>`: three leads of the repo (on any box) are live, counting those just started;
- `not started NNN <title>: <the refusal>`, the two ruling refusals above included.

When any started, a last line names their family: `family <repo>-build-<N>`. `--json` prints the lines as objects, each with `family`.

#### From a chat

From a chat (a phone, or a session with the plugin and no command typed), the operator runs this lifecycle through the plugin's skills:

- **`buildermesh:ideate`** ("proposals for X", "run a round on X") runs `ideate` on the row's `host` (locally, else over ssh), with what was asked beyond the repo's name as `--context`; watches with `bmesh watch --nodes <repo>-round-<N>`, relaying an engineer's `ask`; and reports the round proposed (its ranked pitch) or blocked. For one idea of the operator's own (by what was asked, not the words) it starts no two-thread round: `ideate --idea` (one engineer thread, on the row's `host`) when a thread, a model or an effort is asked for, or the idea needs the repo read and measured at length; otherwise it writes the spec itself (one half-pager, or two or three in build order for an idea too big for one lead, `proposed_by: operator`) and lands it on `main` as `proposed`.
- **`buildermesh:choose-ideas`** (the reply to a pitch, in plain words) runs `choose-ideas`.
- **`buildermesh:build`** ("build X 3", "build what's accepted in X") runs `build` on the row's `host`; after a `held` refusal it drains its own family, waits with `bmesh watch` on the named nodes and runs `build` again; a hold by another coordinator's family, or a red `main`, it reports. It watches the leads with `bmesh watch --nodes <paths>`, relays each `ask` and answers it with `bmesh --node operator send`, reports a lead reaching its gate as one line, holds and releases with `hold` and `unhold` ("hold X", "hold land on X", "release X"; see Holds) and stops with `node stop` ("stop X"; see Stopping a node), never with free text. Once the family's watch is done it names the fixer started for the issues naming bmesh's own repo (see The fixer), and turns the rest into specs and drains them (see Feedback).
- **`buildermesh:scaffold`** ("scaffold X") runs `scaffold --check`, shows the diff, runs `scaffold` and the row's verify, commits `Scaffold the shared definition of done and proposals` and pushes.
- **`buildermesh:mesh-status`** ("what's building", "where is X") summarises `mesh-status` (and `spec list` for a named repo) as working, asking, silent, stopped and done since the last look.

`ideate` and `mesh-status` also run "stop X" as `node stop`, never as a message, which would wake the node. `propose` and `build` add the Fable fallback the binary lacks: a Fable model the runner rejects runs once more with `--model claude-opus-5-5`, and the skill reports what the runner said.

Each skill ends with one line: what is started or landed and what waits on the operator, each thing named in plain words (a spec or issue by what it does, its number after it in brackets if at all). The done rule `scaffold` writes and every build brief's closing end the same way (`brief.PlainWords`).

#### `scaffold`

`scaffold` writes into a ship repo what a build expects, from the row and the binary, and nothing else, in the row's checkout (given by name or path) or in a linked worktree of it given by path (the row whose checkout shares the worktree's git common dir), written in place, so a lead scaffolds its own branch without touching the main checkout:

1. **`AGENTS.md`**: three blocks between `<!-- bmesh:done -->`, `<!-- bmesh:proposals -->` and `<!-- bmesh:mesh -->` markers, in that order: the definition of done from the row's verify commands and remote; the proposals convention; the Buildermesh paragraph of `docs/agents-snippet.md` (its verify placeholder filled from the row). Only the text between the markers is rewritten; a missing block is appended at the end, a missing `AGENTS.md` created, and a block between `command-center:` markers (an earlier scaffold's) rewritten in place under `bmesh:` markers.
2. `docs/proposals/README.md` (this repo's, embedded) and `docs/proposals/assets/.gitkeep`, when missing.
3. A kind file for each of the row's `routines` the repo lacks, seeded from `examples/routines/` (`verify` inline and deterministic, its `command` the row's verify commands joined with ` && `). A row with no verify commands gets no verify kind and a line on stderr saying so; a routine with no example is an error naming the file to write by hand.

An existing kind is never touched, except the verify kind's `command` when the row has verify commands: one that (as the kind loader reads it) is not those commands joined with ` && ` gets its one `command = ...` line rewritten and the rest kept, whether or not the row's `routines` name it. A verify kind that runs a model is left alone; one whose command spans lines is an error naming the file to set by hand. Change the verify commands in the row (see Hosts: The registry) and run `scaffold` again.

It prints `wrote <file>` per file, or `<repo> is scaffolded`. `--check` writes nothing, prints the unified diff of what would change and exits 1 when there is any. A row not `ship`, or a path without a row, exits 2. A second run changes nothing. Run it again after the row or the snippet changes and commit what it wrote.

### Holds

```
bmesh hold <path>|--family F [--gate land|now] --text "<why>"
bmesh unhold <path>|--family F [--text "<words>"]
bmesh held
```

A coordinator (the `build` skill's session, or the operator) holds a lead at a gate until it releases it, for example to land in order, to read a diff first, to wait out low quota or to redirect it.

**`hold`** records the hold (`by`: `--node`, else this node, else the host name) and sends the lead a message of kind `hold`, `hold from <by> (<gate>): <why>`, which wakes an idle lead. It prints `holding <path> before land: <why>` (`now: <why>` at gate `now`) per lead; `--json` prints `[{"path","gate","text","by","at"}]`.

- `--gate land` is the default; any gate but `land` or `now` is a usage error (exit 2). `--text` is required. A new hold replaces one already there.
- `<path>` must be a registered live lead. An executor is refused (exit 3, `refused: <path> is an executor; executors are never held: hold its lead <lead>`); so are another role, a done or blocked lead, and a path with no node.
- `--family F` holds every live lead of build family `F`; with none, exit 3.

Rounds and routines are never held. A hold stays until someone releases it; it never times out. Any sender may hold, as any sender may `send`. The two gates:

- `land`: `bmesh land` checks for a hold before it takes the lease and again once it holds it, and stops (exit 3, nothing pushed) with `bmesh: held before land by <by> <age> ago: <why>; nothing is pushed: run bmesh held and end your turn, and run bmesh land again once the release wakes you`. A `now` hold stops it the same way (`held now by ...`).
- `now`: every `bmesh inbox` the lead runs while the hold stands prints it first, on its own line: `held now by <by> <age> ago: <why>; stop here: run bmesh held, end your turn, and wait for the release` (under `--json`, first, a message of kind `held` from `<by>`). Ages are to the minute, whole days as `1d3h`.

**`held`**, run by the lead at its gate, reads the lead's hold, publishes `held` with the text `before land by <by>: <why>` (`now by ...`), moves the node to state `held` and prints `held <path> <text>; end your turn: the release wakes you`. With no hold (a release came first) it is refused: exit 3, `refused: <path> is not held (released already?); run bmesh inbox and carry on`.

A node reaches `held` from `working`, `asking` or `silent` and leaves it on its own next event: the release wakes the lead, and its next `started` or `progress` makes it `working`. The daemon never marks a `held` node silent (only `working` goes silent) and never pushes it (`held` can't be in `[notify] push`); the map's needs-you band has no row for it (see Map). A hold shows in `mesh-status` (the state, and after the table one line per hold, `<path> held before land by operator 3m ago: <why>`; `--json`: the row's `hold`), in `watch` (the `held` event, `<at> <path> held <text>`; it goes on watching) and in the hourly digest once per whole day of its age (`<path>: held before land by operator 1d0h ago: <why>`, in the pass whose window holds that mark).

**`unhold`** removes the hold, sends the lead a message of kind `release`, `released by <by>` (`: <words>` with `--text`), which wakes it, and prints `released <path>`. A lead not held prints `<path>: not held`, sends nothing, exit 0. `--family F` releases every held lead of the family, or prints `<F>: nothing held`. (`bmesh release` cuts a release tag, see Releases.) `node forget` deletes a node's hold with the node.

**`build --hold land|now`** (both forms; "build these, land none until I say") writes each lead's hold before the runner starts it (`by` the sender, text `held at dispatch (build --hold <gate>)`) and sends the hold message once it has started; a failed message goes to stderr, `build: <path> is held before land, but the hold message failed: <err>`, and the start stands. Its JSON carries `hold`; `--accepted`'s started line ends `, held before land` (`, held now`). A failed start deletes the hold; a build without `--hold` deletes a hold an earlier lead left on its path.

Internals: [docs/internals.md](docs/internals.md#holds)

### Briefs

The engineer, UI/UX engineer, build, fix and half-pager templates are embedded in the binary (`internal/brief/templates/`). A repo overrides any but `fix` with `.bmesh/briefs/<name>.md` (`engineer`, `ux-engineer`, `build`, `half-pager`); the fix brief carries bmesh's rulings, which no repo sets. Rendered briefs are kept under `<user cache dir>/bmesh/briefs/<node>.md`.

A template is filled in one pass: `{{name}}` placeholders take values; braces inside a value stay as written. A placeholder with no value is an error naming the template and the placeholder.

Every brief gets:

- `repo`, `repo_path`;
- `verify`: the row's commands in backticks, or that none are listed;
- `surfaces`: the row's, or what to do without them;
- `standard_closing`: the closing paragraph, verify commands inlined;
- `done_rule`: the definition of done the row implies: verify passes; a change sized M or larger as built (its spec's `size`, or the size its scope grew to; work with no spec sized as a spec would be) is reviewed by Opus in a session that did not write it, at medium effort for M and high for L or larger, while XS or S is done on verify alone; rebased, fast-forwarded to `main`, pushed to the row's `remote`.

A round's brief also gets `node`, `round`, `round_n`, `worktree`, `branch`, `thread_id`, `next_id`, `date` and `operator_context` (the operator's focus section, or empty). The fix brief gets `node`, `date` and `plain_words` (the plain-words rule every closing ends with). A build brief gets `spec`, `spec_path`, `spec_id`, `node`, `date` and `upgrading` (empty, except in bmesh's own repo: a bullet asking the lead to fill the spec's `## Upgrading` section as built, with the template's rule).

The proposal briefs carry the repo's facts and point at the `ideate-engineer` and `ideate-ux` skills for the method. The build brief carries the spec, points at the `lead` skill, and says: the review is sized as `done_rule` says and queued with `--effort`; its findings are fixed and then landed, a second pass only after a rebase changed the code; a taste finding is noted in the final message, not re-reviewed.

## Feedback

```
bmesh feedback "<text>" [--repo R] [--where <file|command>] [--fixed SHA]
bmesh feedback list <family>|--repo R [--json]
bmesh feedback drain <family> [--through SEQ]
bmesh feedback drain --seq SEQ[,SEQ...]
bmesh fix [--model M] [--effort E]
```

A lead leaves one `feedback` line per issue of friction outside its spec that another build would meet too, once it has decided it is done and just before `bmesh done`; its own spec's work is not feedback. Executors never write feedback: they report to their lead, which decides what affects other builds.

`feedback` reads the family, repo and spec from the node; one with no family (not started by `build`) is refused (exit 3, `no build family`), an unregistered one exits 1. It publishes the issue to the family's feedback queue, with the node's worktree commit when the worktree is on this box, and prints `feedback <seq> in <family>` (`--json`: the record with `seq`). `--repo` (a registry name or path; default the node's repo) names where the friction is, `buildermesh` for the mesh itself (bmesh, a skill, a brief); `--where` the file or command it was met in; `--fixed` a fix the lead already landed, leaving the general case.

An issue is pending until it is drained or 30 days pass.

- **`list`** with a family prints its pending feedback, oldest first: `<seq> <from> (<repo> <spec>[, where W][, fixed F]): <text>` (`--json`: an array of the records, each with `seq`). Nothing pending prints nothing.
- **`list --repo R`** (a registry name or path) prints every family's pending feedback naming `R`, in stream order, each line naming its family: `<seq> <from> (<repo> <spec>, family <F>[, where W][, fixed F]): <text>`. It leaves out an issue whose sequence a spec on `R`'s `main` names in `from_feedback` (`#<seq>`), whatever the spec's status.
- **`drain --seq`** deletes exactly the feedback messages at those sequences, whatever their family, and prints `drained N`, then `not pending: <seqs>` for any already gone. A sequence holding anything but feedback is refused (exit 3, `refused: <seq> is <subject>, not feedback`) and nothing is deleted.
- **`drain <family>`** deletes the family's pending feedback up to and including `--through` (default the last one there when it starts) and prints `drained N from <family>`; a message published after `--through` stays.

`mesh-status` closes with `feedback: N pending in <family>` per family with any pending; issues naming bmesh's own repo show instead as `fixes: N pending for <repo> (...)` (see The fixer).

The coordinator (the `build` skill) reads the queue once `watch` prints `watch-done` for a family:

1. Issues naming bmesh's own repo are the fixer's (see The fixer): it reports the fixer `mesh-status` names on its `fixes:` line and leaves them pending.
2. The rest it lists, merges duplicates, drops what a spec on `main` already covers (any status) or a `--fixed` commit settled, and sorts: a quick fix (one cause, one obvious change, XS or S) becomes a spec landed as `accepted` (`Feedback NNN: accepted from <family>`, `proposed_by` its thread, `from_feedback` the family and leads) and built in a new family once this family is drained, with no operator review; anything that needs thought becomes a `proposed` spec (`Feedback NNN: proposed from <family>`, the same `proposed_by` and `from_feedback`) for the operator's review, never a round; an issue from a lead built from a `from_feedback` spec is always proposed.
3. It drains the issues it ruled on with `drain --seq`, leaving the fixer's, so the specs' `from_feedback` (`<family> (<leads>) #<seq>`) is the only record left.
4. It reports one line per issue: `fixed by NNN (started)`, `proposed NNN`, `already NNN`, `dropped: <why>` or `to the fixer`.

A coordinator that dies between list and drain leaves the family pending, and `mesh-status` shows it.

Internals: [docs/internals.md](docs/internals.md#feedback)

### The fixer

On bmesh's own repo only (the registry row whose `go.mod` declares the module the binary was built from; the row `update` and `land` find), the feedback naming it is ruled on by the fixer, a fix lead in a pipeline of its own, under the rulings below, and the fixes it rules are built without the operator reviewing them first. With no such row, nothing here is active; with no such row, or a binary that doesn't name its module, the daemon starts nothing.

`bmesh fix` takes no repo. It levels `main`, then starts the fixer through the runner as `lead start` starts a lead: node, family and path `<repo>-fix-<N>` (`N` one past the highest in `bm-nodes`, as a node or a fix lead's family, or still holding feedback), role `fixer`; on the row's `host` (another box runs `bmesh fix` over ssh); on Opus (or `--model`) at `[models] lead_effort` (or `--effort`). It prints `fix: <path> started (thread <id>, <model>, on <host>)` (`--json`: `{"path","thread","branch","worktree","model","family","host"}`). Refused (exit 3): no such row (`refused: no registry row holds the module bmesh was built from`); a row not `ship`; a live fixer of the repo (`refused: fixer <path> is <state>; one fixer writes the queue`). `[runner] max_leads` doesn't count the fixer, only the fix leads it starts. The daemon wakes and watches a fixer as it does a lead.

No session has to run `fix`: the daemon of the row's `host` (any daemon when the row names none) starts one while the row is `ship` and all three hold: none is live; the newest isn't `blocked` (a block, a stop included, waits on the operator and is never restarted); an open issue (pending feedback naming the repo that no spec on its `main` names in `from_feedback`; what `list --repo` prints) was published at or after the newest fixer's start, or there has been no fixer. An issue a fixer had when it started and left is not started on again. The daemon starts one within a minute of the issue, once across every box; its output and any failure go to the daemon's log.

Only the fixer reaches the operator. A lead in a `<repo>-fix-<N>` family (a fix lead) pushes nothing: its `ask`, `blocked` and `done` go to the fixer's inbox in the shapes an executor's take to its lead (see Executors), and the fixer answers or relays them under the rulings and reports them. The fixer's own `ask`, `blocked` and `done` push as a lead's do (see Pushes).

`mesh-status` shows the open issues as `fixes: N pending for <repo> (<fixer> <state>)` while a fixer has them; `(starts within a minute)` when the daemon starts one on its next pass; `(<fixer> blocked)` while the newest fixer's block waits on the operator; `(<fixer> done; they wait on you)` when it had them and left them.

Its brief, the embedded `fix` template, carries these rulings; each spec it writes cites by slug the ones it rests on:

1. `root`: fix the first wrong decision, not the place it showed, then every other place with the same fault. A guard is a fix only when the state it handles is legitimate.
2. `failing-first`: name what fails before the fix and passes after: a test through the real command or package, or a run with its output. A rate or timing the fix rests on carries its run count. An issue that can't be made to fail is dropped, saying what was tried.
3. `smallest`: the smallest change that removes the cause, deleting before adding. A sentence added to a skill or brief every session loads names the sentence it replaces, or why none can go.
4. `structure`: a lesson met twice becomes a check. When a skill sentence already warns about the issue, the fix is a refusal, warning or test in the binary, and the sentence goes.
5. `premise`: no third fix on one premise. When two fixes of the same thing haven't held, write down the belief both assumed, test it, and propose it to the operator with the evidence.
6. `no-new-surface`: a fix adds no command, flag, config or registry key, frontmatter field, bucket, subject or event. Anything new the operator would have to learn is product direction, and is proposed.
7. `reversible`: build what can be undone inside the repo without asking, then report it. Proposed, never built: taste, product direction, anything sized L, and anything outside the repo's `main` (a step on another box, deleted records, the registry or a box's config).
8. `drop`: dropping is an outcome: what a spec on `main` already covers (any status), what a `--fixed` commit settled, and the reporting lead's own spec's work.

Code-design principles (types, boundaries, domain modelling, reader load) are left to the repo's `AGENTS.md` and its review; a migration is sized L and goes to the operator; what is proposed is designed by the ideate method. `build` checks the two rulings it can (see Builds), and `list --repo` hides what a spec already names.

The fixer follows the `fix` skill, one pass:

1. `feedback list --repo <repo>`.
2. One ruling per issue. A fix is a spec with `status: accepted`, `ruled: [<slugs>]`, `fix_of: <the reporting lead's spec>` and `from_feedback: <family> (<lead>) #<seq>`. What `premise`, `no-new-surface` or `reversible` send on is a spec with `status: proposed` and the ruling named in its Opportunity, with no `ruled` or `fix_of`. A drop is a line in the report.
3. Each spec lands on `main` and its issues are drained at once (`drain --seq`).
4. Each fix is built one at a time (`bmesh build <repo> NNN --family <the fixer's>`) and watched; the fixer answers an `ask` the rulings settle, relays the rest, and lists again after each (a fix lead's own feedback is ruled on in the same pass).
5. Once the queue is empty and no fix lead runs, it releases the repo when it has a release tag, then runs `done`: one line per issue (`<seq>: fixed by NNN (root, failing-first)`, `<seq>: proposed NNN (no-new-surface: ...)`, `<seq>: dropped (drop: 044 covers it)`), what waits on the operator first, then the release.

Internals: [docs/internals.md](docs/internals.md#the-fixer)

## Releases

```
bmesh release <repo> [--minor|--patch] [--dry-run]
```

`release` needs the repo's registry row (no row: exit 2), fetches `origin` with its tags, reads `origin/main` (`main` without an `origin`; the tag then stays local) and tags the code head (see Updates) with an annotated `vX.Y.Z` tag, pushed to `origin`.

**The version.** With no `vX.Y.Z` tag in `main`'s history, `v0.1.0`. Otherwise the highest tag's minor goes up (patch `0`) when any spec landed since it is a `feature`, else its patch. `--minor` or `--patch` overrides this (both: exit 2).

**The specs a release carries** are those of the `NNN: done at <sha8>` subjects after the last tag (every one, for the first release), less any whose `merged` commit is already in that release's history. A release tags the code head, so the `done` commit of a spec it carries comes after its tag.

**The tag's message.**

1. `<repo> vX.Y.Z`, a blank line;
2. one `NNN <title>` per spec the release carries (`no specs landed since <tag>` when none);
3. in bmesh's own repo, with upgrade notes: a blank line, `Upgrading to vX.Y.Z (docs/upgrading/vX.Y.Z.toml):`, the summary, what a start-over loses, one numbered line per step (see Upgrade notes).

It prints `released <repo> vX.Y.Z at <sha8> (NNN, NNN)` (`(no specs)`; `; no origin, so it is local only` appended without one; `--json`: `{"repo","tag","commit","specs","pushed"}`). A failed push deletes the local tag.

**Refusals** (exit 3, tagging nothing):

- `nothing to release since vX.Y.Z: nothing under code changed`: no commit since the last tag changes the repo's code (its row's `code`). A last tag on the code head itself (as a tag `release` cut is) prints `already released <repo> vX.Y.Z at <sha8>`, exit 0.
- `held: <family> is still running (...)` or `held: <family> has N feedback pending; ...`: the hold `build` reads (see Builds), a live lead (neither `done` nor `blocked`) or pending feedback of the repo's. `release` has no family of its own, so the coordinator's own family and both pipelines hold it; feedback naming bmesh's own repo holds neither command.
- `main is red at <sha8> <subject>: verify fail t:<tree8> (...); release once main is green`, then the tail of the output: `verify` at the code head is not ok (answered from the record or run and recorded as `build` does; a repo with no `verify` kind says so and goes on).
- In bmesh's own repo only (see The fixer; `land`'s scaffold check makes the same test; no key turns this on elsewhere):
  - `refused: 055, 057 have Upgrading steps and docs/upgrading/vX.Y.Z.toml is missing: distil them into it (the build skill's step 7), commit it to main and release again`: a spec landed since the last tag has `## Upgrading` steps (any text, unless the section is empty or its first word is `nothing`, in any case) and the ref read has no notes for the new tag;
  - `refused: docs/upgrading/vX.Y.Z.toml: <what doesn't parse>`;
  - `refused: docs/upgrading/vX.Y.Z.toml names keys this binary doesn't know: [runner] foo`: an ask or a `when` names a key this binary's config or registry lacks.

  Notes that exist are checked whether or not any spec has steps.

**`--dry-run`** runs the same checks and prints `would release <repo> vX.Y.Z at <sha8> (...); --dry-run tags nothing`, a blank line and the tag's message, and pushes nothing. A pushed tag is public, so a bad release is followed by the next one, never moved.

The first tag is cut by hand or with `release`; from then on the `build` skill ends each batch, once its family is done and drained, with `bmesh release <repo>` for every repo with a `v*` tag where the family landed code. A box on `[update] track = "release"` (the default) updates to each release (see Updates); `track = "latest"` and `bmesh update --latest` follow the code head.

### Upgrade notes

In bmesh's own repo, `docs/upgrading/vX.Y.Z.toml` holds the steps an install at the release before needs, distilled by the coordinator from the `## Upgrading` sections of the specs the release carries (the `build` skill, step 7) and committed to `main` before `release`. A TOML key not listed here is an error:

- `summary`: one line, what the release asks of an install;
- `start_over`: `true` when the steps uninstall the box and set it up again;
- `loses`: what a start-over loses;
- `[[step]]`, in order, each with a `kind` and an optional `when`:
  - `kind = "ask"` sets a key: `file` is `config` (the default; `key` is `[section] key` in `config.toml`) or `registry` (`key` is a row key in `repos.toml`, set on every row); `changed`, one line on what changed; `question` (required); `choices`, a list; `default` (required; one of `choices` when given). No `command`, `os`, `confirm` or `expect`.
  - `kind = "run"`: `command` and `why` (both required), `os` (a list of `linux`, `darwin`, `windows`; none means every OS), `confirm = true` for anything destructive, which prompts before running.
  - `kind = "check"`: `command` (required), `expect` (what its output must show), `why`, `os`.
  - Run and check steps take no `key`, `question`, `choices`, `default` or `changed`.
- `when`: comparisons that must all hold, `[section] key == "value"` or `!=` (the value quoted or bare), joined by `&&`, read from this box's `config.toml` just before the step; an unset key reads as its default, so a step after `init` sees the new config.

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

A repo's specs live in `docs/proposals/NNN-slug.md` (`[spec] glob`): a flat `key: value` frontmatter between `---` fences (lines starting with `#` are ignored) over a half-page body. `README.md` is not a spec.

```
bmesh spec list [--repo R] [--status S] [--json]
bmesh spec next-id [--repo R]
bmesh spec set --id NNN --status S [--repo R] [--from A,B] [--field K=V]...
bmesh spec template [--repo R]
bmesh spec steps [<NNN>] [--repo R] [--tick N,N [--note W] | --check [--wait D]]
bmesh choose-ideas <repo> "<reply>"
```

The frontmatter is the whole record, so any of these is an error for every command, never skipped or counted around: a missing or non-numeric `id`, or one that differs from the file name's; a `status` outside `proposed`, `accepted`, `adjusting`, `later`, `rejected`, `building`, `blocked`, `done`; a `judge` outside `fable`, `opus`, `sonnet`; missing fences or a line that is not `key: value`; two files sharing an id.

**Fields the mesh writes.**

- `branch`: set by dispatch, corrected by the build thread when the runner renamed it; the `spec set` that writes `done` and `merged` adds `--field branch=<the branch landed>` when it differs.
- `family`: the build family it was dispatched in, set with `building`.
- `from_feedback: <family> (<the leads whose feedback it came from, comma-separated>) #<seq>`: on a spec a coordinator wrote from feedback, several issues comma-separated, each with its stream sequence (see Feedback).
- `ruled: [<the rulings' slugs>]` and `fix_of: <the reporting lead's spec>`: on a fix the fixer accepted; `build` checks them (see Builds and The fixer).

`--repo` is a registry name or a path (default the current directory; a row's path counts as that row, as in every command taking a repo).

- `list` prints one row per spec, `NNN  status  size  kind  lens  title` (`?` for a missing field), sorted by id; `--json` prints an array of each spec's frontmatter plus `path`.
- `next-id` is one past the highest id (`001` for an empty ledger).
- `set` rewrites the status and the given fields in place (an existing key keeps its line, a new one is added last, every other byte stays) and prints the row. Refused (exit 1, changing nothing): with `--from`, a status not listed (named); a write the ledger would read back differently (a duplicated `status:` key); a value with a line break.
- `template` prints the half-pager (the repo's `.bmesh/briefs/half-pager.md`, else the embedded one). In bmesh's own repo (its `go.mod` declares this binary's module) it adds an `## Upgrading` section before `## Needs from the operator` unless the text has one: the steps an install from the previous release needs, for any fleet, or `nothing`, a new config key as an ask (what changed, question, choices, default); `release` reads it (see Releases).

#### Steps for the operator

When `bmesh land` lands a spec (step 5 of Landing), it reads the spec's `## Needs from the operator` section as a list and copies it into the frontmatter as `operator_steps`.

- Each top-level `-`, `*`, `+` or numbered (`1.`, `1)`) line is a step, with the lines under it (indented, or running on with no blank line between; a nested list too).
- A step may start with a box in brackets, `[<host>]` (lowercase letters, digits and `-`); with `[hosts]` on and a roster read, a word naming neither a roster box nor this one is text (`[optional] ...`). A leading checkbox `[ ]` or `[x]` is dropped.
- A step may end with `check: <command>` (backticks dropped); the step is done when the command exits 0.
- A paragraph of prose is a step of its own, except one ending in `:` straight before a list (`On the Mac:`): its words go in front of each of that list's steps, and its box, if any, applies to the steps that name none.
- A section that is absent, empty, or whose first word is `nothing` or `none` has no steps and gets no `operator_steps`; a spec landed again with steps from before gets `operator_steps: []`.

The value is JSON on one line, `[{"n":1,"box":"<host>","text":"...","check":"...","done":"","note":"..."}]` (`check` and `note` only when set), `done` empty until the step is ticked, then `<RFC 3339 time> <box>`. A spec landed again keeps the ticks of the steps whose box and words are unchanged, each old tick at most once.

**Reading.** `steps NNN` reads the spec from `main` (`origin/main` after a fetch) and prints `NNN <title>: X of Y open`, then each step as `<n>. [<box>] <text> (check: <command>) -- done <time> <box>: <note>` with the parts it has, or `NNN <title>: no operator steps`. With no id it prints `operator: <repo> NNN <title>: X of Y open (bmesh spec steps NNN --repo <repo>)` per landed spec with open steps (as `mesh-status` ends), in `--repo`'s ledger or else every registered repo with a checkout here, from its local `origin/main`, fetched again when it has open steps (so a step ticked from another box drops out).

**Ticking.** `--tick 2,3` records `done: <time> <this box>` (with `--note`, the words as `note`) on those steps and lands it on `main` as `choose-ideas` lands a review (worktree `<repo>-steps-<hex8>`, commit `Steps: NNN 2, 3 done`), printing `landed <sha8> on main: Steps: NNN 2, 3 done; X of Y open`. A step already done is named (`step 2 already done <when>`) and left alone; when every step named is done, nothing lands. A number past the last step is refused (exit 2), a spec with no steps too (exit 3).

**Checking.** `--check` runs the check of each open step that has one: here, in the repo's checkout, when the step names no box or this one; else as a `check-step` job to the step's box (see Hosts), waiting `--wait` (default 30s).

- It prints `<n>. <box>: exit 0`, `exit <code>: <the output's last line>`, `timed out after 2m0s`, a refusal, or, for a box that hasn't answered, `asleep` or `no answer in <wait>` followed by `; run --check again once it answers`.
- It ticks each step whose check exited 0 with `done: <time> <the box that ran it>` and the note `check passed`, one landing per box. When the other boxes can't be asked (no hub, a box not joined), the passes here still land, the failure reported after them.
- With `[hosts]` off a step of another box is not run (`not run: hosts are off here; run bmesh spec steps NNN --check on <box>`).

Open steps block nothing: `done` and `release` never wait on them.

#### The operator's reply: `choose-ideas`

`choose-ideas` takes the operator's reply to a round, typed in plain words: `"1 and 3 yes, 2 smaller, 4 no, 5 later, 3 on opus"`.

**Clauses** split at commas, semicolons and line breaks and, once a clause's phrase has begun, at a number followed by a spoken verdict (`3 yes and 4 no` and `11 smaller and 12 no` are two clauses each; `4 no because 5 covers it` is one). A clause is its leading numbers (joined by `and` or the like), which name specs, and the phrase after them.

**Numbers.**

- Three or more digits (`012`, as ids are written) name that spec.
- One or two digits are a position in the pitch when there is one, else a spec id: `1 and 3 yes` to a pitch of 012 to 016 means its first and third items; `45 yes` to a five-item pitch is reported as no position, so the spec is `045`; `6` to a pitch of 001 to 003 and 006 to 010, both a pitched id and the sixth item, is reported as ambiguous.
- `all` among the numbers names every spec of the pitch (`all yes`, `all later on opus`); with no pitch it is reported.

**The pitch** is the text of the repo's newest settled round (`done` or `blocked`) on the mesh, the one the operator saw last, read whole, line by line, so a position means the same spec in every reply (a line whose spec has moved on is reported as left unchanged).

- A line is the id it starts with; one holding two or more items of the shape `NNN title (kind, size, judge):` after sentence ends (a `.;!?` before a space; `v1.2` is fine), the title free of sentence ends, is a list typed on one line and yields them all. An id cited inside a why is not an item.
- An open round, even with a finished child, is not a pitch yet; a settled round whose text names no spec (one blocked before a pitch) is passed over for an older one.
- With no mesh or no settled round with a pitch, only ids count, and the output's first line says so.

**The phrase's first word decides.**

| First word | Moves the spec to |
|---|---|
| `yes`, `accept`, `ok` | `accepted` |
| `no`, `reject` | `rejected`, the rest of the phrase as `reason` |
| `later` | `later` |
| `on fable\|opus\|sonnet` alone | nothing: it sets `judge` |
| any other | `adjusting`: the whole phrase is an instruction, recorded as `Adjust (<date>): <phrase>` under its Proposal heading, for the thread that reworks the body |

Reasons and instructions are kept as written, so a verdict word or number inside one is just a word (`2 make it work with no network` adjusts 002). `on <judge>` right after `yes` or `later` sets the judge as well; any further words after `yes`, `later` or `on <judge>` (`yes but smaller`) make the phrase an adjustment.

**A verdict said first** is `yes`, `accept`, `build`, `go`, `approve`, `no`, `reject` or `later` (optionally followed by `to`), or `work on`, then its numbers and, for `reject`, the reason (`accept 012`, `reject 2 too big`, `work on 1, 2 and 3`, `yes to all`).

- It carries to the clauses of bare numbers after it.
- A phrase after its numbers that is itself a verdict said first is a clause of its own (`accept 012 and reject 013`).
- After two or more numbers, a phrase that is not the verdict's own (after `reject`, one starting with a spoken verdict; after `yes` or `later`, anything but `on <judge>`) belongs to the last number, and the verdict covers the others (`reject 2 and 3 yes`, `accept 012 and 3 smaller`; `reject 2 and 3 too big` rejects both, `yes 1 and 3 on opus` accepts both on opus).

**What moves.**

- A clause of bare numbers that no verdict said first reaches takes the verdict of the clause right after it (`1, 2 and 3 yes`); a clause with no number in between leaves them unmatched.
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

It prints one line per spec (`NNN title: old -> new [on judge] [(note)]`), the unmatched items, the warnings below and `landed <sha8> on main: Review: ...` (or `nothing to land`). A `main` that won't fast-forward is refused (exit 3), the review kept on its branch.

**Commands an accepted spec defines** are checked before the commit, for each spec turned `accepted`.

- A spec defines a command where its Proposal has a bold run that is a bmesh synopsis and nothing else: ``**`bmesh release <repo> [--minor]`**``, optionally closed inside the bold by `.`, `:`, `,` or `;`. The command is the synopsis's leading bare words: `bmesh spec verified [--commit C]` defines `spec verified`.
- A bold run that goes on in words (``**`bmesh build` repeats the warning**``), a plain code span, and a bold run with a backtick right outside it (a quote inside a code span) define nothing.
- A synopsis going straight to a flag (`bmesh build --hold land|now`) adds a flag; flags aren't checked.

Each is compared with the commands this binary's `bmesh help` lists (`main`'s, on an up-to-date box) and with every other spec `accepted`, `adjusting` or `building` after the review; a match prints `warning: 048 defines bmesh release, which 046 (accepted) also defines` or `warning: 046 defines bmesh version, which main already has` after the unmatched items (two specs accepted in one reply defining one command warn once, from the higher id). The reply still lands; the operator decides, and a spec changing a command on purpose warns at every accept, naming `main`. `build` repeats the warnings at dispatch (see Builds).

## Executors

A lead fans out by making executors: subagents in worktrees of their own, on branches under the lead's, each a node under the lead's path.

```
bmesh executor new --name N [--base REF] [--model M] [--json]
bmesh executor list [--json]
bmesh executor remove <name> [--force]
```

**`new`** runs as the lead (or as an executor fanning out again; the caller is the parent) and:

1. makes `<[runner] worktrees>/<repo>/<parent path>.<name>` with `git worktree add -b <parent's branch>-<name>` from the parent's HEAD (`--base` picks another commit; the parent's branch is its worktree's current one, renamed or not);
2. registers node `<parent path>.<name>` with an inbox and state `idle`;
3. prints `{"path","branch","worktree"}` and, unless `--json`, the executor header to paste at the top of the subagent's prompt: the path, worktree and branch. The header says to start every shell command with `cd <worktree> &&`, pass `--node <path>` on every `bmesh` command, run `bmesh started` first, report with `bmesh done --commit <sha> --text "<one line>"` alone, and invoke the `buildermesh:executor` skill.

A name matches `^[a-z0-9][a-z0-9-]*$`; one whose node is live (neither `done` nor `blocked`) or whose branch exists is refused (exit 3). When `git worktree add` fails (a hook, an unwritable root), nothing is registered and the worktree and branch are removed.

**`list`** prints every node under the lead: path, state, branch, head (when the worktree is on this host) and unread count.

**`remove`** refuses (exit 3) a branch not merged into the caller's HEAD, unless `--force`. It removes the worktree (`--force` also drops uncommitted changes) and the branch (`-d`, or `-D` with `--force`), forgets the worktree in the node, drains its inbox and marks it `done`. The node stays on the mesh until `bmesh node forget` (see Finished nodes); the name can be used again.

**What reaches the lead.** An executor's `ask`, `blocked` and `done` are published as events and also put in its parent's inbox, since only leads reach the operator; `started` and `progress` are events only.

- `ask`: a message, `ask from <path>: <text>`;
- `blocked --text T`: a message of kind `report`, `blocked from <path>: <T>`;
- `done --commit SHA [--text T]`: a `report`, `done from <path>: branch <b> at <sha8>: <T>`; it needs `--commit` and never reads the spec.

A report carries `{"event","from","branch","commit"}` (branch and commit for `done`) in its `report` field, which `inbox --json` prints.

With no text, each ends before its `: <T>`. The branch is the worktree's current one (`rev-parse --abbrev-ref HEAD`), or the recorded one when the worktree is detached, gone or on another host; the sha is `--commit` resolved in the worktree, else as given. A retry within two minutes is one message, and a refused event sends none. The digest nests an executor's ask, silence, block or completion under its lead's line and counts only leads under "asking".

Internals: [docs/internals.md](docs/internals.md#executors)

### The skills

The repo is a Claude Code plugin, `buildermesh`, with twelve skills. Seven are the operator's chat entry points: `ideate`, `choose-ideas`, `build`, `scaffold` and `mesh-status` for the lifecycle (see Builds); `sync` and `prune` across boxes (below). The nodes the lifecycle starts follow the other five: `fix` (the fixer's pass, see The fixer), `lead` (one spec, its executors, one `review` pass, landing, feedback), `executor` (its own worktree and branch only; it reports and asks through the lead and never merges or pushes, except its own branch to queue a kind of another OS), `ideate-engineer` and `ideate-ux` (a round's method, from reading the repo to the pitch the operator answers with `bmesh choose-ideas`).

**`buildermesh:sync`** ("sync my repos", "sync X") shows what `bmesh sync` would do (it has no dry run): this box's checkout against a fresh fetch of origin (up to date, behind, ahead, or what will refuse it) and, from `bmesh hosts --json`, the boxes that would get a `level`, their state and which are secure; then runs `bmesh sync --all` or `--repo R` and reports per box what moved, is held for approval, was refused or has yet to answer.

**`buildermesh:prune`** ("clean up worktrees") runs `bmesh prune` dry first, shows the keep and remove rows per box, runs it with `--apply` and reports per box; on a bmesh without `prune` it says so. Both stop, saying so, on a box with `[hosts]` off.

The node skills name optional pstack and method skills at fixed moments, each with a written default for a session without it (see Quickstart): the lead `poteto-mode`, `method:writing-plans` (an L spec), `architect` and `attack-the-premise` (the shape), `control-ui` (a browser surface), `figure-it-out` (before an ask) and `blast-radius` (before landing); the executor `tdd` and `figure-it-out`; the round's engineers `exhaust-the-design-space` and `attack-the-premise` before ranking.

**Install** it from the repo:

```
claude plugin marketplace add rickdiculousli/buildermesh && claude plugin install buildermesh@buildermesh
```

or from a checkout: `claude plugin marketplace add <path to the checkout> && claude plugin install buildermesh@buildermesh`.

**The hook.** `hooks/hooks.json` runs `bmesh hosts --here` and then `bmesh update --notice` at every session start when `bmesh` is on PATH (stderr dropped, so a binary older than the plugin prints nothing). On a box with `[hosts]` on, a session opens with the line naming the box (see Several boxes); on a box whose binary is behind `main`, with the notice (see Updates). The hook prints nothing else.

A lead's brief says "follow the `buildermesh:lead` skill" and adds the operator's own conventions; the executor header names the executor skill; a round's briefs name the ideate skills. The skills name no repo convention: `docs/agents-snippet.md`, the paragraph a repo's AGENTS.md gets, is embedded in the binary for `bmesh scaffold` and kept in the repo for reading.

## Finished nodes

```
bmesh mesh-status [--all] [--json]
bmesh node forget <path> [--worktree [--wait 30s]]
bmesh node stop <path> [--text "<why>"] [--wait 30s]
```

**`mesh-status`** prints nodes sorted by path: path, role, state, since, runner and unread count. It leaves out a node `done` for longer than `[status] keep_done` (default `24h`; `0` leaves none out) and closes with `… N finished nodes older than 24h (--all shows them)`; a `blocked` node is never left out, and a round goes only with all its children. Then, in order:

1. `<path> held before land by <by> <age> ago: <why>` per hold (see Holds);
2. `fixes: N pending for <repo> (<fixer> working)` or `(starts within a minute)`, the open issues naming bmesh's own repo (see The fixer);
3. `feedback: N pending in <family>` per family with other feedback pending (see Feedback);
4. `t3 sign-in on <host>: expires in N days (bmesh t3 login)` with the `t3` runner and this box's T3 sign-in under 7 days from expiry (see T3 sign-in);
5. the behind-main notice (see Updates);
6. the hub line when this box's `bmesh hub` runs an older binary than its `bmesh serve` (see Several boxes);
7. the `operator: ...` lines `bmesh spec steps` prints with no id (see Steps for the operator under The ledger).

`--all` lists every node; `--json` carries every node whatever the flag, as the map's `/api/nodes` does: an array of `{"node","state","runner","unread"}`, a held node's with `hold`.

**Routine nodes** leave the mesh by themselves: a fresh run's node goes once its run ends, and a pooled instance that stops (its pool restarted or shrunk, its kind or repo gone) is marked `done` and removed after `[status] keep_done` (`0`: never). Their events stay for the map. Every other node stays until forgotten.

**`node forget`** removes a node from the mesh, printing `forgot <path>`; its events stay, and nodes under it are not forgotten with it. It takes a `done` or `blocked` node; a round whose children are all gone (forgotten before the daemon settled it), whatever its state; and an `idle` node that never reported and whose thread isn't running (a lead whose session died before its first event). An idle node is refused (exit 3) when:

- it published an event of its own: `refused: <path> is idle but published <event>; only an idle node that never reported is forgotten`;
- it names no box, is on another box, or its runner is not this box's: `refused: <path> is idle and names no host`, `refused: <path> is idle and its thread is on <host>; run it on <host>`, `refused: <path> is idle and its runner is "<kind>", not this box's <runner>; run it on <host>`;
- its runner, asked once for its thread's status, doesn't report it known and not running: running (`refused: <path> is idle and its thread <thread> is running; stop it first`), can't tell (tmux) or names no thread (`refused: <path> is idle and its thread may be running; stop it first`), or fails (the same, with the error in parentheses before `; stop it first`).

A lead between its runner start and its first `started` is such a node; the status check keeps it. Any other state is refused (exit 3, `refused: <path> is <state>; only a done or blocked node is forgotten`), as is a node with a live node under it: `refused: <child> under <path> is <state>`.

**Worktrees.** Without `--worktree` the disk is untouched, and a node with a worktree prints `worktree <path> kept`. `--worktree` first removes the node's worktree (`git worktree remove` in its repo; one already gone from disk is pruned) and deletes its branch (the one checked out there, renamed or not; else the node's), printing `removed worktree <path>` and `deleted branch <b>`.

- For another box's node, with `[hosts]` on and this box joined, `node forget` sends a `forget-worktree` job to that box (see Hosts: Another box's worktree) and waits `--wait` (default 30s). It forgets the node only once that box answers that it removed the worktree. With `[hosts]` off it is refused (`run it there`).
- A dirty worktree (`git status --porcelain` prints anything) or a branch not merged into `main` (an executor's: or into its parent's branch, while that exists) is refused (exit 3, naming the cause, nothing touched).
- Merged means the tip is an ancestor, or every commit has an equivalent there (`git cherry`, as after a rebase before landing). The branch is deleted with `-D`, since `-d` would also require it merged by ancestry into its upstream.

Internals: [docs/internals.md](docs/internals.md#finished-nodes)

### Stopping a node

`node stop <path>` stops a node and keeps it stopped. It stops the nodes under it first, deepest first (a round's children, a lead's executors; those already `done` or `blocked` are left alone), then the node itself. For each:

1. It writes the node `blocked` with `stopped by <who>: <why>` (`stopped by <who>` without `--text`) and publishes `blocked` as the node's own would be (a lead's pushes).
2. It stops the thread through its runner (`stop`: T3's `t3_thread_interrupt`, claude's turn and queue, tmux's Escape; a thread the runner no longer has counts as stopped), printing `stopped <path>`.

`<who>` is `--node`, else this node, else the host name. A node with no thread (an executor) is only written.

- A failed interrupt exits 1 with the node already marked (`... (it is marked stopped; run node stop again to stop its thread)`).
- Another box's thread gets a `stop-thread` job there (see Hosts: Another box's thread), all sent at once after the threads here and answered within one `--wait`, printing `<box>: stopped <path>`. With `[hosts]` off it is refused before anything is stopped (exit 3, `refused: <path> runs on <host>; run it on <host>`), as is a thread whose runner is not this box's (`refused: <path>'s runner is "<kind>", not this box's <runner>; run it on <host>`).
- A round is then settled from its children as the daemon does (see Proposal rounds), printing `<round> blocked`; one whose children are all gone is written `blocked` directly with the same text, and one with children on an asleep box prints `<round> stays <state> until its children are stopped` (the daemon settles it once they are).
- A node already `done` or `blocked` prints `already <state>`, exit 0, stopping nothing but a stopped node's own thread on this box, which is interrupted again (harmless to an idle thread). A stopped node is `blocked`, so `node forget` takes it.

**A stopped node** is a `blocked` one whose text starts `stopped by ` (`node stop`) or `stopped in ` (an interrupt in its runner). The daemon never wakes it for silence, a report, a reply or a message from a node under it; only a `bmesh send` from outside its tree after the stop resumes it, so a message from before the stop (a STOP sent while the thread ran) doesn't. The wake reads `bmesh: resumed by <sender>; 1 unread: message from <sender>. Run ...` (see Config: Wake text). It stays `blocked` until it reports again.

**An interrupt in the runner is a stop.** When the runner reports the newest run of a lead's (or engineer's) thread ended by the user (T3: the run `interrupted`; see Runners), the daemon's minute poll writes the node (in any state from `idle` through `working`) `blocked` with `stopped in <runner> by the operator` and publishes that `blocked` once; the same run never marks the node again, while a later one does. It is not woken for silence meanwhile, so interrupting a thread in T3 is enough. With a runner that can't tell (tmux, claude), the daemon still wakes an interrupted thread for silence; stop it with `node stop`.

Internals: [docs/internals.md](docs/internals.md#stopping-a-node)

## Routines

A routine runs one of a repo's checks against a commit: a shell command or a headless `claude -p` session. A kind says how; a job asks for one run; a result records the run by tree.

### Kinds

A kind is `.bmesh/routines/<kind>.toml` (the directory is `[routines] dir` in the repo's `.bmesh/config.toml`); its name, the file name, matches `^[a-z0-9][a-z0-9-]*$`.

| Key | Default | Meaning |
|---|---|---|
| `mode` | `"pooled"` | `inline`, `fresh` or `pooled` (see Modes) |
| `os` | `""` | the OS the run needs (Go's `GOOS`, as `bmesh hosts` prints it): `darwin` (macOS), `linux` or `windows`; `""` is any box (see Another OS) |
| `deterministic` | `false` | same tree, same result: a recorded result answers a repeat job |
| `slots` | `[]` | slots (from the repo's `[slots]`) a run leases |
| `timeout` | `"20m"` | the run's process group is killed after this |
| `command` | `""` | run by the platform shell in the checkout at the commit, the job's args appended (`"$@"` under sh) |
| `model` | `""` | a model id: the kind runs `claude -p` with it |
| `models` | `[]` | several model ids; a job's `--model` picks one, default the first |
| `prompt` | `""` | model kinds: a setup prompt, sent once per session before the first job |
| `allow` | `[]` | model kinds: extra `--allowedTools` entries, e.g. `"Bash(npm test:*)"`; `Read`, `Grep`, `Glob` and `LS` are always allowed |
| `pool` | `1` | pooled: instances per model; fresh: runs at once |
| `batch_idle` | `"30s"` | pooled: held jobs start after this long with no new job and no go |
| `recycle_uses` | `20` | pooled model: a new session after this many jobs |
| `recycle_context` | `120000` | pooled model: a new session when the last reply's context tokens (input + cache read) exceed this |
| `recycle_idle` | `"55m"` | pooled model: a new session after this long idle (the prompt-cache TTL) |

- Exactly one of `command`, `model` and `models` is set; an unknown key is an error.
- An `os` outside the three is refused, `kind <k>: os "<v>": want darwin, linux or windows (macOS is darwin)`; so is `os` on an inline kind, which runs in the requester's worktree. `os` states what the work needs; no key names a box.
- bmesh trims a command's trailing spaces, newlines and `;`. Its last line may not end in a comment, which would comment out the appended `"$@"`.
- A model id has no spaces or `;` (it must fit a `verified` entry); `result record`, `routine queue` and `routine run` refuse such a `--model` (exit 2).
- A recycle key of `0` is off.

**Environment.** A command gets `BMESH_JOB`, `BMESH_KIND`, `BMESH_COMMIT`, `BMESH_TREE`, `BMESH_FROM` (the path the job runs as: the instance, or the requester inline) and `BMESH_REPLY` (the requester's path).

**Model kinds** run `claude -p --model M --output-format json --session-id|--resume <uuid> [--effort E] --allowedTools <list> --max-turns 40` with the job on stdin: a header line `job: <id> commit: <sha> tree: <tree>`, the job's `--text` and its args.

- `--effort` is passed only when the job set one; the jobs of a pooled session each run at their own. `queue` and `run` refuse (exit 2) an effort outside `low`, `medium`, `high`, `xhigh`, `max`, and any effort for a command kind.
- The run is ok unless claude errs or the reply's last `VERDICT:` line reads `VERDICT: fail` (the line is `VERDICT: ok` or `VERDICT: fail`). Leading `*`, `_`, `#`, `` ` ``, `>` and spaces are ignored, around the value too: `**VERDICT: fail**`, `**VERDICT:** fail` and `VERDICT: **ok**` all parse.
- When the prompt contains `VERDICT:`, a reply without a verdict line fails; otherwise claude's exit code decides.

A run's output is stdout and stderr (a command) or the reply text (a model).

**Examples**, in `examples/routines/`:

- `verify.toml`: `mode = "inline"`, `deterministic = true`, `command = "npm test"`.
- `review.toml`: `mode = "pooled"`, `models = ["claude-opus-5-5"]` (a session that never saw the change), `allow = ["Bash(git diff:*)", "Bash(git log:*)"]`, and a prompt that reviews `git diff <base>..HEAD` (the job text carries `base: <sha>`, e.g. `--text "base: $(git merge-base origin/main HEAD)"`), asks for findings as `### <severity>: <what breaks>` sections ending in `VERDICT: ok` or `VERDICT: fail`, and says verify already ran tests, vet and formatting at that commit, so the reviewer reads instead of running them.
- `surface.toml`: `mode = "fresh"`, `deterministic = true`, `slots = ["browser"]`, `command = "npx playwright screenshot"`, the job args `<url> <file>` appended as `"$@"`; a commented `os = "darwin"` line shows how a check that needs a Mac says so.

### Modes

| Mode | Runs | Where |
|---|---|---|
| `inline` | `bmesh routine run` in the requester's process; no daemon | the requester's worktree, at HEAD |
| `fresh` | the daemon starts an instance per job, `routine.<kind>.f<id8>`, and removes its checkout and node after | a checkout of its own |
| `pooled` | the daemon keeps `pool` instances per model, `routine.<kind>.<host>-<model-short>-<n>` (`<host>-cmd-<n>` for a command), draining the kind's inbox in batches | one checkout per instance, kept |

`id8` is the job id's first 8 characters; `host` and `model-short` are the box's host name and the model id as path tokens (`routine.review.k16-opus-5-5-1`), so each box's instances are nodes of their own; `n` counts from 1 per model.

**A pooled instance** holds the jobs it takes until `batch_idle` passes with no new job, or a go arrives (`bmesh routine go <kind>`, or a job queued with `--go`); then it runs them in order, and any job queued meanwhile at once.

**A model instance** keeps one session across jobs, starting a new one when `recycle_uses`, `recycle_context` or `recycle_idle` is reached, or the kind's definition at a job's commit differs from the session's. A run whose output isn't claude's JSON result drops the session whatever the recycle settings; the next job starts one with the prompt.

**Instances are nodes** (`role: routine`, with repo, host and model): `working` during a job, `idle` between, `done` when stopped (a fresh run after its job; a pooled one when its pool stops or restarts; see Finished nodes for when the node leaves). The daemon publishes on `bm.<instance path>.<event>`:

- `started`;
- `progress`: `job <id> started` before a run and `job <id> ok|fail <ms>` after it (`job <id> ok|fail <ms> ctx <tokens>` for a model instance), both with `to` = the requester's path; or `recycled`;
- `done`.

An event with a `to` counts as that path's activity too, so a lead waiting on a routine isn't marked silent. The map draws one box per kind, with its queue and instance counts, linking to the routines page, which lists the instances per repo.

**Reading kinds.** Every minute the daemon reads each registered repo's kinds (see repos.toml) from its main (`origin/main` after a fetch when the repo has an `origin`, else `main`) and starts, restarts or stops pools to match.

- A repo whose path doesn't exist here (another box's row) is skipped, logged once per daemon start: `routines: repo has no checkout on this box; skipped repo=<name> path=<path>`.
- A kind whose `os` on main isn't this box's gets no feed, instances or consumer settings here, so its jobs are never fetched here.
- A repo whose kinds can't be read (no `main`, say) keeps what it had, logged once per repo and main commit: `routines: read kinds repo=<name> err=<error>`.
- A box defers the jobs of a kind it can't judge for a repo (see Running).
- Routines are off when `[runner] checkouts` is empty.

Internals: [docs/internals.md](docs/internals.md#modes)

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

`status` prints a kind's (or every kind's) instances with host, state and model, then their unanswered jobs in queue order under `JOB KIND STATE HOLDER FROM`:

| State | Means | Holder |
|---|---|---|
| `waiting` | in the inbox: never taken, handed back, or held by a daemon that died until its `AckWait` passes | `-` |
| `held` | taken by a daemon, not yet running | `daemon.<host>` until a pooled instance takes it into its batch, then that instance, or a fresh run's path |
| `running` | being run | the instance |

A job is answered once a daemon acked it or the requester's inbox holds its reply. `AckWait` is the kind's longest `timeout` + 1m; the jobs of a daemon that has not reported within it read `waiting`. `--json` carries instances as `members`, `[{"path","host","state","model"}]`, and jobs as `jobs`, `[{"id","from","state","holder","seq"}]`.

#### Queueing

A job names a commit (`--commit`, default `HEAD`, resolved in the cwd repo to a full SHA and tree) and a repo, the first of:

1. the requester node's `repo`, when a registered node names one;
2. the registry name whose path shares the cwd's git common dir (so a linked worktree, where leads live, names its main repo);
3. else `queue` refuses (exit 3), `bmesh: <repo> is not in the registry`, naming the main repo directory's base name; a node's repo the registry lacks is refused the same way, as no daemon would run its job.

`queue` also refuses (exit 3) a kind the cwd worktree's `.bmesh/routines/` doesn't define, `no kind <kind> in <worktree>`, and an inline kind, `bmesh: <kind> is inline: run it with bmesh routine run`. A job queued twice is stored once (`queue` prints `duplicate <id>`). The daemon resolves the repo through its own registry.

Every box running the kind shares its queue; each fetches as many jobs as it has instances for the kind (at least 1, so a box whose repos can only refuse still answers). A stopping daemon (SIGTERM or SIGINT, and before an update's re-exec) kills every running job's process group and hands back every job it holds or ran, unanswered, so the next daemon takes it at once (a model job killed mid-run is paid for twice). Only a daemon that dies leaves jobs unacked until `AckWait` passes.

Internals: [docs/internals.md](docs/internals.md#queueing)

#### Running

The daemon runs a job with the kind as defined at its commit (`git show <commit>:.bmesh/routines/<kind>.toml`), falling back to main's only when the file is absent there.

- A deterministic kind's job is answered from its recorded result, ok or failed, without a run, unless `--rerun`; `queue` checks the record first and prints `answered from result <key> (job <id>)`.
- A commit not in the daemon's clone (queued from another box) gets one `git fetch origin` before the job is answered `not a commit`: push the branch before queueing to a daemon elsewhere.
- A job whose slot is busy goes back to the inbox for 30 s.
- A review is one pass per branch: `queue review` is refused (exit 3) when the branch already has a `review` result (for a commit that is an ancestor of HEAD but not of `main`, or `origin/main` when the repo has an origin, so a landed spec's review doesn't count), naming that result, its verdict, commit and time. Only `--again` queues it, for code a rebase changed.

The daemon answers `ok=false` with the reason (`routine wait` prints it and exits 1, as for any failed job) for:

- a kind that a configured repo defines but the job's repo's main doesn't, `kind <k> is not on <repo>'s main (origin/main at <sha8>)` (the commit that box last read main at; `main at` without an origin), so a kind only a branch has is answered at once by any box with the repo;
- an inline kind;
- an unknown model;
- a commit whose kind runs on another OS (see Another OS);
- a lost lease.

**Deferred.** A daemon leaves a job it can't judge for a box that can, with no reply: one for a repo

- its registry doesn't name (it reads `repos.toml` only when `serve` starts, so restart `serve` after changing the registry);
- with no checkout (or no usable path) on that box;
- whose main failed to read and left no earlier definition of the kind;
- whose main gives the kind another OS.

It logs `routines: deferred job <id>: <repo> <why> here` once per job.

A job only boxes without its repo see waits until `routine wait` times out, as does one of a kind no configured repo defines on its main. A go any box fetched starts every box's held batch.

Internals: [docs/internals.md](docs/internals.md#running)

#### Another OS

A kind with an `os` runs only on a box of that OS; its jobs wait on the hub while that box sleeps. Boxes take a kind's jobs by main's definition, but a job runs on the `os` its commit gives the kind (else main's), whatever the working copy says, so a branch could give an `os` to a kind that boxes of both OSes run. After the record lookup, `queue` checks:

1. the kind at the job's commit (`git show`, not the working copy). When it sets an `os`, `queue` compares it with the kind on `origin/main` (`main` without an origin) and refuses (exit 3) when:
    - main's `os` differs: `bmesh: <kind> runs on darwin at <commit8> but anywhere on main, so a job could reach a linux box: land the os change first, or give it a kind name of its own`;
    - main lacks the kind (it has no feed anywhere): `bmesh: <kind> is not on main: land it first`;
    - main can't be read: `bmesh: cannot read kind <kind> on <ref>: <why>`.

    A commit with no `os` in the kind, or no kind file, isn't checked: it runs where main sends it.
2. for an OS other than this box's, that the commit is on an `origin` branch (`git branch -r --contains <commit> --list 'origin/*'` prints something); else exit 3, `bmesh: push first: <kind> runs on darwin, which fetches the commit from origin`.

A job that passes both and runs on another OS prints `queued <id> (runs on darwin)`.

A daemon whose job's commit gives the kind another OS (queued by an old CLI, or main moved between queue and fetch) answers it `ok=false` right after loading that definition, before the record lookup and slot lease, `kind <k> at <commit8> runs on darwin; this box is linux`, and acks it; nothing runs or is recorded there. A kind name is one queue across every repo and box; since a box defers a repo whose main gives the kind another OS, kinds of different OSes may share a name.

#### Replies

A reply reaches the requester's inbox with the job's output, cut at 64 KiB with a trailing `[truncated]` line.

`wait` finds the reply whether or not `inbox` already read it, for the stream's 30 days, then prints its output and exits 0 when ok, 1 when failed, 5 on `--timeout` with `bmesh: timed out waiting for <id>; the job may still be queued or running, and its reply stays in the stream for 30 days: run bmesh routine wait <id> again rather than queueing it twice`. A reply `wait` found is not printed by `inbox` again and wakes no one (one further back than 100 unread messages stays unread and wakes the node once). `--timeout` defaults to 20m of queueing plus the kind's `timeout` plus the daemon's 1m when the cwd repo defines the kind (41m for a 20m kind), else 20m.

`bmesh inbox` returns what the inbox holds at once, acks what it reads (`--peek` leaves it unread), and prints a reply as one line, `<at> <from>: reply to job <id>: <kind> ok|fail, <n> lines; bmesh routine wait <id> prints it`; `--json` prints every message whole.

`queue`, `go` and `wait` need a node identity (`--node`, `BMESH_NODE` or the registered worktree), the reply's address. `run`, `lock-lease` and `result record` use the node when one resolves, else act as the host name; `list`, `status` and `result find` need none.

Internals: [docs/internals.md](docs/internals.md#replies)

### Checkouts

Fresh and pooled runs never touch a requester's worktree: each instance has a detached worktree of the registered repo at `<[runner] checkouts>/<repo directory name>/<instance path>`, moved before each job with `git checkout --detach <commit> && git clean -fd`. The daemon reads the repo's main in a sibling checkout named `main`.

`routine run` is the inline run, of the cwd worktree's HEAD. It refuses (exit 3), in this order:

- a kind the cwd worktree's `.bmesh/routines/` doesn't define, `no kind <kind> in <worktree>`;
- a kind whose `os` isn't this box's, `--here` included, `bmesh: mac-build runs on darwin; this box is linux: queue it`, since the result would stand as the kind's for the tree and answer every later job for it;
- a fresh or pooled kind without `--here`, `kind <kind> is <mode>; queue it, or pass --here to run it in this worktree`;
- a dirty tree, `bmesh: worktree is dirty; commit first`, since every inline run records a result for HEAD's tree;
- a `--commit` other than HEAD, `inline runs run HEAD; check out <commit> or queue the job`.

It prints the run's output, then its verdict line, `<kind> ok|fail t:<tree8>[ m:<model>] (recorded)`, or `(answered from the record)` when a deterministic kind's record answered it (the recorded output printed above). The line is a `verified` entry, not part of the recorded output.

### Leases

```
bmesh lock-lease acquire <slot> [--ttl D]
bmesh lock-lease renew <key> [--ttl D]
bmesh lock-lease release <key>
bmesh lock-lease run <slot> [--ttl D] -- <cmd...>
```

A slot is a resource only so many runs may use at once. Its capacity is in the repo's `.bmesh/config.toml` (default 1):

```toml
[slots]
browser = 1
[routines]
dir = ".bmesh/routines"
```

- `--ttl` defaults to 10m, at least 3 s; an expired lease is free.
- `acquire` prints the lease key (exit 3 when the slot is busy); `renew` and `release` refuse a lease held by someone else (exit 3).
- A holder renews every ttl/3. A failed renew loses the lease: the run is killed and the job answered `ok=false` with `lease lost`, nothing recorded (`lock-lease run` exits 1).
- `lock-lease run` runs the command in its own process group, kills the group and releases on SIGINT or SIGTERM, and exits with the command's code.
- Kinds lease their `slots` for `timeout` + 1m (inline runs and the daemon alike).

Internals: [docs/internals.md](docs/internals.md#leases)

### Results

```
bmesh result find <kind> [--commit C] [--model M] [--json] [-- args...]
bmesh result record <kind> --ok|--fail [--commit C] [--model M [--base B]] [--output FILE|-] [-- args...]
```

A result is kept per kind, tree, args and model, with its verdict, output (cut at 64 KiB), time and who ran it. A model's result also records the patch-id of the diff it was asked to read, from the commit the job text's `base: <sha>` names to the job's commit, which lets a review carry across a rebase (see `verified`); a base that isn't a commit records none (the daemon logs it; `routine run` says so on stderr).

Every run naming a commit records, inline or daemon, deterministic or not; a kind's newest run at a tree replaces the last. Answered-from-record jobs, refused jobs, lost leases and runs cut off by a stopping daemon record nothing.

`result record` stores a result by hand for any kind name, kind file or not. `--commit` defaults to `HEAD`; a result for HEAD is refused (exit 3) while the worktree is dirty, as in `routine run`. `--base B` records the patch-id of `B..<commit>` as a review's; it needs `--model` (exit 2 without) and refuses an empty diff (exit 3).

`result find` prints `<ok|fail> <tree> <at> <by>[ m:<model>]` (exit 3 when none): with `--model` that model's result, else every result of the kind at the tree with those args, one line per model (`--json`: one object per line). A `result` subcommand's usage error prints its own usage, flags included; `-h` on any subcommand names its flags.

A result older than `[routines] keep_results` (default `30d`; `0` keeps them forever) is gone. A deterministic kind then runs again, and `bmesh done` for a spec landed longer ago needs its routines rerun on the merged tree (it names the missing result); the spec's `verified` line stays.

Internals: [docs/internals.md](docs/internals.md#results)

### `verified`

A spec's frontmatter carries one line of proof, `;`-separated, one entry per check:

```
verified: review ok t:3f2a9c1e m:claude-opus-5-5; verify ok t:3f2a9c1e
```

Each entry is `<kind> <ok|fail> t:<tree first 8>[ m:<model>][ c:<tree first 8>]`. `c:` names the tree a model's result was recorded at when it carries to `t:` across a rebase that left the diff unchanged:

```
verified: review ok t:3f2a9c1e m:claude-opus-5-5 c:81d0b7a4; verify ok t:3f2a9c1e
```

`bmesh spec verified [--spec NNN] [--commit C] [--write]` composes the line from the results recorded for the tree of the cwd worktree's HEAD, or of commit `C` (`--commit`, read below as HEAD; exit 2 when not a commit):

- One entry per (kind, model), the newest result winning whatever its args, sorted by kind then model.
- A (kind, model) with no result there takes the newest model result whose patch-id equals HEAD's (the diff from HEAD's merge-base with `origin/main`, or `main` without an origin, to HEAD), written `t:<HEAD's tree8>` and `c:<its own tree8>`: a review of the same diff on another base. A changed diff carries nothing, however small; a command kind's result (verify) never carries, being about its tree; nothing carries when HEAD is on main.
- It prints `verified: ...`, or exits 3 with `bmesh: no results for HEAD's tree; run the routines first` (`no results for <C>'s tree t:<tree8>` under `--commit`).
- `--write` sets the line in the spec's file in this worktree (found by `[spec] glob`; the spec is `--spec`, else this node's), replacing a `verified:` line or adding one before the closing `---`, whichever commit it was composed for, so a line for a merged commit is rebuilt from its records, not typed. A line that wouldn't read back as the same entries isn't written.

`bmesh done` checks, in order, refusing with exit 3 at the first that fails:

1. The spec reads `done` on main with a `merged` commit main contains (`git merge-base --is-ancestor <merged> <ref>`, `ref` = `origin/main` when the repo has an origin, else `main`); else `bmesh: not done: merged <sha> is not on <ref>`.
2. For a lead only (other roles stop here): the `verified` line is non-empty (`bmesh: not done: verified is empty`) and has no `fail` entry (`bmesh: not done: verified has a failing entry <kind>`).
3. For a lead, each entry is for the tree of `merged` (`bmesh: not done: verified entry <kind> is for tree <t>, merged tree is <m>`) and matched by a record of that kind at that tree with the same `ok` (and model, when the entry names one), whatever its args. A carried entry is matched by a model record at the tree its `c:` names, with the same `ok` and model, whose patch-id is the diff `merged` landed. Else `bmesh: not done: verified names <kind> but no result for tree <t>` (`<t>` the `c:` tree for a carried entry).

An executor's `done` skips the spec check.

On success `done` prints `done <path> <sha8>` (`<sha8>` the `--commit` given); `started`, `progress`, `ask` and `blocked` print `<event> <path>`. `inbox` prints one line per message, `<at> <from>: <text>` (a reply as in Replies), or `inbox: empty` when nothing waits (nothing under `--json`).

A `done` for a node already `done` is compared with its newest `done` event's commit: the same commit (equal, or one an abbreviation of at least 7 characters of the other) exits 0 with `already done at <sha8>`, checking and publishing nothing; another is refused, exit 3, `bmesh: refused: <path> is already done at <sha8>, not <sha8>`.

A lead's `--commit` is the commit `main` points at after its push, the follow-up commit recording `done`; the spec check reads `merged` and the results, not this sha.

Internals: [docs/internals.md](docs/internals.md#verified)

## Landing

```
bmesh land [--spec NNN] [--ttl D] [--wait D]
```

`land` lands a lead's branch on `main` from its worktree in one command, under the lease `land.<repo>` (`<repo>` the node's repo, else the registry row or base name of the main checkout), so two leads landing at once queue. The spec is `--spec`, else the node's; the holder is the node, else the host name.

Before the lease, `land`:

1. Stops (exit 3, nothing pushed) when run from a node held in `bm-holds` at either gate, `bmesh: held before land by <by> <age> ago: <why>; ...` (see Holds).
2. Refuses (exit 3) a dirty worktree, and one not on a branch or on `main`.
3. Run from a node, reads its inbox as the daemon's wake does (see Config: Wake text), holding and acking nothing. An unread message sent by `operator` with `bmesh send` refuses it (exit 3), `bmesh: refused: <n> unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh land again`; there is no `--force`. Anything else unread (a report, a reply, another node's message) is named on one line, each item as the wake names it, `unread: done from <path> (branch <b> at <sha8>); reply to job <id> (<kind> ok|fail); message from <path>`. Outside a node there is no inbox and no check.
4. Prints the kinds it will require of the landed tree and where each came from, `required: verify (the repo's verify kind), review (t:<tree8> at HEAD)`, or `review (t:<tree8> before the rebase at <sha8>)` for a result from before a rebase by hand (`required: nothing` when none).
5. While another holder has the lease, prints `waiting for land.<repo> (held by <holder>)` once per holder and retries every 2 s, giving up after `--wait` (default 30m; exit 5, `bmesh: timed out waiting for land.<repo>, held by <holder>`).

The lease lasts `--ttl` (default 10m, at least 3 s) unrenewed and is renewed every ttl/3, so a dead lander blocks the next for at most that long. Holding it, `land` reads the hold and the inbox again, so an operator message that arrived while it waited refuses it before the fetch; one arriving during the rebase and checks is not seen. It prints `lease land.<repo>`, then a line per step:

1. `fetch origin/main <sha8>`: `git fetch origin main`; refused (exit 3) while the main checkout's `main` has commits `origin/main` doesn't (unpushed work, which `land` never pushes). Without an origin the base is the local `main`.
2. `rebase onto <sha8>: N commits` (or `rebase: on <sha8> already`). A failed rebase is aborted and refused (exit 3), `bmesh: refused: rebase onto origin/main conflicts in <files>; it is aborted and nothing is pushed. Rebase yourself, resolve it, re-validate, then run bmesh land again`. A branch with nothing beyond the base is refused (`nothing to land`), as is a spec not reading `building` in the worktree.
3. `scaffold: follows the row`, for a repo with a `ship` row whose `AGENTS.md` (in the rebased tree) has a `<!-- bmesh:` marker. Each marked block of `AGENTS.md` that differs from the base's (`origin/main`'s), and the verify kind's command when it differs from the base's, is compared with what `scaffold` writes from the row and the binary. A file the branch didn't change isn't compared, so a row published mid-build, a newer snippet or another box's older binary never refuse a landing; a file `scaffold` would create isn't drift.
    - Drift prints the first 20 lines of its unified diff (`...` after them) and is refused (exit 3), `bmesh: refused: the branch's scaffolded files differ from what bmesh scaffold writes from <repo>'s row; the row's verify is the source: bmesh repos propose with the new verify, or restore the scaffolded files`. Once the row is published with the new verify, `bmesh scaffold <worktree>` writes the files on the branch.
    - A repo never scaffolded prints `scaffold: not scaffolded, not checked`; one with no `ship` row prints nothing.
    - A tree whose `go.mod` declares this binary's own module renders its own blocks, which this binary may predate, so only the verify kind is compared: `scaffold: the verify kind follows the row (AGENTS.md is rendered by this tree's own bmesh, not checked)`.
4. The results for the rebased tree. Required: the kinds with a result at the branch's tree before the rebase, and `verify` when the repo defines it.
    - **A branch rebased by hand** is read as it was before, by walking its reflog (`git reflog show refs/heads/<branch>`): when HEAD is the commit a rebase made (a `rebase (finish)` entry at HEAD; `rebase -i` and `pull --rebase` count), the entry before it, the head the rebase rewrote, counts as land's own head before the rebase, and so on back while that head was itself made by a rebase. A commit made after the rebase ends the walk and HEAD is read as it stands, as is a branch whose reflog has no rebase or has expired (`gc.reflogExpire`, 90 days), or that was rebased on another box. A kind with no result at HEAD's tree is required when that earlier head's tree has one, judged against that head and its fork point from main, as if land had done the rebase: a review lost to a conflict the lead resolved by hand carries when the resolution left the diff unchanged, and is refused otherwise, as below. A review's findings fixed in a commit and then rebased land on verify alone, the fix's tree having no review.
    - **An inline kind** runs as `routine run` does (a deterministic one is answered from its record) and prints its verdict line; a failing one prints its output and exits 1.
    - **A deterministic kind** (`verify`) with no result for the rebased tree first looks for an ok result at a tree differing only outside the repo's code (its row's `code`, see repos.toml; `git diff --quiet <old> <new> -- <code>`), in order: the last tree it was ok at in this run, the branch's tree before this pass's rebase, the tree its `required` line named. The first found is carried: stored again for the rebased tree and HEAD, its output led by `carried from t:<old8>: nothing under code changed`, so `done` and `spec verified` match it as any record, and printed `verify ok t:<new8> (carried from t:<old8>: nothing under code changed)`, in seconds. A change under the code, or to `.bmesh/`, runs it.
    - **Another kind** (a review) prints the first that applies:
        1. its result for the rebased tree, `<entry> (recorded)`;
        2. a result recorded with the rebased diff's patch-id (see `verified`), `<kind> ok t:<new8> m:<model> c:<old8> (carried from t:<old8>: the diff is unchanged)`;
        3. an ok result from before the rebase with no patch-id whose diff the rebase left unchanged (the patch-id of the fork point to the old head equals that of the new base to the new head): each model's result is stored again with that patch-id and lands on the line as a carried entry, `<kind> ok t:<new8> m:<model> c:<old8> (carried from t:<old8>: the rebase left the diff unchanged)`, which `done` matches as any carried entry. A result with no model has nothing `done` can match, so it carries off the line: `<kind> ok t:<old8> carried: the rebase left the diff unchanged (not on the verified line)`.

        A failing result, or a changed diff, is refused (exit 3) naming the kind and how to re-run it.
    - Any failing entry for the tree is refused too.
5. `verified: ...` and `spec NNN done: merged <sha8>, branch <b>`: the `verified` line as `spec verified --write` composes it, and `status: done` (from `building`) with `merged` the rebased HEAD and `branch` the worktree's branch. A spec whose `## Needs from the operator` section has steps also gets `operator_steps` (see Steps for the operator under The ledger), printed `operator steps: N (bmesh spec steps NNN)`.
6. `commit <sha8> NNN: done at <merged8>`: the spec's change, committed on the branch.
7. `push origin main <sha8>`: `git push origin HEAD:refs/heads/main`, a fast-forward or nothing. A push refused because `origin/main` moved takes the record commit back off the branch, prints `main moved; again from the fetch` and restarts at step 1, once; step 4 then carries the verify it just ran when what moved `main` is outside the code (a coordinator's ledger commit, or, with `code` set, a README commit). A second move is refused (exit 3), nothing pushed. Any other failure takes the commit back and exits 1.
8. `main <sha8>`: the main checkout's `main` fast-forwarded as `choose-ideas` does (`merge --ff-only` when it is on `main`, else `fetch . <branch>:main`); when it can't, `land` says so on stderr and the landing stands, `origin/main` holding the commit. Without an origin this step is the landing, and a `main` that moved is retried as a refused push is.

It ends `released land.<repo>` and `landed <sha8>`, the record commit `bmesh done --commit` names. When `land` stops at any step, nothing is pushed and the branch holds no record commit, so it runs again as it stands.

Internals: [docs/internals.md](docs/internals.md#landing)

## Hosts

Boxes that share the hub act on each other through it, with no SSH between them: the admin's signed roster says which boxes belong, every message between boxes is signed, and a box runs only the ops its roster row accepts (`ping`, `level`, `propose`, `prune`, `forget-worktree`, `stop-thread`, `check-step` and `nix`, each below). Nothing here runs unless `[hosts] enabled = true`, which only `bmesh init --hosts` sets; without it the daemon never pushes or pulls anything. Every `bmesh hosts` command except `--here` and `bmesh sync` is refused (exit 3) on a box with `[hosts]` off (`` hosts are off on this box ([hosts] enabled = false): run `bmesh init --hosts` ``) or not yet joined (`` this box hasn't joined (no <state dir>/roster.json): run `bmesh init --hosts` ``).

**Joining.** `bmesh init --hosts [--admin]` prompts for: join with an invite, or be the admin (`--admin` answers this); whether this box is **secure** (see Several boxes); whether its daemon runs routines (default: no on a secure box). Run again, it defaults to the current answers (from `config.toml`, `host.key` and `<state dir>/roster.json`). It needs a `config.toml` (`bmesh init` first; else refused, exit 3: `` no <path>: run `bmesh init` first, then `bmesh init --hosts` ``) and a `[host] name` (or host name) that is one subject token: `^[a-z0-9][a-z0-9-]*$`, no `--`, at most 63 characters (else `host name "<name>" isn't one subject token: set [host] name in <path>`, exit 3).

Last, after the keys and the join, it writes the `[hosts]` table (`enabled`, `admin`, `secure`) and, with routines off, `[runner] checkouts = ""` (routines back on drops a `checkouts = ""` line). Other `config.toml` lines stay, the old file is kept as `config.toml.prev`, `repos.toml` is not touched, and `--force` still starts over. Restart the daemon afterwards.

- **The admin** (`--admin`) makes the admin key (below) if missing and publishes roster v1 with its own row (its approve key is the admin key; it accepts `ping`): `admin: <host> <fingerprint>, roster v<N>`. Run again, it republishes only when its own row changed. If the hub holds a roster signed by another key it prints `a roster signed by <fingerprint> exists; becoming admin replaces it on every box you invite again`.
- **The invite.** On the admin, `bmesh hosts invite <name> [--secure] [--accept a,b] [--replace]` (default `--accept ping`) prints `invite for <name>: <id>-<secret>` and waits. The id (8 hex characters) isn't secret; the secret is 128 random bits in base32. The code works once, within 10 minutes. A name already in the roster needs `--replace`; the admin's own name is always refused (its row carries the admin key).
- **The join.** On the new box, `bmesh init --hosts` prompts for the code (never argv, so it isn't in `ps` or the history) and sends the admin a join request authenticated with a key derived from the code (the handshake is in [internals](docs/internals.md#hosts)). The admin burns the invite on the first request that proves the code; a failing request is dropped without burning, and the third failure burns it. The name must be the invite's. The admin publishes the next roster with the box's row (its approve key only for a `--secure` invite) and replies with the roster and its key; the new box checks that the roster carries its key, pins the admin key and stores the roster: `joined: roster v2, admin <host> <fingerprint>`. A join with no valid reply is refused (exit 3) and stores nothing: `join: no answer to invite <id>: the code is wrong, used or expired`, or `join: refused: <reason>` from the admin.

A fresh invite re-pins a box pinned to another admin; a joined box may leave the code empty to keep its roster.

**Keys.** Every joined box has an Ed25519 host key in `<config dir>/host.key` (the seed in hex, mode 0600). Its agents can read it, so it proves only "this box".

The admin box, and every secure box, also has one key agents can't use: the admin key (signs rosters, registries and `nix` jobs) or the approve key (signs approvals). Its public half is in `<config dir>/admin.pub` (or `approve.pub`); only signing reads the key itself, from the store `[hosts] key_store` names:

- **`keychain`** (the default on macOS): the login Keychain, service `bmesh`, account `admin` or `approve`, written through `security -i` with the key on stdin (never in `ps`) and an empty trusted-app list (`-T ""`), so every read raises the macOS login-password dialog. bmesh prints `Keychain dialog follows for: <action>` first; deny any unannounced dialog, and choose "Allow", never "Always Allow". Before reading it checks the item's access list (`security dump-keychain -a`) and refuses when an app was added (what one "Always Allow" does) or every app is trusted: `the admin key's Keychain item trusts <app>; remove it in Keychain Access`. Over SSH there is no dialog and the read fails: run admin and approve commands at the Mac's own screen.
- **`file`** (the default elsewhere): `<config dir>/admin.key` (or `approve.key`), mode 0600, the seed sealed with XChaCha20-Poly1305 under `scrypt(passphrase, salt, N=32768, r=8, p=1)`. The passphrase is read from the terminal without echo (twice when making the key).

The admin commands also refuse (exit 3) when stdin isn't a terminal (`<action> needs the admin at this box's own screen (stdin is not a terminal)`); the dialog and the passphrase are the control.

**The roster** is the admin's signed list of boxes: for each, its name, host key, `secure` flag, approve key and the ops it accepts. A box keeps its pinned admin key and newest valid roster in `<state dir>/roster.json` and accepts a roster only when the pinned key signed it and its `version` is higher. The admin alone publishes the next version; every daemon re-reads the hub's roster each minute and before each job, and puts its own back when the hub's copy is missing, unsigned or lower, so a roster a token holder deleted or rolled back is back within a minute.

On the admin box:

```
bmesh hosts set <name> [--secure=true|false] [--accept a,b]   # change a row (secure needs the box's approve key: a --secure invite)
bmesh hosts revoke <name>                                       # the next roster without it; no other box can revoke, and the admin can't revoke itself
```

Each prints `roster v<N>: …`. `invite`, `set` and `revoke` are refused (exit 3) on a box that isn't the roster's admin (`only the admin box (<admin>) can <action>`) or outside a terminal (above). A fingerprint is the first 16 hex characters of a key's SHA-256.

**Presence.** Each daemon publishes, each minute and signed with its host key, what it is and has: its OS, revision, whether it runs routines, the registered repos it has checked out (each with its path and `origin`, the checkout's `origin` URL, else the row's `remote`) and its notes. The notes are `<config dir>/notes.md` (the box's session guidance, see Several boxes), read each minute, invalid UTF-8 dropped, cut to 16 KB on a character boundary (logged once) and trimmed; `""` without the file. bmesh never takes a path or a decision from presence.

`bmesh hosts [--json]` prints `roster v<N>, admin <host> <fingerprint>`, then one row per roster box: its state (`awake`; `asleep`, the entry's server timestamp over 3 minutes old; `unverified`, its signature fails, shown without notes; `absent`), the `admin` and `secure` marks, OS, revision, routines, repo count, `notes` when it has some, what it accepts and when it was seen. `--json` prints `{"roster_version","admin","fingerprint","hosts":[{"host":{row},"state","at","presence":{…}}]}`.

`bmesh hosts --notes [--json]` prints every box's notes in roster order, `## <host> (<state>)`, a blank line and the notes, a blank line between boxes (`no notes` when none has any; `--json`: `{"<host>": "<notes>"}`). An unverified box's are withheld, with `bmesh: <host>: unverified, notes withheld` on stderr. Notes hold a box's session guidance: what is local on it, what it can't reach, how it differs from the others.

**The session line.** Each minute, after presence, the daemon writes `<state dir>/this-box.txt` (through a temporary file and a rename):

`this box is <host>[ (admin)]; secure: yes|no; on the mesh: <host> (<state>[ <age>][, secure][, admin]), …`

It lists every other roster box with its presence state (an asleep one with how long ago it was seen, `asleep 2h`: minutes, hours, or days past two days), or `no other box`, and ``; notes: `bmesh hosts --notes` `` when any verified box has notes.

`bmesh hosts --here` prints that file and nothing else; the plugin's session hook runs it (see The skills). It never dials the hub and never runs or prompts for `init`. A file over 3 minutes old (its daemon stopped or can't reach the hub) gets `; as of <age> ago, not updated since`. With `[hosts]` off or no file it prints nothing (exit 0). After it, `[hosts]` on or off, comes the stale-hub line `hub runs <hub>, serve <serve>: restart bmesh-hub` (see Several boxes).

**Jobs and replies.** A job for a box waits in its inbox on the hub until the box takes it, so a box that was asleep gets what it missed, in order. A job lives 24h unless said otherwise. `sync`, `prune` and the `repos` commands print each answer as it arrives, `<box>: <answer>`, and for a box silent past its `--wait` an `asleep, queued until …` line (`no answer in <wait>` in place of `asleep` when the box's presence is awake); the box takes the job when it wakes. `ping` and `node` print their own forms (below). Host jobs and replies never show in the map, `watch`, status or the activity reader.

Every job, reply, approval, roster, registry and presence entry is signed with its author's key. A receiving box checks a job in this order and stops at the first failure. A refusal is answered `refused: <check>`:

1. The sender is in the roster (an op that needs the admin is checked against the admin key); else `refused: roster`.
2. The signature verifies; else `refused: signature`.
3. The job is addressed to this box; else `refused: to`.
4. The sender's roster version isn't ahead of this box's. Else the job waits a minute and is checked again, until the box has caught up (or while it can't read the hub).
5. The job's time is at most 5 minutes ahead of this box's clock; else the job waits a minute.
6. The job hasn't expired and lives at most 24h; else `refused: expired`. An expired job is acked, answered and logged only.
7. The op is in this box's `accept`; else `refused: accept`.
8. The job hasn't been seen before; else `refused: replay`.

A refused job never runs. A job delivered again after it ran gets its stored reply again, never a re-run. One delivered again after a crash or sleep mid-run is resumed when its op is idempotent (`ping`, `level`, `prune`, `stop-thread`, `check-step`), else answered `interrupted`. Roster, signature, `to` and replay failures are pushed once by any daemon with `[notify]` and `[hosts]` (title `<host> refused <sender>: <check>`, priority `high`), at most once per sender and check every 10 minutes (see Pushes). The caller checks a reply likewise: signed by the box the job went to, naming the job.

**`ping`** answers `<rev8>, roster v<N>`. `bmesh hosts ping [<host>] [--wait 10s]` sends it to every roster box (itself included), or to the one named, and prints each answer (`k16: 1a2b3c4d, roster v2`); a silent box prints `<host>: asleep: queued until <YYYY-MM-DD hh:mm>` (`no answer in <wait>` in place of `asleep` when its presence is awake).

`bmesh hosts replies [--json]` prints every reply this box's jobs got (`<ts> <host> <op> <job id>: <text>`), checked against the job in the stream and the roster; one that fails is shown as `unverified, withheld`.

**Waking.** A daemon detects a wake when two of its minute ticks are 3 minutes or more apart on the wall clock (monotonic time stops while a box sleeps). It logs the wake, re-reads the roster and self-levels (below).

**Levelling** keeps every box's checkout of a repo level with origin; on a secure box, code the operator hasn't seen waits for approval (below). A level, the daemon's routine fetch and the fast-forward of `main` in the main checkout by `bmesh land`, `build` or `choose-ideas` take one lock per repo (the repo's lock), so they never collide; with `[hosts]` off nothing takes it.

- **`level`** is a built-in op: `repo` the registry name, `args` `[<sha>, <branch>, <until>]` (origin's head after the sender levelled, the sender's branch, `""` for a pinned row, and when the sender stops waiting). The receiving box judges it against the checkout of its own registry row, which must exist (else `refused: no checkout of <repo> here`):
  1. `git fetch origin`, plain (for a row with a `pin`, also that tag, never forced).
  2. The target is `origin/<branch>` (the job's branch, else the checkout's current one), or the pin tag's commit for a pinned row. A missing one is `refused: no origin/<branch>` or `refused: no tag <pin>`; a checkout with no origin, `refused: no origin`.
  3. The job's sha must be in the target's history, else `refused: <sha8> not on origin/<branch>` (or `not on <pin>`).
  4. The checkout check moves nothing and answers:
     - `refused: in progress`: a `MERGE_HEAD`, `CHERRY_PICK_HEAD`, `REVERT_HEAD`, a `rebase-merge` or `rebase-apply` directory, or an `index.lock`;
     - `refused: dirty`: uncommitted or untracked changes;
     - `refused: other branch`: on another branch than the target's, or detached, for a row that isn't pinned;
     - `refused: diverged`: both ahead of and behind the target.
  5. Then, by where the checkout stands:
     - A pinned row moves to the tag, detached (`moved to <pin>`), unless it carries commits no remote branch or tag has (`ahead <N>, not pushed: pinned to <pin>`); a pinned row is never pushed.
     - Behind only: `merge --ff-only`, answered `fast-forwarded <N>`.
     - Level with the target: `up to date`.
     - Ahead only: `git push origin <branch> <tags…>` (the tags that point at the pushed commits, lightweight ones included), answered `pushed <N>`, when the caller may push (a received job or `bmesh sync`, never a self-level), the row's mode is in `[hosts] push_modes` (default `["ship", "fork"]`) and the newest local commit is at least 10 minutes old; else `ahead <N>, not pushed: <why>` (`a self-level never pushes`, `mode <m> is not in push_modes`, `the newest commit is <age> old (10m0s before a push)`). If another box pushed first, the push fails as not a fast-forward: `refused: diverged`.
  6. **Plugin refresh.** After the checkout moves, the box refreshes what it loads from it: with `.claude-plugin/marketplace.json` in it, `claude plugin marketplace update <name>`, then `claude plugin update <id> --scope user` for each user-scope install from that marketplace whose version lags the checkout's; with `codex` on `PATH`, `codex plugin add <id>` for each installed Codex plugin whose marketplace source is the checkout. Each refresh adds a part to the answer (`fast-forwarded 3; claude pstack@pstack-claude 0.15.24 -> 0.15.25`); a failure is appended as `plugins: <error>`. A new session loads the change. bmesh runs no other command a job or the registry names.
- **The hold.** On a secure box (its `[hosts] secure`, or its roster row's), a level that would move a repo sessions load waits for the operator, from a job, a self-level or `bmesh sync` alike. Such a repo is a plugin marketplace (`.claude-plugin/marketplace.json`, `.agents/plugins/marketplace.json`, or the marketplace source of a Codex install) or the `[update] repo`. A push moves nothing here, so it isn't held.
  - The hold keeps the job, the repo, its head and target, and `git log --stat HEAD..<target>`; the job is answered `held for approval (+<N> commits)`. A repo has one hold: a newer job's replaces it and the older job's sender gets the reply `hold superseded by job <id>`; a self-level leaves an existing hold as it is. The box pushes the hold once (with `[notify]`; title `<host> holds: <repo> +<N> commits (<sender or self-level>)`, priority `default`).
  - `bmesh hosts held [--json]` lists the holds: `<id> <repo> +<N> commits to <target8>, from <sender>, expires <day hh:mm>`.
  - `bmesh hosts approve <id>`, at the box's own screen, shows the log again, reads the approve key (the admin key on the admin box; the announced dialog or the passphrase), signs an approval of the job at the target the operator saw, and sends it to the box's daemon. The daemon checks that the approval is signed by the box's roster approve key, names the hold's job and target, and is within 5 minutes of its clock; then that the job's signature still verifies against its sender, who is still in the roster, else it drops the hold; then levels. When origin moved since, nothing moves: the hold takes the new target and log, and the command shows them and prompts again (`Approve <repo> at <target8>? [y/N]`, signing again with the key it read). Approved, it prints `<repo>: fast-forwarded <N> (approved)`, which the job's sender gets as a second reply. It is refused (exit 3):
    - for an id this box holds no hold for: `` no hold <id> on this box (`bmesh hosts held` lists them) ``;
    - outside a terminal: `approving needs the operator at this box's own screen (stdin is not a terminal)`;
    - when the roster gives this box no approve key: `<host> has no approve key in roster v<N>: ask the admin to invite it again with --secure --replace`;
    - with no daemon running: `` no daemon on <host> answered: is `bmesh serve` running with [hosts] enabled? ``;
    - when the daemon answers that it did not level: `not levelled`, after the daemon's answer.
  - `bmesh hosts deny <id>` drops the hold; the job's sender gets the reply `denied by the operator`. An id with no hold is refused as above.
  - A hold expires with its job (24h, a self-level's too) and is dropped, logged only.
- **`bmesh sync [--repo R | --all] [--wait 30s]`** levels this box's checkout of the repo (default: the registered repo of the current directory; `--all`: every registered repo this box has), then sends `level` with origin's head to every roster box that accepts `level` and whose verified presence lists the repo, and prints the answers: `k16: fast-forwarded 3`, `ricks-macbook: held for approval (+1 commit)`, `ricks-macbook: asleep, queued until <day hh:mm>`. With several repos each line starts with `<repo>: `. It is refused for a repo the registry doesn't name (`<repo> is not in the registry`) or this box hasn't checked out (`no checkout of <repo> on this box`).
- **Self-level.** Each daemon with `[hosts] enabled` levels every registered repo it has a checkout of, on start and on each wake, as a fast-forward only, never a push. Refusals are logged, not pushed; holds apply as above.
- **Pushes.** A refusal or failure of a received `level` whose sender stopped waiting (its `until` has passed) is pushed once (title `<host>: <repo> <answer> (job from <sender>)`, priority `high`): when two boxes are ahead at once, the second's push is refused as not a fast-forward and the phone gets `diverged`.

**The registry.** With `[hosts]` on, `repos.toml` is the admin's signed document, since a row's `verify` is shell every lead runs (`bmesh scaffold` writes it into `.bmesh/routines/verify.toml`; `bmesh land` refuses a branch that changes it there or in `AGENTS.md` by hand) and its `path` is what `nix` deletes. With `[hosts]` off it is a plain local file.

- The admin publishes its `repos.toml` as written, comments and all. A box takes a registry only when its pinned admin key signed it, its `version` is higher than the box's own, and the box's own loader reads the rows (with `repos.toml`'s refusals, an unknown key among them). It writes the rows as its `repos.toml` (through a rename, the old file kept as `repos.toml.prev`); its hosts layer (presence, levels, prune and nix) reads the new rows at once, the routine loop at start. A registry it can't read leaves the file alone and leaves the newer registry on the hub for the boxes that can. It is pushed once per version (with `[notify]`): `<host>: refused: registry v<N> needs a newer bmesh (<error>)`, priority `high`.
- Every daemon re-reads the hub's registry each minute (so at start and on wake), putting its own back when the hub's copy is missing, unsigned or lower, as with the roster. A box re-pinned to a new admin starts again from no registry.
- **`bmesh repos publish`**, on the admin box, prints the row diff from the current registry to this box's `repos.toml` (`registry v<N> -> v<N+1>:`, then `+ <name>: <field> <value>, …`, `- <name>` and `~ <name>: <field>: <old> -> <new>` lines, or `(comments and layout only)`), reads the admin key (the announced dialog: `publish registry v<N+1>`) and publishes the next version, `registry v<N+1> published`; the version comes from the box's own record, never the hub's. An unchanged file prints `registry v<N>: no change`. On a box that isn't the admin it is refused (exit 3): `` only the admin box (<admin>) publishes the registry: run `bmesh repos propose` to send it this box's changes ``.
- **`bmesh repos propose [--wait 30s]`**, on any other box (on the admin box it is refused, exit 3: `` this is the admin box: publish with `bmesh repos publish` ``), prints the same diff against the registry it has (`proposed against registry v<N>:`) and sends the box's `repos.toml` to the admin box as a `propose` job (args `[<base version>, <rows>]`; the admin box's row must accept `propose`). The admin's daemon refuses rows it can't read and a proposal against an older version than its registry (`refused: proposed against registry v<N>; it is v<M> now: propose again`). Otherwise it holds the proposal with its row diff, pushes `<admin> holds: registry proposal from <box>, <N> row changes` and answers `held for approval as <id> (<N> row changes against registry v<base>)`; a newer proposal from the same box supersedes it. Nothing changes until it is approved:
  - `bmesh hosts held` lists it: `<id> registry proposal, <N> row changes against v<base>, from <box>, expires <day hh:mm>`.
  - `bmesh hosts approve <id>` at the admin's screen shows the diff, reads the admin key and publishes the proposal as the next version, `registry v<N> published: proposal from <box> approved`; the proposer gets the reply `approved: registry v<N> (<N> row changes)`. While the admin's own `repos.toml` has changes the registry doesn't, the approval is refused and the proposal stays held.
  - A proposal the registry has moved past, or past its job's expiry, is dropped and its proposer told so. `bmesh hosts deny <id>` drops it too.
- `publish`, `propose` and `remove` first take a newer registry from the hub when there is one, and having just replaced this box's `repos.toml` stop there: `took registry v<N> from the hub as <path> (your file is kept as repos.toml.prev): bring your edits over and run this again`. A newer registry this bmesh can't read is refused too (exit 3): `` the hub has registry v<N>, which this bmesh can't read: <error>; run `bmesh update` ``.

**Pruning.** `prune [<repo>]` is a built-in op: `repo` a registry name, or `""` for every registered repo with a checkout on the box; `args` `["apply"]` to remove, else a dry run. Each box judges its own checkouts by its own nodes:

1. Each linked worktree of the checkout (the main checkout and routine checkouts under `[runner] checkouts` aside) is a row. One under `[runner] worktrees` is bmesh's: it is removed (`git worktree remove`, never forced, then `git branch -d` of its branch, `-D` when the merge was proven by `git cherry` or the lead's branch) when its node (by worktree path) is `done`, it is clean, and its HEAD is merged into `main` (as `node forget --worktree` judges it, see Finished nodes) or into its lead's branch; else it is kept, with the reason `no bmesh node`, `node <state>` (`idle` with no state), `uncommitted changes` or `unmerged`. Any other worktree is kept, `not bmesh's (T3 cleans its own)`: T3 0.1.0 removes a thread's worktree once it has no commits beyond the default branch, keeping one with uncommitted changes.
2. Then every local `bmesh/*` branch no worktree holds is removed when `git branch -d` accepts it (merged into its upstream, else into the checkout's HEAD), never with `-D`; any other is kept, `unmerged`.

With `apply` it holds the repo's lock while it removes. The answer is a line per row, `<repo> <worktree path> <keep|remove|removed|failed> <reason>` or `<repo> branch <name> <keep|remove|removed> <reason>` (a failed removal adds `  (failed: <error>)`), then `<N> worktrees, <B> branches, <M> to remove, <K> kept` (applied: `, <R> removed`, and `, <F> failed`). A named repo the box has no checkout of answers `no checkout of <repo> here`.

`bmesh prune [--repo R | --all] [--apply] [--wait 30s]` (default: the registered repo of the current directory) prunes this box first, then sends `prune` to every other roster box that accepts it and prints each box's lines as `<box>: <line>`, or `<box>: asleep, queued until <day hh:mm>`. With `[hosts]` off it prunes this box only. It exits 1 when a removal on this box failed.

**Another box's worktree.** `forget-worktree <node path>` runs the removal of `node forget --worktree` on the node's own box, with the same rules (see Finished nodes), and on success forgets the node there too: `removed worktree <path>; deleted branch <b>; forgot <path>`. Beyond the removal's own refusals: `refused: no node <path>`, `refused: <path> is on <host>, not <box>`, `refused: <path> is <state>; only a done or blocked node is forgotten`.

`bmesh node forget --worktree` of another box's node (see Finished nodes) sends the job to that box, which must accept it, and forgets the node only on success, printing `<box>: <answer>` and `forgot <path>`. A refusal is printed and exits 3, forgetting nothing. A box that doesn't answer in time prints `queued; the node stays until <box> answers`; on waking it takes the job and forgets the node itself.

**Another box's thread.** `stop-thread <node path> <who> <why>` stops one of the box's own nodes as `bmesh node stop` does there (see Stopping a node): it marks the node `blocked` with `stopped by <who>[: <why>]`, publishes that `blocked`, interrupts the thread through the box's runner and answers `stopped <path>`, or `already <state>` for a `done` or `blocked` node (a stopped one is interrupted again). It refuses `refused: no node <path>`, `refused: <path> is on <host>, not <box>` and `refused: <path>'s runner is "<kind>", not this box's <runner>`.

`bmesh node stop` sends it for each thread on another box (see Stopping a node; each box must accept it) and prints `<box>: <answer>` for each; refusals are printed and exit 3. A box that doesn't answer in time has the node marked stopped from the sender, printed `queued; <path> is marked stopped and <box> stops its thread when it answers`; on waking it takes the job and interrupts the thread.

**A step's check.** `check-step` (`repo` a registry name, `args` `[<spec id>, <step number>]`) runs one step's `check:` for `bmesh spec steps --check` (see Steps for the operator under The ledger). The job names the step, never a command. The box fetches its own checkout of the repo (under the repo's lock), reads the spec's `operator_steps` from its `origin/main` (`main` without an origin) and runs that step's check through the platform shell in the checkout, for at most 2 minutes, answering `exit 0`, or `exit <code>: <the output's last line>`. It refuses `refused: no checkout of <repo> here`, a spec or step it can't find, `refused: step <n> is for "<box>", not <host>` and `refused: step <n> has no check`. The job lives 10 minutes, since its sender stops listening after `--wait`. Accepting `check-step` lets a box run the `check:` commands of specs on `main`, as a box with routines runs main's kinds; the admin chooses per box (`bmesh hosts set <name> --accept ...`).

**Removing a repo everywhere.** `nix <repo>` is a built-in op signed by the admin key, from the admin box. It carries the repo name and the origin of the repo's row in the admin's registry (`args` `[<origin>]`, plus `ignored-ok`), never a path: the path comes from the receiving box's own registry row. A repo with no row there answers `absent: <repo> is not in this box's registry`; a row whose checkout doesn't exist, `absent: no checkout at <path>` (its routine checkouts are still removed).

The box refuses, touching nothing, unless the directory (symlinks resolved) has the name of the row's path, is a checkout's top level, has an `origin` (`refused: <path> has no origin`) that is the job's (`refused: <path>'s origin is <url>, not the registry's <origin>`; a GitHub URL matches its `owner/name`, and a trailing `/` or `.git` doesn't count), and the box's own row names no other remote. It also refuses:

- a checkout with uncommitted or untracked changes;
- a linked worktree (other than a routine checkout) with uncommitted or untracked changes, or whose HEAD is not in `main` or `origin/main` (`refused: worktree <path> is on <branch>, not merged into main`);
- a commit on no remote (`git log --branches --tags --not --remotes`, so an unpushed tag too: `refused: <path> has commits on no remote (<commit>)`);
- a stash;
- ignored files, in the checkout or a linked worktree, unless the job says `ignored-ok` (`refused: <path> has ignored files (<paths>): nix with ignored-ok to remove them too`).

On success it removes the linked worktrees (`git worktree remove`), the routine checkouts `<[runner] checkouts>/<checkout name>` and the checkout, under the repo's lock: `removed <path> and <checkouts dir>[, with <N> linked worktrees]`.

`bmesh repos remove <name> --checkouts [--ignored-ok] [--apply] [--wait 1m]`, on the admin box, sends `nix` to every roster box whose verified presence lists the repo (the admin box too; each must accept `nix`), prints each answer, and once every box has answered `removed` or `absent` publishes the next registry without the row: `registry v<N>: <name> removed`. Until then it prints `pending: <box>, …; the registry keeps <name> until each answers removed or absent (run this again to re-ask them)` and remembers the boxes and their jobs; run again, it counts the answers that arrived since and re-asks only the boxes still pending. One read of the admin key signs the jobs and the registry. Without `--apply` it prints the boxes it would ask and the version it would publish, and sends nothing. It is refused (exit 3) on a box that isn't the admin (`only the admin box (<admin>) removes a row from the registry`), for a row the registry doesn't have (`<name> is not in registry v<N>`), for a row with no `remote` (`<name> has no remote in the registry: nix matches each checkout's origin against it`), and while this box's `repos.toml` has changes the registry doesn't (`publish them first`). Without `--checkouts` it only publishes the registry without the row; with `[hosts]` off, `bmesh repos remove <name> [--apply]` takes the row out of this box's `repos.toml` (keeping `repos.toml.prev`), and `--checkouts` is refused (`` --checkouts sends nix to every box: it needs [hosts] enabled (`bmesh init --hosts`) ``).

**Threat model.** Whoever holds the hub token can read every message, delete keys and streams and flood subjects; signatures don't stop that. They stop such a party, or any box not in the roster, from getting a box to act. Revocation is best-effort against a token holder, who can delay a box's view of the newest roster but can't forge one. A box's `host.key` is readable by its own agents, so a compromised box can act as itself until it is revoked. The admin and approve keys are the secrets no agent can use.

Internals: [docs/internals.md](docs/internals.md#hosts)

## Names

- A node path matches `^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)*$`.
- Its first token is never `inbox`, `daemon`, `feedback`, `host` or `hostreply`, which start the subjects that are not a node's events. `routine` is the routine pool's (`routine.<kind>`, `routine.<kind>.<instance>`).
- No token contains `--`: the inbox consumer name maps `.` to `--`, so `a--b` and `a.b` would collide.

**Node token.** A registry name may hold capitals, `_` and `.` (see repos.toml); a path token can't. So every path, family and branch made from a repo's name (`<repo>-round-<N>` and its children's branches, `<repo>-<NNN>`, `<repo>-build-<N>`, the branches `build` and `choose-ideas` record on `main` through) starts with its node token: the name in lower case, `.` and `_` as `-`, runs of `-` collapsed, a trailing `-` trimmed (`MapleMarketScanner` gives `maplemarketscanner`, `my_repo.js` gives `my-repo-js`). A node's `repo` and the worktree directories keep the registry name. `bmesh init`, `repos publish` and `repos propose` refuse a registry where two names share a token (`Foo` and `foo`, `my.app` and `my_app`): exit 3, `bmesh: repos Foo and foo are both foo in node paths; rename one`.

Internals: [docs/internals.md](docs/internals.md#names)

## Config

Three files live in the config dir, `<os.UserConfigDir()>/bmesh` (`~/.config/bmesh` on Linux, or anywhere that directory already exists): `config.toml`, this box's settings; `repos.toml`, the registry of repos, the same on every box; and `.setup-complete`, the setup flag `bmesh init` writes (see Quickstart). `BMESH_CONFIG` overrides the config file's path; the other two stay beside it.

`bmesh config check` loads both files as every command does and prints `config ok: <path>`, or refuses (exit 3) with `config check failed: <why>`, naming the key. `bmesh serve` (its supervisor too) and `bmesh hub` run the same check at start; refused, they print `bmesh: config check failed: <why>` and exit 78 (`EX_CONFIG`), which the units `init --service` writes don't restart on (see Quickstart).

### repos.toml

One `[repos.<name>]` table per repo. A name matches `^[A-Za-z0-9][A-Za-z0-9_.-]*$` (a directory name, so a checkout keeps it), quoted when it holds a dot (`[repos."my.app"]`). Keys and defaults:

```toml
[repos.party-buzzer]
path = "~/repos/party-buzzer"           # required; this box's checkout (~ expands; relative = from the config dir)
mode = "ship"                           # ship | companion | publish | fork | dormant
remote = ""                             # owner/repo or a URL, for people and the table
host = ""                               # box for proposal threads ("" = this one)
ux_host = ""                            # box for UI/UX threads
verify = []                             # shell commands a change must pass
surfaces = []                           # UI screens a UI/UX thread screenshots, with viewports
pin = ""                                # release tag every checkout stays on
routines = ["verify", "review"]         # routine kinds a build runs
code = []                               # git pathspecs of the code ("cmd/**", "*.go"); none = all but the spec files
```

**Code.** `code` lists git pathspecs from the repo's root, read as `git log -- <pathspec>` reads them (`*` crosses `/`: `*.go` is every Go file, `cmd/**` everything under `cmd/`); a change is code when it touches a matching path. `.bmesh/` (the routine kinds, which define verify) is always code. Unset or empty, code is everything but the spec files `[spec] glob` matches (`docs/proposals/NNN-*.md`), so a `README.md` beside them is code. The code head (see Updates) is what `build` verifies, `release` tags and `[update]` follows, and `land` carries a `verify` across a rebase that changed nothing under code; so list every file the build or tests read (`go:embed` sources, fixtures, documents a test checks), or a change to that file alone is never verified.

**Errors.** One loader serves every command, so an unknown key, a row without `path` or a mode outside the five fails them all. `[repos]` in `config.toml`, the registry's old place, is refused with a message naming `repos.toml`. `bmesh lead start --repo`, `routine queue` and the daemon resolve repo names here.

```
bmesh repos [--json]               # the table: name, mode, remote, verify, host, ux_host, surfaces, pin (sorted by name)
bmesh repos show <name> [--json]   # one row, key: value lines (code: (everything but the spec files) for a row without it)
bmesh repos publish                # (admin, [hosts] on) publish this file as the next registry version
bmesh repos propose [--wait 30s]   # ([hosts] on) send this file's changes to the admin box, held for approval
bmesh repos remove <name> [--checkouts [--ignored-ok]] [--apply] [--wait 1m]   # take a row out (see Hosts)
```

`--json` prints an array (`show`: one object) of `{"name","path","mode","remote","host","ux_host","verify":[],"surfaces":[],"pin","routines":[],"code":[]}`, defaults filled in; `show` of an unknown name exits 3. `bmesh init` adds the current directory's checkout as a row (`path`, and its GitHub origin as `owner/repo`) unless it is there, rewriting the whole file, so comments are lost.

**With `[hosts]` on**, each daemon writes this file from the newest signed registry (the old file kept as `repos.toml.prev`), so a local edit lasts until the next registry. Change it with `bmesh repos publish` on the admin box or `bmesh repos propose` elsewhere (see Hosts: The registry).

### config.toml

Keys and defaults. An unknown key is ignored. `[runner] t3_dispatch` and `t3_project`, which the t3 runner read before T3 0.1.0, each load with one stderr line: `bmesh: [runner] t3_dispatch is ignored (T3 0.1.0 needs no script); delete it from <path>`.

```toml
[mesh]
nats = ""                                   # "" = the server bmesh serve embeds on listen, which other commands dial; else "nats://localhost:4222"
domain = "hub"                              # JetStream domain
token_file = ""                             # "" = no token
store = "<cache dir>/bmesh/jetstream"       # embedded server's store (os.UserCacheDir: ~/.cache on Linux, ~/Library/Caches on macOS); an existing ~/.local/state/bmesh/jetstream is kept
listen = "127.0.0.1:4222"                   # embedded server's bind
[host]
name = ""                                   # "" = `hostname -s`; also the name of the server bmesh hub runs
[hub]                                       # the server bmesh hub runs here (see Several boxes)
role = ""                                   # "" = no hub here | "hub" | "leaf"
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
max_leads = 0                               # leads at once on this host; 0 = no cap
[notify]
ntfy = ""                                   # "" = no digest; else "http://k16:2586/bmesh"
command = ""                                # test hook instead of HTTP: run through the platform shell, POST body on stdin, title in $BMESH_TITLE (and $NTFY_TITLE)
digest_every = "1h"
push = ["ask", "blocked", "done"]           # lead (and round) events pushed at once; only these three (another is a config error); [] = none
map_url = ""                                # map URL for a push's click-through (<map_url>#/lead/<path>); "" = none
catch_up = "1h"                             # on start, push missed events no older than this
[liveness]
grace = "20m"
poll_every = "60s"
wake_every = "10m"                          # an idle lead (or engineer) with unread mail is woken at most this often
[spec]
glob = "docs/proposals/{id}-*.md"
[models]
lead = "claude-opus-5-5"                    # lead start's model when --model names none (build passes the spec's judge)
lead_effort = "high"                        # low, medium, high, xhigh or max; "" = the runner's default
proposer = "claude-opus-5-5"                # a proposal round's two threads
proposer_effort = "xhigh"
[status]
keep_done = "24h"                           # mesh-status hides nodes done longer than this; the daemon deletes stopped routine instances' nodes after it; "0" = never; days work: "30d"
[routines]
keep_results = "30d"                        # bm-results' age limit, applied by serve; "0" = none
[quota]
command = ""                                # the map's quota strip; "" = none. Run through the platform shell, at most every 5m (30s timeout); prints a JSON array of {"reading":{"provider","five_hour","week","five_reset","week_reset"}}
remind_topic = ""                           # ntfy topic for a used 5-hour window's reset reminder; "" = none; needs command and [notify] ntfy (see Quota reminders)
[console]
thread_url = ""                             # the map's thread link for leads here; {thread} = the runner thread, path-escaped (mcp:x is mcp%3Ax); "" = this host's [console.hosts] entry, else no link
[console.hosts]                             # host = template, for leads on other hosts (e.g. k16 = "http://k16:3773/…/{thread}")
[update]
repo = ""                                   # registry name the daemon rebuilds itself from, at what track follows; "" = off (see Updates)
check_every = "6h"                          # without repo: how often to check what track follows on the module's main and write the notice when behind; "0" = never
upstream = false                            # without repo: also go install a newer target, drain and re-exec; on Windows only under serve --restart
track = "release"                           # what every update follows on main: release = the newest vX.Y.Z tag (the code head until there is one); latest = main's code head (see Updates); bmesh update --latest goes there once
[hosts]                                     # written by bmesh init --hosts (see Hosts)
enabled = false                             # nothing of the hosts layer runs unless true
admin = false                               # this box is the roster's admin
secure = false                              # what others ask of this box waits for the operator
key_store = ""                              # keychain | file: where the admin or approve key lives; "" = keychain on macOS, file elsewhere
push_modes = ["ship", "fork"]               # registry modes whose rows a level pushes when this box is ahead (see Hosts)
```

**State dir.** What the daemon keeps across restarts (the T3 sign-in's push record, `roster.json`, the salt records, holds in `held/`, per-repo locks in `locks/`) lives in `$XDG_STATE_HOME/bmesh` when `XDG_STATE_HOME` is set, else in `~/Library/Application Support/bmesh/state` on macOS and `~/.local/state/bmesh` elsewhere (Linux, Windows); never in the cache dir, which macOS purges.

**Wakes.** At most once per `wake_every`, the daemon sends a wake to the thread of a lead or engineer on this host that is idle with unread mail, or silent whatever its mail. A `done` or `blocked` node is woken only for a message sent with `bmesh send` (so a question from you reaches a finished lead; a late report or reply doesn't wake it), a stopped one only for a message from outside its own tree sent after the stop (see Stopping a node), and a thread its runner reports stopped by the user never.

**Silence.** A `working` node is marked `silent` (`quiet for <age>`) when nothing at or under its path was published within `[liveness] grace`; a `blocked` node, a stopped one included, never is.

**Wake text.** `bmesh: ` (`bmesh: resumed by <sender>; ` for a stopped node), then what waits: `` bmesh: 3 unread: done from app-012.alpha (branch t3code/x-alpha at 1a2b3c4d); reply to job <id> (review fail); message from operator. Run `bmesh inbox` now, then continue. ``

| Item | Reads |
|---|---|
| report | `done from <path> (branch <b> at <sha8>)` or `blocked from <path>` |
| reply | `reply to job <id> (<kind> ok\|fail)` |
| other | `<kind> from <from>` |

Up to five items, oldest first, then `and N more`; the count alone (`bmesh: 3 unread. Run ...`) when there is nothing to list or the read fails.

Internals: [docs/internals.md](docs/internals.md#configtoml)

### Pushes

With `[notify] ntfy` or `command` set, a daemon also pushes a lead's events named in `push` to the same topic as they happen:

| Event | Title | Priority | Tag |
|---|---|---|---|
| ask | `<path> asks` | `high` | `question` |
| blocked | `<path> is blocked` | `high` | `no_entry` |
| done | `<path> is done` | `default` | `white_check_mark` |

- **Body:** the event's text, cut to 3 KB; done adds `commit <sha8>`.
- **Tap (`Click`):** the lead's thread when `[console]` resolves it, with an `Open map` action for its map page when `map_url` is set; else the map page. The phone must reach both, so on a tailnet `map_url` and the `[console]` templates use the full MagicDNS name (`http://<host>.<tailnet>.ts.net:3773/…`), not the short host name.
- **`command`** gets the body on stdin and the headers in `$NTFY_TITLE`, `$NTFY_PRIORITY`, `$NTFY_TAGS`, `$NTFY_CLICK` and `$NTFY_ACTIONS`.

**Other nodes.**

- A fixer (see The fixer) pushes like a lead, its `done` titled `<repo> fixes: N fixed, M wait on you` (`waits` for one): `N` counts its report's `<seq>: fixed by` lines, `M` its `<seq>: proposed` ones, and the body puts the proposed lines first, the rest after as written. A fix lead (a `<repo>-fix-<N>` family) never pushes; its fixer reports it.
- A round's `done` and `blocked` and an engineer's `ask` push as Proposal rounds says, the T3 sign-in's expiry as Runners says, and with `[hosts] enabled` a box's refusals as Hosts says (even with `push = []`). Executors' and routines' events never push.

**Once only.** An event is pushed once, across restarts and daemons. On start a daemon pushes, in order, the listed events no older than `catch_up` that nobody pushed yet, then follows live. A push that still fails after retries at 5 s and 30 s is logged and never repeated; at shutdown the push being posted gets 10 s.

Internals: [docs/internals.md](docs/internals.md#pushes)

### Quota reminders

With `[quota] remind_topic` set, the daemon reads quota on start and then hourly, through the map strip's gate, so `[quota] command` still runs at most every 5 minutes, whichever reader triggers it. For each provider with usage in its 5-hour window (`five_hour` above 0) whose `five_reset`, rounded to the minute, is still ahead, it posts one message per window to the `[notify] ntfy` URL with its last path segment replaced by the topic (`http://127.0.0.1:2586/bmesh` and `quota-reset` give `http://127.0.0.1:2586/quota-reset`):

- title `Claude 5-hour quota reset` (the provider's name, capitalised);
- body `Claude's 5-hour window is fresh. Weekly was 41% used.`;
- tag `hourglass`;
- an `At` header of the rounded reset, so ntfy delivers it then; none when the reset is under 30 s away (ntfy refuses a shorter delay), so it posts at once.

A failed post is logged and retried at the next reading.

Internals: [docs/internals.md](docs/internals.md#quota-reminders)

### Map

The map (`/`) draws one block per lead, a tree of the lead and its executors (a round and its engineers alike), then the orphans block, then the routines column (last on a phone, where blocks stack). Blocks run in need order: `blocked`, `asking`, `silent`, `working`, `held`, `idle` (no state yet), `done`. While a live lead (any state but `done`) is on the map, done leads leave the tree for a shelf under the live blocks: chips (`buildermesh-001 ✓`, newest first) headed `done (N)`, each linking its lead page. With no live lead, done leads draw as trees.

**The strip**, under the header, opens with a summary row, `2 working · 1 asking · 1 silent · 2 held · 3 done` (`all idle` when none), over leads, rounds and their children, not routines. Below it come the quota lines, then the newest digest as a fold (`▸ digest 50m ago: <title>`) that opens to its body.

**The recent-events drawer** shows each row's node as a chip in its lead tree's hue (routines and `daemon.*` grey); an inbox row shows the sender's chip, `→`, and a small recipient chip. Hovering a chip dims every row but that node's and those addressed to it; clicking it (a tap on touch) pins that focus until clicked again or Escape.

**The needs-you band** above the map shows, whenever a lead, round or engineer is `asking`, `blocked` or `silent`, one row per such node in need order: state badge, path (linking its lead page) and the state's words (the ask or block text; for a silent node `quiet for 20m` and how long ago). A row leaves on the node's next `started`, `progress` or `done`. After them comes one row per landed spec with operator steps still open, from every configured repo: an `operator` badge, `<repo> NNN`, and `<title>: X of Y open · bmesh spec steps NNN --repo <repo>`, gone once every step is ticked. On the map, an `asking` or `blocked` box's second line is the state and its words; a `held` box (a coordinator holds the lead at a gate) has a badge in its own slate colour and its words as the second line. A `held` lead counts in the strip but never gets a band row, because the coordinator put it there.

**The lead page** (`#/lead/<path>`, for any node) opens with a `tree` card, one row per child (state badge, last segment of its path, branch, `N unread`) linking its page; then the lead's `spec` card, or for a round a `specs` card with one `NNN title · status` line per spec its engineers proposed, opening the body in place (`no specs landed yet` before any lands); then its inbox and timeline. The spec is read from the repo's local `origin/main` (`main` without an origin). While lead pages are viewed, the daemon fetches `origin main` in the background at most once a minute per repo, so a spec shows a minute or two after landing. A card with nothing to show (`no spec`, `no messages`, no events) is one muted line.

Internals: [docs/internals.md](docs/internals.md#map)

## Exit codes

| Code | Meaning |
|---|---|
| 0 | ok |
| 1 | error; a failed run (`routine run`, `routine wait`, an inline kind `land` runs); a lost lease; an interrupted `lock-lease run`; a malformed spec, or a `spec set` its `--from` refuses |
| 2 | usage; a `scaffold` of a repo without a row or whose row is not `ship` |
| 3 | refused: each command's section names its refusals (`config check` and `repos show`: see Config) |
| 5 | timeout (`watch`, `routine wait`, `t3 login`, a `land` waiting for its lease) |
| 78 | config: `serve` or `hub` refused this box's config at start, naming the key (see Config); their units and `serve --restart` don't start it again |

`lock-lease run` exits with its command's code.

## Build and test

```
GOBIN=~/.local/bin go install ./cmd/bmesh
TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test ./...
GOOS=windows go vet ./...    # type-checks the Windows-only files a Linux go vet skips
```

Tests never use `/tmp`: `meshtest.Start` fails when `TMPDIR` is `/tmp`.

For a spec that calls for repeated runs on a Mac (`go test -count=10`), use the repo's `soak-darwin` kind: push the branch, then run `bmesh routine queue soak-darwin --go` and `bmesh routine wait <job id>`. Only a darwin box takes it; each queue is a fresh run.

### Testing

Runner tests drive shell fakes instead of tmux and claude, and a fake T3; no test reaches the network, the box's cache, the real Keychain dialog, or the box's own `claude` and `codex` (the test-only variables and fixtures are in the internals; leave them unset outside tests). Check the real Keychain dialog by hand on a Mac.

**`scripts/seed.sh [seeder args]`** starts a throwaway `bmesh serve` in the background (embedded NATS and HTTP on free loopback ports; routines and ntfy off) over a seeded mesh that holds everything the map draws: every node state, a done lead on the shelf, a round with its engineers, routines with done instances, a queued job, leases, a digest, quota lines and a landed spec with operator steps open (a band row). It prints the map's URL, its check command and a CLI config for poking the mesh, then returns; serve keeps running.

- State lives under `~/.cache/bmesh-scratch/seed/<checkout directory name>` (`BMESH_SEED_DIR` overrides it, `BMESH_SEED_HTTP` the listen address), wiped each run, so two checkouts seed side by side. `seed.pid` there holds serve's pid; a run first stops its checkout's previous serve.
- `scripts/seed.sh stop` stops it by that pidfile. Never `pkill -f` its command line: it matches the caller's own shell.
- The seeded serve reads the page from the checkout's `internal/web/` on every request, so edits to `index.html`, `map.js` or `map.css` show on reload; a Go change needs a re-run. `BMESH_WEB_DIR=<dir>` does the same for any `bmesh serve` (logged as `bmesh serve: map page from <dir>`); unset, the page is the one embedded at build.

**`node scripts/check.mjs <url> <width> <height> [png]`** (Node 22 or later) is the map's browser check:

1. It loads `url` (adding `?check=1`) in headless Chromium at that viewport, over the DevTools protocol.
2. It waits for the self-check title to show a loaded tree on a live connection (`trees=` at least 1, `conn=` ending `live`).
3. It prints the title and, with `png` given, writes a viewport screenshot there.

It exits 0; 1 when no such title showed within 20 s (the last title is printed anyway) or the title's `bandHeld=` is non-zero (stderr: `check.mjs: a held node is in the needs-you band`); 2 on a usage or browser error. A hash picks the view (`/#/lead/<path>`, `/#/routines`). Chromium is `$CHROME`, else the newest headless shell or Chromium in the Playwright cache (`$PLAYWRIGHT_BROWSERS_PATH`, `~/.cache/ms-playwright`, `~/Library/Caches/ms-playwright`); stderr names it. `chromium --dump-dom` is no substitute: the page's `/events` stream never ends, so it never returns.

To check a UI change: run `scripts/seed.sh`, then the printed `node scripts/check.mjs ... 390 844 shot.png` and the same at `1280 800`, then `scripts/seed.sh stop`.

Internals: [docs/internals.md](docs/internals.md#testing)

## License

MIT.

<br>

<p align="center"><img alt="" src="docs/assets/logo.svg" width="56"></p>
