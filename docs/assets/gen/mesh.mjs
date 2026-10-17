// Generates docs/assets/mesh-dark.svg and mesh-light.svg.
// node mesh.mjs <outdir>
// Five scenes, each followed by a still moment of the whole mesh; in a scene,
// what plays no part is dimmed.
import { writeFileSync } from "node:fs";
import { join } from "node:path";

const W = 960;
const css = [];
const out = [];
let n = 0;
let T = 0; // set once the timeline is laid out
const pct = (t) => +((t / T) * 100).toFixed(2);

// ---------- keyframes ----------
// Keyframes are built lazily (after T is known): anim() records points and
// returns a name; flush() writes them.
const pending = [];
const cache = new Map();
function anim(points) {
  const key = JSON.stringify(points);
  if (cache.has(key)) return cache.get(key);
  const name = "k" + (n++).toString(36);
  cache.set(key, name);
  pending.push([name, points]);
  return name;
}
function flush() {
  const fmt = (o) => Object.entries(o).map(([k, v]) => `${k}:${v}`).join(";");
  for (const [name, points] of pending) {
    // one value per offset (the latest wins), offsets sharing a value in one rule
    const at = new Map();
    at.set(0, fmt(points[0][1]));
    for (const [t, o] of points) at.set(pct(Math.min(Math.max(t, 0), T)), fmt(o));
    if (!at.has(100)) at.set(100, fmt(points[points.length - 1][1]));
    const by = new Map();
    for (const [p, v] of at) by.set(v, [...(by.get(v) ?? []), `${p}%`]);
    css.push(`@keyframes ${name}{${[...by].map(([v, ps]) => `${ps.join(",")}{${v}}`).join("")}}`);
  }
}
const A = (name) => `style="animation-name:${name}"`;
// opacity windows: list of [tOn, tOff]; fade in before tOn, out after tOff
function windows(list, base = 0, on = 1, fade = 0.25, fadeOut = fade) {
  const pts = [[0, { opacity: base }]];
  for (const [a, b] of list) if (!(a < b)) throw new Error(`windows ${JSON.stringify(list)}`);
  // overlapping windows (with their fades) merge into one
  const m = [];
  for (const w of [...list].sort((x, y) => x[0] - y[0])) {
    const p = m[m.length - 1];
    if (p && w[0] - fade <= p[1] + fadeOut) p[1] = Math.max(p[1], w[1]); else m.push([...w]);
  }
  list = m;
  for (const [a, b] of list) {
    if (a > 0) pts.push([a - fade, { opacity: base }]);
    pts.push([a, { opacity: on }], [b, { opacity: on }]);
    if (!(T && b >= T)) pts.push([b + fadeOut, { opacity: base }]);
    else pts.push([1e9, { opacity: on }]);
  }
  if (pts[pts.length - 1][0] < 1e9) pts.push([1e9, { opacity: base }]);
  if (list[0][0] <= 0) pts.shift();
  return anim(pts);
}

// ---------- drawing helpers ----------
const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;");
const text = (x, y, s, cls = "", extra = "") => `<text x="${x}" y="${y}"${cls ? ` class="${cls}"` : ""}${extra ? " " + extra : ""}>${s}</text>`;
const rect = (x, y, w, h, r, cls, extra = "") => `<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="${r}"${cls ? ` class="${cls}"` : ""}${extra ? " " + extra : ""}/>`;
const pd = (pts) => `M${pts.map((p) => p.join(" ")).join(" L")}`;
const sw = (s) => s.length * 6.6; // a 13px sans label's width, roughly
const pill = (x, y, label, cls = "pl") => {
  const ww = Math.round(label.length * 6.4 + 18);
  return `<g class="${cls}">${rect(x, y, ww, 22, 11, "pb")}${text(x + ww / 2, y + 15.5, esc(label), "s13 pt", 'text-anchor="middle"')}</g>`;
};


// ---------- geometry ----------
// Each box: a header, its NATS part (the hub, the leaf, or a note that it has
// none), then its daemon (bmesh serve, always on) as a rail on the left and
// the nodes it hosts stacked to its right. Messages travel on wires; one stops
// at a NATS part's border, the part lights, and the next leaves another border.
const BOX = { y: 162, w: 292, h: 262 };
const BX = { b: 20, h: 334, l: 648 };            // build-box, hub-box, laptop
const CMP = { y: 206, h: 62 };                   // the NATS hub / leaf panels
const LINK = 256;                                // the wire between boxes
const TOP = 290;                                 // rails and rows start here
const RY = [303, 335, 367, 399];                 // row centres
const RH = 26, BOT = RY[3] + RH / 2;             // row height; rails end at BOT
const rail = (b) => ({ x: BX[b] + 12, w: 28, cx: BX[b] + 26 });
const bus = (b) => BX[b] + 47;                   // the wire the nodes share
const ROWX = (b) => BX[b] + 54, ROWW = 226;
const NH = { x: BX.h + 12, y: CMP.y, w: 268, h: CMP.h };   // NATS hub
const LF = { x: BX.l + 12, y: CMP.y, w: 268, h: CMP.h };   // NATS leaf
const PH = { x: 420, y: 442, w: 120, h: 196 };             // phone
const NTY = 500;                                           // the ntfy wire's level

// wires, as point lists
const W_ = {
  // build-box dials the hub: its daemon and its lead share one wire
  bRail: [[rail("b").cx, TOP], [rail("b").cx, LINK], [NH.x, LINK]],
  bRow: (r) => [[ROWX("b"), RY[r]], [bus("b"), RY[r]], [bus("b"), LINK], [NH.x, LINK]],
  // hub-box reaches the hub from below
  hRail: [[rail("h").cx, TOP], [rail("h").cx, NH.y + NH.h]],
  hRow: (r) => [[ROWX("h"), RY[r]], [bus("h"), RY[r]], [bus("h"), NH.y + NH.h]],
  // the laptop reaches its leaf from below; the leaf reaches the hub
  lRail: [[rail("l").cx, TOP], [rail("l").cx, LF.y + LF.h]],
  lRow: (r) => [[ROWX("l"), RY[r]], [bus("l"), RY[r]], [bus("l"), LF.y + LF.h]],
  leaf: [[LF.x, LINK], [NH.x + NH.w, LINK]],
  ntfy: [[rail("h").cx, BOT], [rail("h").cx, NTY], [PH.x, NTY]],
};
const rev = (p) => [...p].reverse();
const hand = (b, i) => [[rail(b).x + rail(b).w, RY[i]], [ROWX(b), RY[i]]]; // a daemon to a node beside it

// ---------- hops ----------
const SPEED = 210;
const lenOf = (pts) => pts.slice(1).reduce((a, p, i) => a + Math.hypot(p[0] - pts[i][0], p[1] - pts[i][1]), 0);
const hops = [];
const at = {}; // glow windows per part, by colour: at[part][colour] = [[a, b], ...]
const glowAt = (part, c, w) => (((at[part] ??= {})[c] ??= []).push(w));
function hop(c, pts, t0) {
  const dur = Math.max(0.45, lenOf(pts) / SPEED);
  const h = { c, pts, t0, dur, land: t0 + dur, done: t0 + dur + 0.3 };
  hops.push(h);
  return h;
}
// a route: wires and the parts they pass through, e.g. [W_.bRow(0), "hub", rev(W_.hRow(0))].
// A part lights while the message is in it; a part last in the list is where it ends.
const PAUSE = 0.35;
function route(c, t0, legs) {
  let t = t0, last = null;
  const hs = [];
  for (let i = 0; i < legs.length; i++) {
    const l = legs[i];
    if (typeof l === "string") {
      const end = i === legs.length - 1;
      glowAt(l, c, [last.land - 0.1, end ? last.land + 0.8 : last.land + PAUSE + 0.25]);
      t = last.land + PAUSE;
      continue;
    }
    last = hop(c, l, t);
    hs.push(last);
  }
  return { hops: hs, t0, land: last.land, done: last.done, first: hs[0], last };
}
const along = (pts, t0, t1) => {
  const len = pts.slice(1).map((p, i) => Math.hypot(p[0] - pts[i][0], p[1] - pts[i][1]));
  const tot = len.reduce((a, b) => a + b, 0);
  let acc = 0;
  return pts.map((p, i) => { if (i) acc += len[i - 1]; return [+(t0 + ((t1 - t0) * acc) / tot).toFixed(3), ...p]; });
};
function train(h) {
  const way = along(h.pts, h.t0, h.t0 + h.dur);
  const tr = (x, y) => `translate(${x}px,${y}px)`;
  const [t0, x0, y0] = way[0], [tn, xn, yn] = way[way.length - 1];
  const pts = [[0, { opacity: 0, transform: tr(x0, y0) }], [t0 - 0.1, { opacity: 0, transform: tr(x0, y0) }], [t0, { opacity: 1, transform: tr(x0, y0) }]];
  for (const [t, x, y] of way.slice(1, -1)) pts.push([t, { transform: tr(x, y) }]);
  pts.push([tn, { opacity: 1, transform: tr(xn, yn) }], [tn + 0.12, { opacity: 0, transform: tr(xn, yn) }], [1e9, { opacity: 0, transform: tr(xn, yn) }]);
  out.push(`<g class="dot ${h.c.slice(2)}f" style="animation-name:${anim(pts)}"><circle r="9"/><circle r="5"/></g>`);
}

// ---------- the timeline ----------
const R = 1.6; // the still moment after each scene
const S = [];  // scenes [start, end]
let t = 0.8;
const scene = (len) => { const s = [t, t + len]; S.push(s); t += len + R; return s; };

// 1. every box builds: each starts its own leads, staggered; nothing crosses the hub
const s1 = scene(7.5);
const RUN1 = {
  h: [s1[0] + 0.5, s1[1] - 0.6], hx: [s1[0] + 1.1, s1[1] - 0.5],
  b: [s1[0] + 1.9, s1[1] - 0.4], bx: [s1[0] + 2.5, s1[1] - 0.3],
  l: [s1[0] + 3.3, s1[1] - 0.2],
};

// 2. a coordinator keeps leads on every box in step
const s2 = scene(21);
const C = {};
C.coord = [s2[0] + 0.4, s2[1] - 0.3];
C.h = s2[0] + 0.9; C.b = s2[0] + 1.3; C.l = s2[0] + 1.7;       // the leads start
C.e1 = route("--tl", s2[0] + 2.2, [W_.hRow(1), "hub"]);
C.e2 = route("--tl", s2[0] + 2.5, [W_.bRow(0), "hub"]);
C.e3 = route("--tl", s2[0] + 2.8, [W_.lRow(0), "leaf", W_.leaf, "hub"]);
C.w1 = route("--tl", C.e3.land + PAUSE, [rev(W_.hRow(0))]);    // the coordinator watches
C.hold = route("--hd", C.w1.done + 0.3, [W_.hRow(0), "hub", rev(W_.bRow(0))]);
C.gate = C.hold.land + 1.4;                                    // app-012 reaches its land gate
C.hv = route("--tl", C.gate + 0.1, [W_.bRow(0), "hub", rev(W_.hRow(0))]);
C.hLand = C.hv.done + 0.3;                                     // api-007 lands
C.e4 = route("--tl", C.hLand + 0.2, [W_.hRow(1), "hub", rev(W_.hRow(0))]);
C.rel = route("--hd", C.e4.done + 0.3, [W_.hRow(0), "hub", rev(W_.bRail), "railB"]);
C.wk = hop("--wk", hand("b", 0), C.rel.land + PAUSE);          // its daemon wakes app-012
C.wake = C.wk.land;
if (C.wake > s2[1] - 2) throw new Error("scene 2 overruns");

// 3. a lead sends a check to the box that can run it
const s3 = scene(13);
const Q = {};
Q.q = route("--am", s3[0] + 0.7, [W_.bRow(0)]);
Q.park = [Q.q.land, Q.q.land + 1.6];                           // waits on the hub
Q.take = route("--am", Q.park[1], [W_.leaf.slice().reverse(), "leaf", rev(W_.lRail), "railL"]);
Q.hand = hop("--am", hand("l", 1), Q.take.land + PAUSE);       // the daemon starts the routine
Q.run = [Q.hand.land, Q.hand.land + 2.0];
Q.rep = route("--pu", Q.run[1] + 0.2, [W_.lRail, "leaf", W_.leaf, "hub", rev(W_.bRow(0))]);

// 4. push notifications
const s4 = scene(13);
const P = [];
const push = (legs, t0, who, what, col) => {
  const a = route("--tl", t0, [...legs, "hub", rev(W_.hRail), "railH"]);
  const c = hop("--tl", W_.ntfy, a.land + 0.5);
  P.push({ a, c, who, what, col, at: c.land });
};
push([W_.hRow(1)], s4[0] + 0.6, "api-007", "is done", "--ok");
push([W_.bRow(0)], s4[0] + 3.6, "app-012", "asks", "--am");
push([W_.lRow(0), "leaf", W_.leaf], s4[0] + 6.6, "docs-003", "is blocked", "--rd");

// 5. a landed change reaches every box
const s5 = scene(20);
const L = {};
L.sleep = [s5[0] + 0.3, 0];
L.land = s5[0] + 1.0;                                          // app-012 lands; its checkout moves
L.e = route("--tl", L.land + 0.5, [W_.bRow(0), "hub", rev(W_.hRow(0))]);
L.sync0 = L.e.land + 0.4;                                      // you run bmesh sync
L.self = L.sync0 + 0.6;                                        // it levels hub-box first
L.j = route("--am", L.self + 0.5, [W_.hRow(0), "hub"]);
L.jb = route("--am", L.j.land + PAUSE, [W_.bRail.slice().reverse(), "railB"]);
L.jbh = hop("--am", hand("b", 3), L.jb.land + PAUSE);          // its daemon levels the checkout
L.rb = route("--pu", L.jbh.land + 0.8, [W_.bRail, "hub", rev(W_.hRow(0))]);
L.park = L.j.land + PAUSE;                                     // the laptop's job waits
L.wake = L.rb.done + 0.6;
L.sleep[1] = L.wake;
L.take = route("--am", L.wake + 0.6, [W_.leaf.slice().reverse(), "leaf", rev(W_.lRail), "railL"]);
L.th = hop("--am", hand("l", 2), L.take.land + PAUSE);
L.ff = L.th.land;
L.held = L.ff + 0.9;                                           // its own level on wake holds plugins
L.rl = route("--pu", L.held + 0.5, [W_.lRail, "leaf", W_.leaf, "hub"]);
L.sync = [L.sync0, L.rb.land + 0.5];

T = +t.toFixed(2); // the last still runs into the first scene
// every message is home, and has faded, before its scene ends
for (const h of hops) {
  const sc = S.find(([a, b]) => h.t0 >= a && h.t0 < b);
  if (!sc || h.done > sc[1] - 0.5) throw new Error(`a hop at ${h.t0.toFixed(1)}s overruns its scene ${JSON.stringify(sc)}`);
}
if (L.held > s5[1] - 3.5) throw new Error("the plugin hold shows too briefly");

// ---------- dimming ----------
// A part plays in some scenes; in the others it dims while the scene runs.
const DIM = 0.28;
function dimFor(plays) {
  const pts = [[0, { opacity: 1 }]];
  S.forEach(([a, b], i) => {
    if (plays.includes(i + 1)) return;
    pts.push([a, { opacity: 1 }], [a + 0.5, { opacity: DIM }], [b - 0.1, { opacity: DIM }], [b + 0.4, { opacity: 1 }]);
  });
  pts.push([1e9, { opacity: 1 }]);
  return anim(pts);
}
const part = (plays, inner) => `<g class="an" ${A(dimFor(plays))}>${inner}</g>`;

// ---------- rows ----------
const glows = [];
const ring = (x, y, w, h, r, c, ws, fade = 0.2, fadeOut = 0.4) => glows.push(rect(x, y, w, h, r, `rg ${c}-s an hide`, A(windows(ws, 0, 1, fade, fadeOut))));
const rowGlow = (r, c, ws) => ring(r.x, r.y, r.w, r.h, 6, c, ws);
function mkRow(b, i, path, role, opt = {}) {
  const x = ROWX(b) + (opt.indent ?? 0), w = ROWW - (opt.indent ?? 0), y = RY[i] - RH / 2;
  const r = { x, y, w, h: RH, cy: RY[i], role, xe: x + w - 12, size: opt.size ?? 13 };
  r.ix = r.xe - sw(role) - 11; // where a state mark sits, left of the role
  r.svg = `${rect(x, y, w, RH, 6, "nd")}<circle cx="${x + 14}" cy="${RY[i]}" r="4.5" fill="var(${opt.dot ?? "--wk"})"/>${text(x + 27, RY[i] + r.size * 0.35, esc(path), `m${r.size}`)}${text(r.xe, RY[i] + 4.5, role, "s13 mu", 'text-anchor="end"')}`;
  r.marks = [];
  return r;
}
// a state mark left of the role, and the role lit in its colour
const SPIN = (() => {
  const d = [];
  for (let k = 0; k < 8; k++) {
    const a = (k / 8) * 2 * Math.PI;
    d.push(`<circle cx="${(5 * Math.sin(a)).toFixed(2)}" cy="${(-5 * Math.cos(a)).toFixed(2)}" r="1.5" opacity="${(1 - k * 0.11).toFixed(2)}"/>`);
  }
  return `<g class="spin">${d.join("")}</g>`;
})();
const MARK = {
  run: ["wk", SPIN],
  held: ["hd", `<rect x="-3.5" y="-4.5" width="2.5" height="9" rx="1"/><rect x="1" y="-4.5" width="2.5" height="9" rx="1"/>`],
  done: ["ok", `<path d="M-4.5 0.5 L-1.5 3.5 L4.5 -3.5" class="ck"/>`],
};
function mark(r, kind, ws) {
  const [c, g] = MARK[kind];
  r.marks.push(`<g class="an hide" ${A(windows(ws, 0, 1, 0.2, 0.3))}><g transform="translate(${r.ix.toFixed(1)},${r.cy})" class="${c}">${g}</g>${text(r.xe, r.cy + 4.5, r.role, `s13 ${c}`, 'text-anchor="end"')}</g>`);
}
// the state dot in another colour
const dotAs = (r, c, ws) => r.marks.push(`<circle class="an hide" ${A(windows(ws, 0, 1, 0.2))} cx="${r.x + 14}" cy="${r.cy}" r="4.5" fill="var(${c})"/>`);
const row = (r) => r.svg + r.marks.join("");

// a checkout: the repo on the left; its branch and the commit it's at together on the right
function mkCheckout(b, i, repo, sha) {
  const x = ROWX(b), w = ROWW, y = RY[i] - RH / 2;
  const r = { x, y, w, h: RH, cy: RY[i], xe: x + w - 12, base: sha, marks: [] };
  r.svg = `${rect(x, y, w, RH, 6, "nd ck-row")}${text(x + 12, RY[i] + 4.55, repo, "m13")}`;
  r.at = (body, cls = "") => text(r.xe, RY[i] + 4.55, body, `m13${cls ? " " + cls : ""}`, 'text-anchor="end"');
  r.main = (s, cls = "mu") => `<tspan class="mu">main </tspan><tspan class="${cls}">${s}</tspan>`;
  r.marks.push(r.at(r.main(sha)));
  return r;
}
// states a checkout passes through, each [from, to, body]; the old commit shows before and after
function states(r, list, end) {
  r.marks.length = 0;
  r.marks.push(`<g class="an" ${A(windows([[list[0][0], end + 0.35]], 1, 0, 0.2, 0.3))}>${r.at(r.main(r.base))}</g>`);
  list.forEach(([a, b, body], i) => r.marks.push(`<g class="an hide" ${A(windows([[a + 0.25, (i + 1 < list.length ? list[i + 1][0] - 0.15 : end)]], 0, 1, 0.2, 0.3))}>${r.at(body)}</g>`));
}
const st = (word, cls) => `<tspan class="s13 ws ${cls}">${word}</tspan> `;

// ---------- the boxes ----------
const head = (b, name, role, os) => {
  const x = BX[b], nw = name.length * 9.2;
  return `<circle cx="${x + 18}" cy="${BOX.y + 24}" r="5" fill="var(--ok)"/>${text(x + 31, BOX.y + 29.5, name, "s16 b")}${role ? pill(x + 31 + nw + 10, BOX.y + 13, role) : ""}${os ? text(x + BOX.w - 16, BOX.y + 29.5, os, "s13 mu", 'text-anchor="end"') : ""}`;
};
const panel = (b) => rect(BX[b], BOX.y, BOX.w, BOX.h, 10, "pn");
const boxRing = (b, ws) => rect(BX[b], BOX.y, BOX.w, BOX.h, 10, "rg wk-s an hide", A(windows(ws, 0, 1, 0.3, 0.6)));
// the daemon: bmesh serve, always on, a rail beside the nodes it serves
const railOf = (b, plays) => {
  const r = rail(b);
  const cy = (TOP + 22 + BOT) / 2;
  return part(plays, `${rect(r.x, TOP, r.w, BOT - TOP, 14, "cmp")}${text(r.cx + 4.5, cy, "bmesh serve", "m13", `text-anchor="middle" transform="rotate(-90 ${r.cx + 4.5} ${cy})"`)}`) +
    `<circle cx="${r.cx}" cy="${TOP + 12}" r="4" fill="var(--ok)"/>`;
};
const cmp = (P, name, sub) => [rect(P.x, P.y, P.w, P.h, 8, "cmp"), text(P.x + 14, P.y + 21, name, "s16 b"), text(P.x + 14, P.y + 40, sub, "s13 mu")].join("");

// build-box: a client of the hub
const bL = mkRow("b", 0, "app-012", "lead", { size: 14 });
const bX = mkRow("b", 1, "app-012.ui", "executor", { indent: 18 });
const bV = mkRow("b", 2, "routine.verify", "routine", { dot: "--mu" });
const bC = mkCheckout("b", 3, "app", "3f2a9c1e");
// hub-box: runs the hub
const hO = mkRow("h", 0, "operator", "coordinator", { dot: "--tl" });
const hL = mkRow("h", 1, "api-007", "lead", { size: 14 });
const hX = mkRow("h", 2, "api-007.db", "executor", { indent: 18 });
const hC = mkCheckout("h", 3, "app", "3f2a9c1e");
// the laptop: a leaf, secure, the one Mac
const lL = mkRow("l", 0, "docs-003", "lead", { size: 14 });
const lM = mkRow("l", 1, "routine.xcode", "routine", { dot: "--mu" });
const lC = mkCheckout("l", 2, "app", "3f2a9c1e");
const lP = mkCheckout("l", 3, "plugins", "5c1e0a92");

// scene 1: every box builds, each on its own
mark(hL, "run", [RUN1.h]); mark(hX, "run", [RUN1.hx]);
mark(bL, "run", [RUN1.b]); mark(bX, "run", [RUN1.bx]);
mark(lL, "run", [RUN1.l]);
const rings = [boxRing("h", [[RUN1.h[0], RUN1.h[0] + 0.4]]), boxRing("b", [[RUN1.b[0], RUN1.b[0] + 0.4]]), boxRing("l", [[RUN1.l[0], RUN1.l[0] + 0.4]])];

// scene 2: the coordinator
mark(hO, "run", [C.coord]);
mark(hL, "run", [[C.h, C.hLand - 0.2]]); mark(hL, "done", [[C.hLand, s2[1] - 0.2]]);
mark(bL, "run", [[C.b, C.gate - 0.2], [C.wake, s2[1] - 0.3]]); mark(bL, "held", [[C.gate, C.wake - 0.2]]);
mark(lL, "run", [[C.l, s2[1] - 0.3]]);
const sent = (r, c, rt) => rowGlow(r, c, [[rt.t0, rt.t0 + 0.6]]);
const got = (r, c, rt) => rowGlow(r, c, [[rt.land, rt.done + 0.3]]);
sent(hL, "tl", C.e1); sent(bL, "tl", C.e2); sent(lL, "tl", C.e3);
got(hO, "tl", C.w1); sent(hO, "hd", C.hold); got(bL, "hd", C.hold);
sent(bL, "tl", C.hv); got(hO, "tl", C.hv); sent(hL, "tl", C.e4); got(hO, "tl", C.e4);
sent(hO, "hd", C.rel); rowGlow(bL, "wk", [[C.wake, C.wake + 0.8]]);

// scene 3: a check that needs a Mac
sent(bL, "am", Q.q);
mark(bL, "run", [[s3[0] + 0.2, s3[1] - 0.3]]);
mark(lM, "run", [Q.run]);
rowGlow(lM, "am", [[Q.hand.land, Q.hand.land + 0.9]]);
sent(lM, "pu", Q.rep);
got(bL, "pu", Q.rep);

// scene 4: pushes
const pstate = [[hL, "--ok"], [bL, "--am"], [lL, "--rd"]];
P.forEach((p, i) => { sent(pstate[i][0], "tl", p.a); dotAs(pstate[i][0], pstate[i][1], [[p.a.t0, s4[1] - 0.2]]); });

// scene 5: levelling
const e5 = s5[1] - 0.2;
mark(bL, "done", [[L.land, e5]]);
rowGlow(bC, "wk", [[L.land + 0.2, L.land + 1.1]]);
sent(bL, "tl", L.e); got(hO, "tl", L.e);
mark(hO, "run", [L.sync]);
rowGlow(hC, "wk", [[L.self, L.self + 0.9]]);
sent(hO, "am", L.j);
rowGlow(bC, "am", [[L.jbh.land, L.jbh.land + 0.9]]);
got(hO, "pu", L.rb);
rowGlow(lC, "am", [[L.ff, L.ff + 0.9]]);
rowGlow(lP, "hd", [[L.held, L.held + 0.9]]);
glowAt("hub", "--pu", [L.rl.land, s5[1] - 0.4]);               // the reply waits on the hub
const NEW = "81d0b7a4";
states(bC, [[L.land + 0.2, 0, `${st("+3", "ok")}${bC.main(NEW, "ok")}`], [L.jbh.land, 0, `${st("up to date", "mu")}${bC.main(NEW, "ok")}`]], e5);
states(hC, [[L.self, 0, `${st("+3", "ok")}${hC.main(NEW, "ok")}`]], e5);
states(lC, [[L.ff, 0, `${st("+3", "ok")}${lC.main(NEW, "ok")}`]], e5);
lP.marks.length = 0;
lP.marks.push(`<g class="an" ${A(windows([[L.held, e5 + 0.35]], 1, 0, 0.2, 0.3))}>${lP.at(lP.main(lP.base))}</g>`);
lP.marks.push(`<g class="an hide" ${A(windows([[L.held + 0.25, e5]], 0, 1, 0.2, 0.3))}><g transform="translate(${(lP.xe - sw("held +2") - 10).toFixed(1)},${lP.cy})" class="hd">${MARK.held[1]}</g>${text(lP.xe, lP.cy + 4.5, "held +2", "s13 hd", 'text-anchor="end"')}</g>`);

// ---------- static wires ----------
const stubs = (x, rows, side) => rows.map((i) => `M${x} ${RY[i]} H${x + side * 8}`).join(" ");
const wires = [];
// build-box's wire to the hub, from its daemon and its lead
wires.push(part([2, 3, 4, 5], `<path class="ln" d="${pd(W_.bRail)} M${bus("b")} ${LINK} V${RY[0]} ${stubs(bus("b"), [0], 1)}"/>`));
// hub-box's daemon and nodes, up into the hub
wires.push(part([2, 4, 5], `<path class="ln" d="${pd(W_.hRail)} M${bus("h")} ${NH.y + NH.h} V${RY[1]} ${stubs(bus("h"), [0, 1], 1)}"/>`));
// the laptop's daemon and lead, up into the leaf
const lpWires = [part([2, 3, 4, 5], `<path class="ln" d="${pd(W_.lRail)} M${bus("l")} ${LF.y + LF.h} V${RY[0]} ${stubs(bus("l"), [0], 1)}"/>`)];
// the hub <-> leaf link: solid; broken while the laptop sleeps; amber dashes while a job waits on it
const lk = `M${NH.x + NH.w} ${LINK} H${LF.x}`;
lpWires.push(part([2, 3, 4, 5], `<path class="ln an" ${A(windows([L.sleep], 1, 0, 0.3))} d="${lk}"/><path class="ln down an hide" ${A(windows([[L.sleep[0], L.park + 0.4]], 0, 1, 0.3))} d="${lk}"/><path class="ln dash an hide" ${A(windows([[L.park + 0.5, L.sleep[1]]], 0, 1, 0.3))} d="${lk}"/>`));
wires.push(part([4], `<path class="ln" d="${pd(W_.ntfy)}"/>${text(rail("h").cx + 8, NTY - 16, "ntfy", "s13 mu")}`));

// lit wires draw behind the dot
const litW = [];
for (const h of hops) {
  const c = h.c.slice(2);
  const k = anim([[0, { opacity: 0, "stroke-dashoffset": 100 }], [h.t0 - 0.05, { opacity: 0, "stroke-dashoffset": 100 }], [h.t0, { opacity: 1, "stroke-dashoffset": 100 }], [h.land, { opacity: 1, "stroke-dashoffset": 0 }], [h.done, { opacity: 1, "stroke-dashoffset": 0 }], [h.done + 0.4, { opacity: 0, "stroke-dashoffset": 0 }], [1e9, { opacity: 0, "stroke-dashoffset": 100 }]]);
  litW.push(`<g class="an hide lit" ${A(k)}><path class="lg ${c}-s" pathLength="100" d="${pd(h.pts)}"/><path class="lc ${c}-s" pathLength="100" d="${pd(h.pts)}"/></g>`);
}

// ---------- the parts' glows ----------
glowAt("hub", "--am", [Q.park[0], Q.park[1]]);
glowAt("hub", "--am", [L.park, L.take.t0 - 0.3]);
glowAt("leaf", "--tl", [L.wake, L.wake + 0.9]); // its leaf reconnects
const SHAPE = {
  hub: [NH.x, NH.y, NH.w, NH.h, 8],
  leaf: [LF.x, LF.y, LF.w, LF.h, 8],
  railB: [rail("b").x, TOP, rail("b").w, BOT - TOP, 14],
  railH: [rail("h").x, TOP, rail("h").w, BOT - TOP, 14],
  railL: [rail("l").x, TOP, rail("l").w, BOT - TOP, 14],
};
for (const [p, byc] of Object.entries(at)) for (const [c, ws] of Object.entries(byc)) ring(...SHAPE[p], c.slice(2), ws, 0.15, 0.4);

// ---------- assemble the boxes ----------
const bb = [panel("b"), head("b", "build-box", "client", "linux")];
const bbIn = [
  part([2, 3, 4, 5], `${text(BX.b + 16, NH.y + 21, "no NATS here: its daemon", "s13 mu")}${text(BX.b + 16, NH.y + 40, "and leads dial hub-box's hub", "s13 mu")}`),
  railOf("b", [2, 5]),
  part([1, 2, 3, 4, 5], row(bL)),
  part([1], `<path class="tr" d="M${bL.x + 14} ${bL.y + RH} V${RY[1]} H${bX.x}"/>` + row(bX)),
  part([], row(bV)),
  part([5], row(bC)),
];
const hb = [panel("h"), head("h", "hub-box", "hub · always on", "linux")];
const hbIn = [
  part([2, 3, 4, 5], cmp(NH, "NATS hub", '<tspan class="m13">bmesh hub</tspan> · JetStream')),
  railOf("h", [4]),
  part([2, 5], row(hO)),
  part([1, 2, 4], row(hL)),
  part([1], `<path class="tr" d="M${hL.x + 14} ${hL.y + RH} V${RY[2]} H${hX.x}"/>` + row(hX)),
  part([5], row(hC)),
];
// a job that waits on the hub
const waits = (w, words) => `<g class="an hide" ${A(windows([w], 0, 1, 0.15))}><circle cx="${NH.x + NH.w - 16}" cy="${NH.y + 16}" r="8" class="am-f2"/><circle cx="${NH.x + NH.w - 16}" cy="${NH.y + 16}" r="4.5" fill="var(--am)"/>${text(NH.x + NH.w - 30, NH.y + 21, words, "s13 am", 'text-anchor="end"')}</g>`;
const parks = [waits(Q.park, "1 job waits for a Mac"), waits([L.park, L.take.t0], "1 job waits for laptop")];

const lpPill = BX.l + 31 + "laptop".length * 9.2 + 10;
const lp = [
  panel("l"), head("l", "laptop", ""),
  `<g class="an" ${A(windows([L.sleep], 1, 0, 0.3))}>${pill(lpPill, BOX.y + 13, "leaf · secure")}${text(BX.l + BOX.w - 16, BOX.y + 29.5, "macOS", "s13 mu", 'text-anchor="end"')}</g>`,
];
const lpIn = [
  part([2, 3, 4, 5], cmp(LF, "NATS leaf", '<tspan class="m13">bmesh hub</tspan> · relays to the hub')),
  railOf("l", [3, 5]),
  part([1, 2, 4], row(lL)),
  part([3], row(lM)),
  part([5], row(lC)),
  part([5], row(lP)),
];
// asleep: the laptop fades, its dots grey and its badge says so
const lpG = [`<g class="an" ${A(windows([L.sleep], 1, 0.45, 0.4))}>`, ...lp, ...lpWires, ...lpIn, `</g>`];
lpG.push(`<circle class="an hide" ${A(windows([L.sleep], 0, 1, 0.3))} cx="${BX.l + 18}" cy="${BOX.y + 24}" r="5" fill="var(--mu)"/>`);
lpG.push(`<circle class="an hide" ${A(windows([L.sleep], 0, 1, 0.3))} cx="${rail("l").cx}" cy="${TOP + 12}" r="4" fill="var(--mu)"/>`);
lpG.push(`<g class="an hide" ${A(windows([L.sleep], 0, 1, 0.3))}>${pill(lpPill, BOX.y + 13, "asleep · leaf · secure", "pl sl")}</g>`);

// ---------- phone ----------
const ph = [`<g class="an" ${A(dimFor([4]))}>`];
ph.push(rect(PH.x, PH.y, PH.w, PH.h, 16, "pn"), rect(PH.x + PH.w / 2 - 12, PH.y + 8, 24, 4, 2, "notch"));
const scr = { x: PH.x + 5, y: PH.y + 18, w: PH.w - 10, h: PH.h - 30 };
ph.push(rect(scr.x, scr.y, scr.w, scr.h, 8, "scr"), text(PH.x + PH.w / 2, scr.y + 22, "09:41", "s14 mu", 'text-anchor="middle"'));
const CH = 34, CG = 6, cy0 = scr.y + 32;
P.forEach((p, i) => {
  // a card lands on top; each later one pushes it down a slot
  const pts = [[0, { opacity: 0, transform: "translate(0px,0px)" }], [p.at - 0.05, { opacity: 0, transform: "translate(0px,0px)" }], [p.at + 0.35, { opacity: 1, transform: "translate(0px,0px)" }]];
  P.slice(i + 1).forEach((q, k) => pts.push([q.at - 0.6, { opacity: 1, transform: `translate(0px,${k * (CH + CG)}px)` }], [q.at - 0.15, { opacity: 1, transform: `translate(0px,${(k + 1) * (CH + CG)}px)` }]));
  const lastY = (P.length - 1 - i) * (CH + CG);
  pts.push([s4[1] - 0.2, { opacity: 1, transform: `translate(0px,${lastY}px)` }], [s4[1] + 0.3, { opacity: 0, transform: `translate(0px,${lastY}px)` }], [1e9, { opacity: 0, transform: "translate(0px,0px)" }]);
  const cx = scr.x + 8, cw = scr.w - 16;
  ph.push(`<g class="an hide" ${A(anim(pts))}>${rect(cx, cy0, cw, CH, 6, "card")}<circle cx="${cx + 9}" cy="${cy0 + 11}" r="3.5" fill="var(${p.col})"/>${text(cx + 17, cy0 + 15, p.who, "m13 b")}${text(cx + 8, cy0 + 29, p.what, "s13")}</g>`);
});
ph.push(`</g>`);

// ---------- captions ----------
// the overview stays; each scene's caption shows under it while the scene plays
const top = [
  text(480, 30, "Three boxes, one NATS hub.", "s20 b", 'text-anchor="middle"'),
  text(480, 52, "hub-box runs the hub; build-box dials it; the laptop's leaf relays to it. On every box, bmesh serve is always on.", "s14 mu", 'text-anchor="middle"'),
];
const caps = [
  ["1. Every box builds.", ["Each box runs its own leads and executors through its own runner,", "each in its own git worktree, all at once."]],
  ["2. A coordinator keeps leads on every box in step.", ["Your coordinator session starts leads on all three boxes and follows their events through the hub.", "It holds app-012 before it lands until api-007 has landed, then releases it,", "and build-box's daemon wakes app-012 to go on."]],
  ["3. A lead sends a check to the box that can run it.", ["app-012 queues <tspan class=\"m14 am\">xcode</tspan>, a kind with <tspan class=\"m14\">os = \"darwin\"</tspan>. The job waits on the hub", "until the laptop's daemon, on the one Mac, takes it and runs it;", "the result comes back to app-012 as a <tspan class=\"pu\">reply</tspan>."]],
  ["4. Push notifications reach your phone.", ["When a lead is done, asks or is blocked, its event goes through the hub", "to the daemon with ntfy set, which pushes it to your phone, once."]],
  ["5. A change that lands reaches every box.", ["app-012 lands, which moves build-box's checkout. You run <tspan class=\"m14\">bmesh sync</tspan>: it levels hub-box", "and sends each other box's daemon a <tspan class=\"m14 am\">level</tspan> job; build-box's answers with a <tspan class=\"pu\">reply</tspan>, the asleep laptop's is queued.", "When the laptop wakes it levels, holds a new plugins commit for your approval, and leaves its reply on the hub."]],
];
const capEls = [];
caps.forEach(([h, lines], i) => {
  const [a, b] = S[i];
  capEls.push(`<g class="an hide" ${A(windows([[a + 0.2, b - 0.2]], 0, 1, 0.3, 0.3))}>${text(480, 88, h, "s16 b", 'text-anchor="middle"')}${lines.map((l, k) => text(480, 110 + k * 18, l, "s14", 'text-anchor="middle"')).join("")}</g>`);
});
// the still picture (reduced motion) names the scenes it would play
capEls.push(`<g class="st">${text(480, 88, "Five scenes play here:", "s16 b", 'text-anchor="middle"')}${text(480, 110, "every box builds; a coordinator keeps leads in step; a check goes to the box that can run it;", "s14", 'text-anchor="middle"')}${text(480, 128, "push notifications reach your phone; a change that lands reaches every box.", "s14", 'text-anchor="middle"')}</g>`);

// ---------- legend ----------
const lg = [];
const ly = PH.y + PH.h / 2;
let lx = PH.x + PH.w + 40;
const item = (c, label) => { const s = `<circle cx="${lx}" cy="${ly}" r="5" fill="var(${c})"/>${text(lx + 11, ly + 4.5, label, "s13 mu")}`; lx += 11 + sw(label) + 22; return s; };
const holdItem = () => { const s = `<g transform="translate(${lx},${ly})" class="hd">${MARK.held[1]}</g>${text(lx + 11, ly + 4.5, "hold", "s13 mu")}`; lx += 11 + sw("hold") + 22; return s; };
lg.push(item("--tl", "event"), holdItem(), item("--am", "job"), item("--pu", "reply"));
lg.push(`<line class="ln dash" x1="${lx - 5}" y1="${ly}" x2="${lx + 19}" y2="${ly}"/>`, text(lx + 25, ly + 4.5, "waits", "s13 mu"));

// ---------- progress bar ----------
const pb = [];
const by = PH.y + PH.h + 20, gapX = 10, segW = (920 - 4 * gapX) / 5;
const words = ["build", "coordinate", "route a check", "push notifications", "level"];
S.forEach(([a, b], i) => {
  const x = 20 + i * (segW + gapX);
  pb.push(rect(x, by, segW, 3, 1.5, "trk"));
  const fill = anim([[0, { transform: "scaleX(0)" }], [a, { transform: "scaleX(0)" }], [b, { transform: "scaleX(1)" }], [1e9, { transform: "scaleX(1)" }]]);
  pb.push(`<rect x="${x}" y="${by}" width="${segW}" height="3" rx="1.5" class="an bf b0" style="animation-name:${fill};animation-timing-function:linear"/>`);
  pb.push(text(x, by + 20, `${i + 1} ${words[i]}`, "s13 mu"));
  pb.push(`<g class="an hide" ${A(windows([[a + 0.1, b - 0.1]], 0, 1, 0.15))}>${text(x, by + 20, `${i + 1} ${words[i]}`, "s13")}</g>`);
});
const HT = by + 32;

for (const h of hops) train(h);
flush();

const style = `
svg{--fg:#e6edf3;--mu:#8b949e;--pf:#161b22;--ps:#30363d;--ed:#3d4652;--sc:#0d1117;--cp:#1c2129;--wk:#58a6ff;--tl:#39c5bb;--am:#d29922;--pu:#a371f7;--ok:#3fb950;--hd:#8fa7c4;--rd:#f85149}
svg.light{--fg:#1f2328;--mu:#59636e;--pf:#f6f8fa;--ps:#d0d7de;--ed:#c3ccd5;--sc:#ffffff;--cp:#ffffff;--wk:#0969da;--tl:#1a7f72;--am:#9a6700;--pu:#8250df;--ok:#1a7f37;--hd:#57728f;--rd:#cf222e}
text{fill:var(--fg)}
.s13,.s14,.s16,.s20{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Noto Sans",Helvetica,Arial,sans-serif}
.s13{font-size:13px}.s14{font-size:14px}.s16{font-size:16px}.s20{font-size:20px}
.m13,.m14{font-family:ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,"Liberation Mono",monospace}
.m13{font-size:13px}.m14{font-size:14px}.m13,.m14{word-spacing:-0.3em}.ws{word-spacing:normal}
.b{font-weight:600}.mu{fill:var(--mu)}.tl{fill:var(--tl)}.am{fill:var(--am)}.pu{fill:var(--pu)}.ok{fill:var(--ok)}.wk{fill:var(--wk)}.hd{fill:var(--hd)}
.pn{fill:var(--pf);stroke:var(--ps);stroke-width:1.5}
.cmp{fill:var(--cp);stroke:var(--ed);stroke-width:1.5}
.nd{fill:none;stroke:var(--ps);stroke-width:1.5}.ck-row{stroke-dasharray:3 3}
.ln{stroke:var(--ed);stroke-width:1.5;fill:none}
.dash{stroke-dasharray:4 6;stroke:var(--am)}.down{stroke-dasharray:4 6}
.tr{stroke:var(--ps);stroke-width:1.5;fill:none}
.ck{fill:none;stroke:var(--ok);stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.pb{fill:none;stroke:var(--ps);stroke-width:1.5}.pt{fill:var(--mu)}
.sl .pb{stroke:var(--hd)}.sl .pt{fill:var(--hd)}
.rg{fill:none;stroke-width:2}
.lc{fill:none;stroke-width:2.5}.lg{fill:none;stroke-width:8;stroke-linecap:round;opacity:.22}
.tl-s{stroke:var(--tl)}.am-s{stroke:var(--am)}.pu-s{stroke:var(--pu)}.hd-s{stroke:var(--hd)}.wk-s{stroke:var(--wk)}
.am-f2{fill:var(--am);opacity:.3}
.scr{fill:var(--sc);stroke:var(--ps);stroke-width:1}.card{fill:var(--pf);stroke:var(--ps);stroke-width:1.5}
.notch{fill:var(--ps)}
svg.light .bf{fill-opacity:.35}
.trk{fill:var(--ps)}.bf{fill:var(--mu);fill-opacity:.6;transform-box:fill-box;transform-origin:left center}.b0{transform:scaleX(0)}
.dot,.an{animation-duration:${T}s;animation-iteration-count:infinite;animation-timing-function:ease-in-out}
.dot,.hide,.st{opacity:0}.dot circle:first-child{opacity:.3}.dot,.lit{animation-timing-function:linear}.lit{stroke-dasharray:100 100}
.wkf{fill:var(--wk)}.tlf{fill:var(--tl)}.amf{fill:var(--am)}.puf{fill:var(--pu)}.hdf{fill:var(--hd)}
.spin{transform-box:fill-box;transform-origin:center;animation:sp .96s steps(8) infinite}
@keyframes sp{to{transform:rotate(360deg)}}
@media (prefers-reduced-motion:reduce){*{animation:none!important}.st{opacity:1}}
${css.join("\n")}
`;

const body = [
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${HT}" width="${W}" height="${HT}" class="dark" role="img" aria-label="buildermesh across three boxes, in five scenes: every box builds at once; a coordinator on hub-box follows leads on every box through the NATS hub and holds one before it lands until another has landed, and the held lead's daemon wakes it on release; a lead queues a check that needs macOS and the laptop's daemon takes it; leads' finishes, asks and blocks reach your phone as push notifications; after a change lands, bmesh sync levels every box's checkout, and the sleeping laptop levels when it wakes">`,
  `<title>buildermesh across boxes</title>`,
  `<style>${style}</style>`,
  ...top, ...capEls, ...bb, ...hb, ...wires, ...rings, ...bbIn, ...hbIn, ...lpG, ...litW, ...glows, ...parks, ...ph, ...lg, ...pb, ...out,
  `</svg>\n`,
].join("\n");

const dir = process.argv[2];
writeFileSync(join(dir, "mesh-dark.svg"), body);
writeFileSync(join(dir, "mesh-light.svg"), body.replace('class="dark"', 'class="light"'));
console.log(Buffer.byteLength(body), "bytes, loop", T, "s, scenes", JSON.stringify(S.map(([a, b]) => [+a.toFixed(1), +b.toFixed(1)])));
