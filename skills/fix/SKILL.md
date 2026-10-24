---
name: fix
description: 'Use when you were started as bmesh''s fix lead - your brief is headed "Fix: rule on the feedback naming" and names your node <repo>-fix-<N>, role fixer. You rule on every pending feedback issue naming bmesh''s own repo under the brief''s rulings, make the XS and S fixes yourself, land the rest as specs on main, build the M fixes one at a time, release and report with bmesh done.'
---

# Fix

You are bmesh's fix lead: one node, `<repo>-fix-<N>`, whose family is its own path, in a worktree of bmesh's own repo. Your brief holds the rulings; every ruling you make cites them by slug. You make the XS and S fixes yourself and land them on `main`; an M fix you write as a spec, and a lead builds it. Every `bmesh` command carries `--node <your path>`. One pass:

## 1. List

`bmesh feedback list --repo <repo>` prints every pending issue naming the repo, across families, one per line: `<seq> <from> (<repo> <spec>, family <F>[, where W][, fixed F]): <text>`. An issue a spec on `main` already names (`#<seq>` in its `from_feedback`), or a fix commit there names in its subject (`Fix #<seq>: ...`), is left out, so a pass that stopped between landing a fix or a spec and draining its issue never does it twice. Nothing listed: go to step 5.

## 2. Rule

Read the repo before you rule: the reporting lead's spec, the file or command `where` names, `bmesh spec list --repo .` and the specs it likely touches. Duplicates are one ruling naming every issue. One ruling per issue:

- **Fix, XS or S** (made without asking): you make it in your worktree, as the `work` skill's rung 3 makes a change (its **Make it**, **Verify** and **Review** steps) (`--node <your path>` on the verify), one commit per ruling titled `Fix #<seq>: <what it changes>` (several issues: `#<seq>, #<seq>`), its body naming the rulings it rests on (`ruled: root, failing-first`) and what fails before and passes after (`failing-first`). No spec, no lead.
- **Fix, M** (built without asking): a spec from `bmesh spec template --repo .`, its id claimed with `bmesh spec next-id --repo .`, then landed, with `status: accepted`, `proposed_by: <your path>`, `judge` honestly (`opus`, or `sonnet` for the rote), `size: M`, `ruled: [<the slugs it rests on>]`, `fix_of: <the reporting lead's spec id>` (left out when that lead had no spec) and `from_feedback: <family> (<lead>) #<seq>` (several issues: comma-separated, each with its `#<seq>`). Its Verification names what fails before and passes after (`failing-first`).
- **Proposed** (what `premise`, `no-new-surface` or `reversible` send on): the same, with `status: proposed` and the ruling named in its Opportunity (`Proposed under no-new-surface: it adds bmesh spec steps.`). New surface is only what `no-new-surface` lists: a refusal, warning or test in the binary is not (`structure` names them as fixes), whatever it reads from a spec as written today. It carries `from_feedback`, never `ruled` or `fix_of`: those are what `bmesh build` checks, and the operator's acceptance is the authority for a proposed spec.
- **Drop**: a line in the report, its ruling cited (`dropped (drop: 044 covers it)`).

## 3. Land, then drain

Land each fix and spec on `main` as soon as it is made: commit it on your branch (a fix as step 2 titles it, a spec as `Fix NNN: accepted from <family> #<seq>`, or `proposed`); bring `main` up to date (`git -C <repo path> pull --ff-only origin main` when that checkout is on `main`, else `git -C <repo path> fetch origin main:main`; if local `main` has commits `origin` doesn't, stop and `ask`, never push or reset them); `git rebase main` (if `main` gained your id from a writer who didn't claim it, claim another with `bmesh spec next-id --repo .` and renumber to it; after a fix's rebase brought in commits, `bmesh --node <your path> routine run verify` again); fast-forward `main` (`git -C <repo path> merge --ff-only <your branch>` when that checkout is on `main`, else `git -C <repo path> fetch . <your branch>:main`) and `git -C <repo path> push origin main`. Never check out, switch, reset or stash in `<repo path>`. Then drain its issues at once: `bmesh feedback drain --seq <seq>[,<seq>...]`, which deletes exactly those and prints `drained N`. A drop is drained right after it is ruled.

## 4. Build the M fixes, one at a time

`bmesh build <repo> NNN --family <your path>` starts its lead and records nothing on `main`: the spec reads `accepted` there until its lead lands it `done` with its code. Arm `bmesh watch --nodes <its path> --timeout 30m` (re-arm with `--since` on exit 5; a Monitor where the harness has one). Fixes and the operator's builds are separate pipelines and don't hold each other. A fix lead's `ask`, `blocked` and `done` never reach the operator: they come to you, on the watch and in your inbox. When the watch shows:

- an `ask` the rulings settle: answer it, `bmesh send <its path> "<answer>"`, citing the ruling; anything else, relay with `bmesh ask --text "<its question>"` and carry on with the rest of the queue;
- `blocked`: it waits on the operator; report it (the block is the lead's node state, never in the spec; once it is settled, `bmesh build <repo> NNN --family <your path>` starts it again);
- `done`: list again (step 1). The fix's own lead may have left feedback; rule on it in this pass.

A `build` refusal under `reversible` or `premise` means the spec should have been proposed: set it `proposed` (`bmesh spec set --repo . --id NNN --status proposed --from accepted`), drop its `ruled` and `fix_of` lines, name the ruling in its Opportunity and land it. `main is red`: `ask` the operator with its line; never `--force`.

## 5. Release and report

Once `feedback list --repo <repo>` is empty and no fix lead of yours is running: when the repo has a release tag (`git -C <repo path> tag --list 'v*'` prints one), release it as the `work` skill's **Releases** releases bmesh's own repo (`bmesh release <repo> --dry-run`, ponytail's level, its upgrade notes, `bmesh release <repo>`). `release` holds while any lead of the repo runs, in either pipeline: a `held` line means the other pipeline cuts it when it goes quiet; report that line instead.

An issue your fix leads left naming another repo stays in your family (`bmesh feedback list <your path>` shows it): it isn't yours to rule on, so the report names it as waiting on the operator. Then `bmesh inbox`, and `bmesh done --commit <main's head> --text "<report>"`. Its push is titled from the report (`<repo> fixes: N fixed, M wait on you`, counting the `fixed by` and `proposed` lines), so keep those forms. The report puts what waits on the operator first, then one line per issue: `<seq>: fixed by <sha8> (root, failing-first)` for a fix you made, `<seq>: fixed by NNN (root, failing-first)` for a spec a lead built, `<seq>: proposed NNN (no-new-surface: adds bmesh spec steps)`, `<seq>: dropped (drop: 044 covers it)`, then the release (`released <repo> vX.Y.Z`). Your final message is the same report, each spec named by what it does with its number after it.
