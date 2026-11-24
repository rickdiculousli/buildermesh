// overview: the tree read, the KPI row, the digest strip, the needs-you
// band and the overview's panels.
import { $, badge, chartRoot, check, clock, dur, el, fetchJSON, hm, keepOpen, legendOf, loader, niceMax, readState, svg, svgText, yAxis } from "./core.js";
import { LIVE, NEEDS, WINDOWS, byStart, eachNode, hubNow, isStale, mesh, needRank, setTree, specs, stateOf, strip, tl, tok } from "./state.js";
import { showConn } from "./live.js";
import { clampRow, hover } from "./drawer.js";
import { abbrev, drawLanes, shortener } from "./timeline.js";
import { LEVEL_RANK, agentHref, agentName, ctxChart, drawAgents, drawQuota, drawRoles, flagWords, fmtTok, phaseBar, share, usage, usageNow, worstLevel } from "./tokens.js";
import { familyBurn, familyNote, repoBurn, specsLoad } from "./specs.js";
import { checkSoon, tally } from "./check.js";

// tree reads /api/tree and redraws the band and tiles: the hub's clock
// from its now, and a live event newer than the read winning over the
// state it returned.
export const tree = loader(async () => {
  const began = Date.now();
  return Object.assign(await fetchJSON("/api/tree"), { began });
}, {
  show: (r) => {
    if (!r.body) return; // the next event or poll tries again
    const v = r.body;
    setTree({ leads: v.leads || [], routines: v.routines || [], orphans: v.orphans || [], operator: v.operator || [] });
    mesh.staleMs = (+v.stale_after || 0) * 1000;
    mesh.host = String(v.host || "");
    const now = Date.parse(v.now || "");
    if (!isNaN(now)) mesh.skew = now - (r.began + Date.now()) / 2;
    for (const [path, st] of mesh.live) if (st.at < r.began) mesh.live.delete(path);
    if (check) check.trees++;
    renderBand();
    renderKpis();
    showConn();
  },
});

// liveState records a node's state from a live event with its words (text)
// and time (since), and redraws the band and tiles.
export function liveState(path, state, text, since) {
  mesh.live.set(path, { state, text: text || "", since: since || "", at: Date.now() });
  renderBand();
  renderKpis();
}

// needRows lists the leads, rounds and engineers (a round's children) in a
// NEEDS state, in need order, newest first within a state, each in its
// shown state (stateOf), as on the boxes.
function needRows() {
  const rows = [];
  const consider = (t) => {
    const st = stateOf(t.node.path);
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
function olderFold(rows) {
  const age = (r) => {
    const ms = Date.now() - Date.parse(r.at);
    return isNaN(ms) ? "" : " " + dur(ms).split(" ")[0];
  };
  const head = el("summary", "tap", null, el("span", null, rows.length + " older: " +
    rows.map((r) => r.path + " " + r.state + age(r)).join(", ")));
  const det = el("details", "older", null, head, ...rows.map((r) => needRow(r, false)));
  return keepOpen(det, "older");
}

// renderBand draws the band between the strip and the map: a needRow per
// needRows entry, the flagRows, the stale and dismissed ones folded into
// olderFold at the foot, then the operatorRows. Hidden when nothing needs the operator.
export function renderBand() {
  const band = $("needs");
  const gone = dismissals(), live = [], older = [];
  for (const r of needRows()) (r.stale || isDismissed(r, gone) ? older : live).push(r);
  const rows = live.map((r) => needRow(r, true));
  rows.push(...flagRows());
  if (older.length) rows.push(olderFold(older));
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

export function pct(x) {
  return Math.round(+x || 0) + "%";
}

// fiveH is a quota reading's 5-hour use; null is a plan with no such window.
export function fiveH(r) {
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

// renderStrip draws the digest fold under the KPI row; an open one (a
// newer digest's too) stays open.
function renderStrip() {
  const s = $("strip");
  const d = freshDigest();
  if (!d) {
    s.hidden = true;
    s.replaceChildren();
    return;
  }
  const head = el("summary", "tap", null, clock("digest", d.at, false), ": " + (d.title || ""));
  let row;
  if (d.body) {
    row = keepOpen(el("details", "digest", null, head, el("pre", null, d.body)), "digest");
  } else {
    row = el("div", "digest", null, ...head.childNodes);
  }
  s.replaceChildren(row);
  s.hidden = false;
}

// stripLoad reads /api/strip: the quota readings and the digest; a failed
// read keeps the last strip.
export const stripLoad = loader(() => fetchJSON("/api/strip"), {
  show: (r) => {
    if (!r.body) return;
    strip.quota = r.body.quota || [];
    strip.digest = r.body.digest || null; // null: no digest any more
    strip.every = (+r.body.digest_every || 0) * 1000;
    renderStrip();
    renderKpis();
  },
});

// liveDigest shows a daemon digest from a live frame at once, without a
// read (the strip reads at most once a minute).
export function liveDigest(f) {
  const p = f.payload && typeof f.payload === "object" ? f.payload : {};
  strip.digest = { at: p.at || f.at, title: String(p.text || ""), body: String(p.body || "") };
  renderStrip();
}

const AGENTS_SHOWN = 20; // the overview's agents table; the tokens tab has every row

// ctxAgent is the agent the overview's context panel shows: the row
// selected, else the live agent with the largest context, else the
// window's largest reader.
export function ctxAgent() {
  const as = usageNow() ? tok.data.agents : [];
  if (tok.sel && as.some((a) => a.agent === tok.sel)) return tok.sel;
  let best = null;
  for (const a of as) if (a.live && (!best || a.last_ctx > best.last_ctx)) best = a;
  return best ? best.agent : as.length ? as[0].agent : "";
}

// ctxPanel reads the context panel's agent (ctxAgent) over the window.
export const ctxPanel = loader(async () => {
  const agent = ctxAgent();
  if (!agent) return { status: 200, agent };
  return Object.assign(await fetchJSON("/api/usage/agent?agent=" + encodeURIComponent(agent) + "&since=" + tl.win), { agent });
}, {
  key: () => ctxAgent() + " " + tl.win,
  show: (r) => {
    if (!r.agent) tok.ctx = null;
    else if (r.body) tok.ctx = { agent: r.agent, data: r.body };
    drawCtxPanel();
  },
});

// select makes agent the context panel's, from a click on its row.
export function select(agent) {
  tok.sel = agent;
  drawAgents($("agents-map"), { limit: AGENTS_SHOWN, select: true });
  ctxPanel.run();
}

// drawCoords draws a row per coordinator session: its design and exec
// read, the design share, peak context and last call.
function drawCoords(box) {
  if (!box) return;
  const d = usageNow();
  if (!d) return box.replaceChildren(el("p", "empty", readState(usage, "usage")));
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

// drawCtxPanel draws the overview's context panel for ctxAgent().
function drawCtxPanel() {
  const box = $("ctx-map");
  if (!box || $("view-map").hidden) return; // drawn on the overview only
  const agent = ctxAgent();
  const a = usageNow() && tok.data.agents.find((x) => x.agent === agent);
  $("ctx-sub").textContent = a ? agentName(agent) + " · " + (a.role || "") + (a.size ? " " + a.size : "") + " · peak " + fmtTok(a.peak) +
    " · pick another in Agents" : "the main thread's context after each model call";
  const d = tok.ctx && tok.ctx.agent === agent ? tok.ctx.data : null;
  const n = !usageNow() ? 0 : ctxChart(box, d, { from: tok.data.from, to: tok.data.to, empty: agent ? readState(ctxPanel, agentName(agent)) || "No model calls." : "No model calls in this window." });
  tally(box, "ctx", n);
}

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
  if (!pts) return box.replaceChildren(el("p", "empty", readState(usage, "usage")));
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

// drawBurnRow draws the overview's row 5: the newest family's burndown and
// the busiest repo's 30 days (most landed).
function drawBurnRow() {
  const fam = $("fam-map"), rep = $("repo-map");
  const d = specs.data;
  if (!d) {
    for (const b of [fam, rep]) b.replaceChildren(el("p", "empty", readState(specsLoad, "specs")));
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

// drawOverview draws the overview: its timeline, tiles and panels.
export function drawOverview() {
  drawLanes($("tl-map"), tl.data, { fold: true, live: true, sessions: true });
  renderKpis();
  drawRoles($("roles-map"));
  drawAgents($("agents-map"), { limit: AGENTS_SHOWN, select: true });
  drawCoords($("coords-map"));
  drawCtxPanel();
  drawQuota($("quota-map"));
  drawOutliers($("outliers-map"));
  drawBurnRow();
}
