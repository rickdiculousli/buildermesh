---
id: 053
title: bmesh init --service leaves a unit that differs from what it would write, and writes its own beside it
kind: refinement
size: S
judge: opus
status: building
lens: eng
proposed_by: dfdc9673-27b2-4f3f-9f17-95a5b436de66
proposed_on: 2026-10-17
from_feedback: buildermesh-build-5 (buildermesh-040)
built_by: db4a6067-e565-49e1-8e8c-3a4620937e38
host: k16
family: buildermesh-build-6
branch: bmesh/db4a6067-e565-49e1-8e8c-3a4620937e38
---

## Opportunity
`writeService` (`cmd/bmesh/cmd_init.go`) writes the unit with `os.WriteFile` whatever is there. k16's `bmesh.service` was tuned by hand (`MemoryMax=512M`, a mise PATH comment), so when 040 needed `init --service` to write the hub's unit, it would also have rewritten serve's, dropping both. `init --service` writes serve's unit and, with `[hub] role` set, the hub's in one pass, so there is no way to write one without the other. 040's lead kept k16's unit by hand (feedback 1534). Every box that gains a hub, or reruns `init --service` after an update, runs into the same thing.

## Proposal
In:
- **An existing unit that differs is left alone.** Before writing, `writeService` reads the file. The same bytes: nothing written, `unchanged <file>`. Missing: written as today. Different: left as it is, the new body written beside it as `<file>.new`, printing `kept <file> (it differs from what init writes); the new one is <file>.new: diff them, and move it over to take it`. `--force` replaces it, as `--force` already means start over.
- The enable commands are printed only for a unit that was written or unchanged, not for one kept, so nobody enables a unit they haven't looked at.
- README: the `init` paragraph and Several boxes say an existing unit that differs is kept, with `.new` beside it.

Out: merging a hand edit into the new body, and keys in config for unit settings such as MemoryMax.

Judge: opus, because init runs on every box and a wrong comparison either clobbers a tuned unit or never updates a stale one.

## What you'll see
On k16, `bmesh init --service` prints `kept ~/.config/systemd/user/bmesh.service (...)` and writes `bmesh.service.new`, and `bmesh-hub.service` is `unchanged`.

## Verification
The repo's verify, plus `cmd_init_test.go` cases with a sandboxed HOME and XDG dirs: no unit gets one written; an identical unit prints `unchanged`; a differing unit is kept with `.new` beside it and no enable line; `--force` replaces it.

## Risks and the alternative considered
A unit that is merely stale (written by an older bmesh, never edited) is kept too, and needs a move by hand. That is the safe side: a stale unit still runs, while a clobbered tuning is lost silently. Alternative: mark generated units with a header comment, and replace only those whose body still matches the marked one. That would also catch stale units, but it needs a stored hash and covers no case the `.new` file doesn't.

## Needs from the operator
