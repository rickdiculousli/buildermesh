---
name: ideate-engineer
description: >-
  Use when you were started as a thread of a buildermesh proposal round - your brief is headed "Engineer: proposal round" or "UI/UX engineer: proposal round". Finds, measures and ranks what to improve in a repo, writes the larger ideas as half-page specs and lands them.
---

# Ideate

You are one thread of a proposal round: you find what's worth improving, prove it, rank it and land the specs; you build nothing. Your brief carries the repo's facts (worktree, node, verify commands, the surfaces and viewports if any, the id to number from) and which parts are yours. Every `bmesh` command carries `--node <your path>`, and every shell command starts with `cd <your worktree> &&`.

The engineering part is yours unless your brief gives you only the UI/UX part. The UI/UX part is yours only when your brief lists surfaces; with none listed, skip it and never mention it.

## Start

`bmesh started --text "<one line>"`, then read, in order: `AGENTS.md` (how the repo works and how to run it); the product doc (`PRODUCT.md`, else the README or `docs/`: what it's for, who uses it, where, its principles); `ideas.md`, if any; every file in `docs/proposals/`, its `assets/` included. Never re-propose a `rejected` spec (its `reason` says why); build on a `later` one only with a reason it's worth doing now; never duplicate one that's `proposed`, `accepted`, `adjusting`, `building` or `done`.

## No measurement, no claim

Keep a lab notebook: every claim points at a measurement, a screenshot and its viewport, or a quoted principle from the product doc or `ideas.md`.

- **Engineering.** Lenses: feature, refinement, test-speed, token-cost, time-cost, maintenance. Time the verify commands and name the slow files or steps with their times. Measure in bytes (`wc -c`) what every session loads (`AGENTS.md`, `CLAUDE.md`, files it says to read at start, always-on skills). Name a repeated manual step and how often it happens. Trace a feature or refinement to a quoted principle.
- **UI/UX.** Run the app and screenshot each listed surface at its viewport: headless on a box with no display (by what `AGENTS.md` names, else Playwright), the real app on one with a display. Look as a user would, at the real distance and size: hierarchy, spacing, legibility, the empty, loading and error states, consistency across surfaces. Save at most four per proposal, a mock of the proposed state included, as `docs/proposals/assets/NNN-<surface>.png`, each referenced from its spec. No screenshot, no UI/UX proposal: a surface that can't be captured is said in your final message, and when the UI/UX part was all you had, `bmesh blocked`.

## Steelman the status quo, then rank

For each candidate, answer:
- What are two or three other ways to close the same gap (for a screen, other ways it could do its job), and why does mine win?
- What's the strongest case that the gap isn't worth closing, or that users don't feel it? If the evidence can't answer it, drop the candidate.

Rank as many as your brief asks, best first, in one list across both parts: what each is worth to the product decides its place. At least one isn't a feature. Sizes are relative, never hours: XS one file, S one module, M crosses modules, L changes an interface others depend on. An M or larger is a spec; an XS or S is a pitch line, `- <title> (<kind>, XS|S): <what changes, where, and why>`, with no file and no id: picked, it's made as a quick fix.

## The half-pagers

- One file per spec, `docs/proposals/NNN-slug.md`, numbered in rank order from your brief's id: a block `bmesh ideate` claimed for you, so a gap is expected when another thread has a block of its own.
- The shape is `bmesh spec template`: every frontmatter field filled, `status: proposed`, the `#` guide lines deleted, the headings kept in order. `lens` is `eng` or `ux` by the part that found it; `kind` a lens above, or `ux`; `proposed_by` the thread id in your brief; `proposed_on` today.
- Judge honestly, in one clause in the Proposal ("Judge: opus, because the API is fixed but touches every handler"): `opus` by default, however large or hard; `fable` only when opus would visibly miss a taste or nuance no rule captures (say what); `sonnet` when a careful junior could build it from the spec.
- Half a page each, evidence over adjectives. "Needs from the operator" stays empty unless a taste call really is theirs, or some box must set a config key to turn the change on (name the key and its file).
- No product code changes. `bmesh spec list --repo .` must list each with no error.

## Land on main

Change nothing outside `docs/proposals/`. Commit on your branch, then:

1. Where the repo has a remote, bring local `main` level with origin: `git -C <repo path> pull --ff-only origin main` when that checkout is on `main`, else `git -C <repo path> fetch origin main:main`. If local `main` has commits origin doesn't, stop and report it; never push or reset them.
2. `git rebase main`. If `main` gained specs with your numbers, claim new ones (`bmesh spec next-id --repo . --count N`), renumber the files, assets and ids, and list again.
3. Fast-forward `main` to your branch: `git -C <repo path> merge --ff-only <branch>` when that checkout is on `main`, else `git -C <repo path> fetch . <branch>:main`. Refused because `main` moved: rebase and try once more; refused otherwise, stop and `bmesh ask`. Then `git -C <repo path> push origin main` where there's a remote.

Never check out, switch, reset or stash in the repo's checkout.

## Report

`bmesh inbox` first. Then `bmesh done --commit <the commit on main> --text "<the pitch>"` (`main`'s head when every idea is a pitch line). The pitch is the ranked list, one line each, readable on a phone: a spec as `NNN title (kind, size, judge): why it matters`, an XS or S idea as its pitch line. Your final message is the same pitch and one line: what landed and what waits on the operator (their review). If you can't finish, `bmesh blocked --text "<why, and what would unblock it>"`; a question only the operator can settle is `bmesh ask`.

If the operator answers in this thread ("1 and 3 yes, 2 no, it duplicates X, 4 later"), map each position to your pitch: land the declines (`bmesh spec set --repo . --id NNN[,NNN] --status rejected --field reason="<their why>" --land`) and the shelved the same way with `--status later`. Start nothing: say which they picked and that picks start from their own chat. A position that could mean two things is asked about in one line, never guessed.
