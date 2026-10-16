---
id: 031
title: A self-updated bmesh knows its own revision, so self-update stays on
kind: refinement
size: S
judge: opus
status: proposed
lens: eng
proposed_by: operator coordinator (Mac session, 2026-10-16)
proposed_on: 2026-10-16
---

## Opportunity
`selfupdate.Install` runs `go build -o <tmp> ./cmd/bmesh` in the detached checkout `<checkouts>/buildermesh/main` (a `git worktree`). Go writes no VCS stamp there, not even with `-buildvcs=true` (reproduced on the Mac with go 1.26.2 on 2026-10-16), so the new binary prints `bmesh unknown`. On its next start the daemon logs `update: this binary has no vcs revision; off` (`internal/daemon/update.go:63`).

The result: **each box's self-update turns itself off after its first update.** k16 printed `bmesh unknown` from 2026-10-15 and never picked up 029. On the Mac, `bmesh update` printed `88dfbe47 -> 0712e1e1` and then `bmesh version` printed `unknown`. Both boxes were fixed by hand with `go install` from `~/repos/buildermesh`.

## Proposal
- `Install` takes the commit it builds (the `head` both callers already read) and its commit time, and builds with `-ldflags "-X <module>/internal/selfupdate.stampRevision=<sha> -X <module>/internal/selfupdate.stampTime=<RFC 3339>"`.
- `FromBuildInfo` prefers `vcs.revision` and `vcs.time` when the build has them. Otherwise it uses the stamp. A binary with neither stays `unknown`, as today.
- Before the swap, `Install` runs `<tmp> version` and refuses the build (`update: built binary reports <x>, not <head8>; keeping this binary`) unless it names `head`. A stamp that went wrong then keeps the old, working binary instead of turning self-update off.

Out: changing where the build runs.

Judge: opus, because it is small, but the refusal must leave both callers (`bmesh update` and the daemon) in their current state.

## What you'll see
After `bmesh update`, `bmesh version` prints the new commit. k16's daemon logs `update: <old> -> <new>` on each push to `main` and keeps doing so, and `journalctl --user -u bmesh | grep 'no vcs revision'` stays empty.

## Verification
The repo's verify. New tests:
- `FromBuildInfo` with VCS settings, with only the stamp, and with neither;
- `Install` against a fake `go` that writes a binary reporting the wrong revision is refused, and the old binary is kept;
- a real build in a temporary `git worktree` (skipped without `go`) reports the head's revision.

A live check on k16: push a no-op commit to `main`, then see the daemon's `update:` line and the stamped version after its restart. Then push a second commit and see a second `update:` line.

## Risks and the alternative considered
The alternative is building in a full clone instead of a worktree, so Go stamps it itself. That costs a clone per box, and it rests on Go's VCS detection, which is what failed here. The `-X` stamp doesn't depend on where the build runs.

## Needs from the operator
