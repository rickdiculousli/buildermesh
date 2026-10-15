---
id: 014
title: Routines draw as one box per kind, with spent instances folded
kind: ux
size: M
judge: opus
status: proposed
lens: ux
proposed_by: f3d2851e-4460-4cd8-87b7-359407c1c774
proposed_on: 2026-10-15
---

## Opportunity
On this mesh the routine pool is six `review` groups (one per repo) holding eighteen instances, and the map draws every one as a box. Leads and engineers number eleven. On the phone the routines section is longer than the leads section (it starts 943 px down a 2224 px page), and most of it is `idle · 0 uses` and `done · 0 uses`:

![routines on the phone map](assets/014-map-phone-routines.png)

Eight of those are `fresh` instances (`routine.review.f<id8>`, README, Modes), each `done` after its one job and kept as a node, so every fresh run adds a permanent grey box. Pooled instances are two per repo per model, idle until a build asks.

The routines page groups the other way (one card per kind, instances of every repo in one list), repeats `routine.review.` on every row, and gives a done fresh instance the same row as a live pooled one:

![routines page](assets/014-routines-desktop.png)

So the two views disagree on what a routine is, and neither tells the operator the thing they ask of a pool: is anything running, is anything queued, and what can be retired.

## Proposal
In:
- The map draws one box per kind (`review`, `surface`, `verify`), second line `N queued · N busy · N idle`, plus `· N spent` when fresh instances have finished. The box links to the routines page; a `busy` count greater than zero gives it the `working` pulse. Instances leave the map.
- The routines page: one card per kind, instances grouped under their repo (a small `h4` per repo, the way leases already have one), the `routine.<kind>.` prefix dropped from rows (the full path stays in the row's `title`), and finished fresh instances folded under one row, `spent (8)`, that expands on tap. Order within a repo: `working`, `idle`, then spent.
- `/api/tree` keeps its shape; the map derives the counts. `/api/routines` adds `repo` to each instance if it is not already there (it is on the node).

Out: retiring or removing instances from the map (that is the daemon's and the CLI's job, and a `bmesh routine` change the engineer may propose); drawing message edges to instances (a job to `routine.review` lands on the kind box, which the flash code already handles for the group key).

Judge: opus, because the result is pinned (counts, grouping, prefix) and the work is reshaping two renderers and their tests.

## What you'll see
The phone map ends with three short boxes, one per kind, each saying what is queued and running. The routines page reads top down by kind then repo, rows are the instance's short name, and the eight spent review runs are one line until you open it.

## Verification
The verify commands; `tree_test.go` and `web_test.go` cases for the counts and the `spent` fold; the `?check=1` title at 390 must still read `ok`. README's map and routines paragraphs say how instances are shown.

## Risks and the alternative considered
Alternatives: (a) one box per kind per repo (today's grouping) with instances folded: still six boxes for one kind; (b) hide idle instances only: the spent ones are the bulk; (c) a routines-only toggle on the map: hides the queue depth the operator does want to see when a build waits on a review.

The case that users don't feel it: a single-repo mesh with one pooled instance. The routines page then shows one row, and the map one box with the same information it has now. The cost of this spec is paid by the multi-repo case the screenshots show.

## Needs from the operator
