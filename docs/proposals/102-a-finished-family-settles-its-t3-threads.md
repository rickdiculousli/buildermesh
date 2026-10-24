---
id: 102
title: When every lead of a family is finished, the daemon settles their T3 threads, so only the coordinators stay on top
kind: maintenance
size: M
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity
Every lead, fix lead and round that bmesh starts on the T3 runner is a T3 thread. When a family finishes, its threads stay active in T3's thread list beside the coordinator threads, and the operator settles them by hand, one at a time. Family buildermesh-build-23 left five lead threads to settle, and every batch does the same. T3 has the action already (MCP `t3_thread_organize`, action `settle`), and the daemon already talks to T3 over MCP (`internal/runner/t3v2.go`: launch, read, send, interrupt).

## Proposal
In:
- **The daemon settles a family's threads once the family is finished**: every lead in it is `done` or stopped, and each lead's closing turn has ended (its runner reads idle after the closing-line wake). It settles each of those leads' T3 threads with `t3_thread_organize` action `settle`. A family with a lead that is blocked, held, asking, silent or still landing is not finished, so nothing in it is settled: those are the threads the operator still has to read.
- **A round settles its threads** when the round itself settles (the daemon's `SettleRound`), and a fix lead's thread when it is `done`, under the same rule.
- **Only on the T3 runner**: a node whose runner is not T3 (tmux, claude) is skipped. A T3 that cannot be reached, or that refuses the settle, is logged once per thread and tried again on the daemon's next pass; it never blocks anything. Settling is safe to repeat: a thread already settled stays settled.
- **The operator can still see what happened**: `bmesh mesh-status` and `watch` print one `settled <family>: <n> threads` line when a family's threads are settled.
- **Coordinator threads are never touched**: bmesh settles only the threads it started (the `thread` recorded on its own nodes).
- The runner fake (`t3fake`) answers `t3_thread_organize`, and a daemon test settles a finished family, and leaves alone a family with one blocked lead and a node on the tmux runner.
- `docs/manual.md` and `docs/internals.md` say when a thread is settled.

Out: settling, archiving or deleting a coordinator's thread; settling any thread of a family that isn't finished; a flag or config key to turn it off (a settled thread is one click to unsettle in T3, and a new message wakes it).

Judge: opus, because it adds a state rule to the daemon across the runner, the mesh and the docs.

## What you'll see
When the last lead of a family lands, its lead threads drop out of T3's active list by themselves, and the coordinator thread that started them is the one left on top. `bmesh watch` prints `settled buildermesh-build-23: 5 threads`.

## Verification
The repo's verify commands. The new daemon test drives `t3fake`: a finished family's lead threads get one settle each, and a second pass changes nothing; a family with one blocked lead and a family on the tmux runner get no settle. On k16, the next family that finishes shows its lead threads settled in T3.

## Risks and the alternative considered
A lead the operator still wants to read gets settled. The rule settles nothing while any lead asks, blocks or is held, and a settled thread stays one click away. Considered: settling each lead as soon as it is done. Rejected because the operator asked for the family as the unit, and the threads of a family that is still landing are what they watch.

## Upgrading
nothing
