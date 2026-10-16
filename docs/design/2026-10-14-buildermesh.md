# buildermesh: one workflow for Rick's repos, from first principles (2026-10-14)

Status: design settled with Rick in a grilling session on 2026-10-14; vocabulary fixed and contradictions resolved while planning the same day (see "Resolved while planning"). Plans: `docs/superpowers/plans/2026-10-14-buildermesh-0{1..5}-*.md`. A thread building any part of this hasn't seen that session; this file is its context. It supersedes `2026-10-14-one-workflow.md` and closes the relay spec.

## Why

Three orchestration layers overlapped: pstack's program playbooks (written for Cursor's cloud VMs, PRs and CI), command-center's own wiring (`t3_dispatch`, the NATS pipe, `lanes`, the relay spec, the leftover trigger) and T3 Code's orchestration V2. Rick asked for one cohesive system designed from first principles for how he works, with the parts he built ripped out where they don't earn their place.

What the evidence showed: the proposal loop is the engine (31 `[build]` threads on k16 in two days; eggnogg-rework closed 22 specs that way and went dormant on long-horizon); the many-lane shape happened once; Rick's seat is the phone and short replies; NATS worked and he trusts it; T3's point-to-point tailnet UI is the thing he enjoys; quota binds.

## Decisions Rick made (don't relitigate)

- **T3 Code is the console, not the engine.** ~~It stays on what is installed (0.0.45) with the current adapter. V2 is optional convenience later.~~ **Superseded 2026-10-16 by buildermesh 029** (merged `f590bf9`): 0.1.0 removed the V1 API and the 2.0 phone app refuses a V1 server, so every box moved to T3 0.1.0 (Orchestrator V2) and the `t3` runner speaks V2 through T3's MCP tools (`docs/superpowers/plans/2026-10-15-t3-v2-migration.md`). No vendor relay, ever: not T3 Connect, not Anthropic's Remote Control. Point to point over Tailscale.
- **Only leads are T3 threads.** Executors and routines never appear in the console; ten to thirty threads in his face is the failure mode.
- **NATS is the mesh, all-to-all.** Any node can message any node: lead to executor, executor to peer, peer to routine. That is why NATS is there.
- **Codex is dropped.** Claude is the only vendor. Diversity means a different Claude family, never the same one twice.
- **Long-horizon folds into specs.** A goal is a long spec in the same place. No separate shape.
- **A lead decides its own delegation.** It may be its own principal executor; it delegates routine work to a lesser model and drives the gaps between executions; fan-out means delegation in whatever shape, one layer or several, not necessarily parallel.
- **`done` is an explicit publish.** A Stop hook fires when an agent stops to ask the operator, so it cannot mean done.
- **Portable.** buildermesh is for other people to reuse: own public repo, one binary, nothing Rick-specific inside.
- **Earlier decisions still hold:** no CI; numbers not rulings; the spec file in git is the record; the shared definition of done; nothing needs Rick for upkeep; no scheduled trigger on the Mac.
- **Dropped from the earlier draft, by Rick:** auto-started proposal rounds, the relay spec's quota zones, auto-dispatch of accepted specs. He starts rounds and builds.

## Vocabulary

One metaphor: a mesh of nodes exchanging messages, drawn as a map. Work roles are plain words. Every other word from earlier drafts (family, cockpit, situation room, ledger, evidence, judge, nudge, lane, needs-rick, spine, worker) is retired; the plans and the code use only these. "Family" survives only as *model family* (Opus, Fable, Sonnet), never for a lead's tree.

| Word | Means | Replaces |
|---|---|---|
| **mesh** | everything connected to one NATS hub: nodes, their messages, the daemon | spine, pipe |
| **node** | one running agent with a mesh id, in a role | agent, thread (in mesh context) |
| **lead** | node that owns one spec; the only role that is a T3 thread and lands on `main` | |
| **executor** | node a lead spawns as its subagent, on its own branch | worker |
| **routine** | node from a shared pool, defined by a kind, owned by nobody | |
| **instance** | one running routine of a kind: a command run or a resumable model session | worker |
| **operator** | the human the mesh works for; the only recipient of `ask` | Rick, in portable text |
| **path** | a node's id, dotted: `<lead>`, `<lead>.<executor>`, `routine.<kind>.<n>`; subjects follow it | id |
| **tree** | a lead and every node under its path | family |
| **inbox** | messages addressed to a path, read at checkpoints | mail, queue |
| **job** | a message to a routine kind's inbox asking for one run | request, task |
| **result** | what a routine run produced, keyed by kind and git tree hash | evidence |
| **kind** | a committed routine definition: `.bmesh/routines/<kind>.toml` | kind file |
| **mode** | where a routine runs: `inline`, `fresh`, `pooled` | exclusive (now `slots`) |
| **slot** / **lease** | a named exclusive resource / holding one for a bounded time | lock |
| **event** | a node's own announcement: `started`, `progress`, `blocked`, `ask`, `done` | |
| **state** | what the daemon derives per node: `working`, `idle`, `blocked`, `asking`, `done`, `silent` | |
| **wake** | the daemon's message to an idle lead with an unread inbox | nudge |
| **grace** | how long a working node may be quiet before it is `silent` | grace period |
| **digest** | the periodic ntfy summary of the mesh | |
| **push** | an immediate ntfy message for a moment that needs the operator: a lead's `ask`, `blocked` or `done` | alert |
| **runner** | what hosts a lead's session: T3 (`t3`) or `claude` in tmux (`tmux`) | runner adapter |
| **console** | where the operator talks to leads: T3's UI | cockpit |
| **map** | the web page the daemon serves: the mesh drawn as a tree, live | dashboard, situation room |
| **brief** | the prompt a node starts with | |
| **spec** | the record: `docs/proposals/NNN-slug.md` with frontmatter status and a `verified` line | ledger |

## The product

`buildermesh`: repo `~/repos/buildermesh` (public, `github.com/rickdiculousli/buildermesh`), one Go binary `bmesh`, subjects under `bm.`.

### Layers

| Layer | Question | Lives in | Changes with the runner |
|---|---|---|---|
| Craft | How does one node do one piece of work well? | pstack's leaf skills; buildermesh's lead and executor skills | No |
| Specs | What is proposed, accepted, building, done, and what proves it? | Each repo's `docs/proposals/`, AGENTS.md definition of done, `verified` lines | No |
| Mesh | Who talks to whom, who runs what, who is alive | `bmesh` daemon, CLI and map over NATS | No |
| Console | Where the operator looks and what he types | T3 Code; command-center's registry, fleet and his config | Yes, only here |

### Nodes

- **Lead.** One per spec, a T3 thread in its own worktree. Owns the spec, chooses its delegation shape, merges its executors' branches, runs verify, and is the only node that lands on `main`, under the definition of done. Its skill is the definition of done plus the mesh protocol; it names no pstack skill (the operator's brief adds those). pstack's program playbooks (orchestrate, autopilot, multi-phase-plan, babysit, shipping, opening-a-pr, swarm) are skipped in the port; they encode Cursor's economics.
- **Executor.** A lead's subagent through the Agent tool, in a worktree `bmesh` made under `~/.bmesh/worktrees/<repo>/<path>` on branch `<lead-branch>-<name>`, resumable by SendMessage. Reports its branch and head commit to the lead's inbox. Has a path and inbox like any node. Executors are peers of each other and of routines, not a layer under them. The harness's own completion notice to the lead is not a mesh message; the inbox carries the content.
- **Routine.** A shared pool, owned by nobody. A kind is `.bmesh/routines/<kind>.toml` in the repo declaring how it runs (`command`, or `model` or `models` with a `prompt`, optionally `allow` for commands the model may run), a `mode`, optional `slots` it needs a lease on, and `deterministic = true` when the same tree always gives the same result:
  - `inline`: the requesting node runs it in its own worktree (`bmesh routine run`), no service;
  - `fresh`: the daemon starts a new instance per job, no reuse;
  - `pooled`: one or more resumable instances per kind drain the kind's inbox in batches so the setup context is loaded once and each job adds a little; a job is sent with `--go` to drain now, or waits for the idle window; recycle when context passes a line, uses pass a cap, or idle passes the prompt-cache TTL (all data per kind).
  A deterministic kind's result is recorded under `<kind>.<tree>`; the daemon answers a repeat job for the same tree from the record. Deterministic jobs name a commit (uncommitted work cannot be addressed) and run in `~/.bmesh/checkouts/<repo>/<instance>`, a clean worktree per instance detached at that commit, never in a requester's worktree. Leases for declared slots live in a KV bucket and are the daemon's; a node holds one only through `bmesh lease`. Inline runs record results through the same CLI, so results are uniform.

### Messaging and liveness

- Every node gets a path at spawn, carried in its brief. Subjects follow paths: `bm.<path>.<event>` for events, `bm.inbox.<path>` for messages and jobs (a kind's inbox is `bm.inbox.routine.<kind>`). First tokens `inbox`, `routine` and `daemon` are reserved. One stream, `bm-mesh`, holds all of it; the map replays and follows it.
- Inboxes are read at checkpoints, never pushed into a running turn (the eggnogg run's 500K-token lesson). The daemon wakes a lead only when its runner says the thread is idle and the inbox has unread messages.
- Events are explicit. Nothing is inferred from reply text.
- Silence: a lead's liveness is its tree's activity on `bm.<lead>.>`; a quiet lead whose executors or routines are working is alive. Executors and routines are judged on their own path. A working node quiet past its grace is marked `silent`, woken once, then reported in the digest.
- `done` is checked by the CLI against the spec on `origin/main` (status `done`, `merged` set, and from slice 2 a `verified` line whose entries match recorded results), never against prose; `bmesh done` refuses otherwise. An executor's `done` needs a commit and touches no spec; an executor's `ask` goes to its lead's inbox, a lead's to the operator.

### The daemon

`bmesh serve`, Go, one per box, generalized from `tools/lanes` (its names, connection, inbox, state, lease, embedded test server, T3 client, sweeper and bridge survive; its lander, gate, judge, policy and quota loops do not). Embeds a NATS server when `[mesh] nats` is empty; an external hub is that one config line. Its whole job:

1. wake an idle lead with unread messages;
2. run routine instances off kind inboxes, hold leases, answer deterministic jobs from results;
3. apply the silence rule, tree-aware for leads;
4. post the digest to ntfy, and push a lead's `ask`, `blocked` and `done` as they happen;
5. serve the map.

No model in the loop. Whatever the daemon cannot decide goes to the lead; whatever the lead cannot decide goes to the operator as `ask`.

### Runners

One small interface: start a lead in a worktree with a brief, send text, report idle or running, interrupt. `t3` runs `t3_dispatch.py` as a subprocess (the Python adapter is not extended); `tmux` runs `claude -p`-less interactive sessions in named tmux windows and is the zero-dependency runner for people without T3.

### The CLI

`bmesh` is the only command a node uses: `whoami`, `send`, `inbox`, `state`, `status`, `watch`, the five events, `lead start`, `executor new | list | remove`, `routine list | status | queue | go | run | wait`, `lease acquire | renew | release | run`, `result record | find`, `spec verified`, `serve`. It replaces `lane`, `pipe.py` and the delegation skill's messaging rules.

### The map

Served by the daemon from the `bm-mesh` stream (history to replay, live events to animate) over server-sent events; one embedded page, plain JavaScript and SVG, no build step; bound to loopback, published on the tailnet by the operator (Rick uses Tailscale Serve like T3), so phone and desktop open the same URL point to point.

Home view is a layered tree, not a hairball: leads at the top, each tree as branches below its lead with few crossings, routines floating in a side column reachable from any node. Edges light when a message passes; nodes pulse while working and dim when silent. A lead's page: spec, branch, console link, inbox, tree timeline. Panels for kind inboxes and instances (uses, context, leases) and a strip for quota and the latest digest.

### Specs

Unchanged convention. A spec may be long (milestones inside it). Landing writes one `verified` line into the frontmatter, `;`-separated, one entry per check: `<kind> <ok|fail> t:<tree8>[ m:<model>]`, composed by `bmesh spec verified --write` from recorded results for the landed tree. `tools/proposals.py` keeps parsing it as a `key: value` line. No other store of proof.

### Verification policy (sized 2026-10-15)

Verify commands, **one** `review` job by a model of a different family than the builder, and one `surface` job per touched UI surface from `projects.md`. The same for every size: a second reviewer buys nothing the experiment could measure. Gemini CLI is not adopted.

Sized by plan 05's seeded-defect run: a private clone of party-buzzer (no remote) with one real copy change and six seeded defects, one per class (a logic off-by-one in `closestAwards`, a 390 px-only buzzer overflow, a `scoreKey` regression whose callers are outside the diff, a removed touch guard, a slider's lost `aria-label`, a quadratic `lay`), reviewed by eight `fresh` jobs of the `review` kind (four `claude-opus-5-5`, four `claude-fable-5-1`, same prompt, no tests run). Every reviewer named all six; one Opus reply had one section naming nothing seeded (a scope note, not a false finding). The run cost about 6 points of the Claude 5-hour window.

| k | family | subsets catching all six |
|---|---|---|
| 1 | opus | 4/4 (100 %) |
| 1 | fable | 4/4 (100 %) |
| 2 | opus, fable, mixed | 6/6, 6/6, 16/16 (100 %) |
| 3–4 | any | 100 % |

The default is the smallest set reaching 90 %: one reviewer. The family rule stands because both families scored alike and a different family costs nothing; the review models are listed in `review.toml` in that order (`claude-opus-5-5` first, since leads default to Fable). The fixture, the defect sheet, the replies and the scoring script are under `~/.cache/bmesh-experiments/sizing/` on k16, never in a repo. The experiment measured catch rate on seeded defects, not the rate at which a reviewer's findings are right about unseeded code; re-run it with new defect classes if that becomes the question.

### Models and config

Every model choice (lead, executor, routine default, reviewer rule), the hub URL, the runner, kind recycle lines and grace live in `~/.config/bmesh/config.toml` on each box, with per-repo overrides in `.bmesh/config.toml`. buildermesh ships Claude defaults; Rick's values live in command-center's `fleet/bmesh/`.

## Resolved while planning (2026-10-14)

Contradictions found when turning the settled design into plans, and the decision taken:

1. **A standing judge thread** contradicted "no model in the loop" and "only leads are T3 threads". There is no judge. Review is a routine kind; conflicts go to the lead; what the lead can't decide is an `ask`.
2. **`exclusive` mode** mixed where a routine runs with what it holds. Modes are `inline | fresh | pooled`; `slots` and `deterministic` are independent flags on any kind, and an inline run may hold a lease.
3. **Deterministic runs "at the requested tree"** need a commit to check out. Jobs carry a commit; the result key is that commit's tree hash; a node commits (WIP is fine) before queueing.
4. **Executor worktrees** collided with the global rule that worktrees come only from T3. They live under `~/.bmesh/worktrees/`; plan 03 updates the global CLAUDE.md line.
5. **"`t3_dispatch.py` stays as the adapter" versus a Go runner** resolves as the lanes bridge did: the Go `t3` runner runs the Python adapter as a subprocess and extends nothing.
6. **A separate job queue stream** cannot share `bm.>` with the events stream. A job is an inbox message to the kind; one stream, `bm-mesh`, holds everything, and the map mirrors it.
7. **`needs-rick`** was not portable. The event is `ask`; the recipient is the operator.
8. **Kind files** move from `.agents/routines/<kind>.md` to `.bmesh/routines/<kind>.toml`: buildermesh owns its own directory, and the daemon already reads TOML.
9. **Open item: which pstack leaves the lead skill names.** None; the operator's brief template adds them.
10. **Open item: judge standing or on demand.** Moot (1).
11. **Build order step 1** said builds keep starting through `t3_dispatch`; they do, through `bmesh lead start`, which calls it. The `dispatch` skill switches to `bmesh` for start and watch in the same slice.
12. **The map's websocket** is server-sent events: one direction is all it needs, and it keeps the binary to the standard library plus NATS and TOML.
13. **A `verified` list** would break `tools/proposals.py`, whose frontmatter grammar is `key: value` lines. It is one line of `;`-separated entries, and `bmesh done` checks each entry against a recorded result.
14. **An executor's `ask`** cannot go to the operator (only leads are in the console). It goes to the lead's inbox; the lead decides whether to ask.
15. **"Worker"** named both executors and routine runs. A running routine is an **instance**; the word worker is retired.
16. **The memory cap** is a config value, `[runner] max_leads`, that `bmesh lead start` enforces per box; plan 03 measures it.
17. **An hourly digest** is too slow for a question or a finished build. The daemon also pushes a lead's `ask`, `blocked` and `done` at once (plan 04, Task 5), once per event across restarts and boxes; the digest stays as the periodic summary.

## What goes, what stays

| Piece | Verdict |
|---|---|
| `delegation` plugin | Replaced by buildermesh's lead and executor skills and the CLI; its results-by-tree-hash idea moves into the daemon |
| `long-horizon` plugin | Replaced: a goal is a long spec; eggnogg's `docs/goals.md` stays archived, nothing converted |
| `tools/lanes` | Generalized into the daemon; tag `lanes-v1` first; removed in plan 05 |
| `pipe.py` | Replaced by `bmesh` in plan 01 |
| Relay spec, leftover trigger | Closed and removed in plan 05 |
| `t3_dispatch.py` | Stays, called by the `t3` runner; V2 rewrite only if V2 is adopted |
| `propose`, `dispatch`, `prune`, `repo-sync` skills | Stay; `dispatch` starts and watches through `bmesh` |
| pstack-claude | Keeps the leaf skills and the gated updater; skips the program playbooks; routes poteto-mode's playbook step to the lead skill |
| NATS hub and Mac leaf | Stay |
| ntfy | Stays as the only notification path |

## Build order

1. **The loop on the mesh** (plan 01): nodes, events, inbox, `bmesh lead start`, wake, silence, digest, a live event list in the map; `dispatch` and the build brief switch from `pipe.py` to `bmesh`; `pipe.py` goes.
2. **Routines** (plan 02): kinds, three modes, jobs, instances, recycle, leases, deterministic results.
3. **Executors** (plan 03): `bmesh executor new`, the lead and executor skills, branch hand-back, the memory-cap measurement.
4. **The map** (plan 04): the tree layout, lead pages, panels, quota and digest strip, and ntfy pushes for asks, blocks and completions.
5. **Migration** (plan 05): verification sizing on party-buzzer, party-buzzer kinds, retire delegation, long-horizon, lanes, the relay spec and the leftover trigger, pstack-claude routing.

## Open

- The exact grace and recycle lines (data; start from lanesd's 20 minutes and tune).
- Per-repo migrations still to approve, each its own approval (plan 05 did party-buzzer, to Rick's gate): `gametrainer`, `command-center`, `rickdiculousli-tools` and `computer-use` need their `.bmesh/routines/` kinds and the AGENTS.md mesh block (`align_repo.py`); `eggnogg-rework` needs the same plus Mac-only slots `game`, `capture` and `ghidra` in its `.bmesh/config.toml`, and its `.agents/long-horizon.md` still names `plugins/delegation/.../check.py` in the Mac's rickdiculousli-tools checkout, which now lives under `retired/delegation/` (the repo is dormant; nothing ran it when the plugin moved).
- ~~A T3 V2 adapter, only if V2 is adopted; `t3_dispatch.py` on 0.0.45 stays the runner until then.~~ **Closed 2026-10-16:** buildermesh 029, merged `f590bf9`, gives the `t3` runner a V2 path over T3's MCP tools; the V1 path and `t3_dispatch.py` go with 024.
