---
id: 094
title: Any bmesh command tells a long session its context, and when to compact
kind: feature
size: M
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity

A coordinating session can't see its own context. In the lean redesign (`docs/design/2026-10-24-lean.md`) the coordinator ran past 400k before compacting, and only by hand, at the operator's prompt.

The operator's policy (2026-10-24):
- hear the context from 250k, then every 50k;
- from 300k, compact loosely, at a milestone, never in the middle of something;
- from 400k, compact seriously;
- never compact while exploring or investigating;
- think about it when nudged while executing, and right before starting to execute.

He asked for a nudge that is portable and comes from buildermesh, not from one harness's hooks: "perhaps should be a buildermesh message nudge instead".

Every agent on a mesh already runs `bmesh` all through its work: leads and executors at each checkpoint, a coordinator at each build, watch and land. Claude Code gives each command it runs its session id, `CLAUDE_CODE_SESSION_ID`, and keeps the session's token usage in its transcript. So bmesh can tell a session its context without any hook or plugin.

## Proposal

In (as the operator revised it mid-build, 2026-10-24):

- **The line.** Any `bmesh` command run on a measured session's main thread ends with at most one line on stderr, after its own output, when the context has crossed a nudge since the session last heard. The form is `bmesh: this session's context is 312k. <advice>`. The advice is a default the session judges, not a rule:
  - from `nudge_from` (250k): `You'll hear again every 50k.`
  - from `compact_from` (300k): `Compact at your next milestone, after updating your state file; never while exploring or investigating.`
  - from `compact_soon` (400k): `Compact at the next safe point, after updating your state file.`

  Each crossing is said once per session. The state lives in `<state dir>/context/<session>`, pruned after 7 days.
- **A compaction is said too.** A drop below the last nudge heard is a compaction, the harness's own auto-compaction included. The first command after it prints, once, `bmesh: this session compacted (412k -> 38k); read your state file before going on.`, the first number the last context bmesh read, and the nudges start again.
- **Only a session's main thread hears it.** An Agent-tool subagent's commands carry its parent's environment unchanged (the same `CLAUDE_CODE_SESSION_ID` and `CLAUDE_PID`, checked 2026-10-24), so unchecked a subagent would be told its parent's context and use up the parent's nudge. So:
  - A command acting as a node with a parent (an executor, a round's engineer, a routine instance: its `--node`, `BMESH_NODE` or worktree resolves to a path with a `.`) never prints, and never records a nudge as heard.
  - Any other caller prints only while the session's main thread is in a tool call of its own: the newest assistant message in the transcript has a Bash call with no `tool_result` yet and no Agent or Task call without one. Built: Claude Code writes the message a second or so after its tool starts, so a quick command would find the message before it with every call answered. A model call in flight whose message isn't written yet (an `api-request` line newer than the newest message) counts as the main thread's tool call too; a subagent's model calls go to its own transcript. Otherwise it says nothing and leaves the state alone. A foreground subagent, whose parent waits on its Agent call, never hears its parent's line.
- **`bmesh context`** prints the context now with the advice, e.g. `context 312k (claude session ec03a02c): <advice>`, and takes `--json`. A session runs it at a milestone and right before executing a design. With no session it can measure, it says so and exits 0. Built: from a child node it says the context is its lead's; off the main thread it prints the number, no advice, and records nothing; a compaction's line goes to its stderr.
- **Measuring**, one reader per harness, cheap: read the transcript's tail, never the whole file.
  - **Claude Code:** `CLAUDE_CODE_SESSION_ID` names the session. Its transcript is `<CLAUDE_CONFIG_DIR, else ~/.claude>/projects/*/<id>.jsonl`. The context is the newest assistant message's `usage`: `input_tokens + cache_read_input_tokens + cache_creation_input_tokens`.
  - **Anything else** prints no line. Codex (0.164) gives a command no session id. A reader is added once a harness names its session.
- **Config** `[context]`: `nudge_from = 250000`, `nudge_every = 50000`, `compact_from = 300000`, `compact_soon = 400000`. `nudge_from = 0` turns it off. The manual notes these defaults suit a 1M window.
- **Skills:**
  - **The work skill gains "Long sessions":**
    - **What lasts is already content:** each unit's spec, which lands with its code, and the landed commits; the mesh (`mesh-status`) has what's building. A stream's plan and progress are disposable scratch, never committed, not `docs/design/` either; `docs/design/` is only for a decision that changes how the product works, written when the operator makes one.
    - **The state file** holds the plan, the order, the ids in flight, what's next, open questions and lessons. It lives in the repo's gitignored scratch dir or another place that outlives the session, never a temp dir; its path goes in replies and in the `/compact` instructions. Update it at each milestone. It's deleted when the stream ends; a lesson worth keeping becomes a rule in `AGENTS.md` or a check before the file goes.
    - **On the line**, act on its advice at the next milestone. Right before executing a finished design is a milestone: run `bmesh context`. The advice is a default the session judges: a session about to finish (its unit nearly done, about to land or report and retire) finishes instead of compacting; a step that needs what the session holds is finished first, then the state file updated, then the compaction.
    - **By role:** the coordinator compacts. A lead doesn't: it hands off by 091 and retires at done. Executors and round engineers never hear it.
    - **Resuming is part of compacting.** Where the harness lets the session queue into its own thread (T3 Code's `t3_thread_send` with mode `queue`), it queues two messages in one step: `/compact <what to keep, naming the state file>`, then `Resume: read <state file>, run bmesh mesh-status, and carry on from its Next.` Only when the operator asked it to pause does it queue the `/compact` alone, and its reply says it waits for the operator's word. Elsewhere it asks the operator to type `/compact` and then to resume. On the compaction's line, it reads the state file before going on.
    - **Effort follows the work** (the operator, 2026-10-24). Where the harness lets a session set its own effort for its next turn (T3 Code's `t3_thread_configure`, for example), it designs at high and executes at medium: high before queueing a `/compact` whose resume goes on to design, and for a block that needs real design; medium once the design is written and the work is running (watching, relaying, starting the next spec). The change takes effect from the next turn, never partway through one. Elsewhere it says so to the operator in a line, rather than assuming the harness.
  - **The lead skill takes the line as its measure for the hand-off (091), in a sentence:** a lead hands off rather than compacting, and retires at done. On 2026-10-23 neither compaction nor a restart paid for leads, whose tokens go mostly to review and land.

Out:
- the daemon sending into threads;
- a Claude Code hook;
- quota in the line, a later step;
- compacting on the session's behalf: the session decides.

Judge: opus, because the line's wording steers sessions' judgment, and the reader must fail silent, never wrong.

## What you'll see

In a coordinator's session past 300k, the next `bmesh watch` ends with:

`bmesh: this session's context is 304k. Compact at your next milestone, after updating your state file; never while exploring or investigating.`

The session compacts after its next landing, and its reply says so in a line.

## Verification

- **The repo's verify.**
- **Tests driving fake transcripts:**
  - each threshold is said once, in order;
  - nothing below `nudge_from`;
  - a drop resets;
  - an unknown or missing session, or a transcript with no usage, says nothing;
  - the line is on stderr only, never in `--json` stdout;
  - a large transcript's tail still yields the newest usage.
- **Tests of who hears it:** an executor's `--node` and an engineer's `BMESH_NODE` say and record nothing; a waiting Agent or Task call, and an idle thread, are not the main thread; a waiting Bash call and a model call in flight are; the compaction's line is said once.
- **The test harness clears `CLAUDE_CODE_SESSION_ID`**, so tests run inside a session stay silent.
- **On the real thing:** `bmesh context` in a live T3 session agrees with T3's own meter for that thread to within a turn's tokens.
- **The skill text** is measured with `scripts/skilltest` against the skills before it.

## Risks and the alternative considered

- **`CLAUDE_CODE_SESSION_ID` and the transcript's shape are Claude Code's**, not a published API. A change makes the reader find nothing, so the line goes quiet; it never prints a wrong number. `bmesh context` reporting no session is how that shows.
- **A background subagent run without `--node`, while its parent is in a model call or a Bash call of its own, still reads as the main thread** and hears the parent's line. An executor passes `--node` (its skill says so), so only an unnamed subagent can, and only in that window.
- **A session that runs no bmesh hears nothing.** By the policy that is a session exploring, which shouldn't compact.
- **Alternatives considered:**
  - a Claude Code `PostToolUse` hook: Claude Code only, and only with the plugin installed;
  - the daemon reading T3's recorded usage and sending it into the thread: T3 only, T3's private schema, and only at the ends of turns.

## Upgrading

- Agent sessions now hear their own context: in a Claude Code session, a `bmesh` command ends with one stderr line when the context crosses a nudge, from 250k tokens. Which nudges should this box's sessions get? Choices: the `[context]` defaults (`nudge_from = 250000`, `nudge_every = 50000`, `compact_from = 300000`, `compact_soon = 400000`, which suit a 1M-token window), values of your own for a smaller window, or `nudge_from = 0` for none. Default: the defaults, with nothing set.
- The work and lead skills change in the same release: update the buildermesh plugin after `bmesh update`.

## Needs from the operator
