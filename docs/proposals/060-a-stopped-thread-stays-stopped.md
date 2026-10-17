---
id: 060
title: bmesh node stop stops a node's thread and keeps it stopped, and a thread interrupted in the runner is never woken again
kind: refinement
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-17
---

## Opportunity
On 2026-10-17 the operator stopped proposal round 3 (`buildermesh-round-3`, started 00:52Z) by interrupting both threads in k16's T3. The nodes stayed `working`. At 01:12Z the round read `silent`, and the daemon woke both threads. README Wakes says it wakes "a silent one whatever its mail, at most once per `wake_every`". By 01:16Z both threads were writing new specs again: the engineer "focus[ing] on" work around round 4's specs, and the UI/UX engineer chasing the round's own `silent` row. The operator interrupted them a second time, and the nodes still read `working`.

There was no way to clear them:
- **bmesh has no command that stops a thread.** The runner has `Stop` (`t3_thread_interrupt`), but nothing in the CLI calls it.
- **`bmesh hold` takes only leads:** `refused: buildermesh-round-3.eng is a engineer; only a lead is held`.
- **`node forget` refuses a `working` node.**
- **The coordinator couldn't reach k16's T3 from the Mac,** and reading k16's T3 sign-in to call it directly was rightly blocked.

The only way left was to `bmesh send` each thread a "STOP, mark yourself blocked" message. That message is unread mail, which wakes the thread again.

So a stop the operator makes in the runner is undone by the daemon within `wake_every`, and a stop through bmesh doesn't exist.

## Proposal
In:
- **`bmesh node stop <path> [--text "<why>"]`.** It stops the node's thread through its runner (`Stop`: T3's `t3_thread_interrupt`, claude's process, tmux's window), then writes the node `blocked` with `stopped by <who>: <why>` and publishes `blocked`.
  - For a round it stops each child first, and the round then settles `blocked` as it does today.
  - For a lead it stops the lead's executors first.
  - A node on another box is stopped there, the way `node forget --worktree` reaches another box. With `[hosts]` off, it is refused with `run it on <host>`.
  - An already `done` or `blocked` node prints `already <state>` and exits 0.
  - After a stop, `node forget` works as it does for any `blocked` node.
- **A stopped node stays stopped.**
  - The daemon never wakes a node whose `blocked` text starts `stopped by`, for a silence or for a report or reply.
  - Only a message sent with `bmesh send` wakes it. That is the documented way to resume one, and the wake text says `resumed by <who>`.
  - Silence marking skips it.
- **An interruption made in the runner counts as a stop.** When the runner reports the newest run of a node's thread ended by the user (T3: the newest run `interrupted` or `cancelled`, not `failed`), the daemon's poll marks the node `blocked` with `stopped in <runner> by the operator` and pushes it once. It never wakes the node for silence, so interrupting a thread in T3 is enough. A runner that can't tell (tmux, claude) keeps today's behaviour.
- **The skills say it.** "Stop X" in `build`, `ideate` and `mesh-status` is `bmesh node stop`, and the coordinator never sends a free-text STOP message.
- The README's Wakes, Silence, `node` and Runners sections name each of these.

Out: pausing and resuming a thread mid-turn; stopping routine instances, which `routine` already manages.

Judge: opus, because it changes what the daemon wakes, and a wrong rule either revives what the operator stopped or never wakes a lead that is waiting.

## What you'll see
`bmesh node stop buildermesh-round-3` prints `stopped buildermesh-round-3.eng`, `stopped buildermesh-round-3.ux` and `buildermesh-round-3 blocked`. `node forget buildermesh-round-3` then clears it. A thread interrupted in T3 shows `blocked: stopped in t3 by the operator` within a minute and stays that way.

## Verification
The repo's verify, plus:
- `cmd_node_test.go` on `meshtest.Start` with the t3 fake: `node stop` interrupts the thread, writes `blocked`, stops a round's children and a lead's executors, and is a no-op on a `done` node. `forget` succeeds afterwards.
- `internal/daemon` wake and silence tests: a `stopped by` node isn't woken for silence or for a report, and a `send` wakes it. A newest T3 run that was interrupted marks the node blocked once. A `failed` run does not.

## Risks and the alternative considered
- **T3's run status could name a user interruption and a crash the same way.** The runner reads the status T3 reports for an interrupt, and the t3 fake records which one it is, so a crash keeps today's waking.
- **Alternative: never wake a silent node, only one with mail.** That breaks the case silence-waking exists for: a lead whose session died mid-turn, which nothing else restarts.

## Needs from the operator
