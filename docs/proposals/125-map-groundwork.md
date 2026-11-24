---
id: 125
title: The map's groundwork - one node-state table in mesh that Go, the daemon and the page all read, the page split into a module per view, and its views, reads, live frames and open folds each driven by one table
kind: maintenance
size: L
judge: opus
status: proposed
lens: ux
proposed_by: coordinator
proposed_on: 2026-11-24
---

## Opportunity
Four accepted specs change the map next:
- the hosts tab and its head lines (117);
- contention colours, a ninth KPI tile and timeline strips (120, after 117);
- one drawer row per landing change (121);
- the landing card and overview landing rows (122, after 121).

Each adds to state machines in the page that today are implicit, spread out or copied.

**One script.** `internal/web/map.js` is 2,687 lines holding every view. Specs that edit the same files are built one after another in one family. So the two chains (117 then 120, 121 then 122) can't run side by side while every view lives in one file. With a module per view, they still share `index.html`, `map.css`, `check.mjs` and the views table, but only as one-row edits.

**The node-state table is copied.** `internal/mesh/state.go` owns the transitions and the event-to-state map. Four tables repeat or extend it elsewhere:
- **The event-to-state map** is in three places: `eventStates` (`state.go`), `nodeState` (`web/timeline.go:25`) and `EVENT_STATE` (`map.js:22`).
- **The daemon-event map** (which daemon event sets which state on its `to`) is in two: `daemonState` (`timeline.go:26`) and `DAEMON_STATE` (`map.js:23`). The daemon's writers set those states by hand (`internal/daemon/silence.go:39,44`, `land.go:575,597,660`).
- **The need order** is in two: `needRank` (`timeline.go:30`) and `NEED` (`map.js:11`).
- **The live set** is in two: `liveStates` (`web/usage.go:28`) and `LIVE` (`map.js:25`).
- The tick kinds (`tickKind`, `timeline.go:28`, against `TICK` and the `refused` case at `map.js:191`) and the band's `NEEDS` (`map.js:944`, which Go writes as `r <= needRank["silent"]` at `timeline.go:304`) are part of the same table.

The copies agree today, and adding `held` and `landing` each touched every copy (`git log -S` on each map shows no fix for a missed one). The cost is the hunt: a new state or daemon event is six edits across three packages and the page. `docs/internals.md:615` documents the table as living in two files.

**The page's own state machines are implicit, and each view's wiring is repeated.**
- **Which reads a view needs** is said in five places:
  - `route()` (`map.js:2626-2640`);
  - the reconnect (`map.js:2492-2504`);
  - the 60 s poller (`map.js:2678-2683`);
  - `setWindow` (`map.js:240-255`);
  - each loader's own guard (`if (pages.view !== "map" && pages.view !== "timeline") return`).

  The router is an if-chain naming each view three times: the hash test, the `view-*` sections and the `#windows` rule.
- **A read's lifecycle** (not loaded, loading, shown, failed, missing) is handled per loader. The response-for-a-page-left check is written six ways, at `timeline`, `usage`, `lead`, `comparePeers` (`map.js:2377`), `ctxPanel` and `sessionPage`. A failed read before anything showed says different things on each page:
  - the lead page says "could not load ...; retrying on the next event";
  - the session page says "could not load", and replaces what it showed even on a re-read (`map.js:2397-2414`);
  - the routines panel says nothing.
- **A live frame's effects** are split between `apply` (`map.js:177`) and `pagesApply` (`map.js:1521`), and both run on every frame (`map.js:2529-2530`). A routine frame extends a job lane in one and refetches the tree, the routines and the open lead in the other. A digest does nothing in the first and fills the strip in the second. The host-traffic return in `apply` (`map.js:181-182`) is dead: `events()` in `web.go` drops those frames before they reach the page.
- **A node's shown state** is the tree's read with live events laid over it (`mesh.live`, evicted on each read at `map.js:141`). Two places merge them, `eachNode` (`map.js:109`) and `needRows` (`map.js:952`), each in its own way.
- **An open fold survives a redraw** four ways:
  - `openRows` for clamped rows (`map.js:802`);
  - `foldOpen` for spec folds (`keepOpen`);
  - for the band's older fold, the DOM read back before the redraw (`map.js:1041`);
  - for the digest, the same (`map.js:1480`).
- **The window** is written by `setWindow` and by `route()`, each reconciling `tl.win`, `localStorage` and the hash. `tok.win` records which window the loaded usage is for (`usageNow`, `map.js:1594`).
- **Six bare timers** (`map.js:2676-2686`), each with its own hidden-tab rule.
- **Already explicit, and left as they are:**
  - the connection (`connecting`, `live`, `reconnecting` with backoff);
  - the drawer's focus (`pin`, `hover`, `mute`);
  - the band's row sort;
  - the lane builder (`laneEvent`).

**The check's rules are prose and code twice over.** `scripts/check.mjs` keeps its rules in a 25-line comment and in the code under it. The manual's exit-1 row (`docs/manual.md:4269`) has drifted from both: it says `kpis=` must be 6 where `check.mjs` checks 8, and it leaves out every overview rule after it (`roles`, `agents`, `ctx`, `quota`, `flags`, `flagsRunaway`, `outliers`, `burndown`). The four specs each add a rule.

## Proposal
Nothing the operator sees changes, except two things:
- a read that fails before anything showed now says so on every page;
- a session page whose re-read fails now keeps what it shows.

In:

**1. One node-state table, in `internal/mesh`.** `state.go` gains `States`: one row per state, holding its need rank, whether it is live (the Live agents tile), whether it needs the operator (the band), the node event that sets it, and the daemon events that set it on their `to`. A `Ticks` map gives each event its tick kind (`progress`, `job`, `refused`, `ready`, `done`).

From these come `Legal`, `StateFor`, a new `DaemonStateFor`, `NeedRank` (an unknown state ranks as `idle`), `Live`, `Needs` and `TickFor`.
- **Deleted, callers moved:** `web/timeline.go`'s `nodeState`, `daemonState`, `needRank` and `tickKind`, and `web/usage.go`'s `liveStates`.
- **The lane order:** `timeline.go`'s `ordered` tests staleness with `Needs`, not a rank compare. It keeps its stale slot between `held` and `idle`.
- **The daemon's writers:** they set the state `DaemonStateFor` names for the event they publish, so a new daemon event is one row.
- **Not touched:** `cmd_status.go`'s `terminal` and the other callers' own lists of states to act on. Those are each command's policy, not the state table.

**The page reads the same table.** `GET /js/mesh-table.js` is an ES module the server renders from `States` and `Ticks` (`export const STATES = {...}, TICKS = {...}`).
- **What derives from it:** `EVENT_STATE`, `DAEMON_STATE`, `NEED`, `LIVE`, `NEEDS` and `TICK`.
- **Load order:** the page imports it statically, so it is evaluated before `/events` opens and no frame can arrive before the table.
- **How it's served:** its handler is registered ahead of `/js/{name}`. Under `Options.Dir` it is still rendered from `States`. It is served with the same cache headers as every other file.
- **What stays presentation:** icons (`ICON`) and colours (`--<state>` in `map.css`).
- **A new check:** the self-check adds `stateGaps=`, the states in `STATES` that have no `ICON` entry, or no `--<state>` token among the segment and badge states (`idle` and `done` excepted, which draw from `--tm` and `--done-*`). `check.mjs` requires 0.
- **Docs:** the internals' state-table section names the one table.

**2. Modules.** `map.js` becomes ES modules in `internal/web/js/`, embedded with `//go:embed js`, and `index.html` loads `/js/main.js` as `type="module"`.
- **How they're served:** each embedded file is served at `/js/<name>` by `asset()`, as `map.js` is today. That covers `text/javascript`, `Options.Dir` reading from disk, and a file missing there answering 500. A name not embedded is 404, `/js/` lists nothing, and `/map.js` is removed with its tests moved.
- **The modules:**
  - `core.js`: DOM and SVG helpers, times and clocks, `fetchJSON`, `loader`, `every`, `kept`, and the `?check=1` counters every view increments.
  - `state.js`: the shared state and `stateOf(path)`, the node's shown state (the tree's, with a live event winning while it is newer than the read). It replaces the merges in `eachNode` and `needRows`.
  - `live.js`: the `/events` connection.
  - `drawer.js`: the recent-events drawer, its chips, lanes and focus, the clamped rows and the tooltip.
  - One module per view: `overview.js`, `timeline.js`, `tokens.js`, `specs.js`, `routines.js`, `lead.js`, `session.js`. `drawLanes` lives in `timeline.js`, and the overview, lead and session pages import it.
  - `check.js`: the self-check title.
  - `main.js`: the views and frames tables, the timers and start-up.
- **Import rules:** only `main.js` has top-level side effects. Table rows reach other modules' functions inside arrow functions, so a cycle between modules never reads a binding before it is set. Functions move as they are, unless a table below replaces them. Nothing hangs off `window`.

**3. The views table.** `main.js` has one `VIEWS` entry per view, with:
- its hash prefix and section;
- whether the window buttons show;
- `reads`: the loaders it needs;
- `poll`: those of them the 60 s re-read covers.

What reads the table:
- `route()` shows the entry's section, hides the rest, sets the nav's `on`, and runs its `reads`.
- The reconnect, the window change and the 60 s re-read each run the shown view's.
- The loaders' own view guards are deleted.

A tab is one module, one entry and a nav link. A spec with its own cadence (117's hosts read, 122's landing read) adds its own `every()` with its own condition.

**4. Reads and frames.**
- **One lifecycle per read:**
  - `loader(load, {key})` keeps `{phase, key, at, status}`, with `phase` one of `idle`, `loading`, `shown`, `failed` and `missing`.
  - `key()` names what the read is for: the window, the lead's path, the agent and window. A response whose key changed while it was in flight is dropped, which replaces the six checks.
  - `usageNow()` reads the usage loader's shown key instead of `tok.win`, which goes.
- **One helper for empty and failed states:** `readState(loader, what, missing)` gives `loading <what>`, the view's own `missing` words on a 404 (`no such node: <path>` for the lead page, `no model calls of <agent> in the last 30 days` for the session page), and `could not load <what>; retrying` while nothing has shown yet. A failed re-read keeps what is shown on every page, the session page included.
- **One frames table:** live frames go through `FRAMES`, an ordered list of `{match, effect}` rows. Every row whose `match` holds runs, in order, and together the rows do what `apply` and `pagesApply` do today. The dead host-traffic case goes.
- **One timer helper:** `every(ms, fn, {hidden})` is the only timer, and each of today's six keeps its hidden-tab rule. The tree, the strip, the now line and the clock run on a hidden tab (`hidden: true`); the 60 s re-read and the routines read skip it. Nothing new runs when the tab shows again.

**5. Folds and the window, one owner each.**
- **Folds:** `kept(key)` is the one set of open folds and rows, capped at 200 as `openRows` is. Its users and their keys:
  - clamped rows keep their keys;
  - spec folds keep `<round> <id>`;
  - the band's older fold is `older`;
  - the digest is `digest`, so a newer digest stays open as it does today.
- **The window:** `setWin(w)` is its one writer, and it writes `localStorage`.
  - It rewrites the hash only when a window button calls it, as `setWindow` does today.
  - Called from `route()`, it never rewrites the hash, so `#/lead/<path>?w=24h` stays the lead's URL.

**6. The check's rules as a table.**
- `check.mjs` keeps a `RULES` table: per view, each title key, its test (`>= 1`, `== 8`, `== 0`, `has working,landing,...`) and its stderr message. The prose comment shrinks to how to run it.
- The manual's exit-1 row is rewritten from the table, listing every rule it enforces.
- The manual's "To check a UI change" runs the three widths in both themes with the existing `CHECK_COLOR_SCHEME=dark|light`.

Out:
- any visible change beyond the two above;
- a new view, state, status colour or check flag;
- a framework, bundler or build step;
- a JS test runner;
- the seed;
- the policy lists of states other commands act on (`terminal`, `wake.go`, `cmd_hold.go`, `stop.go`, `cmd_ideate.go`).

Judge: opus, because the move must keep every view's behaviour across 2,700 lines of shared state, the state table reaches the daemon, the web server and the page, and the next four specs build on these seams.

## What you'll see
- **The map:** it looks and behaves as before.
- **Failed reads:** a read that fails before anything showed says `could not load <what>; retrying` on every page. A session page keeps what it shows when a re-read fails.
- **Adding to it:** a spec adding a tab, a read, a live-frame case, a state, a daemon event or a check rule adds one row, where today it threads several files.
- **Docs:** the manual's check table lists every rule the check enforces.

## Verification
- **Same page, before and after.** On `scripts/seed.sh`, the self-check title of each view is recorded three times before the change and three times after.
  - The views: the overview, timeline, tokens, specs and routines, `#/lead/app-0041` and `#/session/seed/ec03a02c-5eed-4a1b-9c0d-000000000001`, at 1600x1000.
  - Compared: the keys that held still across the three before-runs. Load-timed counts such as `trees=`, `replay=` and `conn=` are left out, and the list of keys left out is named.
  - After the change they must be identical, apart from the new `stateGaps=0`.
  - All six runs per view are in the commit that finishes the move.
- **A live frame still lands.** During a check run on the overview, `go run ./scripts/seed -nats <url> -again docs-0007` gives `live=` 1 and the band's row back, before and after.
- **A failed read says so.** One `check.mjs` run per page fails `/api/routines`, then `/api/lead/*`, then `/api/usage/agent`, with DevTools `Fetch.enable` and `Fetch.failRequest`.
  - Before: only the lead page shows `could not load`.
  - After: every page does, and a session page that had shown keeps its content.
  - The titles go in the commit.
- **`internal/mesh` tests:**
  - `Legal`, `StateFor`, `DaemonStateFor`, `TickFor`, `NeedRank`, `Live` and `Needs` answer as the deleted maps did for every key they had;
  - an unknown state ranks as `idle`;
  - the daemon's silence and land writers write the state `DaemonStateFor` names, through their existing tests.
- **`internal/web` tests:**
  - `/` loads `/js/main.js` as a module;
  - every embedded `js/` file is served as `text/javascript`, embedded and under `Options.Dir`, and a missing one there answers 500;
  - `/js/mesh-table.js` carries every state and tick, under both;
  - `/js/`, an unknown name and `/map.js` are 404;
  - the lane order with a stale need is unchanged.
- **The page's tables:**
  - `grep -n setInterval internal/web/js` finds only `every`;
  - `grep -n 'pages.view !==' internal/web/js` finds none in a loader;
  - the frames table and the views table cover every subject and hash `apply`, `pagesApply` and `route()` handled.
- **The check:** a `check.mjs` run against a view with one count forced wrong exits 1 with that rule's message. The manual's exit-1 row names every rule in `RULES`.
- **Docs:** `docs/ui-skills.md`'s profile, the manual's Testing section and `docs/internals.md`'s state table and map sections describe the modules and the one table.

## Shown

## Risks and the alternative considered
- **The move can break a view in a way only a browser shows.** The checks are the three-run titles on every view, the live-frame run and the failed-read runs. The UI-change check at three widths in both themes catches layout breaks.
- **Alternative: let each spec add what it needs.** It lost because the two chains would then run one after another on one file, each adding a timer, a stale check, a frame case and a check rule its own way.
- **Alternative: a test comparing the state copies instead of deleting them.** It lost because it keeps six edits per new state.
- **Alternative: two specs, the Go table and the page's split.** It lost because the page's table module is the same table, so the two land together or the page carries a copy in between.

## Upgrading
nothing

## Needs from the operator
