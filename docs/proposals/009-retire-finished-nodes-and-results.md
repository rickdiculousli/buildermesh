---
id: 009
title: Finished routine instances, nodes and old results leave the mesh
kind: maintenance
size: M
judge: opus
status: done
lens: eng
proposed_by: fd21a373-851c-4e68-beb6-831cb4be0fa9
proposed_on: 2026-10-15
built_by: abc563ec-cc1d-4b35-9bee-03a5e73b60dd
host: k16
branch: t3code/c4422981
verified: verify ok t:cf62a07e
merged: 608604f70383859b33220783f476303fe638f61e
---

## Opportunity
Nothing in the binary deletes a node: `internal/mesh/nodes.go` has no delete, and `bm-results` has no age limit (`provision.go`). After one day of use `bmesh mesh-status` on k16 prints 31 rows: 8 `routine.review.f<id8>` nodes `done` since 11:4x from a sizing experiment whose repo (`party-buzzer-sizing`, under `~/.cache/bmesh-experiments`) is not even in the registry, 6 done leads, 1 done executor, 10 idle pooled instances and 6 round nodes. A fresh kind adds one node per job forever; the map's `/api/nodes` and tree carry them all. The README says a fresh instance's node stays "for the map", but the map's window is 24 h. Results are keyed by tree and never expire, while the stream that holds the jobs lives 30 days and the `verified` line in the spec is the lasting record. The operator's focus for this round (inbox, 2026-10-15) asks for better retirement flows for routines.

## Proposal
In:
- A fresh instance's node, state and runner sample are deleted when its run ends and its checkout is removed; its `started`, `progress` and `done` events stay in the stream for the map's window and the reply's `by` still names it. A pooled instance stopped because its repo left the registry or its kind left the repo's main is marked `done` as now; the daemon's kind rescan (once a minute) deletes such a node once it has been `done` for `[status] keep_done`.
- `bmesh mesh-status` hides nodes `done` for longer than `[status] keep_done` (a new config.toml section, default `24h`, a row in the README's Config table) and ends with `… N finished nodes older than 24h (--all shows them)`; `--all` shows every node; `--json` carries them all with no flag, as the map does. `blocked` nodes are never hidden: they wait on the operator. A round and its children are one unit: hidden together.
- `bmesh node forget <path> [--worktree]`: deletes a `done` or `blocked` node, its state, runner sample and inbox consumer (exit 3 for any other state, naming it; a lead whose executors are not all `done` or `blocked` is refused too). `--worktree` also removes the node's worktree and branch when they are on this host and the worktree is clean (`git worktree remove` and `branch -d`; a dirty worktree or an unmerged branch is refused, naming it); without the flag nothing on disk is touched and the output says `worktree <path> kept`.
- `bm-results` gets a `MaxAge` of `[routines] keep_results` (default `30d`, the stream's age): a result older than that is gone, so a deterministic kind reruns and `bmesh done` of a spec landed over a month ago needs its routines run again (the `verified` line is the record that stays).
Out: deleting events from the stream; a kind drift report for reuse across repos (today 4 of the 5 ship repos on k16 have a `review.toml` byte-identical to the embedded example and party-buzzer's differs only in its comment, so a prompt change is five commits by hand; its own spec, since it changes `scaffold --check`'s exit contract and the scaffold skill); the map's rendering of hidden nodes (the UX engineer's).

Judge: opus, because every deletion and default is pinned and the change crosses the daemon, the CLI and provisioning.

## What you'll see
`bmesh mesh-status` the morning after shows what is working, asking, blocked and recently finished, not every job ever run; the map's node list stops growing with each fresh job; `bmesh node forget buildermesh-004 --worktree` removes a finished lead and its clean worktree in one command.

## Verification
The repo's verify. New tests: a fresh run deletes its node and keeps its events; the rescan deletes a stopped pooled node after `keep_done` and not before; `mesh-status` hides a day-old done node, shows a day-old blocked one, and `--all` shows both; `node forget` refuses a working node, removes a done one's consumer, and `--worktree` refuses a dirty worktree; the results bucket is provisioned with the configured age.

## Risks and the alternative considered
A result that expires between a lead's landing and its `bmesh done`: only if that gap is a month; the error names the missing result. Alternative: never delete, hide in the map only; rejected, `bmesh mesh-status` and `/api/nodes` are the operator's and the skills' view and they grow without bound. Alternative: a kind-level `keep` instead of one config default; rejected until a kind needs it.

## Needs from the operator
None.
