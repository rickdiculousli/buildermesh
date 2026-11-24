// core: DOM and SVG helpers, times and clocks, fetchJSON, loader, every,
// kept and the ?check=1 counters. It imports nothing: every module may use it.

const NS = "http://www.w3.org/2000/svg";
const PHONE = "(max-width: 699px)";
const NARROW = "(max-width: 999px)";
const FETCH_MS = 10000; // a read taking longer is abandoned
const REFETCH_MS = 500;
const REFETCH_MAX_MS = 2000; // a steady stream of events still refetches this often

// ICON goes with every state colour, beside the state's word.
export const ICON = { working: "●", landing: "▲", held: "‖", silent: "◌", asking: "?", blocked: "■", idle: "○", done: "✓", operator: "▸" };

export const $ = (id) => document.getElementById(id);

// check is set by ?check=1: a self-check that writes viewport and
// connection facts into document.title for headless screenshots.
export const check = new URLSearchParams(location.search).get("check") === "1"
  ? { conn: [], trees: 0, replay: 0, live: 0, lanes: 0, segs: {}, ticks: 0, roles: 0, agents: 0, ctx: 0, quota: 0, outliers: 0, burndown: 0 } : null;

export function svg(tag, attrs, parent) {
  const e = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs || {})) e.setAttribute(k, String(v));
  if (parent) parent.append(e);
  return e;
}

export function svgText(attrs, s, parent) {
  const e = svg("text", attrs, parent);
  e.textContent = s;
  return e;
}

// fit cuts s to n characters with an ellipsis.
export function fit(s, n) {
  return s.length <= n ? s : s.slice(0, n - 1) + "…";
}

export function lastToken(path) {
  return path.slice(path.lastIndexOf(".") + 1);
}

// axisStep is the time between grid lines for a span and width: an hour
// each hour narrower than 1000px on the 6h and 24h windows.
export function axisStep(span) {
  const narrow = narrowQ.matches;
  if (span <= 3600) return 600;
  if (span <= 6 * 3600) return narrow ? 3600 : 1800;
  if (span <= 24 * 3600) return narrow ? 3600 : 7200;
  if (span <= 3 * 86400) return 6 * 3600;
  return 86400;
}

export function hm(t) {
  return new Date(t * 1000).toISOString().slice(11, 16);
}

export function md(t) {
  return new Date(t * 1000).toISOString().slice(5, 10);
}

// spanText is "14:30–15:10 (40m)", the day too when the span crosses one.
export function spanText(a, b) {
  const day = md(a) !== md(b);
  return (day ? md(a) + " " : "") + hm(a) + "–" + (day ? md(b) + " " : "") + hm(b) + " (" + dur((b - a) * 1000) + ")";
}

export function svgTspan(cls, s) {
  const e = document.createElementNS(NS, "tspan");
  e.setAttribute("class", cls);
  e.textContent = s;
  return e;
}

export function span(cls, s) {
  const e = document.createElement("span");
  e.className = cls;
  e.textContent = s;
  return e;
}

export function stamp(at) {
  const d = new Date(at);
  return isNaN(d) ? String(at) : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

const STALE_MS = 10 * 60000; // a quota reading older than this shows its age

// el makes an HTML element with a class and text (both optional) and appends
// the children.
export function el(tag, cls, text, ...children) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined && text !== null && text !== "") e.textContent = String(text);
  e.append(...children);
  return e;
}

export function badge(state) {
  const s = String(state || "idle");
  return el("span", "badge st-" + s.replace(/[^a-z-]/g, ""), (ICON[s] ? ICON[s] + " " : "") + s);
}

// dur renders ms as "1d 3h", "1h 10m", "4m" or, with secs, "4m 10s".
export function dur(ms, secs) {
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

export function clock(kind, at, until) {
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

export function tick() {
  for (const e of document.querySelectorAll("[data-clock]")) tickOne(e);
}

// fetchJSON reads a same-origin URL: {status, body}, body null unless 2xx.
export async function fetchJSON(url) {
  const stop = new AbortController();
  const timer = setTimeout(() => stop.abort(), FETCH_MS);
  try {
    const r = await fetch(url, { cache: "no-store", signal: stop.signal });
    return { status: r.status, body: r.ok ? await r.json() : null };
  } finally {
    clearTimeout(timer);
  }
}

// loader is one read and its lifecycle, state {phase, key, at, status}:
// phase is idle (never read), loading (reading, nothing shown for key),
// shown, failed (the read failed and nothing is shown for key) or missing
// (a 404); key is what the state is for (opts.key(): the window, a lead's
// path), at and status the last answer's. read(key) fetches and resolves
// to {status, ...}; opts.show(answer, key) then draws it, once state has
// moved, unless opts.key() no longer names key (the route or the window
// moved on), when the answer is dropped. A failed re-read keeps shown.
// run() reads one at a time (a call during a read queues one more); soon()
// reads REFETCH_MS after the last call but no later than REFETCH_MAX_MS
// after the first waiting.
export function loader(read, opts = {}) {
  const key = opts.key || (() => "");
  const show = opts.show || (() => {});
  const state = { phase: "idle", key: "", at: 0, status: 0 };
  let busy = false, again = false, timer = 0, first = 0;
  const run = async () => {
    if (busy) {
      again = true;
      return;
    }
    busy = true;
    const k = key();
    if (k !== state.key || state.phase === "idle") Object.assign(state, { phase: "loading", key: k, status: 0 });
    try {
      let r;
      try {
        r = await read(k);
      } catch (e) {
        r = { status: 0 }; // unreachable or timed out
      }
      if (key() !== k) return;
      const ok = r.status >= 200 && r.status < 300;
      state.phase = ok ? "shown" : r.status === 404 ? "missing" : state.phase === "shown" ? "shown" : "failed";
      state.at = Date.now();
      state.status = r.status;
      show(r, k);
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
  return { run, soon, state, key };
}

// readState is what a view shows in place of l's read while it has none
// for l.key(): "loading <what>", missing (a 404's words), or "could not
// load <what>; retrying" when the read failed; "" once something shows.
export function readState(l, what, missing) {
  const s = l.state;
  if (s.key !== l.key() || s.phase === "idle" || s.phase === "loading") return "loading " + what;
  if (s.phase === "missing") return missing;
  if (s.phase === "failed") return "could not load " + what + "; retrying";
  return "";
}

// every runs fn each ms, the page's one timer kind; a hidden tab skips it
// unless opts.hidden.
export function every(ms, fn, opts = {}) {
  setInterval(() => {
    if (opts.hidden || !document.hidden) fn();
  }, ms);
}

export function card(title, ...children) {
  const c = el("section", "card", null, el("h3", null, title), ...children);
  c.dataset.card = title;
  return c;
}

// bare is a card with nothing to show: one muted line. In the lead page's
// one-column layout it sorts after the cards with content.
export function bare(title, text) {
  const c = el("section", "card bare", null, el("span", "k", title), " · " + text);
  c.dataset.card = title;
  return c;
}

// kept holds the keys of the folds and clamped rows left open (a band
// row's path, a frame's frameKey, "inbox <id>", "flag <agent>", "<round>
// <id>" for a round's spec, "<kind> <repo>" for a spent fold, "older" for
// the band's, "digest" for the strip's), so a redraw keeps them open; past
// KEPT_MAX the oldest are forgotten.
const open = new Set();
const KEPT_MAX = 200;

export function kept(key) {
  return open.has(key);
}

// keep records key open (on) or closed.
export function keep(key, on) {
  open.delete(key);
  if (on) open.add(key);
  while (open.size > KEPT_MAX) open.delete(open.values().next().value);
}

// keepOpen opens det when its key is kept and keeps it as it is toggled.
export function keepOpen(det, key) {
  if (kept(key)) det.open = true;
  det.addEventListener("toggle", () => keep(key, det.open));
  return det;
}

export function field(name, value) {
  return el("div", "field", null, el("span", "k", name), el("span", "v", value || "–"));
}

// niceMax rounds v up to a round multiple of a power of ten, whose
// quarters are round too.
export function niceMax(v) {
  if (!(v > 0)) return 1;
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 1.2, 1.6, 2, 2.4, 3, 4, 5, 6, 8, 10]) if (v <= m * p) return m * p;
  return 10 * p;
}

// timeAxis draws the grid lines and labels of [from, to] under a plot.
export function timeAxis(root, x, from, to, top, bottom, H) {
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
export function yAxis(root, y, max, P, W, fmt) {
  for (let i = 0; i <= 4; i++) {
    const v = max * i / 4, Y = y(v);
    svg("line", { class: "grid", x1: P.l, x2: W - P.r, y1: Y, y2: Y }, root);
    svgText({ class: "axis", x: P.l - 6, y: Y + 4, "text-anchor": "end" }, fmt(v), root);
  }
}

export function chartRoot(W, H, label) {
  return svg("svg", { width: W, height: H, viewBox: `0 0 ${W} ${H}`, role: "img", "aria-label": label });
}

// legendOf is a legend row: [class, words] pairs, each a swatch and its name.
export function legendOf(items) {
  return el("div", "legend", null, ...items.map(([cls, words]) => el("span", null, null, el("i", cls), words)));
}

// phone and narrowQ are the widths the drawer's lanes and the charts follow.
export const phone = window.matchMedia(PHONE);
export const narrowQ = window.matchMedia(NARROW);
