---
id: 051
title: The map gets one design pass: a calm page where the live work is the first thing you see
kind: ux
size: M
judge: fable
status: accepted
lens: ux
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
The map grew one spec at a time (012, 013, 015, 016, 017, 028), and it shows. On k16's map at 1280×800 (`assets/049-map-old-news.png`):
- **The done shelf takes over.** 41 done chips fill about 300 px, the biggest thing on the page, while four live leads get one 48 px row.
- **The top of the page is four stacked lines of plain grey text.** The summary, two quota lines and the digest push the band and the map below the fold on a laptop.
- **The band and the boxes say the same thing at two sizes.** Each asking box's second line is the band's text cut at about 20 characters (`asking · Your go arrived, b…`), so it is neither readable nor quiet.
- **The routines column floats.** It sits about 130 px right of the last lead with small grey labels, and doesn't read as part of the same page.
- **The boxes are flat.** Rectangles with a doubled stroke, and the only motion on the page is the `working` stroke pulse.

The operator asked for "overall aesthetic improvements". The pieces work. The page lacks one hand.

## Proposal
Builds on 049 (old alerts fold away) and 050 (the coordinator layer and message trails), landed first, so the pass styles their surfaces too.

In:
- **One visual system** in `map.css` tokens:
  - a type scale of three sizes and two weights;
  - a 4 px spacing grid;
  - one corner radius for boxes, chips and cards;
  - state colours checked for contrast in light and dark (badge text at least 4.5:1).
  - The boxes get a single stroke, a state-tinted fill for `asking`, `blocked` and `held`, and a subtle shadow for the live ones. The `working` pulse becomes a soft glow, not a flashing stroke.
- **The top strip as one line.** The summary counts and the quota as compact chips on one row (`3 asking · 1 blocked · claude 15% 5h · 64% wk`). The digest stays a fold at the row's end.
- **The shelf folds.** One row of the newest done chips, then `+35 more` that opens the rest in place. The `done (41)` head stays.
- **Boxes carry identity, the band carries words.** An `asking` or `blocked` box's second line is its state and age (`asking · 4m`), and the words live only in the band and on the lead page.
- **Routines** align to the blocks' grid as a labelled lane, with the same box style as leads.
- **Motion** is restrained and consistent: the message pulse and trails from 050, a quick fade when a block moves in need order, nothing else animated. `prefers-reduced-motion` turns the pulse into a plain line and stops the glow.
- The lead page's cards share the tokens.
- README Map wherever a described look changes (the box's second line, the shelf fold, the strip row).

Out:
- New data, new endpoints, and layout changes to what is drawn where (049 and 050 own those).
- A framework or a build step. The page stays plain JS and CSS served from `internal/web/`.

Judge: fable, because the whole spec is taste (hierarchy, spacing, colour and motion made to read as one), the one case the guide reserves for it.

## What you'll see
At 1280×800 the strip is one line, the band's live rows come next, and the leads with their coordinator box sit well above the fold. The done shelf is one quiet row. The page looks like one thing in light and dark, and on the phone the same order holds without crowding.

## Verification
The repo's verify, then the browser check (`scripts/seed.sh`, then `scripts/check.mjs`) at 1280×800 and 390×844, light and dark. It checks:
- the first lead block's top edge sits above 320 px at 1280×800 (a new `firstBlock=` in the title);
- the shelf is one row until opened;
- `strip=` counts one row plus the digest fold;
- badge contrast at or above 4.5:1 (the check computes it from the computed styles).

Before and after screenshots go in `assets/051-*.png`, and the lead links them in its final message for the operator.

## Risks and the alternative considered
Taste is the operator's. The screenshots in the final message are where they say what's wrong, and a follow-up is a small spec. Alternative: fold these fixes into 049 and 050. Rejected: those are behaviour, this is the look, and one hand over the whole page is the point.

## Needs from the operator
A look at the before and after screenshots when it lands.
