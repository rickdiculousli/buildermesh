---
id: 108
title: The console flags runaway and overworking agents by fixed rules against the fleet's own baselines, tells watch about a node's flag, and draws outliers and spec burndowns
kind: feature
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity

The operator (2026-10-24) wants to "spot runaway and overworking components", and burndowns. With 105 to 107, the numbers are on the screen, but a person still has to read every row to find the agent that is burning.

Measured on k16:

- **The same work, ten times the tokens.** Leads finished since 10-15 read a median of 4.8M for an S spec, 10.2M for an M and 20.8M for an L, with maxima of 66.5M, 69.8M and 58.6M.
  - `buildermesh-096` (M) read 42.2M, 4.1× its median, and land refused it 5 times (3 rebase conflicts in `scripts/skilltest`, 2 failed reviews).
  - `buildermesh-098` (M) read 6.8M and landed on its first try.
- **A runaway reads more in an hour than a lead does in its life.** Over the six days to 10-24, two agents of 110 read more than 100M tokens in one hour (178M and 154M; the second is coordinator `ec03a02c`). Four more read over 45M in an hour. The heaviest lead read 42M.
- **Context past the point of compacting.** `ec03a02c` ran to 537k, past `[context] compact_soon` (400k). 094 told the session itself, but no one else.
- **Nothing tells the coordinator.** A lead's land refusals reach it one wake at a time, and its token use not at all.
- **No burndown.** `bmesh spec list` says what is open now, but not how a batch went down over its afternoon, nor how the backlog moved over a month (nothing landed on 10-18 to 10-21; 18 specs landed on 10-22).

## Proposal

In:

- **Baselines.**
  - Per repo and spec size (`XS`, `S`, `M`, `L`), and for fixers per repo, from the done leads and fixers with usage in the stream's 30 days: the median read and the median wall time from `started` to `done`.
  - A repo with fewer than 5 done leads of a size takes the fleet's median for that size. With fewer than 5 in the fleet too, the size rule is silent.
  - Repos differ: opengym's S specs had a median of 35.6M against buildermesh's 3.0M.
  - The baselines are computed hourly where they are used (the daemon, the web server) and cached.

- **The rules**, in one table in `internal/usage` (`Flags(agent, baselines, cfg) []Flag`, each flag `{level, rule, words}`):

  | Rule | Overworking | Runaway | Applies to |
  |---|---|---|---|
  | `size` | read past 2.5× its size's median | past 5× | leads and fixers |
  | `refused` | land refused 3 or more times | | leads |
  | `context` | | main-thread context past `[context] compact_soon` | every agent |
  | `burn` | read past 50M in the trailing 60 minutes | past 100M | every agent |

  - **The words** name the numbers and the baseline, e.g. `read 42.2M, 4.1× the buildermesh M median (10.2M)`, `land refused 5× (3 rebase conflicts, 2 reviews failed)`, `context 537k, past compact_soon (400k)`, `read 154M in the last hour`.
  - **Calibration:** the thresholds come from 10-15 to 10-24 as measured above, and are fixed in code, not config. On those 90 leads and fixers:
    - `size` flags 6 as overworking (096 and 101 among them) and 2 as runaway: `buildermesh-039` at 5.8× its repo's S median and `opengym-rick-003` at 6.9× its repo's M;
    - with fleet-wide medians, it would have flagged four of opengym's S leads as runaways too;
    - `burn` flags the two hours over 100M as runaway;
    - `refused` flags 096.

- **On the console:**
  - **The needs-you band** gets one row per flagged live agent (a node not done, or a coordinator session with a call in the last 60 minutes), after its `silent` rows.
    - The row shows `▲ runaway` in critical or `▲ overworking` in serious, the agent (linking its lead or session page) and the words, the worst rule first.
    - The row leaves when the flag clears, or when the agent is done.
  - **The agents table** gets a `watch` column with every flag's words, for live and done agents alike, and its first cell is ruled in the level's colour.
  - **The timeline** draws a `▲` in the level's colour after a flagged lane's label.
  - **The KPI row:** the `Land refused` tile becomes `Flags`, giving the counts of runaway and overworking agents and the land refusals in the window.

- **On the mesh**, for nodes:
  - **The event.** Each box's daemon checks its own nodes after each usage pass (105) and publishes `bm.daemon.<host>.runaway` once per node, rule and level. The event's `to` is the node and its `text` is `<level> <path>: <words>`; the words are as above.
  - **Once only.** The flags announced are kept in `<state dir>/usage/flags.json`, so a restart announces nothing twice.
  - **`bmesh watch`** prints the event for the nodes it watches.
  - **`bmesh mesh-status`** lists each live node's open flags after its asks and blocks.
  - **No push:** a flag is not pushed to the phone.
  - **Sessions** get no event: no node watches a coordinator, and 094's line already tells the session its context.

- **The outliers panel** (row 3, a third, beside the agents table, as in `assets/106-console-mock.png`):
  - **What it plots:** every lead and fixer with usage in 30 days, `x` its wall minutes, `y` its read.
  - **Colours:** by size, S, M and L in the first three categorical slots (the only slots that pass the all-pairs check), with XS and fixers in muted grey.
  - **Markings:** flagged points are ringed in their level's colour and labelled. There's no threshold line, because the baselines are per repo.
  - **Hover:** path, size, read, wall time, peak context and turns.
  - **Click:** the lead page, with `vs` (107) set to the done lead of its size nearest that size's median read.

- **Burndowns**, served by `/api/specs?repo=R`:
  - **Per repo:** for the last 30 UTC days, bars for specs landed that day and a line for specs open at the day's end (`proposed` or `accepted`, not landed). Rejected specs are left out, having no rejection date.
    - A spec's proposed day is the commit that added its file.
    - Its landed day is the commit whose `Spec:` trailer names it, or, before trailers (098), the first commit whose diff sets `status: done`.
    - It is read by one `git log -p` over `docs/proposals/` on the repo's local `origin/main`, cached per head. The background fetch is the lead page's.
  - **Per build family** (the nodes' `family`):
    - a step line of the family's specs still open, from its first lead's `started`;
    - a step down at each lead's `done`;
    - its leads' land refusals as `✕` ticks;
    - a dashed `now` while any lead is live.
  - **Placement:**
    - The overview's row 5 holds the newest family's burndown (two thirds) and the busiest repo's 30 days (one third).
    - A `specs` tab holds every configured repo's 30 days and the families started in 30 days, newest first.

- **`scripts/seed`:** a runaway node (a `burn` flag) and an overworking one (`size` and `refused`), and two families, one finished.
- **`scripts/check.mjs`:** the title adds `flags=`, `outliers=` and `burndown=`, and the check exits 1 when the seed's runaway has no band row or either chart has no marks.
- **Docs:**
  - **The manual:** the console's flags, rules and words, the outliers and burndowns, the `runaway` event under `watch` and `mesh-status`.
  - **The internals:** the rules table, the baselines, `flags.json`, `/api/specs` and its git reading. The daemon subject list gains `runaway`.

Out:
- pushing a flag to the phone;
- acting on a flag (stopping or holding the agent), which is the coordinator's or the operator's call;
- config keys for the thresholds;
- an ideal line on a burndown (specs carry no estimate);
- Codex.

Order: after 107.

Judge: opus, because a flag that cries wolf teaches the operator to ignore the band, so the rules must be checked against the fleet's real 30 days, not only the seed.

## What you'll see

The band says `▲ runaway session ec03a02c: context 537k, past compact_soon (400k); read 154M in the last hour` and `▲ overworking buildermesh-096: read 42.2M, 4.1× the buildermesh M median (10.2M); land refused 5×`, while they happen. A coordinator watching 096 reads the same line in `bmesh watch`.

The outliers chart shows 096 alone above the M cluster. The family's burndown steps down through the afternoon, with 096's ✕s on it, and the repo's month shows the four quiet days and the 10-22 surge.

## Verification

- The repo's verify.
- **Go tests:**
  - each rule at, just under and just over its threshold;
  - a size with fewer than 5 done leads raising no size flag;
  - the event announced once per node, rule and level across a daemon restart;
  - `watch` and `mesh-status` printing the flag;
  - `/api/specs` on a fixture repo whose history holds a trailer landing, a pre-trailer landing, a rejected spec and a family.
- **The browser check** on `scripts/seed.sh` at `1600 1000`, `1000 800` and `390 844`, exit 0, with the screenshots in the closing commit.
- **The real data:** the rules run over k16's 30 days (the 105 stream on a scratch hub, as 105's check builds it). The closing commit lists every flag they raise on 10-15 to 10-24, and each one has to name a run the operator would call heavy. Thresholds are changed in the spec, by the lead, before landing when the list is noisy.

## Risks and the alternative considered

- **Crying wolf.** The rules are checked against the fleet's real 30 days before landing, the size rule compares within a repo and needs 5 samples, and flags never push.
- **The baselines drift** as skills change what a lead does. Taking the median over a rolling 30 days follows the fleet.
- **Alternative: relative rules** (top 5% of agents this week). There is always someone in the top 5%, so they flag someone even on a calm day.

## Upgrading

nothing

## Needs from the operator
