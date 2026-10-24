#!/usr/bin/env python3
# Generates docs/assets/land.svg: a lead's `bmesh land` then `bmesh done`.
import re, sys

OUT = sys.argv[1]
LOOP = 16.0
FS = 14.0
CW = FS * 0.6          # mono advance; textLength pins every line to it
LH = 21.5
X0 = 28.0
TOP = 36.0
Y0 = TOP + 30.0
TITLE = "app-012 — ~/.t3/worktrees/app/bmesh-7c41e2d0…"

# (kind, text, appear-at seconds). Markup: {h:..} hash, {g:..} ok, {m:..} model, {e:..} emphasis, {L:..} landed.
LINES = [
    ("cmd", "bmesh land", 0.0, (0.5, 0.1)),
    ("out", "required: verify (the repo's verify kind), review (t:{h:81d0b7a4} at HEAD)", 1.8),
    ("out", "lease land.app", 2.3),
    ("out", "fetch origin/main {h:5e17c0d2}", 2.8),
    ("out", "rebase onto {h:5e17c0d2}: 3 commits", 3.3),
    ("out", "scaffold: follows the row", 3.8),
    ("out", "verify {g:ok} t:{h:3f2a9c1e} (recorded)", 5.5),
    ("out", "review {g:ok} t:{h:3f2a9c1e} m:{m:claude-opus-5-5} c:{h:81d0b7a4} (carried from t:{h:81d0b7a4}: the diff is unchanged)", 6.1),
    ("out", "{e:verified:} review {g:ok} t:{h:3f2a9c1e} m:{m:claude-opus-5-5} c:{h:81d0b7a4}; verify {g:ok} t:{h:3f2a9c1e}", 6.7),
    ("out", "operator steps: 1 (bmesh spec steps 012)", 7.2),
    ("out", "trailers {h:9b2e6f15}: Spec 012, Verified", 7.7),
    ("out", "push origin main {h:9b2e6f15}", 8.2),
    ("out", "main {h:9b2e6f15}", 8.7),
    ("out", "released land.app", 9.2),
    ("out", "{L:landed} {h:9b2e6f15}", 9.7),
    ("cmd", "bmesh done --commit 9b2e6f15", 10.1, (10.4, 0.05)),
    ("out", "{e:done} app-012 {h:9b2e6f15}", 12.3),
    ("cmd", "", 12.5, None),
]
FADE_OUT = 15.5

def pct(t):
    return f"{t / LOOP * 100:.3f}".rstrip("0").rstrip(".")

def esc(s):
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")

def spans(markup):
    """Returns (svg tspans, plain length)."""
    out, n = [], 0
    for m in re.finditer(r"\{(\w):([^}]*)\}|([^{]+)", markup):
        if m.group(3):
            out.append(esc(m.group(3))); n += len(m.group(3))
        else:
            out.append(f'<tspan class="{m.group(1)}">{esc(m.group(2))}</tspan>'); n += len(m.group(2))
    return "".join(out), n

css, body, cursors, covers = [], [], [], []
height = Y0 + (len(LINES) - 1) * LH + 26
for i, line in enumerate(LINES):
    kind, text, at = line[0], line[1], line[2]
    y = Y0 + i * LH
    cls = f"l{i}" if at > 0 else ""
    if at > 0:
        css.append(f".l{i}{{animation:l{i} {LOOP:g}s linear infinite}}"
                   f"@keyframes l{i}{{0%,{pct(at)}%{{opacity:0}}{pct(at + 0.08)}%,100%{{opacity:1}}}}")
    if kind == "cmd":
        n = 2 + len(text)
        inner = f'<tspan class="p">$</tspan> <tspan class="c">{esc(text)}</tspan>' if text else '<tspan class="p">$</tspan>'
        tl = f' textLength="{n * CW:.1f}" lengthAdjust="spacing"' if text else ""
        body.append(f'<text{" class=%s" % chr(34) + cls + chr(34) if cls else ""} x="{X0:g}" y="{y:g}"{tl}>{inner}</text>')
        cx = X0 + 2 * CW
        typing = line[3]
        if typing:
            start, per = typing
            w = len(text) * CW
            end = start + per * len(text)
            j = len(cursors)
            # The cover hides the command and steps right a character at a time;
            # its base (un-animated) place is past the command, so the still is whole.
            css.append(f".t{j}{{transform:translateX({w:.1f}px);animation:t{j} {LOOP:g}s infinite steps({len(text)},end)}}"
                       f"@keyframes t{j}{{0%,{pct(start)}%{{transform:translateX(0)}}{pct(end)}%,100%{{transform:translateX({w:.1f}px)}}}}")
            # Its cursor shows from the prompt until the output starts.
            hide = LINES[i + 1][2] - 0.15
            css.append(f".v{j}{{opacity:0;animation:v{j} {LOOP:g}s infinite steps(1,end)}}"
                       f"@keyframes v{j}{{0%,{pct(at)}%{{opacity:0}}{pct(at + 0.01)}%,{pct(hide)}%{{opacity:1}}{pct(hide + 0.01)}%,100%{{opacity:0}}}}")
            covers.append(f'<g class="t{j}"><rect class="cv" x="{cx:.1f}" y="{y - 15:g}" width="{w + 12:.1f}" height="20"/>'
                        f'<g class="v{j}"><rect class="cur" x="{cx:.1f}" y="{y - 13:g}" width="{CW:.1f}" height="17"/></g></g>')
            cursors.append(j)
        elif not text:
            body.append(f'<g class="{cls}"><rect class="cur" x="{cx:.1f}" y="{y - 13:g}" width="{CW:.1f}" height="17"/></g>')
    else:
        inner, n = spans(text)
        body.append(f'<text class="{cls}" x="{X0:g}" y="{y:g}" textLength="{n * CW:.1f}" lengthAdjust="spacing">{inner}</text>')

# While verify runs, the cursor waits on the empty line below scaffold.
wi = next(i for i, l in enumerate(LINES) if l[1].startswith("verify "))
w0, w1 = LINES[wi - 1][2] + 0.05, LINES[wi][2] - 0.02
css.append(f".w{{opacity:0;animation:w {LOOP:g}s infinite steps(1,end)}}"
           f"@keyframes w{{0%,{pct(w0)}%{{opacity:0}}{pct(w0 + 0.01)}%,{pct(w1)}%{{opacity:1}}{pct(w1 + 0.01)}%,100%{{opacity:0}}}}")
body.append(f'<g class="w"><rect class="cur" x="{X0:g}" y="{Y0 + wi * LH - 13:g}" width="{CW:.1f}" height="17"/></g>')
H = round(height)
svg = f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 960 {H}" width="960" height="{H}" role="img" aria-label="A lead runs bmesh land: it rebases its branch onto main, runs verify, carries its review across the rebase because the diff is unchanged, writes the verified line into the commit as trailers, pushes main, then runs bmesh done">
<title>bmesh land, then bmesh done</title>
<defs><clipPath id="k"><rect x=".75" y=".75" width="958.5" height="{H - 1.5:g}" rx="10"/></clipPath></defs>
<style>
text{{font:{FS:g}px ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,"Liberation Mono",monospace;fill:#8b949e;white-space:pre}}
.tt{{font:13px -apple-system,BlinkMacSystemFont,"Segoe UI","Noto Sans",Helvetica,Arial,sans-serif;fill:#8b949e}}
.p{{fill:#39c5bb}}.c{{fill:#e6edf3}}.h{{fill:#58a6ff}}.g{{fill:#3fb950}}.m{{fill:#a371f7}}
.e{{fill:#e6edf3;font-weight:600}}.L{{fill:#3fb950;font-weight:600}}
.cv{{fill:#0d1117}}.cur{{fill:#e6edf3;opacity:.85;animation:bl 1s infinite steps(1,end)}}
@keyframes bl{{0%{{opacity:.85}}50%{{opacity:0}}}}
.sc{{animation:sc {LOOP:g}s linear infinite}}
@keyframes sc{{0%{{opacity:0}}2%,{pct(FADE_OUT)}%{{opacity:1}}100%{{opacity:0}}}}
{chr(10).join(css)}
@media (prefers-reduced-motion:reduce){{*{{animation:none!important}}}}
</style>
<rect x=".75" y=".75" width="958.5" height="{H - 1.5:g}" rx="10" fill="#0d1117"/>
<rect clip-path="url(#k)" x="0" y="0" width="960" height="{TOP:g}" fill="#161b22"/>
<path d="M.75 {TOP:g}H959.25" stroke="#30363d" stroke-width="1.5"/>
<g opacity=".7"><circle cx="22" cy="18" r="6" fill="#f85149"/><circle cx="42" cy="18" r="6" fill="#d29922"/><circle cx="62" cy="18" r="6" fill="#3fb950"/></g>
<text class="tt" x="480" y="22.5" text-anchor="middle">{esc(TITLE)}</text>
<rect x=".75" y=".75" width="958.5" height="{H - 1.5:g}" rx="10" fill="none" stroke="#30363d" stroke-width="1.5"/>
<g class="sc">
{chr(10).join(body)}
</g>
{chr(10).join(covers)}
</svg>
'''
open(OUT, "w").write(svg)
print(OUT, len(svg.encode()), "bytes, height", H)
