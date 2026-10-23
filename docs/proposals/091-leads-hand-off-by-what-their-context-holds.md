---
id: 091
title: Leads hand off work by what their context holds, pick the executor's model by the piece, and stop waiting inside a large context
kind: token-cost
size: S
judge: opus
status: accepted
lens: eng
proposed_by: operator
proposed_on: 2026-10-23
---

## Opportunity
A replay of 11 finished opengym-rick builds (specs 001-013 without 008 and 012: their leads, 12 executors and 12 review runs, priced at Opus 5.5 and Sonnet 5.5 list rates, within 2% of the measured bill) shows where a build's tokens go. Leads were $111 of $158 build spend. After its first edit a lead did $78 of hands-on work (1,038 calls) at an average context of 253k, 63% of those calls above 200k: test fixing across many files, edits carried through the repo, browser checks with screenshots, full test runs. Every call re-reads the whole context, so the same call costs two to three times as much at 300k as below 100k. The lead skill pushes that work into the lead: "your own hands for the core, or anything whose pieces depend on each other", "fan out only when a piece can really run apart" and "one executor doing the whole thing is a worse you".

The executors that did run were Opus for 11 of 12 ("model of your choice"), and some grew as large as a lead: spec 002's frontend executor reached 465k context and was resumed twice past 300k, for $25 against its lead's $18. Leads also spent about $9 waiting (sleeps, polls, repeated inbox and progress calls at an average 258k context), and about $20, 12% of spend, went on rebase conflicts between the five removal specs dispatched together (002-006 all edit the same frontend and API files), $12.7 of it one high-effort helper resolving spec 002's.

Size alone doesn't decide a hand-off; what the context holds does. Split three ways (setup: system prompt, tools, skills, brief; orientation: what was read to understand the change; work: the output of acting, test runs, edits and their scripts, screenshots, and the model's own output after its first edit), a lead at the middle of its session held 13-22% setup, 39-53% orientation and 30-46% work, and work overtook orientation by its end (45-56%). A fresh executor drops the work but reads again what its piece needs of the orientation. Replaying each lead's post-first-edit calls as fresh Opus executors in k pieces in sequence: when each re-reads a quarter of the lead's orientation, 2-3 pieces save 21%; at half, 11-13%; when each needs all of it, handing off costs 2% (one piece) to 36% (six pieces) more. Sonnet executors save 15-40% on the same grid (their cache reads cost what Opus's do, so only output and writes are cheaper). So a hand-off pays for a run of work that needs part of what the lead read, above all once its context is mostly work output, and too many pieces lose to re-reading. A single lead kept across specs (a "throughline") cost 2-39% more in every variant, so that is not proposed.

## Proposal
Wording in two skills; no code.

In (`skills/lead/SKILL.md`):
- §2.4: the lead's own hands are for the design and the core (the reading that decides the shape, code whose pieces depend on each other, judgment calls). Executors also take runs of settled work that need only part of what the lead read, even when they run one after another (a browser check that reports what it saw, the tests one area broke). The "worse you" line becomes the reason, in terms of what the context holds: setup, what was read, and the output of what was done. Hand off work that needs part of the reading, above all once the context is mostly output; keep work that needs most of it; cut as few pieces as keep each narrow, since each pays again for its share of the reading. Hand off only once the done condition can be written; never split one file between executors.
- §3.1-3.2: the executor's model by the piece, passed to `executor new --model`: Sonnet for a mechanical piece a careful junior could do from the brief, Opus for judgment. The brief asks for a `progress` at least every 15 minutes of quiet work, so a lead that ended its turn isn't marked `silent`; a piece that only checks reports `done` at the commit it started from.
- §3.3: resume an executor for a question, a correction or a next piece that needs most of what it read; otherwise start a fresh one, above all when it holds mostly the output of earlier work.
- §4: while executors or routines run and nothing else is to do, end the turn; the harness notice or the daemon's wake brings the lead back.
- §6: while building, run the tests the change touches; the whole suite is the verify routine's.

In (`skills/build/SKILL.md` step 2): specs whose Proposals name the same files are dispatched one after another in the same family (not with `--accepted`), and the family is drained once its last lead is done. The manual's `buildermesh:build` entry says so.

Out: any change to `bmesh`, its config or the brief; a cap on context size (a lead can't read its own token count); a throughline mode.

Judge: opus, because the wording has to keep the lead's judgment where it is while moving the grind out.

## What you'll see
Leads that hand narrow runs of settled work to executors once their context is mostly work output, Sonnet on mechanical pieces, fresh executors instead of resumes that carry stale output, and overlapping specs building in sequence.

## Verification
The repo's verify (the skill frontmatter tests read every `SKILL.md`). After the next opengym-rick batch, re-run `~/.cache/bmesh-scratch/analysis/throughline/{extract,profile}.py` on k16, and `compose.py`, and compare the lead/executor split, the setup/orientation/work split at hand-offs and resumes, and any Sonnet executor's rework against the numbers above; a Sonnet piece that its lead had to redo is the signal to narrow that line.

## Risks and the alternative considered
More executors means more re-orientation: each one starts at about 28k and re-reads its share of what the lead read; past three pieces, or when a piece needs most of the reading, the re-reading costs more than the dropped work output saves, which is why the rule turns on what the context holds and how much of it the next work needs. The classification is approximate: a file read just before editing it counts as orientation. Sonnet's quality on these pieces is unmeasured. The alternative, one lead across specs, cost more in every variant: what a later spec can reuse from an earlier one's context is a few percent of its reading, while the carried context is re-read on every call.
