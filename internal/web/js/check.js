// check: the ?check=1 self-check title.
import { $, ICON, check } from "./core.js";
import { STATE_NAMES, pages, tl } from "./state.js";

// tallyCheck keeps, under ?check=1, the counts of the timeline the view shows.
export function tallyCheck(box, c) {
  if (!check || box.closest(".view").hidden) return;
  check.lanes = c.lanes;
  check.segs = c.segs;
  check.ticks = c.ticks;
  checkSoon();
}

// tally keeps, under ?check=1, a panel's count when its view is shown.
export function tally(box, key, n) {
  if (!check || !box || box.closest(".view").hidden) return;
  check[key] = n;
  checkSoon();
}

let checkTimer = 0;

export function checkSoon() {
  if (!checkTimer) checkTimer = setTimeout(() => { checkTimer = 0; selfCheck(); }, 100);
}

// selfCheck writes, under ?check=1, whether the page scrolls sideways, the
// smallest tap target, the tree reads, the timeline drawn (lanes, segments
// per state, ticks), the KPI tiles, the usage panels' counts, the band's
// flag rows (runaway ones too), the outliers' points, the burndowns' marks, the host cards and process rows,
// the band's rows (held and landing ones too), the older fold, the digest,
// the state table's gaps (stateGaps) and the connection history into document.title.
function selfCheck() {
  const sw = document.documentElement.scrollWidth, iw = window.innerWidth;
  let tap = Infinity;
  for (const e of document.querySelectorAll("nav a, #windows button, .drawer summary, header h1 a, .tap")) {
    const r = e.getBoundingClientRect();
    if (r.width || r.height) tap = Math.min(tap, r.height, r.width);
  }
  const c = check;
  const segs = Object.keys(c.segs).sort().map((s) => s + ":" + c.segs[s]).join(",") || "none";
  const n = (sel) => document.querySelectorAll(sel).length;
  document.title = `bmesh ${sw <= iw ? "ok" : "SCROLLS"} view=${pages.view} scrollWidth=${sw} innerWidth=${iw} minTap=${Math.round(tap)}` +
    (pages.view === "lead" ? ` firstCard=${firstCard()}` : "") +
    ` window=${tl.win} trees=${c.trees} lanes=${c.lanes} segs=${segs} ticks=${c.ticks} kpis=${n("#kpis .tile")}` +
    ` roles=${c.roles} agents=${c.agents} ctx=${c.ctx} quota=${c.quota}` +
    ` flags=${n("#needs .need.flag")} flagsRunaway=${n("#needs .need.flag.fl-runaway")} outliers=${c.outliers} burndown=${c.burndown}` +
    ` hosts=${c.hosts} procs=${c.procs}` +
    ` conn=${c.conn.join(">")} replay=${c.replay} live=${c.live}` +
    ` band=${$("needs").hidden ? "hidden" : $("needs").children.length}` +
    ` bandHeld=${n("#needs .need.st-held")} bandLanding=${n("#needs .need.st-landing")}` +
    ` older=${n("#needs details.older .need")} digest=${$("strip").querySelector(".digest") ? 1 : 0} stateGaps=${stateGaps()} landDup=${landDup()}`;
}

// landDup counts the land and landing rows, in the drawer and in the lead
// page's timeline card, whose text equals the previous (older) one's for the
// same node; the rows are newest first.
function landDup() {
  let dups = 0;
  for (const ol of [$("recent"), ...document.querySelectorAll("#lead-panel .card ol.rows")]) {
    const seen = new Map();
    for (const li of [...ol.querySelectorAll("li[data-land]")].reverse()) {
      if (seen.get(li.dataset.land) === li.dataset.landText) dups++;
      seen.set(li.dataset.land, li.dataset.landText);
    }
  }
  return dups;
}

// stateGaps counts the states in the node-state table with no ICON, or no
// --<state> colour token in map.css (idle and done draw from --tm and
// --done-*).
function stateGaps() {
  const css = getComputedStyle(document.documentElement);
  return STATE_NAMES.filter((s) => !ICON[s] || !["idle", "done"].includes(s) && !css.getPropertyValue("--" + s).trim()).length;
}

// firstCard names the lead page's card drawn highest (leftmost on a tie),
// spaces as "_": CSS order, not the DOM, decides it in one column.
function firstCard() {
  let best = null, top = Infinity, left = Infinity;
  for (const e of document.querySelectorAll("#lead-panel .card")) {
    const r = e.getBoundingClientRect();
    if (!r.height) continue;
    if (r.top < top - 0.5 || Math.abs(r.top - top) <= 0.5 && r.left < left) {
      best = e;
      top = r.top;
      left = r.left;
    }
  }
  return best ? String(best.dataset.card).replace(/\s+/g, "_") : "none";
}
