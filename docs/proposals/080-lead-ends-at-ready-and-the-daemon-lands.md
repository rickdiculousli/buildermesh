---
id: 080
title: A lead ends at bmesh ready, and its box's daemon lands the change, waking the lead only when land needs a model
kind: token-cost
size: L
judge: opus
status: proposed
lens: eng
proposed_by: c3029b53-10b6-4fab-83cb-465887cb0b73
proposed_on: 2026-10-18
---

## Opportunity
Builds on 079. Even with that, the landing still runs in the lead's own turn:
- the backgrounded `land`, then the turn its exit brings;
- `bmesh done`;
- for a hold: `held`, the release's wake, then `land` again.

Each of these turns re-reads about 190k tokens of context. Over the same 40 leads as 079, ending each run of landing calls at one command saves 259 calls and 47M context tokens. That is 9.9% of all lead context tokens, against 6.9% for 079 alone, or about 6.5 calls per lead.

The landing also hangs on the lead's session. A T3 restart, a quota stop or a crashed session leaves a finished, reviewed change on its branch until someone notices.

Gas Town (github.com/steveyegge/gastown) splits this work: its workers end at `gt done`, a separate per-rig merge queue (the "Refinery") lands what they finished, and overseers carry the work to completion from persistent state. Here the session stays alive, because the cache is warm. Across 49 leads the prompt cache rewrote more than 40k tokens mid-thread only once, and the median gap between turns is 2 minutes, while a fresh session costs 70-80k tokens to rebuild. So a woken lead is the cheapest fixer, and only the waiting should move off its turn.

## Proposal
In:
- **`bmesh ready --text "<done line>" [--size S]`.**
  - Run from a lead's worktree that is committed and clean, with the spec `building`. Like land, it refuses while there is unread operator mail.
  - It writes `bm-ready` (a new KV bucket) `<path>` → `{spec, head, size, text, at}` and publishes `ready`, which sets a new state, `landing`.
  - It prints `ready: <host>'s daemon lands <sha8>; it wakes you only if land needs you`, and the lead ends its turn.
  - `bmesh ready --cancel` takes the request back, after any step that is running.
- **The lead's own daemon lands it.**
  - Which daemon: the daemon of the box the node runs on. It acts on the `ready` event and on each minute's settle pass.
  - How it decides: from what exists, never from what an earlier pass meant to do (059's rule), and at most one step per lead at a time.
  - The step: run `bmesh --node <path> land [--size S] --wait 10m` in the lead's worktree with this binary, as spec 059 runs `bmesh fix`. Before each step, the lead's state must read `landing`, and HEAD must be the request's head or the rebase land made of it.
  - **It landed:** run `bmesh --node <path> done --commit <landed> --text "<text>; landed <sha8>, origin/main at <sha8> contains it"`, delete the entry, and send one final wake: `bmesh: landed <sha8>, spec NNN done; origin/main at <sha8> contains it. Write your closing line; nothing else to run.`
  - **It timed out** (exit 5): the entry stays and the next pass runs the step again.
  - **A hold names the lead:** run `bmesh --node <path> held`, which sets state `held` as in spec 048, and run no land while held. On the release, set `landing` and go on. Neither the hold nor the release wakes the lead.
  - **Any other refusal** (a conflict, a failing verify or review, a message, scaffold drift): delete the entry and wake the lead at once, past `WakeEvery`, with land's refusal and its last 40 lines, which hold the review's findings when it failed: `bmesh: land refused and needs you: <refusal> … fix it, commit, then bmesh ready again`.
  - **A wake can't reach the thread:** publish `blocked` with the refusal, which pushes as any block does.
- **The `landing` state** works like `held`:
  - Its text follows land's newest `land` event, e.g. `review job <id> running on <host>`.
  - It is never called silent, never in the needs-you band or `[notify] push`. It gets a calm badge on the map, a count on the strip (`1 landing`), and its age in mesh-status.
  - Any event the lead sends itself withdraws the request.
  - The wake pass treats a `landing` lead as finished: only a `message` wakes it. That message also ends the request, as spec 044 has a message change what landing needs.
- **Skills and docs.**
  - The lead skill, section 7: after the blast-radius check, Needs from the operator and feedback, run `bmesh ready --text "<your done line>"` and end your turn. A wake carries the refusal, and the final wake asks for the closing line. `bmesh land` stays for landing in your own turn.
  - One more line in the lead skill: queue a long job and end your turn, since its reply wakes you; no repeated `routine wait`.
  - The manual, internals and README name `ready`, `bm-ready` and `landing`.

Where it lives, the options weighed:
- **A mode of land in the lead's turn (079 alone).** Gets most of the tokens, but the session must outlive the wait, a hold costs turns, and a backgrounded land still brings a turn.
- **A routine kind `land`.** Routines run in their own worktree on any box. A routine's reply lands in the lead's inbox, and that reply wakes the lead, which is exactly what this spec avoids. Routine results are recorded by tree, and land isn't a check. A job would also hold a routine slot through a review or a hold.
- **The coordinator lands.** That is a model at full context doing the same steps.
- **A fresh session for refusals.** Ruled out: it costs 70-80k tokens to rebuild context.
- **The daemon.** Chosen: it already wakes leads, reads holds and runs `bmesh` as a subprocess, on the box that has the worktree.

Out: freeing a `[runner] max_leads` slot while a lead is landing (its session still holds its memory); batching landings; landing for executors or rounds.

Judge: opus, because it crosses the daemon, the state table, the wake rule, the map and two skills, and the daemon must never act in a worktree whose lead is working.

## What you'll see
A lead ends with `ready: k16's daemon lands 3f2a9c1e; it wakes you only if land needs you`. `mesh-status` reads `buildermesh-080 landing 6m: review job … running on k16`. The phone gets the done push, `… landed 81d0b7a4, origin/main at 81d0b7a4 contains it`, and the thread ends with the lead's closing line. A conflict brings one wake that carries the refusal.

## Verification
The repo's verify, plus daemon tests on `meshtest.Start` with the runner and claude fakes in `testdata/`:
- **Landing.** `ready` lands, the done event carries the text, and the final wake is sent once.
- **Refusals.** A failing review or a conflict wakes the lead once with the refusal, and the entry is gone.
- **Holds.** A hold gives state `held` and runs no land and no wake. The unhold lands, and no wake comes before the final one.
- **Withdrawal.** A message to a `landing` lead ends the request once the running step finishes. The lead's own `progress` withdraws it too.
- **No doubles.** A daemon restart, or a second pass, never runs two steps for one lead.
- **Lost thread and silence.** A thread that is gone publishes `blocked`. A `landing` lead is never marked silent.

Also new `ready` refusal tests in `cmd/bmesh`, and the map seed gains a `landing` lead whose badge `scripts/check.mjs` sees, with no band row.

## Risks and the alternative considered
- **The daemon and the lead acting at once.** The state check before each step guards this: a lead the operator types into withdraws the request with its first event.
- **A daemon that is down.** The request waits, and mesh-status shows its age.
- **The final wake.** It costs one warm call (about 190k tokens of context at the cache-read price). The alternative, no wake, leaves the thread's last word at `ready`, which is short of the done rule's closing line.

## Upgrading
nothing: no key. A box's own daemon lands its leads once that box runs this release. Until then, its leads land in their own turn with `bmesh land`.

## Needs from the operator
