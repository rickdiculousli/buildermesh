"use strict";
// The console: KPI tiles, the needs-you band and a fleet timeline drawn from
// /api/tree, /api/strip and /api/timeline, kept current by /events. Live
// node events restyle the band and tiles at once and extend the timeline's
// lanes; replayed frames only fill the recent-events drawer. Every mesh
// value goes in through textContent or setAttribute.

const NS = "http://www.w3.org/2000/svg";
// NEED orders the band's rows and the timeline's lanes: what needs a hand,
// then what runs, what has no state yet, what is finished.
const NEED = { blocked: 0, asking: 1, silent: 2, working: 3, landing: 4, held: 5, idle: 6, done: 7 };
const PHONE = "(max-width: 699px)";
const NARROW = "(max-width: 999px)";
const FETCH_MS = 10000; // a read taking longer is abandoned
const RECENT = 30;
const REFETCH_MS = 500;
const REFETCH_MAX_MS = 2000; // a steady stream of events still refetches this often
const POLL_MS = 30000; // unread counts and runners change without an event
// EVENT_STATE and DAEMON_STATE are the state table (internal/web/timeline.go
// nodeState and daemonState): the state a node's own event sets, and the
// state a daemon event sets on the node it is addressed to (to).
const EVENT_STATE = { started: "working", progress: "working", ask: "asking", blocked: "blocked", held: "held", ready: "landing", done: "done" };
const DAEMON_STATE = { silent: "silent", landing: "landing", "land-refused": "working", "land-withdrawn": "working" };
// LIVE are the states of a node at work or waiting: the Live agents tile.
const LIVE = new Set(["working", "asking", "blocked", "silent", "landing", "held"]);
// ICON goes with every state colour, beside the state's word.
const ICON = { working: "●", landing: "▲", held: "‖", silent: "◌", asking: "?", blocked: "■", idle: "○", done: "✓", operator: "▸" };

const $ = (id) => document.getElementById(id);

const mesh = {
  tree: null, // the last /api/tree
  // path -> {state, text, since, at}: states set by live events, with the
  // event's words and time (since) and when the page saw it (at).
  live: new Map(),
  recent: [], // newest first
  staleMs: 0, // [serve] stale_after from /api/tree; 0 = nothing goes stale
  host: "", // the serving daemon's host
  skew: 0, // the hub's clock less this browser's, in ms
};

// check is set by ?check=1: a self-check that writes viewport and
// connection facts into document.title for headless screenshots.
const check = new URLSearchParams(location.search).get("check") === "1"
  ? { conn: [], trees: 0, replay: 0, live: 0, lanes: 0, segs: {}, ticks: 0, roles: 0, agents: 0, ctx: 0, quota: 0, outliers: 0, burndown: 0 } : null;

function svg(tag, attrs, parent) {
  const e = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs || {})) e.setAttribute(k, String(v));
  if (parent) parent.append(e);
  return e;
}

function svgText(attrs, s, parent) {
  const e = svg("text", attrs, parent);
  e.textContent = s;
  return e;
}

// fit cuts s to n characters with an ellipsis.
function fit(s, n) {
  return s.length <= n ? s : s.slice(0, n - 1) + "…";
}

function lastToken(path) {
  return path.slice(path.lastIndexOf(".") + 1);
}

// hubNow is the hub's clock in unix seconds.
function hubNow() {
  return (Date.now() + mesh.skew) / 1000;
}

function startOf(t) {
  const v = Date.parse(t.node.started_at || "");
  return isNaN(v) ? Infinity : v;
}

// byStart orders nodes by started_at (unknown last), ties by path.
function byStart(a, b) {
  const x = startOf(a), y = startOf(b);
  if (x !== y) return x < y ? -1 : 1;
  return a.node.path < b.node.path ? -1 : a.node.path > b.node.path ? 1 : 0;
}

function needRank(state) {
  const r = NEED[state];
  return r === undefined ? NEED.idle : r;
}

// isStale reports a needs-you state (NEEDS) older than [serve] stale_after.
function isStale(st) {
  if (!mesh.staleMs || !NEEDS.has(st.state)) return false;
  const at = Date.parse(st.at || "");
  return !isNaN(at) && Date.now() - at > mesh.staleMs;
}

function instanceSub(i) {
  const extra = [i.uses === 1 ? "1 use" : (i.uses || 0) + " uses"];
  if (i.context > 0) extra.push(Math.round(i.context / 1000) + "k ctx");
  return extra;
}

// eachNode calls fn with every non-routine node of the loaded tree (leads,
// rounds, orphans and everything under them) and its state, a live event's
// state winning over the tree's.
function eachNode(fn) {
  const walk = (t) => {
    const live = mesh.live.get(t.node.path);
    fn(t, live ? live.state : t.state && t.state.state);
    for (const c of t.children || []) walk(c);
  };
  if (!mesh.tree) return;
  for (const t of mesh.tree.leads) walk(t);
  for (const t of mesh.tree.orphans) walk(t);
}

// --- the tree: the band's and tiles' data ----------------------------------

let loading = false, reload = false;

// loadTree reads /api/tree and redraws the band and tiles. One read runs at
// a time: a call while one is in flight runs one more read after it.
async function loadTree() {
  if (loading) {
    reload = true;
    return;
  }
  loading = true;
  const began = Date.now();
  try {
    const r = await fetchJSON("/api/tree");
    if (!r.body) throw new Error("/api/tree: " + r.status);
    const v = r.body;
    mesh.tree = { leads: v.leads || [], routines: v.routines || [], orphans: v.orphans || [], operator: v.operator || [] };
    mesh.staleMs = (+v.stale_after || 0) * 1000;
    mesh.host = String(v.host || "");
    const now = Date.parse(v.now || "");
    if (!isNaN(now)) mesh.skew = now - (began + Date.now()) / 2;
    // A live event newer than this read wins over the state it returned.
    for (const [path, s] of mesh.live) if (s.at < began) mesh.live.delete(path);
    if (check) check.trees++;
    renderBand();
    renderKpis();
    showConn();
  } catch (e) {
    // The next event or poll tries again.
  } finally {
    loading = false;
    if (reload) {
      reload = false;
      loadTree();
    }
  }
}

const treeSoon = loader(loadTree).soon;

// liveState records a node's state from a live event with its words (text)
// and time (since), and redraws the band and tiles.
function liveState(path, state, text, since) {
  mesh.live.set(path, { state, text: text || "", since: since || "", at: Date.now() });
  renderBand();
  renderKpis();
}

// known reports whether the loaded tree has path.
function known(path) {
  let found = false;
  eachNode((t) => { if (t.node.path === path) found = true; });
  return found;
}

// apply acts on a live frame: node and daemon events move the node's state
// and its lane, a job sent marks its sender's lane, and a node the tree
// doesn't have refetches it.
function apply(f) {
  const rest = f.subject.replace(/^bm\./, "");
  const p = f.payload && typeof f.payload === "object" ? f.payload : {};
  const at = Date.parse(String(p.at || f.at || "")) / 1000;
  // The hosts layer's jobs and replies: mirrors mesh.IsHostTraffic.
  if (rest.startsWith("inbox.host.") || rest.startsWith("hostreply.")) return;
  if (rest.startsWith("inbox.")) {
    if (p.kind === "job" && p.from) liveLane(String(p.from), "", at, "job", String(p.text || (p.job && p.job.kind) || ""));
    return;
  }
  if (rest.startsWith("feedback.")) return; // a family's queue, not a node
  const i = rest.lastIndexOf(".");
  if (i <= 0) return;
  const path = rest.slice(0, i), ev = rest.slice(i + 1);
  const text = String(p.text || "");
  if (path.startsWith("daemon.")) {
    if (p.to && DAEMON_STATE[ev]) {
      liveState(String(p.to), DAEMON_STATE[ev], text, p.at || f.at);
      liveLane(String(p.to), DAEMON_STATE[ev], at, ev === "land-refused" ? "refused" : "", text);
    }
    return;
  }
  if (path.startsWith("routine.")) {
    liveJob(path, ev, text, at);
    return;
  }
  if (EVENT_STATE[ev]) {
    liveState(path, EVENT_STATE[ev], text, p.at || f.at);
    liveLane(path, EVENT_STATE[ev], at, TICK[ev] || "", text);
  }
  if (ev === "started" || ev === "done" || !known(path)) treeSoon();
}

// --- the fleet timeline -------------------------------------------------------

// WINDOWS are the spans the window chips pick, in seconds.
const WINDOWS = { "1h": 3600, "6h": 6 * 3600, "24h": 24 * 3600, "7d": 7 * 86400 };
const WINDOW_KEY = "bmesh.window";
// TICK is the tick a node's event draws on its lane.
const TICK = { progress: "progress", ready: "ready", done: "done" };
// Lane geometry: a lane's height and its bar's, for a top lane and for a
// child's thinner lane on the timeline tab and the lead page.
const LANE = { h: 22, bar: 13, kidH: 16, kidBar: 9, top: 12, axis: 20, gutter: 170, narrowGutter: 118 };
const DRAW_MS = 1000; // live events redraw at most this often
const NOW_MS = 30000; // the now line moves this often

const tl = {
  win: "6h",
  data: null, // the last /api/timeline: {from, to, lanes}
  lead: null, // the lead page's lanes: {path, data}
  drawTimer: 0,
  lastDraw: 0,
};

function storedWindow() {
  try {
    const w = localStorage.getItem(WINDOW_KEY);
    return WINDOWS[w] ? w : "";
  } catch (e) {
    return "";
  }
}

function setWindow(w) {
  if (!WINDOWS[w] || w === tl.win) return;
  tl.win = w;
  try {
    localStorage.setItem(WINDOW_KEY, w);
  } catch (e) {
    // Storage off: the hash still carries it.
  }
  const base = pages.view === "timeline" || pages.view === "tokens" ? "#/" + pages.view : "#/";
  history.replaceState(null, "", base + "?w=" + w);
  showWindow();
  tl.data = null;
  drawAll();
  timeline.run();
  usage.run();
}

function showWindow() {
  for (const b of document.querySelectorAll("#windows button")) b.setAttribute("aria-pressed", String(b.dataset.w === tl.win));
}

const timeline = loader(async () => {
  if (pages.view !== "map" && pages.view !== "timeline") return;
  const win = tl.win;
  const r = await fetchJSON("/api/timeline?since=" + win);
  if (!r.body || win !== tl.win) return;
  tl.data = r.body;
  tl.data.lanes = tl.data.lanes || [];
  replayLive(tl.data);
  drawAll();
});

// liveLog holds the live events since the page opened, so a timeline read
// (cached on the hub for 30 s) gets the ones newer than it reapplied.
const liveLog = [];
const LIVE_LOG_MAX = 500;

// liveLane extends path's lane with a live event at (unix seconds): state
// ("" for none) closes the last segment and opens one, done ends the lane;
// kind ("" for none) adds a tick with its text.
function liveLane(path, state, at, kind, text) {
  if (isNaN(at)) return;
  liveLog.push({ path, state, at, kind, text, job: "" });
  if (liveLog.length > LIVE_LOG_MAX) liveLog.shift();
  for (const d of [tl.data, tl.lead && tl.lead.data]) if (d) laneEvent(d, path, state, at, kind, text);
  drawSoon();
}

// liveJob extends a routine instance's lane: "job <id> started" opens a
// working segment, "job <id> ok|fail ..." closes it.
function liveJob(path, ev, text, at) {
  if (ev !== "progress" || isNaN(at)) return;
  const state = /^job \S+ started$/.test(text) ? "working" : /^job \S+ (ok|fail) \d+/.test(text) ? "idle" : "";
  if (state) liveLane(path, state, at, "", "");
}

function replayLive(d) {
  for (const e of liveLog) if (e.at > d.to) laneEvent(d, e.path, e.state, e.at, e.kind, e.text);
}

function laneEvent(d, path, state, at, kind, text) {
  let l = d.lanes.find((x) => x.path === path);
  if (!l) {
    if (!state || state === "done") return;
    const dot = path.lastIndexOf(".");
    const routine = path.startsWith("routine.");
    l = { path, role: routine ? "routine" : dot > 0 ? "executor" : "lead", parent: !routine && dot > 0 ? path.slice(0, dot) : "",
      repo: "", spec: "", state: "idle", segs: [], ticks: [] };
    eachNode((t) => { if (t.node.path === path) Object.assign(l, { role: t.node.role, parent: t.node.parent || "", repo: t.node.repo || "", spec: t.node.spec || "" }); });
    d.lanes.push(l);
  }
  if (kind) l.ticks.push([at, kind, text]);
  if (!state) return;
  const last = l.segs[l.segs.length - 1];
  const open = !!last && (l.role === "routine" ? l.state === "working" : l.state !== "done");
  if (open) last[1] = Math.max(last[0], at); // the last segment ends at the event
  l.state = state;
  if (state === "done" || l.role === "routine" && state === "idle") return;
  if (open && last[2] === state) return; // the same state goes on
  l.segs.push([at, at, state]);
}

function drawSoon() {
  if (tl.drawTimer) return;
  tl.drawTimer = setTimeout(() => { tl.drawTimer = 0; drawAll(); }, Math.max(0, tl.lastDraw + DRAW_MS - Date.now()));
}

// drawAll draws the timeline of the view shown.
function drawAll() {
  tl.lastDraw = Date.now();
  if (pages.view === "map") {
    drawLanes($("tl-map"), tl.data, { fold: true, live: true, sessions: true });
    renderKpis();
    drawRoles($("roles-map"));
    drawAgents($("agents-map"), { limit: AGENTS_SHOWN, select: true });
    drawCoords($("coords-map"));
    drawCtxPanel();
    drawQuota($("quota-map"));
    drawOutliers($("outliers-map"));
    drawBurnRow();
  } else if (pages.view === "specs") {
    drawSpecs();
  } else if (pages.view === "timeline") {
    drawLanes($("tl-full"), tl.data, { fold: false, live: true, sessions: true });
  } else if (pages.view === "tokens") {
    drawRoles($("roles-tok"));
    drawAgents($("agents-tok"), { sortable: true });
    drawQuota($("quota-tok"));
  } else if (pages.view === "lead" && tl.lead && tl.lead.path === pages.lead) {
    drawLanes($("lead-lanes"), tl.lead.data, { fold: false, live: true, life: true });
    if (tok.lead && tok.lead.path === pages.lead) ctxChart($("lead-ctx"), tok.lead.data, { empty: "No model calls of this node on the mesh." });
  } else if (pages.view === "session") {
    drawSession();
  }
}

// abbrev shortens a repo name for lane labels: its first letter and the
// letter two thirds into its first word (buildermesh: bm).
function abbrev(repo) {
  const w = repo.split(/[-_.]/)[0];
  return w.length < 3 ? w : w[0] + w[Math.floor(2 * w.length / 3)];
}

// shortener drops the repo's prefix from lane labels when every top lane is
// <repo>-... of one repo whose name has five letters or more, putting
// abbrev(repo) in its place.
function shortener(tops) {
  const repo = tops.length && tops[0].repo;
  if (!repo || repo.length < 5 || !tops.every((l) => l.repo === repo && l.path.startsWith(repo + "-"))) return (p) => p;
  const ab = abbrev(repo);
  return (p) => p.startsWith(repo + "-") ? ab + p.slice(repo.length) : p;
}

// rowsOf turns lanes into the rows drawn: with fold, the top lanes only
// (a lane whose parent has no lane), each with the count of lanes under it;
// else every lane at its depth, the routine lanes after the rest.
function rowsOf(lanes, fold) {
  const by = new Map(lanes.map((l) => [l.path, l]));
  const isTop = (l) => !by.has(l.parent) || l.parent === l.path;
  const depth = (l) => {
    let n = 0;
    for (let p = l; !isTop(p) && n < 8; p = by.get(p.parent)) n++;
    return n;
  };
  const nodes = lanes.filter((l) => l.role !== "routine");
  const tops = nodes.filter(isTop);
  const short = shortener(tops);
  const under = (l) => nodes.filter((x) => x !== l && x.path.startsWith(l.path + ".")).length;
  if (fold) return tops.map((l) => ({ l, depth: 0, label: short(l.path), more: under(l) }));
  const rows = nodes.map((l) => {
    const d = depth(l);
    return { l, depth: d, label: d ? lastToken(l.path) : short(l.path), more: 0 };
  });
  const rs = lanes.filter((l) => l.role === "routine");
  if (rs.length) rows.push({ group: "routines" }, ...rs.map((l) => ({ l, depth: 0, label: l.path.replace(/^routine\./, ""), more: 0 })));
  return rows;
}

// axisStep is the time between grid lines for a span and width: an hour
// each hour narrower than 1000px on the 6h and 24h windows.
function axisStep(span) {
  const narrow = narrowQ.matches;
  if (span <= 3600) return 600;
  if (span <= 6 * 3600) return narrow ? 3600 : 1800;
  if (span <= 24 * 3600) return narrow ? 3600 : 7200;
  if (span <= 3 * 86400) return 6 * 3600;
  return 86400;
}

function hm(t) {
  return new Date(t * 1000).toISOString().slice(11, 16);
}

function md(t) {
  return new Date(t * 1000).toISOString().slice(5, 10);
}

// spanText is "14:30–15:10 (40m)", the day too when the span crosses one.
function spanText(a, b) {
  const day = md(a) !== md(b);
  return (day ? md(a) + " " : "") + hm(a) + "–" + (day ? md(b) + " " : "") + hm(b) + " (" + dur((b - a) * 1000) + ")";
}

// drawLanes draws d's lanes into box over d's window moved to the hub's now
// (opts.live), or from d.from to now (opts.life): segments by state, idle
// dashed, ticks, grid lines, the now line, hover tips and links to the lead
// page; with opts.fold only the top lanes.
function drawLanes(box, d, opts) {
  if (!box) return;
  if (!d) {
    box.replaceChildren(el("p", "empty", "loading…"));
    return;
  }
  // A life whose lanes are all done ends at its last event, not now.
  const ended = opts.life && d.lanes.length > 0 && d.lanes.every((l) => l.state === "done");
  const now = Math.max(hubNow(), d.to);
  const to = ended ? d.to : now;
  const from = opts.life ? Math.min(d.from, to - 600) : now - (WINDOWS[tl.win] || d.to - d.from);
  const rows = rowsOf(d.lanes || [], opts.fold);
  const u = opts.sessions ? usageNow() : null;
  // A phone's gutter has room for the label only: the agents table has the reads.
  const reads = new Map(u && !phone.matches ? u.agents.map((a) => [a.agent, +a.read || 0]) : []);
  const flagged = u ? flagMap(u) : new Map();
  if (u && u.sessions.length) {
    // The coordinator sessions, a group under the leads (before the routines).
    const gi = rows.findIndex((r) => r.group === "routines");
    rows.splice(gi < 0 ? rows.length : gi, 0, { group: "coordinators · design / exec" },
      ...u.sessions.map((s) => ({ s, depth: 0, label: agentName(s.agent) })));
  }
  const counts = { lanes: 0, segs: {}, ticks: 0 };
  if (!rows.length) {
    box.replaceChildren(el("p", "empty", opts.life ? "no events for this node" : "No lanes in this window."));
    tallyCheck(box, counts);
    return;
  }
  const W = Math.max(box.clientWidth, 240);
  const G = narrowQ.matches ? LANE.narrowGutter : LANE.gutter;
  const hOf = (r) => r.group ? 18 : r.depth ? LANE.kidH : LANE.h;
  const H = LANE.top + rows.reduce((s, r) => s + hOf(r), 0) + LANE.axis;
  const root = svg("svg", { width: W, height: H, viewBox: `0 0 ${W} ${H}`, role: "group", "aria-label": "fleet timeline" });
  const x = (t) => G + (Math.max(from, Math.min(to, t)) - from) / (to - from) * (W - G - 8);
  const step = axisStep(to - from);
  let lastLabel = -Infinity;
  for (let t = Math.ceil(from / step) * step; t <= to; t += step) {
    const X = x(t);
    svg("line", { class: "grid", x1: X, x2: X, y1: LANE.top - 6, y2: H - LANE.axis + 2 }, root);
    if (X - lastLabel >= 52 && X <= W - 18) {
      svgText({ class: "axis", x: X, y: H - 6, "text-anchor": "middle" }, step >= 86400 ? md(t) : hm(t), root);
      lastLabel = X;
    }
  }
  let y = LANE.top;
  for (const r of rows) {
    if (r.group) {
      svg("line", { class: "grid", x1: 0, x2: W, y1: y + 6, y2: y + 6 }, root);
      svgText({ class: "axis", x: 4, y: y + 15 }, r.group, root);
      y += 18;
      continue;
    }
    if (r.s) {
      sessionLane(root, r, x, y, G, reads.get(r.s.agent), from, to, flagged.get(r.s.agent));
      y += LANE.h;
      continue;
    }
    const l = r.l, h = hOf(r), bar = r.depth ? LANE.kidBar : LANE.bar, by = y + (h - bar) / 2;
    const g = svg("g", { class: "tl-lane" + (r.depth ? " kid" : "") + " role-" + String(l.role || "").replace(/[^a-z-]/g, "") }, root);
    const read = reads.get(l.path);
    if (read) svgText({ class: "gread", x: G - 10, y: by + bar - 2, "text-anchor": "end" }, fmtTok(read), g);
    const fl = flagged.get(l.path);
    const name = svgText({ class: "label", x: 4 + r.depth * 10, y: by + bar - 2 }, fit(r.label, Math.floor((G - 14 - r.depth * 10) / 7) - (r.more ? 3 : 0) - (read ? 6 : 0) - (fl ? 2 : 0)), g);
    if (r.more) name.append(svgTspan("more", " +" + r.more));
    if (fl) name.append(svgTspan("flag fl-" + fl.level, " ▲"));
    hover(name, () => l.path + "\n" + (l.state || "idle") + (l.spec ? " · spec " + l.spec : "") + (l.repo ? " · " + l.repo : "") + (fl ? "\n▲ " + fl.level + ": " + fl.words : ""));
    const live = opts.live && l.state !== "done" && !(l.role === "routine" && l.state !== "working");
    l.segs.forEach((s, i) => {
      const end = live && i === l.segs.length - 1 ? now : s[1];
      if (end < from || s[0] > to) return;
      const st = String(s[2]).replace(/[^a-z-]/g, "");
      const x0 = x(s[0]), w = Math.max(2, x(end) - x0 - 1);
      const rect = svg("rect", { class: "seg st-" + st, x: x0, y: by, width: w, height: bar, rx: 3 }, g);
      hover(rect, () => l.path + "\n" + st + "  " + spanText(s[0], end));
      counts.segs[st] = (counts.segs[st] || 0) + 1;
    });
    if (!r.depth || !opts.fold) {
      for (const [at, kind, text] of l.ticks) {
        if (at < from || at > to) continue;
        const X = x(at);
        let m;
        if (kind === "progress") m = svg("rect", { class: "tick progress", x: X - 1, y: by, width: 2, height: bar }, g);
        else if (kind === "job") m = svgText({ class: "tick job", x: X, y: by - 1, "text-anchor": "middle" }, "◆", g);
        else if (kind === "refused") m = svgText({ class: "tick refused", x: X, y: by - 1, "text-anchor": "middle" }, "✕", g);
        else if (kind === "ready") m = svgText({ class: "tick ready", x: X, y: by - 1, "text-anchor": "middle" }, "▲", g);
        else if (kind === "done") m = svgText({ class: "tick done", x: X + 3, y: by + bar - 1 }, "✓", g);
        if (!m) continue;
        counts.ticks++;
        hover(m, () => l.path + "  " + md(at) + " " + hm(at) + "  " + kind + (text ? "\n" + text : ""));
      }
    }
    if (l.role !== "routine") linkLane(g, l.path);
    counts.lanes++;
    y += h;
  }
  if (!ended) {
    const nx = x(now);
    svg("line", { class: "now", x1: nx, x2: nx, y1: LANE.top - 6, y2: H - LANE.axis + 2 }, root);
  }
  box.replaceChildren(root);
  tallyCheck(box, counts);
}

// sessionLane draws a coordinator session's lane: its busy spans coloured
// by phase, its read in the gutter, a ▲ after its name when fl (flagged), a
// link to its page.
function sessionLane(root, r, x, y, G, read, from, to, fl) {
  const s = r.s, by = y + (LANE.h - LANE.bar) / 2;
  const g = svg("g", { class: "tl-lane session" }, root);
  if (read) svgText({ class: "gread", x: G - 10, y: by + LANE.bar - 2, "text-anchor": "end" }, fmtTok(read), g);
  const name = svgText({ class: "label", x: 4, y: by + LANE.bar - 2 }, fit(r.label, Math.floor((G - 14) / 7) - (read ? 6 : 0) - (fl ? 2 : 0)), g);
  if (fl) name.append(svgTspan("flag fl-" + fl.level, " ▲"));
  hover(name, () => s.agent + (s.repo ? "\n" + s.repo : "") + (fl ? "\n▲ " + fl.level + ": " + fl.words : ""));
  for (const [a, b, ph] of s.spans || []) {
    if (b < from || a > to) continue;
    const p = ph === "exec" ? "exec" : "design";
    const rect = svg("rect", { class: "seg ph-" + p, x: x(a), y: by, width: Math.max(2, x(b) - x(a)), height: LANE.bar, rx: 3 }, g);
    hover(rect, () => agentName(s.agent) + (s.repo ? " · " + s.repo : "") + "\n" + p + "  " + spanText(a, b));
  }
  g.setAttribute("tabindex", "0");
  g.setAttribute("role", "link");
  g.setAttribute("aria-label", s.agent);
  const go = () => { location.hash = agentHref(s.agent); };
  g.addEventListener("click", go);
  g.addEventListener("keydown", (e) => {
    if (e.key === "Enter" || e.key === " ") { e.preventDefault(); go(); }
  });
}

function svgTspan(cls, s) {
  const e = document.createElementNS(NS, "tspan");
  e.setAttribute("class", cls);
  e.textContent = s;
  return e;
}

function linkLane(g, path) {
  g.setAttribute("tabindex", "0");
  g.setAttribute("role", "link");
  g.setAttribute("aria-label", path);
  const go = () => { location.hash = "#/lead/" + path; };
  g.addEventListener("click", go);
  g.addEventListener("keydown", (e) => {
    if (e.key === "Enter" || e.key === " ") { e.preventDefault(); go(); }
  });
}

// hover shows the tip with text() under the mouse while it is on e.
function hover(e, text) {
  const tip = $("tip");
  e.addEventListener("pointermove", (ev) => showTip(ev, text()));
  e.addEventListener("pointerleave", () => { tip.hidden = true; });
}

// tallyCheck keeps, under ?check=1, the counts of the timeline the view shows.
function tallyCheck(box, c) {
  if (!check || box.closest(".view").hidden) return;
  check.lanes = c.lanes;
  check.segs = c.segs;
  check.ticks = c.ticks;
  checkSoon();
}
// --- the recent-events drawer ---------------------------------------------

// describe turns a frame into {path, event, text}; an inbox frame's also
// carries from and to, its path being "from → to".
function describe(f) {
  const p = f.payload && typeof f.payload === "object" ? f.payload : { text: String(f.payload) };
  const rest = f.subject.replace(/^bm\./, "");
  if (rest.startsWith("inbox.")) {
    const kind = p.kind && p.kind !== "message" ? p.kind + " " : "";
    const from = p.from || "?", to = rest.slice(6);
    return { path: from + " → " + to, from, to, event: "inbox", text: kind + (p.text || "") };
  }
  const i = rest.lastIndexOf(".");
  const path = i > 0 ? rest.slice(0, i) : rest;
  const event = i > 0 ? rest.slice(i + 1) : "";
  let text = p.text || "";
  if (rest.startsWith("daemon.") && p.to) text = p.to + (text ? " " + text : "");
  if (p.commit) text += (text ? " " : "") + "@" + p.commit;
  return { path, event, text };
}

function span(cls, s) {
  const e = document.createElement("span");
  e.className = cls;
  e.textContent = s;
  return e;
}

function stamp(at) {
  const d = new Date(at);
  return isNaN(d) ? String(at) : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

let recentTimer = 0;

function remember(f) {
  mesh.recent.unshift(f);
  if (mesh.recent.length > RECENT) mesh.recent.length = RECENT;
  if (!recentTimer) recentTimer = setTimeout(drawRecent, 50);
}

// frameKey names a frame for openRows: its subject, time and words (frames
// carry no sequence; two of one subject in one second differ by their words,
// or read the same).
function frameKey(f) {
  const p = f.payload && typeof f.payload === "object" ? f.payload : {};
  return f.subject + "@" + (p.at || f.at || "") + "#" + (p.text || "");
}

// The drawer's lanes: each path that owns a row (an inbox row's sender) gets
// one, in order of first appearance, newest first, LANE_PX apart; past the
// last lane the rest share it. A chip's hue is its lead tree's (a hash of
// the path's top segment over HUES), a child a lighter step; routines and
// the daemon are grey.
const LANE_PX = 14, LANES = 6, PHONE_LANES = 3;
const HUES = [172, 212, 262, 318, 28, 46, 138, 350];
const NEUTRAL = new Set(["routine", "daemon", "?"]);

// recentFocus is the path the drawer lights: hover while the mouse is on a
// chip or its line, else pin, set by a click and kept across redraws. mute
// is a path just unpinned, which the mouse still on it doesn't relight.
const recentFocus = { pin: "", hover: "", mute: "" };
let recentTW = 0; // the widest time column seen, for the lanes' origin

// paint gives e the hue of path: --h, --l (the step, 0 to 2) and --sat (0
// for grey), which map.css's .hue turns into --c-fg and --c-bg.
function paint(e, path) {
  const segs = String(path).split(".");
  let h = 0, sat = 0;
  if (!NEUTRAL.has(segs[0])) {
    let n = 2166136261; // FNV-1a
    for (const c of segs[0]) n = Math.imul(n ^ c.codePointAt(0), 16777619);
    h = HUES[(n >>> 0) % HUES.length];
    sat = 1;
  }
  e.classList.add("hue");
  e.style.setProperty("--h", h);
  e.style.setProperty("--l", Math.min(segs.length - 1, 2));
  e.style.setProperty("--sat", sat);
}

// chipLabel drops the nearest ancestor's prefix that owns a lane: "eng",
// not "buildermesh-round-1.eng", when the round has rows of its own.
function chipLabel(path, owners) {
  const segs = path.split(".");
  for (let i = segs.length - 1; i > 0; i--) {
    if (owners.has(segs.slice(0, i).join("."))) return segs.slice(i).join(".");
  }
  return path;
}

function chip(path, owners, cls) {
  const b = el("button", cls, chipLabel(path, owners));
  b.type = "button";
  b.title = path;
  b.dataset.path = path;
  paint(b, path);
  return b;
}

function drawRecent() {
  recentTimer = 0;
  const rows = mesh.recent.map((f) => {
    const d = describe(f);
    return { f, d, who: d.from === undefined ? d.path : d.from };
  });
  const order = [...new Set(rows.map((r) => r.who))];
  const owners = new Set(order);
  const max = phone.matches ? PHONE_LANES : LANES;
  const items = rows.map(({ f, d, who }) => {
    const li = document.createElement("li");
    li.className = "ev-" + d.event;
    li.dataset.who = who;
    const c = chip(who, owners, "chip");
    c.style.setProperty("--lane", Math.min(order.indexOf(who), max - 1));
    li.append(span("t", stamp((f.payload && f.payload.at) || f.at)), c);
    if (d.to !== undefined) {
      li.dataset.to = d.to;
      li.append(span("arrow", "→"), chip(d.to, owners, "chip small"));
    }
    li.append(span("e", d.event), span("x", d.text));
    clampRow(li, frameKey(f));
    return li;
  });
  const lanes = order.slice(0, max).map((p, i) => {
    const e = el("div", "lane");
    e.style.setProperty("--lane", i);
    if (order.length > max && i === max - 1) {
      e.classList.add("shared");
    } else {
      e.dataset.path = p;
      paint(e, p);
    }
    return e;
  });
  $("lanes").replaceChildren(...lanes);
  $("recent-box").style.setProperty("--lanes", lanes.length);
  $("recent").replaceChildren(...items);
  $("drawer-count").textContent = items.length ? "(" + items.length + ")" : "";
  const pin = recentFocus.pin;
  if (pin && !rows.some((r) => r.who === pin || r.d.to === pin)) recentFocus.pin = "";
  focusRecent();
  measureTime();
}

// measureTime sets --tw, the time column's width the lanes start after, to
// the widest seen; a closed drawer measures nothing, so opening it measures
// again.
function measureTime() {
  let w = 0;
  for (const t of $("recent").querySelectorAll(".t")) w = Math.max(w, t.getBoundingClientRect().width);
  if (w <= recentTW) return;
  recentTW = Math.ceil(w);
  $("recent-box").style.setProperty("--tw", recentTW + "px");
}

// focusRecent dims every row that neither is the focused path's nor names
// it as recipient, and lights its lane.
function focusRecent() {
  const f = recentFocus.hover || recentFocus.pin;
  for (const li of $("recent").children) li.classList.toggle("dim", !!f && li.dataset.who !== f && li.dataset.to !== f);
  for (const e of $("lanes").children) e.classList.toggle("on", !!f && e.dataset.path === f);
  for (const b of $("recent").querySelectorAll(".chip")) b.setAttribute("aria-pressed", String(b.dataset.path === recentFocus.pin));
}

// hoverPath is the path under the mouse: a chip's, or a lane's when the
// mouse is within a few pixels of its line and not on a row's words.
function hoverPath(ev) {
  const c = ev.target.closest(".chip");
  if (c) return c.dataset.path;
  if (ev.target.closest("span")) return "";
  for (const e of $("lanes").children) {
    if (!e.dataset.path) continue;
    const r = e.getBoundingClientRect();
    if (Math.abs(ev.clientX - (r.left + r.width / 2)) <= 5) return e.dataset.path;
  }
  return "";
}

function setHover(p) {
  if (p === recentFocus.mute) p = "";
  else recentFocus.mute = "";
  if (p === recentFocus.hover) return;
  recentFocus.hover = p;
  focusRecent();
}

// unpin drops the pin; the mouse still on that chip doesn't relight it.
function unpin() {
  if (recentFocus.hover === recentFocus.pin) recentFocus.hover = "";
  recentFocus.mute = recentFocus.pin;
  recentFocus.pin = "";
  focusRecent();
}

// pinChip pins the path of a clicked chip, moves the pin to it from
// another, or unpins it when it is pinned. Touch has no hover: a tap pins.
function pinChip(ev) {
  const c = ev.target.closest(".chip");
  if (!c) return;
  if (recentFocus.pin === c.dataset.path) {
    unpin();
    return;
  }
  recentFocus.pin = c.dataset.path;
  focusRecent();
}

// --- clamped rows -----------------------------------------------------------

// openRows holds the keys of the clamped rows tapped open (a band row's
// path, a drawer or timeline frame's frameKey, an inbox message's id), so
// a redraw keeps them open; past OPEN_MAX the oldest are forgotten.
const openRows = new Set();
const OPEN_MAX = 200;

// clampRow marks a row clamped to three lines until tapped; key names it in
// openRows.
function clampRow(e, key) {
  e.classList.add("clamp");
  e.dataset.key = key;
  if (openRows.has(key)) e.classList.add("open");
}

// toggleRows opens or closes the clamped row a tap lands on (a band row's
// words from anywhere in the row), unless the tap is a link, a fold or a
// chip.
function toggleRows(ev) {
  if (ev.target.closest("a, summary, button, .chip")) return;
  const row = ev.target.closest(".clamp, .need");
  const e = row && row.classList.contains("need") ? row.querySelector(".clamp") : row;
  if (!e || !this.contains(e)) return;
  const open = e.classList.toggle("open");
  if (open) openRows.add(e.dataset.key);
  else openRows.delete(e.dataset.key);
  while (openRows.size > OPEN_MAX) openRows.delete(openRows.values().next().value);
}

// --- pages: the lead, the routines, the strip -------------------------------

const STRIP_MS = 60000; // the server caches the quota 5 min; never poll faster
const ROUTINES_MS = 30000;
const STALE_MS = 10 * 60000; // a quota reading older than this shows its age
const TIMELINE_MAX = 200;

// el makes an HTML element with a class and text (both optional) and appends
// the children.
function el(tag, cls, text, ...children) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined && text !== null && text !== "") e.textContent = String(text);
  e.append(...children);
  return e;
}

function badge(state) {
  const s = String(state || "idle");
  return el("span", "badge st-" + s.replace(/[^a-z-]/g, ""), (ICON[s] ? ICON[s] + " " : "") + s);
}

// dur renders ms as "1d 3h", "1h 10m", "4m" or, with secs, "4m 10s".
function dur(ms, secs) {
  const s = Math.max(0, Math.floor(ms / 1000));
  const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60);
  if (d) return d + "d " + h + "h";
  if (h) return h + "h " + m + "m";
  return secs ? m + "m " + (s % 60) + "s" : Math.max(m, s > 0 ? 1 : 0) + "m";
}

function ago(at) {
  const ms = Date.now() - Date.parse(at);
  if (isNaN(ms)) return "";
  return ms < 60000 ? "just now" : dur(ms) + " ago";
}

// Clocks: [data-until] elements count down to a time and [data-since] ones
// count up from it; tick rewrites them every second.
const CLOCK = {
  lease: (ms) => ms > 0 ? "expires in " + dur(ms, true) : "expired",
  reset: (ms) => ms > 0 ? "resets in " + dur(ms) : "reset due",
  read: (ms) => ms > STALE_MS ? "· read " + dur(ms) + " ago" : "",
  digest: (ms) => "digest " + (ms < 60000 ? "just now" : dur(ms) + " ago"),
  since: (ms) => ms < 60000 ? "just now" : dur(ms) + " ago",
};

function clock(kind, at, until) {
  const e = el("span", "clock " + kind);
  const t = Date.parse(at);
  if (isNaN(t)) return e;
  e.dataset[until ? "until" : "since"] = String(t);
  e.dataset.clock = kind;
  tickOne(e);
  return e;
}

function tickOne(e) {
  const now = Date.now();
  const ms = e.dataset.until ? +e.dataset.until - now : now - +e.dataset.since;
  e.textContent = CLOCK[e.dataset.clock](ms);
  e.classList.toggle("past", !!e.dataset.until && ms <= 0);
}

function tick() {
  for (const e of document.querySelectorAll("[data-clock]")) tickOne(e);
}

// fetchJSON reads a same-origin URL: {status, body}, body null unless 2xx.
async function fetchJSON(url) {
  const stop = new AbortController();
  const timer = setTimeout(() => stop.abort(), FETCH_MS);
  try {
    const r = await fetch(url, { cache: "no-store", signal: stop.signal });
    return { status: r.status, body: r.ok ? await r.json() : null };
  } finally {
    clearTimeout(timer);
  }
}

// loader is loadTree's discipline for another read: run() reads one at a
// time (a call during a read queues one more), soon() reads REFETCH_MS after
// the last call but no later than REFETCH_MAX_MS after the first waiting.
function loader(load) {
  let busy = false, again = false, timer = 0, first = 0;
  const run = async () => {
    if (busy) {
      again = true;
      return;
    }
    busy = true;
    try {
      await load();
    } catch (e) {
      // The next frame or poll tries again.
    } finally {
      busy = false;
      if (again) {
        again = false;
        run();
      }
    }
  };
  const soon = () => {
    const now = Date.now();
    if (!timer) first = now;
    clearTimeout(timer);
    timer = setTimeout(() => { timer = 0; run(); }, Math.max(0, Math.min(REFETCH_MS, first + REFETCH_MAX_MS - now)));
  };
  return { run, soon };
}

const pages = { view: "map", lead: "", leadShown: "", session: "" };

// --- the needs-you band

// NEEDS are the states the band shows: waiting on the operator, or quiet.
const NEEDS = new Set(["blocked", "asking", "silent"]);

// needRows lists the leads, rounds and engineers (a round's children) in a
// NEEDS state, in need order, newest first within a state: a live event's
// state, words and time win over the tree's, as on the boxes.
function needRows() {
  const rows = [];
  const consider = (t) => {
    const live = mesh.live.get(t.node.path);
    const st = live ? { state: live.state, text: live.text, at: live.since } : t.state;
    if (NEEDS.has(st.state)) {
      rows.push({ t, path: t.node.path, state: st.state, text: st.text || "", at: st.at || "", stale: isStale(st) });
    }
  };
  for (const t of mesh.tree ? mesh.tree.leads : []) {
    consider(t);
    if (t.node.role === "round") for (const c of t.children || []) consider(c);
  }
  const at = (r) => { const v = Date.parse(r.at); return isNaN(v) ? 0 : v; };
  return rows.sort((a, b) => needRank(a.state) - needRank(b.state) || at(b) - at(a) || byStart(a.t, b.t));
}

// Dismissals are this browser's, never the mesh's: localStorage maps a
// dismissed row's path to its state's at, so the node's next state shows
// again. DISMISS_SLACK_MS covers a live event's at and the stored state's
// at, which its writer stamps a moment apart.
const DISMISS_KEY = "bmesh.dismissed";
const DISMISS_SLACK_MS = 5000;

function dismissals() {
  try {
    const v = JSON.parse(localStorage.getItem(DISMISS_KEY) || "{}");
    return v && typeof v === "object" ? v : {};
  } catch (e) {
    return {};
  }
}

function isDismissed(r, gone) {
  const at = Date.parse(r.at), was = Date.parse(gone[r.path] || "");
  return !isNaN(at) && !isNaN(was) && at - was <= DISMISS_SLACK_MS;
}

// dismiss hides row r until its node's next state, keeping only the
// dismissals of rows still in the band.
function dismiss(r) {
  const gone = dismissals(), keep = {};
  for (const x of needRows()) if (gone[x.path]) keep[x.path] = gone[x.path];
  keep[r.path] = r.at;
  try {
    localStorage.setItem(DISMISS_KEY, JSON.stringify(keep));
  } catch (e) {
    // Storage off: nothing to hide it with.
  }
  renderBand();
}

// needRow is one band row: its badge and path a link to the lead page, its
// words (for silent, the daemon's verdict and its age) clamped to three
// lines until tapped, and, when x, the × that dismisses it.
function needRow(r, x) {
  const who = el("a", "who tap", null, badge(r.state), el("span", "p", r.path));
  who.setAttribute("href", "#/lead/" + r.path);
  const say = el("div", "say", r.text);
  if (r.state === "silent" && r.at) say.append(r.text ? " · " : "", clock("since", r.at, false));
  clampRow(say, r.path);
  const row = el("div", "need tap st-" + r.state, null, who, say);
  if (x) {
    const b = el("button", "dismiss tap", "×");
    b.type = "button";
    b.title = "hide until it changes";
    b.setAttribute("aria-label", "hide " + r.path + " until it changes");
    b.addEventListener("click", () => dismiss(r));
    row.append(b);
  }
  return row;
}

// olderFold is the band's foot for its stale and dismissed rows: one muted
// line, "1 older: <path> <state> <age>", opening in place to the rows.
function olderFold(rows, open) {
  const age = (r) => {
    const ms = Date.now() - Date.parse(r.at);
    return isNaN(ms) ? "" : " " + dur(ms).split(" ")[0];
  };
  const head = el("summary", "tap", null, el("span", null, rows.length + " older: " +
    rows.map((r) => r.path + " " + r.state + age(r)).join(", ")));
  const det = el("details", "older", null, head, ...rows.map((r) => needRow(r, false)));
  det.open = open;
  return det;
}

// renderBand draws the band between the strip and the map: a needRow per
// needRows entry, the flagRows, the stale and dismissed ones folded into
// olderFold at the foot, then the operatorRows. Hidden when nothing needs the operator.
function renderBand() {
  const band = $("needs");
  const open = band.querySelector("details.older[open]") !== null;
  const gone = dismissals(), live = [], older = [];
  for (const r of needRows()) (r.stale || isDismissed(r, gone) ? older : live).push(r);
  const rows = live.map((r) => needRow(r, true));
  rows.push(...flagRows());
  if (older.length) rows.push(olderFold(older, open));
  band.replaceChildren(...rows, ...operatorRows());
  band.hidden = band.childElementCount === 0;
  if (check) checkSoon();
}

// flagRows are the band's rows for the flagged live agents (/api/usage's
// band, runaway first): "▲ runaway" or "▲ overworking", the agent linking
// its page, the flags' words. A row goes when the usage read drops it.
function flagRows() {
  return (tok.data && tok.data.band || []).map((b) => {
    const lvl = b.level === "runaway" ? "runaway" : "overworking";
    const who = el("a", "who tap", null, el("span", "badge fl-" + lvl, "▲ " + lvl), el("span", "p", agentName(b.agent)));
    who.setAttribute("href", agentHref(b.agent));
    const say = el("div", "say", flagWords(b.flags));
    clampRow(say, "flag " + b.agent);
    return el("div", "need flag fl-" + lvl, null, who, say);
  });
}

// operatorRows are the band's rows for landed specs with steps for the
// operator still open, after the nodes: the spec, how many are open, and
// the command that lists and ticks them.
function operatorRows() {
  return (mesh.tree ? mesh.tree.operator : []).map((o) => {
    const who = el("span", "who", null, badge("operator"), el("span", "p", o.repo + " " + o.id));
    const say = el("div", "say", o.title + ": " + o.open + " of " + o.total + " open · bmesh spec steps " + o.id + " --repo " + o.repo);
    return el("div", "need st-operator", null, who, say);
  });
}

// --- the lead page

// specBody renders a spec's markdown as text: its frontmatter dropped,
// "#" lines bold, fenced blocks monospace, every other line a text line
// (a blank line after text a small gap).
function specBody(text) {
  const out = el("div", "spec-body");
  let lines = String(text || "").replace(/\r\n/g, "\n").split("\n");
  if (lines[0] && lines[0].trim() === "---") {
    const end = lines.findIndex((l, i) => i > 0 && l.trim() === "---");
    if (end > 0) lines = lines.slice(end + 1);
  }
  let pre = null, afterText = false;
  for (const line of lines) {
    if (line.trimStart().startsWith("```")) {
      if (pre) {
        out.append(pre);
        pre = null;
      } else {
        pre = el("pre");
      }
      continue;
    }
    if (pre) {
      pre.append(line + "\n");
    } else if (line.startsWith("#")) {
      out.append(el("strong", "h", line.replace(/^#+\s*/, "")));
    } else if (line.trim()) {
      out.append(line, el("br"));
    } else if (afterText) {
      out.append(el("span", "gap"));
    }
    afterText = !pre && line.trim() !== "" && !line.startsWith("#");
  }
  if (pre) out.append(pre);
  return out;
}

function card(title, ...children) {
  const c = el("section", "card", null, el("h3", null, title), ...children);
  c.dataset.card = title;
  return c;
}

// bare is a card with nothing to show: one muted line. In the lead page's
// one-column layout it sorts after the cards with content.
function bare(title, text) {
  const c = el("section", "card bare", null, el("span", "k", title), " · " + text);
  c.dataset.card = title;
  return c;
}

// foldOpen holds the keys of the folds left open, so a redraw keeps them
// open: "<kind> <repo>" for a spent fold, "<round> <id>" for a round's spec.
const foldOpen = new Set();

function keepOpen(det, key) {
  if (foldOpen.has(key)) det.open = true;
  det.addEventListener("toggle", () => {
    if (det.open) foldOpen.add(key);
    else foldOpen.delete(key);
  });
  return det;
}

function field(name, value) {
  return el("div", "field", null, el("span", "k", name), el("span", "v", value || "–"));
}

function msgText(m) {
  if (m.text) return m.text;
  if (m.reply) return (m.reply.ok ? "ok" : "fail") + (m.reply.output ? " " + m.reply.output : "");
  if (m.job) return m.job.text || m.job.kind || "";
  return "";
}

function safeURL(u) {
  return /^https?:\/\//i.test(String(u || "")) ? String(u) : "";
}

// shortPath drops the lead's own prefix from the paths in s (one, or a
// message's "from → to"): "eng", not "buildermesh-round-1.eng".
function shortPath(s, lead) {
  return String(s).split(" → ").map((p) => p.startsWith(lead + ".") ? p.slice(lead.length + 1) : p).join(" → ");
}

function renderLead(path, v) {
  const head = $("lead-head"), panel = $("lead-panel");
  if (!v) {
    head.replaceChildren();
    panel.replaceChildren(el("p", "empty", "no such node: " + path));
    return;
  }
  const n = v.node || {}, st = v.state || {}, sp = v.spec;
  const parts = [badge(st.state)];
  if (n.spec || sp) parts.push(el("span", "hd-spec", "spec " + (n.spec || sp.id) + (sp && sp.title ? " · " + sp.title : "")));
  for (const f of [n.host, n.model]) if (f) parts.push(el("span", "mono muted", f));
  const began = Date.parse(n.started_at || "");
  if (!isNaN(began)) {
    const ended = st.state === "done" ? Date.parse(st.at || "") : NaN;
    parts.push(el("span", "mono muted", "started " + md(began / 1000) + " " + hm(began / 1000) + "Z"),
      el("span", "mono muted", dur((isNaN(ended) ? hubNow() * 1000 : ended) - began)));
  }
  if (st.text) parts.push(el("span", "muted", st.text));
  if (n.branch) parts.push(el("code", "branch", n.branch));
  const url = safeURL(v.console_url);
  if (url) {
    const a = el("a", "btn tap", "console");
    a.setAttribute("href", url);
    a.setAttribute("target", "_blank");
    a.setAttribute("rel", "noopener");
    parts.push(a);
  }
  head.replaceChildren(...parts);

  const main = [];
  const kids = v.children || [];
  if (kids.length) main.push(card("tree", el("ol", "rows", null, ...kids.map(childRow))));
  if (n.role === "round") {
    const specs = v.specs || [];
    main.push(specs.length ? card("specs", el("ol", "rows", null, ...specs.map((s) => roundSpec(path, s))))
      : bare("specs", "no specs landed yet"));
  } else if (sp) {
    const merged = sp.merged ? String(sp.merged).slice(0, 8) : "";
    main.push(card("spec " + (sp.id || n.spec || ""), el("p", "spec-title", sp.title || "(untitled)"),
      el("div", "fields", null, field("status", sp.status), field("merged", merged),
        field("verified", sp.verified || "not verified")),
      specBody(sp.text)));
  } else {
    main.push(bare("spec", n.spec ? "spec " + n.spec + " is not on main of a repo configured here" : "no spec"));
  }

  const inbox = (v.inbox || []).map((m) => {
    const li = el("li", null, null,
      el("span", "t", stamp(m.at)),
      el("span", "p", (m.from || "?") + " → " + (m.kind || "message") + ":"),
      el("span", "x", msgText(m)));
    clampRow(li, "inbox " + (m.id || (m.from || "") + "@" + (m.at || "")));
    return li;
  });
  const tl = (v.timeline || []).slice(-TIMELINE_MAX).reverse().map((f) => {
    const d = describe(f);
    const who = el("span", "p", shortPath(d.path, path));
    who.title = d.path;
    const li = el("li", "ev-" + d.event, null, el("span", "t", stamp((f.payload && f.payload.at) || f.at)),
      who, el("span", "e", d.event), el("span", "x", d.text));
    clampRow(li, frameKey(f));
    return li;
  });
  const used = tok.lead && tok.lead.path === path ? tok.lead.data : null;
  panel.replaceChildren(el("div", "stack", null, ...main), el("div", "stack", null, tokensCard(used),
    inbox.length ? card("inbox", el("ol", "rows", null, ...inbox)) : bare("inbox", "no messages"),
    tl.length ? card("timeline", el("ol", "rows", null, ...tl)) : bare("timeline", "no events in the last day")));
}

// childRow is one of the node's children in the tree card: a link to its
// own page.
function childRow(c) {
  const n = c.node || {}, path = String(n.path || "?");
  const a = el("a", "child tap", null, badge(c.state && c.state.state), el("span", "p", lastToken(path)));
  if (n.branch) a.append(el("code", "branch", n.branch));
  a.append(el("span", c.unread > 0 ? "unread" : "muted", (c.unread || 0) + " unread"));
  a.setAttribute("href", "#/lead/" + encodeURIComponent(path));
  a.title = path;
  return el("li", null, null, a);
}

// roundSpec is one of a round's specs, "NNN title · status", opening its
// body in place.
function roundSpec(round, s) {
  const det = el("details", "fold-open", null,
    el("summary", "tap", (s.id || "?") + " " + (s.title || "(untitled)") + " · " + (s.status || "?")), specBody(s.text));
  return el("li", "fold", null, keepOpen(det, round + " " + s.id));
}

const lead = loader(async () => {
  const path = pages.lead;
  if (!path) return;
  const get = (url) => fetchJSON(url).catch(() => ({ status: 0, body: null }));
  const vs = tok.lead && tok.lead.path === path ? tok.lead.vs : "";
  const [r, t, u] = await Promise.all([get("/api/lead/" + encodeURIComponent(path)),
    get("/api/timeline?lead=" + encodeURIComponent(path)),
    get("/api/usage/agent?agent=" + encodeURIComponent(path) + (vs ? "&vs=" + encodeURIComponent(vs) : ""))]);
  if (pages.lead !== path) return; // the route moved on
  if (u.body || u.status === 404) tok.lead = { path, vs, data: u.body };
  if (t.body) {
    t.body.lanes = t.body.lanes || [];
    replayLive(t.body);
    tl.lead = { path, data: t.body };
  }
  if (r.status === 404) {
    renderLead(path, null);
  } else if (r.body) {
    renderLead(path, r.body);
  } else if (pages.leadShown !== path) {
    $("lead-panel").replaceChildren(el("p", "empty", "could not load " + path + "; retrying on the next event"));
    return;
  } else {
    return; // keep what is shown
  }
  pages.leadShown = path;
  drawAll();
  comparePeers(path);
  if (check) checkSoon();
});

// touchesLead reports whether a live frame is about the lead shown: its
// tree's events, its inbox, or a daemon event naming it.
function touchesLead(rest, p) {
  const l = pages.lead;
  if (!l) return false;
  if (rest === "inbox." + l) return true;
  if (rest.startsWith("daemon.")) return typeof p.to === "string" && (p.to === l || p.to.startsWith(l + "."));
  return rest.startsWith(l + ".");
}

// --- the routines panel

function leaseRow(l) {
  return el("li", null, null, el("span", "p", l.key), el("span", "muted", l.holder), clock("lease", l.expires_at, true));
}

// RANK orders a repo's instances: working, then any other live state, idle,
// then spent.
const RANK = { working: 0, idle: 2, done: 3 };

function rankOf(i) {
  const s = i.state && i.state.state;
  return s in RANK ? RANK[s] : 1;
}

function instanceRow(k, i) {
  const path = (i.node && i.node.path) || "?";
  const prefix = "routine." + k.kind + ".";
  const li = el("li", null, null, el("span", "p", path.startsWith(prefix) ? path.slice(prefix.length) : path),
    badge(i.state && i.state.state), el("span", "muted", instanceSub(i).join(" · ")));
  li.title = path;
  return li;
}

// instanceRows is a kind's instances under an h4 per repo, its done ones
// folded into one "spent (N)" row per repo.
function instanceRows(k) {
  const byRepo = new Map();
  for (const i of k.instances || []) {
    const repo = (i.node && i.node.repo) || "";
    if (!byRepo.has(repo)) byRepo.set(repo, []);
    byRepo.get(repo).push(i);
  }
  const out = [];
  for (const repo of [...byRepo.keys()].sort()) {
    const list = byRepo.get(repo).slice().sort((a, b) =>
      rankOf(a) - rankOf(b) || String(a.node && a.node.path).localeCompare(String(b.node && b.node.path)));
    const live = list.filter((i) => rankOf(i) < RANK.done), spent = list.filter((i) => rankOf(i) === RANK.done);
    const rows = live.map((i) => instanceRow(k, i));
    if (spent.length) {
      const det = el("details", "fold-open spent", null, el("summary", "tap", "spent (" + spent.length + ")"),
        el("ol", "rows", null, ...spent.map((i) => instanceRow(k, i))));
      rows.push(el("li", "fold", null, keepOpen(det, k.kind + " " + repo)));
    }
    out.push(el("h4", null, repo || "no repo"), el("ol", "rows", null, ...rows));
  }
  return out;
}

function renderRoutines(v) {
  const cards = (v.routines || []).map((k) => {
    const tags = el("div", "tags", null, el("span", "tag", k.mode || "no kind file"));
    if (k.deterministic) tags.append(el("span", "tag det", "deterministic"));
    tags.append(el("span", "tag", (k.depth || 0) + " queued"),
      el("span", "tag", (k.results === 1 ? "1 result" : (k.results || 0) + " results")));
    const inst = instanceRows(k);
    const leases = (k.leases || []).map(leaseRow);
    return card(k.kind, tags,
      ...(inst.length ? inst : [el("p", "empty", "no instances")]),
      ...(leases.length ? [el("h4", null, "leases"), el("ol", "rows", null, ...leases)] : []));
  });
  const free = (v.leases || []).map(leaseRow);
  if (free.length) cards.push(card("other leases", el("ol", "rows", null, ...free)));
  $("routines-panel").replaceChildren(...(cards.length ? cards : [el("p", "empty", "No routine kinds.")]));
  if (check) checkSoon();
}

const routines = loader(async () => {
  if (pages.view !== "routines") return;
  const r = await fetchJSON("/api/routines");
  if (r.body) renderRoutines(r.body);
});

// --- the KPI row and the digest

// every is the daemon's digest period ([notify] digest_every) in ms.
const strip = { quota: [], digest: null, every: 0 };

function pct(x) {
  return Math.round(+x || 0) + "%";
}

// fiveH is a quota reading's 5-hour use; null is a plan with no such window.
function fiveH(r) {
  return r.five_hour == null ? "none" : pct(r.five_hour);
}

// tile is one KPI: a small-caps label, a large mono value and a mono
// subline; a link when href is given.
function tile(label, value, sub, href) {
  const t = el(href ? "a" : "div", "tile" + (href ? " tap" : ""), null, el("div", "k", label),
    value instanceof Node ? value : el("div", "v", value), el("div", "s", null, ...[].concat(sub)));
  if (href) t.setAttribute("href", href);
  return t;
}

// list is up to n of names joined with " · ", "+N" for the rest.
function list(names, n) {
  const more = names.length > n ? " +" + (names.length - n) : "";
  return names.slice(0, n).join(" · ") + more;
}

// renderKpis draws the eight tiles from the tree (with live states), the
// window's timeline and usage, the routines and the quota readings.
function renderKpis() {
  const box = $("kpis");
  const lanes = tl.data ? tl.data.lanes : [];
  const paths = new Set(lanes.map((l) => l.path));
  const short = shortener(lanes.filter((l) => l.role !== "routine" && !paths.has(l.parent)));
  const live = [], waiting = [];
  eachNode((t, s) => {
    if (LIVE.has(s)) live.push(t.node.path);
    if (s === "asking" || s === "blocked") waiting.push(t.node.path);
  });
  const from = hubNow() - WINDOWS[tl.win];
  const inWin = (l, kind) => l.ticks.filter((k) => k[1] === kind && k[0] >= from).length;
  const landed = lanes.filter((l) => l.role === "lead" && inWin(l, "done"));
  let refused = 0, worst = null, worstN = 0;
  for (const l of lanes) {
    const n = inWin(l, "refused");
    refused += n;
    if (n > worstN) [worst, worstN] = [l, n];
  }
  let queued = 0, running = 0;
  for (const g of mesh.tree ? mesh.tree.routines : []) {
    queued += g.depth || 0;
    for (const i of g.instances || []) if (i.state && i.state.state === "working") running++;
  }
  const win = tl.data ? "" : " …";
  const u = usageNow();
  box.replaceChildren(
    tile("Live agents", mesh.tree ? live.length : "–", list(live.map(short), 3)),
    tile("Tokens read · " + tl.win, u ? fmtTok(u.read) : "–", u ? "out " + fmtTok(u.out) : "", "#/tokens"),
    tile("Last hour", u ? fmtTok(u.hour) : "–", "read"),
    tile("Landed · " + tl.win, landed.length + win, list(landed.map((l) => l.spec || short(l.path)), 6)),
    flagsTile(u, refused + win, worst ? short(worst.path) + " ×" + worstN : ""),
    tile("Asking / blocked", mesh.tree ? waiting.length : "–", list(waiting.map(short), 3)),
    quotaTile(),
    tile("Routines", queued, [queued === 1 ? "job queued · " : "jobs queued · ", running + " running"], "#/routines"));
  if (check) checkSoon();
}

// flagsTile counts the window's agents by their worst flag, runaway and
// overworking, over the land refusals in the window (and the most refused).
function flagsTile(u, refused, worst) {
  const n = { runaway: 0, overworking: 0 };
  for (const a of u ? u.agents : []) {
    const l = worstLevel(a.flags);
    if (l in n) n[l]++;
  }
  const line = (l) => el("div", n[l] ? "fl-" + l : "muted", "▲ " + n[l] + " " + l);
  return tile("Flags · " + tl.win, u ? el("div", "v small", null, line("runaway"), line("overworking")) : "–",
    "land refused " + refused + (worst ? " · " + worst : ""));
}

// quotaTile is each provider's 5h and week, a meter for the higher week
// and that week's reset.
function quotaTile() {
  const q = strip.quota || [];
  if (!q.length) return tile("Quota", "–", "no reading");
  const top = q.reduce((a, b) => (+b.week || 0) > (+a.week || 0) ? b : a);
  const lines = el("div", "v small", null, ...q.map((r) => {
    const high = +r.five_hour >= 80 || +r.week >= 80;
    return el("div", high ? "high" : "", (high ? "▲ " : "") + (r.provider || "?") + " 5h " + fiveH(r) + " · wk " + pct(r.week));
  }));
  const meter = el("div", "meter" + (+top.week >= 80 ? " high" : ""), null, el("i"));
  meter.firstChild.style.width = Math.max(0, Math.min(100, +top.week || 0)) + "%";
  meter.setAttribute("role", "img");
  meter.setAttribute("aria-label", (top.provider || "?") + " week " + pct(top.week));
  const sub = [meter, (top.provider || "?") + " wk "];
  if (top.week_reset) sub.push(clock("reset", top.week_reset, true));
  if (top.read_at) sub.push(" ", clock("read", top.read_at, false));
  return tile("Quota", lines, sub);
}

// freshDigest is the newest digest while it is younger than twice the
// digest period, else null: an older one no longer says what is going on.
function freshDigest() {
  const d = strip.digest;
  const age = d ? Date.now() - Date.parse(d.at) : NaN;
  return d && strip.every > 0 && age > 2 * strip.every ? null : d;
}

// renderStrip draws the digest fold under the KPI row; an open one stays open.
function renderStrip() {
  const s = $("strip");
  const d = freshDigest();
  const open = s.querySelector("details[open]") !== null;
  if (!d) {
    s.hidden = true;
    s.replaceChildren();
    return;
  }
  const head = el("summary", "tap", null, clock("digest", d.at, false), ": " + (d.title || ""));
  let row;
  if (d.body) {
    row = el("details", "digest", null, head, el("pre", null, d.body));
    if (open) row.setAttribute("open", "");
  } else {
    row = el("div", "digest", null, ...head.childNodes);
  }
  s.replaceChildren(row);
  s.hidden = false;
}

async function loadStrip() {
  try {
    const r = await fetchJSON("/api/strip");
    if (!r.body) return; // keep the last strip
    strip.quota = r.body.quota || [];
    strip.digest = r.body.digest || null; // null: no digest any more
    strip.every = (+r.body.digest_every || 0) * 1000;
    renderStrip();
    renderKpis();
  } catch (e) {
    // The next minute tries again.
  }
}

// liveDigest shows a daemon digest from a live frame at once, without a
// read (the strip reads at most once a minute).
function liveDigest(f) {
  const p = f.payload && typeof f.payload === "object" ? f.payload : {};
  strip.digest = { at: p.at || f.at, title: String(p.text || ""), body: String(p.body || "") };
  renderStrip();
}

// pagesApply routes a live frame to the open page.
function pagesApply(f) {
  const rest = f.subject.replace(/^bm\./, "");
  const p = f.payload && typeof f.payload === "object" ? f.payload : {};
  if (/^daemon\.[^.]+\.digest$/.test(rest)) liveDigest(f);
  if (pages.view === "lead" && touchesLead(rest, p)) lead.soon();
  if (pages.view === "routines" && (rest.startsWith("routine.") || rest.startsWith("inbox.routine."))) routines.soon();
  if (rest.startsWith("routine.") || rest.startsWith("inbox.routine.")) treeSoon(); // the Routines tile
}
// --- tokens: the usage panels (107) -------------------------------------------

// ROLES are the tokens-by-role series in their fixed order, each drawn in
// map.css's --r-<name>; design and exec are a coordinator's phases, never a
// state's colour.
const ROLES = [["lead", "leads"], ["sub", "subagents"], ["design", "design"], ["exec", "exec"], ["routine", "routines"]];
const AGENTS_SHOWN = 20; // the overview's agents table; the tokens tab has every row
const PEERS_MS = 5 * 60000; // the compare list's read of the week's agents
// PROVIDERS gives each quota provider its colour slot, by name, so a
// provider missing from the window never repaints another.
const PROVIDERS = ["claude", "codex"];

const tok = {
  data: null, // the last /api/usage, for window win
  win: "",
  sel: "", // the agent the overview's context panel shows; "" = the default
  ctx: null, // {agent, data}: that panel's /api/usage/agent
  sort: { key: "read", dir: -1 }, // the tokens tab's agents table
  lead: null, // {path, vs, data}: the lead page's /api/usage/agent
  peers: null, // {at, agents}: the week's agents, for the compare select
  session: null, // {agent, data}: the session page's
};

// fmtTok renders a token count: 416M, 42.2M, 347k, 19k, 845.
function fmtTok(n) {
  n = +n || 0;
  const a = Math.abs(n);
  if (a >= 1e9) return (n / 1e9).toFixed(a >= 1e10 ? 0 : 1) + "G";
  if (a >= 1e6) return (n / 1e6).toFixed(a >= 1e8 ? 0 : 1) + "M";
  if (a >= 1e3) return (n / 1e3).toFixed(a >= 1e4 ? 0 : 1) + "k";
  return String(Math.round(n));
}

function kindsRead(k) {
  return k ? (+k.in || 0) + (+k.cr || 0) + (+k.cw || 0) : 0;
}

// niceMax rounds v up to a round multiple of a power of ten, whose
// quarters are round too.
function niceMax(v) {
  if (!(v > 0)) return 1;
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 1.2, 1.6, 2, 2.4, 3, 4, 5, 6, 8, 10]) if (v <= m * p) return m * p;
  return 10 * p;
}

// sessionOf splits session.<host>.<id> into {host, id}; null for a node.
function sessionOf(agent) {
  const m = /^session\.([^.]+)\.(.+)$/.exec(String(agent || ""));
  return m ? { host: m[1], id: m[2] } : null;
}

// agentName is how an agent reads in a row: "session <id8>" or its path.
function agentName(agent) {
  const s = sessionOf(agent);
  return s ? "session " + s.id.slice(0, 8) : String(agent);
}

// agentHref is an agent's page: a session's, else its lead page.
function agentHref(agent) {
  const s = sessionOf(agent);
  return s ? "#/session/" + encodeURIComponent(s.host) + "/" + encodeURIComponent(s.id) : "#/lead/" + agent;
}

// usageNow is the loaded /api/usage when it is the window shown.
function usageNow() {
  return tok.data && tok.win === tl.win ? tok.data : null;
}

const usage = loader(async () => {
  if (pages.view !== "map" && pages.view !== "timeline" && pages.view !== "tokens") return;
  const win = tl.win;
  const r = await fetchJSON("/api/usage?since=" + win);
  if (!r.body || win !== tl.win) return;
  const d = r.body;
  for (const k of ["agents", "sessions", "quota"]) d[k] = d[k] || [];
  tok.data = d;
  tok.win = win;
  drawAll();
  renderBand();
  ctxPanel.run();
});

// worstLevel is the worst of an agent's flags (the API lists them worst
// first): "runaway", "overworking" or "".
function worstLevel(flags) {
  return flags && flags.length ? flags[0].level : "";
}

// flagWords are the flags' words, the worst first.
function flagWords(flags) {
  return (flags || []).map((f) => f.words).join("; ");
}

// flagMap maps each flagged agent of a usage read (its rows and its band)
// to {level, words}.
function flagMap(d) {
  const m = new Map();
  for (const a of d.agents) if (worstLevel(a.flags)) m.set(a.agent, { level: worstLevel(a.flags), words: flagWords(a.flags) });
  for (const b of d.band || []) m.set(b.agent, { level: b.level, words: flagWords(b.flags) });
  return m;
}

// ctxAgent is the agent the overview's context panel shows: the row
// selected, else the live agent with the largest context, else the
// window's largest reader.
function ctxAgent() {
  const as = usageNow() ? tok.data.agents : [];
  if (tok.sel && as.some((a) => a.agent === tok.sel)) return tok.sel;
  let best = null;
  for (const a of as) if (a.live && (!best || a.last_ctx > best.last_ctx)) best = a;
  return best ? best.agent : as.length ? as[0].agent : "";
}

const ctxPanel = loader(async () => {
  if (pages.view !== "map" || !usageNow()) return;
  const agent = ctxAgent(), win = tl.win;
  if (!agent) {
    tok.ctx = null;
    drawCtxPanel();
    return;
  }
  const r = await fetchJSON("/api/usage/agent?agent=" + encodeURIComponent(agent) + "&since=" + win);
  if (win !== tl.win || ctxAgent() !== agent) return;
  tok.ctx = { agent, data: r.body };
  drawCtxPanel();
});

// select makes agent the context panel's, from a click on its row.
function select(agent) {
  tok.sel = agent;
  drawAgents($("agents-map"), { limit: AGENTS_SHOWN, select: true });
  ctxPanel.run();
}

// tally keeps, under ?check=1, a panel's count when its view is shown.
function tally(box, key, n) {
  if (!check || !box || box.closest(".view").hidden) return;
  check[key] = n;
  checkSoon();
}

// showTip puts the tip with text beside the pointer of ev.
function showTip(ev, text) {
  const tip = $("tip");
  tip.textContent = text;
  tip.hidden = false;
  const w = tip.offsetWidth, h = tip.offsetHeight;
  tip.style.left = Math.max(4, Math.min(ev.clientX + 14, window.innerWidth - w - 4)) + "px";
  tip.style.top = (ev.clientY + 14 + h > window.innerHeight ? ev.clientY - h - 8 : ev.clientY + 14) + "px";
}

// crosshair lays a hit area over a plot (P its padding in a W x H svg):
// at(x) gives the crosshair's x and the tip's words for a pointer at x,
// or null for none.
function crosshair(root, P, W, H, at) {
  const line = svg("line", { class: "xhair", x1: 0, x2: 0, y1: P.t, y2: H - P.b, visibility: "hidden" }, root);
  const hit = svg("rect", { class: "hit", x: P.l, y: 0, width: Math.max(0, W - P.l - P.r), height: H }, root);
  const hide = () => {
    $("tip").hidden = true;
    line.setAttribute("visibility", "hidden");
  };
  hit.addEventListener("pointermove", (ev) => {
    const r = at(ev.clientX - root.getBoundingClientRect().left);
    if (!r) return hide();
    line.setAttribute("x1", r.x);
    line.setAttribute("x2", r.x);
    line.setAttribute("visibility", "visible");
    showTip(ev, r.text);
  });
  hit.addEventListener("pointerleave", hide);
}

// timeAxis draws the grid lines and labels of [from, to] under a plot.
function timeAxis(root, x, from, to, top, bottom, H) {
  const step = axisStep(to - from);
  let lastLabel = -Infinity;
  for (let t = Math.ceil(from / step) * step; t <= to; t += step) {
    const X = x(t);
    svg("line", { class: "grid", x1: X, x2: X, y1: top, y2: bottom }, root);
    if (X - lastLabel >= 52 && X <= +root.getAttribute("width") - 18) {
      svgText({ class: "axis", x: X, y: H - 6, "text-anchor": "middle" }, step >= 86400 ? md(t) : hm(t), root);
      lastLabel = X;
    }
  }
}

// yAxis draws four grid lines up to max with their labels at the left.
function yAxis(root, y, max, P, W, fmt) {
  for (let i = 0; i <= 4; i++) {
    const v = max * i / 4, Y = y(v);
    svg("line", { class: "grid", x1: P.l, x2: W - P.r, y1: Y, y2: Y }, root);
    svgText({ class: "axis", x: P.l - 6, y: Y + 4, "text-anchor": "end" }, fmt(v), root);
  }
}

function chartRoot(W, H, label) {
  return svg("svg", { width: W, height: H, viewBox: `0 0 ${W} ${H}`, role: "img", "aria-label": label });
}

// legendOf is a legend row: [class, words] pairs, each a swatch and its name.
function legendOf(items) {
  return el("div", "legend", null, ...items.map(([cls, words]) => el("span", null, null, el("i", cls), words)));
}

// drawRoles draws the tokens-by-role chart: the five series stacked in
// their fixed order, a surface gap between fills, a crosshair tip giving
// each series and the bucket's total.
function drawRoles(box) {
  if (!box) return;
  const d = usageNow();
  if (!d) return box.replaceChildren(el("p", "empty", "loading…"));
  const r = d.roles || {}, step = +r.step || 60, from = +r.from || d.from, series = r.series || {};
  const n = Math.max(0, ...ROLES.map(([k]) => (series[k] || []).length));
  const val = (k, i) => +((series[k] || [])[i]) || 0;
  const drawn = ROLES.filter(([k]) => (series[k] || []).some((v) => v > 0));
  tally(box, "roles", drawn.length);
  const leg = legendOf(ROLES.map(([k, words]) => ["r-" + k, words]));
  if (!n || !drawn.length) return box.replaceChildren(leg, el("p", "empty", "No tokens read in this window."));
  const W = Math.max(box.clientWidth, 240), H = box.classList.contains("tall") ? 260 : 210;
  const P = { l: 46, r: 8, t: 8, b: 20 };
  let top = 0;
  const totals = [];
  for (let i = 0; i < n; i++) {
    totals.push(ROLES.reduce((s, [k]) => s + val(k, i), 0));
    top = Math.max(top, totals[i]);
  }
  const yMax = niceMax(top), to = d.to;
  const x = (t) => P.l + (Math.max(from, Math.min(to, t)) - from) / Math.max(1, to - from) * (W - P.l - P.r);
  const y = (v) => H - P.b - v / yMax * (H - P.t - P.b);
  const root = chartRoot(W, H, "tokens read by role");
  yAxis(root, y, yMax, P, W, fmtTok);
  timeAxis(root, x, from, to, P.t, H - P.b, H);
  const mid = (i) => x(from + i * step + step / 2);
  const below = new Array(n).fill(0);
  for (const [k] of ROLES) {
    if (!(series[k] || []).some((v) => v > 0)) continue;
    const upper = [], lower = [];
    for (let i = 0; i < n; i++) {
      lower.push([mid(i), y(below[i])]);
      below[i] += val(k, i);
      upper.push([mid(i), y(below[i])]);
    }
    const pts = upper.concat(lower.reverse()).map(([a, b]) => a.toFixed(1) + "," + b.toFixed(1));
    svg("path", { class: "area r-" + k, d: "M" + pts.join("L") + "Z" }, root);
  }
  crosshair(root, P, W, H, (px) => {
    const i = Math.max(0, Math.min(n - 1, Math.round((px - P.l) / (W - P.l - P.r) * (to - from) / step - 0.5)));
    const t = from + i * step;
    const lines = ROLES.map(([k, words]) => words.padEnd(10) + fmtTok(val(k, i)).padStart(7));
    return { x: mid(i), text: hm(t) + "–" + hm(t + step) + "\n" + lines.join("\n") + "\n" + "total".padEnd(10) + fmtTok(totals[i]).padStart(7) };
  });
  box.replaceChildren(leg, root);
}

// AGENT_COLS are the agents table's columns: key, heading, the value it
// sorts by, and whether it hides on a phone.
const AGENT_COLS = [
  ["agent", "agent", (a) => agentName(a.agent), false],
  ["role", "role", (a) => a.role || "", true],
  ["size", "size / repo", (a) => a.size || a.repo || "", false],
  ["state", "state", (a) => a.state || "", true],
  ["read", "read", (a) => +a.read || 0, false],
  ["mainsub", "main · sub", (a) => kindsRead(a.sub), true],
  ["peak", "peak ctx", (a) => +a.peak || 0, false],
  ["wall", "wall", (a) => (+a.last || 0) - (+a.first || 0), true],
  ["out", "out", (a) => +a.out || 0, true],
  // runaway first when sorted ascending, as a word column sorts on its first click
  ["watch", "watch", (a) => -(LEVEL_RANK[worstLevel(a.flags)] || 0), true],
];
const LEVEL_RANK = { runaway: 2, overworking: 1 };

// drawAgents draws the agents table: with opts.limit its first rows and a
// link to the tokens tab, with opts.sortable a header that sorts it, with
// opts.select rows a click selects for the context panel.
function drawAgents(box, opts) {
  if (!box) return;
  const d = usageNow();
  if (!d) return box.replaceChildren(el("p", "empty", "loading…"));
  let as = d.agents.slice();
  if (opts.sortable) {
    const col = AGENT_COLS.find((c) => c[0] === tok.sort.key) || AGENT_COLS[4];
    as.sort((a, b) => {
      const x = col[2](a), y = col[2](b);
      return (x < y ? -1 : x > y ? 1 : 0) * tok.sort.dir || (+b.read || 0) - (+a.read || 0);
    });
  }
  const all = as.length;
  if (opts.limit) as = as.slice(0, opts.limit);
  tally(box, "agents", as.length);
  if (!as.length) return box.replaceChildren(el("p", "empty", "No model calls in this window."));
  const top = Math.max(1, ...as.map((a) => +a.read || 0));
  const sel = opts.select ? ctxAgent() : "";
  const head = el("tr", null, null, ...AGENT_COLS.map(([key, words, , hide]) => {
    const num = !["agent", "role", "size", "state", "mainsub", "watch"].includes(key);
    const th = el("th", (num ? "r" : "") + (hide ? " hide-n" : "") + (key === "watch" ? " watch" : ""));
    if (!opts.sortable) {
      th.textContent = words;
      return th;
    }
    const b = el("button", "sort", words);
    b.type = "button";
    if (tok.sort.key === key) th.setAttribute("aria-sort", tok.sort.dir < 0 ? "descending" : "ascending");
    b.addEventListener("click", () => {
      tok.sort = { key, dir: tok.sort.key === key ? -tok.sort.dir : num ? -1 : 1 };
      drawAgents(box, opts);
    });
    th.append(b);
    return th;
  }));
  const rows = as.map((a) => {
    const name = el("a", "p", agentName(a.agent));
    name.setAttribute("href", agentHref(a.agent));
    name.title = a.agent + (a.repo ? " · " + a.repo : "");
    const lvl = worstLevel(a.flags);
    const who = el("td", "who" + (lvl ? " fl-" + lvl : ""), null, name);
    if (a.live) who.append(el("span", "livedot", " ●"));
    // Narrower than 1400px there's no room for the watch column: the words go under the name.
    if (lvl) who.append(el("div", "wsub fl-" + lvl, "▲ " + flagWords(a.flags)));
    const st = String(a.state || "");
    const bar = el("span", "ms", null, el("i", "m"), el("i", "s"));
    bar.firstChild.style.width = (kindsRead(a.main) / top * 100).toFixed(1) + "%";
    bar.lastChild.style.width = (kindsRead(a.sub) / top * 100).toFixed(1) + "%";
    bar.title = "main " + fmtTok(kindsRead(a.main)) + " · sub " + fmtTok(kindsRead(a.sub));
    const tr = el("tr", a.agent === sel ? "sel" : "", null, who,
      el("td", "hide-n", a.role || ""),
      el("td", null, a.size || a.repo || ""),
      el("td", "hide-n st st-" + st.replace(/[^a-z-]/g, ""), st ? (ICON[st] ? ICON[st] + " " : "") + st : "–"),
      el("td", "r", fmtTok(a.read)),
      el("td", "hide-n", null, bar),
      el("td", "r", a.peak ? fmtTok(a.peak) : "–"),
      el("td", "r hide-n", a.calls > 1 ? dur(((+a.last || 0) - (+a.first || 0)) * 1000) : "–"),
      el("td", "r hide-n", fmtTok(a.out)),
      el("td", "watch hide-n" + (lvl ? " fl-" + lvl : ""), lvl ? "▲ " + flagWords(a.flags) : ""));
    if (opts.select) {
      tr.classList.add("pick");
      tr.addEventListener("click", (ev) => {
        if (ev.target.closest("a")) return;
        select(a.agent);
      });
    }
    return tr;
  });
  const out = [el("div", "tw", null, el("table", "ag", null, el("thead", null, null, head), el("tbody", null, null, ...rows)))];
  if (opts.limit && all > as.length) {
    const more = el("a", "more tap", "more: all " + all + " agents on the tokens tab");
    more.setAttribute("href", "#/tokens");
    out.push(more);
  }
  box.replaceChildren(...out);
}

// phaseBar is a stacked bar of design (violet) against exec (magenta),
// scaled to top.
function phaseBar(design, exec, top) {
  const b = el("span", "ms ph", null, el("i", "d"), el("i", "e"));
  b.firstChild.style.width = (design / top * 100).toFixed(1) + "%";
  b.lastChild.style.width = (exec / top * 100).toFixed(1) + "%";
  b.title = "design " + fmtTok(design) + " · exec " + fmtTok(exec);
  return b;
}

function share(design, exec) {
  const t = design + exec;
  return t ? Math.round(design / t * 100) + "%" : "–";
}

// drawCoords draws a row per coordinator session: its design and exec
// read, the design share, peak context and last call.
function drawCoords(box) {
  if (!box) return;
  const d = usageNow();
  if (!d) return box.replaceChildren(el("p", "empty", "loading…"));
  const ss = d.sessions.slice().sort((a, b) => (b.design + b.exec) - (a.design + a.exec));
  const leg = legendOf([["r-design", "design"], ["r-exec", "exec"]]);
  if (!ss.length) return box.replaceChildren(leg, el("p", "empty", "No coordinator sessions in this window."));
  const top = Math.max(1, ...ss.map((s) => (+s.design || 0) + (+s.exec || 0)));
  const rows = ss.map((s) => {
    const name = el("a", "p", agentName(s.agent));
    name.setAttribute("href", agentHref(s.agent));
    name.title = s.agent + (s.repo ? " · " + s.repo : "");
    return el("tr", null, null, el("td", "who", null, name), el("td", null, null, phaseBar(+s.design || 0, +s.exec || 0, top)),
      el("td", "r", fmtTok(s.design)), el("td", "r", fmtTok(s.exec)), el("td", "r", share(+s.design || 0, +s.exec || 0)),
      el("td", "r hide-n", s.peak ? fmtTok(s.peak) : "–"), el("td", "r hide-n", s.last ? hm(s.last) : "–"));
  });
  const head = el("tr", null, null, ...["session", "design · exec", "design", "exec", "share", "peak", "last"].map((h, i) =>
    el("th", (i > 1 ? "r" : "") + (i > 4 ? " hide-n" : ""), h)));
  box.replaceChildren(leg, el("div", "tw", null, el("table", "ag", null, el("thead", null, null, head), el("tbody", null, null, ...rows))));
}

// mainCalls are a series' main-thread calls with the growth each added
// (null for the first and after a compaction's drop).
function mainCalls(calls) {
  const out = [];
  let prev = null;
  for (const c of calls || []) {
    if (c.sub) continue;
    out.push(Object.assign({}, c, { grow: prev === null ? null : c.ctx - prev }));
    prev = c.ctx;
  }
  return out;
}

// CTX_LINES are the [context] thresholds the upper chart rules, labelled.
const CTX_LINES = [["nudge_from", "nudge"], ["compact_from", "compact"], ["compact_soon", "compact soon"]];

// ctxChart draws an agent's context per call (d, /api/usage/agent) into
// box: above, the main thread's ctx per call with the thresholds dashed and
// each compaction marked; below, a bar per call for the growth it added,
// the two largest labelled, and the node's land refusals (✕) and readies
// (▲). The axis is opts.from to opts.to, else the agent's life; with d.vs
// both agents on minutes since their first call. Returns the points drawn.
function ctxChart(box, d, opts) {
  if (!box) return 0;
  if (!d || !d.agent) {
    box.replaceChildren(el("p", "empty", opts.empty || "No model calls."));
    return 0;
  }
  const mins = !!d.vs;
  const ser = [{ a: d.agent, calls: mainCalls(d.calls), marks: d.marks || [], events: d.events || [], cls: "s-a" }];
  if (d.vs) ser.push({ a: d.vs.agent, calls: mainCalls(d.vs.calls), marks: d.vs.marks || [], events: d.vs.events || [], cls: "s-b" });
  const first = (s) => +s.a.first || (s.calls[0] ? s.calls[0].at : 0);
  const tOf = (s, at) => mins ? at - first(s) : at;
  let from, to;
  if (mins) {
    from = 0;
    to = Math.max(600, ...ser.map((s) => s.calls.length ? tOf(s, s.calls[s.calls.length - 1].at) : 0));
  } else if (opts.from) {
    from = opts.from;
    to = opts.to;
  } else {
    const cs = ser[0].calls;
    from = cs.length ? cs[0].at : 0;
    to = Math.max(from + 600, cs.length ? cs[cs.length - 1].at : 0);
  }
  // The bottom padding holds the ✕ and ▲ row above the time labels.
  const W = Math.max(box.clientWidth, 240), P = { l: 46, r: 8, t: 8, b: 34 };
  const H1 = 170, GAP = 18, H2 = 80, H = P.t + H1 + GAP + H2 + P.b;
  const x = (t) => P.l + (Math.max(from, Math.min(to, t)) - from) / Math.max(1, to - from) * (W - P.l - P.r);
  const ctxs = d.context || {};
  const lines = CTX_LINES.map(([k, words]) => [+ctxs[k] || 0, words]).filter(([v]) => v > 0);
  const peak = Math.max(0, ...ser.flatMap((s) => s.calls.map((c) => c.ctx)));
  const yMax = niceMax(Math.max(peak, lines.length ? lines[0][0] : 0) * 1.05);
  const y = (v) => P.t + H1 - v / yMax * H1;
  const root = chartRoot(W, H, "context per model call");
  // The upper plot's axes, thresholds and lines.
  for (let i = 0; i <= 4; i++) {
    const v = yMax * i / 4;
    svg("line", { class: "grid", x1: P.l, x2: W - P.r, y1: y(v), y2: y(v) }, root);
    svgText({ class: "axis", x: P.l - 6, y: y(v) + 4, "text-anchor": "end" }, fmtTok(v), root);
  }
  if (mins) {
    const step = [600, 1200, 1800, 3600, 7200, 14400, 28800, 86400].find((s) => (to - from) / s <= 8) || 86400;
    for (let t = 0; t <= to; t += step) {
      svg("line", { class: "grid", x1: x(t), x2: x(t), y1: P.t, y2: H - P.b }, root);
      svgText({ class: "axis", x: x(t), y: H - 6, "text-anchor": "middle" }, Math.round(t / 60) + "m", root);
    }
  } else {
    timeAxis(root, x, from, to, P.t, H - P.b, H);
  }
  for (const [v, words] of lines) {
    if (v > yMax) continue;
    svg("line", { class: "ref", x1: P.l, x2: W - P.r, y1: y(v), y2: y(v) }, root);
    svgText({ class: "axis", x: W - P.r - 2, y: y(v) - 3 < P.t + 9 ? y(v) + 11 : y(v) - 3, "text-anchor": "end" }, words + " " + fmtTok(v), root);
  }
  let points = 0;
  for (const s of [...ser].reverse()) {
    const pts = s.calls.filter((c) => tOf(s, c.at) >= from && tOf(s, c.at) <= to);
    points += pts.length;
    if (pts.length) {
      svg("path", { class: "ctxline " + s.cls, d: "M" + pts.map((c) => x(tOf(s, c.at)).toFixed(1) + "," + y(c.ctx).toFixed(1)).join("L") }, root);
    }
    for (const m of s.marks) {
      if (m.sub || tOf(s, m.at) < from || tOf(s, m.at) > to) continue;
      const X = x(tOf(s, m.at));
      svg("line", { class: "cmark " + s.cls, x1: X, x2: X, y1: P.t, y2: P.t + H1 }, root);
      svgText({ class: "axis", x: X + 3, y: P.t + 9 }, "compacted", root);
    }
  }
  // The lower plot: the first agent's growth per call.
  const s0 = ser[0];
  const grows = s0.calls.filter((c) => c.grow > 0 && tOf(s0, c.at) >= from && tOf(s0, c.at) <= to);
  const gMax = niceMax(Math.max(1, ...grows.map((c) => c.grow)));
  const base = P.t + H1 + GAP + H2;
  const gy = (v) => base - v / gMax * H2;
  svg("line", { class: "grid", x1: P.l, x2: W - P.r, y1: gy(gMax), y2: gy(gMax) }, root);
  svgText({ class: "axis", x: P.l - 6, y: gy(gMax) + 4, "text-anchor": "end" }, "+" + fmtTok(gMax), root);
  svg("line", { class: "base", x1: P.l, x2: W - P.r, y1: base, y2: base }, root);
  for (const c of grows) {
    const X = x(tOf(s0, c.at));
    svg("rect", { class: "grow " + s0.cls, x: X - 1, y: gy(c.grow), width: 2, height: Math.max(1, base - gy(c.grow)) }, root);
  }
  // The two largest, labelled; the second a line lower when they'd meet.
  const largest = grows.slice().sort((a, b) => b.grow - a.grow).slice(0, 2);
  const labelX = [];
  largest.forEach((c, i) => {
    const X = x(tOf(s0, c.at)), right = X > W - 200;
    const near = i > 0 && Math.abs(X - labelX[0]) < 220;
    labelX.push(X);
    svgText({ class: "glabel", x: right ? X - 4 : X + 4, y: P.t + H1 + GAP + 2 + (near ? 13 : 0), "text-anchor": right ? "end" : "start" },
      "+" + fmtTok(c.grow) + (c.cause ? " " + fit(c.cause, 28) : ""), root);
  });
  for (const e of s0.events) {
    const glyph = e.kind === "refused" ? "✕" : e.kind === "ready" ? "▲" : "";
    if (!glyph || tOf(s0, e.at) < from || tOf(s0, e.at) > to) continue;
    const m = svgText({ class: "tick " + e.kind, x: x(tOf(s0, e.at)), y: base + 12, "text-anchor": "middle" }, glyph, root);
    hover(m, () => md(e.at) + " " + hm(e.at) + "  " + (e.kind === "refused" ? "land refused" : e.kind) + (e.text ? "\n" + e.text : ""));
  }
  // The tip: the call nearest the pointer, of each agent.
  crosshair(root, P, W, base + P.b, (px) => {
    const t = from + (px - P.l) / (W - P.l - P.r) * (to - from);
    const parts = [];
    let cx = null;
    for (const s of ser) {
      let best = null;
      for (const c of s.calls) if (!best || Math.abs(tOf(s, c.at) - t) < Math.abs(tOf(s, best.at) - t)) best = c;
      if (!best) continue;
      if (cx === null) cx = x(tOf(s, best.at));
      parts.push((mins ? agentName(s.a.agent) + "  " + (tOf(s, best.at) / 60).toFixed(1) + "m  " : md(best.at) + " " + hm(best.at) + "  ") +
        "ctx " + fmtTok(best.ctx) + (best.grow !== null ? "  " + (best.grow >= 0 ? "+" : "") + fmtTok(best.grow) : "") +
        "  out " + fmtTok(best.out) + (best.cause ? "\n" + best.cause : ""));
    }
    return cx === null ? null : { x: cx, text: parts.join("\n") };
  });
  const items = mins ? ser.map((s) => [s.cls, agentName(s.a.agent) + " · " + fmtTok(s.a.read) + (s.a.size ? " · " + s.a.size : "")]) : [];
  box.replaceChildren(...(items.length ? [legendOf(items)] : []), root,
    el("div", "note", "Lower: what each call added to " + agentName(d.agent.agent) + "'s context, named by the tool use before it" +
      (s0.events.some((e) => e.kind === "refused" || e.kind === "ready") ? " · ✕ land refused · ▲ ready" : "") + "."));
  return points;
}

// drawCtxPanel draws the overview's context panel for ctxAgent().
function drawCtxPanel() {
  const box = $("ctx-map");
  if (!box || pages.view !== "map") return;
  const agent = ctxAgent();
  const a = usageNow() && tok.data.agents.find((x) => x.agent === agent);
  $("ctx-sub").textContent = a ? agentName(agent) + " · " + (a.role || "") + (a.size ? " " + a.size : "") + " · peak " + fmtTok(a.peak) +
    " · pick another in Agents" : "the main thread's context after each model call";
  const d = tok.ctx && tok.ctx.agent === agent ? tok.ctx.data : null;
  const n = !usageNow() ? 0 : ctxChart(box, d, { from: tok.data.from, to: tok.data.to, empty: agent ? "loading…" : "No model calls in this window." });
  tally(box, "ctx", n);
}

// drawQuota draws each provider's 5h (dashed) and week (solid) percent over
// the window on one percent axis, each reset a faint line.
function drawQuota(box) {
  if (!box) return;
  const d = usageNow();
  if (!d) return box.replaceChildren(el("p", "empty", "loading…"));
  const by = new Map();
  for (const r of d.quota) {
    const at = Date.parse(r.read_at) / 1000;
    if (isNaN(at)) continue;
    if (!by.has(r.provider)) by.set(r.provider, []);
    by.get(r.provider).push(Object.assign({ at }, r));
  }
  const provs = [...by.keys()].sort((a, b) => slot(a) - slot(b) || (a < b ? -1 : 1));
  tally(box, "quota", d.quota.length);
  const leg = legendOf(provs.flatMap((p) => [["q" + slot(p) + " wk", p + " week"],
    ...(by.get(p).some((r) => r.five_hour != null) ? [["q" + slot(p) + " fh", p + " 5h"]] : [])]));
  if (!provs.length) return box.replaceChildren(el("p", "empty", "No quota readings in this window."));
  const W = Math.max(box.clientWidth, 240), H = 180, P = { l: 40, r: 8, t: 8, b: 20 };
  const from = d.from, to = d.to;
  const x = (t) => P.l + (Math.max(from, Math.min(to, t)) - from) / Math.max(1, to - from) * (W - P.l - P.r);
  const y = (v) => H - P.b - Math.max(0, Math.min(100, v)) / 100 * (H - P.t - P.b);
  const root = chartRoot(W, H, "quota used");
  yAxis(root, y, 100, P, W, (v) => Math.round(v) + "%");
  timeAxis(root, x, from, to, P.t, H - P.b, H);
  const resets = new Set();
  for (const rs of by.values()) for (const r of rs) for (const s of [r.five_reset, r.week_reset]) {
    const t = Date.parse(s || "") / 1000;
    if (t >= from && t <= to) resets.add(t);
  }
  for (const t of resets) svg("line", { class: "reset", x1: x(t), x2: x(t), y1: P.t, y2: H - P.b }, root);
  for (const p of provs) {
    for (const [key, cls] of [["five_hour", "fh"], ["week", "wk"]]) {
      // A gap of over 15 minutes between readings breaks the line, and so
      // does a reading with no 5-hour window.
      let path = "", last = -Infinity;
      for (const r of by.get(p)) {
        if (r[key] == null) { last = -Infinity; continue; }
        path += (r.at - last > 900 ? "M" : "L") + x(r.at).toFixed(1) + "," + y(+r[key] || 0).toFixed(1);
        last = r.at;
      }
      svg("path", { class: "qline q" + slot(p) + " " + cls, d: path }, root);
    }
  }
  crosshair(root, P, W, H, (px) => {
    const t = from + (px - P.l) / (W - P.l - P.r) * (to - from);
    let cx = null;
    const lines = [];
    for (const p of provs) {
      let best = null;
      for (const r of by.get(p)) if (!best || Math.abs(r.at - t) < Math.abs(best.at - t)) best = r;
      if (!best) continue;
      if (cx === null) cx = x(best.at);
      lines.push(p + "  5h " + fiveH(best) + " · wk " + pct(best.week) + "  " + hm(best.at));
    }
    return cx === null ? null : { x: cx, text: lines.join("\n") };
  });
  box.replaceChildren(leg, root);
}

function slot(provider) {
  const i = PROVIDERS.indexOf(provider);
  return i < 0 ? PROVIDERS.length + 1 : i + 1;
}

// --- outliers and burndowns (108) -------------------------------------------

// SIZE_SLOT is a spec size's categorical slot on the outliers chart; XS,
// fixers and the unsized are muted (slot 0).
const SIZE_SLOT = { S: 1, M: 2, L: 3 };

// minutes renders a wall time in minutes: "45m", "2h", "2.5h".
function minutes(m) {
  return m >= 120 ? +(m / 60).toFixed(1) + "h" : Math.round(m) + "m";
}

// drawOutliers draws every lead and fixer with use in 30 days (/api/usage's
// outliers, whatever the window): x its wall minutes, y its read, coloured
// by size; a flagged point ringed in its level's colour and labelled. A
// point's hover gives its numbers, its click opens its lead page compared
// with its vs.
function drawOutliers(box) {
  if (!box) return;
  const pts = tok.data ? tok.data.outliers || [] : null;
  if (!pts) return box.replaceChildren(el("p", "empty", "loading…"));
  tally(box, "outliers", pts.length);
  const leg = legendOf([["o1", "S"], ["o2", "M"], ["o3", "L"], ["o0", "XS & fixers"], ["ring fl-runaway", "runaway"], ["ring fl-overworking", "overworking"]]);
  if (!pts.length) return box.replaceChildren(leg, el("p", "empty", "No leads or fixers with model calls in 30 days."));
  const W = Math.max(box.clientWidth, 240), H = 240, P = { l: 46, r: 12, t: 12, b: 20 };
  const xMax = niceMax(Math.max(...pts.map((p) => (+p.wall || 0) / 60))), yMax = niceMax(Math.max(...pts.map((p) => +p.read || 0)));
  const x = (m) => P.l + m / xMax * (W - P.l - P.r), y = (v) => H - P.b - v / yMax * (H - P.t - P.b);
  const root = chartRoot(W, H, "outliers: read against wall time");
  yAxis(root, y, yMax, P, W, fmtTok);
  for (let i = 0; i <= 4; i++) {
    const X = x(xMax * i / 4);
    svg("line", { class: "grid", x1: X, x2: X, y1: P.t, y2: H - P.b }, root);
    svgText({ class: "axis", x: X, y: H - 6, "text-anchor": i === 4 ? "end" : "middle" }, minutes(xMax * i / 4), root);
  }
  // The flagged on top.
  const order = pts.slice().sort((a, b) => (LEVEL_RANK[a.level] || 0) - (LEVEL_RANK[b.level] || 0));
  for (const p of order) {
    const X = x((+p.wall || 0) / 60), Y = y(+p.read || 0);
    const k = p.role === "lead" && SIZE_SLOT[p.size] || 0;
    const g = svg("g", { class: "opt", tabindex: 0, role: "link", "aria-label": p.agent }, root);
    svg("circle", { class: "dot o" + k, cx: X, cy: Y, r: 4 }, g);
    if (p.level) {
      svg("circle", { class: "ring fl-" + p.level, cx: X, cy: Y, r: 7.5 }, g);
      const short = p.repo && p.repo.length >= 5 && p.agent.startsWith(p.repo + "-") ? abbrev(p.repo) + p.agent.slice(p.repo.length) : p.agent;
      const right = X > W - 110;
      svgText({ class: "olabel fl-" + p.level, x: right ? X - 7 : X + 7, y: Math.max(P.t + 8, Y - 9), "text-anchor": right ? "end" : "start" }, short, g);
    }
    svg("circle", { class: "hit", cx: X, cy: Y, r: 11 }, g);
    hover(g, () => p.agent + (p.live ? " ● live" : "") + "\n" + (p.role === "fixer" ? "fixer" : "size " + (p.size || "–")) + " · " + p.repo +
      "\nread " + fmtTok(p.read) + " · wall " + dur((+p.wall || 0) * 1000) + "\npeak ctx " + fmtTok(p.peak) + " · " + p.turns + " turns" +
      (p.flags && p.flags.length ? "\n▲ " + p.level + ": " + flagWords(p.flags) : "") +
      (p.vs && p.vs !== p.agent ? "\nclick: its page, compared with " + p.vs : ""));
    const go = () => { location.hash = "#/lead/" + p.agent + (p.vs && p.vs !== p.agent ? "?vs=" + encodeURIComponent(p.vs) : ""); };
    g.addEventListener("click", go);
    g.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") { e.preventDefault(); go(); }
    });
  }
  box.replaceChildren(leg, root);
}

// specs is the last /api/specs.
const specs = { data: null };

const specsLoad = loader(async () => {
  if (pages.view !== "map" && pages.view !== "specs") return;
  const r = await fetchJSON("/api/specs");
  if (!r.body) return;
  r.body.repos = r.body.repos || [];
  r.body.families = r.body.families || [];
  specs.data = r.body;
  drawAll();
});

// countAxis is a count axis's top: a multiple of four (whole quarters), at
// least four.
function countAxis(v) {
  return Math.max(4, Math.ceil(v / 4) * 4);
}

// repoBurn draws a repo's 30 days into box: a bar per day for the specs
// landed that day, a line for those open at the day's end. Returns the
// marks drawn.
function repoBurn(box, r) {
  const days = r.days || [];
  const leg = legendOf([["bd-l", "landed that day"], ["bd-o", "open at day's end"]]);
  if (!days.length) {
    box.replaceChildren(leg, el("p", "empty", "No spec history read for " + r.repo + "."));
    return 0;
  }
  const W = Math.max(box.clientWidth, 240), H = 190, P = { l: 30, r: 8, t: 8, b: 20 };
  const n = days.length, bw = (W - P.l - P.r) / n;
  const yMax = countAxis(Math.max(...days.map((d) => Math.max(d.landed.length, d.open))));
  const y = (v) => H - P.b - v / yMax * (H - P.t - P.b), cx = (i) => P.l + (i + 0.5) * bw;
  const root = chartRoot(W, H, r.repo + ": specs per day");
  yAxis(root, y, yMax, P, W, (v) => String(Math.round(v)));
  const every = Math.ceil(52 / bw);
  for (let i = n - 1; i >= 0; i -= every) svgText({ class: "axis", x: i === n - 1 ? W - P.r : cx(i), y: H - 6, "text-anchor": i === n - 1 ? "end" : "middle" }, days[i].day.slice(5), root);
  let marks = 0;
  days.forEach((d, i) => {
    if (!d.landed.length) return;
    svg("rect", { class: "bar bd-l", x: cx(i) - bw * 0.35, y: y(d.landed.length), width: Math.max(1, bw * 0.7), height: y(0) - y(d.landed.length), rx: 1.5 }, root);
    marks++;
  });
  svg("path", { class: "openline bd-o", d: "M" + days.map((d, i) => cx(i).toFixed(1) + "," + y(d.open).toFixed(1)).join("L") }, root);
  marks++;
  if (bw >= 8) for (let i = 0; i < n; i++) svg("circle", { class: "opendot bd-o", cx: cx(i), cy: y(days[i].open), r: 2 }, root);
  crosshair(root, P, W, H, (px) => {
    const i = Math.max(0, Math.min(n - 1, Math.floor((px - P.l) / bw)));
    const d = days[i];
    return { x: cx(i), text: d.day + "\nlanded " + d.landed.length + (d.landed.length ? ": " + d.landed.join(", ") : "") + "\nopen at day's end " + d.open };
  });
  box.replaceChildren(leg, root);
  return marks;
}

// familyBurn draws a build family's burndown into box: a step line of its
// specs still open from its first lead's start, a step down at each lead's
// done, its land refusals as ✕ on the line, a dashed now while live.
// Returns the marks drawn.
function familyBurn(box, f) {
  const steps = f.steps || [];
  if (!steps.length) {
    box.replaceChildren(el("p", "empty", "No leads in " + f.family + "."));
    return 0;
  }
  const now = Math.max(hubNow(), steps[steps.length - 1][0]);
  const end = f.live ? now : steps[steps.length - 1][0];
  const from = f.start, to = Math.max(end, from + 600) + (Math.max(end, from + 600) - from) * 0.02;
  const W = Math.max(box.clientWidth, 240), H = 190, P = { l: 30, r: 8, t: 8, b: 20 };
  const yMax = countAxis(steps[0][1]);
  const x = (t) => P.l + (Math.max(from, Math.min(to, t)) - from) / Math.max(1, to - from) * (W - P.l - P.r);
  const y = (v) => H - P.b - v / yMax * (H - P.t - P.b);
  const root = chartRoot(W, H, f.family + ": specs open");
  yAxis(root, y, yMax, P, W, (v) => String(Math.round(v)));
  timeAxis(root, x, from, to, P.t, H - P.b, H);
  const openAt = (t) => {
    let v = steps[0][1];
    for (const [at, o] of steps) if (at <= t) v = o;
    return v;
  };
  let d = "M" + x(steps[0][0]).toFixed(1) + "," + y(steps[0][1]).toFixed(1);
  for (const [at, o] of steps.slice(1)) d += "H" + x(at).toFixed(1) + "V" + y(o).toFixed(1);
  d += "H" + x(end).toFixed(1);
  svg("path", { class: "stepline", d }, root);
  let marks = 1;
  if (f.live) svg("line", { class: "now", x1: x(now), x2: x(now), y1: P.t, y2: H - P.b }, root);
  const done = (f.leads || []).filter((l) => l.done).sort((a, b) => a.done - b.done);
  crosshair(root, P, W, H, (px) => {
    const t = Math.min(end, from + (px - P.l) / (W - P.l - P.r) * (to - from));
    const by = done.filter((l) => l.done <= t);
    return { x: x(t), text: md(t) + " " + hm(t) + "  " + openAt(t) + " open" + (by.length ? "\nlast done: " + by[by.length - 1].path : "") };
  });
  // Over the crosshair's hit area, so each ✕ has its own tip.
  for (const [at, path, text] of f.refused || []) {
    const m = svgText({ class: "tick refused", x: x(at), y: y(openAt(at)) + 4, "text-anchor": "middle" }, "✕", root);
    hover(m, () => md(at) + " " + hm(at) + "  " + path + "  land refused" + (text ? "\n" + text : ""));
    marks++;
  }
  box.replaceChildren(root);
  return marks;
}

// familyNote is a family's subtitle: its repo, leads, start and state.
function familyNote(f) {
  const leads = (f.leads || []).length;
  return f.family + " · " + f.repo + " · " + leads + (leads === 1 ? " lead" : " leads") + " from " + md(f.start) + " " + hm(f.start) + "Z · " +
    (f.live ? "live" : "finished") + " · step down: a lead done · ✕ land refused" + (f.live ? " · dashed: now" : "");
}

// drawBurnRow draws the overview's row 5: the newest family's burndown and
// the busiest repo's 30 days (most landed).
function drawBurnRow() {
  const fam = $("fam-map"), rep = $("repo-map");
  const d = specs.data;
  if (!d) {
    for (const b of [fam, rep]) b.replaceChildren(el("p", "empty", "loading…"));
    return;
  }
  let marks = 0;
  const f = d.families[0];
  $("fam-sub").textContent = f ? familyNote(f) : "the newest build family's specs still open";
  if (f) marks += familyBurn(fam, f);
  else fam.replaceChildren(el("p", "empty", "No build families started in 30 days."));
  const landed = (r) => (r.days || []).reduce((s, x) => s + x.landed.length, 0);
  const busy = d.repos.reduce((a, b) => !a || landed(b) > landed(a) ? b : a, null);
  $("repo-sub").textContent = busy ? busy.repo + " · " + landed(busy) + " landed in 30 days · open: proposed or accepted, not landed" : "no repo configured";
  if (busy) marks += repoBurn(rep, busy);
  else rep.replaceChildren(el("p", "empty", "No repos configured."));
  tally(fam, "burndown", marks);
}

// drawSpecs draws the specs tab: every repo's 30 days, then every family
// started in 30 days, newest first.
function drawSpecs() {
  const repos = $("specs-repos"), fams = $("specs-families");
  const d = specs.data;
  if (!d) return repos.replaceChildren(el("p", "empty", "loading…"));
  const panel = (title, note) => {
    const box = el("div", "chart");
    return { box, p: el("section", "tpanel", null, el("h2", null, title, el("small", null, note)), box) };
  };
  const rs = d.repos.map((r) => Object.assign(panel(r.repo, "specs landed per day (bars) and open at each day's end (line), 30 days"), { r }));
  const fs = d.families.map((f) => Object.assign(panel("Burndown · " + f.family, familyNote(f)), { f }));
  repos.replaceChildren(...rs.map((x) => x.p));
  if (!rs.length) repos.replaceChildren(el("p", "empty", "No repos configured."));
  fams.replaceChildren(...fs.map((x) => x.p));
  if (!fs.length) fams.replaceChildren(el("p", "empty", "No build families started in 30 days."));
  let marks = 0;
  for (const x of rs) marks += repoBurn(x.box, x.r);
  for (const x of fs) marks += familyBurn(x.box, x.f);
  tally(repos, "burndown", marks);
}

// tokensCard is the lead page's tokens card: the four kinds, main and
// sub, and each subagent with its description.
function tokensCard(d) {
  if (!d || !d.agent) return bare("tokens", "no model calls on the mesh");
  const a = d.agent;
  const head = el("tr", null, null, ...["", "input", "cache read", "cache write", "out", "read"].map((h, i) => el("th", i ? "r" : "", h)));
  const row = (name, k) => el("tr", null, null, el("td", null, name), el("td", "r", fmtTok(k.in)), el("td", "r", fmtTok(k.cr)),
    el("td", "r", fmtTok(k.cw)), el("td", "r", fmtTok(k.out)), el("td", "r", fmtTok(kindsRead(k))));
  const kinds = el("div", "tw", null, el("table", "ag", null, el("thead", null, null, head),
    el("tbody", null, null, row("main", a.main || {}), row("sub", a.sub || {}))));
  const subs = (d.subs || []).map((s) => el("li", null, null, el("span", "x", s.desc || s.id),
    el("span", "t", fmtTok(s.read) + " read · " + fmtTok(s.out) + " out · " + s.calls + " calls")));
  return card("tokens", el("p", "muted", fmtTok(a.read) + " read · peak " + fmtTok(a.peak) + " · " + a.calls + " calls" +
    (a.compactions ? " · " + a.compactions + " compacted" : "")), kinds,
  ...(subs.length ? [el("h4", null, "subagents"), el("ol", "rows", null, ...subs)] : []));
}

// comparePeers fills the lead page's compare select with the done leads of
// its spec size that read in the last week.
async function comparePeers(path) {
  const sel = $("lead-vs");
  const me = tok.lead && tok.lead.data && tok.lead.data.agent;
  if (!tok.peers || Date.now() - tok.peers.at > PEERS_MS) {
    const r = await fetchJSON("/api/usage?since=7d").catch(() => ({ body: null }));
    if (r.body) tok.peers = { at: Date.now(), agents: r.body.agents || [] };
  }
  if (pages.lead !== path) return;
  const size = me ? me.size : "";
  const peers = (tok.peers ? tok.peers.agents : []).filter((a) => size && a.size === size && a.role === "lead" && a.state === "done" && a.agent !== path);
  const opts = [el("option", null, peers.length ? "compare with a done " + size + " lead…" : "no done " + (size || "sized") + " lead to compare")];
  opts[0].value = "";
  const vs = tok.lead && tok.lead.vs || "";
  // A vs from the hash may be older than the week the list reads.
  const shown = vs && !peers.some((a) => a.agent === vs) ? [{ agent: vs, read: tok.lead.data && tok.lead.data.vs ? tok.lead.data.vs.agent.read : 0 }] : [];
  for (const a of peers.concat(shown)) {
    const o = el("option", null, a.agent + " · " + fmtTok(a.read));
    o.value = a.agent;
    opts.push(o);
  }
  sel.replaceChildren(...opts);
  sel.value = vs;
  sel.disabled = opts.length < 2;
}

// --- the session page

const sessionPage = loader(async () => {
  const agent = pages.session;
  if (!agent) return;
  const r = await fetchJSON("/api/usage/agent?agent=" + encodeURIComponent(agent));
  if (pages.session !== agent) return;
  tok.session = { agent, data: r.body, status: r.status };
  drawSession();
});

// drawSession draws a coordinator session's page: its head, busy lane,
// context chart and subagents.
function drawSession() {
  const s = tok.session;
  if (!s || s.agent !== pages.session) return;
  const d = s.data, head = $("session-head");
  if (!d || !d.agent) {
    head.replaceChildren(el("p", "empty", s.status === 404 ? "no model calls of " + s.agent + " in the last 30 days" : "could not load " + s.agent));
    for (const id of ["session-lane", "session-ctx", "session-subs"]) $(id).replaceChildren();
    return;
  }
  const a = d.agent, design = +a.design || 0, exec = +a.exec || 0;
  head.replaceChildren(
    el("span", "mono", a.repo || "no repo"), el("span", "mono muted", a.host || ""),
    el("span", "mono muted", md(a.first) + " " + hm(a.first) + "Z – " + md(a.last) + " " + hm(a.last) + "Z"),
    el("span", "mono", fmtTok(a.read) + " read · " + fmtTok(a.out) + " out"),
    phaseBar(design, exec, Math.max(1, design + exec)),
    el("span", "mono", "design " + fmtTok(design) + " · exec " + fmtTok(exec) + " · " + share(design, exec) + " design"));
  drawSpans($("session-lane"), d.spans || [], a.first, Math.max(a.last, a.first + 600), a.agent);
  ctxChart($("session-ctx"), d, {});
  const subs = d.subs || [];
  const rows = subs.map((x) => el("tr", null, null, el("td", "desc", x.desc || x.id),
    el("td", null, null, el("i", "sw r-" + (x.phase === "exec" ? "exec" : "design")), x.phase || "design"),
    el("td", "r", fmtTok(x.read)), el("td", "r", fmtTok(x.out)), el("td", "r hide-n", String(x.calls))));
  $("session-subs").replaceChildren(subs.length ? el("div", "tw", null, el("table", "ag", null,
    el("thead", null, null, el("tr", null, null, el("th", null, "description"), el("th", null, "phase"), el("th", "r", "read"),
      el("th", "r", "out"), el("th", "r hide-n", "calls"))), el("tbody", null, null, ...rows))) : el("p", "empty", "No subagents."));
}

// drawSpans draws one session's busy spans over [from, to].
function drawSpans(box, spans, from, to, agent) {
  const W = Math.max(box.clientWidth, 240), H = 44, P = { l: 8, r: 8 };
  const x = (t) => P.l + (Math.max(from, Math.min(to, t)) - from) / Math.max(1, to - from) * (W - P.l - P.r);
  const root = chartRoot(W, H, "busy spans");
  timeAxis(root, x, from, to, 4, 24, H);
  for (const [a, b, ph] of spans) {
    const r = svg("rect", { class: "seg ph-" + (ph === "exec" ? "exec" : "design"), x: x(a), y: 6, width: Math.max(2, x(b) - x(a)), height: LANE.bar, rx: 3 }, root);
    hover(r, () => agentName(agent) + "\n" + (ph || "design") + "  " + spanText(a, b));
  }
  box.replaceChildren(legendOf([["r-design", "design"], ["r-exec", "exec"]]), root);
}

// --- events, routes, start ------------------------------------------------

function parseFrame(data) {
  try {
    const f = JSON.parse(data);
    return f && typeof f.subject === "string" ? f : null;
  } catch (e) {
    return null;
  }
}

let connState = "connecting";

function setConn(s) {
  connState = s;
  showConn();
  if (check) {
    if (check.conn[check.conn.length - 1] !== s) check.conn.push(s);
    checkSoon();
  }
}

// showConn writes the connection state: "● live · <host> hub" once live.
function showConn() {
  const c = $("conn");
  c.textContent = connState === "live" ? "● live" + (mesh.host ? " · " + mesh.host + " hub" : "") : connState;
  c.classList.toggle("down", connState === "reconnecting");
  c.classList.toggle("up", connState === "live");
}

// showClock writes the hub's clock in UTC.
function showClock() {
  const d = new Date(hubNow() * 1000).toISOString();
  $("clock").textContent = d.slice(5, 10) + " " + d.slice(11, 16) + "Z";
}

const RETRY_MS = [1000, 2000, 5000, 10000];
let retries = 0;

// connect follows /events. The browser retries a dropped stream itself; a
// stream it gave up on (an error status while the daemon starts) is opened
// again here with backoff.
function connect() {
  const es = new EventSource("/events");
  es.onopen = () => {
    // Every connection replays the last day, and live frames were missed
    // while it was down.
    retries = 0;
    mesh.recent = [];
    drawRecent();
    setConn("live");
    loadTree();
    timeline.run();
    usage.run();
    specsLoad.run();
    if (pages.view === "lead") lead.run();
    if (pages.view === "routines") routines.run();
  };
  es.onerror = () => {
    setConn("reconnecting");
    if (es.readyState !== EventSource.CLOSED) return;
    setTimeout(connect, RETRY_MS[Math.min(retries++, RETRY_MS.length - 1)]);
  };
  // Replayed frames only fill the drawer: they already happened.
  es.addEventListener("replay", (e) => {
    const f = parseFrame(e.data);
    if (!f) return;
    if (check) {
      check.replay++;
      checkSoon();
    }
    remember(f);
  });
  es.addEventListener("live", (e) => {
    const f = parseFrame(e.data);
    if (!f) return;
    if (check) {
      check.live++;
      checkSoon();
    }
    remember(f);
    apply(f);
    pagesApply(f);
  });
}

let checkTimer = 0;

function checkSoon() {
  if (!checkTimer) checkTimer = setTimeout(() => { checkTimer = 0; selfCheck(); }, 100);
}

// selfCheck writes, under ?check=1, whether the page scrolls sideways, the
// smallest tap target, the tree reads, the timeline drawn (lanes, segments
// per state, ticks), the KPI tiles, the usage panels' counts, the band's
// flag rows (runaway ones too), the outliers' points, the burndowns' marks,
// the band's rows (held and landing ones too), the older fold, the digest
// and the connection history into document.title.
function selfCheck() {
  const sw = document.documentElement.scrollWidth, iw = window.innerWidth;
  let tap = Infinity;
  for (const e of document.querySelectorAll("nav a, #windows button, .drawer summary, header h1 a, .tap")) {
    const r = e.getBoundingClientRect();
    if (r.width || r.height) tap = Math.min(tap, r.height, r.width);
  }
  const c = check;
  const segs = Object.keys(c.segs).sort().map((s) => s + ":" + c.segs[s]).join(",") || "none";
  const n = (sel) => document.querySelectorAll(sel).length;
  document.title = `bmesh ${sw <= iw ? "ok" : "SCROLLS"} view=${pages.view} scrollWidth=${sw} innerWidth=${iw} minTap=${Math.round(tap)}` +
    (pages.view === "lead" ? ` firstCard=${firstCard()}` : "") +
    ` window=${tl.win} trees=${c.trees} lanes=${c.lanes} segs=${segs} ticks=${c.ticks} kpis=${n("#kpis .tile")}` +
    ` roles=${c.roles} agents=${c.agents} ctx=${c.ctx} quota=${c.quota}` +
    ` flags=${n("#needs .need.flag")} flagsRunaway=${n("#needs .need.flag.fl-runaway")} outliers=${c.outliers} burndown=${c.burndown}` +
    ` conn=${c.conn.join(">")} replay=${c.replay} live=${c.live}` +
    ` band=${$("needs").hidden ? "hidden" : $("needs").children.length}` +
    ` bandHeld=${n("#needs .need.st-held")} bandLanding=${n("#needs .need.st-landing")}` +
    ` older=${n("#needs details.older .need")} digest=${$("strip").querySelector(".digest") ? 1 : 0}`;
}

// firstCard names the lead page's card drawn highest (leftmost on a tie),
// spaces as "_": CSS order, not the DOM, decides it in one column.
function firstCard() {
  let best = null, top = Infinity, left = Infinity;
  for (const e of document.querySelectorAll("#lead-panel .card")) {
    const r = e.getBoundingClientRect();
    if (!r.height) continue;
    if (r.top < top - 0.5 || Math.abs(r.top - top) <= 0.5 && r.left < left) {
      best = e;
      top = r.top;
      left = r.left;
    }
  }
  return best ? String(best.dataset.card).replace(/\s+/g, "_") : "none";
}

// route shows the view the hash names: #/ the overview, #/timeline the
// timeline, either with ?w=<window>; #/lead/<path> a lead (with ?vs=<path>
// compared), #/specs the burndowns, #/routines the routines.
function route() {
  const [h, q] = location.hash.replace(/^#\/?/, "").split("?");
  const w = new URLSearchParams(q || "").get("w");
  if (WINDOWS[w] && w !== tl.win) {
    tl.win = w;
    tl.data = null;
    try { localStorage.setItem(WINDOW_KEY, w); } catch (e) { /* the hash carries it */ }
  }
  let view = "map";
  let path = "";
  if (h.startsWith("lead/")) {
    view = "lead";
    path = h.slice(5);
    try { path = decodeURIComponent(path); } catch (e) { /* shown as written */ }
    // ?vs=<path> opens the context chart compared (the outliers' click).
    const vs = new URLSearchParams(q || "").get("vs");
    if (vs !== null && !(tok.lead && tok.lead.path === path && tok.lead.vs === vs)) {
      tok.lead = { path, vs, data: tok.lead && tok.lead.path === path ? tok.lead.data : null };
    }
    if (path !== pages.leadShown) {
      $("lead-title").textContent = path;
      $("lead-head").replaceChildren();
      $("lead-lanes").replaceChildren();
      $("lead-panel").replaceChildren();
      pages.leadShown = "";
    }
  } else if (h.startsWith("session/")) {
    view = "session";
    const [host, id] = h.slice(8).split("/").map((x) => { try { return decodeURIComponent(x || ""); } catch (e) { return x || ""; } });
    pages.session = "session." + host + "." + id;
    $("session-title").textContent = "session " + id.slice(0, 8) + " on " + host;
    if (!tok.session || tok.session.agent !== pages.session) {
      for (const b of ["session-head", "session-lane", "session-ctx", "session-subs"]) $(b).replaceChildren();
    }
  } else if (h === "routines" || h === "timeline" || h === "tokens" || h === "specs") {
    view = h;
  }
  pages.view = view;
  pages.lead = path;
  if (view !== "session") pages.session = "";
  for (const v of ["map", "timeline", "tokens", "specs", "lead", "session", "routines"]) $("view-" + v).hidden = v !== view;
  $("windows").hidden = view === "lead" || view === "session" || view === "routines" || view === "specs";
  showWindow();
  if (view === "lead") lead.run();
  if (view === "session") sessionPage.run();
  if (view === "routines") routines.run();
  if (view === "specs") drawAll();
  if (view === "map" || view === "specs") specsLoad.run();
  if (view === "map" || view === "timeline" || view === "tokens") {
    drawAll(); // the width may have changed while hidden
    timeline.run();
    usage.run();
  }
  for (const a of document.querySelectorAll("nav a[data-route]")) a.classList.toggle("on", a.dataset.route === view);
}

const phone = window.matchMedia(PHONE);
const narrowQ = window.matchMedia(NARROW);
phone.addEventListener("change", drawRecent); // the lane count follows the width
// Timelines fit the width: redrawn when it changes (not the height: a
// phone's toolbar hiding on scroll resizes it).
let resizeTimer = 0, lastWidth = window.innerWidth;
window.addEventListener("resize", () => {
  if (window.innerWidth === lastWidth) return;
  lastWidth = window.innerWidth;
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(drawAll, 100);
});

for (const id of ["needs", "recent", "lead-panel"]) $(id).addEventListener("click", toggleRows);
$("lead-vs").addEventListener("change", (ev) => {
  if (!tok.lead || tok.lead.path !== pages.lead) return;
  tok.lead.vs = ev.target.value;
  lead.run();
});
$("recent").addEventListener("click", pinChip);
$("recent").addEventListener("pointermove", (ev) => { if (ev.pointerType === "mouse") setHover(hoverPath(ev)); });
$("recent").addEventListener("pointerleave", () => setHover(""));
$("drawer").addEventListener("toggle", measureTime);
$("windows").addEventListener("click", (ev) => {
  const b = ev.target.closest("button[data-w]");
  if (b) setWindow(b.dataset.w);
});
document.addEventListener("keydown", (ev) => { if (ev.key === "Escape" && recentFocus.pin) unpin(); });
window.addEventListener("hashchange", route);
tl.win = storedWindow() || "6h";
route();
loadTree();
connect();
loadStrip();
setInterval(loadTree, POLL_MS);
setInterval(loadStrip, STRIP_MS);
setInterval(() => {
  if (document.hidden) return;
  timeline.run();
  usage.run();
  specsLoad.run();
}, 2 * POLL_MS);
setInterval(() => { if (pages.view === "routines" && !document.hidden) routines.run(); }, ROUTINES_MS);
setInterval(drawAll, NOW_MS);
setInterval(() => { tick(); showClock(); }, 1000);
showClock();
