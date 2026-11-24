---
id: 129
title: Tests run as narrow and as shallow as their phase allows, an executor's brief names its tests, and a slow command an agent hits becomes a line in the repo's AGENTS.md
kind: test-speed
size: M
judge: opus
status: done
lens: eng
proposed_by: 6df9de23-3ba1-4c33-8273-21cdd370289e
proposed_on: 2026-11-24
---

## Opportunity
128 gives an agent's long commands `bmesh run`. This spec says how wide those test runs should be in each phase of a build, and where an agent writes down a slow command so the next session doesn't pay for it again.

**A narrow run is 18 to 140 times faster here.**
- One test takes 1.2 to 7.8 s: `go test -run '^TestPooledKindDrainsOnGoAndOnIdle$' ./internal/daemon`.
- `internal/daemon` alone takes 143 s.
- The suite takes 3m05s (at load 16 on k16).

**Agents run wide while they build.** A regex over k16's 11,158 Bash calls by leads, executors and rounds counted wide runs (a package or the suite, with no test named):
- **This repo:** 23% of test runs but 63% of test time (42 of 68 minutes).
- **party-buzzer:** 50% of runs and 82% of time.
- **Leads, across repos:** 192 wide runs, 126 minutes, 34% of their test time. Their skill already says "While building, run the tests you touch; the whole suite is the verify routine's" (`skills/lead/SKILL.md:37`).
- **Executors:** 104 wide runs, 78 minutes, 21% of their test time. Neither the executor skill nor the brief template names any tests.

**Two repos wrote phase rules down by hand.**
- party-buzzer's AGENTS.md: "After a UI change, run `npm run test:review` in full".
- eggnogg-rework's `.agents/delegation.md`: "A writer runs the cheap validators plus the scoped fixture validator for its crate", with the full set once before a merge.

**bmesh's `tests` rule set** (`internal/scaffold/rules/tests.md`) says what to test, never how much of the suite to run or when to go deep.

**A slow command one session learns about dies with that session.**
- party-buzzer's AGENTS.md has one such line, written by hand: "The first `npm run typecheck` in a fresh worktree is a full check (slow)".
- This repo's AGENTS.md says nothing of its 3-minute suite.
- The lead skill sends "the repo's tooling" to `bmesh feedback` (`SKILL.md:52`). A coordinator drains that later, and meanwhile the next lead runs the same slow command.
- Only the work skill makes "a lesson worth keeping a rule in `AGENTS.md`", and only for coordinators.

## Proposal
In:

- **The `tests` rule set gains two lines.** They go in `internal/scaffold/rules/tests.md`, in the manual's Rule sets section, and in this repo's AGENTS.md `tests` block. They reach other repos at their next scaffold; an edited block is kept, as now.
  - "Test as narrow as the phase: while changing code, the one test that covers it, by name (else its file); when a piece is done, its package or module; the whole suite once the code is final, as the repo's verify."
  - "Go deep as late as you can: fakes and unit tests in the loop; the real entry point when a piece is done; browsers, devices and repeated runs once, before landing."
- **Lead skill:**
  - **Verify and review.** "While building, run the tests you touch; the whole suite is the verify routine's." is replaced by: "While building, run the one test that covers what you changed, by name; its package when a piece is done; the whole suite is the verify routine's, once the code is final."
  - **Delegate.** "once you can write a done condition you can check" becomes "once you can write a done condition you can check and name the tests that check it (files and test names, and the command that runs them)".
  - **Land.** "the repo's tooling" leaves the feedback paragraph's list, and this sentence takes its place: "A command of the repo's that ran far longer than its phase needed (the suite where one test would do, a cold first run, a wait on a timer) gets one line in the repo's `AGENTS.md`, in its section on running and testing, on your branch: what is slow, why, and what to run instead. Delete a line that no longer holds."
- **Executor skill**, While you work, one sentence added (it has none on tests today): "In your loop, run the tests your brief names, by name; when the piece is done, their package."
- **The executor's done text** becomes `<what's there>[; slow: <command>: <why>]; Verified: …`, so `Verified` stays last for the lead's `Verified: none` check. It changes in the executor skill and on both lines of `internal/brief/templates/executor.md`. The lead puts a `slow:` clause into `AGENTS.md` as above.
- **The brief template's piece section** gains `- Tests: <files and test names, and the command that runs them>`.
- **This repo's AGENTS.md** gets its line from this round's measurement, beside its `TMPDIR` testing sentence: "`go test ./...` takes about 3 minutes, 2½ of them in `internal/daemon`; while changing code, run the one test (`go test -run '^TestX$' ./internal/daemon`, under 10 s)."
- **Each skill and template change** is measured with `scripts/skilltest` against today's text.

Out:
- a detector for a run slower than usual. Nothing exists to compare against: none of 82 measured verifies passed twice its repo's median, and agents' long commands never repeat their command line.
- a scaffold-managed practices block. Scaffold owns only marked blocks, and land's drift check refuses a ship repo whose blocks differ.
- a check in the binary that a run was too wide. A wrapper would see only the commands routed through it, and most test runs are direct.

Judge: opus, because every lead and executor loads these sentences, and they have to change what an agent runs, as `skilltest` measures it.

## What you'll see
- An executor's brief names the tests that check its piece, and the executor runs them by name while it builds.
- Leads run one test by name while building, and the suite once, as verify.
- Each scaffolded repo's AGENTS.md carries the two lines in its `tests` block.
- This repo's AGENTS.md names its slow suite and the narrow command.
- An executor's done line can carry `slow: <command>: <why>`, and the lead's landed change carries the line it became.

## Verification
- **`scripts/skilltest`:** the lead and executor cases run against today's skills and the new ones.
  - Under the new skills, the build loop's test runs name a test (a `-run`, a file or a test name) more often than under today's.
  - The lead case's executor brief names its tests.
  - The case that flips is named.
- **A scaffold test:** a `tests` block written today is rewritten with the two lines, and an edited block is kept.
- **A brief test:** the executor template renders the done text with `slow:` before `Verified:`, and has the `Tests:` line.

## Shown
- An executor's brief names its tests: `TestExecutorNewMakesWorktreeBranchNode` (cmd/bmesh) checks the brief's `- Tests: <files and test names, and the command that runs them>.` line; `go run ./scripts/skilltest --arm base=32e54f5 --arm cand=. --cases lead-s,lead-exec --runs 2` -> `lead-exec cand 1 True the executor's brief runs go test ./cart -run TestReceiptSaysTotal -v` (from its results.json).
- Executors and leads run one test by name while building: the same run's summary, `named tests` per arm -> `| base | 4 | 50% | 0 | 0 | $1.22 | 173s | 0/16 |` and `| cand | 4 | 100% | 0 | 0 | $1.20 | 178s | 9/14 |`; lead-s alone -> `| lead-s | base | 2 | 2/2 | 0 | 0 | $0.45 | 64s | 494k | 24 | 0/2 |` and `| lead-s | cand | 2 | 2/2 | 0 | 0 | $0.46 | 70s | 499k | 24 | 2/2 |`.
- Each scaffolded repo's `tests` block carries the two lines: `TestApplyRulesBringsAnOldTestsBlockUpToDate` (internal/scaffold) rewrites a block scaffold wrote with the old set and keeps an edited one; `TestTheManualHoldsEveryRuleSet` holds the manual to the set; this repo's block is rendered by `scaffold.ApplyRules` (marker hash `3daa1588`).
- This repo's AGENTS.md names its slow suite: `grep -c "go test ./...\` takes about 3 minutes" AGENTS.md` -> `1`.
- An executor's done line can carry `slow:` before `Verified:`: `TestExecutorNewMakesWorktreeBranchNode` checks `<what's there>[; slow: <command>: <why>]; Verified: ...` in the header and in the piece's report line; the executor skill and the manual's `bmesh executor new` say the same.
- skilltest's case that flips: the run's summary -> `- lead-exec run 1: base fail, cand pass` and `- lead-exec run 2: base fail, cand pass` (expectation `executor_brief_tests`; it was tightened, after the first scoring, from "names a test" to "gives a `go test` that names a test", which the spec's "the command that runs them" asks, and the kept cells rescored with `--rescore`).
- Easy to miss: `Verified` stays last on both of the executor template's lines, for the lead's `Verified: none` check: `TestExecutorNewMakesWorktreeBranchNode` checks the header's line and the piece's `... | none"` alone, once it holds` line.
- Not shown: the lead's landed change carries the line an executor's `slow:` became: no executor in the skilltest run reported a `slow:` clause, and no live build has run under the new skills.

## Risks and the alternative considered
- **Leads already break a sentence of this kind** (34% of their test time is wide). So the measure is `skilltest`'s count, not the text. If the new sentence doesn't move it, the next step is a check, not another sentence.
- **The text is in every session's context.** The two rule lines add about 350 bytes to each scaffolded repo's AGENTS.md. In the lead skill, the sentences replace the ones they rewrite.
- **Alternative: a repo-specific test selector** (changed packages to their tests). It needs per-language tooling in every repo, where a named test in the brief needs none.

## Upgrading
The `tests` rule set gains two lines on how wide and how deep to test in each phase. `bmesh scaffold` writes them into each repo's AGENTS.md at its next scaffold, and keeps a `tests` block you have edited.

## Needs from the operator
