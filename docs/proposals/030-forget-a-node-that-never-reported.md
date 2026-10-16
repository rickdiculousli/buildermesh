---
id: 030
title: node forget takes a node that never reported and whose thread isn't running
kind: refinement
size: S
judge: opus
status: accepted
lens: eng
proposed_by: operator coordinator (Mac session, 2026-10-16)
proposed_on: 2026-10-16
from_feedback: buildermesh-build-1 (buildermesh-029)
---

## Opportunity
`bmesh node forget` deletes only a `done` or `blocked` node (009, `cmd/bmesh/cmd_node.go:61`). A node that was registered but never reported stays `idle`, and `bmesh blocked` refuses `idle -> blocked`. So the only way out is to impersonate the node with `started` and then `blocked` first. The 029 lead hit this while cleaning up a live check's throwaway demo lead (feedback 613). Phase 5's smoke test of the T3 0.1.0 migration starts another such lead, and so does any lead whose runner start succeeded but whose session died before its first event.

## Proposal
`node forget <path>` also accepts an `idle` node when both of these hold:
- No event in `bm-mesh` comes from the node itself (`From` is the node's path).
- Its runner, asked once through `Runner.Status` for `runner.thread`, reports `Known` and not `Running`. A runner that can't tell (tmux) or fails is a refusal: `refused: <path> is idle and its thread may be running; stop it first`.

The executor rule (every child `done` or `blocked`) and `--worktree`'s rules are unchanged. Other states keep refusing as today. The node's `runner.kind` must match this box's runner and its `host` must be this box. Otherwise it refuses with `run it on <host>`.

Out: forgetting a running thread; a `--force`.

Judge: opus, because the running-thread guard decides whether a live session gets orphaned.

## What you'll see
`bmesh node forget t3v2-smoke --worktree` right after a smoke lead whose thread has ended prints the node forgotten. The same command while its turn is still running refuses and says why.

## Verification
The repo's verify. New tests in `cmd/bmesh` with a fake runner:
- an idle node with no own events and a stopped thread is forgotten;
- one with its own event is refused;
- one whose thread is running is refused;
- one whose runner returns `Known: false` is refused;
- one on another host is refused.

## Risks and the alternative considered
A lead between its runner start and its first `started` is `idle` with no events. The `Status` check is what keeps it. Letting `blocked` accept `idle` was the alternative, but it would let any node skip `started`, and the mesh's states mean less then.

## Needs from the operator
