---
id: 121
title: bmesh land puts its step, the command it runs and that command's newest output line, each job's state and host, and how each run ended on the mesh, and readers show a landing once per change
kind: feature
size: M
judge: opus
status: accepted
lens: eng
proposed_by: 47745d6c-4585-4a15-b7a7-19807a4abecf
proposed_on: 2026-11-24
---

## Opportunity
A landing tells the mesh one phrase a minute, so the operator can't tell a slow landing from a stuck one. Specs 079 and 080 both assumed that this phrase, refreshed each minute and copied into the lead's state, says where a landing is. Bluke shows it doesn't.

- **What bluke-003's two landings said** (2026-11-24 at 03:38 and 04:09Z, read back from `bm.>`). Each `land` event read `landing` for the first minute. Then it read `running the inline kinds on t:<tree8>`, unchanged for 12 minutes and then 11. Meanwhile Bluke's verify ran Gradle, booted an emulator and printed 19 `PASS` lines, and none of it reached the mesh.
  - In the first landing, `land` used up the daemon's `--wait 10m` during verify, queued the review and exited 5. The daemon ran it again at 03:50:30, and the only sign was the text going back to `landing`.
  - While the review ran, the text was `waiting for job 3XANly81l6K4RUxIThBTcp (review)`. Spec 080 and the manual (`bmesh ready`) promise `waiting for job <id> (review, running on <host>)`, but `land` stores only the id and kind (`pause`, `cmd_land.go:326`).
- **Asking a model costs tokens and gets a guess.** At 04:16Z the operator asked bluke-003's thread "how long does landing take, is it stuck". The lead was at 579k context. It spent 3 model calls inspecting processes (1.74M tokens read, `bmesh usage bluke-003 --calls`). It answered that an Opus review of 10 to 20 minutes came next, and to expect `main` in 20 to 30 minutes.
  - `land` had already decided at its start that no review was due, because the branch carried one. The landed commit's trailer reads `review fail t:ca2ec50d m:claude-opus-5-5 at:c9e97e5a; verify ok t:4208beec`.
  - It landed at 04:20:59, 4 minutes after that answer.
  - Asking through the mesh is worse: a `bmesh send` to a landing lead withdraws its request (`bmesh ready`, daemon step 2).
- **Readers repeat each heartbeat.** `watch` printed 32 `land` lines for those two landings, only 6 of them distinct, into the coordinator's context. The map's recent-events drawer holds 30 rows (`RECENT`, `map.js:15`), so one landing fills it with identical `land` rows within half an hour, and the lead page's timeline card fills the same way.

Spec 122 draws this on the map. This spec makes `land` publish it, and makes today's readers show each change once.

## Proposal
In:

- **The `land` event gains a `land` object** beside `text`: `mesh.Event.Land`, a type in `internal/mesh`, so that `internal/web` and `cmd/bmesh` decode it alike. Its fields are `{"run", "step", "since", "cmd", "jobs", "end"}`.
  - `run` is when this `land` process started; `since` is when `step` began.
  - `step` is one of:
    - `checks`: steps 1–5 before the lease;
    - `kinds`: step 6;
    - `jobs`: steps 7–8;
    - `main`: step 9;
    - `lease`: step 10;
    - `rebase`: under the lease, steps 1–3, including the wait for the main checkout there;
    - `results`: step 4;
    - `push`: steps 5–8.
  - **Steps repeat in the order `land` takes them:**
    - after a jobs wait, it starts again at `checks`;
    - a review queued again after the rebase goes back to `checks`;
    - a refused push goes back to `rebase` (`main moved; again from the fetch`).
  - `cmd` is present while an inline kind's command or a `[setup]` runs. Its fields:
    - `kind`: the kind's name, or `setup`;
    - `command`: the command cut at 200 characters, sent only on the event the command starts in;
    - `since`: when it started;
    - `last` and `last_at`: its newest non-empty output line, cut at 160, and when that line was printed;
    - `lines`: how many lines it has printed so far.

    How the output reaches `land`:
    - a command kind's output is teed as it is captured: `instance.Job` gains a writer that the command instance also writes to, and the result still records the whole output;
    - a setup's output comes through `setup.Options.Out`, which already exists and which `runSetup` doesn't pass today;
    - a model kind run inline names `claude -p` and has no `last`;
    - while an inline kind waits for its slot (119), `cmd` carries `wait: {slot, holder, place}` instead of `last`.
  - `jobs` is present while `land` waits for replies. Each entry has the job's `id`, `kind`, `state` (`waiting`, `held` or `running`) and `host`. These are read again (`KindJobs`) whenever `land` publishes.
  - `end` is on the run's last event, published as `land` exits. It reads `landed <sha8>`, or else `land`'s error line: a refusal, `timed out waiting for job <id> (review)` or `interrupted`.
- **`text` changes only when what `land` is doing changes:**
  - a step;
  - an inline kind starting;
  - a job's state or host;
  - the slot `land` waits for.

  It reads, by step:
  - `checks`: `checking`;
  - `kinds`: `running verify on t:<tree8>`, or `waiting for the emulator slot for verify`;
  - `jobs`: `review: on the branch already (fail at t:ca2ec50d)` or `review: none (size S)` when no job is waited on, else `waiting for job <id> (review, running on k16)`;
  - `main`: `waiting for main in <path> to be pushed` (as under the lease);
  - `lease`: `waiting for land.<repo> (held by <holder>)`;
  - `rebase`: `rebasing onto <sha8>`;
  - `results`: `running verify on the rebased tree t:<tree8>`;
  - `push`: `pushing <sha8>`.

  The slot's holder and place, and the newest output line, never go in `text`. So the daemon writes the lead's `landing` state, and publishes its `landing` event, only on these changes.
- **When it publishes:** at once whenever `text` changes, again at the end, and otherwise once a minute, as now. The minute's event carries the newest `cmd.last` and each job's `state`. An event stays under ~500 bytes, against ~200 today.
- **`mesh-status`'s landing line** reads the lead's newest `land` event (`mesh.Last`).
  - It uses the event only when its `run` is no earlier than the request's `at` and it has no `end`. The line then gains the step's age and the newest line: `bluke-003 landing 10m: running verify on t:4208beec, 9m50s in: PASS switch to android (40s ago)`.
  - Otherwise it reads `landing 12m: last run ended 20s ago: timed out waiting for job 3XANly81 (review)`.
  - `--json` is unchanged.
- **`ready`'s output** ends with `; bmesh mesh-status shows where it is`, so a lead asked about its landing reads the line instead of inspecting processes.
- **Readers show each change once.** `watch`, the map's recent-events drawer and the lead page's timeline card show a node's `land` event, and the daemon's `landing` event addressed to it, only when its text differs from the last such text shown for that node.

Out:
- the map's landing views (122);
- a queued review's model session (122 reads it from the usage stream);
- process trees (117's hosts tab);
- any change to what `land` does or how long it waits;
- live output for `bmesh routine run` and the daemon's command jobs (a `soak-darwin` runs up to 100 minutes the same way). They have no reader, and giving them one is new surface. `routine run` passes no writer.

Judge: opus, because the event spans land's passes before and under the lease, the inline run's capture, and five readers (the daemon's `stepWithdrawn`, `mesh-status`, `watch`, the drawer and the lead page).

## What you'll see
- `bmesh mesh-status` during a daemon's landing names the step, how long it has run, and the command's newest line or the job's state and host. Once a run has ended, it names how that run ended.
- `bmesh watch` and the map's recent-events drawer show one `land` line per change instead of one a minute.
- `bmesh ready` points the lead at `bmesh mesh-status`.
- The stream carries, for each land run, every step with its time and how the run ended, ready for the map (122).

## Verification
- **`cmd/bmesh` tests through `land`'s entry point:**
  - a land whose fake inline kind prints three lines publishes events whose `step`s come in the order `land` took them, each `since` no earlier than the one before;
  - after a jobs wait it shows `checks` again, and a refused push shows `rebase` again;
  - `cmd.last` is the newest line printed before the heartbeat, with its `last_at`, and the heartbeat interval is a package variable the test shortens;
  - a queued review's `jobs` entry carries `state` and `host`, and its text names them;
  - the last event's `end` reads `landed <sha8>`, and a refused land and a timed-out land each end with their own line.
- **A `watch` test:** a `land` event, a daemon `landing` event with the same text, and a third with new text print two lines.
- **`mesh-status` tests:**
  - a fresh run's line carries the step's age and the newest line;
  - an event from before the request's `at` is not used;
  - an ended run's line reads `last run ended`.
- **The map's drawer and the lead page's timeline card:** on the seed, a landing lead's repeated `land` events show once. `check.mjs` on the overview and on `#/lead/app-0041` adds `landDup=`, the count of `land` rows whose text equals the previous one for their node, and exits 1 when it is not 0. The seed publishes two `land` events with the same text.

## Shown

## Risks and the alternative considered
- **The stream grows a little.** Measured:
  - the stream holds 247 `land` events among ~3,500 (1.3 MB) over 30 days;
  - the map's timeline reads that stream whole every 30 seconds, and a cold read of the 7-day window took 1.6, 1.3 and 6.0 s over three runs;
  - this spec adds a handful of events per landing, one per change of `text`, and grows each event from ~200 to ~500 bytes, so about half a megabyte more a month at today's 87 readies.

  A faster refresh than the minute would have multiplied that, and is left out.
- **The newest output line goes on the mesh.** Verify's whole output is already in `bm-results`, for the same readers.
- **119 (accepted) will build the slot wait.** Its `waiting for <slot> (held by <holder>, <priority>); #N` status becomes `cmd.wait` and the text above, so a change in place doesn't rewrite the lead's state.
- **Alternative: put the newest line in `text` alone.** Every new line would then be a daemon state write, a `landing` event and a `watch` line, and there would still be no step, job state or ending to show.

## Upgrading
nothing

## Needs from the operator
- [k16] after the release carrying 121, during the next landing, run `bmesh mesh-status` and check that its landing line names the step, its age and the newest line (the daemon runs the installed release, so no build can show this before then)
