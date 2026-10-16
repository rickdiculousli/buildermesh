# buildermesh round two: one stack, standalone (2026-10-15)

Status: built 2026-10-15. Its plans were the numbered specs in `~/repos/buildermesh/docs/proposals/`, each built by a `bmesh build` lead and recorded there (`done`, `merged`, `verified`): 001 registry and init (`5f74fec`), 002 rounds and review (`19213a3`), 003 build and align (`ba3d6aa`), 004 the method plugin (`fb40ffb`), 005 this repo's reorg (buildermesh `docs/proposals/005-command-center-reorg.md`). No plan went into `docs/superpowers/plans/`. computer-use was aligned on the Mac the same day (`5ef825a`, its only checkout). Still open: the Open list below.

Round one (`2026-10-14-buildermesh.md`) built the mesh: leads, executors, routines, results, the daemon and the map. The map of what runs where (2026-10-15) showed the front half of the lifecycle still outside it: proposal rounds, Rick's review, dispatch, the brief templates and the repo registry live in command-center as Python and skills; pstack is installed beside the mesh but never invoked by it; superpowers' hook and skills overlap both. Round two moves the whole lifecycle into buildermesh, makes buildermesh usable by anyone with nothing of Rick's, and trims the method stacks to what is used.

## Why

- Two layers own one lifecycle. `bmesh` already reads the spec ledger (`done`, `verified`) but command-center's `propose`, `dispatch`, `brief.py`, `proposals.py`, `projects.py` and `templates/` own the round, the review and the dispatch. Every change to the lifecycle touches both.
- Results don't come to Rick. A round ends as files on `main`; nothing pushes the pitch, and the reply only works inside the proposing thread.
- pstack is dead weight on the mesh. Its SessionStart hook loads an adapter into every thread; no lead or executor skill names a pstack skill.
- superpowers conflicts. Its "invoke a skill first" hook, brainstorming (vs grilling), subagent-driven-development (vs lead/executor), TDD and verification-before-completion (vs pstack) all fight the mesh. Rick uses writing-plans and grilling (now `method:writing-plans` and `method:grilling`, decision 8).
- buildermesh assumes Rick's fleet. T3 or tmux as the only runners, a NATS hub someone set up, command-center's registry. A stranger can't start it.

## Decisions Rick made 2026-10-15 (don't relitigate)

Grilled in command-center session `da6488fd`, Q1 to Q25.

1. **Ownership.** The build lifecycle moves wholesale into buildermesh: proposal rounds, Rick's review, dispatch, brief rendering, the spec ledger and align. command-center keeps only fleet ops: `repo_sync`, `prune`, `pr_watch`, `reset_reminder`, the fleet docs, and the skills `prune`, `repo-sync`, `tooling-editor`, `pr-comment`. `propose`, `dispatch`, `align-repo`, `brief.py`, `proposals.py`, `projects.py`, `align_repo.py` and `templates/` are deleted once bmesh has their function. (Q1, Q13) **Reversed in part the same day:** deleting the operator skills left the lifecycle reachable only from a shell, and on the phone the chat is the interface. The entry points come back as skills in the buildermesh plugin (`buildermesh:propose|review|build|align|status`, spec 006 there), not in command-center, so a stranger's install has them too.
2. **One registry.** Per-repo facts move into `~/.config/bmesh/repos.toml`, identical on every box, `[repos.<name>]` with `path`, `mode`, `remote`, `host`, `ux_host`, `verify` (a list of shell commands), `surfaces` (a list), `pin`, `routines`. Every repo under `~/repos/` gets a row, not only ship repos; only `mode = "ship"` gets the lifecycle. `config.toml` stays per box. Both come from `fleet/bmesh/` in command-center. `projects.md` keeps prose, no table. `bmesh repos [--json]` prints the table; `repo_sync` and `prune` read it. A `path` is enough; everything else has a default. (Q2, Q9, Q18, Q19)
3. **Results through the mesh.** A round is a node `<repo>-round-<N>` with children `eng` and `ux`, each a thread. The daemon pushes one ntfy message with the merged ranked pitch when both are `done` (or one `blocked`), linking the `eng` thread. Rick replies there, in plain words; the thread (or any session) runs `bmesh spec review <repo> "<reply>"`, which works in a fresh bmesh worktree, applies verdicts, lands on `main`, pushes and reports unmatched IDs or phrases rather than guessing. The map's proposals panel is a later spec. (Q3, Q16, Q17)
4. **Proposal threads live in the buildermesh T3 project.** Their worktree of the target repo is a bmesh worktree under `~/.bmesh/worktrees/<repo>/<repo>-round-<N>-eng|ux`, like an executor's. Leads stay in a T3 worktree of the target repo (diff view, preview, prune). (Q8)
5. **Commands.** `bmesh repos`, `bmesh propose <repo>`, `bmesh spec review <repo> "<reply>"`, `bmesh build <repo> <NNN>` (today's `lead start --spec` with the brief rendered inside), `bmesh build --accepted <repo>` (every accepted spec, three at a time, dependents after), `bmesh align <repo>`. `lead start` stays as the low-level form. (Q10)
6. **Briefs embed.** The engineer, ux-engineer, build and half-pager templates embed in the binary; a repo overrides any with `.bmesh/briefs/<name>.md`. The standard closing and the done rule render from repos.toml. (Q11)
7. **pstack is optional, and so is method.** The lead and executor skills carry a written default for every step and say "if the pstack plugin is installed, use `pstack:<skill>` here" at fixed moments: poteto-mode as the working style; `architect` and `attack-the-premise` when the lead decides the shape; `tdd` as the executor's default; `figure-it-out` before any `bmesh ask`; `blast-radius` before landing; `exhaust-the-design-space` for the round's engineers; `method:writing-plans` when a spec is big enough to plan. No runtime detection, no declared dependency; the README lists both as recommended. (Q4, Q12, Rick's rule: bmesh must be easy for anybody to start)
8. **superpowers retires.** Uninstalled at user scope on both boxes; the `~/repos/superpowers` clone is deleted. A `method` plugin in `rickdiculousli-tools/plugins/method` carries `grill-me` and `grilling` from `mattpocock/skills` and `writing-plans` from `obra/superpowers`, pinned in one `UPSTREAM` file, with an `update.sh` that fetches both at their pins or `--to <ref>`, copies the skill directories, shows the diff per skill and records new pins. executing-plans is not kept: a plan is run by a `bmesh build` lead. The local `~/.claude/skills/grill-me` and `grilling` are removed once the plugin is on both boxes. (Q5, Q14)
9. **Standalone install.** `bmesh` ships complete: nothing in it, its briefs or its README assumes command-center, Rick's fleet, pstack or method beyond what config names. `[mesh] nats = ""` (embedded server, already built) is the single-box default; a URL is the multi-box form. A `claude` runner (headless Claude Code session, resumable by session id) is the default runner when `claude` is on PATH; `t3` and `tmux` stay as adapters; `codex` can follow in the same shape. `bmesh init` is guided: it asks single box or multi-box, writes `config.toml` and `repos.toml`, detects runners on PATH, registers the current repo, writes the systemd unit or launchd plist with `--service`, and sets a setup-completed flag; every `bmesh` command checks the flag and runs init when it is missing. First run is `go install github.com/rickdiculousli/buildermesh/cmd/bmesh@latest`, `bmesh init`, `bmesh serve`, `bmesh propose`. Skills install with `claude plugin marketplace add rickdiculousli/buildermesh`. (Q20 to Q23)
10. **Windows and binaries later.** Round two writes every new piece cross-platform (no `sh -c`, no tmux assumption, `os.UserConfigDir` paths) and adds a Windows build to CI; tested Windows support (the quota panel's `sh -c`, worktree paths) and tagged release binaries are their own spec after round two. (Q24, Q25)
11. **Dogfood.** Round two's plans are numbered specs in buildermesh's `docs/proposals/`, one per plan, each built by a `bmesh build` lead; each lead lands its cross-repo pieces (command-center, rickdiculousli-tools) directly on `main` there as round one's threads did. The command-center reorg is its own spec and runs alone (reorg barrier). The five ship repos without routine kinds (gametrainer, computer-use, command-center, rickdiculousli-tools, buildermesh) get theirs in the final `bmesh align` pass. (Q6, Q7, Q15)

## Vocabulary (additions to round one)

- **round**: node `<repo>-round-<N>`, children `eng` and `ux`; `N` counts rounds per repo from 1. Done when both children are done; its event carries the merged ranked pitch.
- **registry**: `repos.toml`. A **row** is one `[repos.<name>]` table.
- **brief**: a rendered template; **template**: the embedded default or the repo's `.bmesh/briefs/<name>.md`.
- **method**: the `method` plugin (`method:grilling`, `method:writing-plans`). Optional, like pstack.
- **setup flag**: `~/.config/bmesh/.setup-complete` (or the OS config dir equivalent); its absence means `bmesh init` runs first.

## The product

### Registry

`repos.toml` is read by every command through one loader; `config.toml`'s `[repos]` table (round one) is replaced by it and refused when present. Defaults: `mode = "ship"`, `host = ""` (this box), `ux_host = host`, `verify = []`, `surfaces = []`, `pin = ""`, `routines = ["verify", "review"]`. `bmesh repos` prints a table with the same columns `projects.md` had; `--json` prints the rows. `repo_sync.py` and `prune_worktrees.py` call it instead of `projects.py`.

### Rounds

`bmesh propose <repo> [--model m]`: refuses a repo with five or more `proposed` specs (as today), allocates `N`, makes the two bmesh worktrees from the repo's `main`, renders the two briefs with the next free spec IDs (ux numbers five past eng), registers the round node and its children, and starts both threads through the runner on their rows' hosts (a host other than this box goes through that box's runner the way `lead start` does; k16 can't reach the Mac, so a Mac host from k16 is refused with a message). Each engineer thread reports `started`, `progress` and `done` as a node; `done` carries the ranked list as text. The daemon's push list gains the round's completion.

`bmesh spec review <repo> "<reply>"` parses `(id, verdict, note)` triples with today's grammar (yes, no, later, adjust words, "on fable|opus|sonnet"), refuses to move a spec whose status isn't `proposed`, `later` or `adjusting`, edits an adjusting spec's body, commits `Review: <one line>` on a fresh bmesh worktree, fast-forwards `main` (`fetch . <branch>:main` when the main checkout is elsewhere), pushes, removes the worktree, and prints one line per spec plus the unmatched items. `bmesh spec list|next-id|set|template` keep `proposals.py`'s functions.

### Build

`bmesh build <repo> <NNN>` renders the build brief (spec body, done rule from the row, node path, verify commands, standard closing) and calls `lead start`. It records `building` with thread, branch and host on `main` first, as `dispatch` does today. `--accepted` orders accepted specs by `after`, starts up to three, and prints what waits. `bmesh align <repo>` is `align_repo.py` in Go: the three marked sections of AGENTS.md from the row and the embedded snippet, `docs/proposals/README.md`, `assets/.gitkeep`, and the row's `routines` kinds seeded from the embedded examples (`verify` as an inline deterministic kind from `verify`), leaving existing kinds alone; `--check` diffs.

### Skills

`buildermesh:lead` and `buildermesh:executor` gain the "if installed" pstack and method moments of decision 7, each with its written default. A new `buildermesh:propose-engineer` and `buildermesh:propose-ux` carry the round's method (today's engineer and ux-engineer briefs' "how to work"), so the briefs shrink to the repo's facts.

### Install

`bmesh init` (decision 9). The `claude` runner starts `claude -p --output-format json` with the brief in a worktree it makes under `[runner] worktrees`, records the session id as the thread id, resumes with `--resume` for `send`, and reports state from the process. Documentation: the README's first section becomes the quickstart; `docs/agents-snippet.md` and the skills mention nothing of Rick's.

## What goes, what stays

| Goes | Stays |
|---|---|
| command-center `propose`, `dispatch`, `align-repo` skills; `brief.py`, `proposals.py`, `projects.py`, `align_repo.py`, `templates/`, their tests; the `projects.md` table | command-center `prune`, `repo-sync`, `tooling-editor`, `pr-comment`; `repo_sync.py`, `prune_worktrees.py`, `pr_watch.py`, `reset_reminder.py`, `t3_dispatch.py`; `fleet/` |
| superpowers plugin (both boxes); `~/repos/superpowers`; `~/.claude/skills/grill-me`, `grilling` | pstack-claude (optional); quota, yt-transcript |
| `config.toml [repos]` | `config.toml` for box facts; `fleet/bmesh/<box>.toml` plus new `fleet/bmesh/repos.toml` |

## Build order

Numbered specs in `~/repos/buildermesh/docs/proposals/`, one lead each:

1. **Registry and init**: `repos.toml`, loader, `bmesh repos`, `bmesh init` with the setup flag, the `claude` runner, Windows CI build. Nothing else depends on command-center yet.
2. **Rounds and review**: round nodes, `bmesh propose`, the engineer skills, the daemon's round push, `bmesh spec review|list|next-id|set|template`, embedded templates and overrides.
3. **Build and align**: `bmesh build`, `--accepted`, `bmesh align`, the lead and executor skills' optional-method moments.
4. **Method plugin**: `rickdiculousli-tools/plugins/method` with `update.sh`; install on both boxes; remove the local skills; uninstall superpowers.
5. **command-center reorg** (barrier, runs alone): delete what goes, point CLAUDE.md, `repo_sync` and `prune` at `bmesh repos`, `fleet/bmesh/repos.toml`, `projects.md` to prose, final `bmesh align` on every ship repo, this spec's status.

Specs 1 to 3 are sequential (each builds on the last); 4 can run beside 2 or 3; 5 runs alone after all.

## Open

- The `claude` runner's `send` while a turn runs: `claude -p --resume` has no steer; a message queues until the turn ends.
- Where the round's two threads' rankings merge: the daemon concatenates by rank, eng first, in this round; a smarter merge waits for a map panel.
- `codex` runner: same shape, later.
