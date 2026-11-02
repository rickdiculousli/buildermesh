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
  ? { conn: [], trees: 0, replay: 0, live: 0, lanes: 0, segs: {}, ticks: 0 } : null;

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
  const base = pages.view === "timeline" ? "#/timeline" : "#/";
  history.replaceState(null, "", base + "?w=" + w);
  showWindow();
  tl.data = null;
  drawAll();
  timeline.run();
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
    drawLanes($("tl-map"), tl.data, { fold: true, live: true });
    renderKpis();
  } else if (pages.view === "timeline") {
    drawLanes($("tl-full"), tl.data, { fold: false, live: true });
  } else if (pages.view === "lead" && tl.lead && tl.lead.path === pages.lead) {
    drawLanes($("lead-lanes"), tl.lead.data, { fold: false, live: true, life: true });
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
    const l = r.l, h = hOf(r), bar = r.depth ? LANE.kidBar : LANE.bar, by = y + (h - bar) / 2;
    const g = svg("g", { class: "tl-lane" + (r.depth ? " kid" : "") + " role-" + String(l.role || "").replace(/[^a-z-]/g, "") }, root);
    const name = svgText({ class: "label", x: 4 + r.depth * 10, y: by + bar - 2 }, fit(r.label, Math.floor((G - 14 - r.depth * 10) / 7) - (r.more ? 3 : 0)), g);
    if (r.more) name.append(svgTspan("more", " +" + r.more));
    hover(name, () => l.path + "\n" + (l.state || "idle") + (l.spec ? " · spec " + l.spec : "") + (l.repo ? " · " + l.repo : ""));
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
  e.addEventListener("pointermove", (ev) => {
    tip.textContent = text();
    tip.hidden = false;
    const w = tip.offsetWidth, h = tip.offsetHeight;
    tip.style.left = Math.max(4, Math.min(ev.clientX + 14, window.innerWidth - w - 4)) + "px";
    tip.style.top = (ev.clientY + 14 + h > window.innerHeight ? ev.clientY - h - 8 : ev.clientY + 14) + "px";
  });
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

const pages = { view: "map", lead: "", leadShown: "" };

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
// needRows entry, the stale and dismissed ones folded into olderFold at
// the foot, then the operatorRows. Hidden when nothing needs the operator.
function renderBand() {
  const band = $("needs");
  const open = band.querySelector("details.older[open]") !== null;
  const gone = dismissals(), live = [], older = [];
  for (const r of needRows()) (r.stale || isDismissed(r, gone) ? older : live).push(r);
  const rows = live.map((r) => needRow(r, true));
  if (older.length) rows.push(olderFold(older, open));
  band.replaceChildren(...rows, ...operatorRows());
  band.hidden = band.childElementCount === 0;
  if (check) checkSoon();
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
  panel.replaceChildren(el("div", "stack", null, ...main), el("div", "stack", null,
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
  const [r, t] = await Promise.all([get("/api/lead/" + encodeURIComponent(path)),
    get("/api/timeline?lead=" + encodeURIComponent(path))]);
  if (pages.lead !== path) return; // the route moved on
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

// renderKpis draws the six tiles from the tree (with live states), the
// window's timeline, the routines and the quota readings.
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
  box.replaceChildren(
    tile("Live agents", mesh.tree ? live.length : "–", list(live.map(short), 3)),
    tile("Landed · " + tl.win, landed.length + win, list(landed.map((l) => l.spec || short(l.path)), 6)),
    tile("Land refused · " + tl.win, refused + win, worst ? short(worst.path) + " ×" + worstN : ""),
    tile("Asking / blocked", mesh.tree ? waiting.length : "–", list(waiting.map(short), 3)),
    quotaTile(),
    tile("Routines", queued, [queued === 1 ? "job queued · " : "jobs queued · ", running + " running"], "#/routines"));
  if (check) checkSoon();
}

// quotaTile is each provider's 5h and week, a meter for the higher week
// and that week's reset.
function quotaTile() {
  const q = strip.quota || [];
  if (!q.length) return tile("Quota", "–", "no reading");
  const top = q.reduce((a, b) => (+b.week || 0) > (+a.week || 0) ? b : a);
  const lines = el("div", "v small", null, ...q.map((r) => {
    const high = +r.five_hour >= 80 || +r.week >= 80;
    return el("div", high ? "high" : "", (high ? "▲ " : "") + (r.provider || "?") + " 5h " + pct(r.five_hour) + " · wk " + pct(r.week));
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
// per state, ticks), the KPI tiles, the band's rows (held and landing ones
// too), the older fold, the digest and the connection history into
// document.title.
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
// timeline, either with ?w=<window>; #/lead/<path> a lead, #/routines the
// routines.
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
    if (path !== pages.leadShown) {
      $("lead-title").textContent = path;
      $("lead-head").replaceChildren();
      $("lead-lanes").replaceChildren();
      $("lead-panel").replaceChildren();
      pages.leadShown = "";
    }
  } else if (h === "routines" || h === "timeline") {
    view = h;
  }
  pages.view = view;
  pages.lead = path;
  for (const v of ["map", "timeline", "lead", "routines"]) $("view-" + v).hidden = v !== view;
  $("windows").hidden = view === "lead" || view === "routines";
  showWindow();
  if (view === "lead") lead.run();
  if (view === "routines") routines.run();
  if (view === "map" || view === "timeline") {
    drawAll(); // the width may have changed while hidden
    timeline.run();
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
setInterval(() => { if (!document.hidden) timeline.run(); }, 2 * POLL_MS);
setInterval(() => { if (pages.view === "routines" && !document.hidden) routines.run(); }, ROUTINES_MS);
setInterval(drawAll, NOW_MS);
setInterval(() => { tick(); showClock(); }, 1000);
showClock();
