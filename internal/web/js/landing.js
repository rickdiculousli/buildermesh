// landing: what /api/landing says about a landing as it runs: the
// overview's rows under the needs-you band, the lead page's landing card,
// and the per-second ticking of their times on the hub's clock.
import { $, check, dur, el, fetchJSON, loader } from "./core.js";
import { hubNow, land, pages } from "./state.js";
import { fmtTok } from "./tokens.js";
import { checkSoon } from "./check.js";

// ORDER is land's steps in the order a run takes them; main is drawn only
// when the run waited there, never ahead.
const ORDER = ["checks", "kinds", "jobs", "lease", "rebase", "results", "push"];

// landing reads every landing running or quiet, for the overview's rows.
export const landing = loader(() => fetchJSON("/api/landing"), {
  show: (r) => {
    if (!r.body) return; // a failed re-read keeps the rows
    land.list = Array.isArray(r.body) ? r.body : [];
    renderLandings();
  },
});

const at = (s) => Date.parse(s || "");

// clockDur is a ticking time: "8s", "3m 29s", "16m 00s", "1h 05m".
function clockDur(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), r = s % 60;
  const p = (n) => String(n).padStart(2, "0");
  if (h) return h + "h " + p(m) + "m";
  return m ? m + "m " + p(r) + "s" : r + "s";
}

// usualDur is a usual time in seconds: "29s", "11m", "6m 30s".
function usualDur(secs) {
  const m = Math.floor(secs / 60), r = secs % 60;
  return !m ? r + "s" : r ? m + "m " + r + "s" : m + "m";
}

function agoText(ms) {
  return ms < 60000 ? Math.max(0, Math.floor(ms / 1000)) + "s ago" : dur(ms) + " ago";
}

// FMT are the ticking texts: each gets the ms since the element's start.
const FMT = {
  run: clockDur,
  min: (ms) => dur(ms),
  ago: agoText,
  quiet: (ms) => "no word from land for " + dur(ms),
  over: (ms, e) => {
    const x = ms / 1000 / +e.dataset.usual;
    return x > 2 ? "▲ " + x.toFixed(1) + "× usual" : "";
  },
};

// ticker is a span whose text counts up from from (an RFC 3339 time) on the
// hub's clock in format fmt; landTick rewrites it every second.
function ticker(cls, from, fmt, usual) {
  const e = el("span", cls);
  const t = at(from);
  if (isNaN(t)) return e;
  e.dataset.lt = String(t);
  e.dataset.lfmt = fmt;
  if (usual) e.dataset.usual = String(usual);
  tickOne(e);
  return e;
}

function tickOne(e) {
  e.textContent = FMT[e.dataset.lfmt](hubNow() * 1000 - +e.dataset.lt, e);
  e.hidden = e.textContent === "";
}

export function landTick() {
  for (const e of document.querySelectorAll("[data-lt]")) tickOne(e);
}

// fixed is a span of the time between two RFC 3339 times.
function fixed(cls, from, to, fmt) {
  const ms = at(to) - at(from);
  return el("span", cls, isNaN(ms) ? "–" : (fmt || clockDur)(ms));
}

const newest = (v) => (v.runs || [])[(v.runs || []).length - 1];
const begun = (v) => v.ready_at || ((v.runs || [])[0] || {}).run || v.at;
// current is the newest run's row the land is in: its last, while it has no to.
function current(v) {
  const r = newest(v), s = r && r.steps[r.steps.length - 1];
  return r && !r.end && s && !s.to ? s : null;
}
// ended reports a landing whose newest run is neither running nor quiet.
export function landEnded(v) {
  const r = newest(v);
  return !v.running && !(r && r.quiet);
}

function usualOf(v, step, label) {
  return (v.usual || []).find((u) => u.step === step && u.label === label);
}

const usualText = (u) => "usually " + usualDur(u.secs) + " (" + u.n + ")";

// nowJobs are the jobs the current step waits on, with the endpoint's
// details where it has them.
function nowJobs(v) {
  const byId = new Map((v.jobs || []).map((j) => [j.id, j]));
  return ((v.now && v.now.jobs) || []).map((j) => Object.assign({}, j, byId.get(j.id) || {}));
}

function leadLink(path, cls) {
  const a = el("a", cls, path);
  a.setAttribute("href", "#/lead/" + encodeURIComponent(path));
  return a;
}

// --- The overview's rows -------------------------------------------------

// renderLandings draws one row per landing under the band; hidden with none.
function renderLandings() {
  const box = $("landing");
  box.replaceChildren(...(land.list || []).map(landRow));
  box.hidden = box.childElementCount === 0;
  if (check) checkSoon();
}

// landRow is a landing's row: its badge and path linking its lead page,
// then its time and its current step on one line.
function landRow(v) {
  const who = el("a", "who tap", null, el("span", "badge st-landing", "▲ landing"), el("span", "p", v.path));
  who.setAttribute("href", "#/lead/" + encodeURIComponent(v.path));
  const say = el("div", "say", null, ticker("", begun(v), "min"), " · ");
  const r = newest(v), cur = current(v);
  if (r && r.quiet) {
    say.append(ticker("warn", v.at, "quiet"));
  } else if (!v.now || !cur) {
    say.append(v.text || "");
  } else {
    say.append(...stepWords(v, cur));
  }
  return el("div", "need st-landing", null, who, say);
}

// stepWords is the overview's words for the current step: a job's instance
// time, host, calls and last tool use; else the step's time against its
// usual and the command's newest line or the step's text.
function stepWords(v, cur) {
  const job = cur.step === "jobs" ? nowJobs(v)[0] : null;
  if (job) {
    if (!job.started) return [job.kind + " ", ticker("", cur.from, "run"), " · waiting: no instance has taken it"];
    const out = [job.kind + " ", ticker("", job.started, "run"), job.host ? " on " + job.host : ""];
    if (v.review_usual && job.kind === "review") out.push(", usually " + usualDur(v.review_usual.secs));
    out.push(" · " + job.calls + " calls");
    if (job.cause) out.push(" · " + job.cause);
    return out;
  }
  const u = usualOf(v, cur.step, cur.label);
  const out = [cur.label + " ", ticker("", cur.from, "run")];
  if (u) out.push(", usually " + usualDur(u.secs));
  const cmd = v.now.cmd;
  const said = cmd && cmd.wait ? "waiting for " + cmd.wait.slot : cmd && cmd.last ? cmd.last : v.text;
  if (said) out.push(" · " + said);
  return out;
}

// --- The lead page's card ------------------------------------------------

// landCard is the lead page's landing card for the view v of
// /api/landing?lead=.
export function landCard(v) {
  const ended = landEnded(v);
  const head = el("h3", "lhead", null, el("span", "k", "landing"), " ",
    ended ? fixed("big", begun(v), v.at) : ticker("big", begun(v), "run"),
    el("span", "muted", v.ready_at ? " since ready at " + v.ready_at.slice(11, 19) + "Z" : ""));
  const c = el("section", "card landing", null, head);
  c.dataset.card = "landing";
  if (!v.now && !(v.runs || []).length) {
    c.append(el("p", "lsay", v.text || ""), el("p", "muted", "this bmesh publishes no steps"));
    return c;
  }
  const runs = v.runs || [];
  const earlier = runs.slice(0, -1).map((r, i) => runLine(r, i, runs));
  if (earlier.length) c.append(el("div", "lruns", null, ...earlier));
  c.append(stepRows(v, ended));
  return c;
}

// runLine is an earlier run on one line: its number, time, steps and how
// it ended.
function runLine(r, i, runs) {
  const last = r.steps[r.steps.length - 1];
  const steps = r.steps.map((s) => {
    const ms = at(s.to) - at(s.from);
    return ms >= 60000 ? s.label + " " + clockDur(ms) : s.label;
  }).join(", ");
  let end = r.end ? "ended: " + r.end : r.quiet ? "went quiet" : "";
  if (r.end && /^timed out/.test(r.end) && i < runs.length - 1) end += "; the daemon ran land again";
  return el("p", "lrun", ["run " + (i + 1), last && last.to ? clockDur(at(last.to) - at(r.run)) : "", steps, end].filter(Boolean).join(" · "));
}

// stepRows is a row per step of the newest run, then the steps still ahead.
function stepRows(v, ended) {
  const r = newest(v) || { steps: [] }, cur = current(v);
  const rows = r.steps.map((s) => s === cur ? curRow(v, s) : doneRow(v, s));
  if (!ended && cur) {
    const lastKnown = [...r.steps].reverse().find((s) => ORDER.includes(s.step));
    const i = lastKnown ? ORDER.indexOf(lastKnown.step) : -1;
    for (const step of ORDER.slice(i + 1)) rows.push(stepRow("○", "ahead", step, el("span", "lt", "–"), []));
  }
  return el("ol", "lsteps", null, ...rows);
}

function stepRow(glyph, cls, label, time, notes, ...under) {
  return el("li", "lstep " + cls, null, el("span", "lg", glyph), el("span", "ll", label), time,
    el("span", "ln", null, ...notes), ...under);
}

function doneRow(v, s) {
  const u = usualOf(v, s.step, s.label);
  const notes = [];
  if (s.text && s.text !== s.label && s.text !== "checking") notes.push(el("span", "muted", s.text));
  if (u) notes.push(el("span", "muted", usualText(u)));
  return stepRow("✓", "done", s.label, fixed("lt", s.from, s.to), notes);
}

// curRow is the step the land is in: its ticking time, against its usual,
// or how long land has said nothing; under it the command, its jobs or the
// lease holder.
function curRow(v, s) {
  const r = newest(v), u = usualOf(v, s.step, s.label), notes = [];
  if (r.quiet) {
    notes.push(ticker("warn", v.at, "quiet"));
  } else if (u) {
    notes.push(el("span", "muted", usualText(u)), ticker("warn", s.from, "over", u.secs));
  }
  return stepRow("▸", "now", s.label, ticker("lt", s.from, "run"), notes, ...detail(v, s));
}

function line(...parts) {
  return el("div", "dl", null, ...parts);
}

// detail is what the current step is doing, whichever applies.
function detail(v, s) {
  const out = [], n = v.now || {};
  if (n.cmd) {
    const c = n.cmd, box = el("div", "ldetail");
    if (c.command) {
      const l = line(el("span", "muted", c.kind + " ·"), el("code", "lcmd", c.command));
      l.classList.add("cmdl");
      box.append(l);
    }
    else box.append(line(el("span", "muted", c.kind)));
    if (c.last || c.lines) {
      const l = line(el("span", "muted", "last: "), c.last || "–");
      if (c.last_at) l.append(el("span", "muted", " · "), ticker("muted", c.last_at, "ago"));
      l.append(el("span", "muted", " · " + (c.lines || 0) + " lines"));
      box.append(l);
    }
    if (c.wait) box.append(line(el("span", "muted", "waiting for "), c.wait.slot,
      el("span", "muted", " · held by "), c.wait.holder || "–", el("span", "muted", " · place " + c.wait.place)));
    out.push(box);
  }
  if (s.step === "jobs") for (const j of nowJobs(v)) out.push(jobBox(v, j));
  if (v.lease) {
    const l = v.lease;
    out.push(el("div", "ldetail", null, line(el("span", "muted", "lease held by "), leadLink(l.holder),
      l.text ? el("span", "muted", " · " + l.text) : "")));
  }
  return out;
}

// jobBox is a job the step waits on: which instance runs it where, what its
// session has done so far, and how long the repo's reviews usually take.
function jobBox(v, j) {
  const head = line("job " + String(j.id).slice(0, 8), el("span", "muted", " · "), [j.model, j.effort].filter(Boolean).join(" ") || "–");
  if (j.instance) head.append(el("span", "muted", " · "), leadLink(j.instance), j.host ? " on " + j.host : "");
  else if (j.host) head.append(" on " + j.host);
  const box = el("div", "ldetail", null, head);
  if (!j.started) {
    box.append(line(el("span", "muted", "waiting: no instance has taken it")));
  } else {
    const did = line();
    if (j.state === "ok" || j.state === "fail") did.append(el("span", j.state === "fail" ? "warn" : "", j.state), " in " + clockDur(j.ms || 0));
    else did.append(el("span", "muted", "running "), ticker("", j.started, "run"));
    did.append(" · " + j.calls + " calls · read " + fmtTok(j.read) + " · context " + fmtTok(j.ctx));
    box.append(did);
    if (j.cause) {
      const l = line(el("span", "muted", "last: "), j.cause);
      if (j.cause_at) l.append(el("span", "muted", " · "), ticker("muted", j.cause_at, "ago"));
      box.append(l);
    }
  }
  if (v.review_usual && j.kind === "review") box.append(line(el("span", "muted", usualText(v.review_usual))));
  return box;
}

// landPoll re-reads the overview's landings while a row shows; the lead
// page's card re-reads through lead.js.
export function landPoll() {
  if (pages.view === "map" && !$("landing").hidden) landing.run();
}
