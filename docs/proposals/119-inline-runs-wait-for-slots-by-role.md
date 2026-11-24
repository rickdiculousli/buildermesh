---
id: 119
title: An inline run whose slot is busy waits its turn instead of failing, a landing goes first, and a best-effort run gives way and is told so
kind: time-cost
size: M
judge: opus
status: proposed
lens: eng
proposed_by: coordinator on ricks-macbook, from the operator's request during the Bluke build
proposed_on: 2026-11-23
---

## Opportunity
In the Bluke build (family `bluke-build-1`), four leads on k16 shared one Android emulator.

- **What arbitrates today is a lock inside a script.** Bluke's `verify` kind is `mode = "inline"` with no `slots`. Its emulator step, `tools/hid-rig/run.sh`, takes its own `flock`. A rig executor ran `rig.py` outside that lock, and another lead's verify failed on the clash. That lock also has two holes:
  - Its path depends on `TMPDIR` (`run.sh:17`), so sessions with different `TMPDIR`s take different locks.
  - Where there is no `flock`, such as a stock Mac, it is skipped (`run.sh:16`).
- **bmesh already has the right primitive, but an inline run can't wait on it.** A kind can declare `slots` (`internal/kind/kind.go:45`), with capacity in the repo's `.bmesh/config.toml` `[slots]`. Inline runs lease those slots (`cmd/bmesh/cmd_routine.go:788,835`). But an inline run that finds its slot busy is refused with exit 3 (`cmd/bmesh/cmd_lease.go:112-116`), so a lead must retry by hand or give up. `land` already waits for its own lease through `acquireWait` (`cmd_land.go:242-280`), which prints the holder and feeds the status line shown in `mesh-status`.
- **Waiting has no order.** Whoever polls first wins, so a landing verify can sit behind a lead's exploratory run. The operator's ask was: a request says whether it is urgent or best effort; best effort runs when the slot is idle; and an urgent arrival halts the best-effort run and tells its requester it was reprioritised.

Lease keys are `<slot>.<n>` and shared by every repo on the mesh (`internal/mesh/leases.go:78`), with capacity read from the caller's repo.

## Proposal
In:

**An inline run waits for its slot instead of failing.**
- `withLeases` (`cmd_lease.go:166-191`) waits for each slot through `acquireWait`, widened from one place to the slot's capacity.
- The exit-3 busy path for inline runs goes.
- While a run waits, its status line reads `waiting for <slot> (held by <holder>, <priority>); #N`, the same way `land`'s does today. `N` is the waiter's place among the waiters for that slot.

**Waiters are ordered by role, never by a flag.**
- A wait from inside `land` (its inline verify), or from the operator node, is **urgent**.
- Every other inline run is **best effort**.
- When a unit frees, the oldest urgent waiter takes it, then the oldest best-effort waiter.
- Each waiter records itself beside the lease, so every waiter sees the same order. The holder's lease value carries its priority.

**Preemption.**
- An urgent waiter that finds every unit held by best-effort runs marks one of them to yield: the one that started most recently.
- The holder's `withLeases` watches its lease. Once marked, it kills its child's process group, releases the lease, and waits again as urgent, so it can't be preempted twice.
- It prints `preempted by <holder> (land of <path>); waiting again at #N`. A lead sees that line in its run's output and on its `mesh-status` line.

**In Bluke:**
- Bluke's `verify` declares `slots = ["emulator"]`.
- Bluke's `.bmesh/config.toml` sets `[slots] emulator = 1`.
- `rig.py` refuses to run unless it was started under the lock.
- The lock path no longer depends on `TMPDIR`.

Bluke's slot and lock changes are its own change, listed under Needs from the operator.

Out:
- **Priority for daemon-queued jobs** (`mesh.Job`). Bluke's verify never goes through the daemon, and `routine queue` refuses inline kinds (`cmd_routine.go:335-336`).
- **Per-box leases.** Spec 037 left them out, and every lead here was on one box.
- **A queue ETA.** `mesh.Result` (`internal/mesh/results.go:20-31`) records no duration.
- **Keeping a resource warm between runs.** Bluke's `run.sh` already reuses a running emulator.
- **A merge queue.** Specs 026 and 080 chose one land lease over a queue.

Judge: opus, because it changes how `land` and every inline run take leases.

## What you'll see
A lead whose verify needs the emulator while another lead holds it no longer fails with exit 3. Its run waits, and its status reads `waiting for emulator (held by bluke-003, best effort); #2`. A `land` that needs the emulator while a mid-build run holds it takes the emulator within seconds. The mid-build run prints that it was preempted by that landing and waits again first in line.

## Verification
`go test ./...` gains tests through `bmesh routine run` and `land`, against `meshtest`'s in-process NATS, on a kind with `slots = ["x"]` and capacity 1:

- **Waiting.** A second `routine run` while the first holds `x` waits, then runs and records. Today it exits 3 at `cmd_lease.go:114`.
- **Order.** Best-effort waiters A and B, then an urgent land-path waiter C: C gets `x` next.
- **Preemption.**
  - A land-path waiter, with a best-effort run holding `x`, makes that run's child exit killed, its lease released, and its output carry the preempted line. The run then completes after the landing.
  - A run that waits again after a preemption isn't preempted a second time.
- **No flag sets priority.** A `routine run` from a lead node is best effort whatever its arguments.

The manual's paragraph on inline slots (`docs/manual.md:2163`) says an inline run waits and how it is ordered.

## Shown

## Risks and the alternative considered
- **Preemption throws away the killed run's work.** It only ever hits best-effort runs, and a run is preempted at most once.
- **A lease is as opt-in as a flock.** A process that skips `bmesh routine run` skips the lease. That is why Bluke's `rig.py` must refuse to run outside the lock.
- **Alternative: priority and preemption on daemon jobs.** It would not reach an inline verify, which is where the clash happened.

## Upgrading
nothing. An inline run that used to exit 3 on a busy slot now waits. A script that relied on that exit code stops seeing it.

## Needs from the operator
- **Product direction you asked for, built here.** A best-effort run can be killed by a landing. Lease values gain a priority, waiters record themselves beside the lease, and status lines gain a place in line.
- **A Bluke change after this lands:**
  - `verify.toml` gets `slots = ["emulator"]`;
  - `.bmesh/config.toml` gets `[slots] emulator = 1`;
  - `rig.py` refuses to run outside `run.sh`'s lock;
  - the lock path stops depending on `TMPDIR`.
