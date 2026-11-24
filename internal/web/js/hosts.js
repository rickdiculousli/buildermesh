// hosts: the hosts tab, a card per box: its head line, contention, CPU and
// memory stacked by lead tree over the window, and its live processes.
import { $, bare, card, chartRoot, el, fetchJSON, hm, loader, niceMax, readState, svg, svgText, timeAxis, yAxis } from "./core.js";
import { pages, tl } from "./state.js";
import { crosshair, paint } from "./drawer.js";
import { tally } from "./check.js";
import { agentHref } from "./tokens.js";

const FAST_MS = 10000; // the live poll while a box is busy
const SLOW_MS = 60000; // while no box's live CPU is over half its cores
const GB = 1024 * 1024 * 1024;
const MAC_LEVEL = { 1: "normal", 2: "warn", 4: "critical" };
// WAITS are the contention lines: pressure's key, its words, its class.
const WAITS = [["cpu", "CPU wait", "w-cpu"], ["mem", "mem wait", "w-mem"], ["io", "io wait", "w-io"]];

let data = null; // the last /api/hosts for the window shown

// hosts reads every box's history in the window and its live answer.
export const hosts = loader((win) => fetchJSON("/api/hosts?live=1&since=" + win), {
  key: () => tl.win,
  show: (r) => {
    if (r.body) data = r.body;
    drawHosts();
  },
});

// hostsPoll re-reads while the tab is shown: every FAST_MS while some box's
// live CPU is over half its cores, else every SLOW_MS.
export function hostsPoll() {
  if (pages.view !== "hosts") return;
  const busy = ((data && data.boxes) || []).some((b) => b.live && b.live.cpu > b.live.cores / 2);
  if (Date.now() - hosts.state.at >= (busy ? FAST_MS : SLOW_MS) - 1000) hosts.run();
}

// gb renders bytes: "5.2 GB", "29 GB", or "340 MB" under a GB.
export function gb(bytes) {
  const v = (+bytes || 0) / GB;
  if (v > 0 && v < 1) return Math.round(v * 1024) + " MB";
  return (v < 10 && v > 0 ? v.toFixed(1) : Math.round(v)) + " GB";
}

function cores(v) {
  return (+v || 0).toFixed(1);
}

// axisNum labels a cores axis: "8", "2.5".
function axisNum(v) {
  return String(v >= 10 ? Math.round(v) : +v.toFixed(2));
}

function isMac(m) {
  return !!m && (m.os === "darwin" || m.mac_pressure != null);
}

// group is the series an agent's use draws in: its lead tree (the top
// segment of a node's path), "misc" for routines, sessions and repos, or
// "other".
function group(agent) {
  if (agent === "other") return "other";
  if (/^(routine\.|session\.|repo:)/.test(agent)) return "misc";
  return agent.split(".")[0];
}

// headLine is "k16 · CPU wait 11% · mem wait 0% · io wait 2% · load 25.5/16 · 11/29 GB".
function headLine(b) {
  const m = b.live || b.last;
  const parts = [b.host];
  if (isMac(m)) {
    parts.push("pressure " + (MAC_LEVEL[m.mac_pressure] || "–"));
  } else {
    const p = (b.live && b.live.pressure10) || m.pressure || {};
    for (const [k, words] of WAITS) parts.push(words + " " + (p[k] == null ? "–" : Math.round(p[k]) + "%"));
  }
  parts.push("load " + (+m.load1 || 0).toFixed(1) + "/" + m.cores);
  parts.push(((+m.mem_used || 0) / GB).toFixed(0) + "/" + ((+m.mem_total || 0) / GB).toFixed(0) + " GB");
  return parts.join(" · ");
}

// runs splits a box's sparse buckets into runs of adjacent ones: a missing
// bucket is a gap in every chart.
function runs(pts, step) {
  const out = [];
  for (const p of pts) {
    const r = out[out.length - 1];
    if (r && p.at - r[r.length - 1].at <= step) r.push(p);
    else out.push([p]);
  }
  return out;
}

// frame lays out a chart of the window in box: {root, x, y, W, H, P}.
function frame(box, label, yMax, fmt, H) {
  const W = Math.max(box.clientWidth, 240), P = { l: 46, r: 8, t: 8, b: 20 };
  const from = data.from, to = data.to;
  const x = (t) => P.l + (Math.max(from, Math.min(to, t)) - from) / Math.max(1, to - from) * (W - P.l - P.r);
  const y = (v) => H - P.b - Math.min(v, yMax) / yMax * (H - P.t - P.b);
  const root = chartRoot(W, H, label);
  yAxis(root, y, yMax, P, W, fmt);
  timeAxis(root, x, from, to, P.t, H - P.b, H);
  return { root, x, y, W, H, P };
}

// nearest is the bucket under the pointer at px, or null off the data.
function nearest(f, pts, step, px) {
  const t = data.from + (px - f.P.l) / (f.W - f.P.l - f.P.r) * (data.to - data.from);
  let best = null;
  for (const p of pts) if (!best || Math.abs(p.at + step / 2 - t) < Math.abs(best.at + step / 2 - t)) best = p;
  return best && Math.abs(best.at + step / 2 - t) <= step ? best : null;
}

function span(p, step) {
  return hm(p.at) + "–" + hm(p.at + step);
}

// drawWaits draws the box's contention: CPU, memory and I/O wait in %
// (macOS: load per core), a line each, broken where buckets are missing.
function drawWaits(box, b) {
  const step = data.step, pts = b.points, mac = isMac(b.last);
  const ncores = (b.last && b.last.cores) || 1;
  const lines = mac ? [["load", "load per core", "w-cpu", (p) => p.load1 / ncores]]
    : WAITS.map(([k, words, cls]) => [k, words, cls, (p) => p.pressure ? p.pressure[k] : null]);
  let top = 0;
  for (const p of pts) for (const l of lines) top = Math.max(top, +l[3](p) || 0);
  const yMax = mac ? niceMax(Math.max(top, 1)) : Math.min(100, niceMax(Math.max(top, 10))); // PSI is at most 100%
  const fmt = mac ? (v) => v.toFixed(2) : (v) => Math.round(v) + "%";
  const f = frame(box, mac ? "load per core" : "CPU, memory and I/O wait", yMax, fmt, 130);
  const mid = (p) => f.x(p.at + step / 2);
  for (const [, , cls, val] of lines) {
    let d = "", prev = null;
    for (const p of pts) {
      const v = val(p);
      if (v == null) { prev = null; continue; }
      d += (prev && p.at - prev.at <= step ? "L" : "M") + mid(p).toFixed(1) + "," + f.y(v).toFixed(1);
      prev = p;
    }
    if (d) svg("path", { class: "wline " + cls, d }, f.root);
  }
  crosshair(f.root, f.P, f.W, f.H, (px) => {
    const p = nearest(f, pts, step, px);
    if (!p) return null;
    return { x: mid(p), text: span(p, step) + "\n" + lines.map(([, words, , val]) => {
      const v = val(p);
      return words.padEnd(14) + (v == null ? "–" : mac ? v.toFixed(2) : v.toFixed(1) + "%");
    }).join("\n") };
  });
  const leg = el("div", "legend", null, ...lines.map(([, words, cls]) => el("span", null, null, el("i", cls), words)));
  box.replaceChildren(leg, f.root);
}

// drawStack draws one of the box's use (cpu or rss) over the window,
// stacked: each lead tree in its hue, routines, sessions and repos in
// slate, other in grey on top; cap is the box's size, a dashed line.
function drawStack(box, b, key, label, fmt, axisFmt, cap) {
  const step = data.step, pts = b.points;
  const sums = new Map();
  for (const p of pts) for (const [a, u] of Object.entries(p.agents || {})) {
    const g = group(a);
    sums.set(g, (sums.get(g) || 0) + (+u[key] || 0));
  }
  const trees = [...sums.keys()].filter((g) => g !== "misc" && g !== "other").sort((a, c) => sums.get(c) - sums.get(a));
  const order = [...trees, ...["misc", "other"].filter((g) => sums.has(g))];
  const val = (p, g) => {
    let s = 0;
    for (const [a, u] of Object.entries(p.agents || {})) if (group(a) === g) s += +u[key] || 0;
    return s;
  };
  let top = 0;
  for (const p of pts) top = Math.max(top, order.reduce((s, g) => s + val(p, g), 0));
  // Memory's axis rounds in GB, not bytes.
  const unit = key === "rss" ? GB : 1;
  const f = frame(box, label, niceMax(Math.max(top, cap || 0) / unit) * unit, axisFmt, 150);
  const lo = (p) => f.x(p.at), hi = (p) => f.x(p.at + step);
  for (const run of runs(pts, step)) {
    const below = run.map(() => 0);
    for (const g of order) {
      const upper = [], lower = [];
      run.forEach((p, i) => {
        const v = val(p, g);
        for (const X of [lo(p), hi(p)]) lower.push([X, f.y(below[i])]);
        below[i] += v;
        for (const X of [lo(p), hi(p)]) upper.push([X, f.y(below[i])]);
      });
      const area = svg("path", { class: "area " + seriesClass(g),
        d: "M" + upper.concat(lower.reverse()).map(([a, c]) => a.toFixed(1) + "," + c.toFixed(1)).join("L") + "Z" }, f.root);
      if (seriesClass(g) === "hue") paint(area, g);
    }
  }
  if (cap > 0) svg("line", { class: "ref", x1: f.P.l, x2: f.W - f.P.r, y1: f.y(cap), y2: f.y(cap) }, f.root);
  crosshair(f.root, f.P, f.W, f.H, (px) => {
    const p = nearest(f, pts, step, px);
    if (!p) return null;
    const lines = order.map((g) => seriesName(g).padEnd(16) + fmt(val(p, g)).padStart(8));
    return { x: f.x(p.at + step / 2), text: span(p, step) + "\n" + lines.join("\n") + "\n" + "box".padEnd(16) + fmt(order.reduce((s, g) => s + val(p, g), 0)).padStart(8) };
  });
  const leg = el("div", "legend", null, ...order.map((g) => {
    const i = el("i", seriesClass(g));
    if (seriesClass(g) === "hue") paint(i, g);
    return el("span", null, null, i, seriesName(g));
  }));
  if (cap > 0) leg.append(el("span", null, null, el("i", "refkey"), (key === "cpu" ? cap + " cores" : gb(cap))));
  box.replaceChildren(leg, f.root);
}

function seriesClass(g) {
  return g === "misc" ? "u-misc" : g === "other" ? "u-other" : "hue";
}

function seriesName(g) {
  return g === "misc" ? "routines, sessions" : g;
}

// chip is a process's agent: a node's in its lead tree's hue linking its
// page, a session's grey linking its page, a routine's or repo's grey;
// null for other.
function chip(agent) {
  if (agent === "other") return null;
  const sess = agent.startsWith("session.");
  const words = agent.startsWith("routine.") ? "routine" : agent.startsWith("repo:") ? agent.slice(5)
    : sess ? "session " + agent.split(".").slice(2).join(".").slice(0, 8) : agent;
  const node = group(agent) !== "misc";
  const c = el(node || sess ? "a" : "span", "chip", words);
  if (node || sess) c.setAttribute("href", agentHref(agent));
  c.title = agent;
  paint(c, node ? agent : "routine");
  return c;
}

// fold is the name a live total folds under: a node's lead tree, routines
// together, each session and repo on its own, other.
function fold(agent) {
  if (agent.startsWith("routine.")) return "routines";
  if (agent.startsWith("session.")) return "session " + agent.split(".").slice(2).join(".").slice(0, 8);
  if (agent.startsWith("repo:")) return agent.slice(5);
  return agent === "other" ? "other" : agent.split(".")[0];
}

// procTable is the live processes, by CPU, and under them the totals
// folded by lead tree.
function procTable(live) {
  const rows = (live.procs || []).map((p) => {
    const c = chip(String(p.agent || "other"));
    const name = el("td", "who", p.comm);
    name.title = "pid " + p.pid + " · parent " + p.ppid;
    return el("tr", c ? "" : "other", null, name, el("td", "chipcell", null, ...(c ? [c] : [])),
      el("td", "r", cores(p.cpu)), el("td", "r", gb(p.rss)));
  });
  const head = el("tr", null, null, el("th", null, "process"), el("th", null, "agent"), el("th", "r", "cpu"), el("th", "r", "mem"));
  const folded = new Map();
  for (const [a, t] of Object.entries(live.totals || {})) {
    const k = fold(a), s = folded.get(k) || { procs: 0, cpu: 0, rss: 0 };
    s.procs += +t.procs || 0;
    s.cpu += +t.cpu || 0;
    s.rss += +t.rss || 0;
    folded.set(k, s);
  }
  const totals = [...folded].sort(([a, x], [c, y]) => (a === "other") - (c === "other") || y.cpu - x.cpu).map(([k, s]) =>
    el("li", k === "other" ? "muted" : "", [k, s.procs + (s.procs === 1 ? " proc" : " procs"), gb(s.rss), cores(s.cpu) + " cores"].join(" · ")));
  return [el("div", "tw", null, el("table", "ag procs", null, el("thead", null, null, head), el("tbody", null, null, ...rows))),
    el("ul", "totals", null, ...totals), rows.length];
}

// boxCard is a box's card; charts lists the chart boxes to draw once the
// card is in the page and has its width.
function boxCard(b, charts) {
  if (!b.last && !b.live) return [bare(b.host, "no stats"), 0];
  const m = b.live || b.last;
  const parts = [];
  if (!b.live) parts.push(el("p", "empty", "no live stats"));
  if (b.points.length) {
    const waits = el("div", "chart"), cpu = el("div", "chart"), mem = el("div", "chart");
    parts.push(el("h4", null, isMac(m) ? "load per core" : "contention"), waits, el("h4", null, "cpu, cores"), cpu, el("h4", null, "memory"), mem);
    charts.push(() => drawWaits(waits, b),
      () => drawStack(cpu, b, "cpu", "CPU in cores by lead tree", cores, axisNum, m.cores),
      () => drawStack(mem, b, "rss", "memory by lead tree", gb, (v) => axisNum(v / GB) + " GB", m.mem_total));
  } else {
    parts.push(el("p", "empty", "no history in this window"));
  }
  let procs = 0;
  if (b.live) {
    const [table, totals, n] = procTable(b.live);
    procs = n;
    parts.push(el("h4", null, "processes now"), table, totals);
  }
  const c = card(headLine(b), ...parts);
  c.classList.add("hostcard");
  return [c, procs];
}

// drawHosts draws the tab from the last read of the window shown.
export function drawHosts() {
  const panel = $("hosts-panel");
  const s = hosts.state;
  if (!data || s.key !== tl.win || s.phase !== "shown") {
    panel.replaceChildren(el("p", "empty", readState(hosts, "hosts")));
    tally(panel, "hosts", 0);
    return;
  }
  const charts = [];
  let procs = 0;
  const cards = data.boxes.map((b) => {
    const [c, n] = boxCard(b, charts);
    procs += n;
    return c;
  });
  panel.replaceChildren(...cards, el("p", "note", "Memory sums each process's resident memory: pages processes share count once for each."));
  for (const draw of charts) draw();
  tally(panel, "hosts", cards.length);
  tally(panel, "procs", procs);
}
