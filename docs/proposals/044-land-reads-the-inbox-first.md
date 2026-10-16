---
id: 044
title: bmesh land stops while the lead has an unread message from the operator
kind: refinement
size: S
judge: opus
status: proposed
lens: eng
proposed_by: 56979e66-321a-467e-9cbc-a2165df787ff
proposed_on: 2026-10-16
from_feedback: buildermesh-build-4 (buildermesh-042)
---

## Opportunity
Spec 042's lead got an operator message (a two-review bar for landing) while it built. It ran `bmesh inbox` in the same shell command as `bmesh land`, so the message was read only as land pushed: 042 landed after one review and the second ran on `main` afterwards (both ok, this time) (feedback 1294). The lead skill says to read the inbox "at every checkpoint" (`skills/lead/SKILL.md:36`), but nothing makes landing one: `bmesh land` (`cmd/bmesh/cmd_land.go`) checks the branch, the spec's status and the results, never the node's inbox, though `mesh.Unread` (`internal/mesh/inbox.go:113`) already counts it. Landing is the one step that can't be taken back, and the operator's messages are the ones most likely to change what landing needs: a review bar, a scope cut, "hold".

## Proposal
In:
- **Land refuses on unread operator mail.** In `prepare`, before the rebase, land peeks the calling node's inbox (without acking it) and, when it holds a message sent by `operator` with `bmesh send`, refuses (exit 3): `refused: <n> unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh land again`. Reports, replies and messages from other nodes don't stop it; they are listed on one line (`unread: done from <path>; reply to job <id>`) so the lead sees them before the push. A node with no inbox (land run outside a node) skips the check.
- **The lead skill says it**: landing is a checkpoint, and `bmesh inbox` runs as its own command before `bmesh land`, never chained into it.
- README Landing names the refusal.

Out: blocking on other senders' mail (a late executor report must not stall a landing), and any `--force` (reading the inbox is the way past it).

Judge: opus, because land gains a refusal on mesh state and a wrong one blocks every landing.

## What you'll see
A lead whose inbox holds an unread operator message runs `bmesh land` and gets the refusal; after `bmesh inbox` the same command lands.

## Verification
The repo's verify. New `cmd_land_test.go` cases on `meshtest.Start`: an unread `send` from `operator` refuses with the count and leaves the message unread; after it is read, land proceeds; an unread executor report or routine reply lands and prints the `unread:` line; land with no node skips the check.

## Risks and the alternative considered
A message that arrives between the check and the push still lands unread; the window is the rebase and checks, seconds rather than a build. Alternative: have the daemon wake the lead on operator mail sooner (`wake_every`). That shortens the gap but can't stop a landing already under way, which is where 042 lost the message.

## Needs from the operator
