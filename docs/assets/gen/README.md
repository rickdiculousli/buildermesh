# README art generators

Every SVG in `docs/assets/` is written by a script here; edit the script, never the SVG. None of these paths is in the repo's `code`, so a change here or to the art never causes a verify, a release or a box's update.

| Script | Writes | Run |
|---|---|---|
| `brand.mjs` | `logo.svg`, `banner-dark.svg`, `banner-light.svg` | `node docs/assets/gen/brand.mjs docs/assets` |
| `land.py` | `land.svg` | `python3 docs/assets/gen/land.py docs/assets/land.svg` |
| `lifecycle.py` | `lifecycle-dark.svg`, `lifecycle-light.svg` | `python3 docs/assets/gen/lifecycle.py docs/assets` |
| `mesh.mjs` | `mesh-dark.svg`, `mesh-light.svg` | `node docs/assets/gen/mesh.mjs docs/assets` |
| `frames.mjs` | PNG frames of an animated SVG, to check it | see Checking your work |

The `map-*.png` screenshots are of the running map, not generated.

## What GitHub does to an SVG

- A README shows an SVG through `<img>` (served via GitHub's image proxy, CSP
  `default-src 'none'; style-src 'unsafe-inline'`). So: **no `<script>`, no
  external fonts, images or `<use href>` to other files, no SMIL
  (`<animate>`, `<animateMotion>`, `<set>`)**. Animate with **CSS `@keyframes`
  in an inline `<style>`** only. `offset-path` motion is fine (CSS), but test it.
- Theme: an `<img>` SVG's own `prefers-color-scheme` follows the OS, not the
  GitHub theme. So themed diagrams ship as **two files**, `<name>-dark.svg`
  and `<name>-light.svg`, switched in the README by `<picture>`. Write ONE
  source with both palettes as CSS custom properties on the root
  (`svg{--bg:..}` dark by default, `svg.light{...}` overrides) and make the
  light file by changing only the root's `class` (`sed 's/class="dark"/class="light"/'`).
  Terminal-style visuals are dark in both themes and ship as one file.
- Fidelity comes before size, about 60/40: add the frames, states and motion a scene needs, then trim what costs bytes without showing anything (repeated markup, keyframe values). Keep files hand-readable, with no base64 blobs. The mesh, the largest, is about 60 KB.
- Size: `viewBox` 960 wide (the README column is about 880-1010 px on desktop,
  much narrower on a phone), height as the content needs. No text smaller than
  13 px at 960 wide; labels 14-16 px, titles 18-22 px.
- Transparent background for diagrams (they sit on GitHub's page); panels
  inside them have their own fill. A terminal is its own rounded card.

## Palette (from bmesh's own map, internal/web/map.css, so the README and the product look like one thing)

| role | dark | light |
|---|---|---|
| text | `#e6edf3` | `#1f2328` |
| muted text | `#8b949e` | `#59636e` |
| panel fill | `#161b22` | `#f6f8fa` |
| panel stroke | `#30363d` | `#d0d7de` |
| edge / link line | `#3d4652` | `#c3ccd5` |
| working (blue) | `#58a6ff` | `#0969da` |
| message (teal) | `#39c5bb` | `#1a7f72` |
| job (amber) | `#d29922` | `#9a6700` |
| reply / review (purple) | `#a371f7` | `#8250df` |
| blocked (red) | `#f85149` | `#cf222e` |
| ok / landed (green) | `#3fb950` | `#1a7f37` |
| held (slate) | `#8fa7c4` | `#57728f` |
| brand gradient | `#39c5bb -> #58a6ff` | `#1a7f72 -> #0969da` |

Terminal card: fill `#0d1117`, stroke `#30363d`, title bar `#161b22` with
three dots `#f85149 #d29922 #3fb950` at 70% opacity, prompt `$` in teal,
command text `#e6edf3`, output `#8b949e`, ok words green, hashes blue,
model ids purple.

## Type

- Sans: `-apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif`
- Mono: `ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace`
- Font metrics vary by OS: leave 15% slack in every box a label sits in;
  never let text touch a stroke. Use `text-anchor="middle"` for centred labels.
- Commands, paths, kinds and hashes in mono; prose labels in sans.

## Shapes and motifs

- Corner radius 10 for panels, 6 for chips, full pill for state badges.
- Strokes 1.5 px; edges 1.5 px, dashed `4 6` for "waits / queued" links.
- A node is a rounded rect with a small state dot (left) and a mono path.
- A message in flight is a 4-5 px dot in its kind's colour with a soft glow
  (a second circle at 30% opacity, r+4), moving along an edge.
- Motion is calm: ease-in-out, 0.4-0.8 s moves, a loop of 10-16 s with a
  short rest before it restarts. Nothing flashes or blinks faster than 1 Hz.
- `@media (prefers-reduced-motion: reduce) { * { animation: none !important } }`
  and the **un-animated state must be a complete, meaningful picture**
  (everything visible, final state), since some viewers see only that.
  So write keyframes so the element's base style is its "shown" state.

## Words

Every label must be true to the README (it is the reference). Use bmesh's own
words: lead, executor, routine, kind, verify, review, ledger, spec, round,
engineer, UI/UX engineer, hub, leaf, daemon, map, inbox, `bmesh land`,
`verified:` line. Plain words over jargon where both fit. No marketing
adjectives. No real hostnames of the author's fleet (no k16, no ricks-macbook):
use `hub-box`, `laptop`, `build-box`.

## Checking your work

Render frames with
`TMPDIR=$HOME/.cache/bmesh-scratch/tmp node docs/assets/gen/frames.mjs <svg> <outprefix> 960 dark 0,2,4,6,8 [reduce]`
(writes `<outprefix>-<t>.png`; frames freeze CSS animations at t seconds) and
look at the PNGs with the Read tool. Check: dark and light; mid-animation
frames; the reduced-motion frame; nothing overlaps, clips or runs off a box.
Write frames outside the repo, e.g. under `~/.cache/bmesh-scratch/`. `frames.mjs` needs Playwright's headless Chromium (`npx playwright install chromium-headless-shell`); its path is at the top of the script.
