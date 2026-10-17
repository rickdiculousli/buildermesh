// Generates the README's mesh pictures into <outdir>, each as -dark.svg and -light.svg:
//   mesh: a demo that plays all five scenes back to back, fast, with no text
//   mesh-<scene>: one scene looped on its own, with a still before it, its
//     opening moment and a beat after each step; the README explains it
// In a scene, what plays no part is dimmed.
// node mesh.mjs <outdir>
import { writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const NAMES = ["build", "coordinate", "route", "push", "level"];
const VARIANT = process.env.MESH_VARIANT ?? "";
if (!VARIANT) {
  // the README's colour key: a dot for each kind of chain, beside the word in the page's own font
  for (const [name, fill] of [["grey", "#8b949e"], ["teal", "#2b9d94"]])
    writeFileSync(join(process.argv[2], `dot-${name}.svg`), `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 12 12" width="12" height="12" role="img" aria-label="${name}"><circle cx="6" cy="6" r="5" fill="${fill}"/></svg>\n`);
  // one run per picture: the demo, then each scene
  for (const v of ["demo", "one", ...NAMES]) execFileSync(process.execPath, [fileURLToPath(import.meta.url), ...process.argv.slice(2)], { env: { ...process.env, MESH_VARIANT: v }, stdio: "inherit" });
  process.exit(0);
}
const DEMO = VARIANT === "demo";
const ONE = VARIANT === "one"; // the whole workflow on a single box: its bmesh serve embeds the hub
const ONLY = DEMO ? 0 : NAMES.indexOf(VARIANT) + 1; // the scene this picture loops; 0 for all
const F = DEMO ? 0.45 : 1;                           // the demo waits less between steps
const PHONE = DEMO || ONE || ONLY === 4;                     // only these show the phone

const W = 960;
const css = [];
const out = [];
let n = 0;
let T = 0;      // the loop's length, set once the timeline is laid out
let ORIGIN = 0; // where on the timeline the loop starts
const pct = (t) => +(((t - ORIGIN) / T) * 100).toFixed(2);

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
// a value part way between two keyframes: numbers (alone or inside a string like
// translate(...)) are mixed; anything else takes the nearer side
const NUM = /-?\d+(?:\.\d+)?/g;
function mix(x, y, f) {
  if (typeof x === "number" && typeof y === "number") return +(x + (y - x) * f).toFixed(3);
  const xs = String(x), ys = String(y), nx = xs.match(NUM), ny = ys.match(NUM);
  if (!nx || !ny || nx.length !== ny.length || xs.replace(NUM, "#") !== ys.replace(NUM, "#")) return f < 0.5 ? x : y;
  let i = 0;
  return xs.replace(NUM, () => { const v = +(+nx[i] + (+ny[i] - +nx[i]) * f).toFixed(2); i++; return String(v); });
}
const lerp = (a, b, f) => Object.fromEntries([...new Set([...Object.keys(a), ...Object.keys(b)])].map((k) => [k, a[k] === undefined ? b[k] : b[k] === undefined ? a[k] : mix(a[k], b[k], f)]));
// the points inside the loop's window, with the values at its two edges worked out
function cut(points) {
  const lo = ORIGIN, hi = ORIGIN + T;
  const pts = [...points].sort((p, q) => p[0] - q[0]);
  const at = (t) => {
    let i = pts.findIndex(([u]) => u > t);
    if (i === -1) return pts[pts.length - 1][1];
    if (i === 0) return pts[0][1];
    const [t0, a] = pts[i - 1], [t1, b] = pts[i];
    return t1 === t0 ? b : lerp(a, b, (t - t0) / (t1 - t0));
  };
  return [[lo, at(lo)], ...pts.filter(([u]) => u > lo && u < hi), [hi, at(hi)]];
}
function flush() {
  const fmt = (o) => Object.entries(o).map(([k, v]) => `${k}:${v}`).join(";");
  for (const [name, points] of pending) {
    // one value per offset (the latest wins), offsets sharing a value in one rule
    const at = new Map();
    for (const [t, o] of cut(points)) at.set(pct(t), fmt(o));
    const by = new Map();
    for (const [p, v] of at) by.set(v, [...(by.get(v) ?? []), `${p}%`]);
    css.push(`@keyframes ${name}{${[...by].map(([v, ps]) => `${ps.join(",")}{${v}}`).join("")}}`);
  }
}
const A = (name) => `style="animation-name:${name}"`;
// opacity windows: list of [tOn, tOff]; fade in before tOn, out after tOff
function windows(list, base = 0, on = 1, fade = 0.25, fadeOut = fade) {
  if (!list.length) return anim([[0, { opacity: base }], [1e9, { opacity: base }]]);
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
    // a window still open when the loop ends stays on; one that starts after the loop is never seen
    if (!(T && b >= ORIGIN + T && a < ORIGIN + T)) pts.push([b + fadeOut, { opacity: base }]);
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
// Each box: a header; its NATS part (the hub, the leaf, or a note that it has
// none); then bmesh, the only thing on a box that talks to NATS, in two halves:
// serve (the daemon, always on) and the commands its sessions run; then the
// sessions and checkouts. A session sends and reads through bmesh's commands
// (the wire on the right); serve acts on the box's sessions and checkouts (the
// wire on the left). A message stops at a part's border, the part lights, and
// the next leaves by another border.
const BOX = { y: 16, w: 292, h: ONE ? 346 : 282 };
const BX = ONE ? { b: -2000, h: 20, l: -2000 } : { b: 20, h: 334, l: 648 }; // one box draws as hub-box does            // build-box, hub-box, laptop
const CMP = { y: BOX.y + 44, h: 56 };                   // the NATS hub / leaf panels
const LINK = BOX.y + 92;                                // the wire between boxes
const BAR = { y: BOX.y + 114, h: 26 };                   // bmesh: serve | cli
const RY = (ONE ? [161, 193, 225, 257, 289, 321] : [161, 193, 225, 257]).map((d) => BOX.y + d);                 // row centres
const RH = 26, BOT = RY[RY.length - 1] + RH / 2;
const SV = (b) => ({ x: BX[b] + 12, w: 132, cx: BX[b] + 78 });    // bmesh serve
const CL = (b) => ({ x: BX[b] + 148, w: 132, cx: BX[b] + 214 });  // bmesh cli: the commands sessions run
const svLane = (b) => BX[b] + 22, clLane = (b) => BX[b] + 270;
const ROWX = (b) => BX[b] + 32, ROWW = 230;
const NH = { x: BX.h + 12, y: CMP.y, w: 268, h: CMP.h };   // NATS hub
const LF = { x: BX.l + 12, y: CMP.y, w: 268, h: CMP.h };   // NATS leaf
const PH = ONE ? { x: BX.h + BOX.w + 64, y: BOX.y + 76, w: 120, h: 196 } : { x: 420, y: BOX.y + BOX.h + 22, w: 120, h: 196 }; // phone: under hub-box, or beside the one box
const NTY = BOX.y + BOX.h + 10;                             // the ntfy wire's level, under the boxes

// wires, as point lists
const half = (P, b) => (b === "b" ? [[P.cx, BAR.y], [P.cx, LINK], [NH.x, LINK]] : [[P.cx, BAR.y], [P.cx, CMP.y + CMP.h]]);
const W_ = {
  svUp: (b) => half(SV(b), b),                   // serve up to its box's NATS (build-box: along the link to the hub)
  clUp: (b) => half(CL(b), b),                   // the commands, likewise
  toCl: (b, i) => [[ROWX(b) + ROWW, RY[i]], [clLane(b), RY[i]], [clLane(b), BAR.y + BAR.h]], // a session runs a bmesh command
  svTo: (b, i) => [[svLane(b), BAR.y + BAR.h], [svLane(b), RY[i]], [ROWX(b), RY[i]]],         // serve acts on a row
  leaf: [[LF.x, LINK], [NH.x + NH.w, LINK]],
  ntfy: ONE
    ? [[svLane("h"), BAR.y + BAR.h], [svLane("h"), NTY], [PH.x - 24, NTY], [PH.x - 24, PH.y + PH.h / 2], [PH.x, PH.y + PH.h / 2]]
    : [[svLane("h"), BAR.y + BAR.h], [svLane("h"), NTY], [PH.x + PH.w / 2, NTY], [PH.x + PH.w / 2, PH.y]],
};
const rev = (p) => [...p].reverse();
const PART = { b: "B", h: "H", l: "L" };
// a session sends through bmesh to the hub; reads from the hub through bmesh; serve takes from / gives to the hub
const up = (b, i) => [W_.toCl(b, i), "cl" + PART[b], W_.clUp(b), ...(b === "l" ? ["leaf", W_.leaf] : []), "hub"];
const down = (b, i) => [...(b === "l" ? [rev(W_.leaf), "leaf"] : []), rev(W_.clUp(b)), "cl" + PART[b], rev(W_.toCl(b, i))];
const svIn = (b) => [...(b === "l" ? [rev(W_.leaf), "leaf"] : []), rev(W_.svUp(b)), "sv" + PART[b]];
const svOut = (b) => [W_.svUp(b), ...(b === "l" ? ["leaf", W_.leaf] : []), "hub"];
// a routine's result: from its row up to its box's serve, then on as serve sends it
const fromRoutine = (b, i) => [rev(W_.svTo(b, i)), "sv" + PART[b], ...svOut(b)];

// ---------- hops ----------
const SPEED = 210;
const SWEEP = 0.7; // the longest wires light end to end in this time
const lenOf = (pts) => pts.slice(1).reduce((a, p, i) => a + Math.hypot(p[0] - pts[i][0], p[1] - pts[i][1]), 0);
const hops = [];
const at = {}; // glow windows per part, by colour: at[part][colour] = [[a, b], ...]
const glowAt = (part, c, w) => (((at[part] ??= {})[c] ??= []).push(w));
function hop(c, pts, t0, speed = SPEED) {
  // short wires flash through; long ones take up to SWEEP
  const dur = +(0.16 + (SWEEP - 0.16) * Math.min(1, lenOf(pts) / 420)).toFixed(3);
  const h = { c, pts, t0, dur, land: t0 + dur, done: t0 + dur + 0.5 };
  hops.push(h);
  return h;
}
// a route: wires and the parts they pass through, e.g. [W_.bRow(0), "hub", rev(W_.hRow(0))].
// A part lights while the message is in it; a part last in the list is where it ends.
// two colours for what a chain carries: teal, a message (an event, a result, a push);
// grey, a command (start, hold, release, a job, sync, a level)
const MSG = "--tl", CMD = "--cm";
const PAUSE = DEMO ? 0.3 : 0.8; // a beat after a message lands before the next one moves
// Within a route the chain flows: a part lights as the pulse reaches it and the
// next wire starts almost at once; every wire and part of the route stays lit
// until the message lands, then they fade together.
const THRU = 0.06;
function route(c, t0, legs) {
  let t = t0, last = null;
  const hs = [], parts = [];
  for (let i = 0; i < legs.length; i++) {
    const l = legs[i];
    if (typeof l === "string") { parts.push([l, last.land - 0.08, i === legs.length - 1]); t = last.land + THRU; continue; }
    last = hop(c, l, t);
    hs.push(last);
  }
  const end = last.land;
  for (const h of hs) h.done = end + 0.5;
  for (const [p, a, isEnd] of parts) glowAt(p, c, [a, isEnd ? end + 0.8 : end + 0.5]);
  return { hops: hs, t0, land: end, done: end + 0.5, first: hs[0], last };
}
const along = (pts, t0, t1) => {
  const len = pts.slice(1).map((p, i) => Math.hypot(p[0] - pts[i][0], p[1] - pts[i][1]));
  const tot = len.reduce((a, b) => a + b, 0);
  let acc = 0;
  return pts.map((p, i) => { if (i) acc += len[i - 1]; return [+(t0 + ((t1 - t0) * acc) / tot).toFixed(3), ...p]; });
};

// ---------- the timeline ----------
const R = DEMO ? 0.5 : 1.6;    // the still moment after each scene
const HEAD = DEMO ? 0 : 0.8;   // a scene opens on its starting state for a moment before anything moves
const TAIL = DEMO ? 0.5 : 1.8; // and ends this long after its last step
const S = [];                  // scenes [start, end]
let t = 0.8;
// open a scene: s.go is when its first step starts; close it after its last step
const open = () => { const s = [t, 0]; s.go = t + HEAD; S.push(s); return s; };
const close = (s, ...extra) => {
  const last = Math.max(...hops.filter((h) => h.t0 >= s[0]).map((h) => h.done), ...extra);
  s[1] = last + TAIL;
  t = s[1] + R;
};

// 1. every box builds: the leads start box after box, quickly, and all run on
const s1 = open();
const run1 = (i, d = 0) => [s1.go + i * 0.35 + d, 0];
const RUN1 = { b: run1(0), bx: run1(0, 0.18), h: run1(1), hx: run1(1, 0.18), l: run1(2) };
close(s1, s1.go + 0.7 + (DEMO ? 1.6 : 2.4));
for (const w of Object.values(RUN1)) w[1] = s1[1] - 0.3;

// 2. a coordinator keeps leads on every box in step: it starts a lead on each box
// in turn, overlapping; then it listens, and works only when it acts
const s2 = open();
const C = {};
const ORDER = ["b", "h", "l"];
C.d = ORDER.map((_, i) => s2.go + i * 0.7);                    // it starts a lead on each box in turn
C.go = C.d.map((d) => d + 0.5);                                // that box's lead starts
C.hold = route(CMD, C.go[2] + 1.6 * F, [...up("h", 0), ...down("b", 0)]);
C.gate = C.hold.land + 0.15;                                   // a now hold: task-1 stops at its next inbox read
C.hLand = C.gate + 1.8 * F;                                    // task-3, on the laptop, lands
C.e4 = route(MSG, C.hLand + 0.5 * F, [...up("l", 0), ...down("h", 0)]);
// the release: through build-box's serve, which wakes task-1
C.rel = route(CMD, C.e4.land + PAUSE + 0.2 * F, [...up("h", 0), ...svIn("b"), W_.svTo("b", 0)]);
C.wake = C.rel.land;
close(s2, C.wake + 2.0 * F);

// 3. a lead sends a check to the box that can run it
const s3 = open();
const Q = {};
Q.q = route(CMD, s3.go, up("b", 0));
Q.park = [Q.q.land, Q.q.land + 1.6 * F];                       // waits on the hub for a Mac
Q.take = route(CMD, Q.park[1], [...svIn("l"), W_.svTo("l", 1)]); // the laptop's serve takes it and starts the routine
Q.run = [Q.take.land, Q.take.land + 2.0 * F];
Q.rep = route(MSG, Q.run[1] + 0.3, [...fromRoutine("l", 1), ...down("b", 0)]); // the result: task-1 acts on it
close(s3);

// 4. push notifications: one chain from the lead through hub-box's serve to the phone
const s4 = open();
const P = [];
const push = (b, i, t0, who, what, col) => {
  const a = route(MSG, t0, [...up(b, i), ...svIn("h"), W_.ntfy]);
  P.push({ a, who, what, col, at: a.land });
};
const GAP4 = DEMO ? 2.6 : 4.2;
push("l", 0, s4.go, "task-3", "is done", "--ok");
push("b", 0, s4.go + GAP4, "task-1", "asks", "--am");
push("h", 1, s4.go + 2 * GAP4, "task-2", "is blocked", "--rd");
close(s4, P[2].at + (DEMO ? 0.8 : 1.6));

// 5. a landed change reaches every box; sync sends its level jobs and doesn't wait on a box that sleeps
const s5 = open();
const L = {};
const SETTLE = DEMO ? 0.4 : 1.6; // in this scene, a longer pause after each chain lands, to take in what changed
L.sleep = [ONLY === 5 ? s5[0] - 1 : s5[0] + 0.3, 0]; // its own picture opens with the laptop already asleep
L.land = s5.go + (DEMO ? 0.3 : 0);                             // task-1 lands; its checkout moves
L.e = route(MSG, L.land + SETTLE, [...up("b", 0), ...down("h", 0)]);
L.sync0 = L.e.land + SETTLE;                                   // the coordinator runs bmesh sync:
// one chain from the coordinator through bmesh cli to the hub, where the laptop's job waits ...
// (sync asks build-box too, but the box that landed is already level: that no-op isn't drawn,
// so the change is seen spreading out from where it landed)
L.s = route(CMD, L.sync0, [W_.toCl("h", 0), "clH", W_.clUp("h"), "hub"]);
// ... with a branch from bmesh cli to hub-box's own checkout, which it levels as it sends
L.selfh = hop(CMD, rev(W_.toCl("h", 3)), L.s.hops[1].t0);
L.selfh.done = L.s.done;
L.self = L.selfh.land;
L.park = L.s.hops[1].land;                                     // the laptop's job waits on the hub
L.up = L.s.land;
L.wake = L.up + SETTLE + 0.6 * F;
L.sleep[1] = L.wake;
L.take = route(CMD, L.wake + SETTLE, [...svIn("l"), W_.svTo("l", 2)]);
L.ff = L.take.land;
// on wake its serve also levels itself, every repo: plugins moves at the same moment, and is held
L.hp = hop(CMD, W_.svTo("l", 3), L.take.last.t0);
L.hp.done = L.take.done;
L.held = L.hp.land;
L.sync = [L.sync0, L.park + 0.5];
close(s5, L.held + 3.5 * F);

// when the laptop sleeps; in the levelling scene's own picture it dozes off again at the end, so the loop is seamless
const SLEEPS = DEMO ? [] : ONLY === 5 ? [L.sleep, [s5[1] - 0.2, s5[1] + 5]] : [L.sleep];
// the loop: the demo runs every scene; a scene's picture runs a short still, that scene, and a short still
if (ONLY === 1) { ORIGIN = s1.go + 1.5; T = 3; } // every box simply running: the loop sits inside the scene, after the starts
else if (ONLY) { const [a, b] = S[ONLY - 1]; ORIGIN = a; T = +(b + 0.5 - ORIGIN).toFixed(2); } // from the scene's opening state to just after its last fade
else T = +t.toFixed(2); // the last still runs into the first scene
// every message is home, and has faded, before its scene ends
for (const h of hops) {
  const sc = S.find(([a, b]) => h.t0 >= a && h.t0 < b);
  if (!sc || h.done > sc[1] - 0.3) throw new Error(`a hop at ${h.t0.toFixed(1)}s overruns its scene ${JSON.stringify(sc)}`);
}

// ---------- the demo's story ----------
// The demo isn't the scenes in a row: it's one workflow, end to end, that touches each of them once.
// The coordinator starts a lead on every box; task-1's xcode check goes to the Mac and back; task-2
// asks (a push); the coordinator holds task-1 until task-3 lands (reported to the coordinator and the
// phone); it releases task-1, which lands; bmesh sync carries the change to hub-box and the laptop.
// Then it all settles and starts again.
let ST = null, OB = null;
if (ONE) {
  hops.length = 0;
  for (const k of Object.keys(at)) delete at[k];
  OB = {};
  const fork = (c, t0, trunk, ...branches) => {
    const a = route(c, t0, trunk);
    const bs = branches.map((legs) => route(c, a.land + THRU, legs));
    const end = Math.max(...bs.map((b) => b.land));
    for (const h of [...a.hops, ...bs.flatMap((b) => b.hops)]) h.done = end + 0.5;
    glowAt("hub", c, [a.land - 0.08, end + 0.5]);
    return { a, bs, land: end, done: end + 0.5, t0 };
  };
  const BEAT = 1.2; // on one box every wire is short and close together: a longer beat keeps messages apart
  const LONG = 2.2; // and a longer one still between the story's steps
  const POST = 2.6; // and after a lead lands and the phone hears it, before the next action
  // a lead lands: the box's checkout quietly takes the new commit; then its done goes to the phone
  // (the coordinator hears it too, but on one box that branch is left out so the push reads clearly)
  const land = (i, t) => {
    const e = route(MSG, t + 0.7, [...up("h", i), ...svIn("h"), W_.ntfy]);
    return { at: t, e };
  };
  OB.d = [0.8, 1.5];                                             // two leads: the box's cap
  OB.go = OB.d.map((d) => d + 0.45);
  OB.try3 = OB.go[1] + 0.7;                                      // it tries task-3 too: refused, the box is at its cap
  const toRoutine = [rev(W_.svUp("h")), "svH", W_.svTo("h", 4)];              // the hub to this box's serve, which runs it
  // task-1 queues xcode; the routine is free, so it runs straight away: one chain to it
  OB.take = route(CMD, OB.go[1] + LONG, [...up("h", 1), ...toRoutine]);
  OB.q = OB.take;
  // task-2 queues xcode too; the routine has one instance and it's busy, so the job waits on the hub
  OB.q2 = route(CMD, OB.take.land + LONG, up("h", 2));
  OB.rep = route(MSG, OB.q2.land + 1.5 * BEAT, [...fromRoutine("h", 4), ...down("h", 1)]); // task-1's result
  OB.run = [OB.take.land, OB.rep.t0 - 0.1];
  OB.take2 = route(CMD, OB.rep.land + BEAT, toRoutine);                          // the routine is free: task-2's job runs
  OB.rep2 = route(MSG, OB.take2.land + 1.2 * BEAT, [...fromRoutine("h", 4), ...down("h", 2)]); // task-2's result
  OB.run2 = [OB.take2.land, OB.rep2.t0 - 0.1];
  OB.l2 = land(2, OB.rep2.land + BEAT);                                          // task-2 lands first
  OB.d3 = OB.l2.e.land + POST;                                                   // a slot is free: start task-3
  OB.go3 = OB.d3 + 0.45;
  // task-3 is quick but depends on task-1: the coordinator holds it until task-1 lands
  OB.hold = route(CMD, OB.go3 + LONG, [...up("h", 0), ...down("h", 3)]);
  OB.gate = OB.hold.land + 0.15;
  OB.l1 = land(1, OB.gate + BEAT);                                               // task-1, working all along, lands
  OB.rel = route(CMD, OB.l1.e.land + POST, [...up("h", 0), ...svIn("h"), W_.svTo("h", 3)]); // release; serve wakes task-3
  OB.wake = OB.rel.land;
  OB.l3 = land(3, OB.wake + BEAT);                                               // task-3 lands
  OB.end = OB.l3.e.land + POST;
  OB.cards = [
    { who: "task-2", what: "is done", col: "--ok", at: OB.l2.e.land },
    { who: "task-1", what: "is done", col: "--ok", at: OB.l1.e.land },
    { who: "task-3", what: "is done", col: "--ok", at: OB.l3.e.land },
  ];
  ORIGIN = 0;
  T = +(OB.end + 0.7).toFixed(2);
}
if (DEMO) {
  hops.length = 0;
  for (const k of Object.keys(at)) delete at[k];
  ST = {};
  const fork = (c, t0, trunk, ...branches) => {
    // one chain to the hub, then on to several places at once; all of it stays lit to the last arrival
    const a = route(c, t0, trunk);
    const bs = branches.map((legs) => route(c, a.land + THRU, legs));
    const end = Math.max(...bs.map((b) => b.land));
    for (const h of [...a.hops, ...bs.flatMap((b) => b.hops)]) h.done = end + 0.5;
    glowAt("hub", c, [a.land - 0.08, end + 0.5]);
    return { a, bs, land: end, done: end + 0.5, t0 };
  };
  const BEAT = 0.8; // every message starts at least this long after the one before it lands
  const POST = 1.6; // and after a lead lands and its report reaches the coordinator and the phone
  ST.d = ["b", "h", "l"].map((_, i) => 0.8 + i * 0.6);          // the coordinator starts a lead on each box
  ST.go = ST.d.map((d) => d + 0.45);
  ST.q = route(CMD, ST.go[2] + 1.0, up("b", 0));                 // task-1 queues xcode
  ST.park = [ST.q.land, ST.q.land + 0.9];                        // it waits for a Mac
  ST.take = route(CMD, ST.park[1], [...svIn("l"), W_.svTo("l", 1)]);
  ST.run = [ST.take.land, 0]; // runs until just before its result goes back
  ST.ask = route(MSG, ST.take.land + BEAT, [...up("h", 1), ...svIn("h"), W_.ntfy]); // task-2 asks: to the phone
  ST.rep = route(MSG, Math.max(ST.run[1], ST.ask.land) + BEAT, [...fromRoutine("l", 1), ...down("b", 0)]);          // the result, to task-1
  ST.run[1] = ST.rep.t0 - 0.1;
  ST.hold = route(CMD, ST.rep.land + BEAT, [...up("h", 0), ...down("b", 0)]);       // hold task-1 until task-3 lands
  ST.gate = ST.hold.land + 0.15;
  ST.l3 = ST.gate + BEAT;                                                           // task-3 lands
  ST.e3 = fork(MSG, ST.l3 + 0.5, up("l", 0), down("h", 0), [...svIn("h"), W_.ntfy]);
  ST.rel = route(CMD, ST.e3.land + POST, [...up("h", 0), ...svIn("b"), W_.svTo("b", 0)]); // release; serve wakes task-1
  ST.wake = ST.rel.land;
  ST.l1 = ST.wake + 0.9;                                                           // task-1 lands; its checkout moves
  ST.e1 = fork(MSG, ST.l1 + 0.5, up("b", 0), down("h", 0), [...svIn("h"), W_.ntfy]);
  ST.l2 = ST.e1.land + POST;                                                        // task-2 is done too
  ST.e2 = fork(MSG, ST.l2 + 0.5, up("h", 1), down("h", 0), [...svIn("h"), W_.ntfy]);
  ST.sync0 = ST.e2.land + POST;                                                   // the coordinator runs bmesh sync
  // each land moved its own box's checkout; hub-box, where task-2 landed last, is already level,
  // so sync's level jobs go on from the hub to build-box and the laptop at once
  ST.s = fork(CMD, ST.sync0, [W_.toCl("h", 0), "clH", W_.clUp("h"), "hub"], [...svIn("b"), W_.svTo("b", 3)], [...svIn("l"), W_.svTo("l", 2)]);
  ST.end = ST.s.land + 2.2;                                                        // the finished state holds, then fades
  ST.cards = [
    { who: "task-2", what: "asks", col: "--am", at: ST.ask.land },
    { who: "task-3", what: "is done", col: "--ok", at: ST.e3.bs[1].land },
    { who: "task-1", what: "is done", col: "--ok", at: ST.e1.bs[1].land },
    { who: "task-2", what: "is done", col: "--ok", at: ST.e2.bs[1].land },
  ];
  ORIGIN = 0;
  T = +(ST.end + 0.7).toFixed(2);
}

// ---------- dimming ----------
// A part plays in some scenes; in the others it dims while the scene runs.
const DIM = 0.28;
function dimFor(plays) {
  if (DEMO || ONE) return anim([[0, { opacity: 1 }], [1e9, { opacity: 1 }]]); // in the demo's one workflow, everything takes part
  // a scene's own picture opens ready: what plays no part is dimmed from the first frame and stays so
  if (ONLY) { const o = plays.includes(ONLY) ? 1 : DIM; return anim([[0, { opacity: o }], [1e9, { opacity: o }]]); }
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
const pulse = (r, c, t) => ring(r.x, r.y, r.w, r.h, 6, c, [[t, t + 0.2]], 0.12, 0.35);
function mkRow(b, i, path, role, opt = {}) {
  const x = ROWX(b) + (opt.indent ?? 0), w = ROWW - (opt.indent ?? 0), y = RY[i] - RH / 2;
  const r = { x, y, w, h: RH, cy: RY[i], role, xe: x + w - 12, size: opt.size ?? 13 };
  r.ix = r.xe - sw(role) - 11; // where a state mark sits, left of the role
  if (!role) r.ix = r.xe - 10;
  r.svg = `${rect(x, y, w, RH, 6, "nd")}<circle cx="${x + 14}" cy="${RY[i]}" r="4.5" fill="var(--mu)"/>${text(x + 27, RY[i] + r.size * 0.35, esc(path), `m${r.size}`)}${role ? text(r.xe, RY[i] + 4.5, role, "s13 mu", 'text-anchor="end"') : ""}`;
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
  // listening: a dot with arcs either side, the outer ones pulsing slowly
  listen: ["tl", `<circle r="2.2"/><path class="arc" d="M-5 -3.5 A5 5 0 0 0 -5 3.5 M5 -3.5 A5 5 0 0 1 5 3.5"/><path class="arc a2" d="M-8.5 -6 A9 9 0 0 0 -8.5 6 M8.5 -6 A9 9 0 0 1 8.5 6"/>`],
  held: ["hd", `<rect x="-3.5" y="-4.5" width="2.5" height="9" rx="1"/><rect x="1" y="-4.5" width="2.5" height="9" rx="1"/>`],
  done: ["ok", `<path d="M-4.5 0.5 L-1.5 3.5 L4.5 -3.5" class="ck"/>`],
  // held: the spinner stays where it was, stopped and grey
  frozen: ["mu", SPIN.replace('class="spin"', "")],
  // the coordinator sending a hold, or a release
  sendHold: ["cm", `<rect x="-3.5" y="-4.5" width="2.5" height="9" rx="1"/><rect x="1" y="-4.5" width="2.5" height="9" rx="1"/>`],
  sendGo: ["cm", `<path d="M-3 -5 L4.5 0 L-3 5 Z"/>`],
};
// and the state dot takes the mark's colour
function mark(r, kind, ws) {
  const [c, g] = MARK[kind];
  r.marks.push(`<g class="an hide" ${A(windows(ws, 0, 1, 0.2, 0.3))}><circle cx="${r.x + 14}" cy="${r.cy}" r="4.5" fill="var(--${c})"/><g transform="translate(${r.ix.toFixed(1)},${r.cy})" class="${c}">${g}</g>${r.role ? text(r.xe, r.cy + 4.5, r.role, `s13 ${c}`, 'text-anchor="end"') : ""}</g>`);
}
// the gaps between a node's working windows, within [a, b]
const between = (work, a, b) => {
  const out = [];
  let t = a;
  for (const [x, y] of [...work].sort((p, q) => p[0] - q[0])) { if (x - 0.3 > t + 1.0) out.push([t, x - 0.3]); t = Math.max(t, y + 0.3); }
  if (b > t + 1.0) out.push([t, b]);
  return out;
};
// working windows with gaps too short to read as listening closed up
const closed = (work) => {
  const m = [];
  for (const w of [...work].sort((p, q) => p[0] - q[0])) { const p = m[m.length - 1]; if (p && w[0] - p[1] < 1.6) p[1] = Math.max(p[1], w[1]); else m.push([...w]); }
  return m;
};
// the state dot in another colour
const dotAs = (r, c, ws) => r.marks.push(`<circle class="an hide" ${A(windows(ws, 0, 1, 0.2))} cx="${r.x + 14}" cy="${r.cy}" r="4.5" fill="var(${c})"/>`);
const row = (r) => r.svg + r.marks.join("");
// a small grey tag in a row, naming the command that just arrived
function tag(r, label, t0, dur) {
  const w = Math.round(label.length * 6.6 + 16), x = r.x + 96;
  glows.push(`<g class="an hide" ${A(windows([[t0, t0 + dur]], 0, 1, 0.15, 0.3))}>${rect(x, r.cy - 9, w, 18, 9, "tg")}${text(x + w / 2, r.cy + 4.3, label, "s13 cm", 'text-anchor="middle"')}</g>`);
}

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
// bmesh, in two halves: serve (always on, its dot stays green) and the commands sessions run
const serveOf = (b, plays) => part(plays, `${rect(SV(b).x, BAR.y, SV(b).w, BAR.h, 8, "cmp")}${text(SV(b).x + 24, BAR.y + 17.5, "bmesh serve", "m13")}`) + `<circle cx="${SV(b).x + 13}" cy="${BAR.y + 13}" r="4" fill="var(--ok)"/>`;
const cmdsOf = (b, plays) => part(plays, `${rect(CL(b).x, BAR.y, CL(b).w, BAR.h, 8, "cmp")}${text(CL(b).x + CL(b).w / 2, BAR.y + 17.5, "bmesh cli", "m13", 'text-anchor="middle"')}`);
const cmp = (P, name, sub) => [rect(P.x, P.y, P.w, P.h, 8, "cmp"), text(P.x + 14, P.y + 21, name, "s16 b"), text(P.x + 14, P.y + 40, sub, "s13 mu")].join("");

// build-box: a client of the hub
const bL = mkRow("b", 0, "task-1", "lead", { size: 14 });
const bX = mkRow("b", 1, "task-1.a", "executor", { indent: 18 });
const bV = mkRow("b", 2, "routine.verify", "routine");
const bC = mkCheckout("b", 3, "app", "3f2a9c1e");
// hub-box: runs the hub
const hO = mkRow("h", 0, "coordinator", "", { size: 14 });
const hL = mkRow("h", 1, "task-2", "lead", { size: 14 });
const hX = mkRow("h", 2, "task-2.a", "executor", { indent: 18 });
const hC = mkCheckout("h", 3, "app", "3f2a9c1e");
// the laptop: a leaf, secure, the one Mac
const lL = mkRow("l", 0, "task-3", "lead", { size: 14 });
const lM = mkRow("l", 1, "routine.xcode", "routine");
const lC = mkCheckout("l", 2, "app", "3f2a9c1e");
const lP = mkCheckout("l", 3, "plugins", "5c1e0a92");
// the one box: the coordinator, three leads, the xcode routine and the checkout
const O = ONE ? {
  co: mkRow("h", 0, "coordinator", "", { size: 14 }),
  t1: mkRow("h", 1, "task-1", "lead", { size: 14 }),
  t2: mkRow("h", 2, "task-2", "lead", { size: 14 }),
  t3: mkRow("h", 3, "task-3", "lead", { size: 14 }),
  rx: mkRow("h", 4, "routine.xcode", "routine"),
  ck: mkCheckout("h", 5, "app", "3f2a9c1e"),
} : null;

if (ONE) {
  const E = OB.end - 0.4, { co, t1, t2, t3, rx, ck } = O;
  const sent = (r, c, rt) => rowGlow(r, c, [[rt.t0, rt.done]]); // the sender stays lit until its chain has connected
  const got = (r, c, rt) => rowGlow(r, c, [[rt.land, rt.done + 0.3]]);
  // the coordinator: starts two leads; starts the third when a slot frees; holds it; releases it; listens in between
  const work = [[OB.d[0], OB.try3 + 0.3], [OB.d3, OB.go3 + 0.3]];
  const sendHold = [OB.hold.t0 - 0.5, OB.hold.land], sendGo = [OB.rel.t0 - 0.5, OB.rel.land];
  mark(co, "run", work); mark(co, "sendHold", [sendHold]); mark(co, "sendGo", [sendGo]);
  mark(co, "listen", between(closed([...work, sendHold, sendGo]), OB.try3 + 0.3, E));
  rowGlow(co, "cm", [[OB.d[0], OB.try3 + 0.2], [OB.d3, OB.go3 + 0.2]]); // lit while it starts leads
  pulse(t3, "am", OB.try3); // the third start is refused: two leads already working on this box
  pulse(t1, "cm", OB.go[0]); pulse(t2, "cm", OB.go[1]); pulse(t3, "cm", OB.go3);
  // task-1: queues xcode, gets its result, keeps working, lands
  mark(t1, "run", [[OB.go[0], OB.l1.at - 0.2]]); mark(t1, "done", [[OB.l1.at, E]]);
  sent(t1, "cm", OB.q); got(t1, "tl", OB.rep);
  // task-2: queues xcode, is told to wait while the routine is busy, runs once it's free, lands first
  mark(t2, "run", [[OB.go[1], OB.q2.land - 0.05], [OB.take2.t0, OB.l2.at - 0.2]]); mark(t2, "frozen", [[OB.q2.land, OB.take2.t0 - 0.05]]); mark(t2, "done", [[OB.l2.at, E]]);
  tag(t2, "wait", OB.q2.land, OB.take2.t0 - OB.q2.land);
  sent(t2, "cm", OB.q2); got(t2, "tl", OB.rep2);
  // task-3: waits for a slot (the box's lead cap is 2); starts; is held until task-1 lands; released, lands
  tag(t3, "max 2 leads", OB.try3 + 0.5, OB.d3 - OB.try3 - 0.5);
  mark(t3, "run", [[OB.go3, OB.gate - 0.05], [OB.wake, OB.l3.at - 0.2]]); mark(t3, "frozen", [[OB.gate, OB.wake - 0.05]]); mark(t3, "done", [[OB.l3.at, E]]);
  glows.push(`<rect x="${t3.x + t3.w - 5}" y="${t3.y + 5}" width="3" height="${RH - 10}" rx="1.5" class="cm an hide" ${A(windows([[OB.gate, OB.wake - 0.1]], 0, 1, 0.15, 0.25))}/>`);
  tag(t3, "hold", OB.hold.land, 1.6); tag(t3, "release", OB.rel.land, 1.6);
  sent(co, "cm", OB.hold); got(t3, "cm", OB.hold); sent(co, "cm", OB.rel); got(t3, "cm", OB.rel);
  // the xcode routine, run by this box's serve
  mark(rx, "run", [OB.run, OB.run2]); got(rx, "cm", OB.take); sent(rx, "tl", OB.rep); got(rx, "cm", OB.take2); sent(rx, "tl", OB.rep2);
  // each land moves the checkout through bmesh, then is reported
  for (const [r, l] of [[t2, OB.l2], [t1, OB.l1], [t3, OB.l3]]) sent(r, "tl", l.e);
  states(ck, [
    [OB.l2.at, 0, `${st("+1", "ok")}${ck.main("9b4e2c71", "ok")}`],
    [OB.l1.at, 0, `${st("+2", "ok")}${ck.main("c05a7d3e", "ok")}`],
    [OB.l3.at, 0, `${st("+3", "ok")}${ck.main("81d0b7a4", "ok")}`],
  ], E);
} else if (!DEMO) {
// scene 1: every box builds, each on its own
mark(hL, "run", [RUN1.h]); mark(hX, "run", [RUN1.hx]);
mark(bL, "run", [RUN1.b]); mark(bX, "run", [RUN1.bx]);
mark(lL, "run", [RUN1.l]);
pulse(hL, "cm", RUN1.h[0]); pulse(bL, "cm", RUN1.b[0]); pulse(lL, "cm", RUN1.l[0]);

// scene 2: the coordinator works when it acts and listens in between
C.sendHold = [C.hold.t0 - 0.5, C.hold.land];
C.sendGo = [C.e4.land + 0.2, C.rel.land];
mark(hO, "run", [[C.d[0], C.go[2] + 0.3]]);
mark(hO, "sendHold", [C.sendHold]);
mark(hO, "sendGo", [C.sendGo]);
mark(hO, "listen", between(closed([[C.d[0], C.go[2] + 0.3], C.sendHold, C.sendGo]), C.d[0], s2[1] - 0.3));
const leadOf = { b: bL, h: hL, l: lL };
// the coordinator stays lit while it starts the leads; each lead flashes as it starts
rowGlow(hO, "cm", [[C.d[0], C.go[2] + 0.2]]);
ORDER.forEach((b, i) => pulse(leadOf[b], "cm", C.go[i]));
mark(hL, "run", [[C.go[1], s2[1] - 0.3]]);
mark(bL, "run", [[C.go[0], C.gate - 0.05], [C.wake, s2[1] - 0.3]]); mark(bL, "frozen", [[C.gate, C.wake - 0.05]]);
// where it stopped: a bar at the row's edge while it's held
glows.push(`<rect x="${bL.x + bL.w - 5}" y="${bL.y + 5}" width="3" height="${RH - 10}" rx="1.5" class="cm an hide" ${A(windows([[C.gate, C.wake - 0.1]], 0, 1, 0.15, 0.25))}/>`);
// what arrived: a tag beside the row for a moment
tag(bL, "hold", C.hold.land, 1.8); tag(bL, "release", C.rel.land, 1.8);
mark(lL, "run", [[C.go[2], C.hLand - 0.2]]); mark(lL, "done", [[C.hLand, s2[1] - 0.2]]);
const sent = (r, c, rt) => rowGlow(r, c, [[rt.t0, rt.done]]); // the sender stays lit until its chain has connected
const got = (r, c, rt) => rowGlow(r, c, [[rt.land, rt.done + 0.3]]);
sent(hO, "cm", C.hold); got(bL, "cm", C.hold);
sent(lL, "tl", C.e4); got(hO, "tl", C.e4);
sent(hO, "cm", C.rel); got(bL, "cm", C.rel);

// scene 3: a check that needs a Mac
sent(bL, "cm", Q.q);
mark(bL, "run", [[s3[0] + 0.2, s3[1] - 0.3]]);
mark(lM, "run", [Q.run]);
got(lM, "cm", Q.take);
sent(lM, "tl", Q.rep); got(bL, "tl", Q.rep);

// scene 4: pushes
const pstate = [[lL, "--ok"], [bL, "--am"], [hL, "--rd"]];
P.forEach((p, i) => { sent(pstate[i][0], "tl", p.a); dotAs(pstate[i][0], pstate[i][1], [[p.a.t0, s4[1] - 0.2]]); });

// scene 5: levelling
const e5 = s5[1] - 0.2;
mark(bL, "done", [[L.land, e5]]);
rowGlow(bC, "cm", [[L.land + 0.2, L.land + 1.1]]);
sent(bL, "tl", L.e); got(hO, "tl", L.e);
mark(hO, "run", [L.sync]);
mark(hO, "listen", between([L.sync], s5[0] + 0.4, s5[1] - 0.3));
rowGlow(hC, "cm", [[L.self, L.s.done + 0.3]]);
sent(hO, "cm", L.s);
got(lC, "cm", L.take);
rowGlow(lP, "cm", [[L.held, L.held + 0.9]]);

const NEW = "81d0b7a4";
states(bC, [[L.land + 0.2, 0, `${st("+3", "ok")}${bC.main(NEW, "ok")}`]], e5);
states(hC, [[L.self, 0, `${st("+3", "ok")}${hC.main(NEW, "ok")}`]], e5);
states(lC, [[L.ff, 0, `${st("+3", "ok")}${lC.main(NEW, "ok")}`]], e5);
lP.marks.length = 0;
lP.marks.push(`<g class="an" ${A(windows([[L.held, e5 + 0.35]], 1, 0, 0.2, 0.3))}>${lP.at(lP.main(lP.base))}</g>`);
lP.marks.push(`<g class="an hide" ${A(windows([[L.held + 0.25, e5]], 0, 1, 0.2, 0.3))}><g transform="translate(${(lP.xe - sw("held +2") - 10).toFixed(1)},${lP.cy})" class="hd">${MARK.held[1]}</g>${text(lP.xe, lP.cy + 4.5, "held +2", "s13 hd", 'text-anchor="end"')}</g>`);
} else // the demo's marks
{
  const E = ST.end - 0.4, NEW = "81d0b7a4";
  const sent = (r, c, rt) => rowGlow(r, c, [[rt.t0, rt.done]]); // the sender stays lit until its chain has connected
  const got = (r, c, rt) => rowGlow(r, c, [[rt.land, rt.done + 0.3]]);
  // the coordinator: starts the leads, holds, releases, syncs; listens in between
  const work = [[ST.d[0], ST.go[2] + 0.3], [ST.sync0, ST.s.a.land + 0.4]];
  const sendHold = [ST.hold.t0 - 0.5, ST.hold.land], sendGo = [ST.e3.land + 0.2, ST.rel.land];
  mark(hO, "run", work); mark(hO, "sendHold", [sendHold]); mark(hO, "sendGo", [sendGo]);
  mark(hO, "listen", between(closed([...work, sendHold, sendGo]), ST.go[2] + 0.3, E));
  const leadOf = { b: bL, h: hL, l: lL };
  rowGlow(hO, "cm", [[ST.d[0], ST.go[2] + 0.2]]); // lit while it starts the leads
  ["b", "h", "l"].forEach((b, i) => pulse(leadOf[b], "cm", ST.go[i]));
  // task-1: runs, queues xcode, gets its result, is held, released, lands
  mark(bL, "run", [[ST.go[0], ST.gate - 0.05], [ST.wake, ST.l1 - 0.2]]); mark(bL, "frozen", [[ST.gate, ST.wake - 0.05]]); mark(bL, "done", [[ST.l1, E]]);
  mark(bX, "run", [[ST.go[0] + 0.2, ST.l1 - 0.2]]);
  glows.push(`<rect x="${bL.x + bL.w - 5}" y="${bL.y + 5}" width="3" height="${RH - 10}" rx="1.5" class="cm an hide" ${A(windows([[ST.gate, ST.wake - 0.1]], 0, 1, 0.15, 0.25))}/>`);
  tag(bL, "hold", ST.hold.land, 1.6); tag(bL, "release", ST.rel.land, 1.6);
  sent(bL, "cm", ST.q); got(bL, "tl", ST.rep); sent(hO, "cm", ST.hold); got(bL, "cm", ST.hold); sent(hO, "cm", ST.rel); got(bL, "cm", ST.rel);
  // task-2 runs on and asks; task-3 lands
  mark(hL, "run", [[ST.go[1], ST.l2 - 0.2]]); mark(hL, "done", [[ST.l2, E]]); mark(hX, "run", [[ST.go[1] + 0.2, ST.l2 - 0.2]]);
  dotAs(hL, "--am", [[ST.ask.t0, ST.l2 - 0.2]]); sent(hL, "tl", ST.ask); sent(hL, "tl", ST.e2.a); got(hO, "tl", ST.e2.bs[0]);
  mark(lL, "run", [[ST.go[2], ST.l3 - 0.2]]); mark(lL, "done", [[ST.l3, E]]); sent(lL, "tl", ST.e3.a); got(hO, "tl", ST.e3.bs[0]);
  // the xcode routine on the laptop
  mark(lM, "run", [ST.run]); got(lM, "cm", ST.take); sent(lM, "tl", ST.rep);
  // task-1's land, reported; then sync carries it to hub-box and the laptop
  sent(bL, "tl", ST.e1.a); got(hO, "tl", ST.e1.bs[0]); sent(hO, "cm", ST.s);
  // a lead's land moves its own box's checkout: task-3 the laptop's, task-1 build-box's, task-2 hub-box's
  rowGlow(lC, "cm", [[ST.l3 + 0.2, ST.l3 + 1.1]]); rowGlow(bC, "cm", [[ST.l1 + 0.2, ST.l1 + 1.1]]); rowGlow(hC, "cm", [[ST.l2 + 0.2, ST.l2 + 1.1]]);
  got(bC, "cm", ST.s.bs[0]); got(lC, "cm", ST.s.bs[1]);
  states(lC, [[ST.l3 + 0.2, 0, `${st("+1", "ok")}${lC.main("9b4e2c71", "ok")}`], [ST.s.bs[1].land, 0, `${st("+2", "ok")}${lC.main(NEW, "ok")}`]], E);
  states(bC, [[ST.l1 + 0.2, 0, `${st("+2", "ok")}${bC.main("c05a7d3e", "ok")}`], [ST.s.bs[0].land, 0, `${st("+1", "ok")}${bC.main(NEW, "ok")}`]], E);
  states(hC, [[ST.l2 + 0.2, 0, `${st("+3", "ok")}${hC.main(NEW, "ok")}`]], E);
}

// ---------- static wires ----------
// each half's wire up to NATS, and its lane down to the rows it serves
const lane = (x, rows, toX) => `M${x} ${BAR.y + BAR.h} V${Math.max(...rows.map((i) => RY[i]))} ${rows.map((i) => `M${x} ${RY[i]} H${toX}`).join(" ")}`;
const svWire = (b, rows) => `<path class="ln" d="${pd(W_.svUp(b))}${rows.length ? " " + lane(svLane(b), rows, ROWX(b)) : ""}"/>`;
const clWire = (b, rows) => `<path class="ln" d="${pd(W_.clUp(b))} ${lane(clLane(b), rows, ROWX(b) + ROWW)}"/>`;
const wires = [];
wires.push(part([2, 5], svWire("b", [0, 3])), part([2, 3, 4, 5], clWire("b", [0])));
wires.push(part([4], svWire("h", [])), part([2, 4, 5], clWire("h", [0, 1, 3])));
const lpWires = [part([3, 5], svWire("l", [1, 2, 3])), part([2, 4], clWire("l", [0]))];
// the hub <-> leaf link: solid; broken while the laptop sleeps; amber dashes while a job waits on it
const lk = `M${NH.x + NH.w} ${LINK} H${LF.x}`;
const LINKDOWN = DEMO ? [] : [[L.sleep[0], L.park + 0.4], ...SLEEPS.slice(1)], LINKWAIT = DEMO ? [] : [[L.park + 0.5, L.sleep[1]]];
lpWires.push(part([2, 3, 4, 5], `<path class="ln an" ${A(windows(SLEEPS, 1, 0, 0.3))} d="${lk}"/><path class="ln down an hide" ${A(windows(LINKDOWN, 0, 1, 0.3))} d="${lk}"/><path class="ln dash an hide" ${A(windows(LINKWAIT, 0, 1, 0.3))} d="${lk}"/>`));
if (PHONE) wires.push(part([4], `<path class="ln" d="${pd(W_.ntfy)}"/>${text(PH.x - 10, NTY + 16, "ntfy", "s13 mu", 'text-anchor="end"')}`));

// a message is a pulse that sweeps its wire from where it starts: a blurred haze
// leads, a soft glow follows and the bright core comes last, so the front is misty
// and the wire seems to light almost at once; it holds, then fades
const litW = [];
const LAYERS = [["haze", -0.06, -0.18], ["glow", 0, -0.08], ["core", 0.08, 0]]; // [class, start shift, end shift]
const inLoop = (a, b) => !ONLY || (b >= ORIGIN && a <= ORIGIN + T);
for (const h of hops) {
  if (!inLoop(h.t0, h.done + 0.5)) continue; // another scene's: never seen in this picture
  const c = h.c.slice(2);
  const paths = LAYERS.map(([cls, s0, s1]) => {
    const a = h.t0 + s0, b = h.land + s1;
    const k = anim([[0, { opacity: 0, "stroke-dashoffset": 100 }], [a - 0.04, { opacity: 0, "stroke-dashoffset": 100 }], [a, { opacity: 1, "stroke-dashoffset": 100 }], [b, { opacity: 1, "stroke-dashoffset": 0 }], [h.done, { opacity: 1, "stroke-dashoffset": 0 }], [h.done + 0.45, { opacity: 0, "stroke-dashoffset": 0 }], [1e9, { opacity: 0, "stroke-dashoffset": 100 }]]);
    return `<path class="an hide lit ${cls} ${c}-s" pathLength="100" ${A(k)} d="${pd(h.pts)}"/>`;
  });
  litW.push(paths.join(""));
}

// ---------- the parts' glows ----------
if (DEMO) glowAt("hub", CMD, ST.park);
else if (ONE) glowAt("hub", CMD, [OB.q2.land, OB.take2.t0 + 0.2]);
else if (!ONE) {
  glowAt("hub", CMD, [Q.park[0], Q.park[1]]);
  glowAt("hub", CMD, [L.park, L.take.t0 - 0.3]);
  glowAt("leaf", CMD, [L.wake, L.wake + 0.9]); // its leaf reconnects
}
const SHAPE = { hub: [NH.x, NH.y, NH.w, NH.h, 8], leaf: [LF.x, LF.y, LF.w, LF.h, 8] };
for (const b of ["b", "h", "l"]) {
  SHAPE["sv" + PART[b]] = [SV(b).x, BAR.y, SV(b).w, BAR.h, 8];
  SHAPE["cl" + PART[b]] = [CL(b).x, BAR.y, CL(b).w, BAR.h, 8];
}
for (const [p, byc] of Object.entries(at)) for (const [c, ws] of Object.entries(byc)) ring(...SHAPE[p], c.slice(2), ws, 0.15, 0.4);

// ---------- assemble the boxes ----------
const bb = [panel("b"), head("b", "build-box", "client", "linux")];
const bbIn = [
  part([2, 3, 4, 5], `${text(BX.b + 16, NH.y + 19, "no NATS here: its bmesh dials", "s13 mu")}${text(BX.b + 16, NH.y + 37, "hub-box's hub", "s13 mu")}`),
  serveOf("b", [2, 5]), cmdsOf("b", [2, 3, 4, 5]),
  part([1, 2, 3, 4, 5], row(bL)),
  part([1], `<path class="tr" d="M${bL.x + 14} ${bL.y + RH} V${RY[1]} H${bX.x}"/>` + row(bX)),
  part([], row(bV)),
  part([5], row(bC)),
];
const hb = [panel("h"), head("h", "hub-box", "hub · always on", "linux")];
const hbIn = [
  part([2, 3, 4, 5], cmp(NH, "NATS hub", '<tspan class="m13">bmesh hub</tspan> · JetStream')),
  serveOf("h", [4]), cmdsOf("h", [2, 4, 5]),
  part([2, 5], row(hO)),
  part([1, 2, 4], row(hL)),
  part([1], `<path class="tr" d="M${hL.x + 14} ${hL.y + RH} V${RY[2]} H${hX.x}"/>` + row(hX)),
  part([5], row(hC)),
];
// a job that waits on the hub
const waits = (w, words) => `<g class="an hide" ${A(windows([w], 0, 1, 0.15))}><circle cx="${NH.x + NH.w - 16}" cy="${NH.y + 16}" r="8" class="cm-f2"/><circle cx="${NH.x + NH.w - 16}" cy="${NH.y + 16}" r="4.5" fill="var(--cm)"/>${text(NH.x + NH.w - 30, NH.y + 21, words, "s13 cm", 'text-anchor="end"')}</g>`;
const MAC = DEMO ? { park: ST.park, take: ST.take, run: ST.run } : Q;
const parks = ONE ? [waits([OB.q2.land, OB.take2.t0], "xcode busy · 1 waits")] : DEMO ? [waits(ST.park, "xcode · needs macOS")] : [waits(Q.park, "xcode · needs macOS"), waits([L.park, L.take.t0], "1 job waits for laptop")];
// the laptop's macOS answers the job's need, just before it takes it
parks.push(`<g class="an hide" ${A(windows([[MAC.park[0] + 0.4, MAC.take.land]], 0, 1, 0.2, 0.4))}>${rect(BX.l + BOX.w - 74, BOX.y + 14, 62, 22, 11, "tg")}${text(BX.l + BOX.w - 16, BOX.y + 29.5, "macOS", "s13 cm b", 'text-anchor="end"')}</g>`);
// the job meets its routine: the kind in routine.xcode lights
parks.push(`<g class="an hide" ${A(windows([[MAC.take.land, MAC.run[1]]], 0, 1, 0.2, 0.4))}>${rect(lM.x + 27 + 8 * 7.8 - 2, lM.cy - 9, 5 * 7.8 + 4, 18, 4, "hl")}</g>`);

const lpPill = BX.l + 31 + "laptop".length * 9.2 + 10;
const lp = [
  panel("l"), head("l", "laptop", ""),
  `<g class="an" ${A(windows(SLEEPS, 1, 0, 0.3))}>${pill(lpPill, BOX.y + 13, "leaf · secure")}${text(BX.l + BOX.w - 16, BOX.y + 29.5, "macOS", "s13 mu", 'text-anchor="end"')}</g>`,
];
const lpIn = [
  part([2, 3, 4, 5], cmp(LF, "NATS leaf", '<tspan class="m13">bmesh hub</tspan> · relays to the hub')),
  serveOf("l", [3, 5]), cmdsOf("l", [2, 4]),
  part([1, 2, 4], row(lL)),
  part([3], row(lM)),
  part([5], row(lC)),
  part([5], row(lP)),
];
// asleep: the laptop fades, its dots grey and its badge says so
const lpG = [`<g class="an" ${A(windows(SLEEPS, 1, 0.45, 0.4))}>`, ...lp, ...lpWires, ...lpIn, `</g>`];
lpG.push(`<circle class="an hide" ${A(windows(SLEEPS, 0, 1, 0.3))} cx="${BX.l + 18}" cy="${BOX.y + 24}" r="5" fill="var(--mu)"/>`);
lpG.push(`<circle class="an hide" ${A(windows(SLEEPS, 0, 1, 0.3))} cx="${SV("l").x + 13}" cy="${BAR.y + 13}" r="4" fill="var(--mu)"/>`);
lpG.push(`<g class="an hide" ${A(windows(SLEEPS, 0, 1, 0.3))}>${pill(lpPill, BOX.y + 13, "asleep · leaf · secure", "pl sl")}</g>`);

// ---------- phone ----------
const ph = [`<g class="an" ${A(dimFor([4]))}>`];
ph.push(rect(PH.x, PH.y, PH.w, PH.h, 16, "pn"), rect(PH.x + PH.w / 2 - 12, PH.y + 8, 24, 4, 2, "notch"));
const scr = { x: PH.x + 5, y: PH.y + 18, w: PH.w - 10, h: PH.h - 30 };
ph.push(rect(scr.x, scr.y, scr.w, scr.h, 8, "scr"), text(PH.x + PH.w / 2, scr.y + 22, "09:41", "s14 mu", 'text-anchor="middle"'));
const CH = 34, CG = 6, cy0 = scr.y + 32;
const CARDS = ONE ? OB.cards : DEMO ? ST.cards : P, CARD_END = ONE ? OB.end - 0.2 : DEMO ? ST.end - 0.2 : s4[1];
CARDS.forEach((p, i) => {
  // a card lands on top; each later one pushes it down a slot
  const pts = [[0, { opacity: 0, transform: "translate(0px,0px)" }], [p.at - 0.05, { opacity: 0, transform: "translate(0px,0px)" }], [p.at + 0.35, { opacity: 1, transform: "translate(0px,0px)" }]];
  // the screen holds three: a card pushed to a fourth slot slides off and fades
  CARDS.slice(i + 1).forEach((q, k) => pts.push([q.at - 0.6, { opacity: k < 3 ? 1 : 0, transform: `translate(0px,${k * (CH + CG)}px)` }], [q.at - 0.15, { opacity: k + 1 < 3 ? 1 : 0, transform: `translate(0px,${(k + 1) * (CH + CG)}px)` }]));
  const lastY = (CARDS.length - 1 - i) * (CH + CG);
  pts.push([CARD_END - 0.2, { opacity: CARDS.length - 1 - i < 3 ? 1 : 0, transform: `translate(0px,${lastY}px)` }], [CARD_END + 0.3, { opacity: 0, transform: `translate(0px,${lastY}px)` }], [1e9, { opacity: 0, transform: "translate(0px,0px)" }]);
  const cx = scr.x + 8, cw = scr.w - 16;
  ph.push(`<g class="an hide" ${A(anim(pts))}>${rect(cx, cy0, cw, CH, 6, "card")}<circle cx="${cx + 9}" cy="${cy0 + 11}" r="3.5" fill="var(${p.col})"/>${text(cx + 17, cy0 + 15, p.who, "m13 b")}${text(cx + 8, cy0 + 29, p.what, "s13")}</g>`);
});
ph.push(`</g>`);

// no text beyond the boxes' own labels: the README says what each picture shows
const HT = ONE ? Math.max(NTY + 26, PH.y + PH.h + 16) : PHONE ? PH.y + PH.h + 16 : BOX.y + BOX.h + 16;
const WIDTH = ONE ? PH.x + PH.w + 24 : W;

flush();

const style = `
svg{--fg:#e6edf3;--mu:#8b949e;--pf:#161b22;--ps:#30363d;--ed:#3d4652;--sc:#0d1117;--cp:#1c2129;--wk:#58a6ff;--tl:#39c5bb;--am:#d29922;--pu:#a371f7;--ok:#3fb950;--hd:#8fa7c4;--rd:#f85149;--cm:#c3cbd4}
svg.light{--fg:#1f2328;--mu:#59636e;--pf:#f6f8fa;--ps:#d0d7de;--ed:#c3ccd5;--sc:#ffffff;--cp:#ffffff;--wk:#0969da;--tl:#1a7f72;--am:#9a6700;--pu:#8250df;--ok:#1a7f37;--hd:#57728f;--rd:#cf222e;--cm:#6b7480}
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
.dash{stroke-dasharray:4 6;stroke:var(--cm)}.down{stroke-dasharray:4 6}
.tr{stroke:var(--ps);stroke-width:1.5;fill:none}
.ck{fill:none;stroke:var(--ok);stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.pb{fill:none;stroke:var(--ps);stroke-width:1.5}.pt{fill:var(--mu)}
.sl .pb{stroke:var(--hd)}.sl .pt{fill:var(--hd)}
.rg{fill:none;stroke-width:2}
.lit{fill:none;stroke-linecap:round;stroke-dasharray:100 100}.an.lit{animation-timing-function:cubic-bezier(.25,.8,.35,1)}
.haze{stroke-width:10;stroke-opacity:.22;filter:url(#mist)}.glow{stroke-width:5;stroke-opacity:.4}.core{stroke-width:2.5}
.tl-s{stroke:var(--tl)}.am-s{stroke:var(--am)}.pu-s{stroke:var(--pu)}.hd-s{stroke:var(--hd)}.wk-s{stroke:var(--wk)}
.cm-f2{fill:var(--cm);opacity:.3}.tg{fill:var(--pf);stroke:var(--cm);stroke-width:1.2}.hl{fill:var(--cm);fill-opacity:.25}.cm{fill:var(--cm)}.cm-s{stroke:var(--cm)}
.scr{fill:var(--sc);stroke:var(--ps);stroke-width:1}.card{fill:var(--pf);stroke:var(--ps);stroke-width:1.5}
.notch{fill:var(--ps)}
svg.light .bf{fill-opacity:.35}
.trk{fill:var(--ps)}.bf{fill:var(--mu);fill-opacity:.6;transform-box:fill-box;transform-origin:left center}.b0{transform:scaleX(0)}
.an{animation-duration:${T}s;animation-iteration-count:infinite;animation-timing-function:ease-in-out}
.hide{opacity:0}
.wkf{fill:var(--wk)}.tlf{fill:var(--tl)}.amf{fill:var(--am)}.puf{fill:var(--pu)}.hdf{fill:var(--hd)}
.spin{transform-box:fill-box;transform-origin:center;animation:sp .96s steps(8) infinite}
@keyframes sp{to{transform:rotate(360deg)}}
.arc{fill:none;stroke:var(--tl);stroke-width:1.8;stroke-linecap:round}.a2{animation:pg 1.8s ease-in-out infinite}
@keyframes pg{0%,100%{opacity:.45}50%{opacity:1}}
.cap{opacity:.42}
@media (prefers-reduced-motion:reduce){*{animation:none!important}.cap{opacity:1}}
${css.join("\n")}
`;

const LABEL = {
  one: "All of it on one box: the laptop's bmesh serve embeds the NATS hub. The coordinator starts two leads, the box's cap; task-1's xcode check runs on the same box; task-2 queues one too and waits on the hub while the routine is busy, then runs once it's free, and lands first; the freed slot starts task-3, which depends on task-1, so the coordinator holds it until task-1 lands, then releases it; each land moves the checkout and is pushed to the phone",
  demo: "Three boxes share one NATS hub on hub-box, and only bmesh talks to it. One workflow, end to end: the coordinator starts a lead on every box; task-1's check that needs macOS runs on the laptop and its result comes back; task-2's question reaches the phone; the coordinator holds task-1 until task-3 has landed, then releases it; task-1 lands, then task-2, each reported to the coordinator and the phone, each land moving its own box's checkout, and bmesh sync carries the last to build-box and the laptop",
  build: "Every box builds at once: the leads on build-box, hub-box and the laptop start one after another and keep running, each in its own git worktree",
  coordinate: "The coordinator on hub-box starts a lead on each box in turn, then listens; it holds task-1, which stops where it is, until task-3 has landed, then releases it, and build-box's bmesh serve wakes task-1",
  route: "task-1 queues an xcode job that needs macOS; it waits on the NATS hub until the laptop's bmesh serve takes it and runs routine.xcode, and the result comes back to task-1",
  push: "When a lead is done, asks or is blocked, its event goes through the hub to hub-box's bmesh serve, which pushes it to your phone with ntfy",
  level: "task-1 lands and moves build-box's checkout; the coordinator runs bmesh sync, which levels hub-box and sends level jobs; the asleep laptop's waits on the hub, and it levels when it wakes and holds a plugins commit for approval",
};
const body = [
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${WIDTH} ${HT}" width="${WIDTH}" height="${HT}" class="dark" role="img" aria-label="${esc(LABEL[VARIANT]).replace(/"/g, "&quot;")}">`,
  `<title>buildermesh across boxes${DEMO ? "" : ": " + VARIANT}</title>`,
  `<defs><filter id="mist" x="-20%" y="-20%" width="140%" height="140%"><feGaussianBlur stdDeviation="1.6"/></filter></defs>`,
  `<style>${style}</style>`,
  ...(ONE
    ? [panel("h"), head("h", "laptop", "single box", "macOS"),
      `<path class="ln" d="${pd(W_.svUp("h"))} ${lane(svLane("h"), [1, 4], ROWX("h"))}"/>`,
      `<path class="ln" d="${pd(W_.clUp("h"))} ${lane(clLane("h"), [0, 1, 2, 3], ROWX("h") + ROWW)}"/>`,
      `<path class="ln" d="${pd(W_.ntfy)}"/>${text(PH.x - 30, NTY + 15, "ntfy", "s13 mu", 'text-anchor="end"')}`,
      cmp(NH, "NATS hub", 'embedded in <tspan class="m13">bmesh serve</tspan>'), serveOf("h", []), cmdsOf("h", []),
      ...Object.values(O).map(row)]
    : [...bb, ...hb, ...wires, ...bbIn, ...hbIn, ...lpG]),
  ...litW, ...glows, ...parks, ...(PHONE ? ph : []), ...out,
  `</svg>\n`,
].join("\n");

const dir = process.argv[2];
const base = DEMO ? "mesh" : `mesh-${VARIANT}`;
writeFileSync(join(dir, `${base}-dark.svg`), body);
writeFileSync(join(dir, `${base}-light.svg`), body.replace('class="dark"', 'class="light"'));
console.log(`${base}: ${Buffer.byteLength(body)} bytes, loop ${T}s`);
