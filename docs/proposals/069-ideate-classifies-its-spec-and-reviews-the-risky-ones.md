---
id: 069
title: An idea's spec is classified by Haiku, and a load-bearing, security or cross-cutting one is reviewed by Opus under the fix rulings before it lands
kind: feature
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-17
---

## Opportunity
An idea's spec lands on `main` as its writer left it, and nothing checks it before the operator reads it. 068 (verify answered from the last verified tree) was written in the operator's session and pushed. An Opus review afterwards found it wrong in four ways that matter:
- it carried results for kinds `verify` never runs as;
- its lookup came up empty after `land`'s rebase;
- it missed `build`'s and `release`'s gate;
- its evidence measured the wrong commits.

Each would have recorded an ok no verify saw. The fixes are a second Propose commit (bea5259). Specs vary in what a mistake costs. Of the 36 specs on `main` with `proposed_by: operator`, 6 are S, 22 M and 8 L, and many touch `land`, `build`, verify or release. So a review should go where a wrong spec is expensive, and the classifier has to show it can separate the cases (Verification).

The principles a reviewer should hold a spec to already exist: the fix lead's eight rulings adapted from pstack (`root`, `failing-first`, `smallest`, `structure`, `premise`, `no-new-surface`, `reversible`, `drop`). The fix brief has them (`internal/brief/templates/fix.md:15-22`). The manual has a reworded copy that has already drifted (`docs/manual.md:2627-2636`). No other session can quote either one.

## Proposal
In:
- **One source for the rulings.**
  - The numbered list (fix.md lines 15-22, nothing else) moves to `internal/brief/rulings.md`. It is embedded on its own (`//go:embed rulings.md` into `brief.Rulings`), outside `templates/`, so it is neither a brief nor overridable.
  - `cmd_fix.go` sets `values["rulings"] = brief.Rulings`. fix.md keeps its heading, the citing sentence and the `bmesh build` sentence around `{{rulings}}`, so the fix brief renders byte for byte as today.
  - **`bmesh spec rulings`** takes no flags, reads no config or repo, and prints `brief.Rulings`.
  - The manual's fixer section replaces its reworded list with the exact text, and a test checks that `docs/manual.md` contains `brief.Rulings`.
- **One check, written once.** A new section of `ideate-engineer`, between 4 (write) and 5 (land), holds the definitions, the reviewer prompt and the outcomes. `ideate`'s here-path says "check it as `ideate-engineer` section 5 says" (sections renumber). An idea thread knows to run it because `OperatorIdea()` (`internal/brief/brief.go:152`) gains "Before landing, check the spec as `ideate-engineer` section 5 says." That reaches it even when a repo overrides the engineer brief, and a round brief doesn't carry it. Rounds are out by the operator's call.
- **Classify.** A Haiku subagent reads the spec, the repo's `AGENTS.md` and its product doc (README or `PRODUCT.md`). It answers three lines, `<category>: yes|borderline|no (<one clause why>)`:
  - **load-bearing:** changes something other parts of the system trust or keep: stored records, data or wire formats, keys or IDs, a migration, the merge, release or deploy path, or the checks that gate them. A mistake ships or keeps something nobody checked.
  - **security:** auth, permissions, secrets or keys, signing, sensitive or personal data, network exposure, or acting on input from outside the trust boundary.
  - **cross-cutting:** reaches across most of the codebase, changes an interface other code or other repos depend on (a spec sized L), or changes config or text loaded everywhere.

  What a repo counts as load-bearing comes from its own `AGENTS.md`.
- **The strongest line decides.**
  - **yes:** an Opus review at `high` effort.
  - **borderline:** the session asks the operator in one line whether to run the review or skip it. An idea thread runs the review without asking: a review costs minutes, and an ask costs hours.
  - **no:** the spec lands as today.
- **In front of the operator** (the session only), the three lines are followed by an italic line, *Adjust the classification if it's off; just tell me.*
  - On yes, the review starts at once. A correction down to no lets it finish, and its findings are listed, not applied.
  - On borderline, the line and the question come together.
  - A correction after a no starts the review on the landed spec, and its fixes go in a second commit.
- **The reviewer** is a fresh Opus subagent, of a read-only agent type where the harness has one. It is forbidden to start a thread or job or to write a record: no `ideate`, `build`, `fix`, `lead start`, `executor new`, `routine queue` or `run`, `choose-ideas`, `spec set`, `land` or `done`, and no edit, commit or push. Its prompt has:
  - the spec's path and repo, and "read the code and specs it names", with nothing of the writer's reasoning;
  - "Read 'fix' as this spec's change and 'issue' as the gap its Opportunity names";
  - the output of `bmesh spec rulings` verbatim.

  A break of `root`, `failing-first`, `smallest`, `structure` or `drop` is a defect. A break of `premise`, `no-new-surface` or `reversible` is a note for the operator. The findings come back ranked, with evidence and a fix to the spec text. The writer fixes every defect, lists the notes and taste notes in the report, and runs no second pass.
- **When it can't run:**
  - With no subagent on the model, the session classifies the spec itself.
  - When `bmesh spec rulings` fails (an older binary), it reports that the review didn't run.
- **Recorded in words only**, in the Propose commit message and the report line. The forms:
  - `qa: no`;
  - `qa: <class> (<kinds>) → opus: N fixed, M notes`;
  - `qa: borderline (<kinds>), skipped by the operator`;
  - `qa: <class> (<kinds>), review failed: <why>`;
  - `qa: not run (<why>); review recommended`.
- **Docs:**
  - the manual's `bmesh spec` synopsis, and `specUsage` (`cmd/bmesh/cmd_spec.go:19`);
  - its Briefs section (`rulings` is embedded and never overridden);
  - its fixer rulings list (above);
  - its Skills section on `ideate` and `ideate-engineer`;
  - the README's command index.

Out: rounds, a second review pass, a classification file a repo sets, any record on the mesh, and the repo's `review` routine kind (it reviews a diff for bugs, with only `git diff` and `git log`, and doesn't fit a spec).

Judge: opus, because the skill text decides when an Opus review is spent, and the rulings' move must leave the fix brief exactly as it renders.

## What you'll see
"Spec this: ..." writes the spec, then shows:

```
load-bearing: yes (changes what land trusts as a verify result)
security: no (no keys, auth or outside input)
cross-cutting: borderline (touches cmd, daemon and selfupdate)
```
*Adjust the classification if it's off; just tell me.*

The review runs and the fixes go in. The report reads `069 ... (feature, M, opus): ... qa: yes (load-bearing) → opus: 6 fixed, 2 notes`. A README tweak shows three `no` lines and lands as today.

## Verification
The repo's verify, plus:
- `internal/brief`:
  - the fix brief renders identically before and after (a golden of today's render);
  - with `.bmesh/briefs/rulings.md` committed in the repo, the fix brief and `spec rulings` still print the embedded text;
  - `TestNames` is unchanged;
  - an idea brief carries the check sentence and a round brief doesn't;
  - `docs/manual.md` contains `brief.Rulings`.
- `cmd/bmesh`: `spec rulings` prints `brief.Rulings` from any directory.
- The skills test checks that `ideate-engineer` names `bmesh spec rulings` and the three categories, and that `ideate` points at that section.
- **A rehearsal, numbers in the closing commit:**
  - the classifier prompt, 3 runs each, on 068 as first proposed (`git show fe2c025:docs/proposals/068-*.md`, which should be yes), on the S operator specs 025, 030, 031 and 039, on 051 (map), and on all 36 operator specs if time allows. It reports agreement across runs and the share of `no`;
  - the reviewer, once, on fe2c025's 068, with whether it finds the four issues above.

## Risks and the alternative considered
- **Haiku calls a risky spec `no`.** The operator sees the three lines and the invitation, and a correction starts the review. An idea thread has no one watching, so its borderline already reviews. The rehearsal measures the miss rate before this lands.
- **The models are fixed in the skill text.** Changing Haiku or Opus takes a spec, by design: no config key.
- **The skill grows** by about 1 KB in `ideate-engineer` (6,886 bytes), loaded only by ideate sessions, and `ideate` (9,040 bytes) by a sentence.
- **Alternative: always review an idea's spec with Opus.** Simpler, but it spends a high-effort session on every doc tweak.
- **Alternative: copy the rulings into the skills with a match test.** No new command, but two more copies to keep in step, where the manual's copy has already drifted.

## Upgrading
Update the buildermesh plugin after `bmesh update`, not before. Skills that call `bmesh spec rulings` on an older binary report `qa: not run (bmesh has no spec rulings; update it)` and land without the review.

## Needs from the operator
