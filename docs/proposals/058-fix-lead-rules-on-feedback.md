---
id: 058
title: A fix lead, a pipeline separate from the operator's batches, rules on leads' feedback under bmesh's rulings and builds the fixes itself
kind: feature
size: L
judge: opus
status: proposed
lens: eng
proposed_by: 3a280408-0b8a-43a8-951a-2545a4d777f3
proposed_on: 2026-10-17
---

## Opportunity
Every batch ends with the operator's own session doing the build skill's step 7 (3,332 of the skill's 9,385 bytes): list the feedback, sort it into quick fixes and proposed specs, drain it, build the quick fixes, watch them, then drain their feedback too. Seven build families have produced ten feedback specs. Five were built at once (030, the two from build-2, 042, 053) and five waited on the operator's review (043, 044, 054, 056, 057). Only one of those five (056) asks for something the operator would have to learn: a new `bmesh spec steps` command and a new frontmatter field. The others are a warning in land (043), a refusal in land (044), a test wait (054) and a warning in choose-ideas (057). The fix chains are short: the deepest is two (044 came from 042's lead, and 042 was itself a fix). The loop runs from the operator's chat, and the operator's review is its only brake. Both can move into bmesh: a fix lead that runs separately, and written rulings it judges each issue by and cites.

## Proposal
In:
- **The rulings** ship in the binary (`internal/brief/templates/rulings.md`; a repo's `.bmesh/briefs/rulings.md` replaces them). `bmesh rulings [--repo R]` prints the ones in effect. Each has a slug that a spec cites:
  1. `root`: fix the first wrong decision, not the place it showed. Then fix every other place with the same fault. A guard is a fix only when the state it handles is legitimate.
  2. `failing-first`: name what fails before the fix and passes after: a test through the real command or package, or a run with its output. A rate or timing the fix rests on carries its run count. An issue that can't be made to fail is dropped, saying what was tried.
  3. `smallest`: make the smallest change that removes the cause, deleting before adding. A sentence added to a skill or brief that every session loads names the sentence it replaces, or why none can go.
  4. `structure`: a lesson met twice becomes a check. When a skill sentence already warns about the issue, the fix is a refusal, warning or test in the binary, and the sentence goes.
  5. `premise`: no third fix on one premise. When two fixes of the same thing haven't held, write down the belief both assumed, test it, and propose it to the operator with the evidence.
  6. `no-new-surface`: a fix adds no command, flag, config or registry key, frontmatter field, bucket, subject or event. Anything new the operator would have to learn is product direction, and is proposed.
  7. `reversible`: build what can be undone inside the repo without asking, then report it. Proposed, never built: taste, product direction, anything sized L, and anything outside the repo's `main` (a step on another box, deleted records, the registry or a box's config).
  8. `drop`: dropping is an outcome. That covers what a spec on `main` already covers (any status), what a `--fixed` commit settled, and the reporting lead's own spec's work.

  These restate the principles that hold for automated fixes. Excluded: the code-design principles (types, boundaries, domain modelling, reader load), which are the repo's AGENTS.md and its review's to judge; the migration ones, since a migration is L and goes to the operator; the design-space ones, which are the ideate method for what is proposed. The pipeline's own design applies the rest: it is idempotent, it fixes one thing at a time, and it has one writer per repo. Nothing depends on pstack.
- **`bmesh fix <repo> [--model M] [--effort E]`** starts the fix lead. It is node `<repo>-fix-<N>`, role `fixer`, family `<repo>-fix-<N>`, on the row's `host`, and runs on Opus at `[models] lead_effort`. Its brief (`fix`, overridable) carries the rulings and points at a new `fix` skill. A second fixer of the repo while one is live is refused (exit 3), so each repo's queue has one writer. A non-ship row is refused.
- **The `fix` skill** runs one pass:
  1. `bmesh feedback list --repo <repo>`: every pending issue naming the repo, across families.
  2. One ruling per issue:
     - A fix becomes a spec with `status: accepted`, `ruled: [<slugs>]`, `fix_of: <the reporting lead's spec>` and `from_feedback: <family> (<lead>) #<seq>`.
     - What rulings 5 to 7 send on becomes a spec with `status: proposed` and the ruling named in its Opportunity.
     - A drop is a line in the report.
  3. Each spec lands on `main`, then its issue is drained at once (`feedback drain --seq <seq>`).
  4. Each fix is built one at a time (`bmesh build <repo> NNN --family <its own>`) and watched; the fixer relays an `ask` the rulings settle and asks the operator the rest. It lists again after each one, so the fix's own lead's feedback is ruled on in the same pass.
  5. Once the queue is empty and no fix lead is running: `bmesh release <repo>` when the repo has a tag, then `bmesh done`. Its text is one line per issue (`<seq>: fixed by NNN (root, failing-first)`, `proposed NNN (no-new-surface: ...)`, `dropped (drop: 044 covers it)`), with what waits on the operator first.
- **The binary holds the rulings it can check.**
  - `feedback list` hides an issue whose `#<seq>` a spec on `main` already names, so a fixer that crashed between landing a spec and draining it never writes that spec twice.
  - `build` refuses a `ruled` spec sized L (`reversible`), and one whose `fix_of` chain is three deep (`premise`). The refusal says to set it `proposed`.
- **Separate pipelines.** A family named `<repo>-fix-<N>` and the operator's families don't hold each other's builds, and pending feedback no longer holds a build, since the fixer owns it. `[runner] max_leads` still caps a host, and the main lease still serializes landing. `release` holds while any lead of the repo is running, whichever pipeline it is in, so whichever pipeline goes quiet last cuts the release.
- **The build skill's step 7** becomes: once `watch-done`, run `bmesh fix <repo>` for each repo with pending feedback, report the fixer's path, and release when no fixer is live. The loop bound and the triage leave the build skill.

Out: the daemon starting the fixer on its own (059), map drawing for the fixer, and quota-aware pacing.

Judge: opus, because the rulings' wording and the skill's triage are what every automated fix rests on, and the binary changes touch build's holds.

## What you'll see
At the end of a batch, the coordinator prints `fix: buildermesh-fix-1 started` and stops. Later `mesh-status` shows the fixer working with its fix lead under it. Its done reads, for example: `1779: proposed 060 (no-new-surface: adds bmesh spec steps)`, `1786: fixed by 061 (structure, failing-first)`, `released buildermesh v0.4.1`. `bmesh rulings` prints the eight rulings.

## Verification
The repo's verify, plus tests on `meshtest.Start`:
- `fix` refuses a second live fixer and a non-ship row.
- `feedback list --repo` spans families and hides a seq that a spec names.
- `drain --seq` deletes exactly those messages.
- `build` refuses a `ruled` L spec and a three-deep `fix_of` chain, and builds a two-deep one.
- A fix family and an operator family don't hold each other, and `release` holds while a fix lead runs.
- `rulings` prints the repo override when there is one.

One rehearsal outside verify: replay the issues behind 043, 044, 054, 056 and 057 through a fixer on a scratch mesh. Four should come out as fixes and 056 as proposed. Record the result in the closing commit.

## Risks and the alternative considered
Fixes now land without anyone reading them first. The `ruled` line on each spec and the fixer's done text are the audit trail. The checkable rulings stop the expensive kinds (an L change, a third fix in one chain), and `no-new-surface` keeps product growth with the operator.

Alternatives:
- Let the coordinator auto-accept what it now proposes. Rejected: the loop would stay in the operator's session.
- A routine kind. Rejected: a routine is one job on one commit, and it can't dispatch and watch leads for an hour.
- A T3 scheduled thread. Rejected: bmesh would then depend on T3's scheduler.

## Needs from the operator
