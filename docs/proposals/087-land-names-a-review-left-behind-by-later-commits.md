---
id: 087
title: Land names a review the branch's later commits left behind, instead of dropping it without a line
kind: refinement
size: S
judge: opus
status: building
lens: eng
proposed_by: buildermesh-fix-3
proposed_on: 2026-10-23
ruled: [root, failing-first, smallest, structure, no-new-surface]
fix_of: 072
from_feedback: buildermesh-build-13 (buildermesh-072) #2376
built_by: ca7e018d-762c-46c1-870b-33e77bca53ca
host: k16
family: buildermesh-fix-3
branch: bmesh/ca7e018d-762c-46c1-870b-33e77bca53ca
---

## Opportunity
The lead of spec 072 (Upgrading names the one hand restart that brings it in, sized M) had an Opus review pass on its commit `882e6c9`, then committed a change to the spec's own text (its Upgrading section). `bmesh land` printed `required: verify (the repo's verify kind)` and wrote `verified: verify ok ...` with no review entry and no line saying a review existed (feedback #2376).

Not requiring the review there is the design: the review is one pass, its findings are fixed in later commits and landed, and a second pass is only for code a rebase changed (lead skill, step 6 point 2; `routine queue review` refuses a second job without `--again`). What is wrong is that the drop is silent. `land`'s `prepare` (`cmd/bmesh/cmd_land.go`, the loop over `heads`) reads results only at HEAD's tree and at the heads a hand rebase rewrote (`rebasedFrom`), so a result on any earlier commit of the branch is never seen, and the `required:` line (manual, `bmesh land` step 4) cannot tell a branch that was reviewed from one that never was. That is the first wrong decision (`root`): the lead, the fix lead and the operator then read a verify-only line on an M spec with nothing to say whether a review happened.

## Proposal
In: `land` also reads the branch's own first-parent commits from the fork point (`git merge-base <base> HEAD`) up to HEAD. A kind with a result on one of them but none at HEAD's tree (and not required from a rewritten head) stays not required, as today, and is named after the kinds it requires, with its newest result there:

`required: verify (the repo's verify kind); left behind by later commits: review ok t:<tree8> at <sha8> (not on the verified line)`

A failing one reads `review fail t:<tree8> at <sha8>`, as a review that failed and was fixed. Nothing else changes: the required kinds, the carries, the `verified` line and `done` are as today. The manual's `bmesh land` step 4 gains the form, with one clause saying why it is not required (one pass, `--again` is for rebased code). The lead skill's "Its first line, `required: ...`, names each kind and where it came from" stays true and needs no new sentence (`smallest`).

Out:
- carrying a review across a later commit onto the `verified` line, or refusing the landing until it is re-run, the two the lead asked for. Carrying changes what `review ok` on the line asserts (a reviewer read that tree), and refusing reverses the operator's one-pass rule; both are product direction for the operator, not this fix;
- `spec verified` and `done`, which read the landed tree only.

Judge: opus, because the walk must not double-count the heads a hand rebase rewrote, which are not on the rebased branch's first-parent line.

## What you'll see
A lead whose review ran on an earlier commit sees land say so, `left behind by later commits: review ok t:3f2a1b0c at 882e6c94 (not on the verified line)`, and its final message and the operator's read of a verify-only line agree.

## Verification
Failing first, in `cmd/bmesh/cmd_land_test.go` over the existing bare-origin fixture: a branch with a recorded `review` ok at commit A and a later commit B that changes only its spec file. Today `land` prints `required: verify (the repo's verify kind)` and nothing about the review; after the fix the line ends `; left behind by later commits: review ok t:<A's tree8> at <A8> (not on the verified line)`, and the `verified` line is unchanged (verify only). The same with a `review` fail at A prints `review fail`. A branch whose review is at HEAD prints `review (t:<tree8> at HEAD)` as required, as today, with no left-behind clause, and the hand-rebase test still names `before the rebase at <sha8>` alone. The repo's verify commands pass.

## Risks and the alternative considered
- **The walk** is one `rev-list` over the branch's own commits and the result list `prepare` already holds, so it costs nothing measurable.
- **Alternative: a lead skill sentence** telling the lead to say in its final message that the review is behind HEAD. The skill already says a review's findings are fixed and landed; a second sentence every lead loads is what `structure` turns into the binary's line.

## Upgrading
nothing

## Needs from the operator
