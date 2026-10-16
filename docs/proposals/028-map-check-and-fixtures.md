---
id: 028
title: The map's browser check and test fixtures are in the repo
kind: maintenance
size: M
judge: opus
status: accepted
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
from_feedback: retrospective of 007-016 and 018 (buildermesh-010, -011, -012, -013, -014, -015, -016)
---

## Opportunity
Five UI specs (012 to 016) each asked for "a headless Chromium script" and each lead rebuilt one: `--dump-dom` hangs on the map's `/events` stream, so they wrote a CDP driver in Node, a seed of node states, a restart script, and copied each other's from `~/.cache/bmesh-scratch/`. 10 to 25 minutes per spec, plus `pkill -f` killing their own shell. 017 will pay it again. In Go tests, `tgit`-style git helpers exist three times and a safe log buffer twice, and fixtures trip on state invariants (`idle -> done`) and on a queued job counting toward depth only once its consumer exists.

## Proposal
In:
- `scripts/check.mjs <url> <w> <h> [png]`: drives Chromium over CDP, waits for the `?check=1` title, prints it, optionally screenshots, exits. Finds Chromium from `CHROME` or the Playwright cache.
- `scripts/seed.sh` (with 021's `stop`) seeds every state the map draws (each node state, a round, routines with done instances, a queued job) and serves on a port it prints.
- `serve` reads `internal/web/` from disk when `BMESH_WEB_DIR` is set.
- README Testing names all three; the spec template's Verification guide line points UI specs at them.
- `internal/meshtest`: `Git(t, dir, args...)`, `CommitFiles`, `SyncBuffer`, `ForceState(path, state)`, `QueueJob(kind, ...)`; the duplicates move onto them.

Out: a `surface` routine kind (later, once the script has settled).

Judge: opus, because it is tooling across web, scripts and tests with no taste call.

## What you'll see
A UI lead's check is `scripts/seed.sh` then `node scripts/check.mjs http://127.0.0.1:<port>/?check=1 390 844 shot.png`, minutes instead of most of an hour.

## Verification
Verify; running the check against the seed at both viewports prints a title with `trees=`; the moved helpers' tests still pass.

## Risks and the alternative considered
Chromium path differs per box; the script says which it found. The alternative, Playwright as a dependency, adds npm to a Go repo.

## Needs from the operator
None.
