---
id: 106
title: The map becomes a console, with KPI tiles, the needs-you band and a fleet timeline of every lead's states over 1h to 7d in place of the tree and the done shelf
kind: ux
size: M
judge: opus
status: proposed
lens: ux
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity

The operator (2026-10-24): the map "is ugly and lacks a real command console dashboard feel"; "I want to observe charts and timelines".

The real map that day (`assets/106-map-before.png`, 1600×1000) draws:
- one working lead;
- an `orphans` block of eight stacked fixer boxes;
- a `done (107)` shelf of chips filling the rest of the screen.

It has no time axis and no numbers. Nothing on it shows that `buildermesh-096` was refused at land five times in 25 minutes, how long anything took, or what ran beside what.

Everything a timeline needs is already on `bm-mesh`, which keeps 30 days:
- the nodes' `started`, `progress`, `ask`, `blocked`, `held`, `ready` and `done`;
- the daemon's `silent`, `landing`, `land-refused` and `land-withdrawn`, addressed by `to`;
- the routine instances' `started` and `done`.

The page replays only 24 hours of it, and only into the recent-events drawer.

The operator liked the mockup built from k16's data for 10-24 (`assets/106-console-mock.png`, `106-console-mock-phone.png`). The page itself is `assets/106-console-mock.html`, a self-contained file whose CSS custom properties are this spec's palette. This spec builds its frame: the look, the KPI row, the band and the fleet timeline. 107 adds the token panels and 108 the flags, outliers and burndowns into the same grid.

## Proposal

In:

- **The look**, as the mockup:
  - **Dark first**, from the mockup's custom properties:
    - surfaces `#111110` (page), `#1a1a19` (panel), `#232321`;
    - lines `#2e2e2b`;
    - text `#f0efec`, `#c3c2b7`, `#8d8c84`;
    - the dark categorical steps in their fixed order (`#3987e5 #d95926 #199e70 #c98500 #d55181 #008300 #9085e9 #e66767`);
    - the status colours `good #0ca30c`, `warning #fab219`, `serious #ec835a`, `critical #d03b3b`, used only with an icon and a label.
  - **Light** is kept under `prefers-color-scheme: light`, with the light steps of the same palette (categorical `#2a78d6 #eb6834 #1baf7a #eda100 …`, surface `#fcfcfb`).
  - **Type:** monospace tabular numerals for every number, path and time; small-caps panel titles with a one-line muted subtitle.
  - **Drawing:** charts are inline SVG drawn by `map.js` with no library, with 2px lines, 13px state bars with 3px corners, and recessive grid lines.
  - The page keeps no scroll sideways at any width.

- **The overview (`/`)**, top to bottom:
  1. **The top bar:**
     - `BMESH`;
     - the connection state (`● live · <host> hub`, else what it is);
     - tabs `overview` · `timeline` · `routines` (the routines page as it is);
     - window chips `1h 6h 24h 7d` (default `6h`, kept in the URL hash and across reloads);
     - the hub's clock in UTC.
  2. **The KPI row**, six tiles with a small caps label, a large mono value and a mono subline:
     - `Live agents`: count, and up to three paths;
     - `Landed`: leads `done` in the window, and their spec ids;
     - `Land refused`: `land-refused` events in the window, and the node with the most;
     - `Asking / blocked`: count;
     - `Quota`: the strip's readings, each provider's 5h and week, a meter for the higher week and its reset;
     - `Routines`: queued jobs and running instances, linking the routines tab.

     The strip's summary row and quota lines move into these tiles. The digest fold stays, under the row.
  3. **The needs-you band**, its rows and rules as now, restyled as the mockup: a 3px left rule in the row's state colour and a mono badge with an icon.
  4. **The fleet timeline**, full width (107 puts a panel beside it).

  The recent-events drawer stays as it is. Gone from the overview: the lead blocks, the `orphans` block, the done shelf and the routines column.

- **The fleet timeline**, served as `/api/timeline?since=1h|6h|24h|7d`:
  - **Built in Go** (`internal/web/timeline.go`) from `bm-mesh` over the window, plus each node's state at the window's start.
  - **What it serves:** `{from, to, lanes:[{path, role, parent, repo, spec, state, segs:[[from, to, state]], ticks:[[at, kind, text]]}]}`.
  - **The state machine** is the one the map applies live today, in one table:
    - `started` and `progress` are `working`;
    - `ask` is `asking`, `blocked` is `blocked`, `held` is `held`;
    - `ready` and the daemon's `landing` are `landing`;
    - the daemon's `silent` is `silent`;
    - `land-refused` and `land-withdrawn` are `working`;
    - `done` ends the lane;
    - a node registered but not yet `started` is `idle`, drawn dashed.

    `map.js` only draws it and extends each lane's last segment with live events.
  - **Ticks:** `progress` (a 2px bar across the lane), a job queued (`◆`), `land-refused` (`✕` in the serious colour), `ready` (`▲`), `done` (`✓` in good). Hovering a segment shows its state, span and duration; hovering a tick shows its event's time and words. Clicking opens the lead page.
  - **Lanes:**
    - one per lead, round and fixer with a segment in the window, ordered by need (as the blocks are now), then by start;
    - each lane's label is its path, with the repo's prefix shortened when every lane shares it (`bm-096`);
    - a lead done before the window isn't drawn, which replaces the shelf; `7d` shows the week;
    - executors and a round's engineers fold into their parent's lane on the overview (`+2` after the label) and get their own thinner lanes, under their parent, on the `timeline` tab.
  - **The `timeline` tab** also draws, in a group at the bottom, one lane per routine instance with a job in the window, each job a segment from `started` to `done`.
  - **Live:** live events redraw the timeline at most once a second, and a dashed `now` line moves every 30 seconds.

- **The lead page** (`#/lead/<path>`, which pushes' links keep opening):
  - a head line: state badge, spec id and title, host, model, started, duration;
  - then the node's lane and its children's lanes over its whole life;
  - then the cards it has today.

- **Narrower than 1000px:**
  - tiles in 3 columns (2 under 480px);
  - band rows wrap their words under the badge;
  - the timeline's label gutter shrinks to 118px, with an hour tick each hour;
  - the drawer as now.

- **`scripts/seed`** back-dates its events (payload `at`) over the last 6 hours, so every state segment and every tick kind shows on the default window. `seed.sh` is otherwise as it is.

- **`scripts/check.mjs`:**
  - It waits for a drawn timeline (`lanes=` at least 1) instead of `trees=`.
  - The `?check=1` title adds `lanes=`, `segs=<state>:<n>,…`, `ticks=` and `kpis=`.
  - It exits 1 when any of `working`, `landing`, `held`, `silent`, `asking` or `blocked` has no segment on the seed, when `kpis=` isn't 6, or when a held or landing band row appears (as now).

- **Docs:** the manual's Map section rewritten (the console, the tiles, the timeline, the lead page) and the internals' Map (the `/api/timeline` shape, the state table, the ordering).

Out:
- tokens, context and quota history (107);
- runaway flags, outliers and burndowns (108);
- a chart library;
- any change to the band's rows or to what pushes link;
- the recent-events drawer's design.

Order: beside 105 (no shared files but the docs); before 107.

Judge: opus, because the mockup sets the taste and the work is in the state machine and in keeping the band, the drawer and the lead page whole.

## What you'll see

The console in `assets/106-console-mock.png`, without the token panels yet:
- the KPI tiles;
- the band;
- a timeline in which 096's five ✕ sit in its landing segments, 101's silent stretch shows yellow, and the afternoon's batch reads left to right.

`7d` shows the week's leads, and nothing done hides the live ones.

## Verification

- The repo's verify.
- **Go tests for `/api/timeline`:**
  - each state transition;
  - a lane already working at the window's start;
  - a daemon event setting the state of its `to`;
  - done ending a lane;
  - executor lanes under their lead;
  - routine jobs;
  - window bounds.
- **The browser check** on `scripts/seed.sh`: `node scripts/check.mjs <url> 1600 1000 <png>`, then `1000 800` and `390 844`, all exit 0, with the screenshots in the closing commit.
- **The real map** on k16, built from the branch with `BMESH_WEB_DIR`, at 1600×1000 on `24h`: 096's five refusals as ✕ on its lane, the screenshot in the closing commit.

## Risks and the alternative considered

- **The tree's hierarchy is lost.** Sub-lanes on the `timeline` tab and the `+N` fold keep it; the lead page keeps its `tree` card.
- **A 7-day window is heavy.** A week of events was about 3,000 messages on k16, so the window is read per request and cached for 30 seconds.
- **Alternative: restyle the tree and keep it.** It still has no time, and done leads still need a shelf.

## Upgrading

nothing

## Needs from the operator
