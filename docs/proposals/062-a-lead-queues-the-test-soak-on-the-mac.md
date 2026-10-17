---
id: 062
title: A lead on k16 queues bmesh's ten-run test soak on the Mac through a routine, instead of asking the operator to run it by hand
kind: time-cost
size: XS
judge: sonnet
status: building
lens: eng
proposed_by: buildermesh-fix-1
proposed_on: 2026-10-17
ruled: [root, failing-first, smallest, reversible]
fix_of: 054
from_feedback: buildermesh-build-9 (buildermesh-054) #1986
built_by: 2efa43a1-176d-44ff-a18f-159024826d0d
host: k16
family: buildermesh-fix-1
---

## Opportunity
054's Verification asked for the repo's tests run ten times on each box (`go test -count=10 ./...`). On k16 the lead ran it itself. For the Mac it had to ask the operator to run it by hand, twice, because nothing on `main` runs bmesh's tests on a Mac. `verify` is `inline` (it runs in the requester's own worktree, so on k16), and `routine queue` refuses a kind `main` doesn't have (`bmesh: <kind> is not on main: land it first`, `cmd/bmesh/cmd_routine.go:441`), so a branch can't add one for its own use (feedback 1986).

`root`: routines already carry work to the Mac (036: a kind with `os = "darwin"` is taken only by a darwin box, and the Mac runs routines). The first wrong decision is the repo's: it ships a darwin build (`GOOS=windows go vet`, the Keychain store, launchd) but defines no kind that runs its tests on darwin. The refusal on `main` is right and stays.

## Proposal
In:
- **`.bmesh/routines/soak-darwin.toml`**, one new file: `mode = "pooled"`, `os = "darwin"`, `deterministic = false` (each queue is a fresh sample, since a soak looks for flakes), a `timeout` above the run's own (`100m`), and `command = "mkdir -p \"$HOME/.cache/bmesh-scratch/tmp\" && TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test -count=10 -timeout 90m ./..."`. A header comment says what it is for: a spec whose Verification asks for repeated runs on a Mac; push the branch, then `bmesh routine queue soak-darwin`.
- **README, Build and test**: one line beside the `go test` line naming the kind and how to queue it.

Out:
- A `soak-linux` (or any-OS) twin. Leads of this repo run on k16 (its row's `host`), where the lead runs `go test -count=10` itself. An unset `os` would let either box take the job, which is no use for a per-OS count.
- Any change to `verify`, the registry row's `routines` (that list is what every build runs; a soak is queued on demand) or `routine queue`'s refusal.
- A sentence in a skill or `AGENTS.md`: a lead finds the kind where it finds `verify` and `review`, in `.bmesh/routines/`.

Judge: sonnet, because the file's shape is fixed here and its run shows at once whether it works.

## What you'll see
From a pushed branch in a buildermesh worktree on k16: `bmesh routine queue soak-darwin --go` prints `queued <id> (runs on darwin)`, and `bmesh routine wait <id>` prints the Mac's `go test` output, `ok` per package.

## Verification
`failing-first`, through the real command, with the output in the closing commit:
- Before: today a buildermesh worktree on k16 answers `bmesh routine queue soak-darwin` with `bmesh: no kind soak-darwin in <worktree>` (exit 3, run by the fix lead on 2026-10-17); on the fix's branch, with the kind committed and pushed but not landed, it exits 3 with `bmesh: soak-darwin is not on main: land it first`, which is where 054's lead stood.
- After landing: from a pushed commit on k16, `bmesh routine queue soak-darwin --go` prints `queued <id> (runs on darwin)` and `bmesh routine wait <id>` exits 0 with the Mac's test summary; `bmesh routine status` shows the instance's `HOST` as the Mac. Its pass count (ten runs of every package) goes in the closing commit.
- The repo's verify passes.

## Risks and the alternative considered
The Mac's launchd daemon may not have `go` on its `PATH` (the Mac's Go comes from mise). Then the job answers `go: command not found`: that is the Mac's service config, outside `main` (`reversible`), so the lead lands the kind anyway, says so in its report and names the step under Needs from the operator rather than writing a box's path into the kind. A soak holds the Mac's routine slot for its run (about as long as ten verifies); it is queued only on demand.

Alternative: a non-deterministic `verify-darwin` running `-count=1`. A spec asking for one Mac run can pass `-count` nowhere then; the soak is what 054 needed, and one run on the Mac is a soak's first pass.

## Upgrading
nothing: a kind file in this repo's `.bmesh/routines/`; no install's config or binary changes.

## Needs from the operator
