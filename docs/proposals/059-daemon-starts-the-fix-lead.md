---
id: 059
title: The daemon starts bmesh's own fix lead when feedback on bmesh arrives, so no session has to
kind: time-cost
size: M
judge: opus
status: building
lens: eng
proposed_by: 3a280408-0b8a-43a8-951a-2545a4d777f3
proposed_on: 2026-10-17
built_by: 150d6be8-06eb-4668-8d86-66be9fb099f5
host: k16
family: buildermesh-build-9
branch: bmesh/150d6be8-06eb-4668-8d86-66be9fb099f5
---

## Opportunity
After 058, the trigger is still a session. A coordinator has to reach `watch-done` and run `bmesh fix`. Feedback from a build started by hand with `lead start`, from a coordinator that died, or from another repo's lead naming `--repo buildermesh` waits until someone notices the `feedback: N pending` line in `mesh-status`, for up to the stream's 30 days. The fix lead's own events also push like any lead's, so each fix lead's `done` would reach the phone. The operator asked for the loop to be fully automated. That means nothing about it should need the operator unless the rulings send it there.

## Proposal
Builds on 058; build it after 058 lands.

In:
- **Only for bmesh's own repo**, the row 058's `fix` runs on. There is no key: a mesh whose registry has no such row never starts a fixer, and every other repo's feedback stays with the coordinator.
- **The daemon starts it.** The daemon of the row's `host` acts, resolved as `build` resolves it. It notices a feedback message naming that repo, or, on each minute's settle pass, any pending issue for it. If no fixer is live (one neither `done` nor `blocked`), it runs `bmesh fix` as a subprocess. The decision is derived from what exists (pending messages and fixer nodes), never from what an earlier pass meant to do. So a daemon restart, a missed message or a fixer that ended with issues still pending all converge on one fixer, and two boxes never start two: `fix` refuses the second, and the start is claimed with a `Nats-Msg-Id`, as a push is. A `blocked` fixer is never restarted: its issues wait on the operator, and its block pushes as a lead's does.
- **Only the fixer reaches the operator.** Leads of a `<repo>-fix-<N>` family don't push `done`: the fixer reports them. Their `ask` and `blocked` go to the fixer, which answers or escalates under the rulings. The fixer's own `ask`, `blocked` and `done` push as a lead's do. A done push is titled `<repo> fixes: N fixed, M wait on you`, and its body puts the proposed specs first.
- **mesh-status** shows bmesh's own pending feedback as `fixes: N pending for <repo> (<fixer> working)`, or `(starts within a minute)`. Other repos keep `feedback: N pending in <family>`.
- **The build skill's step 7** names the fixer the daemon started for bmesh's issues, and triages other repos' issues as today.

Out: fixers for any other repo, and pacing fixes by quota.

Judge: opus, because the start has to converge across restarts and boxes, and the push filtering changes what the operator hears.

## What you'll see
A lead in any batch leaves feedback before its `done`. Within a minute `mesh-status` shows `buildermesh-fix-2 working`. The phone stays quiet until `buildermesh fixes: 2 fixed, 1 waits on you`, and that one is a proposed spec for `choose-ideas`.

## Verification
The repo's verify, plus daemon tests on `meshtest.Start` with the fake runner:
- A feedback message naming bmesh's repo starts exactly one fixer, also with two daemons racing.
- A pending issue with no live fixer starts one on the next pass.
- A `blocked` fixer isn't restarted, and feedback naming another repo, or a registry with no own row, starts nothing.
- A fix family's lead `done` doesn't push, while the fixer's done pushes with the title above.
- The new `mesh-status` line.

README: Feedback, Pushes, and the build skill's step 7.

## Risks and the alternative considered
A runaway loop, where fix leads keep leaving feedback that starts more fixers. 058's checkable rulings bound each chain at two, and the fixer's pass ends when its queue is empty. Alternative: a daemon timer that starts a fixer every few hours. Rejected: it fixes later and starts threads with nothing to do.

## Needs from the operator
