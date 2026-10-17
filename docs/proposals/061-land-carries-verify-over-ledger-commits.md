---
id: 061
title: bmesh land carries a verify across a rebase that moved only the ledger, so a coordinator's spec commits don't cost a lead a re-verify
kind: time-cost
size: S
judge: opus
status: proposed
lens: eng
proposed_by: dfdc9673-27b2-4f3f-9f17-95a5b436de66
proposed_on: 2026-10-17
from_feedback: buildermesh-build-8 (buildermesh-055)
---

## Opportunity
Landing the upgrade-notes spec (055) took `bmesh land` three runs. Each run fetched, rebased and re-ran `verify` (about 2 minutes), and both times the coordinator's ledger-only pushes (`Review`, `Propose` and `Dispatch` commits, which touch only `docs/proposals/`) moved `main` while the verify ran. The push was refused, `main moved; again from the fetch`, and the second move refused the landing outright. The code under test was the same every time. Only the tree hash differed, because the spec directory is part of the tree, and results are recorded by tree.

`build` and `release` already treat the ledger this way: both check main's code head, the newest commit that changes anything outside the spec directory, "so the `Dispatch`, `Review` and `done` ledger commits never ask for a verify of their own" (README, build). `land` is the one place that still pays for them, and it pays most when a coordinator is busiest.

## Proposal
In:
- **Land step 4 carries a deterministic kind across a ledger-only move.** When a required deterministic kind (`verify`) has no result for the rebased tree, `land` looks for an ok result for the tree it last verified in this run (or the branch's tree before the rebase). It carries that result when the only difference between the two trees is in the spec directory (`[spec] glob`'s directory, as `codeHead` reads it) and the changed spec files parse as the ledger. It stores the result again for the new tree, so `done` and `spec verified` match it as they match any recorded result. The line reads `verify ok t:<new8> (carried from t:<old8>: the rebase moved only the ledger)`.
- **The push retry uses it.** When `main moved; again from the fetch` finds only ledger commits since the last fetch, the second pass carries verify and doesn't re-run it, so a landing that raced a coordinator goes through on the retry instead of being refused.
- A difference anywhere else, or a ledger file that doesn't parse, re-runs the kind as today.
- The README's land step 4 and step 7 say it.

Out: carrying a review (it already carries by patch-id); carrying over any change outside the spec directory; a lock that keeps a coordinator from pushing during a landing.

Judge: opus, because a wrong carry lands untested code with a green `verified` line.

## What you'll see
A lead whose landing races a coordinator's `Review` commit prints `main moved; again from the fetch`, then `verify ok t:<new8> (carried from t:<old8>: the rebase moved only the ledger)`, and lands in seconds instead of re-running verify or being refused.

## Verification
The repo's verify, plus `cmd_land_test.go` over a bare origin:
- a ledger-only commit pushed to `main` between the fetch and the push is carried, and the push lands on the retry, with the stored result for the new tree;
- a commit that also touches a code file re-runs verify;
- a ledger file that doesn't parse re-runs verify (and fails it where the repo's tests read the ledger);
- `done` accepts the carried entry.

## Risks and the alternative considered
- **A repo whose verify reads the ledger.** bmesh's own tests could parse `docs/proposals/`. The parse check covers the ledger's own format. A repo whose tests assert more about its specs than that they parse would land on a carried verify: README says carrying is by spec directory only, and such a repo moves its checks out of verify into a kind of its own.
- **Alternative: hold coordinator pushes while a land lease is held.** That serializes every ledger write behind a 2-minute verify and makes `choose-ideas` wait on landings. The code-head rule `build` and `release` already use is the consistent one.

## Upgrading
nothing

## Needs from the operator
