---
id: 068
title: A verify after a change outside the code is answered from the last verified tree, wherever it runs
kind: time-cost
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-17
---

## Opportunity
061 taught `land` to carry an ok `verify` across a change outside the row's `code`, but only from two trees: the branch's tree before the rebase, and the last tree verified in that run. Everywhere else a deterministic kind is answered only from a record for the exact tree:
- `bmesh routine run` (`runInline`, `cmd/bmesh/cmd_routine.go:687`);
- `bmesh routine queue` (`cmd_routine.go:346`);
- the daemon's routine worker (`internal/daemon/routines_job.go:315`).

A lead's last commits are usually outside the code. After verify and review it updates `docs/manual.md`, `docs/internals.md`, the README and the spec's own frontmatter, as AGENTS.md requires in the same change. Each such commit is a new tree, so `bmesh routine run verify` runs the whole suite again, about 2 minutes on k16 (054 measured `cmd/bmesh` at 68 s and `internal/daemon` at 99 s, run in parallel) and longer on the Mac. The code under test is unchanged. `land` then misses as well: the branch's pre-rebase tree is the unverified docs commit, so `land`'s carry finds nothing and runs verify once more. Over the last four days, 294 of the 553 commits on `main` touched nothing under bmesh's `code` list.

## Proposal
In:
- **One lookup, `mesh`-side or `selfupdate`-side, used by all four callers.** For a deterministic kind with no result at `commit`'s tree, it walks `commit`'s first-parent ancestors back to the commit's code head (`selfupdate.CodeHead` with the row's `code`), at most 100 commits. It takes the newest one whose tree has an ok result under the same kind, args and model, and confirms it with `CodeSame(that tree, commit's tree)`. Every commit after the code head leaves the code untouched, so the walk ends there.
- **A carried result is stored for the new tree** with its `Commit` and `By` set, and its output prefixed with `carried from t:<old8>: nothing under code changed`, as `carryCode` stores it today. `done`, `spec verified` and later lookups then match it like any recorded result. The verdict line reads `verify ok t:<new8> (carried from t:<old8>: nothing under code changed)`.
- **`runInline`, `routine queue`, the daemon worker and `land` all call it.** `land`'s `carryCode` becomes this lookup, with its two extra candidates (`l.ran`, the pre-rebase tree) tried first.
- **Only an ok result carries.** A fail at an ancestor runs the kind again, so a timing flake (054) clears on the next commit and isn't copied forward.
- **`--rerun` skips the carry** as it skips the record today. A change to `.bmesh/routines/*.toml` is always code (`AlwaysCode`), so a changed verify kind runs.
- The manual's routine sections (`run`, `queue`), `land` step 4 and the internals' results section say so.

Out:
- keying results by a hash of the code paths instead of the whole tree. That would change every key and every `verified` entry (`t:<tree8>`), which is an L-sized change for the same gain;
- carrying model kinds (review), which already carry by patch-id;
- searching beyond first-parent ancestry, such as a sibling branch with the same code.

Judge: opus, because a carry that crosses a code change records an ok that no verify saw, and `land`, `build` and `release` trust it.

## What you'll see
A lead commits a manual edit after its verify and runs `bmesh routine run verify`. It answers in under a second with `verify ok t:<new8> (carried from t:<old8>: nothing under code changed)`. `bmesh land` on that branch, rebased over ledger commits, carries again and lands without a run. A commit that touches a `.go` file runs the suite as before.

## Verification
The repo's verify, plus tests over a bare origin with a row that sets `code`:
- `cmd_routine_test.go`: `run` and `queue` carry across a docs-only commit; across a `.go` commit, or one to `.bmesh/routines/verify.toml`, they run; `--rerun` runs; a fail at the ancestor runs; with no `code`, a spec file carries and `docs/proposals/README.md` doesn't.
- `internal/daemon`: a queued job on a docs-only commit gets a carried reply with a `ResultKey` that `routine wait` reads, and no checkout or slot is taken.
- `cmd_land_test.go`: a branch whose last commit is docs-only and unverified lands on a carry from its verified parent.
- `done` and `spec verified --write` accept a carried entry.

## Risks and the alternative considered
- **A `code` list that misses a file the tests read** carries a stale verify more often now, since every run trusts the list and not only `land`. It is the same list 061 already trusts for `land`, `build`, `release` and `update`, and a row without `code` only ever skips spec files.
- **The walk costs** one `rev-list` and up to 100 result reads, each a few milliseconds, instead of a 2-minute run.
- **Alternative: the lead skill says "commit the docs before you verify".** That is free, but review fixes and blast-radius edits come after the verify by design, so every lead would have to break the rule.

## Upgrading
nothing: no key or record shape changes. A box still on the previous release answers only exact-tree records, as before.

## Needs from the operator
