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
  <a href="docs/manual.md#several-boxes"><img alt="NATS JetStream, built in" src="https://img.shields.io/badge/NATS-JetStream%2C_built_in-a371f7?style=flat-square&logo=natsdotio&logoColor=white&labelColor=161b22"></a>
  <a href="#quickstart-single-box"><img alt="Linux, macOS and Windows" src="https://img.shields.io/badge/runs_on-Linux_%C2%B7_macOS_%C2%B7_Windows-8b949e?style=flat-square&labelColor=161b22"></a>
  <a href="LICENSE"><img alt="MIT license" src="https://img.shields.io/github/license/rickdiculousli/buildermesh?style=flat-square&color=3fb950&labelColor=161b22"></a>
</p>

<p align="center">
  <a href="#one-mesh-many-boxes"><b>Many boxes</b></a> &nbsp;·&nbsp;
  <a href="#proof-before-main"><b>Proof before main</b></a> &nbsp;·&nbsp;
  <a href="#how-a-change-reaches-main"><b>How it works</b></a> &nbsp;·&nbsp;
  <a href="#quickstart-single-box"><b>Quickstart</b></a> &nbsp;·&nbsp;
  <a href="#all-commands-and-flags"><b>Commands</b></a> &nbsp;·&nbsp;
  <a href="docs/manual.md"><b>Manual</b></a>
</p>

<br>

**buildermesh** (`bmesh`) is one Go binary that runs Claude Code sessions on your own machines (boxes) and turns ideas into changes on `main`. You decide what gets built.

Agents write half-page specs; you accept or reject them in plain words. A *lead* session builds an accepted spec in its own git worktree, with *executor* sessions on branches under it. *Routines* run the repo's checks and record each result against a commit's exact files, and `bmesh land` refuses to land a tree those checks haven't passed.

## One mesh, many boxes

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/mesh-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/mesh-light.svg">
    <img alt="Three boxes share one NATS hub, which runs on hub-box: every box builds at once; a coordinator holds one lead before it lands until another has; a check that needs macOS goes to the laptop; asks and finishes reach your phone; after a change lands, bmesh sync levels every box, the sleeping laptop when it wakes" src="docs/assets/mesh-light.svg" width="100%">
  </picture>
</p>

- **One hub.** `bmesh hub` runs NATS, a message server, inside the binary on an always-on box, over a tailnet such as Tailscale. Boxes share one token; nothing else is installed. A single box needs no hub: `bmesh serve` embeds one.
- **A daemon on every box.** `bmesh serve` wakes quiet leads, runs routines, sends phone notifications and draws the map. A box set to update itself installs each bmesh release; on others, each session opens with a line to run `bmesh update`.
- **A coordinator.** The session that started the builds follows its leads' events through the hub, and on your word holds a lead before it lands (`bmesh hold`) until another has landed, then releases it (`bmesh unhold`).
- **Shared checks.** A check that runs apart from the lead, like the review, is queued once for every box that runs it, so a free box takes it. A check that needs macOS runs only on a Mac.
- **Every box levels.** `bmesh sync` levels this box's checkout with origin and sends a `level` job to every other box that has the repo; each fast-forwards its own unless it's dirty or diverged, and a secure box holds a plugin change for your approval. A laptop that wakes takes the jobs it missed, in order; a job waits on the hub for a day.
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

## Quickstart (single box)

You need Go 1.26 or later, git, Claude Code (`claude` on your `PATH`, signed in) and a repo with a `main` branch.

```sh
go install github.com/rickdiculousli/buildermesh/cmd/bmesh@latest
claude plugin marketplace add rickdiculousli/buildermesh
claude plugin install buildermesh@buildermesh
cd ~/repos/your-repo
bmesh init      # press Enter at every prompt
bmesh serve     # keep it running
```

Add the repo's verify commands to the row `bmesh init` wrote in `repos.toml` (it prints the path):

```toml
[repos.your-repo]
# the keys init wrote stay as they are
verify = ["go vet ./...", "go test ./..."]
```

```sh
bmesh scaffold your-repo    # commit and push what it writes
bmesh ideate your-repo      # your first proposal round
```

## Quickstart (mesh)

One always-on box runs the hub; the other boxes reach it over a tailnet such as Tailscale. Install `bmesh` and the plugin on every box as above, then:

1. On the hub box:

   ```sh
   bmesh init --role hub --service    # Enter at every prompt; enable the two services it prints
   ```

2. Copy the hub's token file (`nats.token` in its config dir) into each other box's config dir.
3. On each other box:

   ```sh
   bmesh init --hub nats://<hub tailnet ip>:4222 --token-file <config dir>/nats.token --service    # enable the service it prints
   # a laptop that sleeps: bmesh init --role leaf --service, with nats://<hub tailnet ip>:7422 at the prompt; enable the two services it prints
   ```

4. Optional, so boxes can act on each other: join them to a roster. On the box you type at, run `bmesh init --hosts --admin`. For each other box, run `bmesh hosts invite <its host name>` on the admin box, then `bmesh init --hosts` on that box and paste the code. Restart `bmesh serve` on each box after its `init --hosts`.

## The map

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/map-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/map-light.png">
    <img alt="The bmesh map: leads with their executors, a proposal round, a held lead, the routines column and the needs-you band" src="docs/assets/map-light.png" width="100%">
  </picture>
</p>

`bmesh serve` draws the mesh live at `http://127.0.0.1:3774/`: leads with their executors, rounds with their engineers, each check with its queue, and at the top what needs you: a session that is blocked, asking or silent.

## All commands and flags

`--config FILE`, `--node PATH` and `--json` go before or after any command ([Synopsis](docs/manual.md#synopsis)). Each command links to its section of the [manual](docs/manual.md).

**Setup and updates**

| Command | Flags |
|---|---|
| [`bmesh init`](docs/manual.md#bmesh-init) | `[--service] [--force] [--role hub\|leaf\|client] [--hub URL] [--token-file F] [--runner K] [--no-repo]` |
| [`bmesh init --hosts`](docs/manual.md#bmesh-init) | `[--admin]` |
| [`bmesh serve`](docs/manual.md#bmesh-serve) | `[--listen ADDR] [--restart [--detach]]` |
| [`bmesh hub`](docs/manual.md#bmesh-hub) |  |
| [`bmesh config check`](docs/manual.md#bmesh-config-check) |  |
| [`bmesh t3 login`](docs/manual.md#bmesh-t3-login) | `[--server URL] [--callback-port 52299] [--timeout D] · --status` |
| [`bmesh version`](docs/manual.md#bmesh-version) |  |
| [`bmesh help`](docs/manual.md#synopsis) |  |
| [`bmesh update`](docs/manual.md#bmesh-update) | `[--repo R] [--latest] [--answer KEY=VALUE]... [--defaults] [--apply [--yes]]` |
| [`bmesh update --plan`](docs/manual.md#bmesh-update) | `[--json]` |
| [`bmesh update --walk`](docs/manual.md#bmesh-update) | `--from REV [--to REV] [--dir D] [--before-swap]` |
| [`bmesh update --notice`](docs/manual.md#bmesh-update) |  |

**Proposals and builds**

| Command | Flags |
|---|---|
| [`bmesh ideate <repo>`](docs/manual.md#bmesh-ideate) | `[--idea] [--model M] [--effort E] [--context <text\|->] [--force]` |
| [`bmesh choose-ideas <repo> "<reply>"`](docs/manual.md#bmesh-choose-ideas) |  |
| [`bmesh spec list`](docs/manual.md#bmesh-spec-list) | `[--repo R] [--status S] [--json]` |
| [`bmesh spec next-id`](docs/manual.md#bmesh-spec-next-id) | `[--repo R]` |
| [`bmesh spec set`](docs/manual.md#bmesh-spec-set) | `--id NNN --status S [--repo R] [--from A,B] [--field K=V]...` |
| [`bmesh spec template`](docs/manual.md#bmesh-spec-template) | `[--repo R]` |
| [`bmesh spec steps [<NNN>]`](docs/manual.md#bmesh-spec-steps) | `[--repo R] [--tick N,N [--note W] \| --check [--wait D]]` |
| [`bmesh spec verified`](docs/manual.md#bmesh-spec-verified) | `[--spec NNN] [--commit C] [--write]` |
| [`bmesh build <repo> <NNN>`](docs/manual.md#bmesh-build) | `[--model M] [--effort E] [--path P] [--family F] [--hold land\|now] [--force]` |
| [`bmesh build --accepted <repo>`](docs/manual.md#bmesh-build) | `[--model M] [--effort E] [--family F] [--hold land\|now] [--force]` |
| [`bmesh lead start`](docs/manual.md#bmesh-lead-start) | `--repo R --spec NNN --brief FILE [--model M] [--effort E] [--title T] [--path P]` |
| [`bmesh hold <path>\|--family F`](docs/manual.md#bmesh-hold-unhold-held) | `[--gate land\|now] --text T` |
| [`bmesh unhold <path>\|--family F`](docs/manual.md#bmesh-hold-unhold-held) | `[--text T]` |
| [`bmesh held`](docs/manual.md#bmesh-hold-unhold-held) |  |
| [`bmesh feedback "<text>"`](docs/manual.md#bmesh-feedback) | `[--repo R] [--where <file\|command>] [--fixed SHA]` |
| [`bmesh feedback list <family>\|--repo R`](docs/manual.md#bmesh-feedback) | `[--json]` |
| [`bmesh feedback drain <family>`](docs/manual.md#bmesh-feedback) | `[--through SEQ]` |
| [`bmesh feedback drain`](docs/manual.md#bmesh-feedback) | `--seq SEQ[,SEQ...]` |
| [`bmesh fix`](docs/manual.md#bmesh-fix) | `[--model M] [--effort E]` |
| [`bmesh release <repo>`](docs/manual.md#bmesh-release) | `[--minor\|--patch] [--dry-run]` |
| [`bmesh scaffold <repo>`](docs/manual.md#bmesh-scaffold) | `[--check]` |

**Nodes**

| Command | Flags |
|---|---|
| [`bmesh whoami`](docs/manual.md#bmesh-whoami) |  |
| [`bmesh started\|progress\|blocked\|ask`](docs/manual.md#bmesh-started-progress-blocked-ask-done) | `[--text T]` |
| [`bmesh done`](docs/manual.md#bmesh-started-progress-blocked-ask-done) | `--commit SHA [--text T]` |
| [`bmesh send <path> <text\|->`](docs/manual.md#bmesh-send) |  |
| [`bmesh inbox`](docs/manual.md#bmesh-inbox) | `[--peek] [--json]` |
| [`bmesh thread-status [<path>]`](docs/manual.md#bmesh-thread-status) | `[--json]` |
| [`bmesh mesh-status`](docs/manual.md#bmesh-mesh-status) | `[--all] [--json]` |
| [`bmesh watch`](docs/manual.md#bmesh-watch) | `--nodes a,b [--since T] [--timeout D]` |
| [`bmesh executor new`](docs/manual.md#bmesh-executor-new) | `--name N [--base REF] [--model M] [--json]` |
| [`bmesh executor list`](docs/manual.md#bmesh-executor-list) | `[--json]` |
| [`bmesh executor remove <name>`](docs/manual.md#bmesh-executor-remove) | `[--force]` |
| [`bmesh node forget <path>`](docs/manual.md#bmesh-node-forget) | `[--worktree [--wait 30s]]` |
| [`bmesh node stop <path>`](docs/manual.md#bmesh-node-stop) | `[--text T] [--wait 30s]` |

**Checks and landing**

| Command | Flags |
|---|---|
| [`bmesh routine list`](docs/manual.md#bmesh-routine-list) | `[--json]` |
| [`bmesh routine status [<kind>]`](docs/manual.md#bmesh-routine-status) | `[--json]` |
| [`bmesh routine queue <kind>`](docs/manual.md#bmesh-routine-queue) | `[--commit C] [--model M] [--effort E] [--go] [--rerun] [--again] [--text T] [-- args...]` |
| [`bmesh routine go <kind>`](docs/manual.md#bmesh-routine-go) |  |
| [`bmesh routine wait <job id>`](docs/manual.md#bmesh-routine-wait) | `[--timeout D]` |
| [`bmesh routine run <kind>`](docs/manual.md#bmesh-routine-run) | `[--commit HEAD] [--model M] [--effort E] [--here] [--rerun] [--text T] [-- args...]` |
| [`bmesh result find <kind>`](docs/manual.md#bmesh-result-find) | `[--commit C] [--model M] [--json] [-- args...]` |
| [`bmesh result record <kind>`](docs/manual.md#bmesh-result-record) | `--ok\|--fail [--commit C] [--model M [--base B]] [--output FILE\|-] [-- args...]` |
| [`bmesh lock-lease acquire <slot>`](docs/manual.md#bmesh-lock-lease) | `[--ttl D]` |
| [`bmesh lock-lease renew <key>`](docs/manual.md#bmesh-lock-lease) | `[--ttl D]` |
| [`bmesh lock-lease release <key>`](docs/manual.md#bmesh-lock-lease) |  |
| [`bmesh lock-lease run <slot>`](docs/manual.md#bmesh-lock-lease) | `[--ttl D] -- <cmd...>` |
| [`bmesh land`](docs/manual.md#bmesh-land) | `[--spec NNN] [--ttl D] [--wait D]` |

**Boxes**

| Command | Flags |
|---|---|
| [`bmesh hosts`](docs/manual.md#bmesh-hosts) | `[--json]` |
| [`bmesh hosts --here`](docs/manual.md#bmesh-hosts---here) |  |
| [`bmesh hosts --notes`](docs/manual.md#bmesh-hosts---notes) | `[--json]` |
| [`bmesh hosts invite <name>`](docs/manual.md#bmesh-hosts-invite) | `[--secure] [--accept a,b] [--replace]` |
| [`bmesh hosts set <name>`](docs/manual.md#bmesh-hosts-set) | `[--secure=true\|false] [--accept a,b]` |
| [`bmesh hosts revoke <name>`](docs/manual.md#bmesh-hosts-revoke) |  |
| [`bmesh hosts ping [<host>]`](docs/manual.md#bmesh-hosts-ping) | `[--wait D]` |
| [`bmesh hosts replies`](docs/manual.md#bmesh-hosts-replies) | `[--json]` |
| [`bmesh hosts held`](docs/manual.md#bmesh-hosts-held) | `[--json]` |
| [`bmesh hosts approve <id>`](docs/manual.md#bmesh-hosts-approve) |  |
| [`bmesh hosts deny <id>`](docs/manual.md#bmesh-hosts-deny) |  |
| [`bmesh sync`](docs/manual.md#bmesh-sync) | `[--repo R \| --all] [--wait 30s]` |
| [`bmesh prune`](docs/manual.md#bmesh-prune) | `[--repo R \| --all] [--apply] [--wait 30s]` |
| [`bmesh repos`](docs/manual.md#bmesh-repos) | `[--json]` |
| [`bmesh repos show <name>`](docs/manual.md#bmesh-repos-show) | `[--json]` |
| [`bmesh repos publish`](docs/manual.md#bmesh-repos-publish) |  |
| [`bmesh repos propose`](docs/manual.md#bmesh-repos-propose) | `[--wait 30s]` |
| [`bmesh repos remove <name>`](docs/manual.md#bmesh-repos-remove) | `[--checkouts [--ignored-ok]] [--apply] [--wait 1m]` |

## What's inside

- **`bmesh`**: the CLI, daemon, map server and NATS hub in one binary.
- **Runners**: `t3` (a T3 Code thread), `claude` (headless `claude -p` turns) or `tmux` runs each session.
- **The plugin**: twelve Claude Code skills, seven for your chat and five that the sessions bmesh starts follow.
- **Specs**: `docs/proposals/NNN-slug.md` in each repo, with status, builder, merged commit and `verified` line in the frontmatter.

The [manual](docs/manual.md) covers every command, flag, config key, output and refusal, and the concepts behind them. [Internals](docs/internals.md) covers NATS subjects, buckets, record shapes and signing.

## Build and test

```sh
go build ./cmd/bmesh
TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test -count=1 ./...
```

Tests refuse `/tmp`, so `TMPDIR` points elsewhere. The seeded mesh (`scripts/seed.sh`), the map's browser check (`node scripts/check.mjs`) and the UI-change check are in the manual's [Testing](docs/manual.md#testing) section.

## License

MIT.

<br>

<p align="center"><img alt="" src="docs/assets/logo.svg" width="56"></p>
