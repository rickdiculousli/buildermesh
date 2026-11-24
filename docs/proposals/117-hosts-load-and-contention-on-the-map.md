---
id: 117
title: The map shows each box's CPU, memory and contention, and which agents' processes use them
kind: feature
size: L
judge: opus
status: done
lens: ux
proposed_by: coordinator
proposed_on: 2026-11-23
---

## Opportunity
The map says what every agent reads in tokens but nothing about the boxes they run on. Presence (`bm-hosts`, `internal/hosts/canon.go` `Presence`) carries a box's release, OS, repos and notes, and no load. On k16, while four bluke leads ran, `/proc/pressure/cpu` read `some avg10=14.02 avg60=10.99`, then `avg60=11.69 avg300=26.13` with the load average at 25.5 on 16 cores: tasks waited for a CPU for minutes at a time, and no page shows it or which agent caused it. The top CPU users then were Gradle JVMs started in a lead's worktree (322%, 254%, 140%). The operator finds a heavy agent today with `top` on the box, and can't see the Mac's from k16 at all.

This spec records and shows the numbers. Their thresholds (what reads warn or red, and the `heavy` flag) are 120's, set from the history this one records.

## Proposal
In:

- **The sampler** (`internal/hoststat`), one per daemon, its own loop started whenever the daemon has a mesh client (not under `[hosts] enabled`: `hostsRun` starts only with it, `internal/daemon/daemon.go`). It reads:
  - **Box:** CPU in cores (busy since the sampler's previous read of the same series), cores, load1, memory used and total in bytes, swap used in bytes.
  - **Contention:** Linux `/proc/pressure/{cpu,memory,io}` `some avg10` and `avg60` (no PSI file: `null`, never 0); macOS `kern.memorystatus_vm_pressure_level` (1 normal, 2 warn, 4 critical) and load per core.
  - **Processes:** pid, ppid, command name (never args: the live answer is unsigned and readable by any holder of the hub's token), CPU in cores since that series' previous read, resident memory in bytes (summed RSS counts shared pages twice; noted on the page, not corrected).
  - **Linux:** `/proc/stat`, `/proc/meminfo` (used = `MemTotal - MemAvailable`), `/proc/loadavg`, `/proc/<pid>/stat` (comm parsed up to the last `)`, as it may hold spaces and parens; times in USER_HZ 100; a process's CPU counts its reaped children through `cutime+cstime`, so a `go test` worker that exits between reads is counted to its parent), `/proc/<pid>/cwd`.
  - **macOS** (no cgo): `ps -axS -o pid,ppid,time,rss,comm` (`-S` sums exited children into their parent; CPU from the diff of cumulative `time`, never `pcpu`, a decaying average); box CPU is the sum of those diffs (`shortcut: kernel time isn't counted, upgrade with a cgo host_statistics read if it misleads`); memory used = (wired + anonymous − purgeable + occupied by compressor) × the page size `vm_stat` prints in its header; total `sysctl hw.memsize`; swap `sysctl vm.swapusage`; working directories from one `lsof -a -d cwd -Fpn -u <the daemon's uid>` (exit 1 from a vanished pid is tolerated).
  - **Windows:** nothing; the box shows `no stats`.
  - Two series keep their own previous read: the minute's and the live one's. A live read diffs against a previous live read at least 5 s old, else serves that read again.
- **Attribution,** named as usage names agents, so the history joins the agents table without translation. A process's working directory is matched as `internal/usage/match.go` matches a transcript's cwd: the newest node on this host that isn't done whose worktree is the directory or contains it on a path-separator boundary (`bluke-002.app` never takes `bluke-002.app2`'s); else a routine instance by `[runner] checkouts` (`routine.<kind>.<host>-…`, as `routine` there); else, for a Claude Code process (comm `claude`, or `node` whose args name Claude Code's CLI), `session.<host>.<id>` from `<claude config dir>/sessions/<pid>.json`'s `sessionId`, else `repo:<repo>` by `repoOf(cwd)`; else its nearest ancestor's agent; else `other`. The node list is read once a minute and reused by live reads; `repoOf` is cached per directory. Executors and engineers keep their own path; the page folds them under their lead where it folds lanes.
- **History, every minute:** `bmusage.host.<host>` on `bm-usage`, `Nats-Msg-Id` `<host>/host/<at>`, `{at, cores, cpu, load1, mem_used, mem_total, swap_used, pressure: {cpu, mem, io} | null (each some avg60), mac_pressure | null, agents: {<agent>: {cpu, rss}}}`, `other` = the box's CPU and memory less the agents'. A failed publish is dropped, as quota's is. About 500 bytes a message: some 25 MB per box over the stream's 30 days.
- **Live, on request:** each daemon answers core-NATS requests on `bmhosts.load.<host>` (beside `bmhosts.approve.<host>` and `bmhosts.join.<id>`; no stream stores it) with the live read: the history's fields plus avg10, the top 15 processes by CPU and the top 15 by memory (merged), each with its agent, and per-agent totals (processes, CPU, memory). Nothing is read live while no one asks.
- **`GET /api/hosts?since=1h|6h|24h|7d`:** each box's minute history in the window from `bm-usage`, cached 30 s like `/api/usage`; with `live=1` (not cached) also each box's live answer, asked in parallel of this box and every host with history in the last 3 minutes, 2 s in all. A box that doesn't answer (asleep, or an older release: no responders) is `"live": null`.
- **The hosts tab** (`#/hosts`), one card per box:
  - a head line `k16 · CPU wait 11% · mem wait 0% · io wait 2% · load 25.5/16 · 11/29 GB`, uncoloured (120 colours it); macOS shows its pressure level in words;
  - contention over the window (CPU, memory and I/O wait as lines; macOS load per core);
  - CPU and memory each stacked as agents (one series per lead tree, its hue) under other (grey);
  - the live process table: an agent's rows carry a chip in its lead tree's hue (the events drawer's chips), routine rows a grey `routine` chip, session rows a grey `session <id8>` chip linking its page, `repo:` rows a grey `<repo>` chip, other rows muted with none; under it, per-agent totals folded by lead tree (`bluke-002 · 3 procs · 5.2 GB · 2.1 cores`). A box with no live answer shows `no live stats` over its history.
  - The tab polls `live=1` every 10 s while it is the tab shown and visible, every 60 s when no box's CPU is over half its cores, and not at all on another tab or while hidden.
- **The agents table** gains `cpu` (mean cores over the window's minutes the agent appears in) and `peak mem` (the largest per-minute sum), sortable on the tokens tab.

As built: `/api/hosts` serves each box's minutes as means over buckets of the window's step (a minute on `1h`, 10 minutes on `6h`, 30 on `24h`, 3 hours on `7d`), so `7d` isn't 10,080 points a box; the agents table's `cpu` and `peak mem` are computed from the minutes themselves. A box with no minutes (a release before 117, or Windows) gets its card from its `bm-hosts` presence entry, is asked live too, and reads `no live stats` (`no stats` when presence says Windows). A live read with no live read at least 5 s old diffs against the minute series' read instead. A child's time read before it exits is subtracted from its parent's when the parent reaps it, so it is never counted twice.

Out: thresholds, colours, the KPI tile, the timeline strip and the `heavy` flag (120); alerts or pushes; killing or pausing processes; per-process history; GPU, disk space, network; Windows.

Judge: opus, because the attribution must agree with usage's matching on two OSes and the page joins three data sources.

## What you'll see
A `hosts` tab with a card per box: what it's waiting on, its load and memory, CPU and memory split into each lead tree's share and the rest, and the live processes, agents' in their lead's colour and everything else grey. The agents table's `cpu` and `peak mem` columns. A box on an older release reads `no live stats` and keeps working.

## Verification
- Unit tests on fixtures: the Linux parsers (`/proc/stat`, `meminfo`, `loadavg`, `pressure`, a per-pid `stat` whose comm holds `) (`, a missing `pressure`), the macOS parsers (`ps -S`, `vm_stat` with a 16 KB page, `sysctl`, `lsof` with a vanished pid), and the attribution (a worktree, a sibling with a longer name, a done node's reused worktree, a routine checkout, a session file, a `repo:` fallback, an ancestor's agent, other).
- `/api/hosts` against `meshtest` with seeded history and the test's own responder, including a host that never answers.
- The browser check: the seeded serve's own daemon is the live responder (box `seed`, the dev box's real processes); the seeder publishes two other boxes' minute history only. `scripts/check.mjs` adds `hosts=` (cards drawn) and `procs=` (process rows shown) on `#/hosts` and exits 1 when either is 0; on today's seed both fail. Screenshots at 390x844, 1000x800 and 1600x1000.
- On k16, never a second `bmesh serve` and no test touching the hub (the manual's Testing rules): a command under `scripts/` reads the real box and the real `bm-nodes` read-only, and prints one live answer; Shown quotes the running leads it attributed.

## Shown
- A hosts tab with a card per box, its waits, load, memory, stacked charts and live processes: `CHECK_COLOR_SCHEME=light node scripts/check.mjs "<seed>/#/hosts" 390 844` -> `bmesh ok hosts=3 procs=23 light 390 844 exit 0` (also 1000x800 and 1600x1000, dark and light, all exit 0); `internal/web/js/hosts.js`.
- An agent's processes in its lead tree's hue, everything else grey: the seeded serve runs in app-0042's worktree, and `/api/hosts?live=1`'s totals read `['app-0042', 'other']`; the 390x844 screenshot shows `bmesh` with an `app-0042` chip and `app-0042 · 1 proc · 60 MB · 0.0 cores` under the table.
- The agents table's `cpu` and `peak mem`: `curl /api/usage?since=6h` on the seed -> `app-0045 8.1 4559954498`; `TestHostsHistoryLiveAndAgentColumns` (a mean of 1.75 cores and a 3 GB peak across two boxes).
- A box on an older release reads `no live stats`: `TestHostsHistoryLiveAndAgentColumns`'s `win`, with a presence entry and no minutes, gets a card (`os` `windows`, no points, live null), as do `mac` (recent, no responder) and `old` (not recent), all within 2 s; `internal/web/js/hosts.js` `boxCard` reads `no live stats` for a box with nothing, `no stats` on Windows. On the seed, box-a and mac-b answer no request: `/api/hosts?live=1` -> `box-a 37 14.33 None`, and the 1600x1000 screenshot shows `no live stats` on both.
- The Linux and macOS parsers and the attribution on fixtures: `TestParseProcStat`, `TestParseMeminfoLoadavgPSI`, `TestParsePidStatCommWithParens` (comm `a) (b`), `TestReadLinuxMissingPressureIsNull`, `TestParseDarwin` (`ps -S` comm with spaces, `vm_stat` at 16384-byte pages, `sysctl`, `lsof` with a vanished pid), `TestDarwinSampleSumsProcesses`, `TestProcCPUSubtractsReapedChildren`, `TestAttribution` (worktree, `bluke-002.app2` beside `bluke-002.app`, routine checkout, session file and a stale one, `repo:` fallback, ancestor, other), `TestLiveNodesSkipsDoneAndOtherHosts` (a done node's reused worktree).
- `/api/hosts` against meshtest with seeded history, the test's own responder and a host that never answers: `TestHostsHistoryLiveAndAgentColumns`; the daemon's minute publish and responder: `TestHostStatAnswersAndPublishes`.
- `check.mjs`'s `hosts=` and `procs=` on `#/hosts`: `scripts/check.mjs` `RULES.hosts`; before this change the seed had no hosts tab, so both were 0.
- On k16, read-only: `go run ./scripts/hostload` -> `{'pid': 3554318, 'ppid': 3554306, 'comm': 'qemu-system-x86', 'cpu': 1.2, 'rss': 3913732096, 'agent': 'bluke-007'}`, `{'pid': 3529718, 'ppid': 2009, 'comm': 'claude', 'cpu': 0.01, 'rss': 344506368, 'agent': 'buildermesh-117'}`, `{'pid': 3529923, 'ppid': 2009, 'comm': 'claude', 'cpu': 0.02, 'rss': 332955648, 'agent': 'buildermesh-119'}` (three running leads, the emulator its lead's).
- Easy to miss: a missing PSI file is `null`, never 0: `TestReadLinuxMissingPressureIsNull`.
- Not shown: the Mac's card on a real Mac: only fixtures cover macOS before a release carries 117 (the operator's step below).

## Risks and the alternative considered
- A shared daemon counts for the agent whose worktree it started in, or as other: a Gradle daemon reparented to `systemd --user` keeps its starter's cwd; nix builds under root's `nix-daemon` are other; 119's one emulator shared by several leads is its starter's.
- The alternative was putting the sample in presence: it is signed and kept one deep, so it gives no history, and every release that reads presence would carry it.

## Upgrading
nothing

## Needs from the operator
- [ricks-macbook] once a release carries 117, open the map's hosts tab and check the Mac's card shows its memory and its agents' processes (only fixtures cover macOS before then).
