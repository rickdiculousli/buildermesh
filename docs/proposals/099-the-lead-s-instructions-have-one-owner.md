---
id: 099
title: The build brief carries only what it alone knows; procedure lives in the lead skill and the binary's output, and the skill test measures the brief
kind: maintenance
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity
A lead reads three layers that each restate verify, review and land: the build brief (`internal/brief/templates/build.md`, 1,437 words), the lead skill (1,901 words) and the repo's `AGENTS.md`, ranked by a precedence rule ("where it and this brief differ, it wins"). The article calls this the "Frankenprompt"; here the layers already disagree:
- `build.md:21` has the lead `routine wait` for its review in its turn; the lead skill (section 4, `skills/lead/SKILL.md:37`, and section 6) says never wait in a turn: queue it and end the turn, the reply wakes you.
- `build.md:30` says to run `bmesh done` "as the last thing"; its own Landing section and the lead skill say the daemon runs it after `ready`.

Lean step 5 moved procedure into the binary's refusals and measured it (lead-s: 199k tokens against 345k), but the brief kept the old procedure. And the skill test can't see the brief: every arm gets the brief this tree renders (`scripts/skilltest/README.md`, A cell), so brief text has never been measured. A lead's start is 42 to 44k tokens against 26 to 29k for a subagent (2026-10-24 transcripts). Executors are rare: none of 49 leads measured on 2026-10-18 started one, and 1 of the last 10 did. Yet the lead skill carries their mechanics (section 3, 287 words) for every lead.

## Proposal
In:
- **The skill test arms the brief.** A lead case renders its brief from the arm's own `internal/brief/templates/build.md`, so a brief change is measured as a skill change is.
- **The brief keeps only what it alone knows:** the spec, the node, the repo path and worktree, the operator's words, the row's verify commands and done rule, the spec's Upgrading note, and one line to follow `buildermesh:lead`. What the lead skill or a command's output already says (verify, review sizing, land's steps, done's checks, inbox and progress cadence, the final message's shape) goes; nothing the lead needs is lost. Where the lead skill lacks something only the brief said, it moves to the skill or to the binary's output, said once.
- **`bmesh executor new` prints the rest of an executor's brief** after the header: how to start it (Agent tool, in the background, its model, medium effort), how it reports (`done --commit`, `progress` at least every 15 minutes), when to resume it and when to start a fresh one. The lead skill keeps when to hand off and why, and points at that output.
- **An executor case in the skill test**: the lead's brief carries operator words asking for one named piece to go to an executor; scored on `executor new`, the executor's `done`, its branch merged, `executor remove`, then one `ready`, with the case's hidden test passing on the lead's branch.
- The manual (`executor new`'s output, the brief's contents), internals and the README where they describe the brief.

Out: the fix, engineer and half-pager templates; the lead skill's judgment (shape of the work, when to ask, sizing); repos that override `build.md` in `.bmesh/briefs/` (they keep their text).

Judge: opus, because cutting the brief must not drop a step only it carried; each removed sentence needs a home or a reason it's dead.

## What you'll see
A lead's first message is the spec and a dozen lines of facts. `bmesh executor new` prints a brief the lead pastes and completes.

## Verification
The repo's verify (the brief's tests updated to the new render). Skill test, base (main before this) against cand, `--cases lead-s,lead-stop,lead-exec --runs 2`: no case that passes on base fails on cand; tokens and cost per arm in the closing commit.

## Risks and the alternative considered
- **A lead misses a step the brief used to spell out.** The lead cases cover verify, ready, the stop's inbox read and the executor chain; a missed step fails a named expectation.
- **Alternative: keep the brief and delete from the skill.** The skill is what every lead on every box loads by version; the brief is per spec and was never measured.

## Upgrading
nothing

## Needs from the operator
