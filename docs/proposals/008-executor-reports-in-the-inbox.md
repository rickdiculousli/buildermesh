---
id: 008
title: An executor's done and blocked reach the lead's inbox; the wake says what waits
kind: refinement
size: S
judge: opus
status: proposed
lens: eng
proposed_by: fd21a373-851c-4e68-beb6-831cb4be0fa9
proposed_on: 2026-10-15
---

## Opportunity
The mesh relays one executor event to its lead: `ask` becomes the inbox message `ask from <path>: <text>` (`cmd/bmesh/cmd_events.go:77`). `done` and `blocked` are an event and a state only. So the lead skill (§3) has every executor hand-type its own report, `bmesh send <lead> "branch <b> at <sha>: <one line>"`, before `bmesh done --commit <sha>`: two commands that must agree, written from memory by a subagent, and a `done` without the `send` leaves the lead polling `executor list`. The daemon's wake text (`internal/daemon/daemon.go:56`) is `bmesh: you have N unread messages. Run `bmesh inbox` now, then continue.`: a count, not what is waiting, so a woken lead cannot tell a finished executor from a reply to a job it is still waiting on, or from the operator's answer to its ask. The operator's focus for this round (inbox, 2026-10-15) asks first for actionable messages between nodes.

## Proposal
In:
- An executor's `done --commit SHA [--text T]` and `blocked --text T` send the parent one inbox message each, as `ask` does: id `done-<path>-<sha>` or `blocked-<path>-<hash of text>` (a retry is one message), kind `report`, text `done from <path>: branch <b> at <sha8>: <T>` (the branch read from the executor's worktree as `executor list` does) or `blocked from <path>: <T>`. `--json` on `inbox` carries `{"event","from","branch","commit"}` in a `report` field.
- The wake text names what waits, from a peek of the inbox (what `inbox --peek` reads): `bmesh: 3 unread: done from app-012.alpha (branch t3code/x-alpha at 1a2b3c4d); reply to job 9f2k… (review fail); message from operator. Run `bmesh inbox` now, then continue.` Up to five items, then `and N more`; a count alone when the peek fails.
- The lead skill §3 and the executor skill: the report is `bmesh done --commit <sha> --text "<one line>"`; the separate `send` goes. The executor header (`executor new`) says the same.
- README Executors and Pushes paragraphs.
Out: pushing executors' events to the operator (unchanged: they never push); the digest's lines (already nest executors under their lead).

Judge: opus, because the message shapes are pinned here and the change spans the CLI, the daemon and two skills.

## What you'll see
A lead's `bmesh inbox` after an executor finishes shows `done from app-012.alpha: branch t3code/x-alpha at 1a2b3c4d: parser and its tests`, whether or not the subagent said anything else. A wake arrives as `bmesh: 2 unread: done from …; reply to job …`, and the lead knows what to do before running a command.

## Verification
The repo's verify. New tests: executor `done` and `blocked` put one message in the parent's inbox and a retry does not add a second; a lead's `done` sends nothing; `WakeText` from a peeked inbox lists the three message kinds and caps at five; the executor header names `done --text`.

## Risks and the alternative considered
A peek before every wake is one more inbox read per wake (at most one per `wake_every`); with 007 it is instant. Alternative: keep the hand-typed `send` and have `executor list` be the lead's source; rejected, the lead reads its inbox at every checkpoint already and the mesh knows the branch and commit better than the subagent does. Alternative: relay `progress` too; rejected, it would flood the inbox with lines the lead does not act on.

## Needs from the operator
None.
