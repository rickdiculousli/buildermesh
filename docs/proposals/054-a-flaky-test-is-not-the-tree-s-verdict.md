---
id: 054
title: A test that fails by timing doesn't become the tree's recorded verify fail
kind: refinement
size: M
judge: opus
status: proposed
lens: eng
proposed_by: dfdc9673-27b2-4f3f-9f17-95a5b436de66
proposed_on: 2026-10-17
from_feedback: buildermesh-build-5 (buildermesh-046)
---

## Opportunity
In 046's landing, `TestClaudeSendKeepsOrderBehindQueue` (`internal/runner/claude_test.go:213`) timed out ("waiting for Status to start the next queued send") on a tree that doesn't touch `internal/runner`. It then passed 5 of 5 on its own and on `routine run verify --rerun` (feedback 1621). Its waits go through `until`, which gives every condition a fixed 5 s. The full verify runs `cmd/bmesh` (68 s) and `internal/daemon` (99 s) in parallel with it, so a fake turn plus a Status poll can miss that bound under load. 038's lead hit the same kind of race in `TestLandWaitsForHeldLease`, about one run in four (feedback 1505, fixed in 0f743d6f).

`verify` is deterministic, so its result is recorded under the tree hash. A flake becomes that tree's `fail`, and every later run of the same tree is answered from the record. The lead gets past it only by knowing `--rerun`, and then a real failure and a timing one look the same in the ledger.

## Proposal
In:
- **The test.** Make `TestClaudeSendKeepsOrderBehindQueue` wait on events, not on time under load: `until`'s bound comes from the test's deadline (`t.Deadline`, less a margin), not a fixed 5 s. Do the same for every `until` in `internal/runner` and the land tests that poll a lease.
- **Find the rest.** Run the verify ten times on k16 and the Mac (`go test -count=10 ./...`) and list every test that fails at least once with its rate. Each one is fixed here if the cause is a wall-clock bound, or named in this spec's closing commit if not.
- **Out, until the measurement says otherwise:** retrying a failed verify automatically inside the routine. That would hide a real race in bmesh's own code, which is the kind of bug the mesh runs on.

Judge: opus, because telling a timing bound from a real race needs reading each failure.

## What you'll see
Ten runs of the verify on each box with no failure, and no lead's done text mentioning `--rerun`.

## Verification
The repo's verify, run ten times on each box (`go test -count=10 ./...` with the repo's TMPDIR), with the counts in the closing commit.

## Risks and the alternative considered
Ten runs may not catch a one-in-fifty flake. The measurement is cheap to repeat, and a later flake comes back as feedback. Alternative: let `verify` record a fail only after a second failing run. That halves the cost of a flake, but it records a real intermittent bug as `ok`, and land trusts that record.

## Needs from the operator
