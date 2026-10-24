---
id: 100
title: The work skill keeps the ladder; the steps for running leads, releasing and compacting come from the commands that need them
kind: token-cost
size: M
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity
`skills/work/SKILL.md` is the plugin's largest skill (2,642 words) and loads on every request in a registry repo. About 43% of it matters only after a specific step: "Running leads and rounds" (668 words: watch, relay, hold, stop, wipe, the Fable fallback, feedback, releases, status) and "Long sessions" (482 words: the state file, compacting, effort). Lean step 5 did this for the lead skill: procedure moved into the binary's refusals and output, and the skill kept judgment (3,717 to 1,839 words; lead-s 199k tokens against 345k). The work skill has grown since (8 edits since a51e2c6). The article: load a procedure when it's needed, and keep instructions in one place.

## Proposal
In:
- **The skill keeps** the read-first steps, the ladder with each rung's judgment, what's never cut, and the end line. Target about 1,400 words.
- **Each procedure moves to the output of the command that needs it,** said once:
  - `bmesh build` ends with how to watch the leads it started (`bmesh watch` with their paths, re-arming on exit 5) and how to relay, hold, stop and wipe one;
  - `bmesh watch`'s `watch-done` line says to run `bmesh feedback list <family>`, and `feedback list` ends with how each kind of issue is handled and drained;
  - `bmesh release <repo> --dry-run` ends with the steps before a release: the upgrade notes from the specs it names. What is only this repo's (levelling ponytail first) moves to this repo's `AGENTS.md`;
  - the context line (094), when it advises compacting, says how: the state file, then the two queued messages, or the `/compact` alone when the operator asked for a pause;
  - the Fable fallback and the effort rule go where the runner's error and the context line already speak.
- **The effort rule is corrected** where it moves (operator, 2026-10-24): a session changes its own effort only after a compaction has run, as the first step of the resumed turn, never before queueing the `/compact`: a change before it makes the compaction reread the whole long context with a cache miss. The skill's "high before queueing a `/compact`" is wrong and goes.
- **Subagents the coordinator starts** (not a lead's executors): when the coordinator hands a change to a subagent instead of making it itself, the subagent verifies, reviews and lands its own work and reports one line, so the coordinator reads the line, not the diff. The 2026-10-24 study found the coordinator spent 31% of a subagent-built stretch checking and landing their work at up to 537k tokens. (This bullet holds only if the operator said yes to it before this lead started; otherwise it is Out.)
- The skill test's fake prints what these commands now print.
- The manual's sections for each command's output.

Out: the ladder's rungs and their order; what a rung decides; the lead and executor skills.

Judge: opus, because every sentence removed from the skill needs a home where an agent will meet it at the moment it applies.

## What you'll see
A request in a registry repo loads a skill about half the size; `bmesh build` and `release --dry-run` print the next steps where the session needs them.

## Verification
The repo's verify (tests for each command's new output). Skill test, base (main before this) against cand, every chat case, `--runs 2`: no case that passes on base fails on cand; tokens and cost per arm in the closing commit.

## Risks and the alternative considered
- **A step the session needs before running the command** (knowing to watch at all) is lost. The ladder keeps that a lead is watched and relayed; only the how moves.
- **Alternative: split the skill into several skills** (run leads, release). Each is another description in every session's listing, and the model must pick it.

## Upgrading
nothing

## Needs from the operator
