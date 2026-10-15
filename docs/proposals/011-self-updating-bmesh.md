---
id: 011
title: bmesh installs itself when main moves
kind: time-cost
size: M
judge: opus
status: building
lens: eng
proposed_by: fd21a373-851c-4e68-beb6-831cb4be0fa9
proposed_on: 2026-10-15
built_by: 0fb120c6-9c16-4e06-ad17-e82204b16d55
host: k16
branch: t3code/a05c4d42
---

## Opportunity
AGENTS.md: "After a change lands on `main`, install it: `GOBIN=~/.local/bin go install ./cmd/bmesh` on both boxes and `systemctl --user restart bmesh` on k16." Six specs landed on 2026-10-15 (001 to 006), so six installs on k16 by leads, and six left for the Mac, which k16 cannot reach: 004's body ends with a "Left for the Mac" list and five lines across the landed specs say "both boxes". Until the operator does them, the Mac's `bmesh` writes an older schema against the k16 daemon, the stall-and-collide zone this round names. The daemon already fetches every registered repo's `origin/main` once a minute to read kinds, and keeps a `main` checkout of each under `[runner] checkouts`; it knows the moment buildermesh's main moves and does nothing with it. The binary cannot even say which commit it is: no `version` command, no `ReadBuildInfo`, and the daemon publishes no event of its own on start (its subjects today are `wake`, `silent`, `digest`, `push`, `push-failed`).

## Proposal
In:
- `bmesh version`: `bmesh <vcs.revision8> <vcs.time> [modified]` from `debug.ReadBuildInfo` (`unknown` when built without VCS).
- A new daemon event `bm.daemon.<host>.started` on every `serve` start, `text` the revision (and the digest title carries it).
- `[update] repo = "<registry name>"` in config.toml (default `""`: off). When set, the daemon's kind rescan compares that repo's `origin/main` with its own revision; when main contains the revision and is ahead, it builds `./cmd/bmesh` from the `main` checkout it keeps under `[runner] checkouts` to a temporary path, moves it over the running binary's path (the previous one kept beside as `bmesh.prev`), publishes `bm.daemon.<host>.update` with `<old8> -> <new8>` and logs it, then drains: the kind feeds stop taking jobs, every job in flight runs to its reply (bounded by its kind's timeout; today a SIGTERM cancels a running job and leaves it to redeliver only after AckWait, 21 m for `review`, which an update timed with a lead's landing would do to the other leads' reviews), then the shutdown path (pools stopped, the embedded hub stopped) and it re-execs itself (`syscall.Exec` on Unix; exit 0 for the service manager on Windows). The embedded hub comes back with the new process and clients reconnect as nats.go does on any hub restart, the same as `systemctl --user restart bmesh` today. The build runs in a transient scope of its own (`systemd-run --user --scope`) when systemd is present, else inline, because the unit's `MemoryMax=512M` is shared with the daemon and `go build ./cmd/bmesh` peaks at 313 MB RSS on k16 (measured 2026-10-15, warm cache). A build that fails is logged once per commit and the daemon keeps running. A main that does not contain the running revision (a rollback) is left alone and logged.
- `bmesh update`: the same build and swap on demand, on any box, no daemon needed: it fetches `origin main` into a bmesh-owned checkout (`<[runner] checkouts>/<repo>/main` when checkouts are configured, else a clone under `<user cache dir>/bmesh/update/<repo>`), never the operator's checkout; prints old and new revisions, or `up to date`.
- Every command that connects to the hub (`mesh-status`, `inbox`, `send`, `routine`, `choose-ideas`, `build`, `ideate`, the events, and the rest that call `connect`) reads the newest `bm.daemon.*.started` message once and prints one stderr line when any host's revision differs from its own: `bmesh: k16 runs 1a2b3c4d, this is 9e8f7a6b; run bmesh update`. Never a refusal. `help`, `version`, `init`, `repos`, `spec list|next-id|set|template` and `scaffold` connect to nothing and get no hint.
- AGENTS.md's install sentence becomes: k16 updates itself; on the Mac, `bmesh update`. README Quickstart, Config and Names (the two new daemon subjects).
Out: tagged release binaries and tested Windows support (round two's decision 10 keeps them their own spec); updating the pstack or method plugins.

Judge: opus, because the sequence is pinned and the exec-and-swap must be right on both platforms.

## What you'll see
A lead lands on `main`; within two minutes `journalctl --user -u bmesh` shows `update: … -> …` and `bmesh version` on k16 prints the new commit. On the Mac the next `bmesh mesh-status` prints the one-line hint, and `bmesh update` makes it match. The operator never types the install again.

## Verification
The repo's verify. New tests against the fakes: `version` reads build info; the rescan with a fake `go` command swaps the binary, leaves `.prev` and publishes `update`; a failing build leaves the binary and logs once; a CLI with a different revision prints the hint, the same revision prints nothing; `update` from a checkout whose main is at HEAD says `up to date`; a job running when an update is seen still gets its reply before the exec, and no new job is taken meanwhile.

## Risks and the alternative considered
A main that builds but crashes at start: systemd restarts it, each start fails, and `bmesh.prev` is the way back by hand; the next successful start, which only comes once someone restores `bmesh.prev`, sees the `update` message with no `started` after it and pushes `bmesh update failed on <host>` through the notify channel; until then the operator's signal is the digest going quiet. Alternative: a cron `go install`; rejected, it does not know when main moved and cannot wait for running jobs. Alternative: the lead installs on both boxes; rejected, k16 cannot reach the Mac.

## Needs from the operator
None.
