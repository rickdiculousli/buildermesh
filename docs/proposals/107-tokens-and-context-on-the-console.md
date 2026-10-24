---
id: 107
title: The console shows tokens by role over time, every agent's totals, a coordinator's design and execution, context per model call with what grew it, and quota history
kind: feature
size: M
judge: opus
status: proposed
lens: ux
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity

105 puts every agent's model calls on the mesh, and 106 gives the map a console frame. Neither shows a token count to someone looking at the screen.

The operator (2026-10-24) asked:
- to see token use "per agent of any kind in totality";
- to see the "context size diff after each turn";
- for a coordinator's tokens to be split "between its design & discussion, and execution".

The mockup (`assets/106-console-mock.png`, `106-console-mock.html`) shows what that data says about 10-24:
- **Coordinators led the afternoon.** The tokens-by-role chart puts the coordinators and their subagents beside the leads.
- **One session topped the table.** The agents table puts `ec03a02c` first, at 416M.
- **096 grew in three steps.** Its context-per-call chart climbs to 347k. Its largest single growth, +19k, was a `Read cases.go`, and 098 levels off at 147k.

Quota is read every five minutes for the strip, and only the latest reading is kept, so a week's burn can't be drawn.

## Proposal

In:

- **`/api/usage?since=1h|6h|24h|7d`**, from `usage.Read` (105), cached 30 seconds per window, carrying:
  - **The KPIs:** read and out in the window, and read in the last hour.
  - **`agents`:** per agent, its path or session id, role, host, repo, a lead's spec size (from its spec), state, the four kinds main and sub, peak and last context, first and last call.
  - **`roles`:** read per time bucket for five series: `lead` (leads, fixers, rounds and their engineers), `sub` (executors and Agent subagents of nodes), `design` and `exec` (coordinator sessions and their subagents, by phase), and `routine`. The bucket is 1 minute on `1h`, 10 minutes on `6h`, 30 on `24h` and 3 hours on `7d`.
  - **`sessions`:** per coordinator session, its busy spans (runs of calls less than 5 minutes apart, each split where the phase changes) and its read by phase.

- **`/api/usage/agent?agent=<path|session.host.id>&since=…[&vs=<path>]`:**
  - the agent's calls (time, ctx, out, sub, phase, cause) and its compaction marks;
  - a node's `progress`, `ready`, `land-refused` and `done` times;
  - the box's `[context]` thresholds;
  - with `vs`, the second agent's calls, both on minutes since their first call.

- **The overview grid**, as the mockup:
  - **Row 2:**
    - **The fleet timeline** (two thirds). It gains the coordinator sessions in a group under the leads, each a lane of busy spans coloured by phase: `design` in violet (`#9085e9`), `exec` in magenta (`#d55181`). Neither colour is used by any state. A lane is labelled `session <id8>` with its repo on hover, and its read is shown in the gutter, as is each lead's.
    - **Tokens read by role** (one third), a stacked area of the five series in the fixed order `lead, sub, design, exec, routine`, coloured blue, orange, violet, magenta and yellow:
      - dark `#3987e5 #d95926 #9085e9 #d55181 #c98500`;
      - light `#2a78d6 #eb6834 #6250d6 #e87ba4 #eda100`;
      - in this order, every adjacent pair passes the palette validator's CVD and normal-vision floors in both modes (worst dark ΔE 13.2, light 16.3);
      - light magenta and yellow sit under 3:1 on the light surface, so the legend and the tokens tab's table carry them;
      - a 1.5px surface gap between fills, and the legend above. A crosshair tooltip gives each series and the total for the bucket.
  - **Row 3:**
    - **Agents** (two thirds), one row per agent with calls in the window, most read first: agent (`●` when live), role, spec size or repo, state, read, a main·sub bar (blue · orange), peak context, wall time, out. Clicking a row selects it for row 4.
    - **Coordinators: design and execution** (one third), one row per session in the window: a stacked bar of design (violet) against exec (magenta) read, both numbers and the design share, peak context, last call.
  - **Row 4:**
    - **Context per model call** (two thirds), for the selected agent. With nothing selected, the live agent with the largest context is shown, else the window's largest reader.
      - The upper chart is the main thread's `ctx` per call over the window's time axis. It carries dashed reference lines at `nudge_from`, `compact_from` and `compact_soon`, labelled, and a mark at each compaction.
      - The lower chart has one bar per call for the growth it added, its hover giving the growth, the time and the cause. The two largest are labelled, and the node's `✕` and `▲` ticks run along its axis.
    - **Quota** (one third): each provider's 5h and week percent over the window on one percent axis, with each reset as a faint line.
  - **Two more KPI tiles:** `Tokens read` (with out) and `Last hour`, making eight. Narrower than 1000px, each row stacks to one column.

- **A `tokens` tab:**
  - the tokens-by-role chart, full width;
  - the agents table with every agent in the window, sortable by any column header;
  - the quota chart.

- **Drill-downs:**
  - **The lead page** gains a `tokens` card (the four kinds main and sub, its subagents with their descriptions) and the context-per-call chart over its life. A `compare` select, listing done leads of its spec size, sets `vs` to draw the second agent in orange on minutes since start.
  - **A session page**, `#/session/<host>/<id>`, linked from its lane and its rows:
    - a head (repo, first and last call, read, the design/exec split);
    - its busy lane;
    - the context chart;
    - its subagents (description, phase, read, out).

- **Quota history.** `web.Quota` publishes each good run to `bmusage.quota.<host>` (the 105 stream) as `{provider, five_hour, week, five_reset, week_reset, read_at}`. The chart reads the window's readings, keeping each provider's newest per 5 minutes across boxes.

- **`scripts/seed`** publishes usage for its nodes, two coordinator sessions with design and exec spans, one compaction and a day of quota readings.
- **`scripts/check.mjs`** adds `roles=` (series drawn), `agents=` (rows), `ctx=` (points) and `quota=` to the title, and exits 1 when any is 0 on the seed.
- **Docs:** the manual's Map section (rows 2 to 4, the tokens tab, the session page, quota history) and the internals' Map (the two endpoints, the buckets, the busy-span rule, the quota subject).

Out:
- runaway flags, the agents table's `watch` column, outliers and burndowns (108);
- dollars;
- a lead's tokens split by its state;
- Codex.

Order: after 105 and 106; before 108.

Judge: opus, because the charts must read right at a glance on a phone as on a desktop, and the phase colours must never be taken for state colours.

## What you'll see

The mockup's rows 2 to 4, live:
- the afternoon's tokens by role, coordinators split into design and execution;
- every agent's totals, coordinators first when they lead;
- a session's design share at a glance;
- 096's context climbing to 347k with `+19k Read cases.go` labelled;
- the quota's week line over seven days.

Clicking a lane opens its node's context, and a session's link opens its page.

## Verification

- The repo's verify.
- **Go tests** for both endpoints on a `meshtest` hub with seeded usage:
  - the role buckets for each window;
  - busy spans split at a phase change;
  - `vs` aligned on minutes since start;
  - a session page's data;
  - quota readings kept newest per provider per 5 minutes.
- **The browser check** on `scripts/seed.sh` at `1600 1000`, `1000 800` and `390 844`, exit 0, with the screenshots in the closing commit.
- **The real map** on k16 (`BMESH_WEB_DIR`, a binary with 105 reading k16's transcripts) on `24h` over 10-24. It must show:
  - `ec03a02c` first in the agents table;
  - its design/exec split;
  - 096's context chart with its largest growth labelled.

  The screenshot goes in the closing commit.

## Risks and the alternative considered

- **A crowded overview.** Four rows of panels stack on a phone. The tabs hold the full table and chart, and the overview's table shows the first 20 rows and a `more` link to the tokens tab.
- **The phase colours could be read as states.** The states take blue, teal, the warning yellow, slate and the critical red. Violet and magenta are used only for a coordinator's phase, and the legend names them on every panel that uses them.
- **Alternative: a Grafana board over the stream.** It's another install on every operator's box, and it can't join usage to nodes, specs and events as the console does.

## Upgrading

nothing

## Needs from the operator
