---
id: 017
title: Recent events draw each node as a coloured chip in its own lane
kind: ux
size: S
judge: opus
status: accepted
lens: ux
proposed_by: 3ac8b8a4-8c59-4284-8f78-728ed0e1a551
proposed_on: 2026-10-15
---

## Opportunity
The recent-events drawer is the operator's live log of who did what, and every row looks alike. `drawRecent()` in `map.js` writes `time path event text` as plain spans; the path is bold black text in the same column on every row, so following one node through a burst means re-reading names. Only the event word is coloured (`.ev-ask`, `.ev-blocked`, `.ev-started`, `.ev-inbox`). During a build, the lead, its executors, the review routine and the daemon interleave, and an inbox row (`from → to`) reads the same as a state change. The operator, in their own words (2026-10-15): "color code recent events names, make them chips, so it's easier to monitor what did what."

## Proposal
In (the drawer only, `map.js` and `map.css`):
- **Chips.** The path is a chip: rounded, a tinted background with text in the same hue. The hue is stable per lead tree (a hash of the top segment of the path, over a fixed palette of about eight hues readable in light and dark), so a lead and its executors share a hue and a child is a lighter step of it. Routines and `daemon.*` take a neutral grey chip. The chip drops the parent prefix when the parent's chip is in the same row's lane set (`eng`, not `buildermesh-round-1.eng`); the full path stays in `title`.
- **Lanes.** Each path in the visible list gets a lane, a fixed horizontal offset (about 14 px a step) assigned in order of first appearance, newest first, so a row's chip sits at its node's offset. A thin vertical line runs through each lane in the chip's hue at low opacity, top to bottom of the list, like a git graph, so one node can be followed down the log by eye. At most six lanes on the desktop and three under the 699 px breakpoint; paths past that share the last lane (its line grey). An inbox row's chip sits in the sender's lane, followed by `→` and a small chip for the recipient in its hue.
- **Focus.** Hovering a chip (or its line) focuses that path: other rows dim to about 35 % opacity, the lane's line goes full opacity and slightly wider. Clicking a chip pins the focus, so it survives the mouse leaving and new events arriving; clicking it again, or Escape, unpins. Clicking another chip moves the pin. On touch, a tap pins and a second tap unpins (no hover). Rows naming the path as a recipient (`→ path`) stay lit with the focused path.
- The event word keeps its colours; the time column stays first and unshifted.

Out: the lead page's timeline (same rendering would fit, a later spec once this one reads right), filtering rows out instead of dimming, a legend, any server change (`describe()` already gives path, event and text).

Judge: fable, because the palette, the lane width and how far the dimming goes decide whether this reads as calm or busy, and the spec can't pin that.

## What you'll see
Open the drawer during a build: the lead's rows are a teal chip in lane one, its executor's a lighter teal in lane two, the review routine grey in lane three, each with a faint line running down. Hover the executor: everything else fades and its line brightens. Click it: it stays that way while new rows stream in, until you click it again.

## Verification
The verify commands pass. The browser check runs against the branch's own binary (`go run ./cmd/bmesh serve` with a test config on another port, since the daemon on 3774 is the installed `main`) with a headless Chromium script at 1280x800 and 390x844 that opens the drawer on a mesh with at least three paths: each `li` has one chip; two rows of the same path have equal chip `offsetLeft` and colour; hovering a chip sets a dimmed class on other rows; a click persists it across a new frame and a second click clears it; `?check=1` still reads `ok` (no sideways scroll at 390). README's `### Map` section (012's, or here) gets a sentence on the drawer's chips, lanes and pinning.

## Risks and the alternative considered
Alternatives: (a) colour only the path text: helps scanning but not following one node across a burst; (b) a filter dropdown per path: a second control to operate, and filtering hides the interleaving the operator wants to watch; (c) one column per node, a full grid: too wide on the phone. Lanes with dimming keep every row visible and make one node stand out on demand.

Risk: hash collisions give two leads the same hue; with eight hues and the handful of live leads 012 leaves on the first screen that is rare, and the lane offset still tells them apart.

## Needs from the operator
