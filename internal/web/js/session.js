// session: a coordinator session's page.
import { $, chartRoot, el, fetchJSON, hm, legendOf, loader, md, readState, spanText, svg, timeAxis } from "./core.js";
import { pages, tok } from "./state.js";
import { hover } from "./drawer.js";
import { LANE } from "./timeline.js";
import { agentName, ctxChart, fmtTok, phaseBar, share } from "./tokens.js";

// sessionPage reads the session page's agent; a failed re-read keeps what
// it shows.
export const sessionPage = loader((agent) => agent ? fetchJSON("/api/usage/agent?agent=" + encodeURIComponent(agent)) : { status: 404 }, {
  key: () => pages.session,
  show: (r, agent) => {
    if (r.body) tok.session = { agent, data: r.body };
    drawSession();
  },
});

// drawSession draws a coordinator session's page: its head, busy lane,
// context chart and subagents.
export function drawSession() {
  const agent = pages.session, missing = "no model calls of " + agent + " in the last 30 days";
  const s = tok.session && tok.session.agent === agent ? tok.session : null;
  const d = s && sessionPage.state.phase !== "missing" ? s.data : null, head = $("session-head");
  if (!d || !d.agent) {
    head.replaceChildren(el("p", "empty", readState(sessionPage, agent, missing) || missing));
    for (const id of ["session-lane", "session-ctx", "session-subs"]) $(id).replaceChildren();
    return;
  }
  const a = d.agent, design = +a.design || 0, exec = +a.exec || 0;
  head.replaceChildren(
    el("span", "mono", a.repo || "no repo"), el("span", "mono muted", a.host || ""),
    el("span", "mono muted", md(a.first) + " " + hm(a.first) + "Z – " + md(a.last) + " " + hm(a.last) + "Z"),
    el("span", "mono", fmtTok(a.read) + " read · " + fmtTok(a.out) + " out"),
    phaseBar(design, exec, Math.max(1, design + exec)),
    el("span", "mono", "design " + fmtTok(design) + " · exec " + fmtTok(exec) + " · " + share(design, exec) + " design"));
  drawSpans($("session-lane"), d.spans || [], a.first, Math.max(a.last, a.first + 600), a.agent);
  ctxChart($("session-ctx"), d, {});
  const subs = d.subs || [];
  const rows = subs.map((x) => el("tr", null, null, el("td", "desc", x.desc || x.id),
    el("td", null, null, el("i", "sw r-" + (x.phase === "exec" ? "exec" : "design")), x.phase || "design"),
    el("td", "r", fmtTok(x.read)), el("td", "r", fmtTok(x.out)), el("td", "r hide-n", String(x.calls))));
  $("session-subs").replaceChildren(subs.length ? el("div", "tw", null, el("table", "ag", null,
    el("thead", null, null, el("tr", null, null, el("th", null, "description"), el("th", null, "phase"), el("th", "r", "read"),
      el("th", "r", "out"), el("th", "r hide-n", "calls"))), el("tbody", null, null, ...rows))) : el("p", "empty", "No subagents."));
}

// drawSpans draws one session's busy spans over [from, to].
function drawSpans(box, spans, from, to, agent) {
  const W = Math.max(box.clientWidth, 240), H = 44, P = { l: 8, r: 8 };
  const x = (t) => P.l + (Math.max(from, Math.min(to, t)) - from) / Math.max(1, to - from) * (W - P.l - P.r);
  const root = chartRoot(W, H, "busy spans");
  timeAxis(root, x, from, to, 4, 24, H);
  for (const [a, b, ph] of spans) {
    const r = svg("rect", { class: "seg ph-" + (ph === "exec" ? "exec" : "design"), x: x(a), y: 6, width: Math.max(2, x(b) - x(a)), height: LANE.bar, rx: 3 }, root);
    hover(r, () => agentName(agent) + "\n" + (ph || "design") + "  " + spanText(a, b));
  }
  box.replaceChildren(legendOf([["r-design", "design"], ["r-exec", "exec"]]), root);
}

// enterSession opens the session page on <host>/<id> (rest of the hash); a
// session not the one loaded starts blank.
export function enterSession(rest) {
  const [host, id] = rest.split("/").map((x) => { try { return decodeURIComponent(x || ""); } catch (e) { return x || ""; } });
  pages.session = "session." + host + "." + id;
  $("session-title").textContent = "session " + id.slice(0, 8) + " on " + host;
  if (!tok.session || tok.session.agent !== pages.session) {
    for (const b of ["session-head", "session-lane", "session-ctx", "session-subs"]) $(b).replaceChildren();
  }
}
