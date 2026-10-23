---
id: 088
title: A daemon's self-update keeps the upgrade notes a box hasn't walked yet, so a second release never erases the first one's steps
kind: refinement
size: S
judge: opus
status: building
lens: eng
proposed_by: t3-e20b6274
proposed_on: 2026-10-23
built_by: a0971197-bc1c-4a76-8f9a-9a20e3355e08
host: k16
family: buildermesh-build-16
branch: bmesh/a0971197-bc1c-4a76-8f9a-9a20e3355e08
---

## Opportunity
On 2026-10-23 k16's daemon updated itself twice before anyone ran `bmesh update`: v0.4.0+4 -> v0.4.1 at 05:50 (serve logged `upgrade notes pending since v0.4.0+4: run bmesh update`), then v0.4.1 -> v0.4.2 at 06:24. After the second swap `<state dir>/installed` read `v0.4.2 f4cb2cf7`, `bmesh update --plan` printed `v0.4.2 -> v0.4.2: nothing to do`, and the pending line was gone, while `bmesh update --plan --from v0.4.0` still listed `v0.4.1#1` (restart the hub once). The hub stayed on v0.4.0+4 until it was restarted by hand.

The first wrong decision is in the daemon's update gate (`internal/daemon/upgrade.go`, `gate`): it plans `update --plan --from <own>`, the running binary, not from this box's installed record. v0.4.1 -> v0.4.2 has no steps, so `steps` is false and `swapped` writes the new binary as installed, over a record that still lagged at v0.4.0+4. `swapped` also overwrites `<state dir>/previous` with the running revision, the other point `pendingWalk` could have walked from.

## Proposal
In: the gate plans from the point the box last walked to (the installed record's revision, as `pendingWalk` reads it; else `previous`; else the running binary), not from the running binary. Then `steps` counts the notes still owed as well as the new ones, `swapped` writes `installed` only when that whole range has nothing to walk, and a hold for an ask or a confirm step covers a pending one too. `previous` is not overwritten while the record lags. Every other place that plans a daemon walk from `own` gets the same reading.

Out: the walk itself, the notes format, and `bmesh update`'s by-hand path, which already walks from the record.

Judge: opus, because the gate decides when a box's daemon swaps without a person, and a wrong range either drops steps or holds every update.

## What you'll see
A box whose daemon takes two releases in a row still prints `upgrade notes pending since v0.4.0+4: run bmesh update`, and `bmesh update` walks v0.4.1's hub restart then.

## Verification
The repo's verify commands. A failing-first test through the daemon's update path: a state dir whose `installed` record lags one release with steps, a swap to the next release with none; before the fix the record reads the new release and `Pending` is empty; after it the record is unchanged and `Pending` names the lagging release.

## Risks and the alternative considered
- A record that lags because a walk was refused stays lagging, so each later swap plans the longer range: that is the point, and the gate already holds on an ask.
- Alternative: leave the gate and have `swapped` skip writing `installed` while `Pending` is non-empty. Smaller, but the gate would still let a swap through that an owed ask or confirm step should hold.

## Upgrading
nothing

## Needs from the operator
