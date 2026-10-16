---
id: 050
title: The map draws each build family's coordinator as a layer above its leads, and leaves a faint trail where messages went
kind: ux
size: M
judge: opus
status: accepted
lens: ux
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
The coordinator isn't on the map. It dispatches a family of leads, relays their asks and sends them messages as `operator`. A message from a sender the map doesn't draw comes in from off the top of the map (`entry()` in `map.js`), so the operator's hand in the build is a line from nowhere. Every message flash also vanishes completely after `EDGE_MS` (1.2 s): the dot travels and the line fades in its last quarter. So a glance a minute later can't tell which lead talked to which executor, or which leads the coordinator has been steering. The operator's words: "I don't see coordinator as its own layer, which it should. If one thing has talked to another, leave a faint line indicating something has been exercised, after the initial pulse of message. The initial message pulse should stay longer and gradually fade away."

## Proposal
Builds on 049 (old alerts fold away), landed first: both change the band, sort and layout code in `map.js`.

In:
- **The coordinator layer.** Above the blocks, one row of coordinator boxes, one per build family that has a live lead (`family` on the lead's node, which `/api/tree` already carries):
  - Each box reads `<family>` on the first line and `coordinator · N leads` on the second.
  - A thin tree edge runs down to each of its leads' blocks, so a family reads as one unit.
  - Leads with no family (rounds, leads from before families) hang from no box.
  - When every lead of a family is done, its box goes with them to the shelf as a chip (`buildermesh-build-7 ✓`).
  - On the phone the layer is one row of chips above the stacked blocks.
- **Messages from the coordinator.** An inbox message from `operator` to a node starts at the box of that node's family (its own, or its lead's for an executor). It comes from off the map only when the node has no family.
- **The pulse.**
  - The dot's travel stays about 1.2 s.
  - The line then holds at full strength for 2 s.
  - It then fades over 8 s with an ease-out curve. It stops at a trail, not at nothing.
- **Trails.**
  - After its pulse, each sender and recipient pair keeps a faint line: about 15% opacity, 1 px, in the message kind's colour.
  - The line lasts as long as both ends are drawn. It is redrawn on every layout, as edges in flight already are.
  - A new message between the pair pulses again over the trail.
  - A message's trail is seeded from the replayed frames too. Today those only fill the recent-events drawer, so the trails show on page load without waiting for live traffic.
  - Hovering a trail names the pair and the message count (`buildermesh-046 → buildermesh-046.helpers · 4 messages`).
  - Routine traffic (`job`, `reply`) keeps its pulse but leaves no trail. A lead's dozens of verify jobs would otherwise web the map.
- README Map: the layer, the pulse timing and trails. The seed gets a family with two leads and a message from `operator`. `scripts/check.mjs` gains `coords=` and `trails=` in the title.

Out:
- Telling two coordinators apart when they send as `operator` to the same family. One family has one coordinator.
- A coordinator node in `bm-nodes`. The layer is drawn from the leads' `family`, and nothing new is registered.

Judge: opus, because it adds a layout row and a persistent edge layer to the existing SVG tree without new mesh state.

## What you'll see
At the top of the map, a `buildermesh-build-7 · coordinator · 3 leads` box with edges down to its three leads. A `bmesh hold` (048) sent from a chat travels visibly from that box to the lead, glows for a few seconds and settles into a hairline. After a busy hour, faint lines show which leads talked to which executors and which ones the coordinator has been steering.

## Verification
The repo's verify, then the browser check (`scripts/seed.sh`, then `scripts/check.mjs`) at 1280×800 and 390×844. It checks:
- `coords=1` for the seeded family;
- its message from `operator` starts at the family's box (the check reads the flash's path start);
- a trail present after the replay, and still present 15 s after a live message;
- no trail for a `job`.

## Risks and the alternative considered
Trails could turn into a web on a big family. The opacity is low, job traffic is excluded, and a pair keeps one line however many messages it carries. Alternative: a single `operator` box above everything. Rejected by the operator: a box per family shows which coordinator owns which leads.

## Needs from the operator
