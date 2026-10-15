---
id: 010
title: A lead's branch follows its worktree, not its dispatch
kind: refinement
size: S
judge: sonnet
status: proposed
lens: eng
proposed_by: fd21a373-851c-4e68-beb6-831cb4be0fa9
proposed_on: 2026-10-15
---

## Opportunity
On 2026-10-15 T3 Code renamed lead 005's branch from `t3code/b0ee2f6d`, the name dispatch recorded in the node and the spec (`Dispatch 005: branch t3code/b0ee2f6d`), to `t3code/command-center-reorg` while the lead worked; the lead's `git merge --ff-only t3code/b0ee2f6d` in the main checkout failed with "not something we can merge", and the fix lives in a session memory rather than in the product. `bm-nodes`, `bmesh status --json`, the map's lead page and the ledger all still name the dispatch-time branch; nothing re-reads it. `executor new` already follows a rename (README Executors: "the branch is the worktree's current one, so a renamed branch is followed", `cmd/bmesh/gitwork.go:26`), so the lead is the one node the mesh can misname. Lost work is one of this round's three focus areas (inbox, 2026-10-15): a lead that lands the wrong branch lands nothing.

## Proposal
In:
- The daemon's poll (`internal/daemon/poll.go`, once a minute per local lead and engineer that is not done) reads the worktree's current branch (`git symbolic-ref --short HEAD` in `n.Worktree`, when the worktree is on this host and exists) and, when it differs from `n.Branch`, rewrites the node and logs `branch: <path> <old> -> <new>`. A detached worktree leaves the node as it is.
- The lead skill §7 and the build brief's standard closing say in one sentence: land the branch your worktree is on (`git branch --show-current`), never the name in your brief, and before the merge run `bmesh spec set --id NNN --field branch=<that name>` when it differs, so the ledger's `branch` names the branch that was merged. The README's ledger section says `branch` is set by dispatch and corrected by the build thread when the runner renamed it.
- `bmesh build`'s second commit (`Dispatch NNN: branch <b>`) is unchanged.
Out: a `git branch` watch (a minute's lag is fine); executors (already right).

Judge: sonnet, because every step is named and each touches one small place.

## What you'll see
`bmesh status --json` and the map show `t3code/command-center-reorg` within a minute of the rename; a spec landed after a rename reads `branch: t3code/command-center-reorg`, and the lead's land step works first time.

## Verification
The repo's verify. New tests: a poll after a rename in the worktree rewrites the node's branch and a detached worktree does not; the rendered build brief carries the sentence; the lead skill's §7 names `git branch --show-current`.

## Risks and the alternative considered
Reading one git ref per lead per minute: negligible. Alternative: fix it in the lead skill only; rejected, the node and the map would keep lying and the skill's memory is what failed once already. Alternative: ask T3 not to rename; not ours.

## Needs from the operator
None.
