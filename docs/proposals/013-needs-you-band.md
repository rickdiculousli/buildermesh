---
id: 013
title: Asks, blocks and silences carry their words onto the map
kind: ux
size: S
judge: fable
status: proposed
lens: ux
proposed_by: f3d2851e-4460-4cd8-87b7-359407c1c774
proposed_on: 2026-10-15
---

## Opportunity
A lead's `ask` and `blocked` are the two events the daemon pushes to the operator's phone, and the map is where the push's `Click` lands (`<map_url>#/lead/<path>`, README, Pushes). On the map itself, though, an ask is a yellow ring on a box and the word `asking` in 11 px, and the question is nowhere: the node's second line is `state · N unread`, and the text travels only to the lead page's header and timeline.

![map phone](assets/013-map-phone.png)

A `silent` lead (the daemon's "stalled" verdict, `[liveness] grace`) is drawn at 40 % opacity (`.st-silent { opacity: .4 }` in `map.css`): the one state that means "a node may be stuck" is the faintest thing on the screen, fainter than `done`.

The recent-events drawer is the other place the words go, and there a long inbox message fills the whole phone screen, twice (the operator's round brief, sent to `.eng` and `.ux`):

![drawer phone](assets/013-drawer-phone.png)

So the operator on the phone opens the map and still has to tap into each lead to learn what it wants; the lead page then shows the question in a `muted` span after the badge.

![lead page](assets/013-lead-desktop.png)

## Proposal
In:
- A "needs you" band under the strip, shown only when a lead or engineer is `asking`, `blocked` or `silent`: one row per node, its state badge, its path, and the event's text (the ask or block text; for silent, "quiet for 25m" from its last event's time), cut to three lines with the rest on tap. The row links to the lead page. `done` clears it; so does the next `progress` from that node.
- On the map, an `asking` or `blocked` box's second line is the text's first 16 characters' worth (what `room()` fits) after the state, instead of the unread count; `silent` boxes draw at full opacity with the `blocked` ring colour in a dashed stroke.
- The drawer and the lead page's inbox and timeline clamp each row's text to three lines; a tap on the row expands it. The row's path drops the repeated prefix when the drawer is on a lead page.

Out: replying from the map (the thread is the place for that; the lead page's `console` link already goes there), sound, any new event.

Judge: fable, because the band must be loud enough to stop a thumb and quiet enough to live under the quota strip, and that balance is a taste call.

## What you'll see
A lead asks; the operator's push opens the map and the top of the page reads, in one row, who asks and what. A node that goes quiet is outlined, not faded. The drawer lists thirty events on one phone screen instead of three.

## Verification
The verify commands; a web test that serves a tree with an `asking` lead and checks the band holds the event text; the `?check=1` self-check counts the band as a tap target (min 40 px). README's map paragraph names the band and the silent styling.

## Risks and the alternative considered
Alternatives: (a) put the text only on the node box: 184 px fits nineteen characters, so it is a teaser, not the ask; (b) badge the header nav with a count: tells you something waits, not what; (c) rely on the push notification body: the push is gone once dismissed and a second ask overwrites it in the eye.

The case that users don't feel it: a mesh with one lead and an operator at a desk sees the ring and clicks. The map's own README says the phone is a first-class viewport (the 699 px breakpoint, the bottom sheet), and spec 006 records that "on the phone the chat is the interface"; the map has to carry the words there.

## Needs from the operator
