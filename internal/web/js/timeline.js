// timeline: the fleet timeline's data and drawLanes, which the overview,
// the timeline tab, the lead page and the session page draw with.
import { $, axisStep, el, fetchJSON, fit, hm, lastToken, loader, md, narrowQ, open, phone, readState, spanText, svg, svgText, svgTspan } from "./core.js";
import { WINDOWS, hubNow, mesh, tl } from "./state.js";
import { hover } from "./drawer.js";
import { agentHref, agentName, flagMap, fmtTok, usageNow } from "./tokens.js";
import { tallyCheck } from "./check.js";
import { drawAll } from "./main.js";

// Lane geometry: a lane's height and its bar's, for a top lane and for a
// child's thinner lane on the timeline tab and the lead page.
export const LANE = { h: 22, bar: 13, kidH: 16, kidBar: 9, top: 12, axis: 20, gutter: 170, narrowGutter: 118 };

const DRAW_MS = 1000; // live events redraw at most this often

// timeline reads the window's lanes.
export const timeline = loader((win) => fetchJSON("/api/timeline?since=" + win), {
  key: () => tl.win,
  show: (r) => {
    if (r.body) {
      tl.data = r.body;
      tl.data.lanes = tl.data.lanes || [];
      replayLive(tl.data);
    }
    drawAll();
  },
});

// liveLog holds the live events since the page opened, so a timeline read
// (cached on the hub for 30 s) gets the ones newer than it reapplied.
const liveLog = [];

const LIVE_LOG_MAX = 500;

// liveLane extends path's lane with a live event at (unix seconds): state
// ("" for none) closes the last segment and opens one, done ends the lane;
// kind ("" for none) adds a tick with its text.
export function liveLane(path, state, at, kind, text) {
  if (isNaN(at)) return;
  liveLog.push({ path, state, at, kind, text, job: "" });
  if (liveLog.length > LIVE_LOG_MAX) liveLog.shift();
  for (const d of [tl.data, tl.lead && tl.lead.data]) if (d) laneEvent(d, path, state, at, kind, text);
  drawSoon();
}

// liveJob extends a routine instance's lane: "job <id> started" opens a
// working segment, "job <id> ok|fail ..." closes it.
export function liveJob(path, ev, text, at) {
  if (ev !== "progress" || isNaN(at)) return;
  const state = /^job \S+ started$/.test(text) ? "working" : /^job \S+ (ok|fail) \d+/.test(text) ? "idle" : "";
  if (state) liveLane(path, state, at, "", "");
}

export function replayLive(d) {
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
    const t = mesh.nodes.get(path);
    if (t) Object.assign(l, { role: t.node.role, parent: t.node.parent || "", repo: t.node.repo || "", spec: t.node.spec || "" });
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

// abbrev shortens a repo name for lane labels: its first letter and the
// letter two thirds into its first word (buildermesh: bm).
export function abbrev(repo) {
  const w = repo.split(/[-_.]/)[0];
  return w.length < 3 ? w : w[0] + w[Math.floor(2 * w.length / 3)];
}

// shortener drops the repo's prefix from lane labels when every top lane is
// <repo>-... of one repo whose name has five letters or more, putting
// abbrev(repo) in its place.
export function shortener(tops) {
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

// drawLanes draws d's lanes into box over d's window moved to the hub's now
// (opts.live), or from d.from to now (opts.life): segments by state, idle
// dashed, ticks, grid lines, the now line, hover tips and links to the lead
// page; with opts.fold only the top lanes.
export function drawLanes(box, d, opts) {
  if (!box) return;
  if (!d) {
    box.replaceChildren(el("p", "empty", readState(timeline, "the timeline")));
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

// drawTimeline draws the timeline tab: every lane.
export function drawTimeline() {
  drawLanes($("tl-full"), tl.data, { fold: false, live: true, sessions: true });
}
