---
id: 067
title: bmesh node forget --wipe removes a stopped node and everything under it from the mesh, as if it never ran
kind: maintenance
size: M
judge: opus
status: building
lens: eng
proposed_by: dfdc9673-27b2-4f3f-9f17-95a5b436de66
proposed_on: 2026-10-17
built_by: be9109d0-70ba-4862-ba55-6f9a40ddd133
host: k16
family: buildermesh-build-13
branch: bmesh/be9109d0-70ba-4862-ba55-6f9a40ddd133
---

## Opportunity
On 2026-10-17 at 04:25 the coordinator dispatched a lead (`buildermesh-066`) the operator never asked for. The operator stopped it in T3 and asked for it to be wiped "as if it doesn't exist", above all from NATS.

bmesh could do part of that:
- `node stop` doesn't apply here, since the thread was already stopped;
- `node forget --worktree` removed the node's current `bm-nodes`, `bm-state` and `bm-runner` entries, its inbox consumer, its worktree and its branch.

Several traces were left behind:
- **its event** (`bm.buildermesh-066.blocked`), kept in `bm-mesh` for 30 days and shown by `bmesh watch --nodes buildermesh-066`;
- **a purge marker on each of its three keys**, so each key's history still records the node;
- **its brief**, `~/.cache/bmesh/briefs/buildermesh-066.md` (16 KB, holding the spec and the operator's rules).

Wiping those took the raw `nats` CLI with the mesh token, and a JetStream API request (`$JS.API.STREAM.PURGE.KV_<bucket>` with a subject filter), because `nats stream purge` refuses without a terminal.

`forget` is also one node at a time. A lead's executors, a round's children and a family's feedback each need their own command. The README says so: "its events stay in the stream, and nodes under it aren't forgotten with it".

Interrupted and unwanted work will recur: a wrong dispatch, a round stopped early, a lead whose session died. The operator's wipe should be one command, through bmesh, with no token handling.

## Proposal
In:
- **`bmesh node forget <path> --wipe [--dry-run]`.** It takes the nodes `forget` takes: `done` or `blocked`, a round with no children left, and an idle node that never reported. Any other state is refused with `stop it first: bmesh node stop <path>`. For the node and every node under it (a lead's executors, a round's children, found by path prefix in `bm-nodes`), it:
  - does everything `--worktree` does: the worktree, the branch, and another box's worktree through the `forget-worktree` host job;
  - purges the node's events (`bm.<path>.>`) and its inbox (`bm.inbox.<path>`) from `bm-mesh`, with a subject-filtered purge;
  - purges each key's whole history in `bm-nodes`, `bm-state` and `bm-runner` (a subject-filtered purge of `$KV.<bucket>.<path>`), so no history and no marker is left;
  - deletes its inbox consumer;
  - deletes its brief from `<user cache dir>/bmesh/briefs/` on the box that ran it, through the same host job when that is another box;
  - purges its family's pending feedback (`bm.feedback.<family>`) when no node outside the wipe is in that family.
- **It prints what it removed:** `wiped buildermesh-066: 1 event, 3 keys, inbox, brief, worktree, branch` (and one line per node under it), and with `--dry-run` the same as `would wipe ...`. Then it publishes one line on the wiping box's subject, `bm.daemon.<host>.wiped`, text `<path> by <who>`. That leaves a record that a wipe happened, never of what the node did.
- **The skills say it.** "Wipe X" in `build`, `ideate` and `mesh-status` is `bmesh node stop` then `bmesh node forget X --wipe`, and the coordinator never reaches for the `nats` CLI or the token.
- The README's `node` section and its Buckets table say it.

Out:
- The spec's ledger commits on `main`, which aren't the mesh's to rewrite.
- The runner's thread (T3, tmux, claude).
- A push already sent.
- The daemon's own lines that name the node (`bm.daemon.<host>.wake`, `push`, `silent`): they live under the box's subject, so a subject-filtered purge can't single them out, and they expire with the stream after 30 days.
- Results recorded in `bm-results`, which are keyed by tree, not by node.

Judge: opus, because a purge filter that is one dot too wide deletes other nodes' history, and nothing brings it back.

## What you'll see
- `bmesh node forget buildermesh-066 --wipe --dry-run` prints `would wipe buildermesh-066: 1 event, 3 keys, inbox, brief, worktree, branch`.
- Without `--dry-run`, it prints the same as `wiped`.
- After the wipe, `bmesh watch --nodes buildermesh-066 --since <before it started>` prints nothing, and `nats kv history bm-nodes buildermesh-066` finds no entry.

## Verification
The repo's verify, plus `cmd_node_test.go` on `meshtest.Start`:
- **What goes:**
  - a wiped lead with two executors leaves no message on `bm.<path>.>` or `bm.inbox.<path>`, for itself or for them;
  - no history on its three keys in any bucket;
  - no consumer, no brief, worktree or branch;
  - one `wiped` line on the box's subject.
- **What stays:**
  - a sibling with a common prefix (`buildermesh-06` beside `buildermesh-066`, `buildermesh-066-2`) keeps every message and key;
  - another family's feedback stays;
  - a family with a node outside the wipe keeps its feedback.
- **What's refused:**
  - a `working` node, with the stop line;
  - `--dry-run` changes nothing.
- **Another box:** a node there has its worktree and brief removed through the host job.

## Risks and the alternative considered
- **A filter that matches more than the node.** Paths are matched as whole tokens: `bm.<path>.>` and `$KV.<bucket>.<path>` exactly, never a prefix of a token. The sibling test covers it.
- **Evidence for a postmortem is lost.** A wipe is the operator's call, and it leaves the `wiped` line. Plain `forget`, which keeps the events, stays the default.
- **Alternative: let the 30-day stream age do it.** That leaves the node visible to `watch` for a month and leaves the key history behind for good.

## Upgrading
nothing: a wipe that reaches another box needs that box on this release; an older one refuses the `forget-worktree ... wipe` job, and the wipe stops before it purges anything from the mesh.

## Needs from the operator
