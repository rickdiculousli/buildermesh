---
id: 128
title: Every command bmesh runs reports a checkup every 2 minutes (5 for a run bounded at an hour or more), and an agent's long command runs under bmesh run, which waits on it, bounds it and ends it once it stops moving
kind: time-cost
size: M
judge: opus
status: done
lens: eng
proposed_by: 6df9de23-3ba1-4c33-8273-21cdd370289e
proposed_on: 2026-11-24
---

## Opportunity
Every layer that runs a command has a limit at the far end, and nothing looks at the command before that limit. Nobody can tell a slow run from a stuck one, and an agent that wants to wait for its own command has only the harness's notice or a loop it writes itself.

**The limits today:**
- **Routine command kinds** are killed at their `timeout`: 20m by default, 40m for this repo's `review`, 100m for `soak-darwin`. `[setup]` is killed at 10m.
  - Between a job's `job <id> started` and its `job <id> ok|fail <ms>`, the daemon publishes nothing.
  - `routine run` prints its output only when it ends. 121 left live output for these runs out because they had no reader.
- **An agent's own commands** run in Claude Code's Bash tool.
  - A foreground call is held up to its timeout (2m by default, 10m at most), then moved to the background. That happened 31 times in k16's transcripts.
  - Nothing looks at a background task until it exits or the harness kills it at its limit, 30m by default.
- **A node** is marked silent after `[liveness] grace` (20m). Only a lead or engineer is woken for that.

**Measured on k16.** The sample is the transcripts of every lead, executor and round on this box: 90 directories, 11,158 Bash calls.
- **No command hang was found.**
  - None of the 294 routine results in the store (30 days) hit its timeout.
  - The only agent commands the harness killed (twice, at 40m 3s each) were waits written by hand. Executor `opengym-rick-038.browser-check` had already bounded its browser check itself (`timeout 1200 sh cache/038/run.sh`). It then waited on it with `until ! pgrep -f checks.mjs; do sleep 3; done`. That loop's own command line matches `checks.mjs`, so it could never end.
  - Each such wait held the executor 10 minutes before the harness moved it to the background. There it ran until the harness killed it.
- **Waiting by hand is the measured cost.** The agents ran 247 `sleep N` calls and `until … sleep` loops: 128 minutes of foreground waiting.
  - Leads ran 161 of them (62 min), though the lead skill says "no `sleep`, no polling".
  - Executors ran 70 (61 min). Their skill says nothing about waiting.
- **Background tasks.** There were 189. The 98 whose end was seen ran a median of 158 s (p90 557 s).
- **Silence isn't the signal.** The 7-day timeline holds 16 silent stretches: 5.1 h, against 30.9 h working. The two longest were waits, not hangs: bluke-005 waited 141 minutes for bluke-003 to land, and eggnogg-rework-026 waited 40 minutes for the operator to install a tool. So the unit to check is the command, not the node.

**Blocking vs. backgrounded.**
- **Blocking** fits a command that finishes within 2 minutes. A narrow test here takes 1 to 8 s: `go test -run '^TestPooledKindDrainsOnGoAndOnIdle$' ./internal/daemon` took 7.8 s, against 143 s for the package and 3m05s for the suite at load 16.
- **Backgrounded, a lead's turn ends.**
  - Having the model look every 2 minutes would cost a model call that rereads the session's whole context each time: five for a 10-minute build that is going fine. A lead's context reaches 579k tokens (121 measured bluke-003 at that).
  - So code should do the looking, and the lead should hear only how the run ended.
- **An executor can't end its turn to wait.** It runs as a background Agent subagent or a T3 delegate task (`ExecutorLead`, `internal/brief/brief.go`), so the end of its turn is the end of its task.
  - It runs a long command in the foreground.
  - It needs that call to return when the command stops moving, not at the harness's 10-minute move to the background.

## Proposal
In:

- **One watcher, in `internal/proc`** beside `Group` and `KillGroup`. It is used by `instance.command.Run` (`Model.Run` passes none), by `setup.Ensure`'s run, and by `bmesh run`.
  - **What a checkup reads**, since the last one:
    - the bytes and lines the run printed, and its newest non-empty line, cut at 160 characters. A `\r` progress meter counts as output.
    - the CPU time its process group used: a delta per pid, reaped children included. This comes from 117's `internal/hoststat` per-pid reader, filtered to the group: `/proc/<pid>/stat` on Linux (utime + stime + cutime + cstime), `ps -axS` on macOS.
    - The output count comes through 121's `instance.Job` output writer.
  - **Shared pieces.** Whichever of 117, 121 and this spec ships first builds those pieces where its spec names them.
  - **Quiet** means no new output byte and under 1% of one core's time since the last checkup (1.2 s over 2 minutes).
  - **Cadence** is every 2 minutes, or every 5 when the run's timeout is an hour or more, as `soak-darwin`'s 100m is. The timeout is the run's declared expectation, so there is no new key, and no kind file changes.
    - A run that ends before its first checkup is never looked at.
    - A run waiting for its slot (119) starts its checkups when its command starts.
  - **Windows** reads no CPU (a `shortcut:` comment), so quiet there is judged on output alone. It is reported and never acted on.
- **Routines and setup only report.** Nothing new is killed or recorded differently: the timeout stays the bound, and a run killed at its timeout stays a recorded fail.
  - Why not kill there: no routine hang was found in 30 days. A false kill would cost a recorded fail or a refused landing. Bluke's verify, for one, waits silently on `flock` for the emulator while another worktree's verify holds it.
  - **A daemon job.** Its instance publishes a progress event `to` the requester, so it counts as the requester's activity: `job <id> running 4m: 37 lines, cpu 61s: <newest line>`, with `; quiet 4m` added when the run is quiet.
    - The map's `jobStartRE` and `jobEndRE` (`internal/web/timeline.go`) and `jobDoneRE` (`tree.go`) don't match it.
    - The routines box shows it as the instance's text.
  - **`routine run`, and the inline kinds `land` and `build` run.** One stderr line: `bmesh: verify 4m: 37 lines, cpu 61s: ok internal/mesh 16.0s`. Under `land`, 121's `cmd` object carries the newest line instead.
  - **Setup.** `bmesh: setup 4m: …` on stderr.
- **`bmesh run [--timeout D] [--no-stall] -- <cmd...>`** waits on an agent's long command, bounds it, and ends it once it stops moving.
  - **How it runs.** Like `lock-lease run`: the command is argv (a pipeline is `sh -c '…'`), run in the cwd in its own process group, with its output passing through as it comes.
  - **What it refuses.** A command whose argv[0] is bmesh (its own executable, or `bmesh` on PATH), exit 2: `bmesh run: bmesh <cmd> bounds itself; run it as it is`. `land`, `routine run`, `routine wait` and `watch` wait silently by design.
  - **Each checkup** prints one stderr line: `bmesh run 4m: 37 lines, cpu 61s: <newest line>`.
  - **The bound.** `--timeout` defaults to 20m, a kind's default.
  - **A stall** is two quiet checkups in a row: 4 minutes, or 10 at the 5-minute cadence. It ends the run.
    - The group gets SIGQUIT first, so Go and the JVM print their stacks, then SIGKILL 10 s later.
    - `--no-stall` keeps a quiet run going until its timeout. It is for work outside the group: a Gradle daemon, adb's server, a container engine, or a Playwright browser, which launches detached.
    - Never on Windows.
  - **Exit.**
    - The command's own code.
    - 5 when bmesh ended the run, with `bmesh run: stalled: no output and no CPU for 4m; last line: <line>` or `bmesh run: timed out after 20m; last line: <line>` on stderr.
    - SIGINT or SIGTERM kills the group and exits 1 (`interrupted`).
  - **The `run` event.** From a node, `bmesh run` publishes a `run` event under it at the start, at each checkup and at the end. Like `land`'s event, it sets no state but counts as the node's activity.
    - It carries the program's base name (`go`, `./gradlew`) and a `key`: a sha256 of the argv and the cwd relative to the worktree's top.
    - It never carries args or output lines. 117's rule applies: the stream is readable by any holder of the hub's token.
    - Text: `go 4m: 37 lines, cpu 61s`; at the end, `go: ok after 3m12s`, `exit 1 after 40s`, `stalled after 4m` or `timed out after 20m`.
    - Object: `{prog, key, since, lines, cpu, quiet, end}`.
    - With no hub, it runs all the same and publishes nothing.
  - **Readers.**
    - `watch` prints a `run` event only when the run ends stalled or timed out.
    - The map's recent-events drawer and the lead page's timeline card skip a `run` event without `end`, and a routine's `job <id> running`.
- **The skills.** Each change is measured with `scripts/skilltest` against today's text. Today the skilltest fake `bmesh` exits 0 on any command it doesn't know, which would score a test that never ran as passing; its fake now runs `run`'s argv for real.
  - **Lead**, at Checkpoints. "(no `sleep`, no polling)" is replaced by: "Run a command you expect to take over 2 minutes in the background as `bmesh run -- <cmd>` and end your turn. It ends by itself when the command does or stops moving, and that end brings you back." The skilltest's lead cases also expect no `sleep N` and no `until … sleep` loop among the session's commands.
  - **Executor**, under While you work. It has no sentence about waiting today; this one is added: "Run a command you expect to take over 2 minutes in the foreground as `bmesh run --timeout 9m -- <cmd>`, with the Bash tool's timeout at 10 minutes. bmesh ends it once it stops moving, so the call returns. Ending your turn ends your task, so never end it to wait. A longer command is a routine's (`routine queue`) or your lead's."
  - **Both.** A stalled or timed-out end means: read its last lines (a Go or JVM stack is there), find what it waited on, and fix that before you run it again.

Out:
- killing or recording a quiet routine or setup run differently. The checkups' `quiet` lines are the data a later rule would need.
- model kinds' checkups: `claude -p` prints nothing until its result, 122 reads its calls from the usage stream, and its timeout bounds it.
- `lock-lease run` (bounded by its lease) and the operator steps' `check:` commands (bounded at 2 minutes, `spec.CheckTimeout`).
- any `timeout` default, and the silence grace.
- a hook that wraps every Bash call.
- readiness waits, such as a `sleep 2` while a server starts.
- telling a run slower than usual. It was dropped this round because nothing it would compare against exists: none of 82 measured verifies passed twice its repo's median, and none of the 172 agent commands that ran 2 minutes or more repeated its command line even three times.

Judge: opus, because the watcher must read a process group's CPU on two OSes without ever ending a live run, and it reaches the daemon, setup, the inline runs and a new command.

## What you'll see
- `bmesh routine run verify` prints a line every 2 minutes on stderr while it runs. A daemon job's instance reads `job <id> running 6m: …` on the map's routines box, ending `quiet 4m` when nothing moved.
- A lead runs `bmesh run -- ./gradlew test` in the background and ends its turn. A hang ends the run within 4 minutes (10 for an hour-long bound), with the stack on stderr, and the node isn't silent while it runs.
- An executor's foreground `bmesh run` returns within 4 minutes of a hang, not at the harness's 10-minute move to the background.
- `bmesh run -- bmesh land` is refused.
- `bmesh watch` shows an agent's run only when it stalled or timed out.

## Verification
- **`internal/proc` tests**, with the checkup a package variable the tests shorten:
  - a grouped `sleep 600` is quiet at each checkup;
  - a busy loop is never quiet;
  - a command printing a `\r` meter, with no newline and no CPU, is never quiet.
- **`cmd/bmesh` tests through the entry points:**
  - `bmesh run -- sleep 600` exits 5 with `stalled`, after SIGQUIT, and a grandchild's pid is gone;
  - `--no-stall` runs to `--timeout` and exits 5 with `timed out`;
  - `bmesh run -- sh -c 'exit 3'` exits 3;
  - `bmesh run -- bmesh land` exits 2;
  - from a node, the `run` events carry `end` and no args, and `watch` prints only the stalled one;
  - `routine run` of a kind whose command sleeps prints a checkup line ending `quiet`, and records the run as before.
- **A daemon test:** a command kind's job publishes `job <id> running` with `to` the requester. The timeline regexes and `jobDoneRE` skip it, and its result is recorded as before.
- **The threshold's run count, on k16, before landing.** Under `bmesh run`, run this repo's `go test -count=1 ./...` 10 times and `go test -count=3 ./internal/daemon` 3 times. No run may stall, and the longest quiet stretch of each is printed.
- **On k16:** `bmesh run -- sleep 300` ends stalled at about 4 minutes.

## Shown
- `routine run verify` prints a checkup on stderr: `cache/bmesh-v --node buildermesh-128 routine run verify` (this branch's build) -> `bmesh: verify 2m: 3 lines, cpu 446s: ok  	github.com/rickdiculousli/buildermesh/addons/t3done	0.872s`; and `TestRoutineRunPrintsCheckups` (cmd/bmesh/cmd_run_unix_test.go): a sleeping kind's last line ends `; quiet`, its result recorded as before.
- A daemon job's instance reads `job <id> running …`: `TestCommandJobReportsCheckups` (internal/daemon/routines_test.go) reads the instance's state text `job <id> running …` mid-run, its progress event `to` the requester, the last ending `; quiet`; the routines page shows a working instance's text (`instanceSub`, internal/web/js/routines.js).
- A hang ends within 4 minutes, and so does an executor's foreground call: `bmesh run -- sleep 300` on k16 -> `bmesh run: stalled: no output and no CPU for 4m`, exit 5 after 241 s, after `bmesh run 2m: 0 lines, cpu 0s; quiet 2m` and `bmesh run 4m: 0 lines, cpu 0s; quiet 4m`.
- The node isn't silent while it runs: `TestRunEventsAndWatch`: run events under the node at start, each checkup and end (`run` moves no state; any event under the node is its activity).
- `bmesh run -- bmesh land` is refused: `cache/bmesh run -- bmesh land` -> `bmesh run: bmesh land bounds itself; run it as it is` (exit 2).
- `bmesh watch` shows a run only when it stalled or timed out: `TestRunEventsAndWatch` (one ` a run sh: stalled after …` line, none for the ok run).
- `internal/proc` checkups: `TestWatchSleepIsQuiet`, `TestWatchBusyLoopIsNeverQuiet`, `TestWatchProgressMeterIsNeverQuiet` (internal/proc/watch_unix_test.go); the group's per-pid CPU with a reaped child: `TestGroupCPU` (internal/hoststat/group_test.go).
- Entry points: `TestRunEndsAStall` (exit 5, the QUIT trap's line printed, the grandchild gone), `TestRunTimeoutCodeAndRefusal` (`--no-stall` -> `bmesh run: timed out after 3s`, `exit 3` -> 3, bmesh refused, usage), `TestRunEventsAndWatch` (events carry `end`, no args), `TestRoutineRunPrintsCheckups`.
- Daemon: `TestCommandJobReportsCheckups`; the regexes skip it: `TestJobRunningIsNoJobEdge` (internal/web/timeline_test.go: `jobStartRE`, `jobEndRE`, `jobDoneRE`, `jobMsRE`).
- The threshold's run count on k16, under `bmesh run` at load 15 to 27: 10 × `go test -count=1 ./...` (155 to 224 s each) and 3 × `go test -count=3 ./internal/daemon` (442, 497, 604 s): no run stalled, the longest quiet stretch of every one `none` (no checkup ever quiet). Two runs failed their tests, not on a stall: run 10's `TestSyncLevelsEveryBoxAndHoldsOnASecureOne` (an invite that got no answer under load; `go test -count=3 -run TestSyncLevelsEveryBoxAndHoldsOnASecureOne ./cmd/bmesh` -> `ok`), and daemon run 3 reached go test's own 10m limit (`panic: test timed out after 10m0s`, the test running then 2 s old).
- The skills, measured: `scripts/skilltest --cases lead-s,lead-stop,lead-exec --runs 2 --model opus`, base vs this text: 6/6 vs 6/6, no expectation flipped, $1.68 vs $1.65; the lead cases now expect no `sleep N` or `until … sleep` loop (`no_sleep`, scripts/skilltest/cases.go, failing reference "waited by hand"), and the fake runs `run`'s argv for real. A first try with only the spec's sentences scored 5/6 (a lead ran `sleep 60; bmesh inbox` after `ready`), so the lead skill keeps "Never wait with `sleep` or a poll." ahead of them.
- Easy to miss: the `run` event never carries the command's args or an output line: `TestRunEventsAndWatch` fails on any event holding `secret-arg` or `secret-out`.
- Not shown: the routines page drawn in a browser with a running job's words: no capture run; the state text it reads is tested.

## Risks and the alternative considered
- **A false stall ends good work, in `bmesh run` only.**
  - Output and CPU are read together: this repo's `internal/daemon` tests use 43 CPU-seconds in 149 s (about 35 per 2 minutes) while printing nothing under `go test ./...`.
  - Work the group can't see is what `--no-stall` is for.
  - The cost is a rerun, since nothing is recorded.
- **The stream grows.** It gains about four events per long run: about 1,400 events a month at this month's rate of 189 background commands and 151 inline verifies. That is roughly 0.4 MB, against the ~1.3 MB a month 121 measured. The map's timeline reads that stream every 30 seconds.
- **Alternatives:**
  - *Only skill text* ("give every command a `timeout` of twice what you expect"). It costs nothing, but leads already broke "no `sleep`, no polling" 161 times, and the 40-minute kills were a hand-written wait.
  - *A hook that wraps every Bash call.* It would catch every command, but it rewrites what the agent typed (pipes, `cd`, backgrounding), and every short call pays for it.
  - *The model checks every 2 minutes*, as the idea reads literally. Each check is a full-context call, where the code check is free and the model hears only the end.

## Upgrading
nothing

## Needs from the operator
