---
id: 036
title: A routine kind can say it needs a Mac, and only a Mac takes its jobs
kind: feature
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
built_by: ae866914-0010-40e5-b884-1c7dd2b2cf00
host: k16
family: buildermesh-build-3
---

## North star

The operator's words: "All I really wanted was that some execution work that needs a Mac, k16 can reach out and get results from." A lead on k16 queues Mac-only work the way it queues any routine, starting with a macOS build of a game. The Mac runs it unattended, and the result comes back to the requester's inbox. Nobody names a box, opens ssh, approves a hold or touches the roster. Checks of the party system in Safari or the iOS Simulator ride on the same key once that repo writes the kind (Out says what such a kind needs).

## Opportunity

Routines already carry work between boxes. A job goes on the hub, a daemon with routines on pulls it, runs it in its own checkout at the job's commit, records the result by tree and replies. Both boxes run routines (`bmesh hosts` prints `routines on` for k16 and ricks-macbook). One thing is missing: a job can't say it needs a Mac. A kind's inbox has one consumer on the hub with no box in it (`inbox-routine--<kind>`, `internal/mesh/names.go:116`). Whichever daemon fetches first takes the job, so a macOS build is as likely to land on k16. There it fails, or worse, passes as a Linux build and is recorded as the Mac kind's result for that tree.

The work is concrete. `eggnogg-rework` is a Rust and SDL3 rewrite of a macOS game, and its registry row already names the Mac as `ux_host` for "the running game on the Mac". `party-buzzer` is the browser and phone party system.

## Proposal

A new kind key, `os`, defaults to `""`, which means any box. Otherwise it holds the word `bmesh hosts` already prints for a box: `darwin`, `linux` or `windows`, Go's `GOOS`. `kind.Parse` refuses any other value with `kind <k>: os "<v>": want darwin, linux or windows (macOS is darwin)`, and refuses `os` on an `inline` kind, which runs in the requester's own worktree and so can't move. The key states a fact about the work ("this build needs macOS"), true on any fleet.

```toml
# eggnogg-rework/.bmesh/routines/mac-build.toml
mode = "pooled"
os = "darwin"
deterministic = true
timeout = "40m"
command = "cd rust && ~/.cargo/bin/cargo build --release --quiet -p nogg-app && ~/.cargo/bin/cargo test --quiet -p nogg-app"
```

`pooled` keeps one checkout per instance, and the `git clean -fd` before each job leaves ignored files, so `target/` stays warm. A `fresh` build would rebuild SDL3 from source every time. `deterministic` answers a repeat job for the same tree from its record, so a lead that re-queues after a timeout costs nothing (`--rerun` forces a build). `--quiet` drops cargo's progress lines, so errors usually fit in the first 64 KiB a reply keeps.

What changes:

1. **A daemon takes only its own OS's kinds.** In `rescan` (`internal/daemon/routines.go:252-257`), a kind whose `os` on the repo's main is set and isn't `runtime.GOOS` is left out of `want`. That box starts no feed, writes no consumer settings and starts no instances for it. So k16 never fetches a `mac-build` job, and only the Mac's daemon configures that kind's consumer and runs its instances. Every other kind behaves as today.

   Where a kind runs is read from main, as its feeds already are. A job, though, runs the kind as defined at its commit (`handle`, `routines_job.go:288`), so a branch could add `os` to a kind both boxes run: a lead gives party-buzzer's `surface` a Safari command and `os = "darwin"`. Two checks keep that job off the wrong box:
   - `routine queue` checks it when the kind's definition at the job's commit (`kind.LoadAt(top, commit, kind)`, not the working copy) sets an `os`. It compares that with the kind on the base ref `priorReview` already uses (`cmd_routine.go:701`: `origin/main` when the repo has an origin, else `main`). It refuses (exit 3) with `bmesh: surface runs on darwin at <commit8> but anywhere on main, so a job could reach a linux box: land the os change first, or give it a kind name of its own` when main's `os` differs, `bmesh: <kind> is not on main: land it first` when the kind isn't there (it has no feed anywhere, README:290), and a refusal naming the ref when main can't be read. A commit with no `os`, or no kind file, skips the check: it runs anywhere, the box that takes it runs it, and it costs no extra git call. The check sits after the record lookup, next to the push refusal (`cmd_routine.go:337-357`), since a recorded answer queues nothing.
   - In `handle`, right after the commit's definition loads and before the record lookup and the slot lease, a definition whose `os` is set and isn't this box's is answered `ok=false` with `kind <k> at <commit8> runs on darwin; this box is linux` and acked. It is never run or recorded. This is the backstop for an old CLI, and for main moving between queue and fetch. It answers rather than NAKing, because a delayed NAK keeps holding one of the consumer's few `MaxAckPending` places and would stall the kind for every box.
2. **`routine queue` says where the job runs.** It already loads the kind from the requester's worktree (`cwdKinds`, `cmd/bmesh/cmd_routine.go:303`). When that kind's `os` isn't this box's, and no recorded result answers the job:
   - it refuses (exit 3) when the commit is on no `origin` branch (`git branch -r --contains <commit> --list 'origin/*'` prints nothing): `bmesh: push first: <kind> runs on darwin, which fetches the commit from origin`. Today that mistake shows up only as `not a commit` from the far box, after a fetch;
   - it prints `queued <id> (runs on darwin)`.
3. **`routine run` refuses an `os` kind on another OS**, `--here` included (exit 3): `bmesh: mac-build runs on darwin; this box is linux: queue it`. Without it, `routine run mac-build --here` on k16 (`cmd_routine.go:537`) would build for Linux and record `mac-build ok` for the tree (`:633-636`). Every later job for that tree, the Mac's included, would then be answered from that false record.
4. **`routine status` gains a `HOST` column**, the instance node's `host`, so it shows at a glance which box did the work, and `status --json` carries `host` in each instance object.
5. **`routine wait`'s timeout says what to do**: `timed out waiting for <id>; the job may still be queued or running, and its reply stays in the stream for 30 days: run bmesh routine wait <id> again rather than queueing it twice`.
6. **Docs.** The README's Kinds table gains `os`. Jobs gains the queue lines, the push refusal, the dropped kinds and one rule: a kind name is one inbox across every repo and box, so give an `os` kind a name no other OS's kind uses (`mac-build`, not `build`). 037 makes shared names safe. Several boxes says plainly what `secure` covers: roster ops (`level` holds and the approve key), not routines. A box with routines on runs any pushed commit's kind command for any mesh member. Routines are on by default in `config.toml` (`[runner] checkouts`, default `~/.bmesh/checkouts`). `bmesh init --hosts` asks, with "no" the default on a secure box, so a secure Mac that is to run Mac kinds answers yes there. README:290's list of `ok=false` answers gains `handle`'s `runs on darwin; this box is linux`, and its queue sentences gain queue's main-versus-commit refusal (exit 3) beside the push refusal. The executor skill's description (`skills/executor/SKILL.md:3`) and push line (`:22`) names an `os` kind for another OS as one that always runs elsewhere, and its never-push rule (`:39`) lets an executor push its own branch for one. The lead skill tells a lead to push before queueing one, and that `bmesh land` needs the kind's result at the rebased tree, so a landing whose rebase changed the code waits for the Mac to be awake, and the rebased commit must be pushed (`git push --force-with-lease origin HEAD`) before the kind is queued again. `examples/routines/` gains a commented `os` line.

In: the `os` key and its two refusals, queue's main-versus-commit `os` refusal and `handle`'s backstop answer, the rescan drop, the queue, run, status and wait changes, the README, the lead and executor skill lines, the example, and tests for each.

Out:
- **Making shared kind names safe across boxes.** Today a box with no checkout of a repo answers that repo's jobs `kind <k> is not on <repo>'s main`. Concurrency is whichever box wrote the consumer last, and both boxes name their pooled instances alike. These are real bugs in `review` and `verify` across two boxes, and they are 037's. 036 doesn't need them fixed: a Mac kind has one consumer, the Mac.
- **Pinning to a named host.** It would tie a repo file to one fleet. With one Mac, `os = "darwin"` is the pin.
- **Returning files.** A screenshot or a `.app` stays on the Mac. A build answers with its log and exit status. A check that needs eyes answers in text: a model kind on the Mac that looks at its own screenshots and ends with `VERDICT: ok|fail`. Such a kind needs `allow` entries for the commands it runs (`Bash(xcrun simctl:*)`, `Bash(screencapture:*)`), a logged-in GUI session, and Screen Recording permission for the daemon's process, which macOS may reset when `bmesh update` rebuilds the binary. That kind is party-buzzer's to write, and nothing here tests it.
- **Leads on the Mac from k16**, ssh, the roster ops and any cut to the hosts layer.

Judge: opus, because it touches the daemon's job loop and the queue path two boxes share, though the shape is fixed here.

## What you'll see

On k16, from a lead or executor in `eggnogg-rework` (any node: `queue` and `wait` need one, so a plain session passes `--node <path>`):

```
$ git push origin HEAD
$ bmesh routine queue mac-build --go
queued 7f3a9c21 (runs on darwin)
$ bmesh routine wait 7f3a9c21
(cargo's warnings and the test summary, exit 0)
```

`bmesh routine status mac-build` lists `routine.mac-build.cmd-1` with `HOST ricks-macbook`, and k16 has no `mac-build` instances. With the Mac's lid shut the job waits on the hub, `status` shows it `waiting`, and it runs when the Mac wakes. A queue from an unpushed branch is refused before anything waits. A second queue of the same tree is answered from the record at once.

## Verification

The verify kind (`go test`, `go vet`, `gofmt`) with new tests:
- `internal/kind`: `os` is accepted, an unknown value is refused with the `macOS is darwin` hint, and `os` on an inline kind is refused.
- `internal/daemon`: two daemons on one `meshtest.Start` server with different `Host` values and a fake GOOS. A darwin kind gets a feed and instances on the darwin daemon only, and its job is run there and replied. A job whose commit gives the kind another box's `os`, while main gives none, is answered `ok=false` and acked by the wrong box, never run or recorded there.
- `cmd/bmesh`: queue's refusals of a commit `os` that differs from main's or a kind not on main, skipped for a commit without `os`, and placed after the record lookup, queue's push refusal and its ordering after the record lookup, the `runs on` line, run's refusal with and without `--here`, status's `HOST`, and wait's timeout text.

On the fleet, once both boxes run the new bmesh: land `mac-build.toml` on `eggnogg-rework`'s main, queue it from a k16 worktree, and read the reply and `status`'s `HOST ricks-macbook`. That check proves the north star, and only the operator's boxes can run it.

## Risks and the alternative considered

- **Old binaries.** `kind.LoadDir` fails a whole repo on one unknown key. An old daemon then freezes every kind of a repo that gained an `os` kind, and an old CLI fails `routine queue`, `list`, `run` and `bmesh land` in that repo. So both boxes update before any repo lands an `os` kind. k16 updates itself within minutes of the merge, and the Mac needs `bmesh update` (Needs from the operator).
- **A name shared with another OS's kind.** The README rule above covers it until 037.
- **macOS sleep.** A job waits while the Mac sleeps, which is the intended behaviour. A Mac that sleeps through a run longer than the kind's `AckWait` misses its heartbeats and is handed the job again on waking. For `mac-build` (pooled, pool 1, deterministic) the redelivery waits in the pool behind the running job and is answered from that job's record: one duplicate reply, no second build. A non-deterministic Mac kind, such as a future model check, would run twice. A guard against that is left for when one does.

Alternatives:
- **A per-OS inbox** (`routine.<kind>.darwin`). No box would ever see another OS's jobs. But it needs a second subject grammar, consumers per lane, and `go` and `status` per lane. Leaving the kind out at rescan gets the same result with no new subject.
- **The requester picks a box from presence.** The README holds that presence is never the source of a decision, and a box asleep at queue time would strand the job.
- **ssh from k16 to the Mac.** It needs Mac keys any k16 agent can use for anything, and it bypasses results, replies and the map.
- **A roster op that runs a command on a named box.** It needs an `accept` entry, a target in every call and approval on a secure Mac. It duplicates routines.

## Needs from the operator

- After the merge, run `bmesh update` on the Mac, before any repo lands an `os` kind. k16 updates itself.
- `eggnogg-rework`'s registry row is `mode = "dormant"`, and `bmesh build` dispatches leads only in `ship` rows. For a lead there to queue `mac-build`, set the row to `ship` in `repos.toml` on the admin box (the Mac) and `bmesh repos publish`. A session with `--node` can queue without it.
- Land a kind with `os = "darwin"` in each repo with Mac-only work, starting with `eggnogg-rework/.bmesh/routines/mac-build.toml` (above). Those commits are the repos' own.
