---
id: 071
title: A model job whose claude never answered (no sign-in, a crash, a timeout) records nothing, instead of a review fail for the tree
kind: refinement
size: M
judge: opus
status: building
lens: eng
proposed_by: buildermesh-fix-2
proposed_on: 2026-10-17
ruled: [root, failing-first, smallest, no-new-surface]
fix_of: 070
from_feedback: buildermesh-build-10 (buildermesh-070) #2086
built_by: 74a4dbc2-c4b4-42a9-a9e0-9709f2c98ac3
host: k16
family: buildermesh-fix-2
---

## Opportunity
A pooled review job on ricks-macbook (`routine.review.ricks-macbook-opus-5-5-1`) failed in 44 ms with `Failed to authenticate: OAuth session expired` and was recorded as a review fail for the tree, as though a reviewer had read the diff and rejected it (feedback #2086, from the lead of spec 070). The lead re-ran it with `routine run review --here` on k16. A false fail record does damage: `bmesh spec verified` composes the line from it, and a review that "failed" and was then "fixed" leaves a verify-only line that `bmesh done` accepts, so a sign-in failure can let an M change land with no review.

The first wrong decision is in `internal/instance/model.go` `(*Model).Run`. Every outcome where claude produced no result (the run was killed or timed out, claude exited non-zero or didn't start, its output wasn't a result, or the result was `is_error`) comes back as `Reply{OK: false}`, the same reply a reviewer's `VERDICT: fail` gives. Both callers that record a fresh reply then store it as the tree's result: the daemon's `handle` in `internal/daemon/routines_job.go`, and `runInline` in `cmd/bmesh/cmd_routine.go` (`routine run --here`). A failed setup already follows the opposite rule there, answered `ok=false` with `; nothing recorded` (spec 070); a model run that never answered is the same kind of infrastructure error.

## Proposal
In: `instance.Reply` says whether the run judged the job (an internal Go field, so `no-new-surface` holds). A command kind's reply always judges, since its exit code is the verdict. A model kind's reply judges only when claude returned a parsed result with no `is_error`: a reply with a verdict, or without one, is judged as today (R18). Every other model outcome is unjudged. Neither caller records an unjudged reply:

- the daemon answers the job `ok=false` with `model run failed: <why>; nothing recorded`, and its `job <id> fail <ms>` progress event is unchanged;
- `routine run --here` refuses with exit 3 and the same message, as it does for a failed setup.

`<why>` is the first non-empty line of the reply's output, e.g. `Failed to authenticate: OAuth session expired`, `[bad claude output]`, or the timeout note. The session reset that `Run` already does for unparsed output is unchanged. Fix both callers (`root`): they are the only places a fresh reply is recorded (`land`'s `PutResult` calls re-store existing results).

Out: steering the job to another box of the pool, or stopping an instance that can't sign in. Either one is new daemon behaviour, and an unrecorded `ok=false` already lets the requester queue again. Command kinds are unchanged.

Judge: opus, because the line between a judged and an unjudged reply decides what becomes a recorded verdict.

## What you'll see
A review job on a box whose claude can't sign in comes back as `model run failed: Failed to authenticate: OAuth session expired; nothing recorded`, `bmesh result` shows no result for that tree, and a re-queued review records its real verdict.

## Verification
Failing first: in `internal/daemon/routines_test.go`, a pooled model job run against `testdata/fake-claude.sh` with `FAKE_IS_ERROR=1 FAKE_EXIT=1 FAKE_RESULT='Failed to authenticate: OAuth session expired'` today leaves a failed result under the job's key. After the fix, no result is stored and the reply reads `model run failed: Failed to authenticate: OAuth session expired; nothing recorded`. The same holds for `FAKE_NOJSON=1` and a `FAKE_SLEEP` past the kind's timeout. In `cmd/bmesh/cmd_routine_run_test.go`, `routine run review --here` with the same fake exits 3 with that message and records nothing, where today it records `review fail`. A reply with `VERDICT: fail`, and one with no verdict when the prompt asks for one, are still recorded as fails. `internal/instance/model_test.go` checks the judged flag for each case its `Run` table covers. The manual's daemon answer list (Running) and the `routine run` refusal table gain the line, and internals' result-recording section says an unjudged model reply is never stored.

## Upgrading
nothing: no config, registry or record changes. A daemon on the previous release still records a model run that never answered as a fail, so upgrade the boxes that run model kinds' jobs to stop those false fails.
