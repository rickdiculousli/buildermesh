---
id: 089
title: Levelling main never reads another process's FETCH_HEAD, so a daemon fetching at the same moment can't refuse a build or a tick
kind: refinement
size: S
judge: opus
status: building
lens: eng
proposed_by: t3-e20b6274
proposed_on: 2026-10-23
built_by: d14515c8-7268-494a-800e-5cf68e119b99
host: k16
family: buildermesh-build-20
branch: bmesh/d14515c8-7268-494a-800e-5cf68e119b99
---

## Opportunity
Twice on 2026-10-23, each a minute after a release, a command refused with `main in <repo> is not level with origin: git pull: fatal: Cannot fast-forward to multiple branches.`: `bmesh build buildermesh 075` on k16 (`~/repos/buildermesh` was level with `origin/main`), then `bmesh spec steps 073 --tick 1` on the Mac, run seconds after a plain `git pull` there had succeeded. Each passed when run again, and the error never reproduced by hand.

The first wrong decision is in `levelMain` (`cmd/bmesh/gitwork.go`): on main it runs `git pull --ff-only origin main`. `pull` fetches, then merges whatever `.git/FETCH_HEAD` lists for merge. The daemon fetches in the same checkout without a lock shared with `levelMain`: its self-update (`internal/selfupdate/selfupdate.go`, `fetch origin +refs/heads/main:... +refs/tags/v*:...`) writes every refspec it names into `FETCH_HEAD` as for-merge. When that fetch lands between `pull`'s fetch and its merge, the merge sees main plus every tag and refuses. A release is when the daemon fetches tags, so that is when it shows.

## Proposal
In: `levelMain` stops reading `FETCH_HEAD`: on main it runs `git fetch --quiet origin +refs/heads/main:refs/remotes/origin/main` then `git merge --quiet --ff-only refs/remotes/origin/main`, so the merge names a ref no other fetch rewrites. The refusals stay what they are (local commits origin lacks, a dirty checkout the merge would touch), with the same message. Every other place in bmesh that runs `git pull`, or merges `FETCH_HEAD`, gets the same change. Fetches that never merge (`selfupdate`, `spec`, `land`, `release`) pass `--no-write-fetch-head`, so none of them leaves a `FETCH_HEAD` behind for a person's own `git pull` either.

As built, every fetch bmesh runs goes through one helper, `gitfetch.Args`, which adds `--no-write-fetch-head` when `git version` reads 2.29 or newer: the four named above and also the sync level's, the routine checkout's, the lander's and `LevelMain`'s own. A rule that holds for every fetch is easier to keep than a list.

Out: a lock shared between the daemon and every command. Reading a named ref removes the sharing instead.

Judge: opus, because `levelMain` gates `build`, `choose-ideas`, `spec set`, `spec steps --tick` and `land`.

## What you'll see
A `build` or a tick run while a daemon takes a release lands, with no `Cannot fast-forward to multiple branches`.

## Verification
The repo's verify commands. A failing-first test through `levelMain` on a real repo with an origin: write a `FETCH_HEAD` that lists main and a tag as for-merge (what the self-update's fetch leaves) between the fetch and the merge, by running a second `git fetch` of main and a tag in the checkout from the test's fake git hook or by calling the two halves in order. Before the fix the level refuses with `multiple branches`; after it, main is level. A test that the non-merging fetches leave no `FETCH_HEAD`.

## Risks and the alternative considered
- `--no-write-fetch-head` needs git 2.29 (2020). Older git rejects the flag; the fix then keeps the flag off those fetches and relies on the named-ref merge alone, which is the part that removes the refusal.
- Alternative: retry the pull once on that message. It hides the race rather than removing it, and a third process can still land in the retry.

## Upgrading
nothing

## Needs from the operator
