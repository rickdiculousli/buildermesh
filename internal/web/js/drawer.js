// drawer: the recent-events drawer (its chips, lanes and focus), the
// clamped rows and the tooltip.
import { $, el, keep, kept, phone, span, stamp, svg } from "./core.js";
import { mesh } from "./state.js";

const RECENT = 30;

// hover shows the tip with text() under the mouse while it is on e.
export function hover(e, text) {
  const tip = $("tip");
  e.addEventListener("pointermove", (ev) => showTip(ev, text()));
  e.addEventListener("pointerleave", () => { tip.hidden = true; });
}

// describe turns a frame into {path, event, text}; an inbox frame's also
// carries from and to, its path being "from → to".
export function describe(f) {
  const p = f.payload && typeof f.payload === "object" ? f.payload : { text: String(f.payload) };
  const rest = f.subject.replace(/^bm\./, "");
  if (rest.startsWith("inbox.")) {
    const kind = p.kind && p.kind !== "message" ? p.kind + " " : "";
    const from = p.from || "?", to = rest.slice(6);
    return { path: from + " → " + to, from, to, event: "inbox", text: kind + (p.text || "") };
  }
  const i = rest.lastIndexOf(".");
  const path = i > 0 ? rest.slice(0, i) : rest;
  const event = i > 0 ? rest.slice(i + 1) : "";
  let text = p.text || "";
  if (rest.startsWith("daemon.") && p.to) text = p.to + (text ? " " + text : "");
  if (p.commit) text += (text ? " " : "") + "@" + p.commit;
  return { path, event, text };
}

let recentTimer = 0;

export function remember(f) {
  mesh.recent.unshift(f);
  if (mesh.recent.length > RECENT) mesh.recent.length = RECENT;
  if (!recentTimer) recentTimer = setTimeout(drawRecent, 50);
}

// frameKey names a frame for kept: its subject, time and words (frames
// carry no sequence; two of one subject in one second differ by their words,
// or read the same).
export function frameKey(f) {
  const p = f.payload && typeof f.payload === "object" ? f.payload : {};
  return f.subject + "@" + (p.at || f.at || "") + "#" + (p.text || "");
}

// The drawer's lanes: each path that owns a row (an inbox row's sender) gets
// one, in order of first appearance, newest first, LANE_PX apart; past the
// last lane the rest share it. A chip's hue is its lead tree's (a hash of
// the path's top segment over HUES), a child a lighter step; routines and
// the daemon are grey.
const LANE_PX = 14, LANES = 6, PHONE_LANES = 3;

const HUES = [172, 212, 262, 318, 28, 46, 138, 350];

const NEUTRAL = new Set(["routine", "daemon", "?"]);

// recentFocus is the path the drawer lights: hover while the mouse is on a
// chip or its line, else pin, set by a click and kept across redraws. mute
// is a path just unpinned, which the mouse still on it doesn't relight.
export const recentFocus = { pin: "", hover: "", mute: "" };

let recentTW = 0; // the widest time column seen, for the lanes' origin

// paint gives e the hue of path: --h, --l (the step, 0 to 2) and --sat (0
// for grey), which map.css's .hue turns into --c-fg and --c-bg.
export function paint(e, path) {
  const segs = String(path).split(".");
  let h = 0, sat = 0;
  if (!NEUTRAL.has(segs[0])) {
    let n = 2166136261; // FNV-1a
    for (const c of segs[0]) n = Math.imul(n ^ c.codePointAt(0), 16777619);
    h = HUES[(n >>> 0) % HUES.length];
    sat = 1;
  }
  e.classList.add("hue");
  e.style.setProperty("--h", h);
  e.style.setProperty("--l", Math.min(segs.length - 1, 2));
  e.style.setProperty("--sat", sat);
}

// chipLabel drops the nearest ancestor's prefix that owns a lane: "eng",
// not "buildermesh-round-1.eng", when the round has rows of its own.
function chipLabel(path, owners) {
  const segs = path.split(".");
  for (let i = segs.length - 1; i > 0; i--) {
    if (owners.has(segs.slice(0, i).join("."))) return segs.slice(i).join(".");
  }
  return path;
}

function chip(path, owners, cls) {
  const b = el("button", cls, chipLabel(path, owners));
  b.type = "button";
  b.title = path;
  b.dataset.path = path;
  paint(b, path);
  return b;
}

export function drawRecent() {
  recentTimer = 0;
  const rows = mesh.recent.map((f) => {
    const d = describe(f);
    return { f, d, who: d.from === undefined ? d.path : d.from };
  });
  const order = [...new Set(rows.map((r) => r.who))];
  const owners = new Set(order);
  const max = phone.matches ? PHONE_LANES : LANES;
  const items = rows.map(({ f, d, who }) => {
    const li = document.createElement("li");
    li.className = "ev-" + d.event;
    li.dataset.who = who;
    const c = chip(who, owners, "chip");
    c.style.setProperty("--lane", Math.min(order.indexOf(who), max - 1));
    li.append(span("t", stamp((f.payload && f.payload.at) || f.at)), c);
    if (d.to !== undefined) {
      li.dataset.to = d.to;
      li.append(span("arrow", "→"), chip(d.to, owners, "chip small"));
    }
    li.append(span("e", d.event), span("x", d.text));
    clampRow(li, frameKey(f));
    return li;
  });
  const lanes = order.slice(0, max).map((p, i) => {
    const e = el("div", "lane");
    e.style.setProperty("--lane", i);
    if (order.length > max && i === max - 1) {
      e.classList.add("shared");
    } else {
      e.dataset.path = p;
      paint(e, p);
    }
    return e;
  });
  $("lanes").replaceChildren(...lanes);
  $("recent-box").style.setProperty("--lanes", lanes.length);
  $("recent").replaceChildren(...items);
  $("drawer-count").textContent = items.length ? "(" + items.length + ")" : "";
  const pin = recentFocus.pin;
  if (pin && !rows.some((r) => r.who === pin || r.d.to === pin)) recentFocus.pin = "";
  focusRecent();
  measureTime();
}

// measureTime sets --tw, the time column's width the lanes start after, to
// the widest seen; a closed drawer measures nothing, so opening it measures
// again.
export function measureTime() {
  let w = 0;
  for (const t of $("recent").querySelectorAll(".t")) w = Math.max(w, t.getBoundingClientRect().width);
  if (w <= recentTW) return;
  recentTW = Math.ceil(w);
  $("recent-box").style.setProperty("--tw", recentTW + "px");
}

// focusRecent dims every row that neither is the focused path's nor names
// it as recipient, and lights its lane.
function focusRecent() {
  const f = recentFocus.hover || recentFocus.pin;
  for (const li of $("recent").children) li.classList.toggle("dim", !!f && li.dataset.who !== f && li.dataset.to !== f);
  for (const e of $("lanes").children) e.classList.toggle("on", !!f && e.dataset.path === f);
  for (const b of $("recent").querySelectorAll(".chip")) b.setAttribute("aria-pressed", String(b.dataset.path === recentFocus.pin));
}

// hoverPath is the path under the mouse: a chip's, or a lane's when the
// mouse is within a few pixels of its line and not on a row's words.
export function hoverPath(ev) {
  const c = ev.target.closest(".chip");
  if (c) return c.dataset.path;
  if (ev.target.closest("span")) return "";
  for (const e of $("lanes").children) {
    if (!e.dataset.path) continue;
    const r = e.getBoundingClientRect();
    if (Math.abs(ev.clientX - (r.left + r.width / 2)) <= 5) return e.dataset.path;
  }
  return "";
}

export function setHover(p) {
  if (p === recentFocus.mute) p = "";
  else recentFocus.mute = "";
  if (p === recentFocus.hover) return;
  recentFocus.hover = p;
  focusRecent();
}

// unpin drops the pin; the mouse still on that chip doesn't relight it.
export function unpin() {
  if (recentFocus.hover === recentFocus.pin) recentFocus.hover = "";
  recentFocus.mute = recentFocus.pin;
  recentFocus.pin = "";
  focusRecent();
}

// pinChip pins the path of a clicked chip, moves the pin to it from
// another, or unpins it when it is pinned. Touch has no hover: a tap pins.
export function pinChip(ev) {
  const c = ev.target.closest(".chip");
  if (!c) return;
  if (recentFocus.pin === c.dataset.path) {
    unpin();
    return;
  }
  recentFocus.pin = c.dataset.path;
  focusRecent();
}

// --- clamped rows -----------------------------------------------------------

// clampRow marks a row clamped to three lines until tapped; key names it in
// kept.
export function clampRow(e, key) {
  e.classList.add("clamp");
  e.dataset.key = key;
  if (kept(key)) e.classList.add("open");
}

// toggleRows opens or closes the clamped row a tap lands on (a band row's
// words from anywhere in the row), unless the tap is a link, a fold or a
// chip.
export function toggleRows(ev) {
  if (ev.target.closest("a, summary, button, .chip")) return;
  const row = ev.target.closest(".clamp, .need");
  const e = row && row.classList.contains("need") ? row.querySelector(".clamp") : row;
  if (!e || !this.contains(e)) return;
  const open = e.classList.toggle("open");
  keep(e.dataset.key, open);
}

// showTip puts the tip with text beside the pointer of ev.
function showTip(ev, text) {
  const tip = $("tip");
  tip.textContent = text;
  tip.hidden = false;
  const w = tip.offsetWidth, h = tip.offsetHeight;
  tip.style.left = Math.max(4, Math.min(ev.clientX + 14, window.innerWidth - w - 4)) + "px";
  tip.style.top = (ev.clientY + 14 + h > window.innerHeight ? ev.clientY - h - 8 : ev.clientY + 14) + "px";
}

// crosshair lays a hit area over a plot (P its padding in a W x H svg):
// at(x) gives the crosshair's x and the tip's words for a pointer at x,
// or null for none.
export function crosshair(root, P, W, H, at) {
  const line = svg("line", { class: "xhair", x1: 0, x2: 0, y1: P.t, y2: H - P.b, visibility: "hidden" }, root);
  const hit = svg("rect", { class: "hit", x: P.l, y: 0, width: Math.max(0, W - P.l - P.r), height: H }, root);
  const hide = () => {
    $("tip").hidden = true;
    line.setAttribute("visibility", "hidden");
  };
  hit.addEventListener("pointermove", (ev) => {
    const r = at(ev.clientX - root.getBoundingClientRect().left);
    if (!r) return hide();
    line.setAttribute("x1", r.x);
    line.setAttribute("x2", r.x);
    line.setAttribute("visibility", "visible");
    showTip(ev, r.text);
  });
  hit.addEventListener("pointerleave", hide);
}
