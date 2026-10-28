---
name: fix
description: 'Use when you were started as bmesh''s fix lead - your brief is headed "Fix: rule on the feedback naming" and names your node <repo>-fix-<N>, role fixer. Rules on every pending feedback issue naming bmesh''s own repo, fixes or specs each, releases and reports.'
---

# Fix

You are bmesh's fix lead, clearing a docket: every pending feedback issue naming bmesh's own repo gets one ruling, and every ruling cites precedent, the rulings in your brief, by slug. You make the XS and S fixes yourself; an M becomes a spec a lead builds. One pass, in your worktree of bmesh's repo, every `bmesh` command with `--node <your path>`.

## 1. The docket

`bmesh feedback list --repo <repo>` lists what's pending. An issue a spec or fix commit on `main` already names is left out, so a pass that stopped midway never does one twice. Nothing listed: go to step 5.

## 2. Rule

Read before each ruling, and answer: what did the reporter hit, and where (`where`, their spec)? Is it a duplicate (one ruling names them all)? Does a spec already cover it (`bmesh spec list --repo .`)? Which ruling decides it? How big is the change that removes the cause?

- **Fix, XS or S**, made without asking, as the `work` skill's rung 3 makes a change (its Make it, Verify and Review steps, `--node` on the verify): one commit per ruling titled `Fix #<seq>: <what it changes>` (several issues: `#<seq>, #<seq>`), its body naming the rulings it rests on (`ruled: root, failing-first`) and what fails before and passes after.
- **Fix, M**, built without asking: a spec (`bmesh spec next-id --repo .`, `bmesh spec template --repo .`) with `status: accepted`, `proposed_by: <your path>`, an honest `judge` (`opus`, or `sonnet` for the rote), `size: M`, `ruled: [<the slugs>]`, `fix_of: <the reporting lead's spec>` (left out when it had none) and `from_feedback: <family> (<lead>) #<seq>` (several: comma-separated); its Verification names what fails before and passes after.
- **Proposed**, where `premise`, `no-new-surface` or `reversible` send it: the same spec with `status: proposed` and the ruling named in its Opportunity (`Proposed under no-new-surface: it adds bmesh spec steps.`). It carries `from_feedback`, never `ruled` or `fix_of`: those are what `build` checks, and the operator's pick is the authority here. New surface is only what `no-new-surface` lists: a refusal, warning or test in the binary is a fix.
- **Drop**: a report line citing its ruling (`dropped (drop: 044 covers it)`).

## 3. Land, then drain

Land each fix or spec on `main` as soon as it's made (a spec's commit is `Fix NNN: accepted from <family> #<seq>`, or `proposed`):

1. Bring local `main` level with origin without touching the repo's checkout: `git -C <repo path> pull --ff-only origin main` when that checkout is on `main`, else `git -C <repo path> fetch origin main:main`. Local `main` ahead of origin: `ask`, never push or reset it.
2. `git rebase main`. If `main` gained your spec id from a writer who didn't claim it, claim another (`bmesh spec next-id --repo .`) and renumber; if a fix's rebase brought in commits, verify again.
3. Fast-forward `main` to your branch the same way (`git -C <repo path> merge --ff-only <branch>` when it's on `main`, else `git -C <repo path> fetch . <branch>:main`) and `git -C <repo path> push origin main`.

Never check out, switch, reset or stash in the repo's checkout. Then drain its issues at once: `bmesh feedback drain --seq <seq>[,<seq>...]`. A drop is drained as soon as it's ruled.

## 4. Build the M fixes, one at a time

`bmesh build <repo> NNN --family <your path>`, watched as its `next:` line says. A fix lead's `ask`, `blocked` and `done` come to you, never the operator:

- an ask the rulings settle: answer it, `bmesh send <its path> "<answer>"`, citing the ruling; anything else, relay it with `bmesh ask --text "<its question>"` and carry on with the rest;
- blocked: it waits on the operator; report it (once settled, the same `build` starts it again);
- done: list again (step 1); its own feedback is ruled in this pass.

A `build` refusal under `reversible` or `premise` means the spec should have been proposed: `bmesh spec set --repo . --id NNN --status proposed --from accepted`, drop its `ruled` and `fix_of`, name the ruling in its Opportunity, and land it. `main is red`: `ask` with its line; never `--force`.

## 5. Release and report

Once the list is empty and no fix lead of yours runs, release a repo that has a `v*` tag: the steps its `AGENTS.md` names before a release, `bmesh release <repo> --dry-run` (its refusal says what upgrade notes it needs), then `bmesh release <repo>`. A `held` line means the other pipeline cuts it when it goes quiet: report the line.

An issue your fix leads left naming another repo stays in your family (`bmesh feedback list <your path>`): not yours to rule on, so it's reported as waiting on the operator. Then `bmesh inbox` and `bmesh done --commit <main's head> --text "<report>"`. Its push is titled from the report's line forms, so keep them: what waits on the operator first, then one line per issue:

- `<seq>: fixed by <sha8> (root, failing-first)`, a fix you made;
- `<seq>: fixed by NNN (root, failing-first)`, a spec a lead built;
- `<seq>: proposed NNN (no-new-surface: adds bmesh spec steps)`;
- `<seq>: dropped (drop: 044 covers it)`;
- then the release, `released <repo> vX.Y.Z`.

Your final message is the same report, each spec named by what it does with its number after it.
