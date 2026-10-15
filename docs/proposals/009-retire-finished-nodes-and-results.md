---
id: 009
title: Finished routine instances, nodes and old results leave the mesh
kind: maintenance
size: M
judge: opus
status: proposed
lens: eng
proposed_by: fd21a373-851c-4e68-beb6-831cb4be0fa9
proposed_on: 2026-10-15
---

## Opportunity
Nothing in the binary deletes a node: `internal/mesh/nodes.go` has no delete, and `bm-results` has no age limit (`provision.go`). After one day of use `bmesh status` on k16 prints 31 rows: 9 `routine.review.f<id8>` nodes `done` since 11:4x from a sizing experiment whose repo (`party-buzzer-sizing`, under `~/.cache/bmesh-experiments`) is not even in the registry, 6 done leads, 1 done executor, 10 idle pooled instances, 4 round nodes. A fresh kind adds one node per job forever; the map's `/api/nodes` and tree carry them all. The README says a fresh instance's node stays "for the map", but the map's window is 24 h. Results are keyed by tree and never expire, while the stream that holds the jobs lives 30 days and the `verified` line in the spec is the lasting record. The operator's focus for this round (inbox, 2026-10-15) asks for better reuse and retirement flows for routines.

## Proposal
In:
- A fresh instance's node, state and runner sample are deleted when its run ends and its checkout is removed; its `started`, `progress` and `done` events stay in the stream for the map's window and the reply's `by` still names it. A pooled instance stopped because its repo left the registry or its kind left the repo's main is marked `done` as now and deleted after `[status] keep_done`.
- `bmesh status` hides nodes `done` or `blocked` for longer than `[status] keep_done` (config.toml, default `24h`) and ends with `… N finished nodes older than 24h (--all shows them)`; `--all` shows every node; `--json` carries them all with no flag, as the map does. A round and its children are one unit: hidden together.
- `bmesh node forget <path>`: deletes a `done` or `blocked` node, its state, runner sample and inbox consumer (exit 3 for any other state, naming it; a lead whose executors are not all done is refused too). Worktrees are not touched: they are the runner's (and `prune_worktrees` already removes them).
- `bm-results` gets a `MaxAge` of `[routines] keep_results` (default `30d`, the stream's age): a result older than that is gone, so a deterministic kind reruns and `bmesh done` of a spec landed over a month ago needs its routines run again (the `verified` line is the record that stays).
- Reuse: `bmesh align --check` also lists a kind whose body, comments aside, differs from the embedded example as `drifted <file>`, and `align --kinds` rewrites those from the example (today 4 of the 5 ship repos' `review.toml` are byte-identical to the example and party-buzzer's differs only in its comment; a prompt change today is five commits by hand). Existing behaviour unchanged: without `--kinds`, an existing kind is never touched.
Out: deleting events from the stream; retiring leads' worktrees; the map's rendering of hidden nodes (the UX engineer's).

Judge: opus, because every deletion and default is pinned and the change crosses the daemon, the CLI, align and provisioning.

## What you'll see
`bmesh status` the morning after shows what is working, asking and recently finished, not every job ever run; the map's node list stops growing with each fresh job; `bmesh align --check buildermesh` says `drifted .bmesh/routines/review.toml` after the example's prompt changes.

## Verification
The repo's verify. New tests: a fresh run deletes its node and keeps its events; `status` hides a day-old done node and `--all` shows it; `node forget` refuses a working node and removes a done one's consumer; the results bucket is provisioned with the configured age; `align --check` reports a drifted kind and `--kinds` rewrites it idempotently.

## Risks and the alternative considered
A result that expires between a lead's landing and its `bmesh done`: only if that gap is a month; the error names the missing result. Alternative: never delete, hide in the map only; rejected, `bmesh status` and `/api/nodes` are the operator's and the skills' view and they grow without bound. Alternative: a kind-level `keep` instead of one config default; rejected until a kind needs it.

## Needs from the operator
None.
