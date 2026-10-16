---
id: 027
title: A review carries across a rebase that leaves the diff unchanged
kind: refinement
size: S
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
from_feedback: retrospective of 007-016 and 018 (buildermesh-008, -012, -016)
built_by: 29ae067a-1fd3-4374-9962-bddd11163c0c
host: k16
family: buildermesh-build-2
---

## Opportunity
Results are keyed by tree. A rebase onto a moved `main` makes a new tree even when the lead's own diff is byte-identical, so `spec verified --write` drops the review: 006, 007, 008 and 012 landed with `verify ok` alone, and 016 recorded a review by hand that no reviewer ran on that tree. The ledger understates honest reviews and invites invented ones.

## Proposal
In: a review result also records the patch-id of the diff it reviewed (`git patch-id --stable` of `base..HEAD`, base from the job text). `spec verified` accepts a review whose patch-id equals HEAD's (merge-base with `origin/main` to HEAD) and writes it as `review ok t:<tree8> m:<model> c:<reviewed tree8>`. Out: carrying across a rebase that changed the diff, even rotely; that is a new review (cheap with `--again`).

Judge: opus, because it changes what the ledger attests.

## What you'll see
A spec whose code was reviewed and then only rebased lands with both verify and review on its `verified` line.

## Verification
Verify, plus tests: same diff on a new base carries; a changed hunk doesn't; README's `verified` format updated.

## Risks and the alternative considered
Same patch, different surroundings can still break, but verify runs on the landed tree and catches that. The alternative, re-reviewing after every rebase, costs a model run per landing.

## Needs from the operator
None.
