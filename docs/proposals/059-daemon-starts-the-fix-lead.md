---
id: 059
title: The daemon starts a repo's fix lead when feedback arrives, so no session has to
kind: time-cost
size: M
judge: opus
status: proposed
lens: eng
proposed_by: 3a280408-0b8a-43a8-951a-2545a4d777f3
proposed_on: 2026-10-17
---

## Opportunity
After 058, the trigger is still a session. A coordinator has to reach `watch-done` and run `bmesh fix`. Feedback from a build started by hand with `lead start`, from a coordinator that died, or from another repo's lead naming `--repo buildermesh` waits until someone notices the `feedback: N pending` line in `mesh-status`, for up to the stream's 30 days. The fix lead's own events also push like any lead's, so each fix lead's `done` would reach the phone. The operator asked for the loop to be fully automated. That means nothing about it should need the operator unless the rulings send it there.

## Proposal
Builds on 058; build it after 058 lands.

In:
- **A registry key**, `fixes = "auto"` (default `""`: a coordinator starts the fixer, as in 058), on a ship row. The row is the place for it because the registry is the one place every box reads for a repo.
- **The daemon starts it.** The daemon of the row's `host` acts, resolved as `build` resolves it. It notices a feedback message whose `repo` row says `auto`, or, on each minute's settle pass, any pending issue for such a repo. If no fixer of that repo is live (one neither `done` nor `blocked`), it runs `bmesh fix <repo>` as a subprocess. The decision is derived from what exists (pending messages and fixer nodes), never from what an earlier pass meant to do. So a daemon restart, a missed message or a fixer that ended with issues still pending all converge on one fixer, and two boxes never start two: `fix` refuses the second, and the start is claimed with a `Nats-Msg-Id`, as a push is. A `blocked` fixer is never restarted: its issues wait on the operator, and its block pushes as a lead's does.
- **Only the fixer reaches the operator.** Leads of a `<repo>-fix-<N>` family don't push `done`: the fixer reports them. Their `ask` and `blocked` go to the fixer, which answers or escalates under the rulings. The fixer's own `ask`, `blocked` and `done` push as a lead's do. A done push is titled `<repo> fixes: N fixed, M wait on you`, and its body puts the proposed specs first.
- **mesh-status** replaces `feedback: N pending in <family>` with `fixes: N pending for <repo> (<fixer> working)`, or `(starts within a minute)` on an `auto` row, or `(bmesh fix <repo>)` on another.
- **The build skill's step 7**, on an `auto` row, names the fixer the daemon started and does nothing else.

Out: pacing fixes by quota, and starting fixers on non-ship rows.

Judge: opus, because the start has to converge across restarts and boxes, and the push filtering changes what the operator hears.

## What you'll see
A lead in any batch leaves feedback before its `done`. Within a minute `mesh-status` shows `buildermesh-fix-2 working`. The phone stays quiet until `buildermesh fixes: 2 fixed, 1 waits on you`, and that one is a proposed spec for `choose-ideas`.

## Verification
The repo's verify, plus daemon tests on `meshtest.Start` with the fake runner:
- A feedback message for an `auto` row starts exactly one fixer, also with two daemons racing.
- A pending issue with no live fixer starts one on the next pass.
- A `blocked` fixer isn't restarted, and a row without the key starts nothing.
- A fix family's lead `done` doesn't push, while the fixer's done pushes with the title above.
- The new `mesh-status` line.

README: Config's `repos.toml` key, Feedback, Pushes, and the build skill's step 7.

## Risks and the alternative considered
A runaway loop, where fix leads keep leaving feedback that starts more fixers. 058's checkable rulings bound each chain at two, and the fixer's pass ends when its queue is empty. Alternative: a daemon timer that starts a fixer every few hours. Rejected: it fixes later and starts threads with nothing to do.

## Needs from the operator
- [k16] Set `fixes = "auto"` on the `buildermesh` row of `~/.config/bmesh/repos.toml`, then run `bmesh repos publish`.
