---
id: 011
title: bmesh installs itself when main moves
kind: time-cost
size: M
judge: opus
status: proposed
lens: eng
proposed_by: fd21a373-851c-4e68-beb6-831cb4be0fa9
proposed_on: 2026-10-15
---

## Opportunity
AGENTS.md: "After a change lands on `main`, install it: `GOBIN=~/.local/bin go install ./cmd/bmesh` on both boxes and `systemctl --user restart bmesh` on k16." Six specs landed on 2026-10-15 (001 to 006), so six installs on k16 by leads, and six left for the Mac, which k16 cannot reach: 004's body ends with a "Left for the Mac" list and six spec lines say "on both boxes". Until the operator does them, the Mac's `bmesh` writes an older schema against the k16 daemon, the stall-and-collide zone this round names. The daemon already fetches every registered repo's `origin/main` once a minute to read kinds, and keeps a `main` checkout of each under `[runner] checkouts`; it knows the moment buildermesh's main moves and does nothing with it. The binary cannot even say which commit it is: no `version` command, no `ReadBuildInfo`.

## Proposal
In:
- `bmesh version`: `bmesh <vcs.revision8> <vcs.time> [modified]` from `debug.ReadBuildInfo` (`unknown` when built without VCS). Every daemon `started` event and the digest title carry the revision.
- `[update] repo = "<registry name>"` in config.toml (default `""`: off). When set, the daemon's kind rescan compares that repo's `origin/main` with its own revision; when main contains the revision and is ahead, it builds `./cmd/bmesh` from the `main` checkout to a temporary path, moves it over the running binary's path (the previous one kept beside as `bmesh.prev`), logs `update: <old8> -> <new8>`, finishes the jobs it is running (bounded by their kinds' timeouts) and re-execs itself (`syscall.Exec` on Unix; exit 0 for the service manager on Windows). A build that fails is logged once per commit and the daemon keeps running. A main that does not contain the running revision (a rollback) is left alone and logged.
- `bmesh update`: the same build and swap on demand, on any box, no daemon needed (`git -C <path> fetch` and build from `origin/main`); prints old and new revisions.
- Every other command compares its revision with the newest daemon `started` revision on the mesh (one KV read it already pays for) and prints one stderr line when they differ: `bmesh: k16 runs 1a2b3c4d, this is 9e8f7a6b; run bmesh update`. Never a refusal.
- AGENTS.md's install sentence becomes: k16 updates itself; on the Mac, `bmesh update`. README Quickstart and Config.
Out: tagged release binaries and tested Windows support (round two's decision 10 keeps them their own spec); updating the pstack or method plugins.

Judge: opus, because the sequence is pinned and the exec-and-swap must be right on both platforms.

## What you'll see
A lead lands on `main`; within two minutes `journalctl --user -u bmesh` shows `update: … -> …` and `bmesh version` on k16 prints the new commit. On the Mac the next `bmesh status` prints the one-line hint, and `bmesh update` makes it match. The operator never types the install again.

## Verification
The repo's verify. New tests against the fakes: `version` reads build info; the rescan with a fake `go` command swaps the binary and leaves `.prev`; a failing build leaves the binary and logs once; a CLI with a different revision prints the hint, the same revision prints nothing; `update` from a checkout whose main is at HEAD says `up to date`.

## Risks and the alternative considered
A main that builds but crashes at start: systemd restarts it, each start fails, and `bmesh.prev` is the way back by hand; the daemon pushes `bmesh update failed` through the notify channel after the swap when its next start never publishes `started` within two polls (the old binary records the swap in the stream before exec). Alternative: a cron `go install`; rejected, it does not know when main moved and cannot wait for running jobs. Alternative: the lead installs on both boxes; rejected, k16 cannot reach the Mac.

## Needs from the operator
None.
