---
id: 103
title: The skill test's fake lands what it says it landed, and scoring tells its commits from the session's
kind: maintenance
size: M
judge: opus
status: accepted
lens: eng
proposed_by: buildermesh-fix-8
proposed_on: 2026-10-24
ruled: [root, failing-first, smallest]
fix_of: 101
from_feedback: buildermesh-build-24 (buildermesh-101) #3325
---

## Opportunity
`scripts/skilltest/fake/bmesh` prints a landing for every ledger write and every watched landing, but only `land` and the drain case's `watch-land` put a commit on origin's `main`. `spec set --land` and `choose-ideas` print `landed <sha8> on main` with a made-up sha (`fakesha`), the pick case's `watch` says `shop-012 done 012 landed at 3d5f7a9b` and the ideas case's says round 1 is done with 015 to 017, and none of it is on origin. A session that checks, as the work skill tells it to (a fresh fetch before it reports a push), finds the tool lied and goes off course: in 101's skill test (`cache/skilltest/101-cand2` in its worktree) `pick` cand run 2 found `origin/main` still at the base, said "bmesh is reporting landings that don't exist", then built 012 and 014 itself with plain git (failing `no_other_commit`); `ideas` cand run 1 reported "none was saved and nothing was pushed" and spent two steers asking which items to build. Those cells measure the fake, not the skill (feedback #3325 from 101's lead). The first wrong decision is the fake's: it states a landing it didn't make.

## Proposal
In:
- **The fake lands what it prints.** Each of these becomes a real commit on origin's `main`, made as the binary or the other node would make it (a clone in `state/`, as `fake_watch` does today), and the line it prints names that commit's sha8:
  - `spec set --land`: the spec files' status set (and `--field`s written) on origin's `main`, `Spec NNN: <status>` / `Specs NNN, MMM: <status>` as today's line says.
  - `choose-ideas`: the statuses its output names (`NNN ...: proposed -> <status>`) set on origin's `main`, with `reason` for a rejection when given; its `landed` line names that commit.
  - `watch` with `watch-land`: several ids (pick's `012 014`), one commit each setting the spec `done`, each done line's `@LANDED@` its own; and a round's done (the ideas case): its pitch's ids written as `proposed` spec files on origin's `main` (frontmatter from the pitch line: title, kind, size, judge; `lens: eng`), as the round's thread lands them.
- **Scoring tells the fake's commits from the session's.** The fake keeps every commit it pushes for a ledger write or another node in a state file; `NewCommits`, `Changed` and `TurnFacts`' `PushedMain` leave those out, so `no_commit`, `landed`, `no_other_commit` and `rung_named` judge only the session's work. `land`'s commit stays the session's (it is the lead's own commit, reworded), as today.
- **Refs play the fake's landings** (a `Rehearsal` step that commits as the fake would), so the pick and ideas references still pass and the bad ones still fail the expectation they name.
- `scripts/skilltest/README.md`'s description of the fake and of scoring.

Out: the steer detector (fixed by the `Fix #3325` commit on `main`: `Waiting on you:` is no longer an ask); any skill text; the real binary.

Judge: opus, because which commits are the session's decides what every commit-reading expectation measures.

## What you'll see
In a pick or ideas cell, `git log origin/main` holds the ledger commits the fake's lines name; a session that checks finds them, and the cell's expectations still read only what the session did.

## Verification
The repo's verify. Failing first: tests through the fake in `scripts/skilltest` (as `skilltest_test.go` drives it today) that `spec set --land`, `choose-ideas` and pick's and ideas' `watch` each leave origin's `main` holding the change their output names, at the sha it prints; each fails on `main` before this. The rehearsals of every case pass. Then a skill test of `pick`, `ideas` and `qa`, one arm (`cand=.`), `--runs 3`: no cell's transcript says a landing the fake printed is missing from origin, with the run's results named in the closing commit.

## Risks and the alternative considered
- **The fake grows.** It already lands for `land` and `watch-land`; this widens the same clone-commit-push step to the other lines, one helper.
- **Alternative: the fake prints no sha and no landing.** The binary does print them, so the fake would stop standing for it, and a skill that reads them would be measured against output no real session sees.

## Upgrading
nothing

## Needs from the operator
