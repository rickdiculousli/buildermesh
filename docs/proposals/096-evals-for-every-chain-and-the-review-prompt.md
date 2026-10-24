---
id: 096
title: The skill test measures the review prompt on planted defects, runs each case twice, names the cases that flipped, and covers the fix lead and the feedback drain
kind: maintenance
size: M
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
scope_grew: [100]
---

## Opportunity
The article "Building agents: when to fold subagents into your main agent loop" (2026-10-22) asks for evals before changes: a task for every production chain and every rule, each run more than once, compared by which tasks flipped. `scripts/skilltest` covers the chat skills and a lead (13 cases), but:
- **The review prompt is unmeasured since it changed.** 9d0eaf8 (it reads past the diff) and f56a77b (no turn cap) landed 2026-10-24. Its only measure of what it catches is the 2026-10-15 run on the old prompt: six planted defects in a party-buzzer clone, four Opus and four Fable reviewers, all six caught (design 2026-10-14, Verification policy). That kit lives outside the repo (`~/.cache/bmesh-experiments/sizing/` on k16: `defects.md`, `score.py`).
- **`--runs` defaults to 1** (`scripts/skilltest/main.go:42`), while the README says "a pass rate over one run is a coin" and the next-series design made two runs a habit.
- **Nothing says which cases flipped** between arms; `summary.md` gives totals and misses.
- **Chains with no case:** the fix lead (`skills/fix`, daemon-started, lands fixes and specs without the operator) and the work skill's feedback drain at `watch-done`.

## Proposal
In:
- **Review cases.** A diff on the `shop` fixture with planted defects, one per class the 2026-10-15 run used where `shop` can carry it (a logic off-by-one, a regression whose callers are outside the diff, a removed guard, a quadratic loop), plus one real change. A cell runs a review session as the `review` routine does (the kind's prompt, `base: <sha>` job text, the kind's allowed commands), taking the prompt from the arm: the `examples/routines/review.toml` that `bmesh scaffold` seeds. Scored on which planted defects the reply names (file and function, or line), and on findings about the real change that aren't defects. The defect sheet sits beside the case so a reader can check the scoring.
- **`--runs 2` by default.**
- **Flips.** `summary.md` lists each case whose pass differs between arms, run by run, before the totals.
- **A fix-lead case**: the fix brief this tree renders, with the fake's `feedback list --repo shop` holding an XS bug with a `where`, a duplicate of it, one a spec on `main` already covers, and one sized M. Passes when a `Fix #<seq>: ...` commit on origin's `main` fixes the bug (a hidden test), the duplicate is ruled with it, the covered one is dropped, the M one lands as a spec carrying `from_feedback`, `feedback drain` names those sequences, and `done` carries the report.
- **A feedback-drain case** for the work skill: a family's `watch-done`, with `feedback list <family>` holding a one-cause XS issue, one naming bmesh's own repo, and one larger. Passes when the XS lands as `Quick fix from <family> #<seq>: ...`, the bmesh one stays pending, the larger lands as a proposed spec with `from_feedback`, and `feedback drain` names only the handled sequences.

Scope grew: 100 landed first and moved the drain's steps out of the work skill into `watch-done; next: bmesh feedback list <family>` and the three `next:` lines `feedback list <family>` ends with, so the drain case's watch ends with that `next:` and the fake's family list prints those lines on an arm whose manual documents them.

Out: changing the review prompt; cases for rounds and executors (097 and 099 each ship the case for the chain they change).

Judge: opus, because the planted defects must be ones a reviewer can only find by reading past the diff, and the scorers must not pass a vague reply.

## What you'll see
`go run ./scripts/skilltest --arm base=<ref> --arm cand=. --cases review` prints, per arm, which planted defects each run named; every comparison runs twice and opens with the cases that flipped.

## Verification
The repo's verify. `go test ./scripts/skilltest` plays each new case's reference states: a reply naming every defect passes, one naming none fails, a vague reply ("there may be edge cases") fails; the fix-lead and drain cases have good and bad end states as the other cases do. One real run, both arms at main's HEAD, `--cases review,fix,drain`: numbers in the closing commit.

## Risks and the alternative considered
- **Planted defects become a target** a future prompt is tuned to. Keep the sheet's classes broad, and add a class when a real review misses one.
- **Alternative: keep the party-buzzer kit as a script outside the repo.** It isn't the fixture every arm runs against, and another fleet can't run it.

## Upgrading
nothing

## Needs from the operator
