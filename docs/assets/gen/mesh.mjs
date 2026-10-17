// Generates docs/assets/mesh-dark.svg and mesh-light.svg.
// node gen.mjs <outdir>
// Five scenes, each after a still moment of the whole mesh; in a scene,
// what plays no part is dimmed.
import { writeFileSync } from "node:fs";
import { join } from "node:path";

const W = 960, HT = 656;
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
const Y0 = 206;                        // the wire through the hub: build-box -> NATS hub -> laptop's leaf
const RY = [241, 273, 305, 337, 369];  // row centres
const RH = 26;
const BB = { x: 20, y: 104, w: 280, h: 290 };   // build-box
const HB = { x: 340, y: 104, w: 280, h: 290 };  // hub-box
const LP = { x: 660, y: 104, w: 280, h: 290 };  // laptop
const NH = { x: 364, y: 146, w: 240, h: 72 };   // the NATS hub, inside hub-box
const LF = { x: 676, y: 146, w: 248, h: 72 };   // the NATS leaf, inside the laptop
const BL = 292, HL = 349, LL = 932;             // each box's local wire
const ROW = { b: { x: 36, w: 248 }, h: { x: 364, w: 240 }, l: { x: 676, w: 248 } };
const PH = { x: 420, y: 410, w: 120, h: 196 };  // phone
const NT = [[604, RY[0]], [612, RY[0]], [612, 470], [PH.x + PH.w, 470]]; // hub-box's daemon -> ntfy -> phone
const Q1 = [584, Y0];

// paths between a row and the NATS hub
const fromB = (r) => [[284, RY[r]], [BL, RY[r]], [BL, Y0], [NH.x, Y0]];
const fromH = (r) => [[NH.x, RY[r]], [HL, RY[r]], [HL, Y0], [NH.x, Y0]];
const fromL = (r) => [[924, RY[r]], [LL, RY[r]], [LL, Y0], [NH.x, Y0]];
const rev = (p) => [...p].reverse();

// ---------- hops (a message moving along a wire) ----------
const GAP = 0.3, SPEED = 230;
const lenOf = (pts) => pts.slice(1).reduce((a, p, i) => a + Math.hypot(p[0] - pts[i][0], p[1] - pts[i][1]), 0);
const hops = [];
function hop(c, pts, t0, dur) {
  dur = dur ?? Math.max(0.8, lenOf(pts) / SPEED);
  const h = { c, pts, t0, dur, land: t0 + dur, done: t0 + dur + GAP };
  hops.push(h);
  return h;
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
  const pts = [[0, { opacity: 0, transform: tr(x0, y0) }], [t0 - 0.15, { opacity: 0, transform: tr(x0, y0) }], [t0, { opacity: 1, transform: tr(x0, y0) }]];
  for (const [t, x, y] of way.slice(1, -1)) pts.push([t, { transform: tr(x, y) }]);
  pts.push([tn, { opacity: 1, transform: tr(xn, yn) }], [tn + 0.15, { opacity: 0, transform: tr(xn, yn) }], [1e9, { opacity: 0, transform: tr(xn, yn) }]);
  const name = anim(pts);
  out.push(`<g class="dot ${h.c.slice(2)}f" style="animation-name:${name}"><circle r="9"/><circle r="5"/></g>`);
}

// ---------- the timeline ----------
const R = 1.6; // the still moment before each scene
const S = [];  // scenes [start, end]
let t = 0.8;
const scene = (len) => { const s = [t, t + len]; S.push(s); t += len + R; return s; };

// 1. every box builds: each starts its own leads, staggered; nothing crosses the hub
const s1 = scene(7.5);
const RUN1 = {
  hb: [s1[0] + 0.5, s1[1] - 0.6], hbx: [s1[0] + 1.1, s1[1] - 0.5],
  bb: [s1[0] + 1.9, s1[1] - 0.4], bbx: [s1[0] + 2.5, s1[1] - 0.3],
  lp: [s1[0] + 3.3, s1[1] - 0.2],
};

// 2. a coordinator keeps leads on every box in step
const s2 = scene(20);
const C = {};
C.coord = [s2[0] + 0.4, s2[1] - 0.3];
C.a = s2[0] + 0.9; C.w = s2[0] + 1.3; C.d = s2[0] + 1.7;     // leads start
C.e1 = hop("--tl", fromH(2), s2[0] + 2.1);
C.e2 = hop("--tl", fromB(1), s2[0] + 2.4);
C.e3 = hop("--tl", fromL(1), s2[0] + 2.7);
C.w1 = hop("--tl", rev(fromH(1)), C.e3.land + 0.1);           // the hub -> coordinator (watch)
C.h1 = hop("--hd", fromH(1), C.w1.done + 0.3);                // hold web-007 before land
C.h2 = hop("--hd", rev(fromB(1)), C.h1.land + 0.15);
C.atGate = C.h2.land + 1.2;                                   // web-007 reaches its land gate
C.hv1 = hop("--tl", fromB(1), C.atGate + 0.1);                // it publishes held
C.hv2 = hop("--tl", rev(fromH(1)), C.hv1.land + 0.1);
C.held = C.atGate;
C.aDone = C.hv2.done + 0.2;                                   // app-012 lands
C.e4 = hop("--tl", fromH(2), C.aDone + 0.2);
C.w2 = hop("--tl", rev(fromH(1)), C.e4.land + 0.15);
C.u1 = hop("--hd", fromH(1), C.w2.done + 0.2);                // release it
C.u2 = hop("--hd", rev(fromB(1)), C.u1.land + 0.15);
C.wGo = C.u2.land;
if (C.wGo > s2[1] - 2) throw new Error("scene 2 overruns");

// 3. a lead sends a check to the box that can run it
const s3 = scene(12.5);
const Rt = {};
Rt.q = hop("--am", [...fromB(1), Q1], s3[0] + 0.7);
Rt.park = [Rt.q.land, Rt.q.land + 1.2];
Rt.take = hop("--am", [Q1, [NH.x + NH.w, Y0], ...rev(fromL(2)).slice(1)], Rt.park[1]);
Rt.run = [Rt.take.land, Rt.take.land + 1.8];
Rt.rep = hop("--pu", [...fromL(2), ...rev(fromB(1)).slice(0)].filter((p, i, a) => !(i && p[0] === a[i - 1][0] && p[1] === a[i - 1][1])), Rt.run[1] + 0.2);

// 4. push notifications
const s4 = scene(12.5);
const P = [];
const pushFrom = (path, t0, who, what, col) => {
  const a = hop("--tl", path, t0);
  const b = hop("--tl", rev(fromH(0)), a.land + 0.1);
  const c = hop("--tl", NT, b.land + 0.1);
  P.push({ a, b, c, who, what, col, at: c.land });
};
pushFrom(fromH(2), s4[0] + 0.6, "app-012", "is done", "--ok");
pushFrom(fromB(1), s4[0] + 3.3, "web-007", "asks", "--am");
pushFrom(fromL(1), s4[0] + 6.0, "docs-003", "is blocked", "--rd");

// 5. a landed change reaches every box
const s5 = scene(17);
const L = {};
L.sleep = [s5[0] + 0.3, 0];
L.land = s5[0] + 0.9;
L.e = hop("--tl", fromB(1), L.land + 0.3);
L.w = hop("--tl", rev(fromH(1)), L.e.land + 0.1);
L.sync = [L.w.land + 0.2, s5[1] - 0.3];
L.self = L.sync[0] + 0.7;                                     // hub-box levels itself first
L.j = hop("--am", fromH(1), L.self + 0.4);
L.jb = hop("--am", [[NH.x, Y0], [BL, Y0], [BL, RY[4]], [284, RY[4]]], L.j.land + 0.1);
L.jl = hop("--am", [[NH.x, Y0], Q1], L.j.land + 0.4);
L.sleep[1] = L.jl.land + 1.4;
L.wake = L.sleep[1];
L.take = hop("--am", [Q1, [NH.x + NH.w, Y0], [LL, Y0], [LL, RY[3]], [924, RY[3]]], L.wake + 0.6);
L.held = L.take.land + 0.8;
L.rb = hop("--pu", [...fromB(4), ...rev(fromH(1)).slice(1)], L.jb.land + 0.4);   // build-box answers: up to date
L.rl = hop("--pu", [...fromL(3), ...rev(fromH(1)).slice(1)], L.take.land + 0.4); // the laptop answers: fast-forwarded
L.sync[1] = L.rl.land + 0.4;

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
const rowGlow = (r, c, ws) => glows.push(rect(r.x, r.y, r.w, r.h, 6, `rg ${c}-s an hide`, A(windows(ws, 0, 1, 0.2, 0.4))));
function mkRow(box, i, path, role, opt = {}) {
  const x = opt.x ?? ROW[box].x, w = opt.w ?? ROW[box].w, y = RY[i] - RH / 2;
  const r = { x, y, w, h: RH, cy: RY[i], role, xe: x + w - 12, dot: opt.dot ?? "--wk", size: opt.size ?? 13 };
  r.ix = r.xe - sw(role) - 11; // where a state mark sits, left of the role
  r.svg = `${rect(x, y, w, RH, 6, "nd")}<circle cx="${x + 14}" cy="${RY[i]}" r="4.5" fill="var(${r.dot})"/>${text(x + 27, RY[i] + r.size * 0.35, esc(path), `m${r.size}`)}${text(r.xe, RY[i] + 4.5, role, "s13 mu", 'text-anchor="end"')}`;
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
  const k = A(windows(ws, 0, 1, 0.2, 0.3));
  r.marks.push(`<g class="an hide" ${k}><g transform="translate(${r.ix.toFixed(1)},${r.cy})" class="${c}">${g}</g>${text(r.xe, r.cy + 4.5, r.role, `s13 ${c}`, 'text-anchor="end"')}</g>`);
}
// the state dot in another colour
const dotAs = (r, c, ws) => r.marks.push(`<circle class="an hide" ${A(windows(ws, 0, 1, 0.2))} cx="${r.x + 14}" cy="${r.cy}" r="4.5" fill="var(${c})"/>`);
const row = (r) => r.svg + r.marks.join("");

// a checkout: the repo, its branch and the commit it's at
function mkCheckout(box, i, repo, sha) {
  const { x, w } = ROW[box], y = RY[i] - RH / 2;
  const r = { x, y, w, h: RH, cy: RY[i], xe: x + w - 12, marks: [] };
  r.svg = `${rect(x, y, w, RH, 6, "nd")}<path class="br" d="M${x + 11} ${RY[i] - 6} V${RY[i] + 6} M${x + 11} ${RY[i] + 1} Q${x + 11} ${RY[i] - 3} ${x + 17} ${RY[i] - 5}"/>${text(x + 27, RY[i] + 4.55, `${repo}<tspan class="mu"> main</tspan>`, "m13")}`;
  r.base = sha;
  r.marks.push(text(r.xe, RY[i] + 4.55, sha, "m13 mu", 'text-anchor="end"'));
  return r;
}
function levelled(r, at, end, sha, ahead) {
  // the old commit fades out, the new one in, with how far it moved
  r.marks.length = 0;
  r.marks.push(`<g class="an" ${A(windows([[at, end + 0.35]], 1, 0, 0.2, 0.3))}>${text(r.xe, r.cy + 4.55, r.base, "m13 mu", 'text-anchor="end"')}</g>`);
  r.marks.push(`<g class="an hide" ${A(windows([[at + 0.25, end]], 0, 1, 0.2, 0.3))}>${text(r.xe, r.cy + 4.55, `<tspan class="s13">${ahead}</tspan>  ${sha}`, "m13 ok", 'text-anchor="end"')}</g>`);
}

// ---------- build-box (a client of the hub) ----------
const head = (P, name, role, os) => {
  const nw = name.length * 9.2;
  return `<circle cx="${P.x + 18}" cy="${P.y + 24}" r="5" fill="var(--ok)"/>${text(P.x + 31, P.y + 29.5, name, "s16 b")}${role ? pill(P.x + 31 + nw + 10, P.y + 13, role) : ""}${os ? text(P.x + P.w - 16, P.y + 29.5, os, "s13 mu", 'text-anchor="end"') : ""}`;
};
const boxRing = (P, ws) => rect(P.x, P.y, P.w, P.h, 10, "rg wk-s an hide", A(windows(ws, 0, 1, 0.3, 0.6)));
const all = [];

// wires first, under everything
const wires = [];
const stubs = (x, rows, side) => rows.map((i) => `M${x} ${RY[i]} H${x + side * 8}`).join(" ");
wires.push(part([2, 3, 4, 5], `<path class="ln" d="M${BL} ${Y0} V${RY[4]} ${stubs(BL, [1, 4], -1)} M${BL} ${Y0} H${NH.x}"/>`));
wires.push(part([2, 4, 5], `<path class="ln" d="M${NH.x} ${Y0} H${HL} V${RY[2]} ${stubs(HL, [1, 2], 1)}"/>`));
wires.push(part([4], `<path class="ln" d="M${NH.x} ${Y0} H${HL} V${RY[0]} H${NH.x}"/>`));
// the hub <-> leaf link: solid, or dashed while the laptop sleeps
// the hub <-> leaf link: solid; broken while the laptop sleeps; amber dashes while a job waits on it
wires.push(part([2, 3, 4, 5], `<path class="ln an" ${A(windows([L.sleep], 1, 0, 0.3))} d="M${NH.x + NH.w} ${Y0} H${LF.x}"/><path class="ln down an hide" ${A(windows([[L.sleep[0], L.jl.land - 0.1]], 0, 1, 0.3))} d="M${NH.x + NH.w} ${Y0} H${LF.x}"/><path class="ln dash an hide" ${A(windows([[L.jl.land, L.sleep[1]]], 0, 1, 0.3))} d="M${NH.x + NH.w} ${Y0} H${LF.x}"/>`));
wires.push(part([4], `<path class="ln" d="${pd(NT)}"/>${text(620, 462, "ntfy", "s13 mu")}`));

// lit wires: the same geometry, in the message's colour, with a soft glow
const litW = [];
const lit = (h) => {
  const c = h.c.slice(2);
  const k = anim([[0, { opacity: 0, "stroke-dashoffset": 100 }], [h.t0 - 0.05, { opacity: 0, "stroke-dashoffset": 100 }], [h.t0, { opacity: 1, "stroke-dashoffset": 100 }], [h.land, { opacity: 1, "stroke-dashoffset": 0 }], [h.done, { opacity: 1, "stroke-dashoffset": 0 }], [h.done + 0.4, { opacity: 0, "stroke-dashoffset": 0 }], [1e9, { opacity: 0, "stroke-dashoffset": 100 }]]);
  litW.push(`<g class="an hide lit" ${A(k)}><path class="lg ${c}-s" pathLength="100" d="${pd(h.pts)}"/><path class="lc ${c}-s" pathLength="100" d="${pd(h.pts)}"/></g>`);
};
for (const h of hops) lit(h);

const bb = [];
bb.push(rect(BB.x, BB.y, BB.w, BB.h, 10, "pn"), head(BB, "build-box", "client", "linux"));
bb.push(part([2, 3, 4, 5], `${text(36, 168, "no NATS here: its daemon", "s13 mu")}${text(36, 187, "and leads dial hub-box's hub", "s13 mu")}`));
const bD = mkRow("b", 0, "bmesh serve", "daemon", { dot: "--mu" });
const bW = mkRow("b", 1, "web-007", "lead", { size: 14 });
const bX = mkRow("b", 2, "web-007.alpha", "executor", { x: 56, w: 228 });
const bV = mkRow("b", 3, "routine.verify", "routine", { dot: "--mu" });
const bC = mkCheckout("b", 4, "web", "3f2a9c1e");

// ---------- hub-box (runs the hub) ----------
const hb = [];
hb.push(rect(HB.x, HB.y, HB.w, HB.h, 10, "pn"), head(HB, "hub-box", "hub · always on", "linux"));
const hD = mkRow("h", 0, "bmesh serve", "daemon", { dot: "--mu" });
const hO = mkRow("h", 1, "operator", "coordinator", { dot: "--tl" });
const hA = mkRow("h", 2, "app-012", "lead", { size: 14 });
const hX = mkRow("h", 3, "app-012.alpha", "executor", { x: 384, w: 220 });
const hC = mkCheckout("h", 4, "web", "3f2a9c1e");

// ---------- laptop (a leaf) ----------
const lpIn = [];
lpIn.push(rect(LP.x, LP.y, LP.w, LP.h, 10, "pn"), head(LP, "laptop", ""));
const lpPill = LP.x + 31 + "laptop".length * 9.2 + 10;
lpIn.push(`<g class="an" ${A(windows([L.sleep], 1, 0, 0.3))}>${pill(lpPill, LP.y + 13, "leaf · secure")}</g>`);
lpIn.push(`<g class="an" ${A(windows([L.sleep], 1, 0, 0.3))}>${text(LP.x + LP.w - 16, LP.y + 29.5, "macOS", "s13 mu", 'text-anchor="end"')}</g>`);
const lD = mkRow("l", 0, "bmesh serve", "daemon", { dot: "--mu" });
const lW = mkRow("l", 1, "docs-003", "lead", { size: 14 });
const lM = mkRow("l", 2, "routine.mac-build", "routine", { dot: "--mu" });
const lC = mkCheckout("l", 3, "web", "3f2a9c1e");
const lP = mkCheckout("l", 4, "plugins", "5c1e0a92");

// scene 1: every box builds, each on its own
mark(hA, "run", [RUN1.hb]); mark(hX, "run", [RUN1.hbx]);
mark(bW, "run", [RUN1.bb]); mark(bX, "run", [RUN1.bbx]);
mark(lW, "run", [RUN1.lp]);
const rings = [boxRing(HB, [[RUN1.hb[0], RUN1.hb[0] + 0.4]]), boxRing(BB, [[RUN1.bb[0], RUN1.bb[0] + 0.4]]), boxRing(LP, [[RUN1.lp[0], RUN1.lp[0] + 0.4]])];

// scene 2: the coordinator
mark(hO, "run", [C.coord]);
mark(hA, "run", [[C.a, C.aDone - 0.2]]); mark(hA, "done", [[C.aDone, s2[1] - 0.2]]);
mark(bW, "run", [[C.w, C.held - 0.2], [C.wGo, s2[1] - 0.3]]); mark(bW, "held", [[C.held, C.wGo - 0.2]]);
mark(lW, "run", [[C.d, s2[1] - 0.3]]);
rowGlow(hA, "tl", [[C.e1.t0, C.e1.t0 + 0.6], [C.e4.t0, C.e4.t0 + 0.6]]);
rowGlow(bW, "tl", [[C.e2.t0, C.e2.t0 + 0.6], [C.hv1.t0, C.hv1.t0 + 0.6]]);
rowGlow(lW, "tl", [[C.e3.t0, C.e3.t0 + 0.6]]);
rowGlow(hO, "tl", [[C.w1.land, C.w1.done + 0.3], [C.hv2.land, C.hv2.done + 0.3], [C.w2.land, C.w2.done + 0.3]]);
rowGlow(hO, "hd", [[C.h1.t0, C.h1.t0 + 0.6], [C.u1.t0, C.u1.t0 + 0.6]]);
rowGlow(bW, "hd", [[C.h2.land, C.h2.done + 0.3], [C.u2.land, C.u2.done + 0.3]]);

// scene 3: a check that needs a Mac
rowGlow(bW, "am", [[Rt.q.t0, Rt.q.t0 + 0.6]]);
mark(lM, "run", [Rt.run]);
rowGlow(lM, "am", [[Rt.take.land, Rt.take.done + 0.3]]);
rowGlow(bW, "pu", [[Rt.rep.land, Rt.rep.done + 0.6]]);

// scene 4: pushes
const states = [[hA, "--ok"], [bW, "--am"], [lW, "--rd"]];
P.forEach((p, i) => { rowGlow(states[i][0], "tl", [[p.a.t0, p.a.t0 + 0.6]]); dotAs(states[i][0], states[i][1], [[p.a.t0, s4[1] - 0.2]]); });
rowGlow(hD, "tl", P.map((p) => [p.b.land, p.c.t0 + 0.5]));

// scene 5: levelling
mark(bW, "done", [[L.land, s5[1] - 0.2]]);
rowGlow(bW, "tl", [[L.e.t0, L.e.t0 + 0.6]]);
rowGlow(hO, "tl", [[L.w.land, L.w.done]]);
rowGlow(hO, "pu", [[L.rb.land, L.rb.done + 0.3], [L.rl.land, L.rl.done + 0.3]]);
rowGlow(bC, "pu", [[L.rb.t0, L.rb.t0 + 0.6]]);
rowGlow(lC, "pu", [[L.rl.t0, L.rl.t0 + 0.6]]);
rowGlow(hC, "wk", [[L.self, L.self + 0.9]]);
mark(hO, "run", [L.sync]);
levelled(hC, L.self, s5[1] - 0.2, "81d0b7a4", "+3");
// build-box's own land moved its checkout; sync's level finds it up to date
{
  const e = s5[1] - 0.2, at = L.land + 0.4, up = L.jb.land;
  const sha = (body, cls, ws, base = 0, on = 1) => `<g class="an${base ? "" : " hide"}" ${A(windows(ws, base, on, 0.2, 0.3))}>${text(bC.xe, bC.cy + 4.55, body, `m13 ${cls}`, 'text-anchor="end"')}</g>`;
  bC.marks.length = 0;
  bC.marks.push(sha(bC.base, "mu", [[at, e + 0.35]], 1, 0), sha('<tspan class="s13">+3</tspan>  81d0b7a4', "ok", [[at + 0.25, up - 0.15]]), sha('<tspan class="s13 mu ws">up to date</tspan>  81d0b7a4', "ok", [[up + 0.35, e]]));
}
levelled(lC, L.take.land, s5[1] - 0.2, "81d0b7a4", "+3");
rowGlow(bC, "am", [[L.jb.land, L.jb.done + 0.3]]);
rowGlow(bC, "wk", [[L.land, L.land + 0.9]]);
rowGlow(lC, "am", [[L.take.land, L.take.done + 0.3]]);
lP.marks.length = 0;
lP.marks.push(`<g class="an" ${A(windows([[L.held, s5[1] - 0.2 + 0.35]], 1, 0, 0.2, 0.3))}>${text(lP.xe, lP.cy + 4.55, lP.base, "m13 mu", 'text-anchor="end"')}</g>`);
lP.marks.push(`<g class="an hide" ${A(windows([[L.held + 0.25, s5[1] - 0.2]], 0, 1, 0.2, 0.3))}><g transform="translate(${lP.xe - sw("held +2") - 10},${lP.cy})" class="hd">${MARK.held[1]}</g>${text(lP.xe, lP.cy + 4.5, "held +2", "s13 hd", 'text-anchor="end"')}</g>`);
rowGlow(lP, "hd", [[L.held, L.held + 0.9]]);

bb.push(part([], row(bD)), part([1, 2, 3, 4, 5], row(bW)), part([1], `<path class="tr" d="M${bW.x + 14} ${bW.y + RH} V${RY[2]} H${bX.x}"/>` + row(bX)), part([], row(bV)), part([5], row(bC)));
hb.push(part([4], row(hD)), part([2, 5], row(hO)), part([1, 2, 4], row(hA)), part([1], `<path class="tr" d="M${hA.x + 14} ${hA.y + RH} V${RY[3]} H${hX.x}"/>` + row(hX)), part([5], row(hC)));
// the NATS hub, a part of hub-box
hb.push(part([2, 3, 4, 5], [
  rect(NH.x, NH.y, NH.w, NH.h, 8, "cmp"),
  text(NH.x + 14, NH.y + 22, "NATS hub", "s16 b"),
  text(NH.x + 14, NH.y + 41, '<tspan class="m13">bmesh hub</tspan> · JetStream', "s13 mu"),
  `<path class="ln" d="M${NH.x} ${Y0} H${NH.x + NH.w}"/>`,
].join("")));
const pdot = (x, y) => `<circle cx="${x}" cy="${y}" r="9" class="am-f2"/><circle cx="${x}" cy="${y}" r="5" fill="var(--am)"/>`;
const hubGlow = (c, ws) => glows.push(rect(NH.x, NH.y, NH.w, NH.h, 8, `rg ${c}-s an hide`, A(windows(ws, 0, 1, 0.2, 0.4))));
const parks = [];
parks.push(`<g class="an hide" ${A(windows([Rt.park], 0, 1, 0.15))}>${pdot(...Q1)}${text(NH.x + NH.w - 12, NH.y + 22, "waits for a Mac", "s13 am", 'text-anchor="end"')}</g>`);
parks.push(`<g class="an hide" ${A(windows([[L.jl.land, L.take.t0]], 0, 1, 0.15))}>${pdot(...Q1)}${text(NH.x + NH.w - 12, NH.y + 22, "waits for laptop", "s13 am", 'text-anchor="end"')}</g>`);
// the hub glows in a message's colour while it carries one
hubGlow("am", [[Rt.park[0], Rt.park[1]], [L.jl.land, L.take.t0]]);

// the laptop and its leaf
const lp = [];
lp.push(...lpIn);
lp.push(part([2, 3, 4, 5], `<path class="ln" d="M${LF.x + LF.w} ${Y0} H${LL} V${RY[3]} ${stubs(LL, [1, 2, 3], -1)}"/>`));
lp.push(part([2, 3, 4, 5], [
  rect(LF.x, LF.y, LF.w, LF.h, 8, "cmp"),
  text(LF.x + 14, LF.y + 22, "NATS leaf", "s16 b"),
  text(LF.x + 14, LF.y + 41, '<tspan class="m13">bmesh hub</tspan> · relays to the hub', "s13 mu"),
  `<path class="ln" d="M${LF.x} ${Y0} H${LF.x + LF.w}"/>`,
].join("")));
const leafGlow = (c, ws) => glows.push(rect(LF.x, LF.y, LF.w, LF.h, 8, `rg ${c}-s an hide`, A(windows(ws, 0, 1, 0.2, 0.4))));
// when a hop is on the wire through a panel (x0..x1 at Y0), by colour
function crossing(x0, x1) {
  const by = {};
  for (const h of hops) {
    const w = along(h.pts, h.t0, h.t0 + h.dur);
    let lo = Infinity, hi = -Infinity;
    for (let i = 1; i < w.length; i++) {
      const [ta, xa, ya] = w[i - 1], [tb, xb, yb] = w[i];
      if (ya !== Y0 || yb !== Y0) continue;
      const a = Math.max(Math.min(xa, xb), x0), b = Math.min(Math.max(xa, xb), x1);
      if (a > b) continue;
      const at = (x) => (xa === xb ? ta : ta + ((tb - ta) * (x - xa)) / (xb - xa));
      lo = Math.min(lo, at(a), at(b)); hi = Math.max(hi, at(a), at(b));
    }
    if (lo <= hi) (by[h.c] ??= []).push([lo - 0.15, hi + 0.35]);
  }
  return by;
}
for (const [c, ws] of Object.entries(crossing(NH.x, NH.x + NH.w))) hubGlow(c.slice(2), ws);
for (const [c, ws] of Object.entries(crossing(LF.x, LF.x + LF.w))) leafGlow(c.slice(2), ws);
lp.push(part([], row(lD)), part([1, 2, 4], row(lW)), part([3], row(lM)), part([5], row(lC)), part([5], row(lP)));

// asleep: the laptop fades, its dot greys and a badge says so
const sleepK = A(windows([L.sleep], 1, 0.45, 0.4));
const lpG = [`<g class="an" ${sleepK}>`, ...lp, `</g>`];
lpG.push(`<circle class="an hide" ${A(windows([L.sleep], 0, 1, 0.3))} cx="${LP.x + 18}" cy="${LP.y + 24}" r="5" fill="var(--mu)"/>`);
lpG.push(`<g class="an hide" ${A(windows([L.sleep], 0, 1, 0.3))}>${pill(lpPill, LP.y + 13, "asleep · leaf · secure", "pl sl")}</g>`);

// ---------- phone ----------
const ph = [];
ph.push(`<g class="an" ${A(dimFor([4]))}>`);
ph.push(rect(PH.x, PH.y, PH.w, PH.h, 16, "pn"));
ph.push(rect(PH.x + PH.w / 2 - 12, PH.y + 8, 24, 4, 2, "notch"));
const scr = { x: PH.x + 5, y: PH.y + 18, w: PH.w - 10, h: PH.h - 30 };
ph.push(rect(scr.x, scr.y, scr.w, scr.h, 8, "scr"));
ph.push(text(PH.x + PH.w / 2, scr.y + 22, "09:41", "s14 mu", 'text-anchor="middle"'));
const CH = 34, CG = 6, cy0 = scr.y + 32;
P.forEach((p, i) => {
  // a card lands on top; each later one pushes it down a slot
  const pts = [[0, { opacity: 0, transform: "translate(0px,0px)" }], [p.at - 0.05, { opacity: 0, transform: "translate(0px,0px)" }], [p.at + 0.35, { opacity: 1, transform: "translate(0px,0px)" }]];
  P.slice(i + 1).forEach((q, k) => {
    const y = (k + 1) * (CH + CG);
    const prevY = k * (CH + CG);
    pts.push([q.at - 0.6, { opacity: 1, transform: `translate(0px,${prevY}px)` }], [q.at - 0.15, { opacity: 1, transform: `translate(0px,${y}px)` }]);
  });
  const lastY = (P.length - 1 - i) * (CH + CG);
  pts.push([s4[1] - 0.2, { opacity: 1, transform: `translate(0px,${lastY}px)` }], [s4[1] + 0.3, { opacity: 0, transform: `translate(0px,${lastY}px)` }], [1e9, { opacity: 0, transform: "translate(0px,0px)" }]);
  const cx = scr.x + 8, cw = scr.w - 16;
  ph.push(`<g class="an hide" ${A(anim(pts))}>${rect(cx, cy0, cw, CH, 6, "card")}<circle cx="${cx + 9}" cy="${cy0 + 11}" r="3.5" fill="var(${p.col})"/>${text(cx + 17, cy0 + 15, p.who, "m13 b")}${text(cx + 8, cy0 + 29, p.what, "s13")}</g>`);
});

ph.push(`</g>`);

// ---------- captions ----------
const caps = [
  ["1. Every box builds.", "Each box runs its own leads and executors through its own runner,", "each in its own git worktree, all at once."],
  ["2. A coordinator keeps leads on every box in step.", "Your coordinator session starts leads on all three boxes and follows their events through the hub;", "it holds web-007 before it lands until app-012 has landed, then releases it."],
  ["3. A lead sends a check to the box that can run it.", "web-007 queues <tspan class=\"m14 am\">mac-build</tspan>, a kind with <tspan class=\"m14\">os = \"darwin\"</tspan>: the job waits on the hub", "until the laptop, the one Mac, takes it, and the result comes back to web-007 as a <tspan class=\"pu\">reply</tspan>."],
  ["4. Push notifications reach your phone.", "When a lead asks, is blocked or is done, its event goes through the hub", "to the daemon with ntfy set, which pushes it to your phone, once."],
  ["5. A change that lands reaches every box.", "web-007 lands, which moves build-box's checkout; you run <tspan class=\"m14\">bmesh sync</tspan>, which levels hub-box and sends each other box a <tspan class=\"m14 am\">level</tspan> job.", "The laptop slept through it: it levels when it wakes, and holds a new plugins commit for your approval."],
];
const capEls = [];
caps.forEach(([h, d1, d2], i) => {
  const [a, b] = S[i];
  capEls.push(`<g class="an hide" ${A(windows([[a + 0.2, b - 0.2]], 0, 1, 0.3, 0.3))}>${text(480, 40, h, "s20 b", 'text-anchor="middle"')}${text(480, 64, d1, "s14", 'text-anchor="middle"')}${text(480, 84, d2, "s14", 'text-anchor="middle"')}</g>`);
});
// the still picture (reduced motion) says what it shows
const stills = [[0, S[0][0] - 0.3], ...S.map(([, b], i) => [b + 0.3, i + 1 < S.length ? S[i + 1][0] - 0.3 : T])];
capEls.push(`<g class="st an" ${A(windows(stills, 0, 1, 0.3, 0.3))}>${text(480, 40, "Three boxes, one NATS hub.", "s20 b", 'text-anchor="middle"')}${text(480, 64, "hub-box runs the hub; build-box dials it; the laptop's leaf relays to it.", "s14", 'text-anchor="middle"')}${text(480, 84, "Events, jobs and replies on every box go through it.", "s14", 'text-anchor="middle"')}</g>`);

// ---------- legend ----------
const lg = [];
const ly = 508;
let lx = 36;
const item = (c, label) => { const s = `<circle cx="${lx}" cy="${ly}" r="5" fill="var(${c})"/>${text(lx + 11, ly + 4.5, label, "s13 mu")}`; lx += 11 + sw(label) + 22; return s; };
const holdItem = () => { const s = `<g transform="translate(${lx},${ly})" class="hd">${MARK.held[1]}</g>${text(lx + 11, ly + 4.5, "hold", "s13 mu")}`; lx += 11 + sw("hold") + 22; return s; };
lg.push(item("--tl", "event"), holdItem(), item("--am", "job"), item("--pu", "reply"));
lg.push(`<line class="ln dash" x1="${lx - 5}" y1="${ly}" x2="${lx + 19}" y2="${ly}"/>`, text(lx + 25, ly + 4.5, "waits", "s13 mu"));

// ---------- progress bar ----------
const pb = [];
const by = 624, gapX = 10, segW = (920 - 4 * gapX) / 5;
const words = ["build", "coordinate", "route a check", "push notifications", "level"];
S.forEach(([a, b], i) => {
  const x = 20 + i * (segW + gapX);
  pb.push(rect(x, by, segW, 3, 1.5, "trk"));
  const fill = anim([[0, { transform: "scaleX(0)" }], [a, { transform: "scaleX(0)" }], [b, { transform: "scaleX(1)" }], [1e9, { transform: "scaleX(1)" }]]);
  pb.push(`<rect x="${x}" y="${by}" width="${segW}" height="3" rx="1.5" class="an bf b0" style="animation-name:${fill};animation-timing-function:linear"/>`);
  pb.push(text(x, by + 20, `${i + 1} ${words[i]}`, "s13 mu"));
  pb.push(`<g class="an hide" ${A(windows([[a + 0.1, b - 0.1]], 0, 1, 0.15))}>${text(x, by + 20, `${i + 1} ${words[i]}`, "s13")}</g>`);
});

for (const h of hops) train(h);
flush();
// 1e9 stands for "the end of the loop"
const cssText = css.join("\n");

const style = `
svg{--fg:#e6edf3;--mu:#8b949e;--pf:#161b22;--ps:#30363d;--ed:#3d4652;--sc:#0d1117;--cp:#1c2129;--wk:#58a6ff;--tl:#39c5bb;--am:#d29922;--pu:#a371f7;--ok:#3fb950;--hd:#8fa7c4;--rd:#f85149}
svg.light{--fg:#1f2328;--mu:#59636e;--pf:#f6f8fa;--ps:#d0d7de;--ed:#c3ccd5;--sc:#ffffff;--cp:#ffffff;--wk:#0969da;--tl:#1a7f72;--am:#9a6700;--pu:#8250df;--ok:#1a7f37;--hd:#57728f;--rd:#cf222e}
text{fill:var(--fg)}
.s13,.s14,.s16,.s20{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Noto Sans",Helvetica,Arial,sans-serif}
.s13{font-size:13px}.s14{font-size:14px}.s16{font-size:16px}.s20{font-size:20px}
.m13,.m14{font-family:ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,"Liberation Mono",monospace}
.m13{font-size:13px}.m14{font-size:14px}.m13,.m14{word-spacing:-0.3em}
.b{font-weight:600}.mu{fill:var(--mu)}.tl{fill:var(--tl)}.am{fill:var(--am)}.pu{fill:var(--pu)}.ok{fill:var(--ok)}.wk{fill:var(--wk)}.hd{fill:var(--hd)}
.pn{fill:var(--pf);stroke:var(--ps);stroke-width:1.5}
.cmp{fill:var(--cp);stroke:var(--ed);stroke-width:1.5}
.nd{fill:none;stroke:var(--ps);stroke-width:1.5}
.ln{stroke:var(--ed);stroke-width:1.5;fill:none}
.dash{stroke-dasharray:4 6;stroke:var(--am)}
.tr,.br{stroke:var(--ps);stroke-width:1.5;fill:none}.br{stroke:var(--mu);stroke-linecap:round}
.ck{fill:none;stroke:var(--ok);stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.pb{fill:none;stroke:var(--ps);stroke-width:1.5}.pt{fill:var(--mu)}
.sl .pb{stroke:var(--hd)}.sl .pt{fill:var(--hd)}
.rg{fill:none;stroke-width:2}
.lc{fill:none;stroke-width:2.5}.lg{fill:none;stroke-width:8;stroke-linecap:round;opacity:.22}
.tl-s{stroke:var(--tl)}.am-s{stroke:var(--am)}.pu-s{stroke:var(--pu)}.hd-s{stroke:var(--hd)}.wk-s{stroke:var(--wk)}
.am-f2{fill:var(--am);opacity:.3}
.scr{fill:var(--sc);stroke:var(--ps);stroke-width:1}.card{fill:var(--pf);stroke:var(--ps);stroke-width:1.5}
.notch{fill:var(--ps)}
.qh{stroke:var(--ed);stroke-width:1.5;stroke-dasharray:4 6;fill:none}svg.light .bf{fill-opacity:.35}
.trk{fill:var(--ps)}.bf{fill:var(--mu);fill-opacity:.6;transform-box:fill-box;transform-origin:left center}.b0{transform:scaleX(0)}
.dot,.an{animation-duration:${T}s;animation-iteration-count:infinite;animation-timing-function:ease-in-out}
.ws{word-spacing:normal}.dot,.hide,.st{opacity:0}.dot circle:first-child{opacity:.3}.dot,.lit{animation-timing-function:linear}.lit{stroke-dasharray:100 100}
.down{stroke-dasharray:4 6}
.tlf{fill:var(--tl)}.amf{fill:var(--am)}.puf{fill:var(--pu)}.hdf{fill:var(--hd)}
.spin{transform-box:fill-box;transform-origin:center;animation:sp .96s steps(8) infinite}
@keyframes sp{to{transform:rotate(360deg)}}
@media (prefers-reduced-motion:reduce){*{animation:none!important}.st{opacity:1}}
${cssText}
`;

const body = [
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${HT}" width="${W}" height="${HT}" class="dark" role="img" aria-label="buildermesh across three boxes, in five scenes: every box builds at once; a coordinator on hub-box follows leads on every box through the NATS hub and holds one before it lands until another has landed; a lead queues a check that needs macOS and only the laptop takes it; leads' asks, blocks and finishes reach your phone as push notifications; after a change lands, bmesh sync levels every box's checkout, and the sleeping laptop levels when it wakes">`,
  `<title>buildermesh across boxes</title>`,
  `<style>${style}</style>`,
  ...capEls, ...bb.slice(0, 2), ...hb.slice(0, 2), ...wires, ...rings, ...bb.slice(2), ...hb.slice(2), ...lpG, ...litW, ...glows, ...parks, ...ph, ...lg, ...pb, ...out,
  `</svg>\n`,
].join("\n");

const dir = process.argv[2];
writeFileSync(join(dir, "mesh-dark.svg"), body);
writeFileSync(join(dir, "mesh-light.svg"), body.replace('class="dark"', 'class="light"'));
console.log(Buffer.byteLength(body), "bytes, loop", T, "s, scenes", JSON.stringify(S));
