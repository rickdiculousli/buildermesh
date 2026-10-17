// gen-brand.mjs <dir>: writes logo.svg, banner-dark.svg, banner-light.svg.
import { writeFileSync } from "node:fs";
const out = process.argv[2];
const f = (n) => +n.toFixed(1);
// The mark: an isometric box drawn as a mesh. Seven nodes: the six corners
// of the hexagon and the centre (the hub); three faces. One rim node is
// green: a change landed.
function mark(cx, cy, R) {
  const p = {}, s = Math.sqrt(3) / 2;
  p.t = [cx, cy - R]; p.ur = [cx + s * R, cy - R / 2]; p.lr = [cx + s * R, cy + R / 2];
  p.b = [cx, cy + R]; p.ll = [cx - s * R, cy + R / 2]; p.ul = [cx - s * R, cy - R / 2]; p.c = [cx, cy];
  const P = (k) => `${f(p[k][0])} ${f(p[k][1])}`;
  return { p, P };
}
const RIM = ["t", "ur", "lr", "b", "ll", "ul"];
const EDGES = [["t", "ur"], ["ur", "lr"], ["lr", "b"], ["b", "ll"], ["ll", "ul"], ["ul", "t"], ["c", "ul"], ["c", "ur"], ["c", "b"]];
// Pulses: each ends at a rim node, which flashes as it arrives.
const ROUTE = [["c", "ul", "j"], ["c", "b", "r"], ["ul", "t", "j"], ["b", "ll", "r"], ["c", "ur", "j"], ["ur", "lr", "ok"]];
function markSVG({ p, P }, { nr, hr, pulses = false }) {
  const face = (ks) => "M" + ks.map(P).join(" L") + " Z";
  let s = `<path class="ft" d="${face(["t", "ur", "c", "ul"])}"/><path class="fl" d="${face(["ul", "c", "b", "ll"])}"/><path class="fr" d="${face(["c", "ur", "lr", "b"])}"/>`;
  s += `<g class="e">` + EDGES.map(([a, b]) => `<path d="M${P(a)} L${P(b)}"/>`).join("") + `</g>`;
  const dots = pulses ? ROUTE.map(([a, b, k], i) => `<g class="mv ${k} q${i}" style="offset-path:path('M${P(a)} L${P(b)}')"><circle class="gl" r="9"/><circle r="5"/></g>`).join("") : "";
  const flash = (k) => { const i = ROUTE.findIndex((r) => r[1] === k); return pulses && i >= 0 ? ` k${i}` : ""; };
  s += RIM.map((k) => `<circle class="nd${k === "lr" ? " ok" : ""}${flash(k)}" cx="${f(p[k][0])}" cy="${f(p[k][1])}" r="${f(nr)}"/>`).join("");
  if (pulses) s += `<circle class="hg" cx="${f(p.c[0])}" cy="${f(p.c[1])}" r="${f(hr * 2.2)}"/>`;
  s += `<circle class="hub" cx="${f(p.c[0])}" cy="${f(p.c[1])}" r="${f(hr)}"/>`;
  return s + dots;
}
const pal = {
  dark: { fg: "#e6edf3", mu: "#8b949e", g1: "#39c5bb", g2: "#58a6ff", ok: "#3fb950", node: "#0d1117", card1: "#0d1117", card2: "#131a24", stroke: "#30363d", grid: "#222a35", j: "#d29922", r: "#a371f7", k: "#e6edf3", chip: "#161b22" },
  light: { fg: "#1f2328", mu: "#59636e", g1: "#1a7f72", g2: "#0969da", ok: "#1a7f37", node: "#ffffff", card1: "#ffffff", card2: "#f3f6f9", stroke: "#d0d7de", grid: "#dfe5eb", j: "#9a6700", r: "#8250df", k: "#1f2328", chip: "#f6f8fa" },
};
const sans = `-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Noto Sans', Helvetica, Arial, sans-serif`;
const mono = `ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace`;
const grad = (id, g1, g2, x1, y1, x2, y2) => `<linearGradient id="${id}" gradientUnits="userSpaceOnUse" x1="${x1}" y1="${y1}" x2="${x2}" y2="${y2}"><stop offset="0" stop-color="${g1}"/><stop offset="1" stop-color="${g2}"/></linearGradient>`;

// logo.svg: the mark alone, square, for both a white and a #0d1117 page.
{
  const m = mark(64, 64, 50);
  writeFileSync(`${out}/logo.svg`, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128" width="128" height="128" role="img" aria-label="buildermesh">
<title>buildermesh</title>
<defs>${grad("g", "#1f9e94", "#3b8ff0", 14, 14, 114, 114)}</defs>
<style>.ft{fill:url(#g);opacity:.34}.fl{fill:url(#g);opacity:.18}.fr{fill:url(#g);opacity:.26}.e path{stroke:url(#g);stroke-width:5;fill:none;stroke-linecap:round}.nd{fill:#fff;stroke:url(#g);stroke-width:4}.nd.ok{fill:#3fb950;stroke:#2a9a3e}.hub{fill:url(#g)}</style>
${markSVG(m, { nr: 7, hr: 10 })}
</svg>\n`);
}

// banner-<theme>.svg
for (const theme of ["dark", "light"]) {
  const c = pal[theme];
  const W = 960, H = 280, R = 82, cx = 196, cy = 140, P = 11;
  const m = mark(cx, cy, R);
  const tx = 330;
  const tag = "A mesh for coding agents that propose, build, verify and land changes on main, across every box you own.";
  writeFileSync(`${out}/banner-${theme}.svg`, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" role="img" aria-label="buildermesh (bmesh): ${tag}">
<title>buildermesh (bmesh)</title>
<defs>
${grad("g", c.g1, c.g2, cx - R, cy - R, cx + R, cy + R)}
${grad("w", c.g1, c.g2, tx + 194, 0, tx + 354, 0)}
<linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${c.card1}"/><stop offset="1" stop-color="${c.card2}"/></linearGradient>
<pattern id="lat" width="40" height="69.28" patternUnits="userSpaceOnUse"><g fill="${c.grid}"><circle cx="0" cy="0" r="1.6"/><circle cx="40" cy="0" r="1.6"/><circle cx="20" cy="34.64" r="1.6"/><circle cx="0" cy="69.28" r="1.6"/><circle cx="40" cy="69.28" r="1.6"/></g></pattern>
<radialGradient id="fade" cx="${f(cx / W)}" cy=".5" r=".8"><stop offset="0" stop-color="#fff" stop-opacity="1"/><stop offset=".5" stop-color="#fff" stop-opacity=".35"/><stop offset="1" stop-color="#fff" stop-opacity="0"/></radialGradient>
<mask id="lm"><rect width="${W}" height="${H}" fill="url(#fade)"/></mask>
</defs>
<style>
.ft{fill:url(#g);opacity:.30}.fl{fill:url(#g);opacity:.16}.fr{fill:url(#g);opacity:.22}
.e path{stroke:url(#g);stroke-width:2.4;fill:none;stroke-linecap:round}
.nd{fill:${c.node};stroke:url(#g);stroke-width:2.6}.nd.ok{stroke:${c.ok};fill:${c.ok};fill-opacity:.25}
.hub{fill:url(#g)}.hg{fill:url(#g);opacity:.18}
.mv{opacity:0;offset-rotate:0deg}.mv .gl{opacity:.3}
.mv.j{fill:${c.j}}.mv.r{fill:${c.r}}.mv.ok{fill:${c.ok}}
${ROUTE.map((_, i) => `.q${i}{animation:mv ${P}s ${f(i * 1.25)}s ease-in-out infinite}.k${i}{animation:nd ${P}s ${f(i * 1.25 + 1.0)}s ease-out infinite}`).join("\n")}
@keyframes mv{0%{offset-distance:0%;opacity:0}1.5%{opacity:1}8%{offset-distance:100%;opacity:1}10%,100%{offset-distance:100%;opacity:0}}
@keyframes nd{0%{stroke-width:2.6}2.5%{stroke-width:5.5}9%,100%{stroke-width:2.6}}
@supports not (offset-path:path('M0 0')){.mv{display:none}}
.hg{animation:hg 3s ease-in-out infinite;transform-origin:center;transform-box:fill-box}
@keyframes hg{0%,100%{opacity:.10;transform:scale(.85)}50%{opacity:.26;transform:scale(1.08)}}
.wm{font:700 64px ${sans};letter-spacing:-1.5px}.wb{fill:${c.fg}}.wmsh{fill:url(#w)}
.tag{font:400 22px ${sans};fill:${c.fg}}
.st{font:500 18px ${sans};fill:${c.mu}}.ar{fill:${c.g1}}
.chip{fill:${c.chip};stroke:${c.stroke}}.chipt{font:600 15px ${mono};fill:${c.mu}}
@media (prefers-reduced-motion: reduce){*{animation:none!important}}
</style>
<rect x="1" y="1" width="${W - 2}" height="${H - 2}" rx="12" fill="url(#bg)"/>
<rect x="1" y="1" width="${W - 2}" height="${H - 2}" rx="12" fill="url(#lat)" mask="url(#lm)"/>
<rect x="1" y="1" width="${W - 2}" height="${H - 2}" rx="12" fill="none" stroke="${c.stroke}" stroke-width="1.5"/>
${markSVG(m, { nr: R * 0.105, hr: R * 0.15, pulses: true })}
<text class="wm wb" x="${tx}" y="106" textLength="193" lengthAdjust="spacingAndGlyphs">builder</text><text class="wm wmsh" x="${tx + 194}" y="106" textLength="160" lengthAdjust="spacingAndGlyphs">mesh</text>
<rect class="chip" x="${tx + 376}" y="72" width="76" height="28" rx="14"/><text class="chipt" x="${tx + 414}" y="91" text-anchor="middle">bmesh</text>
<text class="tag" x="${tx + 2}" y="154">A mesh for coding agents that propose, build, verify</text>
<text class="tag" x="${tx + 2}" y="183">and land changes on main, across every box you own.</text>
<text class="st" x="${tx + 2}" y="226">propose<tspan class="ar"> → </tspan>choose<tspan class="ar"> → </tspan>build<tspan class="ar"> → </tspan>verify<tspan class="ar"> → </tspan>land</text>
</svg>\n`);
}
