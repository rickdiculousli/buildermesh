---
id: 056
title: A landed spec's steps for the operator stay on mesh-status until they're ticked off
kind: feature
size: M
judge: opus
status: building
lens: eng
proposed_by: dfdc9673-27b2-4f3f-9f17-95a5b436de66
proposed_on: 2026-10-17
from_feedback: buildermesh-build-7 (buildermesh-041)
built_by: 2128f0e7-4526-4ddc-86c7-7043a794d9b0
host: k16
family: buildermesh-build-9
branch: bmesh/2128f0e7-4526-4ddc-86c7-7043a794d9b0
---

## Opportunity
A spec's "Needs from the operator" is read once, in the lead's done text or the coordinator's report, and then nothing tracks it. 022 landed `done`, but its k16 step (set `[quota] remind_topic`, then disable the reset-reminder timer) was never taken. A day later, 041 found the old timer was still the only reminder and fixed it by hand (feedback 1779). This one batch left more steps than that. 039 left two config lines on the Mac, 040 left the Mac's half of the hub move, and 041 left seven Mac steps. Their only record is a chat scroll and the specs' closing commits. `bmesh done` checks the spec's status, merge commit and results, but never whether what it asked of the operator happened.

## Proposal
In:
- **The steps are data.** A spec's "Needs from the operator" section is read as a list: each `-` or numbered line is a step, and a line may start with a box in brackets (`[ricks-macbook]`). When the lead lands, `bmesh land` copies the list into a new `operator_steps` frontmatter field, `[{n, box, text, done: ""}]`. This happens only for a spec whose section isn't empty.
- **They show until ticked.** `bmesh mesh-status` ends with one line per spec with open steps: `operator: 041 retire command-center: 5 of 7 open (bmesh spec steps 041)`. The map's needs-you band shows the same. `bmesh spec steps <NNN>` lists them, and `bmesh spec steps <NNN> --tick 2,3 [--note "<words>"]` records `done: <time> <box>` on `main` as `Steps: 041 2, 3 done`.
- **A step that's checkable says how.** A step may end with `check: <command>`. `bmesh spec steps --check` runs it on the step's box (locally, else as a job to that box), and a zero exit ticks it.
- **The coordinator's report points at it.** The `build` skill's closing line names the specs with open steps and the command, so the list lives on the mesh rather than in the chat.

As built: the operator line names the repo, `operator: <repo> 041 <title>: 5 of 7 open (bmesh spec steps 041 --repo <repo>)`, since a mesh spans repos, and `bmesh spec steps` with no id prints those lines alone (the `build` and `mesh-status` skills read it). A prose paragraph is a step of its own, and one ending in `:` that leads into a list (`On the Mac:`) goes in front of each of its steps. A step's check on another box is the host op `check-step`, which names the spec and step, never a command: the box reads the command from its own checkout's `main`, and runs it only when its roster row accepts `check-step`.

Out: blocking anything on open steps (`done` and `release` don't wait on them); reminders pushed on a schedule.

Judge: opus, because it reads free text into a list, and a misparsed step is worse than none.

## What you'll see
After this batch, `mesh-status` ends with `operator: 041 retire command-center: 7 of 7 open`, and ticking a step on the Mac shrinks the count everywhere.

## Verification
The repo's verify, plus `internal/spec` cases parsing a Needs section (bullets, numbers, box prefixes, `check:`), and `cmd_spec_test.go` / `cmd_status_test.go` on `meshtest.Start` for land copying the steps, `--tick` landing on `main`, `--check` ticking on exit 0, and the mesh-status line.

## Risks and the alternative considered
A Needs section written as prose, not as a list, becomes one long step. The template's guide line says to write it as a list. Alternative: a separate per-repo ledger file of operator steps. That is a second record next to the spec, which is the one record by design.

## Upgrading
- `bmesh spec steps --check` runs a step that names another box as a new host op, `check-step`, which a box runs only when its roster row accepts it. Should each box run the `check:` commands of the specs on its repos' `main`? Choices: add `check-step` to a box's accept list on the admin box (`bmesh hosts set <box> --accept <its current list>,check-step`), or leave it out (default: left out, and that box's checks are run at the box itself with `bmesh spec steps <NNN> --check`).
- Update every box's bmesh: a box on the previous release can't answer a `check-step` job, and its `mesh-status` and map don't show the steps.

## Needs from the operator
