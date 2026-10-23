---
id: 070
title: A repo names its setup command, and every worktree bmesh works in runs it, so a verify never fails for want of installed dependencies
kind: refinement
size: L
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-17
built_by: e64e8811-bc3b-49a5-9119-5abe0523d21a
host: k16
family: buildermesh-build-10
branch: bmesh/e64e8811-bc3b-49a5-9119-5abe0523d21a
merged: 4fca7d2678697d2e0c451460d890441ea8caca6b
operator_steps: [{"n":1,"box":"","text":"Give each repo whose verify needs installed dependencies (the one whose build was refused with `vitest: not found` first) a `setup` and its `setup_inputs` in its registry row, publish the registry once every box running its routines or leads runs this bmesh, then `bmesh scaffold <repo>` and commit and push what it writes.","check":"bmesh repos show <repo>","done":"2026-10-23T06:38:37Z ricks-macbook","note":"party-buzzer scaffolded at b7f460c; opengym-rick already had setup; uv repos and gametrainer need none; openGym (fork) left alone"}]
verified: verify ok t:44c64d29
---

## Opportunity
bmesh makes worktrees with plain `git worktree add`, and nothing installs the dependencies a repo gitignores. opengym-rick's first `bmesh build` was refused with `main is red at 14454cdb ... verify fail`, and the output was `sh: 1: vitest: not found`. `verifyMain` (`cmd/bmesh/cmd_build_gate.go:167`) had run `npm test` in a fresh worktree with no `node_modules`. The main checkout passes only because its three `node_modules` were installed by hand.

The gap is in every worktree something works in:
- **verify-main's worktree** (`cmd_build_gate.go:167`).
- **Agent worktrees.** Leads made by T3 (`workspaceStrategy` worktree, `internal/runner/t3v2.go:375`) or by `worktreeFor` for claude and tmux (`runner.go:122`). Executors (`cmd_executor.go:170`). Ideate threads (`cmd_ideate.go:368`).
- **Routine checkouts.** Created by `checkout.Ensure` (`internal/checkout/checkout.go:64`) and moved per job by `MoveTo` (`:91`). `MoveTo` ends in `git clean -fdq`, which keeps ignored files, so a pooled instance runs a job against the previous job's packages.

A lead installs by hand when it notices. A verify-main or routine run has nobody to notice.

Worktree tools usually answer this with a per-repo setup command run when a worktree is created (T3's project setup, Cursor's `worktrees.json`, Conductor), and they leave reuse to the package manager's own cache:
- npm's `~/.npm`: a warm `npm ci` of opengym's frontend took 2.65 s;
- pnpm, bun and uv hardlink or clone from a global store;
- Go and cargo keep a global module cache.

## Proposal
In:
- **Declaration.** The registry row gains `setup` (one shell command), optional `setup_inputs` (git pathspecs, e.g. `["**/package-lock.json"]`) and optional `setup_timeout` (default `10m`).
  - `bmesh scaffold` writes them into the repo's `.bmesh/config.toml` as `[setup] command / inputs / timeout`. That is a new scaffold target, merge-written so `[slots]` and `[routines]` are kept.
  - `land`'s drift check covers the `[setup]` block. As 043 does, it compares only what the branch changed.
  - bmesh reads `[setup]` from the commit being worked on, so a row that lives on another box doesn't matter, and verify-main runs the setup of the commit it checks.
  - A repo with no `[setup]` behaves exactly as today.
- **Staleness, one rule for every site.** A successful run writes `bmesh-setup` into the worktree's own git dir (`git rev-parse --git-dir`), outside the tree. It holds a sha256 of the command and of every tracked file matching `inputs`. Setup is "current" when that hash matches.
  - With no `inputs`, a worktree is current once setup has run in it, except a routine checkout, which re-runs on every job because `MoveTo` changes its commit.
- **`bmesh setup [--check]`.** Runs setup in the cwd worktree when it isn't current and writes the hash. `--check` exits 1 when stale or missing, and runs nothing. `--force` runs it anyway.
- **Where it runs.**
  - *verify-main:* right before the verify kind runs inline (`cmd_build_gate.go:196`). Never when a record answers or under `--force`.
  - *Inline verify:* `bmesh routine run` runs setup when not current, then checks the tree is clean with `git status --porcelain`, before `runInline` (`cmd_routine.go:683`). A setup that leaves untracked, unignored files fails as `setup dirtied the tree: <paths>`, never a recorded result.
  - *Agent worktrees* (leads, executors, ideate threads, on every runner): the brief's first step is `bmesh setup`, and the agent reads its own failure. T3 makes the lead's worktree after the brief is written (`cmd_lead.go:174`, `t3v2.go:400`), so bmesh can't run setup there before the agent starts, and moving worktree creation out of T3 would change lead branches. A T3 project setup that already calls `bmesh setup` makes this step a no-op.
  - *Routine jobs:* after `MoveTo` (`internal/daemon/routines_job.go:326`) and before the slot is acquired, for command kinds only. A model kind (`review`) reads diffs under an `allow` list and runs nothing that needs dependencies.
  - *Never:* `editOnMain` (`cmd_spec.go:298`) and `recordOnMain` (`cmd_build.go:347`), which are ledger edits with no agent, removed at once. Also never the daemon's `main` read checkout (`routines.go:229`) or selfupdate's checkouts.
- **Failure never records a result.** verify-main refuses with `setup failed` and the output tail, recording nothing, so a registry flake never turns `main` red. An inline verify or routine job fails as an infrastructure error: no record under the tree, and the job is answered `ok=false` with the reason.
- **Inputs are code.** `CodePaths` adds the `setup_inputs` pathspecs, so a lockfile-only commit is never carried as "nothing under code changed" (061 in `land`, 068's carry) and the bumped dependencies get verified.
- **Docs.** The manual (the row's fields, worktrees, `routine run`, the build gate, routine jobs, `bmesh setup`), the lead and executor skills' verify sections ("`bmesh setup` first, and again after changing dependencies") and the brief templates.

Out:
- a dependency store of bmesh's own;
- language presets or detecting lockfiles;
- editing T3's project config;
- syncing caches between boxes;
- a key change for verify results, which stay keyed by tree.

Judge: opus, because it threads one rule through the build gate, routine runs, the daemon, scaffold, land, code paths and briefs, and a setup that records or skips wrongly turns `main` red or hides a stale install.

## What you'll see
- With `setup` set on a Node repo, `bmesh build` verifies `main` in a fresh worktree that installs first and passes.
- A new lead's first command is `bmesh setup`.
- A pooled job after a lockfile change runs against the new packages.
- A lockfile-only commit is verified, not carried.
- `bmesh setup --check` says whether a worktree's installs are current.

## Verification
The repo's verify, plus tests over a bare origin whose `[setup]` command appends to a log file:
- `cmd_build_gate_test.go`: verify-main runs setup before the verify kind and not when a record answers. A failing setup refuses and records nothing.
- `cmd_routine_test.go`:
  - `routine run verify` re-runs setup when an `inputs` file changed, and skips it when unchanged.
  - With no `inputs`, it runs once.
  - A setup that leaves an untracked file fails unrecorded.
- `internal/daemon` routines tests:
  - a command job re-runs setup after a move that changes an input, and runs it on every job with no `inputs`;
  - a model job never runs it;
  - a setup fail answers `ok=false` with no record.
- `cmd_scaffold_test.go`: `[setup]` is merge-written from the row, keeping `[slots]`. `cmd_land_test.go`: drift the branch made in `[setup]` refuses.
- `selfupdate` tests: `CodePaths` includes `setup_inputs`, and a lockfile-only commit isn't carried.
- Brief tests: lead, executor and ideate briefs start with `bmesh setup` when the repo has `[setup]`, and not otherwise.
- `bmesh setup --check`: exit 0 when current, 1 when stale.

## Risks and the alternative considered
- **A slow setup** (a cold cache, large native builds) adds its time to verify-main, to each agent's start and to each lockfile change. `setup_timeout` caps it, and pnpm, bun or uv make it cheap. That's the repo's choice.
- **Network** is needed where the cache is cold. A failed setup records nothing, so a flake costs a retry, not a red `main`.
- **Concurrent setups** in two worktrees share the package manager's cache. npm, pnpm, uv and Go's caches are built for concurrent use.
- **Inputs that miss a file** leave a stale install undetected in a long-lived worktree. The inputs are the repo's to get right, as `code` is.
- **An agent that skips its brief's first step** works without dependencies, as today. Its own verify then runs setup anyway.
- **Alternative: the install in each repo's verify command** (`[ -d node_modules ] || npm ci`). No bmesh change, but every repo repeats it, it never notices a stale install, and agents still start without dependencies.
- **Alternative: a content-addressed dependency store in bmesh.** Dropped while grilling the idea: every key and link rule was specific to one language, and pnpm, bun and uv already do it.

## Upgrading
- Ask: the registry row gains `setup`, `setup_inputs` and `setup_timeout`, which `bmesh scaffold <repo>` writes into the repo's `.bmesh/config.toml` as `[setup]`; which repos get a setup command? The choices are none, or a command per repo (e.g. `setup = "npm ci"` with `setup_inputs = [":(glob)**/package-lock.json"]`, which matches the root lockfile too, then `bmesh scaffold <repo>` and commit what it writes). The default is none, today's behaviour.
- A box on an older bmesh refuses a registry that carries these keys (`registry vN needs a newer bmesh`) and keeps its previous `repos.toml`; its daemon and CLI ignore `[setup]` and run verifies and routine jobs without it. Upgrade every box that runs a repo's routines or leads before giving that repo a setup.

## Needs from the operator
- Give each repo whose verify needs installed dependencies (the one whose build was refused with `vitest: not found` first) a `setup` and its `setup_inputs` in its registry row, publish the registry once every box running its routines or leads runs this bmesh, then `bmesh scaffold <repo>` and commit and push what it writes. check: `bmesh repos show <repo>`
