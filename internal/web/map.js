"use strict";
// The map: /api/tree drawn as an SVG tree and kept current by /events. Live
// node events restyle a node at once, live inbox messages flash an edge,
// replayed frames only fill the recent-events drawer. Every mesh value goes
// in through textContent or setAttribute.

const NS = "http://www.w3.org/2000/svg";
// gutter is the room right of the boxes in the vertical layout, where
// message edges between boxes of one column bow out; minW the narrowest box.
const G = { row: 48, nodeW: 184, nodeH: 40, indent: 24, gap: 40, pad: 16, head: 28, gutter: 28, minW: 120 };
const LABEL_PX = 7.5, SUB_PX = 6; // average glyph widths of .label and .sub
// The done shelf's chips; font and headFont are .chip .label's and
// .shelf-head's in map.css, for textW.
const CHIP = { h: 32, pad: 8, gap: 8, inset: 10, checkW: 16, maxLabel: 28, font: "500 12px", headFont: "700 14px" };
// NEED orders the map's leads: what needs a hand, then what runs, what has
// no state yet, what is finished.
const NEED = { blocked: 0, asking: 1, silent: 2, working: 3, idle: 4, done: 5 };
const PHONE = "(max-width: 699px)";
const FETCH_MS = 10000; // a /api/tree read taking longer is abandoned
const RECENT = 30;
const EDGE_MS = 1200;
const REFETCH_MS = 500;
const REFETCH_MAX_MS = 2000; // a steady stream of events still refetches this often
const POLL_MS = 30000; // unread counts and runners change without an event
const EVENT_STATE = { started: "working", progress: "working", ask: "asking", blocked: "blocked", done: "done" };
const MSG_KINDS = new Set(["message", "wake", "job", "reply"]);

const $ = (id) => document.getElementById(id);

const mesh = {
  tree: null, // the last /api/tree
  drawn: new Map(), // path -> {g, sub, box, extra, text, kind, state}
  layer: null, // where message edges go, above the nodes
  vertical: false, // blocks top to bottom instead of left to right
  width: 0, // the drawn SVG's width
  // path -> {state, text, since, at}: states set by live events, with the
  // event's words and time (since) and when the page saw it (at).
  live: new Map(),
  recent: [], // newest first
};

// check is set by ?check=1: a self-check that writes viewport and
// connection facts into document.title for headless screenshots.
const check = new URLSearchParams(location.search).get("check") === "1"
  ? { conn: [], trees: 0, leads: 0, replay: 0, live: 0, flashes: 0 } : null;

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

let ruler = null;

// textW measures s on a canvas in font, a weight and size, in the page's family.
function textW(s, font) {
  if (!ruler) ruler = document.createElement("canvas").getContext("2d");
  ruler.font = font + " " + getComputedStyle(document.body).fontFamily;
  return ruler.measureText(s).width;
}

// --- layout ---------------------------------------------------------------

function startOf(t) {
  const v = Date.parse(t.node.started_at || "");
  return isNaN(v) ? Infinity : v;
}

// byStart orders leads by started_at (unknown last), ties by path.
function byStart(a, b) {
  const x = startOf(a), y = startOf(b);
  if (x !== y) return x < y ? -1 : 1;
  return a.node.path < b.node.path ? -1 : a.node.path > b.node.path ? 1 : 0;
}

function needRank(state) {
  const r = NEED[state];
  return r === undefined ? NEED.idle : r;
}

function needOf(t) {
  return needRank(t.state.state);
}

// byNeed orders leads by need, ties by byStart, so a running lead's place
// stays stable.
function byNeed(a, b) {
  const x = needOf(a), y = needOf(b);
  return x !== y ? x - y : byStart(a, b);
}

function isDone(t) {
  return t.state.state === "done";
}

function treeSub(t) {
  const extra = [];
  if (t.unread > 0) extra.push(t.unread + " unread");
  return extra;
}

function instanceSub(i) {
  const extra = [i.uses === 1 ? "1 use" : (i.uses || 0) + " uses"];
  if (i.context > 0) extra.push(Math.round(i.context / 1000) + "k ctx");
  return extra;
}

// kindCounts counts a kind's queue and its instances by state: busy the
// working ones, idle the idle ones, spent the done ones.
function kindCounts(g) {
  const c = { queued: g.depth || 0, busy: 0, idle: 0, spent: 0 };
  for (const i of g.instances || []) {
    const s = i.state && i.state.state;
    if (s === "working") c.busy++;
    else if (s === "idle") c.idle++;
    else if (s === "done") c.spent++;
  }
  return c;
}

// kindSub is a kind box's second line: its non-zero counts, else its mode.
function kindSub(g, c) {
  const extra = ["queued", "busy", "idle", "spent"].filter((k) => c[k] > 0).map((k) => c[k] + " " + k);
  return extra.length ? extra : [g.mode || "no kind file"];
}

// layout places the tree: one block per lead in need order (byNeed), then
// the orphans block under its synthetic root, then the routines. Blocks run
// left to right with the routines as the right column, or, when vertical,
// top to bottom with the routines last. Each block is a vertical tree with
// one node per row. Boxes with a key are mesh nodes (or a kind's inbox); a
// routine kind is one box, its instances counted, not drawn.
// When a lead is live (any state but done) and another is done, the done
// leads leave the tree for a shelf: a row of chips, newest first, under the
// blocks (and before the routines when vertical). A vertical layout given a
// width fits it: boxes stretch to its right edge less the gutter, and only a
// tree nested past the narrowest box overflows; a horizontal one uses the
// width only to wrap the shelf.
function layout(tree, vertical, width) {
  const out = { boxes: [], edges: [], headings: [], frames: [], width: 0, height: 0 };
  const boxW = (x) => vertical && width ? Math.max(G.minW, width - G.pad - G.gutter - x) : G.nodeW;
  const add = (b) => {
    b.w = b.w || boxW(b.x);
    b.h = b.h || G.nodeH;
    out.boxes.push(b);
    out.width = Math.max(out.width, b.x + b.w);
    out.height = Math.max(out.height, b.y + b.h);
    return b;
  };
  const elbow = (p, c) => {
    const x = p.x + 12;
    out.edges.push(`M${x},${p.y + p.h} V${c.y + c.h / 2} H${c.x}`);
  };

  // block draws roots (under root when given) at ox, oy; returns its size.
  const block = (roots, ox, oy, root) => {
    let row = 0, depthMax = 0;
    const place = (b, depth) => {
      depthMax = Math.max(depthMax, depth);
      b.x = ox + depth * G.indent;
      b.y = oy + row * G.row;
      row++;
      return add(b);
    };
    const walk = (t, depth, parent) => {
      const role = t.node.role === "lead" ? "lead" : "executor";
      const label = depth === 0 || !parent || parent.kind === "root" ? t.node.path : lastToken(t.node.path);
      const b = place({ key: t.node.path, kind: role, label, title: t.node.path, state: t.state.state,
        text: t.state.text || "", extra: treeSub(t) }, depth);
      if (parent) elbow(parent, b);
      for (const c of t.children || []) walk(c, depth + 1, b);
    };
    let top = null, base = 0;
    if (root) {
      top = place(root, 0);
      base = 1;
    }
    for (const r of roots) walk(r, base, top);
    return { w: depthMax * G.indent + G.nodeW, h: row * G.row };
  };

  // shelf lays leads out as chips, newest first, at ox, oy after a "done (n)"
  // heading, wrapping within maxW; returns the size of the frame around them.
  const shelf = (leads, ox, oy, maxW) => {
    const head = "done (" + leads.length + ")";
    const left = ox + CHIP.inset, right = ox + maxW - CHIP.inset;
    out.headings.push({ x: left, y: oy + CHIP.inset + CHIP.h / 2 + 5, text: head, cls: "shelf-head" });
    let cx = left + Math.ceil(textW(head, CHIP.headFont)) + CHIP.gap, cy = oy + CHIP.inset, w = cx;
    for (const t of leads.slice().sort((a, b) => byStart(b, a))) {
      const label = fit(t.node.path, CHIP.maxLabel);
      const cw = Math.ceil(2 * CHIP.pad + textW(label, CHIP.font) + CHIP.checkW);
      if (cx + cw > right && cx > left) {
        cx = left;
        cy += CHIP.h + CHIP.gap;
      }
      add({ key: t.node.path, kind: "chip", label, title: t.node.path, state: "done", extra: [],
        x: cx, y: cy, w: cw, h: CHIP.h });
      cx += cw + CHIP.gap;
      w = Math.max(w, cx);
    }
    const size = { w: vertical ? maxW : Math.min(maxW, w - CHIP.gap - ox + CHIP.inset), h: cy + CHIP.h + CHIP.inset - oy };
    out.frames.push({ x: ox, y: oy, w: size.w, h: size.h });
    out.width = Math.max(out.width, ox + size.w);
    out.height = Math.max(out.height, oy + size.h);
    return size;
  };

  let x = G.pad, y = G.pad, tall = 0;
  const next = (size) => {
    if (vertical) y += size.h + G.gap / 2;
    else x += size.w + G.gap;
    tall = Math.max(tall, size.h);
  };
  const leads = tree.leads.slice().sort(byNeed);
  const done = leads.filter(isDone);
  const fold = done.length > 0 && done.length < leads.length;
  for (const l of fold ? leads.filter((t) => !isDone(t)) : leads) next(block([l], x, y));
  if (tree.orphans.length) {
    const n = tree.orphans.length;
    const root = { kind: "root", label: "orphans", title: "nodes whose lead is not registered", state: "",
      extra: [n === 1 ? "1 root" : n + " roots"] };
    next(block(tree.orphans, x, y, root));
  }
  const kinds = tree.routines.map((g) => {
    const c = kindCounts(g);
    return { g, busy: c.busy > 0, extra: kindSub(g, c) };
  });
  // In columns the kind boxes widen together to fit their longest counts.
  const colW = vertical && width ? 0 : Math.max(G.nodeW,
    ...kinds.map((k) => Math.ceil(k.extra.join(" · ").length * SUB_PX) + 16));
  if (fold) {
    if (vertical) {
      next(shelf(done, x, y, boxW(x)));
    } else {
      // Under the blocks, as wide as they or the page beside the routines
      // column allow; the column sits right of both.
      const column = kinds.length ? 1.5 * G.gap + colW : 0;
      const room = width ? width - 2 * G.pad - column : 0;
      const s = shelf(done, G.pad, y + tall + G.gap / 2, Math.max(x - G.gap - G.pad, room, G.nodeW));
      x = Math.max(x, G.pad + s.w + G.gap);
    }
  }

  if (tree.routines.length) {
    if (!vertical && x > G.pad) x += G.gap / 2;
    out.headings.push({ x, y: y + 14, text: "routines" });
    let ry = y + G.head;
    for (const k of kinds) {
      const key = "routine." + k.g.kind;
      add({ key, kind: "group", label: k.g.kind, title: key, state: k.busy ? "working" : "", x, y: ry, w: colW,
        extra: k.extra });
      ry += G.row;
    }
    out.width = Math.max(out.width, x + (colW || boxW(x)));
  }
  out.width += vertical && width ? G.gutter + G.pad : G.pad;
  out.height += G.pad;
  return out;
}

// --- drawing --------------------------------------------------------------

function classOf(kind, state) {
  const link = kind === "lead" || kind === "chip" || kind === "group" ? " link" : "";
  return "node " + kind + link + (state ? " st-" + state : "");
}

// subText is a box's second line, cut to n characters: for an asking or
// blocked node the state and its words (the ask or the block), else the
// state and the extras.
function subText(state, text, extra, n) {
  const parts = state === "asking" || state === "blocked" ? [state, text] : [state, ...extra];
  return fit(parts.filter(Boolean).join(" · "), n);
}

// room is how many characters of px average width fit a box w wide less
// its padding (and, for a label, the done check).
function room(w, px, pad) {
  return Math.max(4, Math.floor((w - pad) / px));
}

function hrefOf(b) {
  if (b.kind === "lead" || b.kind === "chip") return "#/lead/" + b.key;
  if (b.kind === "group") return "#/routines";
  return "";
}

function link(g, b) {
  const href = hrefOf(b);
  if (!href) return;
  g.setAttribute("tabindex", "0");
  g.setAttribute("role", "link");
  g.setAttribute("aria-label", b.title);
  const go = () => { location.hash = href; };
  g.addEventListener("click", go);
  g.addEventListener("keydown", (e) => {
    if (e.key === "Enter" || e.key === " ") { e.preventDefault(); go(); }
  });
}

function drawChip(parent, b) {
  const g = svg("g", { class: classOf(b.kind, b.state), transform: `translate(${b.x},${b.y})` }, parent);
  svg("title", {}, g).textContent = b.title;
  svg("rect", { class: "box", width: b.w, height: b.h, rx: b.h / 2 }, g);
  svgText({ class: "label", x: CHIP.pad, y: b.h / 2 + 4 }, b.label, g);
  svgText({ class: "check", x: b.w - CHIP.pad - 9, y: b.h / 2 + 5 }, "✓", g);
  link(g, b);
  return { g, sub: null };
}

function drawBox(parent, b) {
  if (b.kind === "chip") return drawChip(parent, b);
  const g = svg("g", { class: classOf(b.kind, b.state), transform: `translate(${b.x},${b.y})` }, parent);
  svg("title", {}, g).textContent = b.title;
  svg("rect", { class: "ring", x: -4, y: -4, width: b.w + 8, height: b.h + 8, rx: 10 }, g);
  svg("rect", { class: "box", width: b.w, height: b.h, rx: 7 }, g);
  svgText({ class: "label", x: 10, y: 17 }, fit(b.label, room(b.w, LABEL_PX, 32)), g);
  const sub = svgText({ class: "sub", x: 10, y: 32 },
    subText(b.kind === "group" ? "" : b.state, b.text || "", b.extra, room(b.w, SUB_PX, 16)), g);
  svgText({ class: "check", x: b.w - 22, y: 26 }, "✓", g);
  link(g, b);
  return { g, sub };
}

function render() {
  const t = mesh.tree;
  if (!t) return;
  // A hidden map has no width; the view's would be the page's less its padding.
  const w = $("map").clientWidth || document.documentElement.clientWidth - 2 * 12;
  const L = layout(t, mesh.vertical, w);
  const root = svg("svg", { width: L.width, height: L.height, viewBox: `0 0 ${L.width} ${L.height}`,
    role: "group", "aria-label": "mesh map" });
  const edges = svg("g", { class: "edges" }, root);
  for (const f of L.frames) svg("rect", { class: "shelf", x: f.x, y: f.y, width: f.w, height: f.h, rx: 10 }, edges);
  for (const d of L.edges) svg("path", { class: "tree-edge", d }, edges);
  for (const h of L.headings) svgText({ class: h.cls || "heading", x: h.x, y: h.y }, h.text, root);
  const nodes = svg("g", { class: "nodes" }, root);
  const drawn = new Map();
  for (const b of L.boxes) {
    const { g, sub } = drawBox(nodes, b);
    if (b.key) drawn.set(b.key, { g, sub, box: b, extra: b.extra, text: b.text || "", kind: b.kind, state: b.state });
  }
  // Edges still in flight move to the new drawing rather than vanish.
  if (mesh.layer) root.append(mesh.layer);
  else mesh.layer = svg("g", { class: "messages" }, root);
  mesh.drawn = drawn;
  mesh.width = L.width;
  $("map").replaceChildren(root);
  $("map-empty").hidden = L.boxes.length > 0;
  if (check) {
    check.trees++;
    check.leads = t.leads.length;
    checkSoon();
  }
}

// onShelf reports whether path is drawn as a chip, which only a redraw
// can move off the shelf.
function onShelf(path) {
  const d = mesh.drawn.get(path);
  return !!d && d.kind === "chip";
}

// setState restyles a drawn node; text is the state's words.
function setState(path, state, text) {
  const d = mesh.drawn.get(path);
  if (!d || d.kind === "group" || d.kind === "chip") return;
  d.state = state;
  d.text = text || "";
  d.g.setAttribute("class", classOf(d.kind, state));
  d.sub.textContent = subText(state, d.text, d.extra, room(d.box.w, SUB_PX, 16));
}

// curve is a path from box a to box b, leaving and entering on the facing
// sides; boxes in one column bow out to the right, within the drawing.
function curve(a, b) {
  const ay = a.y + a.h / 2, by = b.y + b.h / 2;
  if (Math.abs(a.x - b.x) < a.w / 2 || mesh.vertical) {
    const x1 = a.x + a.w, x2 = b.x + b.w;
    const bow = Math.max(8, Math.min(36 + Math.abs(by - ay) / 4, mesh.width - Math.max(x1, x2) - 4));
    return `M${x1},${ay} C${x1 + bow},${ay} ${x2 + bow},${by} ${x2},${by}`;
  }
  const right = b.x > a.x;
  const x1 = right ? a.x + a.w : a.x, x2 = right ? b.x : b.x + b.w;
  const mx = (x1 + x2) / 2;
  let lift = Math.min(80, Math.abs(x2 - x1) / 3);
  if (Math.min(ay, by) < lift + 8) lift = -lift; // near the top: sag instead
  return `M${x1},${ay} C${mx},${ay - lift} ${mx},${by - lift} ${x2},${by}`;
}

// entry is a path into box b from above the map, for a sender not drawn.
function entry(b) {
  const x = b.x + b.w / 2;
  return `M${x + 40},0 Q${x + 40},${b.y / 2} ${x},${b.y}`;
}

// kindKey is the kind box of a routine instance's path, else "".
function kindKey(path) {
  const m = /^routine\.[^.]+/.exec(path);
  return m ? m[0] : "";
}

// flash draws a message edge from -> to for EDGE_MS with a dot travelling
// along it.
function flash(from, to, kind) {
  const b = mesh.drawn.get(to);
  if (!b || !mesh.layer) return;
  const a = mesh.drawn.get(from) || mesh.drawn.get(kindKey(from));
  const g = svg("g", { class: "msg k-" + (MSG_KINDS.has(kind) ? kind : "message") }, mesh.layer);
  const p = svg("path", { class: "msg-edge", d: a ? curve(a.box, b.box) : entry(b.box) }, g);
  const dot = svg("circle", { class: "msg-dot", r: 5 }, g);
  if (check) check.flashes++;
  const len = p.getTotalLength();
  const t0 = performance.now();
  const step = (now) => {
    if (!g.isConnected) return;
    const k = Math.min(1, (now - t0) / EDGE_MS);
    const pt = p.getPointAtLength(len * k);
    dot.setAttribute("cx", pt.x);
    dot.setAttribute("cy", pt.y);
    g.setAttribute("opacity", k > 0.75 ? ((1 - k) / 0.25).toFixed(2) : "1");
    if (k < 1) requestAnimationFrame(step);
  };
  step(t0);
  setTimeout(() => g.remove(), EDGE_MS); // also when frames are paused (a hidden tab)
}

// --- the tree -------------------------------------------------------------

let loading = false, reload = false, refetchTimer = 0, refetchFirst = 0;

// loadTree reads /api/tree and redraws. One read runs at a time: a call
// while one is in flight runs one more read after it, so a slow server
// still gets every answer drawn instead of each being overtaken.
async function loadTree() {
  if (loading) {
    reload = true;
    return;
  }
  loading = true;
  const began = Date.now();
  const stop = new AbortController();
  const timer = setTimeout(() => stop.abort(), FETCH_MS);
  try {
    const r = await fetch("/api/tree", { cache: "no-store", signal: stop.signal });
    if (!r.ok) throw new Error("/api/tree: " + r.status);
    const v = await r.json();
    mesh.tree = { leads: v.leads || [], routines: v.routines || [], orphans: v.orphans || [] };
    render();
    // A live event newer than this read wins over the state it returned.
    for (const [path, s] of mesh.live) {
      if (s.at >= began) setState(path, s.state, s.text);
      else mesh.live.delete(path);
    }
    renderBand();
  } catch (e) {
    // The next event or poll tries again.
  } finally {
    clearTimeout(timer);
    loading = false;
    if (reload) {
      reload = false;
      loadTree();
    }
  }
}

// refetchSoon loads the tree REFETCH_MS after the last call, but no later
// than REFETCH_MAX_MS after the first one waiting.
function refetchSoon() {
  const now = Date.now();
  if (!refetchTimer) refetchFirst = now;
  clearTimeout(refetchTimer);
  const wait = Math.max(0, Math.min(REFETCH_MS, refetchFirst + REFETCH_MAX_MS - now));
  refetchTimer = setTimeout(() => { refetchTimer = 0; loadTree(); }, wait);
}

// liveState restyles a node at once with the event's words (text) and time
// (since), and redraws the band; a lead whose need changed is also reloaded,
// so the blocks keep their order.
function liveState(path, state, text, since) {
  const d = mesh.drawn.get(path);
  const moved = !!d && d.kind === "lead" && needRank(d.state) !== needRank(state);
  mesh.live.set(path, { state, text: text || "", since: since || "", at: Date.now() });
  setState(path, state, text);
  if (moved) refetchSoon();
  renderBand();
}

// apply acts on a live frame: node and daemon events restyle nodes, inbox
// messages flash an edge, and anything the map cannot place refetches.
function apply(f) {
  const rest = f.subject.replace(/^bm\./, "");
  const p = f.payload && typeof f.payload === "object" ? f.payload : {};
  if (rest.startsWith("inbox.")) {
    const to = rest.slice(6);
    flash(String(p.from || ""), to, String(p.kind || "message"));
    if (!mesh.drawn.has(to) && !to.startsWith("routine.") && !onShelf(to.split(".")[0])) refetchSoon();
    return;
  }
  const i = rest.lastIndexOf(".");
  if (i <= 0) return;
  const path = rest.slice(0, i), ev = rest.slice(i + 1);
  const text = String(p.text || ""), at = String(p.at || f.at || "");
  if (path.startsWith("daemon.")) {
    if (ev === "silent" && p.to) liveState(String(p.to), "silent", text, at);
    return;
  }
  if (EVENT_STATE[ev]) liveState(path, EVENT_STATE[ev], text, at);
  // A shelved lead's executors are not drawn and a reload would not draw them.
  if (!mesh.drawn.has(path) && onShelf(path.split(".")[0])) return;
  if (ev === "started" || ev === "done" || !mesh.drawn.has(path) || onShelf(path)) refetchSoon();
}

// --- the recent-events drawer ---------------------------------------------

// describe turns a frame into {path, event, text}.
function describe(f) {
  const p = f.payload && typeof f.payload === "object" ? f.payload : { text: String(f.payload) };
  const rest = f.subject.replace(/^bm\./, "");
  if (rest.startsWith("inbox.")) {
    const kind = p.kind && p.kind !== "message" ? p.kind + " " : "";
    return { path: (p.from || "?") + " → " + rest.slice(6), event: "inbox", text: kind + (p.text || "") };
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

// frameKey names a frame for openRows.
function frameKey(f) {
  return f.subject + "@" + ((f.payload && f.payload.at) || f.at || "");
}

function drawRecent() {
  recentTimer = 0;
  const items = mesh.recent.map((f) => {
    const d = describe(f);
    const li = document.createElement("li");
    li.className = "ev-" + d.event;
    li.append(span("t", stamp((f.payload && f.payload.at) || f.at)), span("p", d.path), span("e", d.event), span("x", d.text));
    clampRow(li, frameKey(f));
    return li;
  });
  $("recent").replaceChildren(...items);
  $("drawer-count").textContent = items.length ? "(" + items.length + ")" : "";
}

// --- clamped rows -----------------------------------------------------------

// openRows holds the keys of the clamped rows tapped open (a band row's
// path, a drawer or timeline frame's frameKey, an inbox message's id), so
// a redraw keeps them open.
const openRows = new Set();

// clampRow marks a row clamped to three lines until tapped; key names it in
// openRows.
function clampRow(e, key) {
  e.classList.add("clamp");
  e.dataset.key = key;
  if (openRows.has(key)) e.classList.add("open");
}

// toggleRows opens or closes the clamped row a tap lands on (a band row's
// words from anywhere in the row), unless the tap is a link or a fold.
function toggleRows(ev) {
  if (ev.target.closest("a, summary")) return;
  const row = ev.target.closest(".clamp, .need");
  const e = row && row.classList.contains("need") ? row.querySelector(".clamp") : row;
  if (!e || !this.contains(e)) return;
  const open = e.classList.toggle("open");
  if (open) openRows.add(e.dataset.key);
  else openRows.delete(e.dataset.key);
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
  return el("span", "badge st-" + s.replace(/[^a-z-]/g, ""), s);
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
// NEEDS state, in need order: a live event's state and words win over the
// tree's, as on the boxes.
function needRows() {
  const rows = [];
  const consider = (t) => {
    const live = mesh.live.get(t.node.path);
    const st = live ? { state: live.state, text: live.text, at: live.since } : t.state;
    if (NEEDS.has(st.state)) rows.push({ t, path: t.node.path, state: st.state, text: st.text || "", at: st.at || "" });
  };
  for (const t of mesh.tree ? mesh.tree.leads : []) {
    consider(t);
    if (t.node.role === "round") for (const c of t.children || []) consider(c);
  }
  return rows.sort((a, b) => needRank(a.state) - needRank(b.state) || byStart(a.t, b.t));
}

// renderBand draws the band between the strip and the map: one row per
// needRows entry, its badge and path a link to the lead page, its words (for
// silent, the daemon's verdict and its age) clamped to three lines until
// tapped. Hidden when nothing needs the operator.
function renderBand() {
  const band = $("needs");
  const rows = needRows();
  band.replaceChildren(...rows.map((r) => {
    const who = el("a", "who tap", null, badge(r.state), el("span", "p", r.path));
    who.setAttribute("href", "#/lead/" + r.path);
    const say = el("div", "say", r.text);
    if (r.state === "silent" && r.at) say.append(r.text ? " · " : "", clock("since", r.at, false));
    clampRow(say, r.path);
    return el("div", "need tap st-" + r.state, null, who, say);
  }));
  band.hidden = rows.length === 0;
  if (check) checkSoon();
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
  return el("section", "card", null, el("h3", null, title), ...children);
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

  let spec;
  if (sp) {
    const merged = sp.merged ? String(sp.merged).slice(0, 8) : "";
    spec = card("spec " + (sp.id || n.spec || ""), el("p", "spec-title", sp.title || "(untitled)"),
      el("div", "fields", null, field("status", sp.status), field("merged", merged),
        field("verified", sp.verified || "not verified")),
      specBody(sp.text));
  } else {
    spec = card("spec", el("p", "empty", n.spec ? "spec " + n.spec + " is not on main of a repo configured here" : "no spec"));
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
  panel.replaceChildren(spec, el("div", "stack", null,
    card("inbox", inbox.length ? el("ol", "rows", null, ...inbox) : el("p", "empty", "no messages")),
    card("timeline", tl.length ? el("ol", "rows", null, ...tl) : el("p", "empty", "no events in the last day"))));
}

const lead = loader(async () => {
  const path = pages.lead;
  if (!path) return;
  let r;
  try {
    r = await fetchJSON("/api/lead/" + encodeURIComponent(path));
  } catch (e) {
    r = { status: 0, body: null };
  }
  if (pages.lead !== path) return; // the route moved on
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

// spentOpen holds the "<kind> <repo>" keys of the spent folds left open, so
// a redraw keeps them open.
const spentOpen = new Set();

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
      const key = k.kind + " " + repo;
      const det = el("details", "spent", null, el("summary", "tap", "spent (" + spent.length + ")"),
        el("ol", "rows", null, ...spent.map((i) => instanceRow(k, i))));
      if (spentOpen.has(key)) det.open = true;
      det.addEventListener("toggle", () => {
        if (det.open) spentOpen.add(key);
        else spentOpen.delete(key);
      });
      rows.push(el("li", "fold", null, det));
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

// --- the strip

const strip = { quota: [], digest: null };

function pct(x) {
  return Math.round(+x || 0) + "%";
}

// renderStrip draws the strip; an open digest stays open.
function renderStrip() {
  const s = $("strip");
  const q = strip.quota || [], d = strip.digest;
  const open = s.querySelector("details[open]") !== null;
  if (!q.length && !d) {
    s.hidden = true;
    s.replaceChildren();
    return;
  }
  const rows = q.map((r) => {
    const line = el("div", "quota", null, el("b", null, r.provider || "?"),
      " · 5h " + pct(r.five_hour) + " · wk " + pct(r.week));
    if (+r.five_hour >= 80 || +r.week >= 80) line.classList.add("high");
    if (r.five_reset) line.append(" · ", clock("reset", r.five_reset, true));
    if (r.read_at) line.append(" ", clock("read", r.read_at, false));
    return line;
  });
  if (d) {
    const sum = el("summary", "tap", null, clock("digest", d.at, false), ": " + (d.title || ""));
    if (d.body) {
      const det = el("details", "digest", null, sum, el("pre", null, d.body));
      if (open) det.setAttribute("open", "");
      rows.push(det);
    } else {
      rows.push(el("div", "digest", null, ...sum.childNodes));
    }
  }
  s.replaceChildren(...rows);
  s.hidden = false;
}

async function loadStrip() {
  try {
    const r = await fetchJSON("/api/strip");
    if (!r.body) return; // keep the last strip
    strip.quota = r.body.quota || [];
    strip.digest = r.body.digest || null; // null: no digest any more
    renderStrip();
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

function setConn(s) {
  const c = $("conn");
  c.textContent = s;
  c.classList.toggle("down", s === "reconnecting");
  if (check) {
    if (check.conn[check.conn.length - 1] !== s) check.conn.push(s);
    checkSoon();
  }
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
// smallest tap target, the page offset of the first working box's top (none
// without one) and the connection history into document.title.
function selfCheck() {
  const sw = document.documentElement.scrollWidth, iw = window.innerWidth;
  let tap = Infinity;
  for (const e of document.querySelectorAll(".node.link .box, nav a, .drawer summary, header h1 a, .tap")) {
    const r = e.getBoundingClientRect();
    if (r.width || r.height) tap = Math.min(tap, r.height, r.width);
  }
  const live = document.querySelector("#map .node.st-working:not(.group) .box");
  const workingTop = live ? Math.round(live.getBoundingClientRect().top + window.scrollY) : "none";
  const c = check;
  document.title = `bmesh ${sw <= iw ? "ok" : "SCROLLS"} view=${pages.view} scrollWidth=${sw} innerWidth=${iw} minTap=${Math.round(tap)}` +
    ` workingTop=${workingTop}` +
    ` svgWidth=${mesh.width} mapWidth=${$("map").clientWidth}` +
    ` vertical=${mesh.vertical} leads=${c.leads} trees=${c.trees} conn=${c.conn.join(">")}` +
    ` replay=${c.replay} live=${c.live} flashes=${c.flashes} strip=${$("strip").hidden ? "hidden" : $("strip").children.length}` +
    ` band=${$("needs").hidden ? "hidden" : $("needs").children.length}`;
}

// route shows the view the hash names: #/ the map, #/lead/<path> a lead,
// #/routines the routines.
function route() {
  const h = location.hash.replace(/^#\/?/, "");
  let view = "map";
  let path = "";
  if (h.startsWith("lead/")) {
    view = "lead";
    path = h.slice(5);
    try { path = decodeURIComponent(path); } catch (e) { /* shown as written */ }
    if (path !== pages.leadShown) {
      $("lead-title").textContent = path;
      $("lead-head").replaceChildren();
      $("lead-panel").replaceChildren();
      pages.leadShown = "";
    }
  } else if (h === "routines") {
    view = "routines";
  }
  pages.view = view;
  pages.lead = path;
  for (const v of ["map", "lead", "routines"]) $("view-" + v).hidden = v !== view;
  if (view === "lead") lead.run();
  if (view === "routines") routines.run();
  if (view === "map") render(); // the width may have changed while hidden
  for (const a of document.querySelectorAll("nav a[data-route]")) a.classList.toggle("on", a.dataset.route === view);
}

// The phone layout stacks the blocks; the map is redrawn when the media
// query flips and, since boxes and the shelf fit the width, when the window
// changes width (not height: a phone's toolbar hiding on scroll resizes it).
const phone = window.matchMedia(PHONE);
mesh.vertical = phone.matches;
phone.addEventListener("change", () => {
  mesh.vertical = phone.matches;
  render();
});
let resizeTimer = 0, lastWidth = window.innerWidth;
window.addEventListener("resize", () => {
  if (window.innerWidth === lastWidth) return;
  lastWidth = window.innerWidth;
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(render, 100);
});

for (const id of ["needs", "recent", "lead-panel"]) $(id).addEventListener("click", toggleRows);
window.addEventListener("hashchange", route);
route();
loadTree();
connect();
loadStrip();
setInterval(loadTree, POLL_MS);
setInterval(loadStrip, STRIP_MS);
setInterval(() => { if (pages.view === "routines" && !document.hidden) routines.run(); }, ROUTINES_MS);
setInterval(tick, 1000);
