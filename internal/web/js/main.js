// main: the console's views and live-frame tables, its timers and start-up.
// The only module with top-level side effects. Every mesh value goes in
// through textContent or setAttribute.
import { $, every, phone, tick } from "./core.js";
import { DAEMON_STATE, EVENT_STATE, TICK, hubNow, known, pages, setWin, storedWindow, tl, tok } from "./state.js";
import { connect } from "./live.js";
import { drawRecent, hoverPath, measureTime, pinChip, recentFocus, setHover, toggleRows, unpin } from "./drawer.js";
import { drawTimeline, liveJob, liveLane, timeline } from "./timeline.js";
import { drawOverview, liveDigest, liveState, stripLoad, tree } from "./overview.js";
import { drawTokens, usage } from "./tokens.js";
import { drawSpecs, specsLoad } from "./specs.js";
import { routines } from "./routines.js";
import { drawHosts, hosts, hostsPoll } from "./hosts.js";
import { drawLead, enterLead, lead, leadLandPoll, touchesLead } from "./lead.js";
import { landPoll, landTick, landing } from "./landing.js";
import { drawSession, enterSession, sessionPage } from "./session.js";

const POLL_MS = 30000; // unread counts and runners change without an event
const STRIP_MS = 60000; // the server caches the quota 5 min; never poll faster
const ROUTINES_MS = 30000;
const NOW_MS = 30000; // the now line moves this often
const HOSTS_MS = 10000; // the hosts tab's live poll; hostsPoll slows it to 60 s
const LANDING_MS = 5000; // a landing's rows and card while one runs

// VIEWS are the console's views: the hash that shows each (exact, or a
// prefix when it ends in "/", whose rest enter() reads), its section,
// whether the window buttons show, how it draws, the reads it needs
// (route, the reconnect and a window change run them) and those of them
// the 60 s re-read covers. The first is the default.
const VIEWS = [
  { name: "map", hash: "", section: "view-map", windows: true, draw: () => drawOverview(),
    reads: () => [timeline, usage, specsLoad, landing], poll: () => [timeline, usage, specsLoad, landing] },
  { name: "timeline", hash: "timeline", section: "view-timeline", windows: true, draw: () => drawTimeline(),
    reads: () => [timeline, usage], poll: () => [timeline, usage] },
  { name: "tokens", hash: "tokens", section: "view-tokens", windows: true, draw: () => drawTokens(),
    reads: () => [usage], poll: () => [usage] },
  { name: "specs", hash: "specs", section: "view-specs", windows: false, draw: () => drawSpecs(),
    reads: () => [specsLoad], poll: () => [specsLoad] },
  { name: "routines", hash: "routines", section: "view-routines", windows: false, draw: () => {},
    reads: () => [routines], poll: () => [] },
  { name: "hosts", hash: "hosts", section: "view-hosts", windows: true, draw: () => drawHosts(),
    reads: () => [hosts], poll: () => [] },
  { name: "lead", hash: "lead/", section: "view-lead", windows: false, enter: (rest, q) => enterLead(rest, q), draw: () => drawLead(),
    reads: () => [lead], poll: () => [] },
  { name: "session", hash: "session/", section: "view-session", windows: false, enter: (rest) => enterSession(rest), draw: () => drawSession(),
    reads: () => [sessionPage], poll: () => [] },
];

function shown() {
  return VIEWS.find((v) => v.name === pages.view) || VIEWS[0];
}

function runReads(v) {
  for (const l of v.reads()) l.run();
}

// drawAll draws the view shown.
export function drawAll() {
  tl.lastDraw = Date.now();
  shown().draw();
}

// route shows the view the hash names (#/ the overview, #/timeline,
// #/tokens, #/specs, #/routines, #/hosts, #/lead/<path>?vs=<path>,
// #/session/<host>/<id>), takes its ?w=<window>, and runs its reads.
function route() {
  const [h, q] = location.hash.replace(/^#\/?/, "").split("?");
  const params = new URLSearchParams(q || "");
  setWin(params.get("w"), false);
  const v = VIEWS.find((x) => x.hash.endsWith("/") ? h.startsWith(x.hash) : h === x.hash) || VIEWS[0];
  pages.lead = "";
  pages.session = "";
  if (v.enter) v.enter(h.slice(v.hash.length), params);
  pages.view = v.name;
  for (const x of VIEWS) $(x.section).hidden = x !== v;
  $("windows").hidden = !v.windows;
  showWindow();
  drawAll(); // the width may have changed while hidden
  runReads(v);
  for (const a of document.querySelectorAll("nav a[data-route]")) a.classList.toggle("on", a.dataset.route === v.name);
}

function showWindow() {
  for (const b of document.querySelectorAll("#windows button")) b.setAttribute("aria-pressed", String(b.dataset.w === tl.win));
}

// frameOf reads a live frame: its subject less bm. (rest), payload (p),
// time in unix seconds (at), its kind (inbox, feedback, daemon, routine, a
// node's, or "" for none), and for the last three its path, event and
// words. routine marks a routine's events and its inbox.
function frameOf(f) {
  const rest = f.subject.replace(/^bm\./, "");
  const p = f.payload && typeof f.payload === "object" ? f.payload : {};
  const at = Date.parse(String(p.at || f.at || "")) / 1000;
  const fr = { f, rest, p, at, kind: "", path: "", ev: "", text: String(p.text || ""),
    routine: rest.startsWith("routine.") || rest.startsWith("inbox.routine.") };
  if (rest.startsWith("inbox.")) fr.kind = "inbox";
  else if (rest.startsWith("feedback.")) fr.kind = "feedback"; // a family's queue, not a node
  else if (rest.lastIndexOf(".") > 0) {
    const i = rest.lastIndexOf(".");
    fr.path = rest.slice(0, i);
    fr.ev = rest.slice(i + 1);
    fr.kind = fr.path.startsWith("daemon.") ? "daemon" : fr.path.startsWith("routine.") ? "routine" : "node";
  }
  return fr;
}

// FRAMES are what a live frame does, in order: every row whose match holds
// runs. Node and daemon events move the node's state and its lane, a job
// sent marks its sender's lane, a node the tree doesn't have refetches it,
// a digest fills the strip, and the open page re-reads what it touches.
const LAND_EVENTS = new Set(["landing", "land-refused", "land-withdrawn"]);
const FRAMES = [
  { match: (fr) => fr.kind === "inbox" && fr.p.kind === "job" && fr.p.from,
    effect: (fr) => liveLane(String(fr.p.from), "", fr.at, "job", String(fr.p.text || (fr.p.job && fr.p.job.kind) || "")) },
  { match: (fr) => fr.kind === "daemon" && fr.p.to && DAEMON_STATE[fr.ev],
    effect: (fr) => {
      const to = String(fr.p.to), st = DAEMON_STATE[fr.ev];
      liveState(to, st, fr.text, fr.p.at || fr.f.at);
      liveLane(to, st, fr.at, TICK[fr.ev] || "", fr.text);
    } },
  { match: (fr) => fr.kind === "routine", effect: (fr) => liveJob(fr.path, fr.ev, fr.text, fr.at) },
  { match: (fr) => fr.kind === "node" && EVENT_STATE[fr.ev],
    effect: (fr) => {
      liveState(fr.path, EVENT_STATE[fr.ev], fr.text, fr.p.at || fr.f.at);
      liveLane(fr.path, EVENT_STATE[fr.ev], fr.at, TICK[fr.ev] || "", fr.text);
    } },
  { match: (fr) => fr.kind === "node" && (fr.ev === "started" || fr.ev === "done" || !known(fr.path)), effect: () => tree.soon() },
  { match: (fr) => /^daemon\.[^.]+\.digest$/.test(fr.rest), effect: (fr) => liveDigest(fr.f) },
  { match: (fr) => pages.view === "map" && (fr.kind === "node" && fr.ev === "land" || fr.kind === "daemon" && LAND_EVENTS.has(fr.ev)),
    effect: () => landing.soon() },
  { match: (fr) => pages.view === "lead" && touchesLead(fr.rest, fr.p), effect: () => lead.soon() },
  { match: (fr) => pages.view === "routines" && fr.routine, effect: () => routines.soon() },
  { match: (fr) => fr.routine, effect: () => tree.soon() }, // the Routines tile
];

function frame(f) {
  const fr = frameOf(f);
  for (const r of FRAMES) if (r.match(fr)) r.effect(fr);
}

// showClock writes the hub's clock in UTC.
function showClock() {
  const d = new Date(hubNow() * 1000).toISOString();
  $("clock").textContent = d.slice(5, 10) + " " + d.slice(11, 16) + "Z";
}

phone.addEventListener("change", drawRecent); // the lane count follows the width
// Timelines fit the width: redrawn when it changes (not the height: a
// phone's toolbar hiding on scroll resizes it).
let resizeTimer = 0, lastWidth = window.innerWidth;
window.addEventListener("resize", () => {
  if (window.innerWidth === lastWidth) return;
  lastWidth = window.innerWidth;
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(drawAll, 100);
});
for (const id of ["needs", "recent", "lead-panel"]) $(id).addEventListener("click", toggleRows);
$("lead-vs").addEventListener("change", (ev) => {
  if (!tok.lead || tok.lead.path !== pages.lead) return;
  tok.lead.vs = ev.target.value;
  lead.run();
});
$("recent").addEventListener("click", pinChip);
$("recent").addEventListener("pointermove", (ev) => { if (ev.pointerType === "mouse") setHover(hoverPath(ev)); });
$("recent").addEventListener("pointerleave", () => setHover(""));
$("drawer").addEventListener("toggle", measureTime);
$("windows").addEventListener("click", (ev) => {
  const b = ev.target.closest("button[data-w]");
  if (!b || !setWin(b.dataset.w, true)) return;
  showWindow();
  drawAll();
  runReads(shown());
});
document.addEventListener("keydown", (ev) => { if (ev.key === "Escape" && recentFocus.pin) unpin(); });
window.addEventListener("hashchange", route);

tl.win = storedWindow() || "6h";
route();
tree.run();
// Every connection replays the last day, and live frames were missed while
// it was down: the tree and the view's reads are read again.
connect({ open: () => { tree.run(); runReads(shown()); }, frame });
stripLoad.run();
every(POLL_MS, () => tree.run(), { hidden: true });
every(STRIP_MS, () => stripLoad.run(), { hidden: true });
// The 60 s re-read: the view's polled reads, and any read of it that failed.
every(2 * POLL_MS, () => {
  const v = shown(), poll = v.poll();
  for (const l of v.reads()) if (poll.includes(l) || l.state.phase === "failed") l.run();
});
every(ROUTINES_MS, () => { if (pages.view === "routines") routines.run(); });
every(HOSTS_MS, hostsPoll);
every(NOW_MS, drawAll, { hidden: true });
every(LANDING_MS, () => { landPoll(); leadLandPoll(); });
every(1000, () => { tick(); landTick(); showClock(); }, { hidden: true });
showClock();
