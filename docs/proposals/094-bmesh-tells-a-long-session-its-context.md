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

In:

- **The line.** Any `bmesh` command run inside a session it can measure ends with at most one line on stderr, after its own output, when the context has crossed a nudge since the session last heard. The form is `bmesh: this session's context is 312k. <advice>`. The advice:
  - from `nudge_from` (250k): `You'll hear again every 50k.`
  - from `compact_from` (300k): `Compact at your next milestone, after updating your state file; never while exploring or investigating.`
  - from `compact_soon` (400k): `Compact at the next safe point, after updating your state file.`

  Each crossing is said once per session. The state lives in `<state dir>/context/<session>`, pruned after 7 days. A drop below the last nudge, which is what a compaction looks like, resets it.
- **`bmesh context`** prints the context now with the advice, e.g. `context 312k (claude session ec03a02c): <advice>`, and takes `--json`. A session runs it at a milestone and right before executing a design. With no session it can measure, it says so and exits 0.
- **Measuring**, one reader per harness, cheap: read the transcript's tail, never the whole file.
  - **Claude Code:** `CLAUDE_CODE_SESSION_ID` names the session. Its transcript is `<CLAUDE_CONFIG_DIR, else ~/.claude>/projects/*/<id>.jsonl`. The context is the newest assistant message's `usage`: `input_tokens + cache_read_input_tokens + cache_creation_input_tokens`.
  - **Anything else** prints no line. Codex (0.164) gives a command no session id. A reader is added once a harness names its session.
- **Config** `[context]`: `nudge_from = 250000`, `nudge_every = 50000`, `compact_from = 300000`, `compact_soon = 400000`. `nudge_from = 0` turns it off. The manual notes these defaults suit a 1M window.
- **Skills:**
  - **The work skill gains "Long sessions":**
    - Keep a state file for each stream of work, somewhere that outlives the session, and name it in replies.
    - At each milestone, update it with what landed, what's in flight with its ids, what's next, decisions and lessons.
    - On the line, act on its advice at the next milestone. Right before executing a finished design is a milestone: run `bmesh context`.
    - To compact, where the harness lets a session queue `/compact <what to keep>` into its own thread (T3 Code's `t3_thread_send` with mode `queue`), queue it, then a resume message unless the operator paused. Elsewhere, ask the operator to type `/compact`. Say it in one line of the reply.
  - **The lead skill takes the line as its measure for the hand-off (091), in a sentence:** a lead hands off rather than compacting. On 2026-10-23 neither compaction nor a restart paid for leads, whose tokens go mostly to review and land.
  - **The executor skill says the line is its lead's, in a sentence:** an executor never compacts. Built: a subagent's commands carry its parent's `CLAUDE_CODE_SESSION_ID`, so an executor spawned as a subagent hears its lead's context.

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
- **The test harness clears `CLAUDE_CODE_SESSION_ID`**, so tests run inside a session stay silent.
- **On the real thing:** `bmesh context` in a live T3 session agrees with T3's own meter for that thread to within a turn's tokens.
- **The skill text** is measured with `scripts/skilltest` against the skills before it.

## Risks and the alternative considered

- **`CLAUDE_CODE_SESSION_ID` and the transcript's shape are Claude Code's**, not a published API. A change makes the reader find nothing, so the line goes quiet; it never prints a wrong number. `bmesh context` reporting no session is how that shows.
- **A session that runs no bmesh hears nothing.** By the policy that is a session exploring, which shouldn't compact.
- **Alternatives considered:**
  - a Claude Code `PostToolUse` hook: Claude Code only, and only with the plugin installed;
  - the daemon reading T3's recorded usage and sending it into the thread: T3 only, T3's private schema, and only at the ends of turns.

## Upgrading

- Agent sessions now hear their own context: in a Claude Code session, a `bmesh` command ends with one stderr line when the context crosses a nudge, from 250k tokens. Which nudges should this box's sessions get? Choices: the `[context]` defaults (`nudge_from = 250000`, `nudge_every = 50000`, `compact_from = 300000`, `compact_soon = 400000`, which suit a 1M-token window), values of your own for a smaller window, or `nudge_from = 0` for none. Default: the defaults, with nothing set.
- The work, lead and executor skills change in the same release: update the buildermesh plugin after `bmesh update`.

## Needs from the operator
