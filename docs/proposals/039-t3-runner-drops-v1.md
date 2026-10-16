---
id: 039
title: The t3 runner speaks only T3 0.1.0; the V1 path and t3_dispatch.py go
kind: maintenance
size: S
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
built_by: bfc829a7-d5e3-4168-97e9-4e3eea19463a
host: k16
family: buildermesh-build-5
branch: bmesh/bfc829a7-d5e3-4168-97e9-4e3eea19463a
merged: 949ec96dee39a74c8adca18b8a9d2624bce01f06
verified: review ok t:08ca6f30 m:claude-opus-5-5 c:9348c6fe; verify ok t:08ca6f30
---

## Opportunity
Spec 029 taught the `t3` runner T3 0.1.0's MCP tools (V2) and kept the old path (V1) as a bridge while the two boxes upgraded one at a time; it left the removal to 024. Both boxes now run 0.1.0, so the bridge carries nothing, and it is the one place bmesh's code still reaches for command-center:
- `internal/runner/t3.go` (364 lines) is mostly V1: `statusV1`, `sendV1`, `startV1`, the `uv run <Dispatch>` call and the protocol switch between the two paths.
- `[runner] t3_dispatch` defaults to `t3_dispatch.py` (`internal/config/config.go:258`), a script that exists only in command-center (`tools/t3_dispatch.py`); both boxes' `config.toml` set it to `~/repos/command-center/tools/t3_dispatch.py`.
- `[runner] t3_project` is V1 only; `t3_server = ""` means "V1 without asking"; `BMESH_T3_COMMAND` and `testdata/fake-t3.sh` exist for the V1 tests (`t3_test.go`, ten `fake-t3.sh` references across the tests).
- README Runners describes both paths, and Config lists the keys.

The operator asked for the bridge to go now, without waiting on the rest of command-center's retirement.

## Proposal
In:
- Delete the V1 path: `statusV1`, `sendV1`, `startV1`, `command()`, the prompt-file `Dir`, `ExecError` where only V1 raises it, and the protocol switch. Every call goes through V2. `protocol()` stays as a guard: a server reporting `orchestrationProtocolVersion` 1 is refused with `bmesh: T3 <version> at <server> speaks protocol 1; bmesh needs T3 0.1.0 or later`, any other non-2 as today.
- Config: `t3_dispatch` and `t3_project` leave `RunnerConfig`. A `config.toml` that still sets either loads, with one stderr line per key, `bmesh: [runner] t3_dispatch is ignored (T3 0.1.0 needs no script); delete it from <path>`. `t3_server = ""` becomes a load error naming the key, since there is no path that doesn't ask.
- Tests: `t3_test.go`'s V1 cases, `fake-t3.sh`, `BMESH_T3_COMMAND` and every test fixture that sets `t3_dispatch` (`cmd_hosts_test.go:34`, `cmd_lead_test.go`, `daemon/helpers_test.go`) go; the V2 tests stay. A new test: a protocol-1 environment is refused with the line above at start, status and send.
- README: Runners loses the V1 bullet and the "V1 only" notes; Config loses the two keys; the testing paragraph loses `BMESH_T3_COMMAND`.
- The two boxes' config: k16's `[runner] t3_dispatch` line and `~/.config/command-center/t3-session.json` (V1's session, unread since 029) are deleted by the lead; the Mac's are under Needs.

Out: anything else in command-center (041), and the NATS files (040).

Judge: opus, because it is deletion, but a missed V1 branch would strand a live lead's thread.

## What you'll see
`grep -rn -e t3_dispatch -e BMESH_T3_COMMAND --include='*.go' .` finds nothing, `internal/runner/t3.go` is the guard and V2 only, and `bmesh lead start` and `bmesh build` on either box start threads as before. An old config prints the "ignored" line once and runs.

## Verification
The repo's verify, with the new protocol-1 test. Live on k16: a one-line demo lead started, sent to and forgotten against the running T3 after the config line is deleted.

## Risks and the alternative considered
A lead started on the V1 path before 029 would lose its thread; none is live (every node built since 029 is V2), and the lead checks `bmesh mesh-status --json` for a `t3` node with a non-`mcp:` thread id before deleting.

The alternative is to leave it until command-center goes, as 024 did; that keeps a dead path and a default pointing at another repo for no box that needs it.

## Needs from the operator
On the Mac: delete the `t3_dispatch` line from `[runner]` in `~/.config/bmesh/config.toml`, and delete `~/.config/command-center/t3-session.json`.
