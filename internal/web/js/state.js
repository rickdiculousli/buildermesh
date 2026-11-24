// state: the page's shared state, the node-state table as the page reads
// it (from /js/mesh-table.js, rendered from internal/mesh) and stateOf.
import { STATES, TICKS } from "./mesh-table.js";

const stateRows = Object.entries(STATES);
const byEvent = (key) => Object.fromEntries(stateRows.flatMap(([s, r]) => r[key].map((e) => [e, s])));
// NEED orders the band's rows and the timeline's lanes: what needs a hand,
// then what runs, what has no state yet, what is finished.
const NEED = Object.fromEntries(stateRows.map(([s, r]) => [s, r.need]));
// EVENT_STATE is the state a node's own event sets; DAEMON_STATE the state
// a daemon event sets on the node it is addressed to (to).
export const EVENT_STATE = byEvent("events");
export const DAEMON_STATE = byEvent("daemon");
// LIVE are the states of a node at work or waiting: the Live agents tile.
export const LIVE = new Set(stateRows.filter(([, r]) => r.live).map(([s]) => s));
// NEEDS are the states the band shows: waiting on the operator, or quiet.
export const NEEDS = new Set(stateRows.filter(([, r]) => r.needs).map(([s]) => s));
// TICK is the tick an event draws on its lane.
export const TICK = TICKS;
// STATE_NAMES are the table's states.
export const STATE_NAMES = Object.keys(STATES);

export const mesh = {
  tree: null, // the last /api/tree
  nodes: new Map(), // path -> the tree's node (leads, rounds, orphans and everything under them)
  // path -> {state, text, since, at}: states set by live events, with the
  // event's words and time (since) and when the page saw it (at).
  live: new Map(),
  recent: [], // newest first
  staleMs: 0, // [serve] stale_after from /api/tree; 0 = nothing goes stale
  host: "", // the serving daemon's host
  skew: 0, // the hub's clock less this browser's, in ms
};

// hubNow is the hub's clock in unix seconds.
export function hubNow() {
  return (Date.now() + mesh.skew) / 1000;
}

function startOf(t) {
  const v = Date.parse(t.node.started_at || "");
  return isNaN(v) ? Infinity : v;
}

// byStart orders nodes by started_at (unknown last), ties by path.
export function byStart(a, b) {
  const x = startOf(a), y = startOf(b);
  if (x !== y) return x < y ? -1 : 1;
  return a.node.path < b.node.path ? -1 : a.node.path > b.node.path ? 1 : 0;
}

export function needRank(state) {
  const r = NEED[state];
  return r === undefined ? NEED.idle : r;
}

// isStale reports a needs-you state (NEEDS) older than [serve] stale_after.
export function isStale(st) {
  if (!mesh.staleMs || !NEEDS.has(st.state)) return false;
  const at = Date.parse(st.at || "");
  return !isNaN(at) && Date.now() - at > mesh.staleMs;
}

// setTree takes a tree read: its nodes by path, for stateOf and known.
export function setTree(tree) {
  mesh.tree = tree;
  mesh.nodes = new Map();
  const walk = (t) => {
    mesh.nodes.set(t.node.path, t);
    for (const c of t.children || []) walk(c);
  };
  for (const t of tree.leads) walk(t);
  for (const t of tree.orphans) walk(t);
}

// stateOf is a node's shown state, {state, text, at}: the tree's, with a
// live event winning while it is newer than the read (the tree read drops
// the older ones).
export function stateOf(path) {
  const live = mesh.live.get(path);
  if (live) return { state: live.state, text: live.text, at: live.since };
  const t = mesh.nodes.get(path);
  return (t && t.state) || {};
}

// eachNode calls fn with every non-routine node of the loaded tree (leads,
// rounds, orphans and everything under them) and its shown state.
export function eachNode(fn) {
  for (const [path, t] of mesh.nodes) fn(t, stateOf(path).state);
}

// known reports whether the loaded tree has path.
export function known(path) {
  return mesh.nodes.has(path);
}

// WINDOWS are the spans the window chips pick, in seconds.
export const WINDOWS = { "1h": 3600, "6h": 6 * 3600, "24h": 24 * 3600, "7d": 7 * 86400 };

const WINDOW_KEY = "bmesh.window";

export const tl = {
  win: "6h",
  data: null, // the last /api/timeline: {from, to, lanes}
  lead: null, // the lead page's lanes: {path, data}
  drawTimer: 0,
  lastDraw: 0,
};

export function storedWindow() {
  try {
    const w = localStorage.getItem(WINDOW_KEY);
    return WINDOWS[w] ? w : "";
  } catch (e) {
    return "";
  }
}

// setWin is the window's one writer: it moves tl.win to w, drops the loaded
// timeline and stores w; with hash (a window button) it also rewrites the
// hash of the overview, timeline or tokens tab. It reports whether w moved.
export function setWin(w, hash) {
  if (!WINDOWS[w] || w === tl.win) return false;
  tl.win = w;
  tl.data = null;
  try {
    localStorage.setItem(WINDOW_KEY, w);
  } catch (e) {
    // Storage off: the hash still carries it.
  }
  if (hash) {
    const base = pages.view === "timeline" || pages.view === "tokens" ? "#/" + pages.view : "#/";
    history.replaceState(null, "", base + "?w=" + w);
  }
  return true;
}

export const pages = { view: "map", lead: "", leadShown: "", session: "" };

// every is the daemon's digest period ([notify] digest_every) in ms.
export const strip = { quota: [], digest: null, every: 0 };

export const tok = {
  data: null, // the last /api/usage; usageNow says whether it is the window shown
  sel: "", // the agent the overview's context panel shows; "" = the default
  ctx: null, // {agent, data}: that panel's /api/usage/agent
  sort: { key: "read", dir: -1 }, // the tokens tab's agents table
  lead: null, // {path, vs, data}: the lead page's /api/usage/agent
  peers: null, // {at, agents}: the week's agents, for the compare select
  session: null, // {agent, data}: the session page's
};

// specs is the last /api/specs.
export const specs = { data: null };
