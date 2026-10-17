---
id: 048
title: A coordinator holds a lead at a gate, and the lead waits until it's released
kind: feature
size: L
judge: opus
status: building
lens: eng
scope_grew: [046]
proposed_by: operator
proposed_on: 2026-10-16
built_by: f0ae77ee-f879-462f-be44-a79154ce057e
host: k16
family: buildermesh-build-7
branch: bmesh/f0ae77ee-f879-462f-be44-a79154ce057e
---

## Opportunity
A coordinator (the `build` skill's session, or the operator) has no way to say "stop here until I say so" except a free-text `bmesh --node operator send` ("hold"). The lead can only answer that by going `asking`. On k16 right now two leads sit like this: the lead adding release tags (046) and the lead making verify changes go through the registry row (043) both read `asking: ready to land ... Holding per your message; send 'land ok' when the NATS hub move is done.` (screenshot `assets/049-map-old-news.png`). So:
- each lead pushed an `ask` to the phone and holds a row in the needs-you band, as if the operator owed it an answer, when the coordinator is the one waiting on something else;
- nothing enforces it: `bmesh land` would land the moment the lead misread the message;
- every coordinator words its own hold and its own release phrase (`land ok`, `moved`, `retry`), and the lead has to parse them.

Coordinators want this for several reasons: landings in order (wait for a hub move, or let another lead land first), the operator wants to read a diff before it lands, a pause while quota is low, or a pause to redirect a lead before it builds the wrong thing.

## Proposal
In:
- **`bmesh hold <path>|--family F [--gate land|now] --text "<why>"`** writes `bm-holds` (new KV) `<path>` to `{gate, text, by, at}` and sends the lead a message of kind `hold` (`hold from operator (land): <why>`), which wakes an idle lead as any message does. `--gate land` is the default. `--family` holds every live lead in the family.
- **The gates.**
  - `land`: `bmesh land` reads `bm-holds` before it takes the lease, and stops with `held before land by operator <age> ago: <why>`. Nothing is pushed.
  - `now`: the lead's next `bmesh inbox` prints the hold first, as its own line. The lead skill says: stop at that checkpoint and do nothing but wait.

  Executors are never held. Their lead is.
- **State.** At the hold, the lead runs `bmesh held` (the `land` refusal prints the command, and the lead skill says to run it). That publishes a new `held` state with the gate and why, and the lead ends its turn. The release wakes it, and its next `started` or `progress` makes it `working` again.
  - The daemon never calls a `held` node silent.
  - `held` is not in the needs-you band or in `[notify] push`: the coordinator put it there.
  - The map draws a `held` badge in a calm colour, and the strip counts it (`2 held`).
  - `mesh-status` shows the state with its age. `watch` prints the `held` event, so the coordinator sees the lead reach its gate.
- **`bmesh unhold <path>|--family F [--text "<words>"]`** deletes the key and sends a message of kind `release` (`released by operator: <words>`), which wakes the lead. An unhold of a lead with no hold prints `<path>: not held` and exits 0.
- Scope grew (046): the release-tags spec landed `bmesh release <repo>` for cutting a version tag, so the command that lifts a hold is `bmesh unhold`; its message kind (`release`), its words and everything else stay as written.
- **`bmesh build --hold land|now`** dispatches with the hold already written, before the lead starts. Use it for "build these, land none until I say".
- **Skills.**
  - `lead` gains a short section on holds: a hold message at any checkpoint, the `land` refusal, `bmesh held`, wait, and on release carry on. A hold is never answered with `ask`.
  - `build` uses `hold` and `unhold` instead of a free-text hold, and relays a lead reaching its gate as one line.
  - The operator skills learn "hold X", "hold land on X", "release X".
- README: Builds, Landing, Names (`bm-holds`), the state list, Map, and the new commands.

Out:
- Holds on rounds or routines.
- Timed holds that release themselves (release is an act, never a timer).
- Holding at arbitrary named steps beyond `land` and `now`.
- Who may hold: any sender may hold, as any sender may `send` today. The Hosts layer's signing doesn't change.

Judge: opus, because the hold crosses land, the daemon's silence rule, the map and two skills, and the lead must wait without being called silent.

## What you'll see
The coordinator runs `bmesh hold --family buildermesh-build-7 --text "hub move first"`. Leads that reach landing run `bmesh land`, read `held before land by operator 3m ago: hub move first`, run `bmesh held` and stop. Nothing reaches the phone, the needs-you band stays empty, and the map shows two `held` badges. After the move, `bmesh unhold --family buildermesh-build-7 --text "hub moved"` wakes both, and they land one after the other through the land lease.

## Verification
The repo's verify. New tests, with the runner fakes in `testdata/`:
- `hold` then `land` refuses and pushes nothing, and after `unhold` it lands;
- a `now` hold prints first in `inbox`;
- `--family` holds and unholds every live lead of the family and no one else;
- `build --hold land` writes the key before the lead's first event;
- the daemon's liveness pass never marks a `held` node silent and doesn't push it;
- `unhold` of an unheld node exits 0;
- the map's seed (`scripts/seed.sh`) gains a `held` lead, and `scripts/check.mjs` sees its badge and no band row.

## Risks and the alternative considered
A forgotten hold parks a lead forever. `mesh-status` and the map show `held` with its age, and the hourly digest names holds older than a day. Alternative: keep holds as messages and teach leads a fixed phrase. Rejected: `land` can't enforce a phrase, and the state would still be `asking`.

## Needs from the operator
