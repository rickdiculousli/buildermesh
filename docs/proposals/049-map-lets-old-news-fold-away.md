---
id: 049
title: The map lets old alerts fold away instead of heading the page forever
kind: ux
size: M
judge: opus
status: accepted
lens: ux
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
On k16's map at 23:29 (`assets/049-map-old-news.png`), two things at the top of the page were hours old:
- **The digest fold.** The strip still showed `▸ digest 15h 56m ago: bmesh k16 9d33c60e: 0 working, 0 asking, 0 silent`, with three leads asking below it. Its body was a blocked round from the morning. The daemon writes a digest only when there is something to say, so the last one can stay up for a day while saying the opposite of the present.
- **The blocked round.** The second proposal round on buildermesh (`buildermesh-round-2`) has been `blocked` since 06:54 (its threads never started: the branches already existed). It is the first row of the needs-you band and the first block on the map, ahead of three leads that asked within the last ten minutes, because need order puts `blocked` first and nothing ever ages it.

The operator's words: "keeping old alerts and warnings up top in an annoying manner".

## Proposal
In:
- **Stale needs.** A band row, or a map block, whose state is older than `[serve] stale_after` (new key, default `"6h"`; `"0"` = never stale) is stale:
  - Stale rows leave the band's top rows and fold into one muted line at the band's foot: `▸ 1 older: buildermesh-round-2 blocked 16h`. The line opens in place to the full rows.
  - The band draws its live rows newest first within need order.
  - On the map a stale block sorts after `working`, ahead of `idle`, and draws at reduced opacity with its badge kept.
  - The strip counts it in its state as now.
  - Any new event on the node makes it fresh again.
- **Dismiss.** Every band row gets a `×`. A dismissed row goes into the older fold at once. The dismissal is kept in the browser's `localStorage`, keyed by the path and the state's `at`, so a new state on that node shows again. There is no mesh write: the alert is the mesh's, and the hiding is the viewer's.
- **The digest fold** shows only while the digest is younger than twice `[notify] digest_every` (`/api/strip` carries `digest_every`). An older one is dropped from the strip. The phone's notification is unchanged.
- **Held leads.** When the coordinator hold (048) has landed, `held` is never in the band, so nothing here applies to it.
- README Map and Config (`[serve] stale_after`). The seed gets a node blocked for a day and a stale digest.

Out:
- Retiring or forgetting the stuck round (`bmesh node forget` already does that, and the operator decides).
- Any change to pushes.

Judge: opus, because it's a contained rule over the existing sort and band code, plus one config key read by the web package.

## What you'll see
The band opens with the three leads that just asked. Under them is one grey line, `▸ 1 older: buildermesh-round-2 blocked 16h`. The round's box sits faded after the live leads, and no day-old digest line remains in the strip.

## Verification
The repo's verify, then the browser check (`scripts/seed.sh`, then `scripts/check.mjs`) at 1280×800 and 390×844. The seed's day-old blocked node must be:
- in the older fold, not the band's top rows, which the check counts with a new `older=` in the title;
- sorted after the working leads on the map.

The seed's stale digest must be absent from `strip=`. A dismissed row stays dismissed across a reload and comes back after a new event on that node (the seeder publishes one).

## Risks and the alternative considered
A real block left overnight goes quiet on the map. The fold still names it with its age, and the push already went out when it blocked. Alternative: auto-retire blocked nodes after a day. Rejected: that destroys the record, and a stuck round is the operator's to forget.

## Needs from the operator
`stale_after`'s default (6 hours) is a taste call. Change it in `[serve]` of `config.toml` if you want it shorter.
