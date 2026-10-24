---
id: 109
title: bmesh build hands the session the rung line its report opens with
kind: refinement
size: M
judge: opus
status: done
lens: eng
proposed_by: buildermesh-fix-8
proposed_on: 2026-10-24
ruled: [structure, failing-first, smallest]
fix_of: 104
from_feedback: buildermesh-fix-8 (buildermesh-104) #3505
---

## Opportunity
The work skill asks the first message on a request to open with its rung's line (`skills/work/SKILL.md`, "Name the rung"; rung 5: "Open `Starting leads for 012 and 014`"), and the skill test's `rung_named` scores it. In the pick case, where the operator's pick starts leads, sessions miss it: `named none` in 3 of 12 pick cells over 103's four skill-test runs (`cache/skilltest/103-cand*` in its worktree) and 2 of 3 in 104's (feedback #3505 from 104's lead), while ideas and qa named theirs in every final cell. The cells show why: one session paraphrased after the fact ("I've started leads for 012 and 014 and declined 013"), another wrote nothing until its closing report after the watch ("Both leads have finished..."). `rung_named` reads every message of every turn, so the check is not the cause. The sentence is already in the skill. Under `structure` the line moves to the binary, which knows which leads it started, as `build` already ends with the `next:` lines of what follows.

## Proposal
In:
- **`build` names the rung line.** When any lead started, a fourth `next:` line follows the three on stderr: `next: your report on this opens Starting leads for 012 and 014` (one: `Starting a lead for 013`), with the ids of the specs that started, in the order they started. Nothing started prints none, as today.
- **The skill's sentence shrinks.** Rung 5's "Open `Starting leads for 012 and 014` (one: `Starting a lead for 013`)." becomes "Open with the line `build`'s last `next:` gives." ("Name the rung" stays: it serves every rung, and rungs 1 to 4 run no command.)
- **The skill test's fake** prints the same line after build's three, on an arm whose manual documents it (as it does the other `next:` lines today).
- The manual's `bmesh build` Output (four lines, not three) and the README index if it lists them.

Out: `ideate`'s output (ideas names its rung in every final cell); any other rung; `audit`'s words.

Judge: opus, because the line must read as the session's own opening, not as one more instruction it relays.

## What you'll see
After `bmesh build shop 012 014`, stderr ends `next: your report on this opens Starting leads for 012 and 014`, and the session's reply on the pick opens with it.

## Verification
The repo's verify. Failing first: a `cmd_build_test.go` case that `build` of two specs ends its stderr with `next: your report on this opens Starting leads for 002 and 003`, and of one with `Starting a lead for 002`; it fails before. Then a skill test of `pick` and `drain`, base (main before this) against cand, `--runs 3`: `rung_named` passes in every cand cell, no case that passes on base fails on cand, with the run's results in the closing commit. If cand still misses `rung_named` in a pick cell, report it with the cell's opening message rather than adding skill text (`premise`).

## Risks and the alternative considered
- **One more `next:` line to read.** It replaces skill words every session loads, and only the sessions that start leads see it.
- **Alternative: widen `audit.Named` to accept the past tense** ("I've started leads for"). It would pass the paraphrase but not the session that wrote no opening at all, and it loosens what `bmesh audit` reads in real sessions.

## Upgrading
nothing

## Needs from the operator
