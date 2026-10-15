---
id: 003
title: Build, align and the optional method in the skills
kind: feature
size: M
judge: fable
status: done
lens: eng
proposed_by: da6488fd-38bd-4c4c-994c-a7de257d9424
proposed_on: 2026-10-15
built_by: 1150064f-6b68-443a-8ffd-a112dacac88c
branch: t3code/97b72def
host: k16
verified: verify ok t:593c188b
merged: ba3d6aa2f08ed0cc1fd8d33ffdce1b5f7ed44946
---

## Opportunity
Round two decisions 5, 7 and the Build and Skills sections in `~/repos/command-center/docs/superpowers/specs/2026-10-15-buildermesh-round2.md`. Dispatch and align are command-center Python; the lead and executor skills never name pstack or method, so the installed method goes unused.

## Proposal
Builds on 002 (embedded templates, spec commands).
In:
- `bmesh build <repo> <NNN>`: records `building` (thread, branch, host) on main first, renders the build brief (spec body, the row's done rule and verify commands, node path, standard closing), calls `lead start`; exit 3 and the `-2` path rule as today. `bmesh build --accepted <repo>`: accepted specs lowest ID first, a spec that names another's ID in its body after that one's `done`, at most three working leads, prints what waits. The Fable fallback to opus stays in the skill text, not the binary.
- `bmesh align <repo> [--check]`: `align_repo.py` in Go: the three marked AGENTS.md sections from the row and the embedded snippet (`docs/agents-snippet.md` moves into the binary and stays in the repo for reading), `docs/proposals/README.md`, `docs/proposals/assets/.gitkeep`, the row's `routines` kinds seeded from the embedded examples (`verify` inline and deterministic from the row's `verify` commands, joined with `&&`), existing kinds untouched; `--check` diffs, exit 1; a non-ship row exits 2.
- Skills: `buildermesh:lead` and `buildermesh:executor` gain, at fixed moments, "if the pstack plugin is installed, use `pstack:<skill>`; otherwise <one-line default>": poteto-mode as the working style; `architect` and `attack-the-premise` when the lead decides the shape; `writing-plans` (method plugin) when the spec is L; `tdd` as the executor's default; `figure-it-out` before any `bmesh ask`; `blast-radius` before landing. The README lists pstack and method as recommended, optional.
- The review loop, seen on 001 (four passes) and 002 (nine passes) on 2026-10-15: both leads queued a new review after every fix round although the skill says one pass. Make it mechanical and unmissable: `bmesh routine queue review` for a node that already has a review result recorded on any tree prints a warning naming that result and refuses without `--again`; the lead skill's §6 and the build brief say in one sentence that a review's findings are fixed and then landed, the second pass only after a rebase changed the code, and that a taste finding is noted in the final message, not re-reviewed.
- command-center's `dispatch` and `align-repo` skills shrink to one line each pointing at the commands (deleted in 005).

Out: the proposal round (002); removing command-center's tools (005).

Judge: fable, because the skills' method moments and their defaults are judgment.

## What you'll see
`bmesh build party-buzzer 007` starts a lead whose brief came from the binary; `bmesh align gametrainer` gives a repo its sections and kinds in one command; a lead with pstack installed opens in poteto-mode and one without works the same from the written defaults.

## Verification
Routine `verify`; tests for build's ordering and slot limit, align's idempotence (ported from `tests/test_align_repo.py`) and the check mode; `bmesh align --check` on party-buzzer prints no diff; one real `bmesh build` of a small spec.

## Risks and the alternative considered
Runtime detection of pstack in bmesh: rejected (decision 7), the thread knows its skills.

## Needs from Rick
None.
