---
id: 068
title: A verify after a change outside the code is answered from the last verified tree, wherever it runs
kind: time-cost
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-17
built_by: eecffa7f-6926-4b41-9f91-b1052843421b
host: k16
family: buildermesh-build-13
branch: bmesh/eecffa7f-6926-4b41-9f91-b1052843421b
---

## Opportunity
Docs work comes in loops: rework the README, fix the manual, tighten the internals, commit, look again, commit. Over the last five days `main` took 82 commits that were neither ledger commits nor code (by the row's `code` list), 10 of them in one README series. Every one is a new tree, and a deterministic `verify` is answered only from a record for the exact tree (`runInline`, `cmd/bmesh/cmd_routine.go:687`). So each `bmesh routine run verify` in such a loop runs the whole suite, about 2 minutes on k16 (054 measured `cmd/bmesh` at 68 s and `internal/daemon` at 99 s, run in parallel) and longer on the Mac, over code that verify already passed.

061 carries an ok `verify` across a change outside the code, but only inside `land` and only from three trees: the last one verified in that run, the branch's tree before the rebase, and the tree the `required` line named (`cmd_land.go:655-660`). That leaves three gaps:
- A docs-only branch is never verified by those three trees. Its code is main's, which is verified, but `land` doesn't look there, so it runs the suite.
- A lead's docs commit after its verify isn't covered either. The pre-rebase tree is the unverified docs commit, so `land` misses and runs.
- `build`'s and `release`'s gate (`verifyMain`, `cmd_build_gate.go:143`) checks main's code head. When a landing ended in a docs commit, that code head is a rebased commit whose own tree no verify ran on, so the gate runs the suite again.

## Proposal
In:
- **One rule: a run of first-parent commits shares its code.** Take the commits from a tip down its first parents to its first-parent code head (`git rev-list --first-parent -1 <tip> -- <code>`, pathspecs as `CodePaths` gives them), at most 100. No commit in that run touches code relative to its first parent, so all their trees are `CodeSame`.
- **One lookup, in `selfupdate` or `mesh`.** For the `verify` kind (the kind `code` is written for), with no result at the target tree, take the newest commit in the run that has a result of its own under the same args. A record whose output leads with `carried from t:` is skipped, and the walk goes on to its source.
  - If that result is ok, it carries. If it is a fail, verify runs: a fail over identical code is a flake (054) or a sign that `code` misses a file the tests read, and an older ok must not hide it.
  - The carried record is stored for the target tree only if none exists yet (a create, not a put). It has its `Commit` and `By` set and its output led by `carried from t:<old8>: nothing under code changed`, as `carryCode` stores it today. `done` and `spec verified` match it like any record. A conflict reads as "a result exists".
- **Callers:**
  - `bmesh routine run verify`: target HEAD, run from HEAD. A branch with only docs since main reaches main's verified commits.
  - `land`: the lookup on the rebased tree, then each pre-rebase head's run (`l.before`), each confirmed with `CodeSame` against the rebased tree. `carryCode`'s three candidates come first, as today.
  - `verifyMain`, for `build` and `release`: target the code head, run from main's tip down to it.
  - `--rerun` skips the lookup. `.bmesh/**` is always code (`AlwaysCode`).
- **Docs and skills:**
  - The lead skill's verdict line (`skills/lead/SKILL.md:48`) names the `(carried from ...)` form.
  - The manual's `routine run` output, its repos.toml **Code** entry, the build gate and `release` say so.
  - The internals' results section says the carried record skips its own kind.

Out:
- every deterministic kind other than `verify`. `surface` reads exactly the art and CSS `code` leaves out, so its records stay exact-tree, and `routine queue` and the daemon worker don't change, since `verify` is inline and never queued;
- `result find`, which stays exact-tree;
- results keyed by a hash of the code paths, which would change every key and `verified` entry (L) for the same gain.

Judge: opus, because a carry that crosses a code change records an ok no verify saw, and `land`, `build` and `release` all trust it.

## What you'll see
- A README loop: each `bmesh routine run verify` answers in under a second with `verify ok t:<new8> (carried from t:<old8>: nothing under code changed)`, and `bmesh land` carries too.
- A docs-only spec lands without a single run.
- `build` after a docs-last landing answers from the carry.
- A `.go` change, or a fail at the nearest record, runs as before.

## Verification
The repo's verify, plus tests over a bare origin with a row that sets `code`:
- `cmd_routine_test.go`: `run` carries across docs-only commits, and from main's verified tip on a docs-only branch. A `.go` commit, a `.bmesh/routines/verify.toml` commit and `--rerun` each run. A nearer fail ahead of an older ok runs. A carried record is skipped for its source. A merge commit in the run is handled, and the walk is capped at 100. With no `code`, a spec file carries and `docs/proposals/README.md` doesn't.
- `cmd_land_test.go`: a branch whose last commit is docs-only, rebased over a ledger commit, lands on a carry from its verified parent.
- `cmd_build_test.go`: after a docs-last landing, `build` answers from the carry.
- A carry never overwrites a record that already exists. `done` and `spec verified --write` accept a carried entry.

## Risks and the alternative considered
- **A `code` list that misses a file the tests read** now carries on every run, not only in `land`. The nearest-record rule surfaces a fail there instead of hiding it. Skipping carried records means a widened `code` reaches back.
- **The walk costs** one `rev-list`, one `log` and one KV get per commit down to the nearest record (a get per key, not a list of every `verify` result, which grows with `keep_results`), against a 2-minute run.
- **Alternative: the lead skill says "commit the docs before you verify".** It is free, but a docs loop has no code to commit first, and review fixes come after the verify by design.

## Upgrading
nothing: no key or record shape changes. An older binary reads a carried record as an ordinary one and otherwise runs as before.

## Needs from the operator
