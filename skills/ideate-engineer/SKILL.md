---
name: ideate-engineer
description: >-
  Use when you were started as a thread of a buildermesh proposal round - your brief is headed "Engineer: proposal round" or "UI/UX engineer: proposal round" and names your node <repo>-round-<N>.eng or .ux. You read the repo, measure, run the app and screenshot its surfaces when your brief lists them, rank the ideas in one list, write the ones sized M or larger as half-page specs into docs/proposals/, land them on main and report the ranked list with bmesh done.
---

# Ideate

You are a thread of one proposal round on one repo: a node under the round, in a bmesh worktree of the repo on your own branch. Your brief carries the repo's facts (worktree, node, verify commands, the surfaces and their viewports when it has any, the IDs to number from, the thread id) and which parts of this method are yours; this skill carries the method. Every `bmesh` command carries `--node <your path>` and every shell command starts with `cd <your worktree> &&`.

The method has two parts: the engineering part (section 2) and the UI/UX part (section 3). Section 2 is yours unless your brief says you run only the UI/UX part. Section 3 is yours only when your brief lists surfaces to screenshot; a brief that lists none has no UI/UX part, so skip section 3 and say nothing of it, in your specs, your pitch or your final message.

## 1. Start

`bmesh --node <path> started --text "<one line>"` before anything else. Then read, in order:

1. `AGENTS.md`: how this repo works, and how to run the app.
2. `PRODUCT.md`, or whatever plays that role (README, `docs/`): what the product is for, who uses it, where, and the principles it holds to.
3. `ideas.md`, if there is one.
4. Every file in `docs/proposals/`, including `docs/proposals/assets/`. Never re-propose a `rejected` spec (its `reason` says why). Build on a `later` one only with a stated reason it's worth doing now. Don't duplicate one that's `proposed`, `accepted`, `adjusting`, `building` or `done`.

## 2. The engineering part: measure before claiming

- Lenses: feature, refinement, test-speed, token-cost, time-cost, maintenance.
- Time the verify commands your brief names and name the slow files or steps with their times. A test-speed proposal without timings isn't one.
- Measure `AGENTS.md` and anything loaded into every session (CLAUDE.md, files AGENTS.md says to read at start, always-on skills) in bytes with `wc -c`. Token-cost proposals cite those numbers or the repeated context load.
- Time-cost proposals name the repeated manual step or slow loop and how often it happens.
- Feature and refinement proposals trace to a principle in the product doc or an entry in `ideas.md`, quoted.

## 3. The UI/UX part: look at the running app first

- Run the app and screenshot each surface your brief lists at its stated viewport. On a box with no display, drive it headless (Playwright, or whatever the repo's AGENTS.md names); on a box with one, use the real app.
- Save screenshots as `docs/proposals/assets/NNN-<surface>.png`, at most four per proposal, and reference each from its spec (`![player phone](assets/012-phone.png)`). A mock of the proposed state is optional and counts toward the four.
- No screenshot, no UI/UX proposal. If the app won't run or a surface can't be captured, say so in your final message and propose nothing on how it looks from the code alone; when the UI/UX part is all your brief gives you and nothing can be captured, `bmesh blocked`.
- Look as a user would: hierarchy, spacing, legibility at the real distance and size (a board read across a room, a phone in one hand), empty, loading and error states, and consistency across surfaces.

## 4. Widen, then attack, then rank

Before ranking, for each candidate write the two or three other ways the same gap could be closed (for a screen, the other ways it could carry its job) and say why yours wins; then write the strongest case that the gap isn't worth closing, or that users don't feel it, and drop the candidate when you can't answer it (for a screen, from the screenshots).

Then rank as many ideas as your brief asks, best first, in one list whatever their lens: what each is worth to the product decides its place, not which part found it. At least one isn't a feature. An idea sized M or larger is a spec (section 5). One sized XS or S is a pitch line, `- <title> (<kind>, XS|S): <what changes, where, and why>`, with no spec file and no id: picked, it is made as a quick fix.

## 5. Write the half-pagers

- One file per spec at `docs/proposals/NNN-slug.md`, numbered from the ID your brief gives in rank order (`NNN` three digits, slug a few hyphenated lowercase words), screenshots beside them in `docs/proposals/assets/`. Your brief's ID starts the block of ids `bmesh ideate` claimed for you on the mesh; when your brief says another thread has a block of its own, a gap in the numbering is expected.
- The shape is `bmesh spec template`. Fill every placeholder in its frontmatter, keep `status: proposed`, delete the `#` guide lines, keep the six headings in order. `lens` is `eng` for an idea from the engineering part, `ux` for one from the UI/UX part. `kind` is one of the lenses in section 2, or `ux`, usually, for a UI/UX idea. `proposed_by` is the thread id in your brief; `proposed_on` is today.
- Every claim points at evidence: a measurement, a screenshot and its viewport, or a principle in the product doc, quoted.
- No product code changes. A one-line fix is an XS pitch line, not a change made here.
- Sizes are relative, never hours: `XS` one file, `S` one module, `M` crosses modules, `L` changes an interface others depend on.
- Name the judge honestly, the model that should build the spec. `opus` is the default: it builds complex work and work that needs judgment as well as `fable` does. `fable` only when the spec turns on exceptional taste or nuance that opus would visibly miss (a voice, a feel no rule captures), never because the work is large or hard; say what opus would miss. `sonnet` when it's trivial, something a careful junior could do from the spec. Justify it in one clause in the Proposal section: "Judge: opus, because the API is fixed but touches every handler."
- Half a page each. Evidence over adjectives. "Needs from the operator" stays empty unless a taste call really is theirs, or a config key some box must set to turn the change on (name the key and the file it lives in).
- Check the ledger reads them: `bmesh spec list --repo .` lists each with no error.

## 6. Land on main

Change nothing outside `docs/proposals/`. Then:

1. Commit on your branch.
2. Where the repo has a remote (`git -C <repo path> remote`), bring main up to date: `git -C <repo path> pull --ff-only origin main` when the main checkout is on `main` (`git -C <repo path> symbolic-ref --short HEAD`), else `git -C <repo path> fetch origin main:main`. If that refuses because local `main` has commits `origin` doesn't, stop and report it; never push or reset them yourself. Then `git rebase main`.
3. If main gained specs with your numbers while you worked (a writer who didn't claim them), claim new ones with `bmesh spec next-id --repo . --count N` and renumber yours to them, file names, asset names and `id` all, and run the list check again.
4. Land it on `main`, never on another branch. If the main checkout is on `main`, `git -C <repo path> merge --ff-only <your branch>`; otherwise `git -C <repo path> fetch . <your branch>:main`, which fast-forwards `main` without touching that checkout. If either refuses (main moved again: rebase and try once more; main checked out in some other worktree, or not a fast-forward), stop and report it with `bmesh ask`. Never check out, switch, reset or stash in `<repo path>`.
5. `git -C <repo path> push origin main` where a remote exists.

## 7. Report

Read `bmesh --node <path> inbox` first. Then `bmesh --node <path> done --commit <the commit on main> --text "<the pitch>"` (`main`'s head when every idea is a pitch line, so nothing landed): the ranked list, one line each: a spec as `NNN title (kind, size, judge): why it matters`, an XS or S idea as its pitch line, readable on a phone. The daemon pushes it as the round's pitch, the engineer's list first when two threads ran. Your final message is the same pitch and one status line: what landed (the commit on main) and what waits on the operator (their review).

If you can't finish (the repo won't build, the product doc is missing and nothing stands in), `bmesh --node <path> blocked --text "<why>"` and say what would unblock it. For a question only the operator can settle, `bmesh --node <path> ask --text "<question>"`.

## 8. The reply

The operator answers the pitch in their own chat, where picking a spec starts its lead and the leads are watched. When they reply in this thread instead ("1 and 3 yes, 2 no, it duplicates X, 4 later"), map each position to the pitch you reported: land what they declined, `bmesh --node <path> spec set --repo . --id NNN[,NNN] --status rejected --field reason="<their why>" --land`, and what they shelved the same way with `--status later`. Start nothing: say in one line which ones they picked and that the picks start from their chat. A position that could mean two things is asked about in one line, never guessed.
