// tokens: the usage read and the token panels the overview and the tokens
// tab share.
import { $, ICON, chartRoot, dur, el, fetchJSON, fit, hm, legendOf, loader, md, niceMax, readState, svg, svgText, timeAxis, yAxis } from "./core.js";
import { pages, tl, tok } from "./state.js";
import { crosshair, hover } from "./drawer.js";
import { ctxAgent, ctxPanel, fiveH, pct, renderBand, select } from "./overview.js";
import { tally } from "./check.js";
import { gb } from "./hosts.js";
import { drawAll } from "./main.js";

// ROLES are the tokens-by-role series in their fixed order, each drawn in
// map.css's --r-<name>; design and exec are a coordinator's phases, never a
// state's colour.
const ROLES = [["lead", "leads"], ["sub", "subagents"], ["design", "design"], ["exec", "exec"], ["routine", "routines"]];

// PROVIDERS gives each quota provider its colour slot, by name, so a
// provider missing from the window never repaints another.
const PROVIDERS = ["claude", "codex"];

// fmtTok renders a token count: 416M, 42.2M, 347k, 19k, 845.
export function fmtTok(n) {
  n = +n || 0;
  const a = Math.abs(n);
  if (a >= 1e9) return (n / 1e9).toFixed(a >= 1e10 ? 0 : 1) + "G";
  if (a >= 1e6) return (n / 1e6).toFixed(a >= 1e8 ? 0 : 1) + "M";
  if (a >= 1e3) return (n / 1e3).toFixed(a >= 1e4 ? 0 : 1) + "k";
  return String(Math.round(n));
}

export function kindsRead(k) {
  return k ? (+k.in || 0) + (+k.cr || 0) + (+k.cw || 0) : 0;
}

// sessionOf splits session.<host>.<id> into {host, id}; null for a node.
function sessionOf(agent) {
  const m = /^session\.([^.]+)\.(.+)$/.exec(String(agent || ""));
  return m ? { host: m[1], id: m[2] } : null;
}

// agentName is how an agent reads in a row: "session <id8>" or its path.
export function agentName(agent) {
  const s = sessionOf(agent);
  return s ? "session " + s.id.slice(0, 8) : String(agent);
}

// agentHref is an agent's page: a session's, else its lead page.
export function agentHref(agent) {
  const s = sessionOf(agent);
  return s ? "#/session/" + encodeURIComponent(s.host) + "/" + encodeURIComponent(s.id) : "#/lead/" + agent;
}

// usageNow is the loaded /api/usage when it is the window shown.
export function usageNow() {
  const s = usage.state;
  return s.phase === "shown" && s.key === tl.win ? tok.data : null;
}

// usage reads the window's token use.
export const usage = loader((win) => fetchJSON("/api/usage?since=" + win), {
  key: () => tl.win,
  show: (r) => {
    if (r.body) {
      const d = r.body;
      for (const k of ["agents", "sessions", "quota"]) d[k] = d[k] || [];
      tok.data = d;
    }
    drawAll();
    if (!r.body) return;
    renderBand();
    if (pages.view === "map") ctxPanel.run(); // the overview's context panel reads its agent from this
  },
});

// worstLevel is the worst of an agent's flags (the API lists them worst
// first): "runaway", "overworking" or "".
export function worstLevel(flags) {
  return flags && flags.length ? flags[0].level : "";
}

// flagWords are the flags' words, the worst first.
export function flagWords(flags) {
  return (flags || []).map((f) => f.words).join("; ");
}

// flagMap maps each flagged agent of a usage read (its rows and its band)
// to {level, words}.
export function flagMap(d) {
  const m = new Map();
  for (const a of d.agents) if (worstLevel(a.flags)) m.set(a.agent, { level: worstLevel(a.flags), words: flagWords(a.flags) });
  for (const b of d.band || []) m.set(b.agent, { level: b.level, words: flagWords(b.flags) });
  return m;
}

// drawRoles draws the tokens-by-role chart: the five series stacked in
// their fixed order, a surface gap between fills, a crosshair tip giving
// each series and the bucket's total.
export function drawRoles(box) {
  if (!box) return;
  const d = usageNow();
  if (!d) return box.replaceChildren(el("p", "empty", readState(usage, "usage")));
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
  // cpu and peak mem are its processes' (117): mean cores, largest memory
  ["cpu", "cpu", (a) => a.cpu == null ? -1 : +a.cpu, true],
  ["mem", "peak mem", (a) => +a.peak_mem || 0, true],
  ["wall", "wall", (a) => (+a.last || 0) - (+a.first || 0), true],
  ["out", "out", (a) => +a.out || 0, true],
  // runaway first when sorted ascending, as a word column sorts on its first click
  ["watch", "watch", (a) => -(LEVEL_RANK[worstLevel(a.flags)] || 0), true],
];

export const LEVEL_RANK = { runaway: 2, overworking: 1 };

// drawAgents draws the agents table: with opts.limit its first rows and a
// link to the tokens tab, with opts.sortable a header that sorts it, with
// opts.select rows a click selects for the context panel.
export function drawAgents(box, opts) {
  if (!box) return;
  const d = usageNow();
  if (!d) return box.replaceChildren(el("p", "empty", readState(usage, "usage")));
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
      el("td", "r hide-n", a.cpu == null ? "–" : (+a.cpu).toFixed(1)),
      el("td", "r hide-n", a.peak_mem == null ? "–" : gb(a.peak_mem)),
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
export function phaseBar(design, exec, top) {
  const b = el("span", "ms ph", null, el("i", "d"), el("i", "e"));
  b.firstChild.style.width = (design / top * 100).toFixed(1) + "%";
  b.lastChild.style.width = (exec / top * 100).toFixed(1) + "%";
  b.title = "design " + fmtTok(design) + " · exec " + fmtTok(exec);
  return b;
}

export function share(design, exec) {
  const t = design + exec;
  return t ? Math.round(design / t * 100) + "%" : "–";
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
export function ctxChart(box, d, opts) {
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

// drawQuota draws each provider's 5h (dashed) and week (solid) percent over
// the window on one percent axis, each reset a faint line.
export function drawQuota(box) {
  if (!box) return;
  const d = usageNow();
  if (!d) return box.replaceChildren(el("p", "empty", readState(usage, "usage")));
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

// drawTokens draws the tokens tab.
export function drawTokens() {
  drawRoles($("roles-tok"));
  drawAgents($("agents-tok"), { sortable: true });
  drawQuota($("quota-tok"));
}
