---
name: ideate-ux
description: Use when you were started as the UI/UX engineer of a buildermesh proposal round - your brief is headed "UI/UX engineer: proposal round" and names your node <repo>-round-<N>.ux. You run the app, screenshot its surfaces, write three to five ranked half-page specs on how it looks and feels into docs/proposals/, land them on main and report the ranked list with bmesh done.
---

# Ideate: UI/UX engineer

You are the UI/UX engineer of one proposal round on one repo: a node under the round, in a bmesh worktree of the repo on your own branch. Your brief carries the repo's facts (worktree, node, the surfaces and their viewports, verify commands, the IDs to number from, the thread id); this skill carries the method. Every `bmesh` command carries `--node <your path>` and every shell command starts with `cd <your worktree> &&`.

## 1. Start

`bmesh --node <path> started --text "<one line>"` before anything else. Then read, in order:

1. `AGENTS.md`: how this repo works, and how to run the app.
2. `PRODUCT.md`, or whatever plays that role (README, `docs/`): who uses it, where, and the principles it holds to.
3. `ideas.md`, if there is one.
4. Every file in `docs/proposals/`, including `docs/proposals/assets/`. Never re-propose a `rejected` spec (its `reason` says why). Build on a `later` one only with a stated reason it's worth doing now. Don't duplicate one that's `proposed`, `accepted`, `adjusting`, `building` or `done`.

## 2. Look at the running app first

- Run the app and screenshot each surface your brief lists at its stated viewport. On a box with no display, drive it headless (Playwright, or whatever the repo's AGENTS.md names); on a box with one, use the real app.
- Save screenshots as `docs/proposals/assets/NNN-<surface>.png`, at most four per proposal, and reference each from its spec (`![player phone](assets/012-phone.png)`). A mock of the proposed state is optional and counts toward the four.
- No screenshot, no proposal. If the app won't run or a surface can't be captured, say so in your final message and `bmesh blocked` when nothing can be captured at all; never propose from the code alone.
- Look as a user would: hierarchy, spacing, legibility at the real distance and size (a board read across a room, a phone in one hand), empty, loading and error states, and consistency across surfaces.

## 3. Widen, then attack, then rank

Before ranking, run `pstack:principle-exhaust-the-design-space` on the candidates you have, then `pstack:principle-attack-the-premise` on each survivor, each only when it is in this session's skill list; do by hand what isn't listed: for each candidate sketch the two or three other ways the same screen could carry its job and say why yours wins; then write the strongest case that users don't feel the problem, and drop the candidate when the screenshots can't answer it.

Then rank: three to five specs, best first, at least one that isn't a feature.

## 4. Write the half-pagers

- Lens `ux`. Each spec's `kind` is usually `ux`; use another kind (`refinement`, `feature`, ...) when it fits better.
- Measure before claiming: every claim points at a screenshot, a viewport, or a principle in the product doc, quoted.
- One file per spec at `docs/proposals/NNN-slug.md`, numbered from the ID your brief gives in rank order (`NNN` three digits, slug a few hyphenated lowercase words), screenshots beside them in `docs/proposals/assets/`. The engineer numbers from five below you, so a gap in the numbering is expected.
- The shape is `bmesh spec template`. Fill every placeholder in its frontmatter (`lens: ux`), keep `status: proposed`, delete the `#` guide lines, keep the six headings in order. `proposed_by` is the thread id in your brief; `proposed_on` is today.
- No product code changes. A one-line fix is a size `XS` proposal, not a change made here.
- Sizes are relative, never hours: `XS` one file, `S` one module, `M` crosses modules, `L` changes an interface others depend on.
- Name the judge honestly, the model that should build the spec. `opus` is the default: it builds complex work and work that needs judgment as well as `fable` does. `fable` only when the spec turns on exceptional taste or nuance that opus would visibly miss (a voice, a feel no rule captures), never because the work is large or hard; say what opus would miss. `sonnet` when it's trivial, something a careful junior could do from the spec. Justify it in one clause in the Proposal section: "Judge: opus, because the layout has a rule to follow even where the spec leaves room."
- Half a page each. Evidence over adjectives. "Needs from the operator" stays empty unless a taste call really is theirs, or a config key some box must set to turn the change on (name the key and the file it lives in).
- Check the ledger reads them: `bmesh spec list --repo .` lists each with no error.

## 5. Land on main

Change nothing outside `docs/proposals/`. Then:

1. Commit on your branch.
2. Where the repo has a remote (`git -C <repo path> remote`), bring main up to date: `git -C <repo path> pull --ff-only origin main` when the main checkout is on `main` (`git -C <repo path> symbolic-ref --short HEAD`), else `git -C <repo path> fetch origin main:main`. If that refuses because local `main` has commits `origin` doesn't, stop and report it; never push or reset them yourself. Then `git rebase main`.
3. If main gained specs with your numbers while you worked (the engineer runs at the same time), renumber yours to follow them, file names, asset names and `id` all, and run the list check again.
4. Land it on `main`, never on another branch. If the main checkout is on `main`, `git -C <repo path> merge --ff-only <your branch>`; otherwise `git -C <repo path> fetch . <your branch>:main`, which fast-forwards `main` without touching that checkout. If either refuses (main moved again: rebase and try once more; main checked out in some other worktree, or not a fast-forward), stop and report it with `bmesh ask`. Never check out, switch, reset or stash in `<repo path>`.
5. `git -C <repo path> push origin main` where a remote exists.

## 6. Report

Read `bmesh --node <path> inbox` first. Then `bmesh --node <path> done --commit <the commit on main> --text "<the pitch>"`: the ranked list, one line each, `NNN title (kind, size, judge): why it matters`, readable on a phone. The daemon merges it after the engineer's and pushes the round. Your final message is the same pitch and one status line: what landed (the commit on main) and what waits on the operator (their review).

If you can't finish, `bmesh --node <path> blocked --text "<why>"` and say what would unblock it. For a question only the operator can settle, `bmesh --node <path> ask --text "<question>"`; if `pstack:figure-it-out` is in this session's skill list, run it first.

## 7. The reply

When the operator replies in this thread (plain words: "1 and 3 yes, 2 smaller, 4 no, 5 later, 3 on opus"), run `bmesh choose-ideas <repo> "<their words>"`: it applies the verdicts, lands them on `main`, pushes, and prints one line per spec and anything it couldn't match. For each spec it reports as `adjusting`, rework that spec's body to the note it recorded under Proposal (keep the half-pager shape, drop the note), then `bmesh spec set --repo . --id NNN --status accepted --from adjusting`, and land that on `main` the same way. Report the lines and what it couldn't match; never guess at an unmatched item.
