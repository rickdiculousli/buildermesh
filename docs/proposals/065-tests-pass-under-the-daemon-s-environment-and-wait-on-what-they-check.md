---
id: 065
title: The two tests the first Mac soak failed pass there: one stops reading the box's BMESH_CONFIG, one waits for the stdin it checks
kind: maintenance
size: M
judge: opus
status: accepted
lens: eng
proposed_by: buildermesh-fix-1
proposed_on: 2026-10-17
ruled: [root, failing-first, smallest]
fix_of: 062
from_feedback: buildermesh-fix-1 (buildermesh-062) #2037
---

## Opportunity
The first run of `soak-darwin` (062: `go test -count=10 ./...` on the Mac, job OxAIKlFctNjGkYyGqEbQfV) failed in two tests; every other package passed ten times (feedback 2037). Both faults are in the tests, not the code.

1. **`TestTmuxStartMakesWorktreeAndWindow` (`internal/runner/tmux_test.go`), 10 of 10 on the Mac.** The tmux runner passes the caller's `BMESH_CONFIG` to the window on purpose (`tmux.go:64`). A daemon whose service `bmesh init --service` wrote has `BMESH_CONFIG` in its environment (`cmd_init_test.go:147` checks the unit carries it), so a routine that daemon runs inherits it, and the test's expected `new-window` line, written for an empty environment, gains `-e BMESH_CONFIG=...`. Reproduced on k16 by the fix lead: `BMESH_CONFIG=$HOME/.config/bmesh/config.toml go test -count=1 -run TestTmuxStartMakesWorktreeAndWindow ./internal/runner` fails with exactly that diff; without the variable it passes. `root`: the test depends on the environment it runs in; the sibling test at `tmux_test.go:93` already pins it with `t.Setenv`.
2. **`TestLeadStartClaudeRunner` (`cmd/bmesh/cmd_lead_test.go:201`), 1 of 10 on the Mac.** `fake-claude.sh` appends argv to `$FAKE_LOG` and then copies stdin to `$FAKE_LOG.stdin`. `lead start` runs the fake in the background, so the test polls for the argv line (a hand-rolled `100 × 20 ms` loop, a fixed 2 s bound) and then reads `.stdin` once. Read between the fake's two writes, it gets `stdin ""`. Reproduced by the fix lead: with `sleep 0.3` put before the fake's `cat`, the test fails at once (`cmd_lead_test.go:251: stdin ""`). Without it, 40 of 40 passed on k16 under 12 busy loops: the window is narrow, and the Mac's soak hit it. `root`: the test waits on the wrong condition; its fixed 2 s bound is the fault 054 removed from `until`.

## Proposal
In:
- **Pin the environment.** `TestTmuxStartMakesWorktreeAndWindow` sets `t.Setenv("BMESH_CONFIG", "")`, as its sibling does. Then the same fault elsewhere: run the whole suite on k16 with `BMESH_CONFIG` and `BMESH_CONFIG_DIR` set to real paths (the variables a daemon's service gives its routines) and fix every test that fails only that way, the same way, in that test, or in the package's `TestMain` when several tests share it (`smallest`).
- **Wait on what is checked.** `TestLeadStartClaudeRunner` drops its loop and waits with `cmd/bmesh`'s `waitFor` (`cmd_sync_test.go:53`, 20 s) until `.stdin` reads `build it`, then checks argv. The same fault elsewhere: any test that reads the fake's `.stdin` or log after a background start having waited only on something written before it (`internal/runner/claude_test.go`, `internal/instance/model_test.go`, `cmd/bmesh/cmd_routine_run_test.go` are the readers); one that reads after a synchronous run is fine and is left alone.

Out:
- Changing `tmux.go`'s pass-through or `fake-claude.sh`'s write order: both are right, and the tests must hold for them.
- A lint for hand-rolled wait loops: after this, `TestLeadStartClaudeRunner`'s is the only loop of its form in the tests (`for i := 0; i < N && ...` with a sleep), and `soak-darwin` is the check that caught this one, so `structure` is met by the check already on `main`.

Judge: opus, because the sweep means telling a background read from a synchronous one in each test.

## What you'll see
A `soak-darwin` run at the landed commit passes ten times in every package, and the full suite passes on k16 with `BMESH_CONFIG` set.

## Verification
`failing-first`, each with its output in the closing commit:
- Before: `BMESH_CONFIG=$HOME/.config/bmesh/config.toml go test -count=1 ./...` (the repo's TMPDIR) fails in `TestTmuxStartMakesWorktreeAndWindow` and any other test the sweep finds; after, it passes.
- Before: with `sleep 0.3` inserted before `fake-claude.sh`'s `cat` (a scratch edit, not committed), `TestLeadStartClaudeRunner` fails `stdin ""`; after, it passes with the same edit, and so does every test the sweep changed.
- After landing: `bmesh routine queue soak-darwin --go` from the landed commit, and its `routine wait` passes in every package, ten runs each (the counts in the closing commit). The Mac must be awake; if it isn't, the lead says so rather than waiting.
- The repo's verify passes.

## Risks and the alternative considered
The 1-in-10 rate rests on one Mac run, and 0 of 40 on k16; the injected delay shows the cause without needing the rate. Alternative: clear `BMESH_CONFIG` in the `soak-darwin` command. That hides the fault from the soak but leaves every test that reads the box's environment to fail for any developer with the variable set.

## Upgrading
nothing: only tests changed.

## Needs from the operator
