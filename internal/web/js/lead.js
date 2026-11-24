// lead: the lead page.
import { $, badge, bare, card, check, dur, el, fetchJSON, field, hm, keepOpen, lastToken, loader, md, readState, stamp } from "./core.js";
import { hubNow, land, pages, tl, tok } from "./state.js";
import { clampRow, describe, frameKey, landFilter, landOf } from "./drawer.js";
import { drawLanes, replayLive } from "./timeline.js";
import { ctxChart, fmtTok, kindsRead } from "./tokens.js";
import { checkSoon } from "./check.js";
import { drawAll } from "./main.js";
import { landCard, landEnded } from "./landing.js";

const TIMELINE_MAX = 200;

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

// shownLead is the lead view drawn last, so a landing re-read redraws it.
let shownLead = null;

// landingOf is the lead page's landing view for path, null with none (a 404).
function landingOf(path) {
  const l = land.lead;
  return l && l.path === path && l.status === 200 ? l.body : null;
}

function renderLead(path, v) {
  shownLead = { path, v };
  const head = $("lead-head"), panel = $("lead-panel");
  const n = v.node || {}, st = v.state || {}, sp = v.spec;
  const parts = [badge(st.state)];
  if (n.spec || sp) parts.push(el("span", "hd-spec", "spec " + (n.spec || sp.id) + (sp && sp.title ? " · " + sp.title : "")));
  for (const f of [n.host, n.model]) if (f) parts.push(el("span", "mono muted", f));
  const began = Date.parse(n.started_at || "");
  if (!isNaN(began)) {
    const ended = st.state === "done" ? Date.parse(st.at || "") : NaN;
    parts.push(el("span", "mono muted", "started " + md(began / 1000) + " " + hm(began / 1000) + "Z"),
      el("span", "mono muted", dur((isNaN(ended) ? hubNow() * 1000 : ended) - began)));
  }
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

  const main = [];
  // The landing card comes first while its newest run is running or quiet,
  // after the spec card once the landing has ended.
  const lv = landingOf(path), lc = lv && landCard(lv), lateLand = lv && landEnded(lv);
  if (lc && !lateLand) main.push(lc);
  const kids = v.children || [];
  if (kids.length) main.push(card("tree", el("ol", "rows", null, ...kids.map(childRow))));
  if (n.role === "round") {
    const specs = v.specs || [];
    main.push(specs.length ? card("specs", el("ol", "rows", null, ...specs.map((s) => roundSpec(path, s))))
      : bare("specs", "no specs landed yet"));
  } else if (sp) {
    const merged = sp.merged ? String(sp.merged).slice(0, 8) : "";
    main.push(card("spec " + (sp.id || n.spec || ""), el("p", "spec-title", sp.title || "(untitled)"),
      el("div", "fields", null, field("status", sp.status), field("merged", merged),
        field("verified", sp.verified || "not verified")),
      specBody(sp.text)));
  } else {
    main.push(bare("spec", n.spec ? "spec " + n.spec + " is not on main of a repo configured here" : "no spec"));
  }
  if (lc && lateLand) main.push(lc);

  const inbox = (v.inbox || []).map((m) => {
    const li = el("li", null, null,
      el("span", "t", stamp(m.at)),
      el("span", "p", (m.from || "?") + " → " + (m.kind || "message") + ":"),
      el("span", "x", msgText(m)));
    clampRow(li, "inbox " + (m.id || (m.from || "") + "@" + (m.at || "")));
    return li;
  });
  const show = landFilter();
  const tl = (v.timeline || []).filter(show).slice(-TIMELINE_MAX).reverse().map((f) => {
    const d = describe(f);
    const who = el("span", "p", shortPath(d.path, path));
    who.title = d.path;
    const li = el("li", "ev-" + d.event, null, el("span", "t", stamp((f.payload && f.payload.at) || f.at)),
      who, el("span", "e", d.event), el("span", "x", d.text));
    const l = landOf(f);
    if (l) {
      li.dataset.land = l.node;
      li.dataset.landText = l.text;
    }
    clampRow(li, frameKey(f));
    return li;
  });
  const used = tok.lead && tok.lead.path === path ? tok.lead.data : null;
  panel.replaceChildren(el("div", "stack", null, ...main), el("div", "stack", null, tokensCard(used),
    inbox.length ? card("inbox", el("ol", "rows", null, ...inbox)) : bare("inbox", "no messages"),
    tl.length ? card("timeline", el("ol", "rows", null, ...tl)) : bare("timeline", "no events in the last day")));
}

// childRow is one of the node's children in the tree card: a link to its
// own page.
function childRow(c) {
  const n = c.node || {}, path = String(n.path || "?");
  const a = el("a", "child tap", null, badge(c.state && c.state.state), el("span", "p", lastToken(path)));
  if (n.branch) a.append(el("code", "branch", n.branch));
  a.append(el("span", c.unread > 0 ? "unread" : "muted", (c.unread || 0) + " unread"));
  a.setAttribute("href", "#/lead/" + encodeURIComponent(path));
  a.title = path;
  return el("li", null, null, a);
}

// roundSpec is one of a round's specs, "NNN title · status", opening its
// body in place.
function roundSpec(round, s) {
  const det = el("details", "fold-open", null,
    el("summary", "tap", (s.id || "?") + " " + (s.title || "(untitled)") + " · " + (s.status || "?")), specBody(s.text));
  return el("li", "fold", null, keepOpen(det, round + " " + s.id));
}

// lead reads the lead page's node, its lanes and its token use (compared
// with tok.lead.vs when set).
export const lead = loader(async (path) => {
  if (!path) return { status: 404, t: {}, u: {} };
  const get = (url) => fetchJSON(url).catch(() => ({ status: 0, body: null }));
  const vs = tok.lead && tok.lead.path === path ? tok.lead.vs : "";
  const [r, t, u, l] = await Promise.all([get("/api/lead/" + encodeURIComponent(path)),
    get("/api/timeline?lead=" + encodeURIComponent(path)),
    get("/api/usage/agent?agent=" + encodeURIComponent(path) + (vs ? "&vs=" + encodeURIComponent(vs) : "")),
    get("/api/landing?lead=" + encodeURIComponent(path))]);
  return { status: r.status, body: r.body, t, u, l, vs };
}, {
  key: () => pages.lead,
  show: (r, path) => {
    if (!path) return; // a re-read queued as the route left the lead page
    keepLanding(path, r.l);
    if (r.u.body || r.u.status === 404) tok.lead = { path, vs: r.vs, data: r.u.body };
    if (r.t.body) {
      r.t.body.lanes = r.t.body.lanes || [];
      replayLive(r.t.body);
      tl.lead = { path, data: r.t.body };
    }
    if (r.body) {
      renderLead(path, r.body);
    } else if (lead.state.phase === "shown") {
      return; // a failed re-read keeps what is shown
    } else {
      $("lead-panel").replaceChildren(el("p", "empty", readState(lead, path, "no such node: " + path)));
      if (lead.state.phase !== "missing") return;
      $("lead-head").replaceChildren();
    }
    pages.leadShown = path;
    drawAll();
    peers.run();
    if (check) checkSoon();
  },
});

// keepLanding stores a landing read for path: a 404 drops the card, a
// failed read keeps the one shown.
function keepLanding(path, l) {
  if (l.status === 200 || l.status === 404) land.lead = { path, status: l.status, body: l.body };
}

// leadLanding re-reads the lead page's landing alone and redraws the page
// with it (the 5 s poll while its card runs).
const leadLanding = loader((path) => path ? fetchJSON("/api/landing?lead=" + encodeURIComponent(path)) : Promise.resolve({ status: 404 }), {
  key: () => pages.lead,
  show: (r, path) => {
    if (!path) return;
    keepLanding(path, r);
    if (shownLead && shownLead.path === path) renderLead(path, shownLead.v);
    if (check) checkSoon();
  },
});

// leadLandPoll re-reads the lead page's landing while its card shows a run
// not yet ended.
export function leadLandPoll() {
  const v = pages.view === "lead" && landingOf(pages.lead);
  if (v && !landEnded(v)) leadLanding.run();
}

// drawLead draws the lead page's lanes and context chart once they are the
// lead's shown.
export function drawLead() {
  if (!tl.lead || tl.lead.path !== pages.lead) return;
  drawLanes($("lead-lanes"), tl.lead.data, { fold: false, live: true, life: true });
  if (tok.lead && tok.lead.path === pages.lead) ctxChart($("lead-ctx"), tok.lead.data, { empty: "No model calls of this node on the mesh." });
}

// touchesLead reports whether a live frame is about the lead shown: its
// tree's events, its inbox, or a daemon event naming it.
export function touchesLead(rest, p) {
  const l = pages.lead;
  if (!l) return false;
  if (rest === "inbox." + l) return true;
  if (rest.startsWith("daemon.")) return typeof p.to === "string" && (p.to === l || p.to.startsWith(l + "."));
  return rest.startsWith(l + ".");
}

const PEERS_MS = 5 * 60000; // the compare list's read of the week's agents

// tokensCard is the lead page's tokens card: the four kinds, main and
// sub, and each subagent with its description.
function tokensCard(d) {
  if (!d || !d.agent) return bare("tokens", "no model calls on the mesh");
  const a = d.agent;
  const head = el("tr", null, null, ...["", "input", "cache read", "cache write", "out", "read"].map((h, i) => el("th", i ? "r" : "", h)));
  const row = (name, k) => el("tr", null, null, el("td", null, name), el("td", "r", fmtTok(k.in)), el("td", "r", fmtTok(k.cr)),
    el("td", "r", fmtTok(k.cw)), el("td", "r", fmtTok(k.out)), el("td", "r", fmtTok(kindsRead(k))));
  const kinds = el("div", "tw", null, el("table", "ag", null, el("thead", null, null, head),
    el("tbody", null, null, row("main", a.main || {}), row("sub", a.sub || {}))));
  const subs = (d.subs || []).map((s) => el("li", null, null, el("span", "x", s.desc || s.id),
    el("span", "t", fmtTok(s.read) + " read · " + fmtTok(s.out) + " out · " + s.calls + " calls")));
  return card("tokens", el("p", "muted", fmtTok(a.read) + " read · peak " + fmtTok(a.peak) + " · " + a.calls + " calls" +
    (a.compactions ? " · " + a.compactions + " compacted" : "")), kinds,
  ...(subs.length ? [el("h4", null, "subagents"), el("ol", "rows", null, ...subs)] : []));
}

// peers reads the week's agents (again once PEERS_MS old) for the lead
// page's compare select, then fills it.
const peers = loader(async () => {
  if (tok.peers && Date.now() - tok.peers.at <= PEERS_MS) return { status: 200 };
  const r = await fetchJSON("/api/usage?since=7d");
  if (r.body) tok.peers = { at: Date.now(), agents: r.body.agents || [] };
  return r;
}, { key: () => pages.lead, show: (r, path) => comparePeers(path) });

// comparePeers fills the lead page's compare select with the done leads of
// its spec size that read in the last week.
function comparePeers(path) {
  const sel = $("lead-vs");
  const me = tok.lead && tok.lead.data && tok.lead.data.agent;
  const size = me ? me.size : "";
  const peers = (tok.peers ? tok.peers.agents : []).filter((a) => size && a.size === size && a.role === "lead" && a.state === "done" && a.agent !== path);
  const opts = [el("option", null, peers.length ? "compare with a done " + size + " lead…" : "no done " + (size || "sized") + " lead to compare")];
  opts[0].value = "";
  const vs = tok.lead && tok.lead.vs || "";
  // A vs from the hash may be older than the week the list reads.
  const shown = vs && !peers.some((a) => a.agent === vs) ? [{ agent: vs, read: tok.lead.data && tok.lead.data.vs ? tok.lead.data.vs.agent.read : 0 }] : [];
  for (const a of peers.concat(shown)) {
    const o = el("option", null, a.agent + " · " + fmtTok(a.read));
    o.value = a.agent;
    opts.push(o);
  }
  sel.replaceChildren(...opts);
  sel.value = vs;
  sel.disabled = opts.length < 2;
}

// enterLead opens the lead page on path (rest of the hash), with ?vs=<path>
// compared (the outliers' click); a lead not the one shown starts blank.
export function enterLead(rest, q) {
  let path = rest;
  try { path = decodeURIComponent(path); } catch (e) { /* shown as written */ }
  const vs = q.get("vs");
  if (vs !== null && !(tok.lead && tok.lead.path === path && tok.lead.vs === vs)) {
    tok.lead = { path, vs, data: tok.lead && tok.lead.path === path ? tok.lead.data : null };
  }
  if (path !== pages.leadShown) {
    $("lead-title").textContent = path;
    $("lead-head").replaceChildren();
    $("lead-lanes").replaceChildren();
    $("lead-panel").replaceChildren();
    pages.leadShown = "";
  }
  pages.lead = path;
}
