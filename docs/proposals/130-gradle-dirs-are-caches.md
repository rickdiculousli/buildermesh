---
id: 130
title: executor remove and repos remove also leave a Gradle project's own build dirs, beside the build script that makes them, out of their ignored-files refusal
kind: refinement
size: M
judge: opus
status: proposed
lens: eng
proposed_by: buildermesh-fix-22
proposed_on: 2026-11-24
from_feedback: bluke-build-1 (bluke-002) #5673, bluke-build-1 (bluke-003) #5943, bluke-build-1 (bluke-005) #6069, bluke-build-1 (bluke-007) #6292
---

## Opportunity
Proposed under `reversible`: it overturns 116's ruled-out line ("guessing caches by name ... an untagged dir is still refused") and widens what bmesh deletes unseen, which is the operator's call, not a fix lead's.

116 (`executor remove` and `repos remove` leave dirs tagged with `CACHEDIR.TAG` out of their ignored-files refusal) holds for Cargo, which tags `target/`. Gradle tags nothing in a project: in the Android repo Bluke on k16 (`~/repos/Bluke`) the ignored files are 2026 under `app/build/`, 37 under `.gradle/` and 3 under `build/`, with no `CACHEDIR.TAG` among them; `.gradle/configuration-cache/` names its files by hash, so an executor's never match its lead's. Four of Bluke's leads (002, 003, 005, 007) reported that `bmesh executor remove` refused every executor on those files and that each passed `--force`, which also drops the protection for an executor's `cache/` evidence the refusal is there for.

## Proposal
In:
- **`gitwork.CacheDir` also knows Gradle's own dirs**, by the build script that owns them rather than by name alone: a `build/` dir beside a `build.gradle` or `build.gradle.kts`, and a `.gradle/` or `.kotlin/` dir beside a `settings.gradle` or `settings.gradle.kts`, each script a regular file in the same worktree. A dir so placed counts as a cache exactly as a tagged one does today, in both callers (`ignoredOnlyIn` in `cmd/bmesh/cmd_executor.go` and `ignored` in `internal/nix/nix.go`); one table of (dir name, owning script names) holds the rule.
- The manual's `executor remove` (What it does and its refusal row) and the nix job's ignored-files line name the Gradle dirs beside the tagged ones.

Out: any config or flag naming exempt dirs (`no-new-surface`); a `build/` or `.gradle/` with no Gradle script beside it, which is still refused; `local.properties` and other ignored files Gradle doesn't write.

Judge: opus, because both callers must keep refusing what they refuse today outside a Gradle project's own dirs.

## What you'll see
A lead in Bluke runs `bmesh executor remove <name>` on a merged executor whose worktree holds `.gradle/configuration-cache/...` and `app/build/...` files its own doesn't, and gets `removed <path>` without `--force`. An executor's `cache/RESULT.txt` beside them is still refused.

## Verification
The repo's verify. Failing first, each failing before the change:
- In `TestExecutorRemoveRefusesIgnoredFilesOnlyItHas` (`cmd/bmesh/cmd_executor_test.go`), the executor's worktree also gets a tracked `settings.gradle.kts` and `app/build.gradle.kts` with ignored `.gradle/configuration-cache/x` and `app/build/out` the lead lacks: the refusal names only `cache/RESULT.txt`, and once the lead has that file the remove succeeds without `--force`. An ignored `build/out` with no `build.gradle*` beside it is still refused.
- In `internal/nix/nix_test.go`, a checkout whose only ignored files are in `.gradle/` beside its `settings.gradle.kts` is not refused without `ignored-ok`.
- On the real thing: built from the branch, `ignoredOnlyIn` on a Bluke worktree on k16 against `~/repos/Bluke` names no `.gradle/`, `build/` or `app/build/` file (a test-only run; nothing removed).

## Shown

## Risks and the alternative considered
- **A `build/` beside a Gradle script holds a check's output.** Gradle owns that dir and `gradle clean` deletes it; a check's evidence belongs in `cache/`, which stays refused.
- **Alternative: Bluke's build writes a `CACHEDIR.TAG` into its dirs.** It works with no bmesh change, but every Gradle repo would have to learn and keep it.

## Upgrading
nothing

## Needs from the operator
Accept it (`status: accepted`) to have it built, or reject it to keep 116's rule that only a tagged dir is a cache.
