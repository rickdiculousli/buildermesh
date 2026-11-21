---
id: 116
title: executor remove and repos remove leave tagged cache dirs, like Cargo's target, out of their ignored-files refusal
kind: refinement
size: M
judge: opus
status: accepted
lens: eng
proposed_by: buildermesh-fix-21
proposed_on: 2026-11-20
ruled: [root, failing-first, smallest, no-new-surface]
from_feedback: eggnogg-rework-build-1 (eggnogg-rework-026) #5360
---

## Opportunity
`bmesh executor remove` refuses when the executor's worktree holds ignored files the lead's worktree has no file at (`cmd/bmesh/cmd_executor.go`, `ignoredOnlyIn`), so a check's script or output isn't deleted unseen. In a Rust repo every executor's worktree has a `rust/target/` of build output whose file names carry hashes the lead's own `target/` doesn't share, so the refusal fires on every remove and the lead passes `--force` each time (feedback #5360 from eggnogg-rework's 026 lead), which also drops the protection for the evidence the check is there for. A real executor worktree on k16 (`~/.bmesh/worktrees/eggnogg-rework/eggnogg-rework-round-1-eng`) has 8208 ignored files, nearly all under `rust/target/`. The first wrong decision is treating every ignored file as possibly the executor's work: a tool's build cache is regenerable, and Cargo marks its `target/` as one with a `CACHEDIR.TAG` file (the Cache Directory Tagging standard, <https://bford.info/cachedir/>: a file named `CACHEDIR.TAG` whose content begins `Signature: 8a477f597d28d172789f06886806bc55`). The same fault is in the nix job's ignored-files refusal (`internal/nix/nix.go`, `ignored`), which `bmesh repos remove --checkouts` sends: a checkout or linked worktree with a tagged `target/` needs `--ignored-ok` for it alone.

## Proposal
In:
- **One helper** in `internal/gitwork`: given a root and a path relative to it, it returns the outermost directory on that path (the path itself included, when it is a directory) holding a `CACHEDIR.TAG` whose first bytes are the standard signature, or `""`. A tag without the signature does not count, as the standard says.
- **`executor remove`**: `ignoredOnlyIn` skips an ignored file inside a tagged dir before comparing it with the lead's worktree. `git ls-files` lists files sorted, so once a tagged dir is found the files under its prefix are skipped without looking again.
- **The nix job**: `ignored` drops a `!!` entry inside or naming a tagged dir (`git status --ignored` lists `rust/target/` collapsed). Its refusal and `ignored-ok` are otherwise unchanged.
- The manual: `bmesh executor remove`'s What it does and its refusal row, and the nix job's ignored-files line, say that a dir tagged with `CACHEDIR.TAG` is not counted and goes with the worktree.

Out: any config or flag naming exempt dirs (`no-new-surface`); guessing caches by name (`target`, `__pycache__`, `node_modules`): an untagged dir is still refused.

Judge: opus, because both callers must keep refusing what they refuse today outside a tagged dir.

## What you'll see
A lead in a Rust repo runs `bmesh executor remove alpha` on a merged executor whose worktree has a `rust/target/` and gets `removed <path>`, with no `--force`. An executor's `cache/RESULT.txt` beside it is still refused, as today. `bmesh repos remove <name> --checkouts` no longer needs `--ignored-ok` for a checkout whose only ignored files are in a tagged cache dir.

## Verification
The repo's verify. Failing first, each failing before the change:
- In the existing `executor remove` ignored-files test (`cmd/bmesh/cmd_executor_test.go`), the executor's worktree also gets an ignored `target/` holding a `CACHEDIR.TAG` with the signature and a file the lead lacks: the refusal still names only `cache/RESULT.txt`. Then, with `cache/RESULT.txt` also present in the lead's worktree, `executor remove` succeeds without `--force` and the worktree is gone. A `target/` whose `CACHEDIR.TAG` lacks the signature is still refused.
- In `internal/nix/nix_test.go`, a checkout whose only ignored files sit in a tagged dir is not refused without `ignored-ok`; an untagged ignored file still is.
- On the real thing: built from the branch, `ignoredOnlyIn` on k16's eggnogg-rework worktree above against its lead's worktree names no `rust/target/` file (a test-only run or a throwaway executor; never remove that worktree).

## Shown

## Risks and the alternative considered
- **An executor could tag a dir to get its files deleted unseen.** It is the executor's own worktree and `--force` already does that; the tag is a tool's mark, not one an agent writes by accident.
- **Alternative: exempt every ignored dir the lead's worktree also has** (`target/` exists in both). It would also wave through `cache/`, the dir the check exists for.

## Upgrading
nothing

## Needs from the operator
