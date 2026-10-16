---
id: 034
title: Windows keeps bmesh serve running, and comes back after an update
kind: feature
size: M
judge: opus
status: accepted
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
`bmesh serve` is the hub, the daemon and the map in one process, so even a single box needs it running all the time. On Linux and macOS, `init --service` writes a systemd unit or a launchd agent that keeps it alive. On Windows it prints `bmesh serve  # no service file for windows; keep this running` (`cmd/bmesh/cmd_init.go:315`). The process lives in an open terminal and dies at logout or reboot. Issue #1.3: the first Windows user wrote a VBS launcher in the Startup folder to get around it.

Self-update makes it worse. `reexec` on Windows returns nil and exits 0 so that "the service manager starts the new binary" (`cmd/bmesh/reexec_windows.go`). No service manager is there, so the first update stops the daemon for good. That's why 033 keeps Windows on notice only. Nothing checks that the repo still builds for Windows either: round two's decision 10 asked for a Windows CI build, and the verify routine has none. On 2026-10-16, `GOOS=windows go build ./...` passed, but `GOOS=windows go vet ./...` failed: `syscall.Kill` is undefined in `cmd/bmesh/cmd_lease_test.go:121` and `internal/instance/command_test.go:92`.

## Proposal
In:
- **`bmesh serve --restart`** runs `serve` as a child process and starts it again from the same binary path whenever it exits, whatever the exit code. It waits 5 seconds between starts, as `RestartSec=5` does, backs off to 60 seconds after five quick exits, and passes its arguments through. Ctrl-C or a stop signal ends the child, then the supervisor. The child gets `BMESH_SUPERVISED=1`. The flag works on every OS, so a Linux box without systemd can use it too. On Windows the child's output goes to `<state dir>/serve.log`, which keeps one previous file at 10 MB, since there's no journal there.
- **`--detach`** (Windows only, with `--restart`) starts the supervisor again with `CREATE_NO_WINDOW` and returns at once. A task started at logon then leaves no console window open.
- **`init --service` on Windows** prints the two commands to run, as it does for systemd, rather than running them:
  - `schtasks /Create /TN buildermesh /SC ONLOGON /RL LIMITED /F /TR "\"<exe>\" serve --restart --detach --config \"<config.toml>\""`;
  - `schtasks /Run /TN buildermesh` to start it now.

  Paths with spaces are quoted, and a path holding a `"` or `%` is refused, by the same rule `cmd /C` follows. The config path goes in `--config` because a scheduled task can't set environment variables. The log path is printed with them.
- **Self-update on Windows** works under the supervisor. The running binary is renamed to `bmesh.prev` (Windows allows renaming a running executable), the new one is moved in, and the daemon exits 0. The supervisor then starts the new binary. Without `BMESH_SUPERVISED`, a Windows daemon refuses `[update] repo` and 033's `[update] upstream` with one log line naming `serve --restart`. This replaces 033's blanket Windows refusal. Build after 033, or have the lead land the refusal rule with whichever spec builds second.
- **The Windows build is checked.** The verify routine (`.bmesh/routines/verify.toml`) and AGENTS.md's verify commands gain `GOOS=windows go vet ./...`, which type-checks the Windows-only files that a Linux `go vet` skips. The two Unix-only tests that fail it today get a build tag.
- **The docs.** README Quickstart and Updates say what Windows does. The note "only Linux and macOS are run" becomes "Windows is built and vetted on every change, and run by users".

Out: a real Windows service (it needs admin rights and the `svc` package, and a logon task is enough for one user); a real `selfupdate.Lock` on Windows (it stays a no-op, so the daemon and a manual `bmesh update` could race; noted in README); the quota panel's `sh -c`.

Judge: opus, because the restart loop, the detach and the swap each have to hold on an OS no lead can run.

## What you'll see
On Windows, `bmesh init --service` ends with two `schtasks` lines. Once they're run, `bmesh serve` starts at every logon with no window, and the map is up after a reboot. When the box takes an update, `serve.log` shows `update: <old8> -> <new8>` and the next start a few seconds later. On k16 and the Mac, nothing changes.

## Verification
The repo's verify, plus the new `GOOS=windows go vet ./...`. New tests:
- the supervisor restarts a fake child that exits 0 and one that exits 1, backs off after quick exits, passes its arguments and `BMESH_SUPERVISED` through, and stops the child on a signal (Unix, which runs the same loop);
- the `schtasks` lines for paths with spaces, and the refusal of `"` and `%` (a pure function, tested on any OS);
- an unsupervised Windows daemon refuses to update (build-tagged and type-checked by the Windows vet).

A live check on Windows: run `init --service` and the two commands, log out and in, open the map, push a no-op commit with `[update] upstream = true` set, and see `serve.log`.

## Risks and the alternative considered
No box in the fleet runs Windows. Every Windows-only line is checked only by vet and by the live check, so this is the least-tested path in the repo. The alternative is the Startup-folder VBS from #1, written by `init`. It hides the window, but it can't restart a crashed or updated daemon, so it fixes only half of the problem. A real Windows service needs admin rights and a service wrapper, which is too much for a single-user install.

## Needs from the operator
The live check needs a Windows box: ask the friend who filed #1, or skip it and land on vet and the tests alone.
