---
id: 015
title: The strip counts the mesh live instead of quoting the last digest
kind: refinement
size: XS
judge: sonnet
status: proposed
lens: ux
proposed_by: f3d2851e-4460-4cd8-87b7-359407c1c774
proposed_on: 2026-10-15
---

## Opportunity
The strip under the header is the first line of every page, and its third row is the latest digest: `digest 50m ago: bmesh k16: 0 working, 0 asking, 0 silent`. At the moment of the screenshot a round and two engineers were `working` on the same page, two boxes down.

![the strip](assets/015-strip.png)

The digest is right for its hour (`[notify] digest_every = "1h"`) and wrong for the page, and it is the only summary line the map has. The connection badge beside the title says `live`; the digest line beneath it says the opposite of what the tree shows.

## Proposal
In:
- The strip's summary row is computed from the loaded tree on every render: `N working · N asking · N blocked · N silent · N done`, zero counts dropped, the words coloured as the badges are (`asking` yellow, `blocked` red). It updates with the same live events that restyle the nodes.
- The digest keeps its place as the fold below (`▸ digest 50m ago`), opening to the body as now, so the hourly text stays reachable but no longer reads as the present.

Out: changing what the daemon digests, or the push bodies.

Judge: sonnet, because the counts come from data the page already holds and the layout already exists.

## What you'll see
The top of every page says what the mesh is doing now; when a lead asks, the word `asking` appears there within a second, before you find the box.

## Verification
The verify commands; a `web_test.go` case that the `?check=1` title reports `strip=` with the summary row present when the tree is non-empty; README's strip sentence names the summary row.

## Risks and the alternative considered
Alternatives: (a) shorten the digest interval: still stale between runs and costs a push each time; (b) drop the digest line from the strip: loses the hourly body, which is the one place the daemon's own wording is read. Keeping both, present above past, costs one row.

The case that users don't feel it: an operator who reads the boxes and ignores the strip. The strip sits above the fold on the phone where the boxes do not (see 012), so it is read first.

## Needs from the operator
