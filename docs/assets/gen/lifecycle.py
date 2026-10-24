#!/usr/bin/env python3
# Generates docs/assets/lifecycle-{dark,light}.svg.
# Times below are in beats: one loop is L = 16 beats, played over DUR = 24 s (1 beat = 1.5 s).
# Each panel has three looks: waiting (dimmed, details in a quiet pending form), active (ring,
# full opacity, details complete in sequence) and completed (details kept, slightly dimmed).
# At the restart (RESET0..RESET1) completed details ease back to their pending form; nothing
# is ever wiped to an empty panel. The un-animated state is the complete finished picture.
import sys

L, DUR = 16.0, 24.0
RESET0, RESET1 = 15.2, 16.0  # completed details ease back to pending (22.8-24.0 s)
WAIT, DONE = ".65", ".75"     # panel opacity: waiting, completed (active is 1)
STAGGER, EASE = 0.1, 0.6      # reset: panel i eases back over [15.0 + i*STAGGER, +EASE] beats, Propose first
RS = [RESET0, RESET1]         # the current panel's reset window, set by panel_open
css_kf = []
seen = {}


def p(t):
    v = t / L * 100
    return ("%.2f" % v).rstrip("0").rstrip(".") + "%"


def kf(frames):
    """frames: list of (t, css). Returns a class name with its keyframes (shared when identical)."""
    body = "".join("%s{%s}" % (p(t), c) for t, c in frames)
    if body not in seen:
        name = "k%d" % (len(seen) + 1)
        seen[body] = name
        css_kf.append("@keyframes %s{%s}.%s{animation-name:%s}" % (name, body, name, name))
    return seen[body]


SANS = '-apple-system,BlinkMacSystemFont,"Segoe UI","Noto Sans",Helvetica,Arial,sans-serif'
MONO = 'ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,"Liberation Mono",monospace'

# Panels: (key, x, w, title, command, active window in beats)
P = [
    ("p", 12, 176, "Propose", "ideate app", (0.0, 2.6)),
    ("c", 200, 168, "Pick", "build app 012", (2.6, 5.2)),
    ("b", 380, 176, "Build", "executor new", (5.2, 7.9)),
    ("v", 568, 208, "Verify", "routine", (7.9, 10.5)),
    ("l", 788, 160, "Land", "land", (10.5, 15.0)),
]
PY, PH = 8, 240
TRACK = 280
cx = {k: x + w / 2 for k, x, w, *_ in P}

out = []
A = out.append


def panel_open(k, x, w, title, cmd, win):
    t0, t1 = win
    i = [q[0] for q in P].index(k)
    RS[0] = 15.0 + i * STAGGER
    RS[1] = RS[0] + EASE
    r0, r1 = RS
    o = lambda v: "opacity:%s" % v
    if k == "p":
        # Propose is lit again by the end of the loop, so the restart needs no ramp
        dim = kf([(0, o(1)), (t1, o(1)), (t1 + 0.4, o(DONE)), (r1, o(DONE)), (L, o(1))])
        ring = kf([(0, o(1)), (t1, o(1)), (t1 + 0.4, o(0)), (r1, o(0)), (L, o(1))])
    elif k == "l":
        dim = kf([(0, o(WAIT)), (t0, o(WAIT)), (t0 + 0.4, o(1)), (min(t1, r0), o(1)), (r1, o(WAIT)), (L, o(WAIT))])
        ring = kf([(0, o(0)), (t0, o(0)), (t0 + 0.4, o(1)), (t1, o(1)), (t1 + 0.4, o(0)), (L, o(0))])
    else:
        dim = kf([(0, o(WAIT)), (t0, o(WAIT)), (t0 + 0.4, o(1)), (t1, o(1)), (t1 + 0.4, o(DONE)), (r0, o(DONE)), (r1, o(WAIT)), (L, o(WAIT))])
        ring = kf([(0, o(0)), (t0, o(0)), (t0 + 0.4, o(1)), (t1, o(1)), (t1 + 0.4, o(0)), (L, o(0))])
    c = x + w / 2
    A('<g class="an %s">' % dim)
    A('<rect class="pn" x="%g" y="%g" width="%g" height="%g" rx="10"/>' % (x, PY, w, PH))
    A('<rect class="an rg %s" x="%g" y="%g" width="%g" height="%g" rx="10"/>' % (ring, x, PY, w, PH))
    A('<text class="tt" x="%g" y="40" text-anchor="middle">%s</text>' % (c, title))
    A('<text class="cm" x="%g" y="62" text-anchor="middle"><tspan class="mu">bmesh </tspan>%s</text>' % (c, cmd))


def swap(t0, pend, comp, d=0.4):
    """A detail in its pending form until t0, complete from t0+d until the restart eases it back."""
    r0, r1 = RS
    on = kf([(0, "opacity:0"), (t0, "opacity:0"), (t0 + d, "opacity:1"), (r0, "opacity:1"), (r1, "opacity:0"), (L, "opacity:0")])
    off = kf([(0, "opacity:1"), (t0, "opacity:1"), (t0 + d, "opacity:0"), (r0, "opacity:0"), (r1, "opacity:1"), (L, "opacity:1")])
    A('<g class="an hid %s">%s</g><g class="an %s">%s</g>' % (off, pend, on, comp))


def pill(x, y, w, word, col, h=20, mono=False):
    cls = "pm" if mono else "pt"
    return ('<rect class="pl" style="--c:var(--%s)" x="%g" y="%g" width="%g" height="%g" rx="%g"/>'
            '<text class="%s" style="fill:var(--%s)" x="%g" y="%g" text-anchor="middle">%s</text>'
            % (col, x, y, w, h, h / 2, cls, col, x + w / 2, y + h / 2 + 4.5, word))


def slot(x, y, w, h=20):
    return '<rect class="qd" x="%g" y="%g" width="%g" height="%g" rx="%g"/>' % (x, y, w, h, h / 2)


# ---------------- Propose ----------------
panel_open(*P[0])
A('<g transform="translate(4 0)">')
A('<text class="lb l14" x="28" y="104">one ranked list</text>')
for y, ids, t in [(112, ["012", "013", "014"], 0.4), (144, ["015", "016", "017"], 0.55)]:
    for i, sid in enumerate(ids):
        cxx = 28 + i * 48
        swap(t + i * 0.3, '<rect class="qd" x="%g" y="%g" width="40" height="24" rx="6"/>' % (cxx, y),
             '<rect class="sc" x="%g" y="%g" width="40" height="24" rx="6"/><text class="mo" x="%g" y="%g" text-anchor="middle">%s</text>' % (cxx, y, cxx + 20, y + 16.5, sid))
A('<text class="mo mu" x="96" y="200" text-anchor="middle">docs/proposals/</text>')
swap(1.9, slot(14, 212, 164, 26),
     '<rect class="pl" style="--c:var(--teal)" x="14" y="212" width="164" height="26" rx="13"/>'
     '<text style="fill:var(--teal)" x="96" y="229.5" text-anchor="middle">app round 4 proposed</text>')
A('</g></g>')

# ---------------- Pick ----------------
panel_open(*P[1])
A('<g transform="translate(8 0)">')
BUBBLE = "M214 76H338A10 10 0 0 1 348 86V112A10 10 0 0 1 338 122H336L340 131L326 122H214A10 10 0 0 1 204 112V86A10 10 0 0 1 214 76Z"
swap(3.1, '<path class="qs" d="%s"/><text class="tx gh" x="216" y="96">1 yes, 2 no,</text><text class="tx gh" x="216" y="114">4 later</text>' % BUBBLE,
     '<path class="bb" d="%s"/><text class="tx" x="216" y="96">1 yes, 2 no,</text><text class="tx" x="216" y="114">4 later</text>' % BUBBLE)
for i, (sid, word, col) in enumerate([("012", "started", "blue"), ("013", "rejected", "red"), ("017", "later", "slate")]):
    y = 140 + i * 24
    A('<text class="mo mu" x="220" y="%g">%s</text>' % (y + 14.5, sid))
    swap(3.7 + i * 0.22, slot(252, y, 84), pill(252, y, 84, word, col))
A('</g></g>')

# ---------------- Build ----------------
panel_open(*P[2])
c = cx["b"]
LEAD = ('<rect class="%s" x="%g" y="74" width="140" height="44" rx="8"/><circle cx="%g" cy="91" r="4" fill="var(--%s)"/>'
        '<text class="mo%s" x="%g" y="95.5">app-012</text>')
swap(5.7, LEAD % ("qs", c - 70, c - 52, "edge", " mu", c - 42), LEAD % ("nd", c - 70, c - 52, "blue", "", c - 42))
A('<text class="lb" x="%g" y="111" text-anchor="middle">lead, own worktree</text>' % c)
EX = [(c - 54, ".api"), (c, ".ui"), (c + 54, ".cli")]
branch = {}
for i, (ex, name) in enumerate(EX):
    d = "M%g 118C%g 136 %g 132 %g 150" % (c, c, ex, ex)
    branch[name] = d
    node = ('<path class="%s" d="%s"/><rect class="%s" x="%g" y="150" width="48" height="26" rx="6"/>'
            '<text class="mo%s" x="%g" y="167.5" text-anchor="middle">%s</text>')
    swap(6.2 + i * 0.2, node % ("ed", d, "qs", ex - 24, " mu", ex, name), node % ("br", d, "nd", ex - 24, "", ex, name))
# reports travelling back up to the lead (active phase only)
for i, (ex, name) in enumerate(EX):
    t0 = 7.0 + i * 0.12
    mv = kf([(0, "offset-distance:100%;opacity:0"), (t0, "offset-distance:100%;opacity:0"), (t0 + 0.1, "offset-distance:100%;opacity:1"),
             (t0 + 0.6, "offset-distance:0%;opacity:1"), (t0 + 0.75, "offset-distance:0%;opacity:0"), (L, "offset-distance:0%;opacity:0")])
    A('<g class="an mv %s" style="offset-path:path(\'%s\')"><circle r="8" fill="var(--teal)" opacity=".3"/><circle r="4" fill="var(--teal)"/></g>' % (mv, branch[name]))
A('<text class="lb" x="%g" y="200" text-anchor="middle">executors on branches</text>'
  '<text class="lb" x="%g" y="219" text-anchor="middle">report to the lead\'s inbox</text>' % (c, c))
A('</g>')

# ---------------- Verify ----------------
panel_open(*P[3])
A('<g transform="translate(8 0)">')
A('<text class="lb" x="574" y="92">repo checks, run inline</text>')
swap(8.6, '<rect class="qs" x="572" y="100" width="184" height="26" rx="6"/><text class="mo mu" x="584" y="117.5">verify<tspan class="gh"> ok t:81d0b7a4</tspan></text>',
     '<rect class="ch" x="572" y="100" width="184" height="26" rx="6"/>'
     '<text class="mo" x="584" y="117.5">verify <tspan class="ok">ok</tspan> <tspan class="hs">t:81d0b7a4</tspan></text>')
A('<text class="lb" x="574" y="150">queued, on Opus</text>')
swap(9.5, '<rect class="qs" x="572" y="158" width="184" height="44" rx="6"/><text class="mo mu" x="584" y="176">review<tspan class="gh"> ok t:81d0b7a4</tspan></text><text class="mo mu gh" x="584" y="194">m:claude-opus-5-5</text>',
     '<rect class="ch" x="572" y="158" width="184" height="44" rx="6"/>'
     '<text class="mo" x="584" y="176">review <tspan class="ok">ok</tspan> <tspan class="hs">t:81d0b7a4</tspan></text>'
     '<text class="mo" x="584" y="194"><tspan class="md">m:claude-opus-5-5</tspan></text>')
A('<text class="lb" x="664" y="226" text-anchor="middle">recorded by tree hash</text>')
A('</g></g>')

# ---------------- Land ----------------
panel_open(*P[4])
swap(11.2, '<rect class="qs" x="800" y="74" width="136" height="88" rx="6"/><text class="mo mu" x="812" y="94">verified:</text>',
     '<rect class="ch" x="800" y="74" width="136" height="88" rx="6"/><text class="mo" x="812" y="94">verified:</text>')
# entries sorted by kind, as bmesh spec verified writes them
swap(11.5, slot(812, 104, 92), pill(812, 104, 92, "review ok", "green", mono=True))
swap(11.8, slot(812, 132, 92), pill(812, 132, 92, "verify ok", "green", mono=True))
# main line
A('<text class="mo mu" x="800" y="197">main</text><path class="ed" d="M842 192H934"/>')
for gx in (856, 880, 904):
    A('<circle class="cmt" cx="%g" cy="192" r="4.5"/>' % gx)
# the lead's commit on its branch above main; on landing the dot moves onto main and turns green
curve = kf([(0, "opacity:0"), (10.9, "opacity:0"), (11.3, "opacity:1"), (12.0, "opacity:1"), (12.4, "opacity:0"), (L, "opacity:0")])
A('<path class="an hid br %s" d="M908.5 192C918 192 916 172 928 172"/>' % curve)
blue = kf([(0, "opacity:0;transform:translateY(0)"), (10.9, "opacity:0;transform:translateY(0)"), (11.3, "opacity:1;transform:translateY(0)"),
           (12.0, "opacity:1;transform:translateY(0)"), (12.4, "opacity:1;transform:translateY(20px)"), (12.6, "opacity:0;transform:translateY(20px)"), (L, "opacity:0;transform:translateY(20px)")])
A('<circle class="an hid %s" cx="928" cy="172" r="5" fill="var(--blue)"/>' % blue)
green = kf([(0, "opacity:0"), (12.3, "opacity:0"), (12.5, "opacity:1"), (RS[0], "opacity:1"), (RS[1], "opacity:0"), (L, "opacity:0")])
A('<g class="an %s"><path class="ok2" d="M908.5 192H922.5"/><circle cx="928" cy="192" r="5.5" fill="var(--green)"/></g>' % green)
A('<text class="lb" x="868" y="228" text-anchor="middle">rebase, check, push</text>')
A('</g>')

# arrows between panels
for (k1, x1, w1, *_), (k2, x2, *_) in zip(P, P[1:]):
    g = (x1 + w1 + x2) / 2
    A('<path class="ar" d="M%g 124l4 6-4 6"/>' % (g - 2))

# ---------------- track, loop back, label ----------------
R_, LB = 12, TRACK + 32  # loop corner radius, loop bottom
ARC = "A%d %d 0 0 1" % (R_, R_)
LOOP = "M940 %d %s 952 %d V%d %s 940 %d H20 %s 8 %d V%d %s 20 %d" % (
    TRACK, ARC, TRACK + R_, LB - R_, ARC, LB, ARC, LB - R_, TRACK + R_, ARC, TRACK)
A('<path class="ed" d="M20 %d H940"/>' % TRACK)
A('<path class="lp" d="%s"/>' % LOOP)
for ax in (330, 640):
    A('<path class="ed" d="M%g %gl6 -5M%g %gl6 5"/>' % (ax, LB, ax, LB))
for k, *_ in P:
    A('<path class="ed" d="M%g %gV%g"/><circle class="st" cx="%g" cy="%d" r="3.5"/>' % (cx[k], PY + PH, TRACK, cx[k], TRACK))
A('<text class="lb" x="480" y="%d" text-anchor="middle">leads\' feedback becomes new specs · <tspan class="mo mu">bmesh</tspan> <tspan class="mo mu">release</tspan> tags the finished batch</text>' % (LB + 26))

# the dot that carries the eye back to the start
DOT = "M%g %d H940 %s 952 %d V%d %s 940 %d H20 %s 8 %d V%d %s 20 %d H%g" % (
    cx["l"], TRACK, ARC, TRACK + R_, LB - R_, ARC, LB, ARC, LB - R_, TRACK + R_, ARC, TRACK, cx["p"])
mv = kf([(0, "offset-distance:0%;opacity:0"), (14.25, "offset-distance:0%;opacity:0"), (14.4, "offset-distance:0%;opacity:1"),
         (15.6, "offset-distance:100%;opacity:1"), (15.75, "offset-distance:100%;opacity:0"), (L, "offset-distance:100%;opacity:0")])
A('<g class="an mv %s" style="offset-path:path(\'%s\')"><circle r="9" fill="var(--teal)" opacity=".3"/><circle r="4.5" fill="var(--teal)"/></g>' % (mv, DOT))

# ---------------- the spec card (token) ----------------
# It slides back from Land to Propose at the restart, switching done -> proposed as it starts.
SLIDE = 15.2
pos = {k: cx[k] - cx["l"] for k in cx}
T = lambda k: "transform:translateX(%gpx)" % pos[k]
card = kf([(0, T("p")), (2.6, T("p")), (3.2, T("c")), (5.2, T("c")), (5.8, T("b")), (7.9, T("b")), (8.5, T("v")),
           (10.5, T("v")), (11.1, T("l")), (SLIDE, T("l")), (L, T("p"))])
A('<g transform="translate(%g %d)"><g class="an %s">' % (cx["l"], TRACK, card))
A('<rect class="tk" x="-62" y="-16" width="124" height="32" rx="8"/><text class="mo b" x="-50" y="4.5">012</text>')


def cpill(word, col):
    # an opaque base under the tint, so a newer pill fading in over an older one never shows a gap
    return '<rect x="-14" y="-10" width="72" height="20" rx="10" fill="var(--pan)"/>' + pill(-14, -10, 72, word, col)


X = 0.2  # beats a new status takes to fade in over the old one; the old one is cut when it is covered
o = lambda v: "opacity:%d" % v
CUT = 0.01
A('<g class="an hid %s">%s</g>' % (kf([(0, o(1)), (3.8 + X, o(1)), (3.8 + X + CUT, o(0)), (SLIDE, o(0)), (SLIDE + CUT, o(1)), (L, o(1))]), cpill("proposed", "slate")))
A('<g class="an hid %s">%s</g>' % (kf([(0, o(0)), (3.8, o(0)), (3.8 + X, o(1)), (12.38 + X, o(1)), (12.38 + X + CUT, o(0)), (L, o(0))]), cpill("building", "blue")))
A('<g class="an %s">%s</g>' % (kf([(0, o(0)), (12.38, o(0)), (12.38 + X, o(1)), (SLIDE, o(1)), (SLIDE + CUT, o(0)), (L, o(0))]), cpill("done", "green")))
A('</g></g>')

H = LB + 38
style = """
svg{--fg:#e6edf3;--mut:#8b949e;--pan:#161b22;--str:#30363d;--edge:#3d4652;--chip:#0d1117;--blue:#58a6ff;--teal:#39c5bb;--amber:#d29922;--purple:#a371f7;--red:#f85149;--green:#3fb950;--slate:#8fa7c4;--b1:#39c5bb;--b2:#58a6ff}
svg.light{--fg:#1f2328;--mut:#59636e;--pan:#f6f8fa;--str:#d0d7de;--edge:#c3ccd5;--chip:#ffffff;--blue:#0969da;--teal:#1a7f72;--amber:#9a6700;--purple:#8250df;--red:#cf222e;--green:#1a7f37;--slate:#57728f;--b1:#1a7f72;--b2:#0969da}
text{font-family:%s;font-size:13px;fill:var(--fg)}
.tt{font-size:18px;font-weight:600}
.l14{font-size:14px}
.cm,.mo,.pm{font-family:%s;font-size:13px}
.mu,.lb{fill:var(--mut)}
.b{font-weight:600}.ok{fill:var(--green)}.hs{fill:var(--blue)}.md{fill:var(--purple)}
.pt,.pm{font-weight:500}
.pn{fill:var(--pan);stroke:var(--str);stroke-width:1.5}
.rg{fill:none;stroke:url(#g);stroke-width:1.5;opacity:0}
.sc{fill:var(--chip);stroke:var(--slate);stroke-width:1.5}
.ch,.nd,.qs{fill:var(--chip);stroke:var(--str);stroke-width:1.5}
.nd{stroke:var(--blue)}.qs{stroke:var(--edge)}
.qd{fill:none;stroke:var(--mut);stroke-opacity:.5;stroke-width:1.5;stroke-dasharray:3 3}
.bb{fill:var(--chip);stroke:var(--purple);stroke-width:1.5}
.pl{fill:var(--c);fill-opacity:.14;stroke:var(--c);stroke-width:1.5}
.tk{fill:var(--pan);stroke:var(--str);stroke-width:1.5}
.ed,.br,.lp,.ok2{fill:none;stroke:var(--edge);stroke-width:1.5;stroke-linecap:round;stroke-linejoin:round}
.br{stroke:var(--blue);stroke-opacity:.6}.ok2{stroke:var(--green)}
.lp{stroke-dasharray:4 6}
.cmt{fill:var(--pan);stroke:var(--mut);stroke-width:1.5}
.st{fill:var(--edge)}
.tx{fill:var(--fg)}
.gh{fill-opacity:.2}
.hid,.mv{opacity:0}
.an{animation-duration:%gs;animation-iteration-count:infinite;animation-timing-function:ease-in-out}
.ar{fill:none;stroke:var(--mut);stroke-width:1.5;stroke-linecap:round;stroke-linejoin:round}
@supports not (offset-path:path('M0 0')){.mv{display:none}}
@media (prefers-reduced-motion:reduce){*{animation:none!important}}
""" % (SANS, MONO, DUR)

svg = ['<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 960 %d" width="960" height="%d" class="dark" role="img" aria-label="How a change goes from idea to main in buildermesh: propose, pick, build, verify, land, and back again">' % (H, H),
       '<title>The buildermesh lifecycle: propose, pick, build, verify, land</title>',
       '<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" style="stop-color:var(--b1)"/><stop offset="1" style="stop-color:var(--b2)"/></linearGradient></defs>',
       '<style>' + style.strip() + "\n" + "\n".join(css_kf) + '</style>']
svg += out
svg.append('</svg>')
s = "\n".join(svg) + "\n"
dst = sys.argv[1]
open(dst + "/lifecycle-dark.svg", "w").write(s)
open(dst + "/lifecycle-light.svg", "w").write(s.replace('class="dark"', 'class="light"', 1))
print(len(s))
