---
id: 104
title: A watched lead's landing in the skill test carries its code, or the case stops before it
kind: maintenance
size: M
judge: opus
status: proposed
lens: eng
proposed_by: buildermesh-fix-8
proposed_on: 2026-10-24
from_feedback: buildermesh-fix-8 (buildermesh-103) #3351
---

## Opportunity
Proposed under premise: this would be the third fix on one belief. The belief: **the fake can stand in for another node's landing well enough that a session checking origin trusts it.** Spec 096 gave the drain case a watched landing that is a real commit (`watch-land`). Spec 103 then made every landing the fake prints real, because sessions that fetched found none on origin and went off course. Each fix held for what it targeted, and each time a session checked one layer deeper. In 103's skill test (pick, ideas and qa, three runs each, `cache/skilltest/103-cand2` to `103-cand4` in its worktree), one pick cell per run opened the watched leads' landings of 012 and 014. It found they set only each spec's status, with no code. It then built both specs itself and failed `no_other_commit` (feedback #3351 from 103's lead). The drain case's 013 landing is just as hollow. The cell then measures the fake, not the skill. A real lead's land carries its code, so how much of a landing the fake should fake is a choice about what the test measures, and it's the operator's call.

## Proposal
Choose one. Accepting this spec as written picks A; say B and it is rewritten before it is built.
- **A. The landing carries its code (recommended).** Each spec a case's watch lands (pick's 012 and 014, drain's 013) has its change kept in `scripts/skilltest/testdata/`, as the review case keeps `testdata/review/change/`. It is a working change with its test that passes the fixture's tests. The fake's watch commits it along with the `done` status. A session that opens the commit then finds what a real lead lands. Scoring already leaves the fake's commits out (103). Cost: three small reference changes to keep working as the fixture changes, with a rehearsal that their tests pass.
- **B. The case stops before the landing.** Pick's watch shows the leads started and `runner running`, then exits 5 (timed out), as the binary does. A re-armed watch prints the same. The cell is scored on the builds it started, which is what pick measures. Drain still needs 013 to land to reach its feedback, so drain keeps a landing and needs A for 013 anyway.

Out: the round's landed specs in the ideas case (spec files are the whole landing, so nothing is hollow there); any skill text.

Judge: opus, because the reference changes must be real code that a reviewing session can't tell from a lead's.

## What you'll see
In pick and drain cells, a session that runs `git show` on a lead's landing finds the spec's code and test there, and no session builds a spec it watched land.

## Verification
The repo's verify; the rehearsals of every case, including that each reference change's tests pass on the fixture. Failing first: a test that the fake's watch landing for 012 changes a file outside `docs/proposals/` (it changes none today). Then a skill test of pick and drain, one arm, `--runs 3`: no cell builds a spec its watch landed. Compare with 103's one pick cell in three.

## Risks and the alternative considered
- **The reference code drifts from the fixture.** The rehearsal runs its tests, so drift fails `go test ./scripts/skilltest`.
- **Alternative: accept the miss as noise.** One pick cell in three fails on the fake, which hides a real flip at `--runs 2` or `3`.

## Upgrading
nothing

## Needs from the operator
