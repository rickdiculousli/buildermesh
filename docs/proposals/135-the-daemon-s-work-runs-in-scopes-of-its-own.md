---
id: 135
title: On Linux under systemd, the daemon runs each landing and each routine job in a scope of its own, out of its unit's memory cap, at the CPU weight and memory bound the box's config sets, and stopped after the daemon
kind: time-cost
size: M
judge: opus
status: later
lens: eng
proposed_by: 51ddd145-01e0-478b-9081-d503e79bd874
proposed_on: 2026-11-25
reason: k16's daemon memory cap, the cost both measured, was removed from its unit instead; kept for a box that needs a bound or a level later
---

## Opportunity
132 found the daemon's work slowing the agents. Two kinds of that work run inside `bmesh.service`:
- **The landings it runs for leads.** `internal/daemon/land.go` `execStep` runs `bmesh land` as the daemon's child, and the repo's verify, an inline kind, runs inside it.
- **The routine jobs its worker runs in `[runner] checkouts`:** `internal/instance` `run`, and `internal/setup` for a checkout's setup. On k16 those jobs are the queued kinds: reviews, each a `claude -p`. The two claude processes on k16 now hold 259 and 397 MB.

On k16 that unit has CPU weight 100 and `MemoryMax=512M`, a cap set by hand in `~/.config/systemd/user/bmesh.service`. T3 puts every agent under `app-t3code.slice` at `CPUWeight=10`, through a runtime drop-in of its own (`50-CPUWeight.conf`, "created via systemctl set-property", remade at each T3 start).

On k16, since `bmesh.service` started (2026-11-23 12:26, 32 h before these reads):
- **It did 14.5 CPU-hours of work** (`cpu.stat` `usage_usec` 52,228 s). The daemon process itself used 3,163 s of that. The rest was its children.
- **It ran 39 land steps, 185 minutes of landing in all** (from the journal's `land: <node> landing` lines to the next line for the same node). 25 of those steps were Bluke's, whose verify is Gradle at `-Xmx4g` and then `cargo test`.
- **It stalled on memory for 1,289 s** (`memory.pressure` `full`), with `memory.events` `max` at 4,809,507 and `memory.peak` at the cap. These are the totals 132 read; no landing ran between its read and this one. The unit also throttled its sockets 2,354,883 times.

**Measured on k16** (16 logical CPUs, 29 GB, zram swap):
- **The agent:** `go build -a ./cmd/bmesh` looped in a scope under T3's agents' slice, as 132 ran it, started 5 s before each verify, so its first build runs beside the verify's first minute (the suite's compile phase). Alone it took 18.7, 19.3 and 18.6 s across the series, and each ratio is against their mean.
- **The landing's verify:** each verify ran in a scope beside `bmesh.service`, placed three ways:
  - as the daemon runs it today: weight 100 under `MemoryMax=512M`. This is a little roomier than the real unit, which also holds the daemon (36 MB of its own memory now);
  - in a scope of its own at weight 100 with no cap;
  - in a scope of its own at weight 10 with no cap.
- **Bluke's verify:** a warm checkout after a one-line change to one Kotlin file.

Runs per cell are in parentheses.

| verify, placed | alone | its CPU | peak memory | stalled on memory | beside the build loop | the loop's first build, waited on others | the loop's builds overall |
|---|---|---|---|---|---|---|---|
| buildermesh's suite, as today | 179.9 s (1) | 743 s | 512 MB, the cap | 5.6% | 183.6-187.6 s (3) | 2.98-3.04x, 15.7-20.5% | 1.31-1.34x |
| suite, own scope, weight 100 | 158.6 s (1) | 452 s | 2.41 GB | 0 | 165.5-167.5 s (3) | 2.17-2.22x, 12.2-14.6% | 1.20x |
| suite, own scope, weight 10 | | | | | 167.3-168.3 s (3) | 2.04-2.10x, 5.5-6.4% | 1.19x |
| Bluke's verify, as today | 357.0-375.3 s (2) | 525-541 s | 512 MB, 2.7 GB swapped | 17.4-18.1% | 552.9 s (1) | 1.17x, 1.3% | 1.15x (26 builds) |
| Bluke, own scope, weight 100 | 109.1-109.5 s (2) | 356-364 s | 3.34-3.42 GB | 0 | 171.0 s (1) | 1.35x, 1.9% | 1.28x |
| Bluke, own scope, weight 10 | | | | | 174.9 s (1) | 1.27x, 1.4% | 1.28x |

`scripts/hostload --cwd <the loop's dir> --every 1m`, beside the first run of each row, read the box's memory `full` at:
- 9.8-19.9% in each minute of Bluke's capped verify, and 0.02 uncapped;
- 15.8% in the suite's capped compile minute, and 0 uncapped.

What it shows:
- **The cap is most of the cost.**
  - Bluke's verify took 3.3-3.4x as long under it, and 3.2x beside the loop. That is the time `land.bluke` is held, and every lead waiting to land Bluke waits that long too.
  - The suite took 1.13x as long and burned 291 s more CPU on reclaim. The agent beside it paid for that too: its first build ran 2.98-3.04x capped against 2.04-2.22x uncapped, and the loop overall 1.31-1.34x against 1.19-1.20x.
  - Beside Bluke's capped verify the agent was hardly slowed, because that verify stood stalled on memory. But it was slowed for 553 s, not 171: about 72 s of its builds lost against 37.
- **Weight 10 halves how long an agent's work waits on a landing, but moves its time little.**
  - Through the suite's compile phase, the first build waited 12.2-14.6% at weight 100 and 5.5-6.4% at 10.
  - Its time went from 2.17-2.22x to 2.04-2.10x, and the loop overall from 1.20x to 1.19x.
  - The suite took about a second longer: 167.3-168.3 s against 165.5-167.5 s.
  - Beside Bluke, which used about 3 of the 16 CPUs, the weight changed nothing.
- **What neither fixes: the run itself slowing beside other work** (132's Out). At weight 10, waiting only 6% of the time, the first build still took twice as long.

## Proposal
In:
- **Each job in its own scope.** At start, the daemon decides once whether it can scope its jobs. It can when both hold:
  - it runs on Linux as a systemd user service: its own cgroup (`/proc/self/cgroup`'s `0::` line) sits under `user@<uid>.service/` and ends in a `.service` unit;
  - a probe scope exits 0: `systemd-run` with the properties below, running `true`.

  It then runs each of the following through `systemd-run --user --scope --quiet --collect --unit=<name> --slice=<its own slice> -p BindsTo=<its unit> -p Before=<its unit> -p StopPropagatedFrom=<its unit> -p CPUWeight=<cpu_weight> [-p MemoryHigh=<memory_high in bytes>] -- <argv>`:
  - **each lander step** (`land`, `done`, `held`, in `execStep`), named `bmesh-<step>-<node>-<6 hex>.scope`;
  - **each routine job's setup and command**, command and model kinds alike (the worker's `instance.run` and `setup.Ensure`), named `bmesh-setup-<kind>-<job id>-<6 hex>.scope` and `bmesh-<kind>-<job id>-<6 hex>.scope`. The suffix keeps a job handed back and run again from meeting its earlier scope;
  - **the self-update build.**

  A node name keeps only the characters a unit name allows; any other becomes `_`.

  `systemd-run --scope` execs the command under the same pid (measured once: 25 ms to start), so the daemon's interrupt, group kill, output and checkups reach the command as today. Inline runs are never scoped:
  - `routine run`, and `land` run in a lead's own turn, stay in the cgroup they were started in;
  - the inline verify inside a daemon's landing stays too: it already runs in that landing's scope.
- **`selfupdate.Scoped()` goes.** It is an env test that every T3 agent's shell passes, because agents inherit `t3code.service`'s `INVOCATION_ID`.
  - `Install` and `InstallModule` take the scope from their caller: the daemon passes its own, and `bmesh update` (`cmd_update.go`) passes none, so a build by hand runs where it was run.
  - The five `t.Setenv("INVOCATION_ID", "")` lines that only shield tests from that misfire go: `internal/daemon/update_test.go`, `internal/selfupdate/selfupdate_test.go` (2) and `cmd/bmesh/cmd_version_test.go` (2).
- **Stopped after the daemon.** `BindsTo=`, `Before=` and `StopPropagatedFrom=` each name the daemon's unit.
  - A stop or restart reaches the daemon first. It interrupts its landings and waits for them (its land loop waits for its steps), kills its jobs' groups and hands the jobs back, as a stopping daemon does today. Only then does systemd stop each scope, by then empty or holding only what a job left behind.
  - **Measured on k16** (systemd 259), with a test service that does the same: on SIGTERM it interrupts its child and waits for it.
    - On a `systemctl stop` and on a `systemctl restart`, the service got SIGTERM first. The child got the service's SIGINT, cleaned up for 3 s and exited, then the service exited, then the empty scope stopped.
    - On a `kill -9` of the service under `Restart=always`, the scope got SIGTERM and exited cleanly before the service restarted.
    - With `After=` in place of `Before=`, systemd stopped the scope first. The daemon would then read a landing's `interrupted` as a refusal and wake the lead, and would answer an interrupted job as failed.
    - With `BindsTo=` alone, a restart left the scope running.
  - k16's unit has no `KillMode=` line, so a stop already ends everything in it. Units `bmesh init --service` writes have `KillMode=process`; under those, a landing used to outlive a daemon killed outright, and now stops with it.
  - The claude runner's turns are not scoped, so `KillMode=process` still keeps them across a restart.
- **The box's choice**, a new `[jobs]` table in `config.toml`, read at start:
  - `cpu_weight` (1-10000, default 100) is each scope's `CPUWeight`. 100 is what systemd gives every unit, so a box that tunes nothing sees no CPU change.
  - `memory_high` (default `""`: none of its own, only its slice's) is each scope's `MemoryHigh`, written as `[hub] max_file_store` is (`config.ParseSize`: `8GB`, `4GiB`).
    - Past it, a job is throttled, never killed, so a bound set too low costs time, as the capped rows show, and never puts a verdict on a tree.
    - The default is none because what a job needs is its repo's: Bluke's verify peaked at 3.42 GB and the suite at 2.41 GB. The daemon's own cap still guards the daemon.
  - A value out of range or unreadable is a config error naming the key.
- **Where it can't scope, as today.** Jobs run in the daemon's process tree on:
  - macOS (launchd) and Windows (Task Scheduler);
  - Linux with no user manager, a daemon run as a system unit, or one not run as a unit (in a terminal or tmux);
  - a failed probe: no `systemd-run`, systemd before 249 (which lacks `StopPropagatedFrom=`), or no user bus.
- **The daemon says which, once at start:** `jobs: own scopes in app.slice (CPUWeight=100, no MemoryHigh), bound to bmesh.service`, or `jobs: in the daemon's process tree (<why>)`.
- **A scope systemd refuses is the daemon's failure, not the job's.**
  - When this happens, `systemd-run` prints `Failed to start transient scope unit: ...` and exits 1 before the command starts. `instance` returns that as an error of its own before any reply is judged.
  - A job refused this way is handed back as a job whose slot is busy is (`nakDelay`, 30 s). Nothing is answered and nothing recorded, since a recorded fail would stand for the tree.
  - A refused land step is returned as `execStep`'s error. `landStep`, `doneStep` and `held` already log that and try again next pass.
- **Docs:**
  - the manual: the config.toml block, and a "Job scopes" paragraph under Routines that `bmesh ready`'s "What the daemon does" and Jobs' "A stopping daemon" point to;
  - internals: the self-update build line, and the routines section's argv, properties and probe.

Out:
- **Stopping a job's scope when it ends.** What a job leaves behind stays in that job's scope: outside the daemon's cap, listed by `systemctl --user list-units 'bmesh-*'`, and stopped with the daemon.
  - k16's unit holds two such leftovers now: `adb ... fork-server server` (26.7 h, 17 min of CPU, started during Bluke's landings) and a buildermesh test's `sh slow.sh` (17.4 h).
  - The adb server is a per-user server other sessions share, so killing it when a job ends could cut a device step in flight elsewhere.
  - `slow.sh` is a test fixture's own leak. `cmd/bmesh/slot_wait_test.go` loops until `$MARKS/go` exists, which a test that fails early never writes. That is fixed in the fixture, not here.
- **I/O weight:** no run in 132 waited on I/O (`io some` at most 2.8%).
- **A weight per repo or per kind:** one pair per box. A kind that needs its own is a change of its own.
- **The claude runner's turns, which the daemon also starts in its unit:** no box runs that runner under systemd.
- **macOS and Windows priorities:** launchd's default `ProcessType` lightly throttles the daemon and its jobs, and Task Scheduler starts tasks below normal priority. That is the opposite of k16's case, and neither is measured here: k16 can't reach the Mac.
- **k16's own unit and T3's slice:** Needs from the operator.

Judge: opus, because the stop order, the hand-back on a refused scope and the four call sites must each hold on systemd and fall back cleanly everywhere else.

## What you'll see
- While a landing runs on k16, `systemctl --user list-units 'bmesh-*'` lists its scope (`bmesh-land-bluke-004-3f2a9c.scope`) and a review job's (`bmesh-review-<job id>-<hex>.scope`). `bmesh.service` holds the daemon alone.
- `journalctl --user -u bmesh` says once at start where jobs run.
- Bluke's verify in a landing takes about 110 s alone (175 s beside an agent's build) instead of 357-375 s (553 s). `bmesh.service`'s `memory.events` `max` stops growing while landings run.
- An agent's build beside a landing's verify runs about 2.1x instead of 3x through the verify's compile phase. With `cpu_weight = 10` it waits 5.5-6.4% of that time instead of 15.7-20.5%.
- A daemon restart interrupts a landing in flight as today: the request still reads `landing`, no lead is woken, and the restarted daemon runs it again.
- With 132 built, a landing reads as its own cgroup, named by its lead (its processes all work in the lead's worktree), apart from `bmesh.service`. Either spec can land first.

## Verification
- **Fails before, passes after:** `bmesh update`, run through the real command with `INVOCATION_ID` set and a recording `systemd-run` fake first on `PATH`. Today it builds through `systemd-run`; after this change it doesn't.
- **Tests with shell fakes in `testdata/`**, among them a `systemd-run` that records its args and execs what follows `--`. The daemon's unit, slice and probe result come in through `Deps`, so no test reads the real `/proc/self/cgroup`.
  - A command job, a model job and a setup, through the daemon, each run under a scope carrying the config's weight and bound, the three binding properties and its unit name.
  - The land step's test sits beside `TestExecStepInterruptsOnCancel` in `internal/daemon/land_unix_test.go`, since the lander's other tests replace `execStep` with `Deps.RunStep`.
  - `memory_high = ""` passes no `MemoryHigh`.
  - An inline `routine run`, and the inline verify inside a scoped landing, run unscoped.
  - No user `.service` unit, or a failed probe: every job runs unscoped, and the line is logged once.
  - A refusal from the fake: the job is handed back unanswered and nothing is recorded, and `execStep` returns an error.
  - The self-update build carries the scope only when the daemon gives one.
- **Config tests:** `cpu_weight` 0 and 10001 refused; `memory_high` `8GB` and `4GiB` read; `lots` refused.
- **On k16, a second daemon from the build**, run in its own transient service unit on its own config. `scripts/memcap.sh` sets one up the same way: embedded NATS on another port and its own state dir, nothing shared with the live mesh.
  - A pooled command kind's job in a scratch repo runs in its `bmesh-<kind>-<id>-<hex>.scope` with the config's properties.
  - A landing: a lead on that daemon's tmux runner, started as memcap.sh starts them, runs `bmesh ready`. `land` and its inline verify run in `bmesh-land-<node>-<hex>.scope`.
  - `systemctl --user restart` mid-landing: the request still reads `landing`, no refused wake is sent, and the restarted daemon runs the landing again.
  - Restart mid-job: the job is handed back, and `bmesh result find` shows nothing for its tree.
  - `kill -9` of that daemon mid-job: the scope is stopped, and the job is taken again once its `AckWait` passes.
- **This table's rows again, three runs each**, with the neighbour started through the build's own scope line (as that daemon logs it at start) beside the same build loop, at `cpu_weight` 100 and 10, with `hostload --every 1m` running. Shown lists them against the rows above.

## Shown

## Risks and the alternative considered
- **With no bound of its own, a runaway job now presses the whole box's memory**, as an agent's runaway test does, where today it stalls inside 512 MB. `memory_high` is the guard for a box that wants one.
- **A weight below the agents' can slow landings under heavier load than the table's.** `land.<repo>` is held while a landing runs, and 119 puts landings first for slots. At 10 beside T3's 10, a landing keeps at least half the box when every CPU is contended.
- **The alternatives:**
  - **Tune k16's unit by hand:** `CPUWeight=10`, and a `MemoryMax` above Bluke's 3.42 GB. That needs no code, and the jobs would weigh and fit as here. But:
    - the cap would no longer guard the daemon against its own growth;
    - a job the kernel kills would still stop the whole daemon (`OOMPolicy=stop`, the unit's default);
    - every box would tune its own unit.
  - **Land in the lead's own turn, as before 080:** the work runs in the lead's scope, but the lead's turn stays open through the landing, which 080 saved.
  - **`nice` the jobs:** niceness orders tasks inside one cgroup. Between cgroups the weights decide, so it changes nothing here, and nothing for memory.
  - **Put the jobs in T3's agents' slice:** a landing would weigh as one more agent, but bmesh would name T3's slices.

## Upgrading
- On Linux, a daemon run as a systemd user service now runs each landing and routine job in a scope of its own, outside its unit's memory limit.
  - The question: should those jobs weigh less than other units' work, or have a memory bound of their own?
  - `[jobs] cpu_weight` takes 1-10000. The default is 100, systemd's own; 10 weighs a job as T3 weighs all its agents together.
  - `[jobs] memory_high` takes a size such as `8GB`. The default is none, only the jobs' slice's.

## Needs from the operator
- [k16] Choose the jobs' weight in `~/.config/bmesh/config.toml`:
  - `[jobs] cpu_weight = 10` weighs a landing as T3 weighs all its agents together. In the table it halved an agent's wait beside a landing (12.2-14.6% down to 5.5-6.4%) for about a second of the landing's time, but the agent's build time moved only from 2.17-2.22x to 2.04-2.10x.
  - The default 100 keeps today's 10:1.
  - Leave `memory_high` unset.
  - Nothing else on k16 changes. `bmesh.service`'s `MemoryMax=512M` stays: it now bounds the daemon alone (36 MB of its own memory now). T3's `CPUWeight=10` drop-in on `app-t3code.slice` is T3's own, remade at each T3 start, and stays as it is.
