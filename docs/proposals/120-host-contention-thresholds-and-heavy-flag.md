---
id: 120
title: A box records how long its CPU work queued for a core, and the hosts tab names who queued and who held the CPU
kind: feature
size: M
judge: opus
status: rejected
lens: ux
proposed_by: coordinator
proposed_on: 2026-11-23
reason: built, then removed: the box's queued share reads 73% for a test suite alone, counting its own threads, so the hosts tab's "CPU queued" words misled; the per-thread reads cost about 1,400 file reads a minute for it
---

## Opportunity
117 records each box's pressure (PSI `some` avg60 for cpu, memory and io) and each agent's CPU and memory every minute. Box pressure can't say whether work was slowed or whose: CPU `some` of 11% means some task waited for a core 11% of the minute, perhaps a Gradle daemon nobody waits on.

Linux counts how long work queued for a core, with no kernel setting (`sched_schedstats` and `task_delayacct` are 0 on k16 and both files are filled; it needs `CONFIG_SCHED_INFO`):
- **The box:** `/proc/schedstat`'s `cpuN` lines, fields 8 and 9: nanoseconds tasks ran and waited runnable on that CPU, exited tasks included.
- **A thread:** `/proc/<pid>/task/<tid>/schedstat`, fields 1 and 2. `/proc/<pid>/schedstat` is the main thread's only, and a reaped process's counts are gone, unlike its CPU ticks, which `cutime` carries to its parent.

A queued share (wait over run plus wait) is a floor on how much longer CPU work took: 0.25 means at least 1.33x. Hyperthread siblings and lower clocks slow the work that does run, which no counter here sees.

## Proposal
Size M as built: the sampler's reads, its record, `/api/hosts`' buckets, the hosts tab's head line, the harness, the seed and the docs. The spec first also coloured each box ok, warn or red by its queued share; the hour on k16 (below) showed the box's share can't say that anyone was slowed, so the levels are out and re-specced separately.

In:
- **The measure, Linux** (`internal/hoststat`, read by 117's sampler each minute):
  - `Minute` gains `run` and `wait`: the box's seconds from `/proc/schedstat` since the last read.
  - Each agent's `Use` gains `run` and `wait`: seconds summed over its threads' `schedstat` since the last read, for each process whose `cwd` reads. A thread not read before counts all its run and wait (as a process not read before counts all its CPU); a tid is the same thread while its start time (task `stat` field 22) is; a thread gone mid-read is skipped.
  - `pressure` gains `mem_full`: memory `full` (every non-idle task stalled on memory) as the delta of its `total=` over the interval, in percent.
  - All `null` on macOS, and on a kernel whose files are missing or read `0 0 0`.
  - `shortcut: an agent's run and wait miss processes that exited between reads (a build's compiles); per-agent cgroups would keep them, upgrade if the agent figure misleads.` An agent's `run` and `wait` are `null` when its `run` covers under half its CPU seconds over the interval.
- **The hosts tab's head line**, uncoloured, from the box's newest minute: `k16 · CPU queued 22% · bluke-004 queued 31% · bluke-002 holds 52% · load 25.5/16 · 11/29 GB`: the box's queued share (`–` when the box ran under 6 s in the minute, where a few wakeups make the share), the agent that queued most (of those that ran 6 s or more) and the one holding the most CPU (its CPU over the box's). 117's `CPU wait` (PSI) leaves the head line and stays in the contention chart.
- **`/api/hosts`** points gain the box's and agents' mean `run` and `wait`, and the mean `mem_full`.
- **A harness:** `go run ./scripts/hostload --cwd <dir> --minutes N` prints each minute's `Minute` as 117's `hoststat.Sampler` reads it, with a fake node whose worktree is `<dir>`, and each agent's coverage (its run over its CPU seconds); without `--cwd`, against the hub's nodes.
- **Seed:** `scripts/seed` publishes `run`, `wait`, agents' `run` and `wait`, and `mem_full`.
- **Docs:** `docs/internals.md`'s `bmusage.host` shape and `/api/hosts` line; the manual's hosts tab (the head line and what a queued share is) and the harness.

Out:
- **A box's level (ok, warn, red), its colour, the strips under the timeline and a `Hosts` KPI tile:** re-specced separately, on a measure that leaves out a scope's own queueing (the finding below).
- a flag for the agent that slows the others, and `bmesh build` refusing or warning on a busy box;
- I/O: no per-process I/O wait without kernel delay accounting;
- the run-time slowdown the floor misses (hardware counters, `perf_event_open`);
- per-agent cgroups in bmesh.

Judge: opus, because `Minute` and `Use` are records every box writes.

## Finding: the box's queued share doesn't say who was slowed
The spec's false-red check ran before the levels were cut: the harness over an hour of real load on k16 with four leads working. 41 of 60 minutes read red, 4 warn and 15 ok, where the spec allowed one in ten red. The box's share split in two, 0.03 to 0.08 or 0.66 to 0.88, so no threshold sits between them. The red minutes were this repo's `go test ./...` runs: every CPU busy and 2 to 7 threads waiting on each, mostly test binaries queueing against their own threads. On k16 every agent runs in its own cgroup scope (T3's `t3code-claude-<id>.scope`, the CPU controller on down to it), and the kernel shares the CPU fairly between scopes, so neighbours barely queue: a single-threaded `xz` running in the red minutes 09:36 to 09:38 queued 1 to 2% of its time while the box read 77%. Beside 32 busy loops the box read 0.53 to 0.55 queued either way: with the loops in `xz`'s own scope, `xz` queued 48 to 56% of its time and took 3.3 to 3.5x as long; with them in another scope, `xz` queued 13 to 22% and took 1.9x as long, mostly because its run slowed (hyperthreads). A `go build -a` alone on a quiet box queued 0.23 to 0.25 against its own compiles. The box's share says some work queued, not that anyone was slowed by another's.

## What you'll see
On the hosts tab, a head line per box saying how much its CPU work queued, which agent queued most and which held the CPU.

## Verification
- `hoststat` tests on a fake `/proc`: the box's run and wait from `/proc/schedstat`; an agent's summed over threads, a new thread counted whole, a reused tid (new start time) counted as new, a thread gone mid-read skipped; `mem_full` from `total=`; `0 0 0` and macOS reading `null`; an agent under half coverage reading `null`.
- `/api/hosts` tests: a bucket's means of the box's and an agent's `run` and `wait`.
- The map's check on the seed for the hosts tab, and the head line read on it.
- The harness over an hour of real load on k16, and the slowdown runs below, recorded for the level's next spec.

## Shown
- The head line names who queued and who holds: `node scripts/check.mjs "http://127.0.0.1:44363/?check=1#/hosts" 1280 1400 cache/shots/hosts.png` on `scripts/seed.sh` -> `bmesh ok view=hosts scrollWidth=1280 innerWidth=1280 minTap=40 ... hosts=3 procs=22`; its screenshot, read, heads box-a `box-a · CPU queued 30% · app-0042 queued 34% · app-0045 holds 40% · load 11.6/16 · 16/32 GB` and mac-b `mac-b · pressure normal · session ec03a02c holds 45% · load 1.8/10 · 5/16 GB`, uncoloured; at 390 px the same check prints no `SCROLLS`.
- The overview is as before (eight tiles, no strips): `node scripts/check.mjs "http://127.0.0.1:44363/?check=1" 1600 1000 cache/shots/map.png` -> `bmesh ok view=map ... kpis=8 ... landDup=0`.
- The box's and an agent's run and wait on a fake `/proc` (threads summed, a new thread whole, a reused tid new, a thread gone mid-read skipped, `mem_full` from `total=`, `0 0 0` null, under half coverage null): `TestLinuxQueuedRunAndWait` and `TestParseSchedstatAndFullTotal` in `internal/hoststat/hoststat_test.go`; macOS null: `TestDarwinSampleSumsProcesses`.
- `/api/hosts` means of the box's and an agent's run and wait: `TestHostsHistoryLiveAndAgentColumns` in `internal/web/hosts_test.go`.
- The harness on the real box: `./cache/hostload --cwd cache/load --minutes 1 --every 10s` beside a 10 s busy loop in `cache/load` -> box `run 13.72 wait 0.07`, `'load': {'cpu': 1, 'rss': 12165120, 'run': 10.02, 'wait': 0}`, cover `{'load': 1}`.
- The hour of real load (four leads), before the levels were cut: `./cache/hostload --minutes 60` (that build printed each minute's level) -> `60 {'red': '41 (68%)', 'ok': '15 (25%)', 'warn': '4 (7%)'}`; a red minute with this lead's `xz` beside a suite: `09:36 red buildermesh-131 cpu 6.6 ... | buildermesh-120 cpu 1.0 run 61.32 wait 1.23 cov 0.99`.
- Each agent its own CPU scope: `cat .../app-t3code-agents-708bee415fb5.slice/cgroup.subtree_control` -> `cpu memory pids`.
- The box's share against an agent's, `xz` on 20 MB beside 32 busy loops: `python3 cache/slow/demo.py` -> `same-scope-32   wall   54.8  xz run   24.2 wait   30.5 share 0.56  box share 0.55` and `other-scope-32  wall   31.7  xz run   24.7 wait    6.9 share 0.22  box share 0.54` (alone: `wall   15.5 ... share 0.00  box share 0.02`).
- The slowdown runs (busy loops in their own scope; three each; time over alone, run inflation, box share, `xz`'s own share): `python3 cache/slow/slow.py` -> `xz` alone 0.98-1.01x, share 0.01; beside 8: 1.44-1.53x, inflation 1.36-1.53, box 0.00-0.49 (0.49: another lead's tests), own 0.00-0.06; beside 16: 1.63-1.70x, inflation 1.39-1.54, box 0.10-0.67, own 0.05-0.18; beside 32: 1.81-2.71x, inflation 1.50-1.59, box 0.53-0.76, own 0.12-0.44. `go build -a` alone: 19-20 s, box 0.23-0.25 over its own run; beside 16: 5.47-6.39x, inflation 1.06-1.11, box 0.49-0.51, its agent's coverage 0.18-0.51.
- Easy to miss: an agent whose run covers under half its CPU reads null, not a share from the threads it caught: `TestLinuxQueuedRunAndWait` (`under half covered`).
- Not shown: a real Mac's minute: k16 can't reach the Mac; macOS reads no run, wait or `mem_full` by construction (`TestDarwinSampleSumsProcesses`).

## Risks and the alternative considered
- The share is a floor: with hyperthread and clock contention, work can take several times longer than its share says.
- The share counts work queueing against its own threads (the finding above), so the head line's box share reads high whenever a test suite runs; the agent words say whose threads queued.
- Reading every thread's `schedstat` is at most about 1,400 small reads a minute on k16 (about 700 threads, read only for processes whose `cwd` reads), beside 117's per-process reads.
- Alternatives: thresholds fitted to days of box pressure history, which learns busy rather than slowed; box PSI alone, which says someone waited, not whom.

## Upgrading
nothing

## Needs from the operator
