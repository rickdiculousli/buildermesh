---
id: 012
title: The map leads with what's live and folds what's done
kind: ux
size: S
judge: fable
status: proposed
lens: ux
proposed_by: f3d2851e-4460-4cd8-87b7-359407c1c774
proposed_on: 2026-10-15
---

## Opportunity
The map answers "what is the mesh doing right now", and today the answer is below the fold on both viewports. Leads are laid out in `started_at` order, so the oldest, finished ones come first and the one live lead comes last.

At 1280x800 the page is 2136 px wide (`?check=1` reports `SCROLLS scrollWidth=2136`): the first screen is seven grey `done` blocks, and `buildermesh-round-1`, the only `working` lead, starts at roughly x=1860, off the right edge, with the routines column further right still.

![map desktop](assets/012-map-desktop.png)

At 390x844 the blocks stack. The working round is the eighth block; its top edge sits under the closed drawer, so the first paint shows nothing but done boxes and a check mark per box.

![map phone, full page](assets/012-map-phone-full.png)

The map draws every lead ever registered in `bm-nodes`; a node and its branch "stay in `bm-nodes` for the map" (README, Executors), so the done set only grows and the live set never gets the first screen back.

## Proposal
Order and fold, no new data.

In:
- Blocks run in need order, not start order: `blocked`, `asking`, `silent` first, then `working`, then `done`; ties keep `started_at` order so the layout stays stable while a round runs.
- Done leads leave the tree and fold into one shelf under the live blocks: a row of chips (`buildermesh-001 ✓`), each a link to its lead page, headed `done (7)`. Live leads whose children are all done still draw as a tree (a lead's state is the lead's).
- A `done` event moves the lead to the shelf on the next redraw (the map already refetches on `done`); a lead with no live sibling still draws as a tree, so a single running build looks exactly as it does now.
- Routines keep their column (the phone stacks it last) and are the subject of 014.

Out: hiding done leads, a filter UI, any change to `/api/tree` or the daemon.

Judge: fable, because what reads as "live" in a glance is a spacing and weight call the spec can't pin.

## What you'll see
Open the map on the phone: the working round and its two engineers fill the first screen; the seven finished leads are a line of chips beneath; routines follow. On the desktop the live blocks start at the left and the page no longer scrolls sideways for a mesh of this size.

![phone mock](assets/012-mock-phone.png)

## Verification
`TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test -count=1 ./...` (the web tests that read `?check=1`); `go vet ./...`; `gofmt`. The self-check title at 390 and 1280 must read `ok` for the tree above with one live lead, and the first `.node.st-working` box must have a `getBoundingClientRect().top` inside the viewport on first paint. README's map paragraph gains the ordering rule.

## Risks and the alternative considered
Other ways to carry the same job: (a) sort only, keep done leads as full blocks: the phone still scrolls through seven done blocks to reach routines, so it halves the problem; (b) hide done leads behind a toggle: loses the click-through to a finished lead's spec and timeline, which the operator uses after a push; (c) a separate "history" page: a second place to look. The shelf keeps every lead reachable in one screen.

The case that users don't feel it: with one or two leads on the mesh the current order is fine. The screenshots show the mesh after one day of use already at eight leads, and the count only grows.

## Needs from the operator
