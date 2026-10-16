---
id: 036
title: The version hint names which side is behind
kind: refinement
size: S
judge: opus
status: accepted
lens: eng
proposed_by: coordinator (k16 session 54c3af48, 2026-10-16)
proposed_on: 2026-10-16
from_feedback: buildermesh-build-2 (buildermesh-020)
---

## Opportunity
`versionHint` (`cmd/bmesh/cmd_version.go`) prints `bmesh: k16 runs 0712e1e1, this is 4843c782; run bmesh update` whenever this host's daemon runs another revision than the binary. On k16 on 2026-10-16 the binary was current and the daemon was the stale one, so every command of every lead asked for an update that had already been done and couldn't help: the fix was restarting the daemon. The `started` event carries only the revision (`internal/daemon/update.go`, `announce`), so the hint can't tell which side is older.

## Proposal
In:
- The daemon's `started` event also carries its commit time (the `Time` of `selfupdate.Version`, RFC 3339) in a new `rev_time` field of `mesh.Event` (omitempty).
- The hint compares that with the binary's own commit time:
  - the binary older: unchanged, `…; run bmesh update`;
  - the daemon older: `bmesh: <host>'s daemon runs <old8>, older than this <new8>; restart it (systemctl --user restart bmesh, or the launchd agent)`, and when this box's `[update] repo` is set, `; it updates itself within minutes` instead of the restart advice;
  - either time unknown (a daemon from before this spec): the current text, with `run bmesh update, or restart the daemon if it is the older` instead of `run bmesh update`.
- README's paragraph on the hint says so.

Out: the hub-wide fallback's wording beyond the same three cases; 033's prefix comparison, which builds on whichever lands first.

Judge: opus, because 033 changes the same hint and the lead reconciles the two at rebase.

## What you'll see
On a box whose daemon lags its binary, commands say to restart the daemon rather than to run `bmesh update`.

## Verification
The repo's verify. New tests: the three cases of the hint (binary older, daemon older with and without `[update] repo`, a `started` without `rev_time`), and `announce` publishing `rev_time`.

## Risks and the alternative considered
Commit times can tie or skew across rebases; a tie is treated as unknown. The alternative, asking git for ancestry, needs a checkout the CLI may not have.

## Needs from the operator
