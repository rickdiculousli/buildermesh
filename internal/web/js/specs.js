// specs: the specs read, the burndowns and the specs tab.
import { $, chartRoot, el, every, fetchJSON, hm, legendOf, loader, md, readState, svg, svgText, timeAxis, yAxis } from "./core.js";
import { hubNow, specs } from "./state.js";
import { crosshair, hover } from "./drawer.js";
import { tally } from "./check.js";
import { drawAll } from "./main.js";

export const specsLoad = loader(() => fetchJSON("/api/specs"), {
  show: (r) => {
    if (r.body) {
      r.body.repos = r.body.repos || [];
      r.body.families = r.body.families || [];
      specs.data = r.body;
    }
    drawAll();
  },
});

// countAxis is a count axis's top: a multiple of four (whole quarters), at
// least four.
function countAxis(v) {
  return Math.max(4, Math.ceil(v / 4) * 4);
}

// repoBurn draws a repo's 30 days into box: a bar per day for the specs
// landed that day, a line for those open at the day's end. Returns the
// marks drawn.
export function repoBurn(box, r) {
  const days = r.days || [];
  const leg = legendOf([["bd-l", "landed that day"], ["bd-o", "open at day's end"]]);
  if (!days.length) {
    box.replaceChildren(leg, el("p", "empty", "No spec history read for " + r.repo + "."));
    return 0;
  }
  const W = Math.max(box.clientWidth, 240), H = 190, P = { l: 30, r: 8, t: 8, b: 20 };
  const n = days.length, bw = (W - P.l - P.r) / n;
  const yMax = countAxis(Math.max(...days.map((d) => Math.max(d.landed.length, d.open))));
  const y = (v) => H - P.b - v / yMax * (H - P.t - P.b), cx = (i) => P.l + (i + 0.5) * bw;
  const root = chartRoot(W, H, r.repo + ": specs per day");
  yAxis(root, y, yMax, P, W, (v) => String(Math.round(v)));
  const every = Math.ceil(52 / bw);
  for (let i = n - 1; i >= 0; i -= every) svgText({ class: "axis", x: i === n - 1 ? W - P.r : cx(i), y: H - 6, "text-anchor": i === n - 1 ? "end" : "middle" }, days[i].day.slice(5), root);
  let marks = 0;
  days.forEach((d, i) => {
    if (!d.landed.length) return;
    svg("rect", { class: "bar bd-l", x: cx(i) - bw * 0.35, y: y(d.landed.length), width: Math.max(1, bw * 0.7), height: y(0) - y(d.landed.length), rx: 1.5 }, root);
    marks++;
  });
  svg("path", { class: "openline bd-o", d: "M" + days.map((d, i) => cx(i).toFixed(1) + "," + y(d.open).toFixed(1)).join("L") }, root);
  marks++;
  if (bw >= 8) for (let i = 0; i < n; i++) svg("circle", { class: "opendot bd-o", cx: cx(i), cy: y(days[i].open), r: 2 }, root);
  crosshair(root, P, W, H, (px) => {
    const i = Math.max(0, Math.min(n - 1, Math.floor((px - P.l) / bw)));
    const d = days[i];
    return { x: cx(i), text: d.day + "\nlanded " + d.landed.length + (d.landed.length ? ": " + d.landed.join(", ") : "") + "\nopen at day's end " + d.open };
  });
  box.replaceChildren(leg, root);
  return marks;
}

// familyBurn draws a build family's burndown into box: a step line of its
// specs still open from its first lead's start, a step down at each lead's
// done, its land refusals as ✕ on the line, a dashed now while live.
// Returns the marks drawn.
export function familyBurn(box, f) {
  const steps = f.steps || [];
  if (!steps.length) {
    box.replaceChildren(el("p", "empty", "No leads in " + f.family + "."));
    return 0;
  }
  const now = Math.max(hubNow(), steps[steps.length - 1][0]);
  const end = f.live ? now : steps[steps.length - 1][0];
  const from = f.start, to = Math.max(end, from + 600) + (Math.max(end, from + 600) - from) * 0.02;
  const W = Math.max(box.clientWidth, 240), H = 190, P = { l: 30, r: 8, t: 8, b: 20 };
  const yMax = countAxis(steps[0][1]);
  const x = (t) => P.l + (Math.max(from, Math.min(to, t)) - from) / Math.max(1, to - from) * (W - P.l - P.r);
  const y = (v) => H - P.b - v / yMax * (H - P.t - P.b);
  const root = chartRoot(W, H, f.family + ": specs open");
  yAxis(root, y, yMax, P, W, (v) => String(Math.round(v)));
  timeAxis(root, x, from, to, P.t, H - P.b, H);
  const openAt = (t) => {
    let v = steps[0][1];
    for (const [at, o] of steps) if (at <= t) v = o;
    return v;
  };
  let d = "M" + x(steps[0][0]).toFixed(1) + "," + y(steps[0][1]).toFixed(1);
  for (const [at, o] of steps.slice(1)) d += "H" + x(at).toFixed(1) + "V" + y(o).toFixed(1);
  d += "H" + x(end).toFixed(1);
  svg("path", { class: "stepline", d }, root);
  let marks = 1;
  if (f.live) svg("line", { class: "now", x1: x(now), x2: x(now), y1: P.t, y2: H - P.b }, root);
  const done = (f.leads || []).filter((l) => l.done).sort((a, b) => a.done - b.done);
  crosshair(root, P, W, H, (px) => {
    const t = Math.min(end, from + (px - P.l) / (W - P.l - P.r) * (to - from));
    const by = done.filter((l) => l.done <= t);
    return { x: x(t), text: md(t) + " " + hm(t) + "  " + openAt(t) + " open" + (by.length ? "\nlast done: " + by[by.length - 1].path : "") };
  });
  // Over the crosshair's hit area, so each ✕ has its own tip.
  for (const [at, path, text] of f.refused || []) {
    const m = svgText({ class: "tick refused", x: x(at), y: y(openAt(at)) + 4, "text-anchor": "middle" }, "✕", root);
    hover(m, () => md(at) + " " + hm(at) + "  " + path + "  land refused" + (text ? "\n" + text : ""));
    marks++;
  }
  box.replaceChildren(root);
  return marks;
}

// familyNote is a family's subtitle: its repo, leads, start and state.
export function familyNote(f) {
  const leads = (f.leads || []).length;
  return f.family + " · " + f.repo + " · " + leads + (leads === 1 ? " lead" : " leads") + " from " + md(f.start) + " " + hm(f.start) + "Z · " +
    (f.live ? "live" : "finished") + " · step down: a lead done · ✕ land refused" + (f.live ? " · dashed: now" : "");
}

// drawSpecs draws the specs tab: every repo's 30 days, then every family
// started in 30 days, newest first.
export function drawSpecs() {
  const repos = $("specs-repos"), fams = $("specs-families");
  const d = specs.data;
  if (!d) return repos.replaceChildren(el("p", "empty", readState(specsLoad, "specs")));
  const panel = (title, note) => {
    const box = el("div", "chart");
    return { box, p: el("section", "tpanel", null, el("h2", null, title, el("small", null, note)), box) };
  };
  const rs = d.repos.map((r) => Object.assign(panel(r.repo, "specs landed per day (bars) and open at each day's end (line), 30 days"), { r }));
  const fs = d.families.map((f) => Object.assign(panel("Burndown · " + f.family, familyNote(f)), { f }));
  repos.replaceChildren(...rs.map((x) => x.p));
  if (!rs.length) repos.replaceChildren(el("p", "empty", "No repos configured."));
  fams.replaceChildren(...fs.map((x) => x.p));
  if (!fs.length) fams.replaceChildren(el("p", "empty", "No build families started in 30 days."));
  let marks = 0;
  for (const x of rs) marks += repoBurn(x.box, x.r);
  for (const x of fs) marks += familyBurn(x.box, x.f);
  tally(repos, "burndown", marks);
}
