---
id: 018
title: Leads leave feedback; the coordinator drains it into fixes and rounds
kind: feature
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-15
built_by: 6db8bf14-782d-4931-99f9-d2c7084f66e0
host: k16
branch: t3code/9b9a69bc
---

## Opportunity
A lead hits friction outside its spec, works around it or fixes it in passing, and the lesson leaves with the thread. The README records that cost after the fact: 007 came from a stall leads had been working around (the lead skill warned about it twice), and 010 and the "T3 renames lead branches" rule came from leads landing the wrong branch more than once. Each was found by one lead, then met again by the next leads before anyone wrote it up as a spec. Nothing on the mesh collects "I hit X, here's what I did about it" from one build so the next builds don't repeat it, and nothing reads it at the end of a batch.

## Proposal
In:
- **Build families.** A family is the set of leads one coordinator dispatched together. `bmesh build` takes `--family F`. Without it, each invocation mints `<repo>-build-<N>` (`N` one past the highest family registered for the repo in `bm-nodes`, as rounds are numbered) and prints it on the JSON line and in each `--accepted` line. The lead's node records it (`family` on Node), and so does its spec (`family:` in the frontmatter, landed with `building`). A coordinator dispatching across several commands or repos passes the first family to the rest; over ssh it goes on the remote command line.
- **The feedback subject** is `bm.feedback.<family>` in `bm-mesh`. `feedback` becomes a reserved first token for paths, like `inbox` and `daemon` (Names). There is no consumer: a message is in the queue for exactly as long as it is in the stream.
- **`bmesh feedback "<text>" [--repo R] [--where <file|command>] [--fixed <sha>]`**: optional, and only once a lead has decided it is done, just before `bmesh done`. It reads the family, repo and spec from the node (a node with no family is refused: exit 3, `no build family`) and publishes `{from, family, repo, spec, commit, where, fixed, text, at}`. `--repo` defaults to the node's repo. It names `buildermesh` when the friction is in the mesh itself (bmesh, a skill, a brief). `--fixed` points at a fix the lead already landed, so the coordinator knows what's left is the general case. A lead can write it more than once, one issue per message. The lead skill's landing step gets one sentence: friction outside the spec that another build would hit is worth one `feedback` line each; your own spec's work is not.
- **`bmesh feedback list <family> [--json]`** prints what is pending, each with its stream sequence. **`bmesh feedback drain <family> [--through <seq>]`** purges the family's subject from `bm-mesh` (a subject-filtered purge, up to and including `--through`, else up to the last message there when it starts). The messages are deleted from the stream, not just acked: nothing about them is kept on the mesh. Only that family's subject is touched. A message published during the drain, after `--through`, stays. It prints `drained N from <family>`. `mesh-status` adds a closing line `feedback: N pending in <family>` per family with any pending, so feedback whose coordinator has gone isn't lost before it is drained.
- **The coordinator** (the `build` skill) gets a step 7: once `watch` prints `watch-done` for a family, it runs `feedback list <family>`, then synthesizes. It merges duplicates and drops anything already covered by a spec on main (any status, so rejected ideas don't come back) or settled by a `--fixed` commit. It sorts the rest:
  - *Quick fix*: one cause, one obvious change, XS or S. The coordinator writes the spec (`bmesh spec next-id`, the template, `proposed_by` the coordinator's thread, `from_feedback` the source family and leads) and lands it as `accepted` (`Feedback NNN: accepted from <family>`). It runs `bmesh build <repo> NNN` straight away, with no operator review, in a new family, and watches that lead the same way.
  - *Needs thought*: `bmesh ideate <repo> --context "<the issue, its evidence, the leads that hit it>"`, one round per repo with all its open issues as the focus.
  - Once every item has gone to a spec or a round, it runs `feedback drain <family> --through <the last seq it listed>`. The specs' `from_feedback` and the rounds' focus are the only record left. A coordinator that dies between list and drain leaves the family pending, and `mesh-status` shows it.
  - The coordinator reports one line per issue: `fixed by NNN (started)`, `to round N`, `already NNN` or `dropped: <why>`.
- **Loop bound**: a lead built from a `from_feedback` spec can leave feedback, but the coordinator never auto-dispatches from it. Those issues always go to a round.

Out: any daemon-side synthesis (the coordinator is a model thread, and that's where the judgment is), pushes for feedback, and executors writing feedback. Executors report to their lead, and the lead decides what affects other builds.

Judge: opus, because the CLI part is small but the skill text decides what counts as collective and as quick, and that wording is what everything else rests on.

## What you'll see
`bmesh build --accepted buildermesh` prints `family buildermesh-build-3`. After its leads finish, the coordinator's final message carries a feedback section, e.g. `inbox read slow on empty: fixed by 019 (started)`, `routine timeouts vs queueing: to round 4`, `T3 branch rename: already 010`, then `drained 3 from buildermesh-build-3`. `bmesh feedback list buildermesh-build-3` then prints nothing. Until the drain, `bmesh mesh-status` shows `feedback: 3 pending in buildermesh-build-3`.

## Verification
The repo's verify. New tests:
- `build` mints a fresh family per invocation and `--family` joins one; node and spec both carry it.
- `feedback` publishes to `bm.feedback.<family>` with the node's family, repo and spec filled, and is refused for a node with no family.
- `drain` deletes the family's messages from `bm-mesh` (a direct stream read of the subject finds none), leaves another family's messages and a message after `--through` in place, and leaves the stream's other subjects untouched.
- `mesh-status` prints the pending line.
- A path whose first token is `feedback` is refused.

README (Names, Builds for `--family`, the ledger's `family` and `from_feedback` keys, a new Feedback section, Finished nodes for the status line) and the `lead` and `build` skills change in the same commit.

## Risks and the alternative considered
Noise: leads might report their own spec's bumps. The skill sentence and the coordinator's dedup against the ledger are the filter, and a dropped item costs one line. Quick fixes skip the operator's review by design. They're kept narrow by the XS/S rule, the one-level loop bound, and the coordinator's report naming every spec it accepted.

Alternative: a separate `bm-feedback` stream with work-queue retention, so an ack deletes. Rejected: `bm-mesh` already owns `bm.>`, and JetStream refuses overlapping subjects, so it would need a second subject root. An ack per message also can't drain one family up to a sequence the way a filtered purge does.

Alternative: put feedback in the lead's `done --text`. Rejected: it mixes the report with the lesson, and there is no queue left for a coordinator that arrives after the lead has gone.

## Needs from the operator
None.
