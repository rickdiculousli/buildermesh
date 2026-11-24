---
id: 120
title: A box reads ok, warn or red by how long its CPU work queued for a core, and the hosts tab and the overview name who queued and who held the CPU
kind: feature
size: M
judge: opus
status: proposed
lens: ux
proposed_by: coordinator
proposed_on: 2026-11-23
---

## Opportunity
117 records each box's pressure (PSI `some` avg60 for cpu, memory and io) and each agent's CPU and memory every minute, and colours nothing. Box pressure can't say whether work was slowed or whose: CPU `some` of 11% means some task waited for a core 11% of the minute, perhaps a Gradle daemon nobody waits on. Setting thresholds from minutes with and without a lead working, as this spec first planned, would learn what a busy box looks like, not what slows work, and needed days of history before anything could be built.

Linux counts how long work queued for a core, with no kernel setting (`sched_schedstats` and `task_delayacct` are 0 on k16 and both files are filled; it needs `CONFIG_SCHED_INFO`):
- **The box:** `/proc/schedstat`'s `cpuN` lines, fields 8 and 9: nanoseconds tasks ran and waited runnable on that CPU, exited tasks included. Over 5 s on k16 with leads working: 9.01 s run, 0.06 s waited.
- **A thread:** `/proc/<pid>/task/<tid>/schedstat`, fields 1 and 2. `/proc/<pid>/schedstat` is the main thread's only (k16's daemon: 218 s run there against 567 s over its 22 threads), and a reaped process's counts are gone, unlike its CPU ticks, which `cutime` carries to its parent.

Measured on k16 (one run each, 16 logical CPUs on 8 cores): a single-threaded loop took 1.94 s alone, queued share (wait over run plus wait) 0.00; beside 32 busy loops it took 10.33 s, share 0.53. Its run time grew from 1.95 s to 4.82 s, since a hyperthread sibling and lower clocks slow the work that does run, which no counter here sees. So a queued share is a floor on how much longer CPU work took, readable from one minute: 0.25 means at least 1.33x.

## Proposal
Built now: 117 is on `main`. Size M: the sampler, its record, one level table and the views that read it.

In:
- **The measure, Linux** (`internal/hoststat`, read by 117's sampler each minute):
  - `Minute` gains `run` and `wait`: the box's seconds from `/proc/schedstat` since the last read.
  - Each agent's `Use` gains `run` and `wait`: seconds summed over its threads' `schedstat` since the last read. A thread not read before counts all its run and wait (as a process not read before counts all its CPU); a tid is the same thread while its start time (task `stat` field 22) is; a thread gone mid-read is skipped.
  - `pressure` gains `mem_full`: memory `full` (every non-idle task stalled on memory) as the minute's delta of its `total=` over the interval, in percent.
  - All `null` on macOS, and on a kernel whose files are missing or read `0 0 0`.
  - `shortcut: an agent's run and wait miss processes that exited between reads (a build's compiles), so its share names who queued but never sets the level; per-agent cgroups would keep them, upgrade if the agent figure misleads.` An agent's share shows only when its `run` covers at least half its `cpu` over the interval; else `–`.
- **The levels, one table** (`hoststat.Level(Minute)`, read by the web and by anything later), each constant with its reason in code and in the manual's hosts section:
  - CPU, Linux: the box's queued share (`wait` over `run` plus `wait`), counted when `run` is at least 6 s: 0.10 or more warn (its CPU work took at least 1.11x as long), 0.25 or more red (1.33x).
  - CPU, macOS: load1 per logical CPU, 1.25 or more warn, 2 or more red.
  - Memory: Linux `mem_full` 1 or more warn, 5 or more red; macOS its kernel level (2 warn, 4 red).
  - The box's level is the worse of its CPU and memory levels.
- **The hosts tab's head line** coloured by the level, saying the box's queued share and the agent that queued most and the one holding the most CPU: `CPU queued 22% · bluke-004 queued 31% · bluke-002 holds 52%`. "Queued" is the new figure; 117's `CPU wait` (PSI) stays as it reads, in the chart.
- **`/api/hosts`** points gain `level` (the bucket's worst minute) and the box's and agents' mean `run` and `wait` (`bucketAcc` in `internal/web/hosts.go` sums them).
- **A ninth KPI tile `Hosts`:** the box worst now, with the head line's words, linking `#/hosts`; with every box ok, `all ok` and the busiest box's CPU. The overview reads `/api/hosts` on its own `every()` (125's rule), and `renderKpis`'s comment says nine.
- **A strip per box under the fleet timeline:** each bucket coloured by its level.
- **Seed and check:** `scripts/seed` publishes `run`, `wait` and `mem_full` giving warn and red minutes; `check.mjs`'s `kpis=` and the manual's check table go from 8 to 9, and `check.mjs` adds `strips=` on the overview.
- **Docs:** `docs/internals.md`'s `bmusage.host` shape gains `run`, `wait` and `mem_full`, its `/api/hosts` line `level`, `run` and `wait`; the manual's hosts section says what each level means.

Out:
- a flag for the agent that slows the others, and `bmesh build` refusing or warning on a red box: each is proposed on its own with the levels' first day of minutes (the Verification's hour and what follows), since a gate on a level never seen in use would refuse builds the builds themselves made red;
- I/O: no per-process I/O wait without kernel delay accounting, and box io `some` reaches 10% under ordinary builds;
- the run-time slowdown the floor misses (hardware counters, `perf_event_open`);
- per-agent cgroups.

Judge: opus, because the levels must mean slowed on real load and `Minute` and `Use` are records every box writes.

## What you'll see
A coloured head line per box saying how much its CPU work queued, who queued most and who held the CPU; a `Hosts` tile naming the worst box in those words; a strip per box under the timeline, so a stall lines up with the lanes.

## Verification
- **A harness:** `go run ./scripts/hostload --cwd <dir> --minutes N` prints each minute's `Minute` as 117's `hoststat.Sampler` reads it, with a fake node whose worktree is `<dir>`, and `hoststat.Level` of it.
- **Slowdown against level, on k16, before landing, with no live node on the box** (box CPU printed in each row), three runs of each:
  - `xz -9 -T1 -c <a fixed 200 MB file> >/dev/null`, CPU-bound and over 90 s alone, alone and beside 8, 16 and 32 busy loops;
  - `GOCACHE=$(mktemp -d) go build -a -o /dev/null ./cmd/bmesh`, many short processes, alone and beside 16 busy loops; it also prints the agent's coverage (its `run` over `cpu` times the interval).
  - The table (load, wall time over alone, run inflation, box share, agent share, coverage, level) goes in `## Shown`. Pass: every alone run reads ok; every run 2x or more slower whose run inflation is under 1.5x reads red. A run missed because of run inflation is listed, never answered by lowering a threshold.
- **False red, on k16:** the harness over an hour of real load with leads working; the share of minutes at ok, warn and red, and each red minute with what ran, go in `## Shown`. If more than one minute in ten reads red, the thresholds are argued again in the spec before landing.
- `hoststat` tests on a fake `/proc`: the box's run and wait from `/proc/schedstat`; an agent's summed over threads, a new thread counted whole, a reused tid (new start time) counted as new, a thread gone mid-read skipped; `mem_full` from `total=`; `0 0 0` and macOS reading `null`; an agent under half coverage showing `–`.
- `Level` table tests: each boundary, the 6 s floor, the worse of CPU and memory, Mac load and pressure level.
- `check.mjs` on the seed: `kpis=9`, `strips=` one per box, and a red box's tile.

## Shown

## Risks and the alternative considered
- The share is a floor: with hyperthread and clock contention, work can take several times longer while its box reads warn (0.53 at 5.3x above). The slowdown table is where that shows.
- k16's lifetime queued share since boot is 0.365 (`/proc/schedstat`), so red may be common under overlapping leads; the hour's count says how common before anything relies on it.
- Reading every thread's `schedstat` is about 760 small reads a minute on k16, beside 117's per-process reads.
- Alternatives: thresholds fitted to days of box pressure history (the first draft), which learns busy rather than slowed and waits days to build; box PSI alone, which says someone waited, not whom.

## Upgrading
nothing

## Needs from the operator
