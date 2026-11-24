---
id: 122
title: The map shows a landing as it runs: how long, each step against how long it usually takes, the command and its newest line, the review session's calls and last tool use, the lease holder, why a run ended, and a land gone quiet
kind: feature
size: M
judge: opus
status: done
lens: eng
proposed_by: 47745d6c-4585-4a15-b7a7-19807a4abecf
proposed_on: 2026-11-24
---

## Opportunity
Today a landing shows on the overview only as a name in the `Live agents` tile and a teal lane. Its lead page shows the daemon's landing text after the head line. `assets/122-lead-before.png` is bluke-003 at 04:19Z, 10 minutes into verify: `running the inline kinds on t:4208beec`, and nothing else about the landing.

What would answer "is it stuck?" is on the mesh already, in three reads nobody joins:

- **Which session runs the review.** The routine's `job <id> started` progress event, addressed to the lead.
- **What that session is doing.** Its model calls on the usage stream. For bluke-003's first landing, `bmesh usage routine.review.k16-opus-5-5-1 --calls` shows 51 calls in 6.9 minutes, each with what fed it (`Bash: Read TouchpadView part 1`).
- **How long it usually takes.** The repo's earlier review jobs, whose `job <id> ok|fail <ms>` events show Bluke's last three took 5.7, 6.5 and 6.9 minutes. The lead, asked, guessed 10 to 20.

Across the fleet, the 18 landing segments of the last 7 days ran a median 5.7 minutes and a p90 of 18 (`/api/timeline?since=7d`). Bluke's passing verifies took 10 to 12.5 minutes each, and only the daemon's log shows that.

This spec builds on 121's `land` object and needs 121 first.

## Proposal
In:

**`GET /api/landing[?lead=<path>]`.** Without `lead` it serves every node with a running land run; with `lead`, that lead's newest landing, whatever its state.

- **Running and quiet.** A run is running while its newest `land` event has no `end` and came within 2½ minutes (two heartbeats). A run with no `end` and no event for longer reads `quiet` with its age: a land killed past the daemon's 2-minute grace, or by a reboot. This is the stuck case. Running doesn't need the state `landing`: a lead landing in its own turn stays `working`.
- **Its shape:**
  `{path, repo, state, ready_at, runs: [{run, end, quiet, steps: [{step, from, to, text}]}], now, jobs, usual, review_usual, lease}`.
- **Its events.** The node's `land` events since its newest `ready` event, or since its newest run when it landed in its own turn. They are read on each request from the node's own subject (`bm.<path>.land`), not the timeline's 30-second cache. `now` is the newest event's `land` object. A step's `text` is the `text` of the event it began with.
- **`jobs`.** One entry per job the runs waited on.
  - Its routine instance and start come from the `job <id> started` progress event addressed to the node; its end and verdict from `job <id> ok|fail <ms>` (`jobStartRE` and `jobEndRE`). Both are read from `bm.routine.>` since the landing began, as `s.usage` reads it.
  - The instance's `host` and `model` come from its node. The job's `effort` comes from `mesh.FindJob`.
  - Its `calls`, `read`, newest `ctx`, and newest call's `cause` and `at` come from `usage.Read` of that instance since the job's start.
  - A job with no start yet reads `waiting`.
- **`usual`.** Per step and label, the median seconds a run spent in that step, summing its repeats in the run. It covers the repo's land runs over the stream's 30 days that went past the step, read from the timeline's cached read. `review_usual` is the median `<ms>` of the repo's review jobs. Each is present only once it has 3 samples or more.
- **`lease`.** While `now.step` is `lease`: the holder and, when the holder is a node, its newest `land` text.
- Cached 3 seconds.

**Step labels.** The rows use 121's steps.
- A `kinds` row is labelled by its kinds (`verify`).
- A `jobs` row is labelled by its jobs' kinds, or `review` when no job is waited on. That row shows the decision: `review: on the branch already (fail at t:ca2ec50d)`.
- `main` gets a row only when the run waited there.
- A step the run took twice gets a row each time.

**The lead page's `landing` card.** It comes first while a run is running or quiet, and after the spec card for the newest landing once that has ended. See `assets/122-landing-mock.png`: bluke-003's first landing at 03:54Z, rebuilt from its events and its review's usage.
- **Head.** `landing 16m 00s`, counted from `ready_at` (or from the run's start) and ticking.
- **Earlier runs,** each on one line: `run 1 · 12m 29s · checks, verify 12m 28s, review queued · ended: timed out waiting for job 3XANly81 (review); the daemon ran land again`.
- **A row per step of the newest run.** Each row has a glyph (`✓` done, `▸` now, `○` ahead), its time (ticking for the step now), and `usually 12m (4)`. A running step past twice its usual reads `▲ 2.1× usual` in the warning colour. A quiet run's current row reads `no word from land for 4m` in the warning colour.
- **Under the running step,** whichever applies:
  - **The command:** its kind, the command clamped to one line, its newest line with that line's age, and `17 lines`.
  - **Each job:** for example `job 3XANly81 · claude-opus-5-5 high · routine.review.k16-opus-5-5-1 on k16`, then `running 3m 29s · 29 calls · read 2.4M · context 139k · last: Bash: Find animations in surface files · 8s ago`, then `usually 6m 30s (3)`. The instance links to its page. Before the job starts, it reads `waiting: no instance has taken it`.
  - **The slot wait:** its slot, holder and place.
  - **The lease holder,** linked to its page, with its landing text.
- **A land from a binary before 121** has no `land` object. Its card shows the text and `this bmesh publishes no steps`.

**The overview's landing rows.** They sit under the needs-you band, never in it, one per node with a running or quiet run. Each has the `▲ landing` badge, the path linked, and the landing's time and its running step on one line: `16m · review 3m 29s on k16, usually 6m 30s · 29 calls · Bash: Find animations in surface files`. A quiet run's row reads `no word from land for 4m` in the warning colour. With nothing landing there are no rows. Under 1000px the words go under the badge, as on band rows.

**Refresh.** The page reads `/api/landing` every 5 seconds while a landing row or a running card shows and the tab is visible. Times tick each second on the hub's clock.

Out:
- alerts or pushes about a slow or quiet landing;
- an ETA;
- a history page of past landings;
- land's printed lines;
- the box's load and processes (117 and 120);
- the session of a review run inline (see Risks).

Judge: opus, because the endpoint joins four sources and the card has to read at a glance at three widths.

## What you'll see
- **Overview:** one line per landing under the band, saying how long it has run, which step it is on against that step's usual, and the command's newest line or the review session's latest tool use. A land that has gone quiet says so.
- **Lead page:** a landing card with every step's time and its usual, the review's instance, calls, read, context and last tool use, the lease holder when it waits for one, and why an earlier run ended.

## Verification
- **`internal/web` tests on `meshtest`:**
  - a landing of two runs, the first ended by a timeout;
  - a run gone quiet;
  - an own-turn land whose lead is `working`;
  - a review job's start, end and usage calls (count, read, newest cause);
  - a repeated step, counted twice in its run's usual;
  - medians present at 3 samples and absent at 2;
  - a lease holder's text;
  - a land with no `land` object.
- **The seed and the browser check.**
  - The seed's landing lead app-0041 gains `land` objects for two runs. The first ran verify and timed out at the review. The second waits for that review job, which is started on a seeded routine instance with usage calls.
  - A new seeded lead, app-0043, is landing mid-verify with output lines, so both the command view and the job view are drawn.
  - The seed adds three earlier land runs of `app` that reached `push`, so `usually` draws.
  - `scripts/check.mjs` adds `landing=` (the overview's landing rows) and `landSteps=` (the step rows on `#/lead/app-0041` and `#/lead/app-0043`), and exits 1 when either is 0 on its view.
  - Screenshots at 390x844, 1000x800 and 1600x1000, dark and light.

## Shown
- The overview shows one line per landing under the band, with its time, its step against that step's usual, and the review's latest tool use or the command's newest line: on the seed, `CHECK_COLOR_SCHEME=dark node scripts/check.mjs "http://127.0.0.1:45863/" 1600 1000 <png>` -> `bmesh ok view=map ... landing=2`, and its screenshot reads `app-0041 20m · review 7m 03s on seed, usually 7m · 29 calls · Bash: Find animations in surface files` and `app-0043 6m · verify 6m 01s, usually 11m · PASS switch to web_buttons` under the operator row, outside `#needs` (`bandLanding=0`)
- The lead page's landing card shows every step's time and usual, the review's instance, calls, read, context and last tool use, and why the earlier run ended: `node scripts/check.mjs "http://127.0.0.1:45863/#/lead/app-0041" 1600 1000 <png>` -> `bmesh ok view=lead firstCard=landing landSteps=7`; its screenshot reads `run 1 · 12m 30s · checks, verify 12m 28s, review · ended: timed out waiting for job 9f2kQ (review); the daemon ran land again`, then `review 7m 29s usually 7m (5)` and `job 9f2kQ · claude-opus-5-5 high · routine.review.opus-5-5-2 on seed`, `running 7m 06s · 29 calls · read 2.4M · context 139k`, `last: Bash: Find animations in surface files · 21s ago`; on `#/lead/app-0043` the command view `verify · ./gradlew --no-daemon -q assembleD…`, `last: PASS switch to web_buttons · 34s ago · 17 lines`
- Two runs, the first ended by a timeout; a review job's start, end and calls (count, read, newest cause): `TestLandingTwoRunsAndItsReview` in `internal/web/landing_test.go`; `TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test -count=1 -run 'TestLanding' -v ./internal/web` -> `--- PASS: TestLandingTwoRunsAndItsReview (0.14s)`
- A run gone quiet (listed while landing, not once working); a job not yet taken reads waiting: `TestLandingQuiet`, `TestLandingJobWaiting` -> `--- PASS: TestLandingQuiet (0.05s)`, `--- PASS: TestLandingJobWaiting (0.05s)`
- An own-turn land whose lead is working; a land with no `land` object: `TestLandingOwnTurnAndBefore121` -> `--- PASS: TestLandingOwnTurnAndBefore121 (0.06s)`
- Medians at 3 samples and none at 2 (a kinds row answered from the record no sample), a lease holder's text: `TestLandingUsualAndLease` -> `--- PASS: TestLandingUsualAndLease (0.06s)`
- A ready not started reads waiting, not a land before 121; a withdrawn ready starts no later landing; a command keeps its words after a slot wait: `TestLandingReadiesAndSlotWait` -> `--- PASS: TestLandingReadiesAndSlotWait (0.05s)`
- The seed and the browser check: app-0041's two runs, app-0043 mid-verify and three landed runs of app (`scripts/seed/main.go` `seedLanding`); `check.mjs` reads `landing=` and `landSteps=`; 3 views × 390x844, 1000x800, 1600x1000 × dark and light all exit 0 (the executor's 18 runs; after the card moved above the lanes, the 1000x800 runs in both schemes on the three views -> `exit=0` each)
- Easy to miss: a step taken twice counts once in its run's usual, its stays summed: `TestLandingUsualAndLease` (lib-1 runs verify for 100 s and 200 s; the median is over 300, 700 and 800), and the main row only when the run waited there (lib-1 only fetched, so main has two samples and no usual)
- Not shown: a quiet run, a slot wait, a lease wait and a land before 121 drawn in the browser: the seed has none; the endpoint's tests cover each, the card's code for them was never drawn
- Not shown: a real landing on the map: k16's daemon runs the installed release (the step below)

## Risks and the alternative considered
- **The review's last tool use lags.** The usage pass runs every 30 seconds, and a call is published only once the next one shows it finished. So `last` always shows its age.
- **A review run inline is folded into the lead.** In a repo whose review kind is inline, the review runs in the lead's worktree, so its calls count as the lead's. Its step shows the command and its elapsed time only.
- **Alternative: give a landing a row in the needs-you band.** Rejected: a landing doesn't need the operator, which is why 080 and 106 keep it out. The landing rows sit under the band, on their own; only a quiet run takes the warning colour.
- **Alternative: read the land's processes, as 117's hosts tab will.** That shows Gradle and the emulator under the lead, but not the step, the review session or the usual. 117 is also paused while k16 is loaded.

## Upgrading
nothing

## Needs from the operator
- [k16] after the release carrying 121 and 122, open a landing lead's page on the map during a landing and check its landing card shows the step, the command's newest line or the review's last tool use, and the usual (the daemon runs the installed release, so no build can show a real landing before then)
