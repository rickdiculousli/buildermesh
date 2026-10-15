"use strict";
// The map: /api/tree drawn as an SVG tree and kept current by /events. Live
// node events restyle a node at once, live inbox messages flash an edge,
// replayed frames only fill the recent-events drawer. Every mesh value goes
// in through textContent or setAttribute.

const NS = "http://www.w3.org/2000/svg";
const G = { row: 48, nodeW: 184, nodeH: 40, indent: 24, gap: 40, pad: 16, head: 28 };
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
  drawn: new Map(), // path -> {g, sub, box, extra, kind, state}
  layer: null, // where message edges go, above the nodes
  vertical: false, // blocks top to bottom instead of left to right
  live: new Map(), // path -> {state, at}: states set by live events
  recent: [], // newest first
};

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

// layout places the tree: one block per lead (in started_at order), then
// the orphans block under its synthetic root, then the routines. Blocks run
// left to right with the routines as the right column, or, when vertical,
// top to bottom with the routines last. Each block is a vertical tree with
// one node per row. Boxes with a key are mesh nodes (or a kind's inbox).
function layout(tree, vertical) {
  const out = { boxes: [], edges: [], headings: [], width: 0, height: 0 };
  const add = (b) => {
    b.w = b.w || G.nodeW;
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
      const b = place({ key: t.node.path, kind: role, label, title: t.node.path, state: t.state.state, extra: treeSub(t) }, depth);
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

  let x = G.pad, y = G.pad;
  const next = (size) => {
    if (vertical) y += size.h + G.gap / 2;
    else x += size.w + G.gap;
  };
  for (const l of tree.leads.slice().sort(byStart)) next(block([l], x, y));
  if (tree.orphans.length) {
    const n = tree.orphans.length;
    const root = { kind: "root", label: "orphans", title: "nodes whose lead is not registered", state: "",
      extra: [n === 1 ? "1 root" : n + " roots"] };
    next(block(tree.orphans, x, y, root));
  }

  if (tree.routines.length) {
    if (!vertical && x > G.pad) x += G.gap / 2;
    out.headings.push({ x, y: y + 14, text: "routines" });
    let ry = y + G.head;
    for (const g of tree.routines) {
      const key = "routine." + g.kind;
      const head = add({ key: out.boxes.some((b) => b.key === key) ? "" : key, kind: "group", label: g.kind,
        title: key + (g.repo ? " in " + g.repo : ""), state: "", x, y: ry,
        extra: [g.repo || "no repo", (g.depth || 0) + " queued"] });
      ry += G.row;
      for (const i of g.instances || []) {
        const b = add({ key: i.node.path, kind: "instance", label: lastToken(i.node.path), title: i.node.path,
          state: i.state.state, extra: instanceSub(i), x: x + G.indent, y: ry });
        elbow(head, b);
        ry += G.row;
      }
    }
    out.width = Math.max(out.width, x + G.nodeW);
  }
  out.width += G.pad;
  out.height += G.pad;
  return out;
}

// --- drawing --------------------------------------------------------------

function classOf(kind, state) {
  const link = kind === "lead" || kind === "group" || kind === "instance" ? " link" : "";
  return "node " + kind + link + (state ? " st-" + state : "");
}

function subText(state, extra) {
  return [state, ...extra].filter(Boolean).join(" · ");
}

function hrefOf(b) {
  if (b.kind === "lead") return "#/lead/" + b.key;
  if (b.kind === "group" || b.kind === "instance") return "#/routines";
  return "";
}

function drawBox(parent, b) {
  const g = svg("g", { class: classOf(b.kind, b.state), transform: `translate(${b.x},${b.y})` }, parent);
  svg("title", {}, g).textContent = b.title;
  svg("rect", { class: "ring", x: -4, y: -4, width: b.w + 8, height: b.h + 8, rx: 10 }, g);
  svg("rect", { class: "box", width: b.w, height: b.h, rx: 7 }, g);
  svgText({ class: "label", x: 10, y: 17 }, fit(b.label, 22), g);
  const sub = svgText({ class: "sub", x: 10, y: 32 }, subText(b.state, b.extra), g);
  svgText({ class: "check", x: b.w - 22, y: 26 }, "✓", g);
  const href = hrefOf(b);
  if (href) {
    g.setAttribute("tabindex", "0");
    g.setAttribute("role", "link");
    g.setAttribute("aria-label", b.title);
    const go = () => { location.hash = href; };
    g.addEventListener("click", go);
    g.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") { e.preventDefault(); go(); }
    });
  }
  return { g, sub };
}

function render() {
  const t = mesh.tree;
  if (!t) return;
  const L = layout(t, mesh.vertical);
  const root = svg("svg", { width: L.width, height: L.height, viewBox: `0 0 ${L.width} ${L.height}`,
    role: "group", "aria-label": "mesh map" });
  const edges = svg("g", { class: "edges" }, root);
  for (const d of L.edges) svg("path", { class: "tree-edge", d }, edges);
  for (const h of L.headings) svgText({ class: "heading", x: h.x, y: h.y }, h.text, root);
  const nodes = svg("g", { class: "nodes" }, root);
  const drawn = new Map();
  for (const b of L.boxes) {
    const { g, sub } = drawBox(nodes, b);
    if (b.key) drawn.set(b.key, { g, sub, box: b, extra: b.extra, kind: b.kind, state: b.state });
  }
  mesh.layer = svg("g", { class: "messages" }, root);
  mesh.drawn = drawn;
  $("map").replaceChildren(root);
  $("map-empty").hidden = L.boxes.length > 0;
}

// setState restyles a drawn node.
function setState(path, state) {
  const d = mesh.drawn.get(path);
  if (!d || d.kind === "group") return;
  d.state = state;
  d.g.setAttribute("class", classOf(d.kind, state));
  d.sub.textContent = subText(state, d.extra);
}

// curve is a path from box a to box b, leaving and entering on the facing
// sides; boxes in one column bow out to the right.
function curve(a, b) {
  const ay = a.y + a.h / 2, by = b.y + b.h / 2;
  if (Math.abs(a.x - b.x) < a.w / 2) {
    const x1 = a.x + a.w, x2 = b.x + b.w, bow = 36 + Math.abs(by - ay) / 4;
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

// flash draws a message edge from -> to for EDGE_MS with a dot travelling
// along it.
function flash(from, to, kind) {
  const b = mesh.drawn.get(to);
  if (!b || !mesh.layer) return;
  const a = mesh.drawn.get(from);
  const g = svg("g", { class: "msg k-" + (MSG_KINDS.has(kind) ? kind : "message") }, mesh.layer);
  const p = svg("path", { class: "msg-edge", d: a ? curve(a.box, b.box) : entry(b.box) }, g);
  const dot = svg("circle", { class: "msg-dot", r: 5 }, g);
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

let fetchSeq = 0, refetchTimer = 0, refetchFirst = 0;

async function loadTree() {
  const seq = ++fetchSeq, began = Date.now();
  try {
    const r = await fetch("/api/tree", { cache: "no-store" });
    if (!r.ok) throw new Error("/api/tree: " + r.status);
    const v = await r.json();
    if (seq !== fetchSeq) return;
    mesh.tree = { leads: v.leads || [], routines: v.routines || [], orphans: v.orphans || [] };
    render();
    // A live event newer than this read wins over the state it returned.
    for (const [path, s] of mesh.live) {
      if (s.at >= began) setState(path, s.state);
      else mesh.live.delete(path);
    }
  } catch (e) {
    // The next event or poll tries again.
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

function liveState(path, state) {
  mesh.live.set(path, { state, at: Date.now() });
  setState(path, state);
}

// apply acts on a live frame: node and daemon events restyle nodes, inbox
// messages flash an edge, and anything the map cannot place refetches.
function apply(f) {
  const rest = f.subject.replace(/^bm\./, "");
  const p = f.payload && typeof f.payload === "object" ? f.payload : {};
  if (rest.startsWith("inbox.")) {
    const to = rest.slice(6);
    flash(String(p.from || ""), to, String(p.kind || "message"));
    if (!mesh.drawn.has(to) && !to.startsWith("routine.")) refetchSoon();
    return;
  }
  const i = rest.lastIndexOf(".");
  if (i <= 0) return;
  const path = rest.slice(0, i), ev = rest.slice(i + 1);
  if (path.startsWith("daemon.")) {
    if (ev === "silent" && p.to) liveState(String(p.to), "silent");
    return;
  }
  if (EVENT_STATE[ev]) liveState(path, EVENT_STATE[ev]);
  if (ev === "started" || ev === "done" || !mesh.drawn.has(path)) refetchSoon();
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

function drawRecent() {
  recentTimer = 0;
  const items = mesh.recent.map((f) => {
    const d = describe(f);
    const li = document.createElement("li");
    li.className = "ev-" + d.event;
    li.append(span("t", stamp((f.payload && f.payload.at) || f.at)), span("p", d.path), span("e", d.event), span("x", d.text));
    return li;
  });
  $("recent").replaceChildren(...items);
  $("drawer-count").textContent = items.length ? "(" + items.length + ")" : "";
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

function connect() {
  const es = new EventSource("/events");
  es.onopen = () => {
    // Every connection replays the last day, and live frames were missed
    // while it was down.
    mesh.recent = [];
    drawRecent();
    $("conn").textContent = "live";
    refetchSoon();
  };
  es.onerror = () => { $("conn").textContent = "reconnecting"; };
  es.addEventListener("replay", (e) => {
    const f = parseFrame(e.data);
    if (f) remember(f);
  });
  es.addEventListener("live", (e) => {
    const f = parseFrame(e.data);
    if (!f) return;
    remember(f);
    apply(f);
  });
}

// route shows the view the hash names: #/ the map, #/lead/<path> a lead,
// #/routines the routines.
function route() {
  const h = location.hash.replace(/^#\/?/, "");
  let view = "map";
  if (h.startsWith("lead/")) {
    view = "lead";
    let path = h.slice(5);
    try { path = decodeURIComponent(path); } catch (e) { /* shown as written */ }
    $("lead-title").textContent = path;
    $("lead-panel").replaceChildren();
    $("lead-panel").dataset.path = path;
  } else if (h === "routines") {
    view = "routines";
  }
  for (const v of ["map", "lead", "routines"]) $("view-" + v).hidden = v !== view;
  for (const a of document.querySelectorAll("nav a[data-route]")) a.classList.toggle("on", a.dataset.route === view);
}

window.addEventListener("hashchange", route);
route();
loadTree();
connect();
setInterval(loadTree, POLL_MS);
