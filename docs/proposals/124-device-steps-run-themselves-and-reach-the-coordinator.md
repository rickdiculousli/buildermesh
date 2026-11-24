---
id: 124
title: A step that needs a device runs as a routine on whichever box has the device, by itself after landing or by the lead before it, and the watching coordinator sees the result
kind: feature
size: L
judge: opus
status: proposed
lens: eng
proposed_by: coordinator on ricks-macbook, from the operator's request during the Bluke build
proposed_on: 2026-11-23
---

## Opportunity
In the Bluke build (family `bluke-build-1`), two leads landed with a step that needed the phone:
- Bluke's home screen (spec 004) had "[phone] Pair, disconnect and reconnect one real host".
- Bluke's Bluetooth off/on fix (spec 006) had "[phone] Turn Bluetooth off and on once".

Both done lines said "not shown: Pixel run (operator)". The coordinator was watching both leads with `bmesh watch`, reported each landing, and said nothing about the step. Both times the operator had to ask for it ("run the checklist from 4", "do 6 checklist").

Each time the coordinator then ran the check from the Mac, which has the Pixel on USB. It took a few minutes with `adb`: build and install the landed commit, turn Bluetooth off and on, and read Bluke's debug `state` broadcast and a screenshot once a second. The 004 run found a bug, which became spec 006. The 006 run passed.

Steps are tracked once a spec lands:
- they're parsed from "Needs from the operator" (`internal/spec/steps.go:48-103`);
- ticks are stored on the hub (`internal/mesh/steps.go:12-29`);
- `mesh-status` lists open steps (`cmd/bmesh/cmd_status.go:256-260`).

`bmesh spec steps --repo Bluke` reads 5 of 5 open for spec 002, 2 of 2 each for 003 and 004, and 1 of 1 for 006, though two of those were done by hand. The gaps:

1. **The coordinator is never told.**
   - `land` prints `operator steps: N` to the lead only (`cmd_land.go:1114-1116`).
   - `watch` has no step events, and its `watch-done; next:` names only feedback (`drainNext`, `cmd_status.go:447-470`).
   - `watch` ends as soon as every watched node is terminal (`cmd_status.go:524-532`).
2. **Nothing knows which box has the phone.**
   - `[phone]` isn't a roster box, so `KeepBoxes` (`steps.go:237-247`) leaves it as text.
   - No config, registry or presence field names a device.
3. **A step's `check:` can't do a device check.** It runs through a `check-step` host job (spec 056, `internal/daemon/checkstep.go`), with these limits:
   - it is capped at 2 minutes (`internal/spec/stepcheck.go:16`), which a build plus an install exceeds;
   - it runs in the repo's own checkout, which on the Mac is the operator's working copy;
   - it waits in the one-at-a-time host inbox behind `level` and `prune`;
   - it runs only when someone types `--check`;
   - neither box's roster accept list includes `check-step` today.
4. **A lead can't run one before landing.** `CheckStep` reads the step from `main`, and only from a `done` spec (`stepcheck.go:51-65`).
5. **No skill says what happens to a step after a landing.**
   - The lead skill writes steps but never checks them (`skills/lead/SKILL.md:43-58`).
   - The work skill names `spec steps` only when the operator asks what's waiting (`references/in-flight.md:15`).

Routine kinds already have what a device check needs:
- each box pulls only the kinds it can run, and defers the rest to a box that can (`os`, `internal/daemon/routines.go:334-338`);
- a checkout at the job's commit;
- a `timeout`;
- results recorded on the hub;
- `routine queue --commit … -- args` and `routine wait`, which a lead already uses for the Mac test soak (spec 062, `.bmesh/routines/soak-darwin.toml`).

The missing piece is placement by device.

## Proposal
In:

**A kind can need a device.**
- A kind file gains `device = "<name>"`, beside `os`.
- A box has that device when its own config has `[devices] <name> = "<probe>"`, for example `phone = "adb get-state"`, and the probe exits 0 at the moment the runner takes the job.
- A box with no `[devices]` entry for the name doesn't pull the kind, as an `os` mismatch isn't pulled at scan time (`internal/daemon/routines.go:334-338`).
- A box whose probe fails when it takes the job hands the job back to the queue through `deferJob`. This is new at take time: today an `os` mismatch found then fails the job (`routines_job.go:305-310`). So an unplugged phone leaves the job queued, and it doesn't fail.
- `routine status` shows a queued job of a `device` kind as `waits for a box with <name>`.
- A box runs at most one job per device at a time, whatever the kind's `pool`.
- Presence and the roster don't change. The decision stays with the box that takes the job.

**A step's check can be a routine.**
- `check: routine <kind> [args]` marks a step checked by a routine job, for example `check: routine phone-check 006`.
- `Step` gains `Routine` and `Args`, parsed from that form.
- `Words()` doesn't change, so ticks made before this lands still match.
- A plain `check: <command>` keeps its meaning through `check-step`.

**After landing, the checks queue themselves.**
- When `done` succeeds, it queues each open routine step at the landed commit with `--go`. `done` runs from the daemon's land on the lead's box (`internal/daemon/land.go:403`).
- It publishes the queued job ids as a `step-jobs` event on the lead's node, so `watch`, including a `--since` replay, can follow them.
- A routine step is ticked when a reader finds an ok result for its kind and args at the landed commit's tree. The reader is `spec steps`, `mesh-status` or `watch`, and it writes the tick once with `TickSteps`.
  - The tick is safe to write twice.
  - It outlives the result's expiry (`[routines] keep_results`) and any later `--rerun`.
- A failed or timed-out job leaves the step open, and `spec steps` shows the job's reply text. A job that fails before judging records no result.
- `done` never waits on these jobs.

**Before landing, a lead runs the same routine.**
- The lead skill says: for a device step whose result a script can observe, write `check: routine …`, and when a box has the device, run `bmesh routine queue <kind> --commit <head> --go -- <args>` and `routine wait`. Write the result into Shown.
- "Not shown" is kept for what no script can see, such as how a haptic feels.
- The kind file is loaded at the job's commit (`routines_job.go:297`), but a kind that isn't yet on `main` is refused (`routines.go:888-893`). So a repo's device kind lands before any lead can use it, and a lead's commit runs on the device's box with the same trust a `soak-darwin` job has today.

**The coordinator is told.**
- When a watched node is `done` and its `step-jobs` event names jobs, `bmesh watch` follows their replies (`mesh.WaitReply`) for at most 15 minutes, within `--timeout`. It then prints any job still unsettled as `still queued` or `waits for a box with <name>`, and ends with `watch-done` as usual, never with "watch timed out". It prints:
  - `step <repo> NNN.<n> <text>: queued (<job>)` when the job is queued;
  - `step <repo> NNN.<n> ok|failed <reply text>` when it settles.
- For a step with no routine, it prints `step <repo> NNN.<n> <text>: waits for you`.
- `watch-done; next:` names `bmesh spec steps --repo <repo>` while any step is open.
- The work skill says the coordinator's landing report names each failed check and each step that waits for the operator.

Out:
- **Steps about feel or taste,** such as haptic strength. They stay the operator's.
- **Changes to `check-step` and host jobs.**
- **Two devices of one name** on one box.
- **Bluke's device kind, its phone-check script and its `check: routine` lines.** They're a Bluke spec after this lands.

Judge: opus, because it adds a placement key to routines and changes when `watch` ends, which every coordinator relies on.

## What you'll see
In the next Bluke build with a phone step, the coordinator's watch keeps going past the lead's `done`:
- `step Bluke 006.1 [phone] Turn Bluetooth off and on once…: queued (<job>)`;
- then, a couple of minutes later, `step Bluke 006.1 ok PASS Bluetooth off and on`.

`mesh-status` then carries no operator line for it. On the same landing, a haptics step prints `waits for you`, so the coordinator's landing report names it without being asked. With the phone unplugged, `routine status` shows that job as `waits for a box with phone`, and it runs once the phone is back.

## Verification
`go test ./...` gains tests through `bmesh routine`, `spec steps`, `done` and `watch`, against `meshtest`'s in-process NATS. The fixture kind has `device = "phone"`, and the fixture repo's done spec has a step with `check: routine <kind> 006`.

- **Fails today:** `watch` on a `done` whose spec has an open routine step prints that step's `queued` line, then its `ok` line, before `watch-done`.
- **Placement:**
  - a box whose config has `[devices] phone = "true"` runs the job;
  - a box with no `[devices]`, or with the probe `false`, defers it, and `routine status` reads `waits for a box with phone`;
  - a kind without `device` behaves as today.
- **One job per device:** two device jobs on one box with `pool = 2` run one after the other.
- **Ticks from results:**
  - after `done`, a job whose command is `true` makes `spec steps` read the step ticked;
  - a job whose command is `false` leaves it open, showing the failure's last line;
  - once ticked, the step stays ticked after a later failed `--rerun` replaces the result.
- **Watch with the device absent:** with no box declaring `phone`, `watch` prints `waits for a box with phone` after its cap, then `watch-done`, and exits 0.
- **At the landed commit:** the job's command `test "$(git rev-parse HEAD)" = <landed sha>` passes.
- **Parsing:**
  - `check: routine phone-check 006` yields `Routine` and `Args`;
  - a plain `check:` stays a command;
  - `Words()` is unchanged for both.

The manual's operator-steps and routine-kind sections cover `device`, `[devices]` and `check: routine`.

## Shown

## Risks and the alternative considered
- **Unattended jobs act on the operator's own phone.** They reinstall the app and turn Bluetooth off and on, possibly while the phone is in use.
  - Declaring the device in a box's config is the opt-in.
  - A device kind's command must leave the device as it found it.
  - The operator asked for this: they don't want to have to ask.
- **Device checks can be flaky,** because of radio timing. A failed job only leaves its step open with its reply text. It blocks no landing, and `routine queue --rerun` reruns it.
- **`watch` runs longer.** It now waits for the step jobs it queued, still bounded by `--timeout`. A coordinator that wanted the bare landing gets it from the node's `done` line, as before.
- **Alternative: extend `check-step` with devices, a commit checkout and a longer timeout.** It lost for two reasons: it would rebuild routing, waiting, checkouts and results that routines already have, and it would put minute-long jobs in the host inbox, which handles one job at a time.

## Upgrading
A box running an older release ignores `device`, so it would take a device kind's job and fail it there. Every box that runs routines for a repo with a device kind updates first. Kinds without `device`, and steps without `check: routine`, behave as today.

## Needs from the operator
- [ricks-macbook] Add `[devices] phone = "adb get-state"` to this box's bmesh config, so routine jobs that need the phone run here. check: grep -q '^phone *=' ~/.config/bmesh/config.toml
