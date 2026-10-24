---
id: 105
title: Every box puts each agent's token use on the mesh, per model call, with a coordinator's design and execution apart, and bmesh usage reads it
kind: feature
size: L
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity

No token count is visible anywhere on the mesh. The map, `mesh-status` and `watch` carry states and words, but no numbers. `bmesh context` (094) reads only the calling session's newest call. `bmesh audit` (101) reads one transcript, on demand, on its own box.

Measured on k16 on 2026-10-24, from `~/.claude/projects` and T3's database:

- **One coordinator dominated.** Coordinator session `ec03a02c` read 416M tokens in the day: 185M itself and 231M in its 11 Agent subagents. That is 58% of everything the box read. Its context peaked at 537k, past `compact_soon`. All of the day's leads and fixers together read about 215M.
- **Leads of one size spread tenfold.** M leads since 10-15 read a median of 10.2M. `buildermesh-096` read 42.2M (38.9M itself, 3.3M in one subagent), and land refused it 5 times. `buildermesh-098` read 6.8M and landed on its first try.
- **One summed number misleads.** Cache reads are about 99% of the raw input count (43.0M of 43.4M in 096's longest turn), so the four kinds must stay apart.
- **One hour shows a runaway.** Over the six days to 10-24, two agents read more than 100M tokens in one hour (one of them `ec03a02c`), and the heaviest lead read 42M.

The data is already on every box. Every Claude Code session writes a transcript whose model calls each carry `usage`: T3's leads, fixers and rounds, a person's chats, and the `claude -p` routine jobs. Agent subagents write their own files beside it.

The operator (2026-10-24) asked for three things:
- token use per agent of every kind, in total;
- the context size after each turn;
- a coordinator's tokens split between its design and discussion and its execution.

## Proposal

In:

- **`internal/usage`**, one package holding the call record, the transcript reader, the publisher, and the reads that `bmesh usage` and the map (107, 108) share. Its turn reading and its "changes something" test come from `internal/audit`, called rather than copied.

- **The record.**
  - **What a call is:** one per model call (one `message.id`), taken from the first line that carries it, as `audit` counts. It records:
    - `at`, `id` and `model`;
    - `in`, `cr`, `cw`, `out`: the input, cache read, cache write and output tokens;
    - `ctx` = `in + cr + cw`, the context the call read;
    - `sub`: empty on the main thread, else the subagent's id;
    - `phase`: `design` or `exec` for a coordinator, empty for a node;
    - `cause`: what fed the call's growth (below).
  - **A compaction** (`compact_boundary`) is a mark `{at, kind: "compact"}`.
  - **What a message is:** one message per transcript file per pass, carrying the file's new calls and marks, cut at 500 calls. Its head holds `agent`, `role`, `host`, `repo`, `session`, `file`, and for a subagent file `sub` and `desc`.

- **The stream `bm-usage`**, subjects `bmusage.>`, holding 30 days in a file store with a 2-minute duplicate window, and provisioned beside `bm-mesh`.
  - Subjects: `bmusage.node.<path>` for a node, `bmusage.session.<host>.<session id>` for a coordinator.
  - Messages are sent with `Nats-Msg-Id` `<host>/<file>/<offset>`.
  - The stream sits outside `bm.>`, so the map's event list, `watch`, the push loop and the activity reader never see it.

- **The reader**, a daemon loop on every box, `every 30s`, independent of `[routines]`:
  - **What it reads:** the `.jsonl` files under `<CLAUDE_CONFIG_DIR, else ~/.claude>/projects/*/` and their `*/subagents/` dirs whose size changed since the last pass. Each file is read from its byte offset to its last newline, so a half-written line waits for the next pass.
  - **The cursor:** per file, its offset, assignment, last message id, phase state and pending cause, kept in `<state dir>/usage/cursor.json`. The cursor is written through a temp file and a rename, only after the stream acks the message.
  - **Duplicates:** a crash between the ack and the cursor write republishes with the same `Nats-Msg-Id`, and reads also count each `(agent, id)` once. So a retry never double counts.
  - **The first pass with no cursor file** reads the files modified in the last 30 days, oldest first, at most 64 MB per pass, and drops calls older than 30 days. On k16 that is about 1.1 GB, about 17 passes. Later passes read only what grew (about 110 MB a day on k16).
  - **Lines it doesn't know** are skipped, logged once per file (`usage: <file>: skipped N lines it does not read`), and counted as nothing. Without a hub, the pass is skipped.

- **Which agent a file is**, decided once per file and kept in the cursor. The first rule that holds wins:
  1. A subagent file whose first user message starts `You are executor <path> in ` names that executor node, when it is on this box.
  2. A main file whose session id (its name) is the `session` of a node on this box (101) belongs to that node.
  3. A main file whose first `cwd` is the `worktree` of a node on this box belongs to the newest such node started at or before the file's first line. This catches every lead from before 101.
  4. A first `cwd` that is a routine instance's checkout, `<[runner] checkouts>/<repo>/routine.<kind>.<model-short>-<n>`, belongs to the node `routine.<kind>.<host>-<model-short>-<n>`.
  5. Any other main file is a coordinator session, `session.<host>.<id>`, role `coordinator`.
     - Its `repo` is the registry row whose checkout is the cwd's git common dir, so a worktree counts as its repo; else `other`.
     - A main file whose cwd is a git worktree other than a main checkout waits for its node, unread, up to 10 minutes from its first line, so a lead's `started` can name it first.
  6. Any other subagent file is a subagent of its main file's agent: `sub` is its id, and `desc` is the spawning Agent call's `description` (found through the parent's tool result that names the subagent; check the field on real transcripts), else the id alone.

- **The coordinator's phase.** Every call of a coordinator session is `design` or `exec`; nodes carry none:
  1. A turn that `internal/audit` reads as the operator's request starts in `design`.
  2. A turn opened by a `<task-notification>` line (a background command or Monitor waking the session) starts in `exec`.
  3. A turn moves to `exec` at its first tool call that changes something, and stays `exec` until the next turn starts. Changing something means:
     - a shell command `internal/audit` reads as acting: a `bmesh` command that starts or changes something, a `git commit` or a `git push`;
     - or an Edit or Write to a file inside a git work tree that git doesn't ignore, outside `docs/proposals/` and `docs/design/`. Specs, design docs, mockups and state files in a gitignored scratch dir, memory and config stay `design`. A path whose dir is gone counts as tracked.
  4. The call that makes the move is `exec`, and the calls before it are `design`.
  5. A subagent's calls take the phase its session was in at the subagent's first line. A review subagent while landing is `exec`; an Explore subagent during a design is `design`.

- **The cause** of a call names the tool uses of the previous assistant message in the same file:
  - Read, Write, Edit or NotebookEdit: `<tool> <file basename>`;
  - Bash: `Bash: <its description>`, or `Bash` when it has none, never the command text;
  - Skill: `Skill <name>`;
  - Agent or Task: `Agent <description>`;
  - any other tool: its name;
  - several uses: the first and ` +N`;
  - the first call of an operator's turn: `message`; after a wake: `wake`; after a compaction: `compact`.

  The cause is cut to 60 characters. No other session text leaves the box.

- **Reads**, `usage.Read(ctx, client, since, filter)`, for the CLI and the map:
  - Reading starts the stream at `since`. A call is always published after it happened, so this misses nothing.
  - It keeps calls with `at >= since` and counts each `(agent, id)` once.
  - Per agent it sums: role, host, repo, models, first and last call, call count, the four kinds for the main thread and for subagents, the four kinds per phase for a coordinator, peak and last main-thread `ctx`, compactions, and per subagent its `desc` and its kinds.
  - Node roles come from `bm-nodes`.
  - With an agent named, it also returns that agent's calls and marks in order.

- **`bmesh usage [<path> | --session <id>] [--since D] [--repo R] [--calls] [--json]`**, from any box on the mesh:
  - **The defaults:** `--since 24h`, which may not exceed `720h` (exit 2). `--repo` keeps one repo's agents.
  - **With no agent,** one line per agent with calls in the window, most read first, then a total line:

    ```
    AGENT                 ROLE         HOST  READ    OUT    SUB    PEAK  DESIGN  EXEC
    session.k16.ec03a02c  coordinator  k16   416M    1.9M   231M   537k  61%     39%
    buildermesh-096       lead         k16   42.2M   112k   3.3M   347k  -       -
    total 711M read, 2.9M out, 27 agents since 10-23 21:00
    ```

    In this sample, READ, SUB and PEAK are 10-24's measured figures. OUT, DESIGN, EXEC and the total show the format only.

  - **With an agent:**
    - its head line: role, host, repo, models, span;
    - `read` split into input, cache read and cache write, then `out`;
    - main against subagents, each subagent with its `desc`;
    - the phase split, for a coordinator;
    - its context: peak, last and compactions.
  - **With `--calls`,** one line per call: `17:40:12  ctx 303k  +18.8k  out 845  Read cases.go`, with a subagent's calls prefixed by its id.
  - **`--session`** takes a full id or its first 8 characters. Sessions print as `session.<host>.<first 8>`.
  - **Refusals and errors:**
    - a path that is no node: `refused: no node <path>` (exit 3);
    - an agent with no calls in the window: `no usage for <agent> since <time>` (exit 0);
    - no hub: the same error as `mesh-status`.
  - **`--json`** prints the aggregates, and the calls with `--calls`.

- **Docs:** the manual's `bmesh usage` and the daemon's usage loop; the internals' names table (stream, subjects), the record, the match and phase rules, and the cursor; the README index.

Out:
- **Codex.** A Codex command gets no session id, and k16 has had no Codex turns since 10-16. A reader is added when one runs.
- **T3's database.** It is per turn only and leaves out subagents and `claude -p`.
- Prices or dollars; any session text beyond the cause.
- A lead's tokens by its state (working, landing).
- The console's panels (106, 107, 108).

Order: before 107 and 108; builds beside 106 (no shared files but the docs).

Judge: opus, because the match and phase rules must hold on real transcripts, and a wrong number teaches the operator to ignore the console.

## What you'll see

On any box, `bmesh usage` lists every agent of the last 24 hours: leads, executors, fixers, rounds, routine instances, each box's coordinator sessions, and their subagents folded in. It shows what each read and wrote, its peak context and, for a coordinator, how much went to design and how much to execution. `bmesh usage buildermesh-096 --calls` shows the context after every model call and what grew it.

## Verification

- The repo's verify.
- **Tests on fake transcripts** in `internal/usage/testdata`, cut from real sessions with nothing personal in them:
  - **Matching:** each match rule; an executor's subagent file; a lead's worktree file waiting for its `started`.
  - **Counting:**
    - a message id counted once across its content lines;
    - a half-written last line left for the next pass;
    - a compaction mark;
    - a restart from the cursor publishing nothing twice;
    - a duplicated message counted once by `Read`.
  - **Phase:**
    - an operator turn starts `design`;
    - a spec Write stays `design`;
    - a `git commit` and an acting `bmesh` command each move it to `exec`;
    - a `<task-notification>` turn starts `exec`;
    - a subagent takes its session's phase.
  - **Cause:** never holds a Bash command's text.
- **`cmd/bmesh` tests** drive `usage` through a `meshtest` hub: the table, an agent, `--calls`, `--json`, and each refusal.
- **The real thing:** the built binary's reader run over k16's own transcripts into a scratch hub (`scripts/seed.sh`'s serve). `bmesh usage --since` over 2026-10-24 must show:
  - `session.k16.ec03a02c` within 1% of 416M read, 231M of it in subagents;
  - `buildermesh-096` at 38.9M main;
  - `buildermesh-098` at 6.8M;
  - each coordinator session with a design/exec split.

  That output, the first pass's wall time and its peak CPU go in the closing commit.

## Risks and the alternative considered

- **The first pass loads the box.** It reads up to a gigabyte, bounded at 64 MB a pass, and is measured on k16.
- **Claude Code changes its transcript format.** Unknown lines count as nothing and are logged once: a missing number beats a wrong one.
- **A phase rule misfiles a call.**
  - A coordinator making a change by hand edits tracked code (`exec`), but its reading before the first edit counts `design`.
  - The rules are few, and the CLI prints the split, so a misreading is visible.
- **What leaves the box:** counts, model names, times, repo names, subagent descriptions and cause words. A Bash description is written by the model and could carry a value it shouldn't; that's the same exposure as a T3 thread title.
- **Alternative: read T3's database,** which holds per-turn totals and end-of-turn context for T3 threads of both providers. It leaves out per-call context, subagents, `claude -p` jobs and sessions outside T3, and ties bmesh to T3's schema (t3-done already idles when that schema changes).
- **Alternative: harness hooks.** 094 chose a portable reader over hooks.

## Upgrading

nothing. A box's first pass after the update reads its last 30 days of transcripts (on k16, minutes of one core). A box not yet updated publishes nothing, so its agents show no usage until it is.

## Needs from the operator
