// routines: the routines panel.
import { $, badge, card, check, clock, el, fetchJSON, keepOpen, loader, readState } from "./core.js";

import { checkSoon } from "./check.js";

// instanceSub is an instance's uses and context, then, while a job runs,
// its words: the newest checkup ("job <id> running 6m: ...; quiet 4m").
function instanceSub(i) {
  const extra = [i.uses === 1 ? "1 use" : (i.uses || 0) + " uses"];
  if (i.context > 0) extra.push(Math.round(i.context / 1000) + "k ctx");
  const st = i.state || {};
  if (st.state === "working" && st.text) extra.push(st.text);
  return extra;
}

function leaseRow(l) {
  return el("li", null, null, el("span", "p", l.key), el("span", "muted", l.holder), clock("lease", l.expires_at, true));
}

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
      const det = el("details", "fold-open spent", null, el("summary", "tap", "spent (" + spent.length + ")"),
        el("ol", "rows", null, ...spent.map((i) => instanceRow(k, i))));
      rows.push(el("li", "fold", null, keepOpen(det, k.kind + " " + repo)));
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

// routines reads the routines panel; a failed re-read keeps what it shows.
export const routines = loader(() => fetchJSON("/api/routines"), {
  show: (r) => {
    if (r.body) return renderRoutines(r.body);
    const msg = readState(routines, "routines");
    if (msg) $("routines-panel").replaceChildren(el("p", "empty", msg));
    if (check) checkSoon();
  },
});
