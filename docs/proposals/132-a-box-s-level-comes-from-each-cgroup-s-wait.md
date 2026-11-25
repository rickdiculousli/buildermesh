---
id: 132
title: A box reads ok, warn or red by how long a cgroup's work stood waiting while another's held the CPU, or stalled on memory, and the hosts tab's head line says whose
kind: feature
size: L
judge: opus
status: rejected
lens: eng
proposed_by: 9769bad1-0a19-48cd-bcac-a9ae45399211
proposed_on: 2026-11-25
reason: not worth an L: agents barely slow each other under fair CPU sharing, the red it found came from the daemon's 512M cap (removed), and it can't see the hyperthread slowdown that remains
---

## Opportunity
120 cut the box's level because its measure, the box's queued share, can't say anyone was slowed: on k16 it read red 41 of 60 minutes, mostly `go test ./...` threads queueing behind their own. Can a box say when one agent slowed another?

Measured on k16 (16 logical CPUs on 8 cores), one run each unless noted:
- **The victim** ran in its own `systemd-run --user --scope` in the agents' slice (`app-t3code-agents-<id>.slice`, where each T3 thread's `t3code-claude-<id>.scope` lives).
- **Its neighbour** ran in that same scope, in another agent's scope beside it, or in a scope under `app.slice` beside `bmesh.service`, at the daemon's weight.
- **"Waited on others"** is the victim scope's `cpu.pressure` `full` delta over its run (not over a minute): the time a CPU ran another cgroup's task while all this cgroup's work on it waited.

| victim, neighbour | as long as alone | its CPU time | box queued (120) | own threads queued (120) | waited on others, over its run | no idle CPU |
|---|---|---|---|---|---|---|
| `xz -9e -T1` alone (2 runs) | 19.0 s | 1.00x | 0.01 | 0.00 | 0.0% | 0% |
| `go build -a ./cmd/bmesh` alone (2) | 18.4-22.1 s | 1.00x | 0.23-0.25 | 0.19-0.20 | 0.4-0.7% | 0% |
| the suite alone | 163 s | | 0.73 | 0.78 | 0.6% | 1% |
| xz, 32 busy loops in its own scope | 3.35x | | 0.53 | 0.54 | 2.1% | 61% |
| xz, 16 / 32 loops in another agent's scope | 1.44x | 1.41x | 0.11 / 0.53 | 0.02 | 1.0 / 1.4% | 68 / 80% |
| xz, the suite in another agent's scope (2) | 1.88-1.89x | 1.87x | 0.78-0.79 | 0.01 | 0.4-0.7% | 6-8% |
| build, the suite in another agent's scope | 1.87x | 1.24x | 0.79 | 0.54 | 6.8% | 7% |
| build, 32 loops in another agent's scope | 2.22x | 0.99x | 0.65 | 0.75 | 10.8% | 70% |
| xz, 32 loops at the daemon's weight | 1.74x | 1.46x | 0.53 | 0.16 | 11.1% | 87% |
| build, the suite at the daemon's weight | 2.03x | 1.15x | 0.81 | 0.77 | 21.7% | 16% |
| build, 32 loops at the daemon's weight | 6.13x | 1.03x | 0.67 | 0.92 | 32.4% | 83% |
| build, capped at `MemoryMax=512M` | 1.93x | 2.17x | 0.30 | 0.28 | 0.7%, memory 21.4% | 0% |
| the suite, capped at `MemoryMax=512M` | 1.06x | 1.30x | 0.72 | 0.77 | 0.7%, memory 5.2% | 3% |

What it shows:
- **A cgroup's `full` leaves out its own queueing and keeps exited processes.** The lone suite read 0.6% while the box read 0.73. Every alone and own-scope run read at most 2.1% on CPU. 120's harness agreed beside the suite: `./cache/hostload --cwd cache/exp/victim --every 10s` printed box queued 0.67, 0.86 and 0.64 while `xz` ran 6.94/9.85/9.95 s and waited 0.03/0.14/0.04 s.
- **Between two agents' scopes, queueing is small.** The kernel's cpu controller shares the CPU between scopes at equal weights, so a parallel build waited 6.8-10.8% and `xz` 0.4-1.4%.
- **The daemon's work outweighs every agent together.** `app-t3code.slice` reads `CPUWeight=10` (a runtime `set-property` drop-in, `50-CPUWeight.conf`), while `bmesh.service` runs at 100. A build beside work at that weight took 2.03-6.13x as long and waited 21.7-32.4%. Two kinds of work run there:
  - the daemon lands a change by running `bmesh land` in the lead's worktree as its child (`internal/daemon/land.go` `execStep`), verify included;
  - routine runs work in `[runner] checkouts` as the daemon's children (`internal/instance`).
- **The daemon's memory cap stalls the work under it.** k16's hand-tuned unit has `MemoryMax=512M`. A build capped the same way took 1.93x and stalled 21.4% (`memory.pressure` `full`). Before these runs, `bmesh.service` had stalled 1,289 s since its start about 28 hours earlier, with `memory.events` `max` at 4,809,507. All the T3 agents together had stalled 4.0 s since their slice started, about a day earlier.
- **What no scheduler counter sees: the run itself slowing.** Beside another agent's work, `xz`'s CPU time for the same work grew 1.41-1.87x (hyperthread siblings, caches, clocks) while it waited 0.4-1.4%.
- **Seconds with no idle CPU say neither thing.** The lone suite left no CPU idle 1% of its run, and xz ran 1.89x as long with the box full 6-8% of the time.

## Proposal
Size L: `bmusage.host`, which every box's map reads whatever its release, changes shape (fields in, fields out). It also touches the sampler, one level table, the API and the hosts tab.

In:
- **The measure, Linux** (`internal/hoststat`, 117's sampler, the minute series only; a live answer's `cgroups` is `null`):
  - Each process whose `cwd` reads also has its `/proc/<pid>/cgroup` `0::` line read: its cgroup.
  - For each cgroup those processes are in, the deltas of `cpu.pressure` and `memory.pressure` `full total=` (µs) since the previous read.
  - The series keeps each cgroup's directory inode and totals. A cgroup not read before, or one with a new inode, counts its whole totals only when its directory's ctime is after the previous read (a T3 thread's scope is made with its process). Otherwise it is seeded and counts nothing this time. A total below its previous read counts whole.
  - A cgroup whose pressure files are missing or hidden (`psi=0`, `cgroup.pressure` 0) is left out.
  - `shortcut: a cgroup removed before the next read (systemd removes a T3 thread's scope once its processes exit) loses its last interval, and one that lives under a minute is read at most once; read a cgroup's totals whenever a new process is seen in it if that misleads.`
- **The record:** `Minute` gains `cgroups`, `{<name>: {cpu_full, mem_full, agents}}`.
  - `cpu_full` and `mem_full` are each delta over the interval, in percent to two decimals (as 120's `pressure.mem_full` was).
  - A cgroup is kept only when either is at least 0.2, so a quiet minute's record is today's size.
  - `name` is the cgroup's last path element (under systemd, its unit: `t3code-claude-2be4bbca.scope`, `bmesh.service`), or its full path when two share one.
  - `agents` is the sorted list of its processes' agents by 117's attribution, `other` included.
  - `{}` means read, and nothing waited. `null` on macOS, without cgroup v2, on a kernel whose `cpu.pressure` has no `full` line (before 5.13), or when no cgroup's pressure files read.
  - Out, since the level replaces them: the box's and agents' `run` and `wait`, the per-thread `schedstat` reads (about 1,400 a minute on k16), `Cover` and `minCover`, and the box's `pressure.mem_full`, all with their `/api/hosts` means.
- **The level, one table** (`hoststat.Level(Minute)`), each constant with its reason in code and in the manual:
  - A cgroup's share is the larger of its `cpu_full` and its `mem_full`, worded by which one it is.
  - The box's level is its worst cgroup's: ok, warn or red. It is `null` with `cgroups` null (every Mac, and every minute from a release before this one).
  - The thresholds start at 5% for warn and 10% for red, from the runs above. The build sets them from minutes, as Verification says.
- **The hosts tab's head line**, coloured by the box's level:
  - From the newest minute, the cgroup with the largest share, named by its agent when all its processes are one agent's, else by its name. Then the agent holding the most CPU (120's words), then load and memory.
  - CPU: `k16 · bluke-004 waited 24% on others · bluke-002 holds 52% · load 25.5/16 · 11/29 GB`.
  - Memory: `k16 · bmesh.service stalled 21% on memory · …`.
  - `no waits` when no share reaches 1%.
  - 120's `CPU queued` and `<agent> queued` words go. A Mac's `pressure <level>` words stay.
- **`/api/hosts`:** each box gains `level`, `Level(last)`, which the web computes and never publishes (`ok`, `warn`, `red` or `null`). Each point gains `level`, its bucket's worst minute's.
- **Seed, harness and check:**
  - `scripts/seed` publishes `cgroups` giving warn and red minutes, and stops publishing `run`, `wait` and `mem_full`.
  - `scripts/hostload` prints each minute's `cgroups` and level in place of `cover`.
  - `check.js` adds `hostLevel=`, the head lines drawn with a level, and `check.mjs` requires at least 1 on `#/hosts`.
- **Docs:**
  - `docs/internals.md`: the `bmusage.host` shape and the `/api/hosts` line.
  - The manual's hosts tab: the head line, what a level means, and what it misses (the run itself slowing; agents sharing one cgroup; a cgroup's own `cpu.max` throttling, which counts as `full`).
  - The manual's hostload paragraph, which says `cover` today, and the check table's `hostLevel=` row.

Out:
- **The run itself slowing beside others' work:** the largest part for single-threaded work in the table, invisible to every scheduler counter. Hardware counters per cgroup need `kernel.perf_event_paranoid` at 0 or `CAP_PERFMON`, and k16 reads 4. So an `ok` box can still mean a step ran nearly twice as long; the manual says so.
- **The `heavy` flag, and `bmesh build` refusing or warning on a red box:** `full` says who waited, not on whose CPU (the holder in the head line is a guess at that). This measure hasn't read an hour of real load yet: k16 ran no leads while it was measured. Each is proposed on its own with that hour's table.
- **The overview's `Hosts` tile and a level strip under the fleet timeline:** pitched apart, to be built once this level's hour on k16 says its red minutes mean what the table says.
- **Where the daemon's work runs:** landings and routine runs work in the daemon's unit, under its cap and at its weight. The self-update build already leaves the unit through `selfupdate.Scoped()`, a transient scope. Moving landings and routine runs out the same way is the fix for the cost this level shows, and a change of its own. A landing outside the unit would outlive a daemon restart, and which weight jobs get is the operator's call.
- **I/O:** each cgroup has an `io.pressure` `full`, but no run here waited on I/O (`io some` at most 2.8%), so nothing sets its threshold.

Judge: opus, because the record every box writes changes shape and each level must mean what minutes of the table's runs measure.

## What you'll see
- A coloured head line per box, naming who waited on others or what stalled on memory, and who holds the CPU.
- On k16, a landing's verify runs in `bmesh.service`, in the landing lead's worktree, so its processes count as that lead's. While it runs beside another lead's build, the box reads warn or red: the building lead waited on others, and the landing lead holds the CPU. That is the daemon's cost made visible, not a state the level should hide.

## Verification
- `hoststat` tests on a fake `/proc` and cgroup root:
  - a process's cgroup from its `0::` line;
  - `cpu_full` and `mem_full` as percents of the interval;
  - a cgroup with a new inode counted whole when its ctime is after the previous read, and seeded when not;
  - a total below its previous read counted whole;
  - one agent's cgroup, and a shared one named by its name;
  - two with the same last element keyed by path;
  - one under 0.2 left out;
  - `null` for no `full` line in `cpu.pressure`, for no cgroup v2, and on macOS;
  - a live answer's `cgroups` `null`.
- `Level` table tests at each threshold's edges, memory read as CPU is, and `null` cgroups.
- `/api/hosts` tests: a point's `level` is its bucket's worst minute's, and a box's `level` is `Level(last)`.
- The map's check on the seed: `hostLevel=` at least 1 on `#/hosts`, at 1600x1000 and 390x844 with no `SCROLLS`.
- **The thresholds, from minutes, on k16**, with the build's sampler: `hostload --cwd <victim dir> --every 1m`.
  - Each row of the table three times, its victim looped for at least 3 minutes in one scope, counting only minutes wholly inside the run.
  - warn sits above the highest minute of any alone or own-scope run. red sits at or below the lowest minute of any run beside another scope that took twice as long or more with its CPU time grown under 1.5x.
  - The capped rows read memory: each is listed with its level, and a capped run that took twice as long or more reads red.
  - If warn and red can't both be placed so, the level is dropped: the head line stays uncoloured with its words, and the table is reported. A run slowed by its own CPU time growing is listed, never answered by moving a threshold.
- **An hour of real load on k16 with leads working**, recorded for the flag's and the build gate's spec: minutes by level, and each red minute's cgroup and holder. If no leads run before landing, Shown says so and that spec waits for the hour.

## Shown

## Risks and the alternative considered
- **The cgroups are only as fine as the runner makes them.** Only the `t3` runner gives each agent its own cgroup. Under the `claude` runner (the default), turns the daemon starts run in its unit, and under `tmux` panes share their server's. Agents sharing one cgroup queue within it, which `full` leaves out, so one slowing another there reads ok.
- **Between two agents' cgroups the level rarely reads red.** Of the five runs 1.4x or slower beside another agent's scope, three read ok, one warn and one red over their runs. A minute dilutes runs that short: the red one, 10.8% over its 45 s, is at most 8.1% of a minute. Red came from work at the daemon's weight or under its cap. Until the daemon's work moves (Out), a red box will mostly mean a landing or routine run; whether that is worth an L is the operator's call when picking this.
- **Old and new boxes read each other's minutes.** An older map reads `CPU queued –` for a newer box, and a newer map reads an older box's minutes as no level.
- **The alternatives, each measured above:**
  - 120's box queued share (a lone suite reads 0.73), deleted here.
  - The seconds with no idle CPU, which say neither who waited nor how long.
  - 120's per-agent thread share: it counts own queueing (a lone build reads 0.19-0.20) and misses exited processes (the lone suite's threads covered 0.53 of its CPU).
  - A fixed probe (a pointer chase over 32 MB and arithmetic) timed in its thread's CPU once a second. It sees the run slowing: 0.157 s at the median of 18 reads alone, 1.55x beside 32 loops (18 reads), 2.5x beside the suite (38) and 3.2x beside a build loop (18). But it varied 0.117-0.333 s alone and reads box-wide, not whose.

## Upgrading
nothing

## Needs from the operator
