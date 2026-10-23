---
id: 080
title: A lead ends at bmesh ready, and its box's daemon lands the change, waking the lead only when land needs a model
kind: token-cost
size: L
judge: opus
status: building
lens: eng
proposed_by: c3029b53-10b6-4fab-83cb-465887cb0b73
proposed_on: 2026-10-18
built_by: 7397e410-ccdf-49a1-a046-3fa4c719dff7
host: k16
family: buildermesh-build-15
branch: bmesh/7397e410-ccdf-49a1-a046-3fa4c719dff7
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
  - It refuses in these cases:
    - the node isn't a lead (a fixer, an executor or an engineer);
    - the node's host isn't this box, the node has no thread, or its registered worktree isn't this checkout;
    - the worktree is dirty, or the spec isn't `building`;
    - there is unread operator mail, as land refuses;
    - an entry already exists: `already ready; bmesh ready --cancel first`;
    - this box's daemon runs a release older than this one, read from `bm.daemon.<host>.started`: `this box's daemon doesn't land for leads; run bmesh land`.
  - While held, it is allowed, and the daemon waits for the release.
  - It writes `bm-ready` `<path>` → `{spec, head, size, text, at, phase}`. The bucket is created on demand, like `bm-holds`.
  - It publishes `ready`, which sets a new state, `landing`, and prints `ready: <host>'s daemon lands <sha8>; it wakes you only if land needs you`. The lead then ends its turn.
  - `bmesh ready --cancel` marks the entry cancelled, waits until the daemon has cancelled any running step, and prints the outcome. A step that had already pushed still gets `done` and the final wake.
- **The lead's own daemon lands it.** That is the daemon of the box the node runs on, acting on the `ready` event and on its poll tick (beside the wake pass).
  - **Claim.** Each step holds a `ready.<path>` lease in `bm-leases` and renews it while it runs. A pass that can't take the lease leaves that lead alone.
    - Before an update re-execs the daemon, its drain cancels running steps. Land cleans up on cancel, on its own context.
    - A dead daemon's lease expires.
  - **Derive, then act** (spec 059's rule: from what exists, never from what an earlier pass meant to do). Check in this order:
    - **Landed:** the spec on `origin/main` reads `done` and its `merged` commit is an ancestor of the worktree's HEAD. Go to the done step. This covers a daemon that died after the push.
    - **Withdrawn:** the lead's state is neither `landing` nor `held` with an entry (it acted itself), the entry is cancelled, or the node was stopped. Delete the entry.
    - **Held:** `bm-holds` names the lead. The daemon reads holds itself, before each step and after any exit 3.
    - **Otherwise,** run land.
  - **Running a step.** Each step runs this binary with `--config` (as `execFix` passes it), in the node's registered worktree, which must exist.
  - **The land step** runs `bmesh --node <path> land [--size S] --wait 10m`. It starts only while HEAD is the request's head, or land's own rebase of it (read through `rebasedFrom`).
    - **It landed (exit 0):** run `bmesh --node <path> done --commit <HEAD> --text "<text>; landed <sha8>, origin/main at <sha8> contains it"`. The `origin/main` commit comes from a fetch the daemon makes after the push, and `done` is idempotent. The entry turns `landed` and stays until the final wake is delivered: `bmesh: landed <sha8>, spec NNN done; origin/main at <sha8> contains it. Write your closing line; nothing else to run.` A final wake that still fails after an hour is logged and dropped, since `done` is the record.
    - **It timed out (exit 5):** the entry stays and the next pass runs the step again. 079's land waits for the review job already out, never a second one.
    - **It was held:** run `bmesh --node <path> held`. That sets state `held`, and `watch` shows the lead reach its gate, as in spec 048. On the release, the daemon writes `landing` from `held` and goes on.
    - **Any other refusal** (a conflict, a failing verify or review, a message, scaffold drift): the entry turns `refused`, holding land's refusal and its last 40 lines (which carry the review's findings when it failed). The daemon wakes the lead at once, past `WakeEvery`: `bmesh: land refused and needs you: <refusal> … fix it, commit, then bmesh ready again`.
      - Once delivered, it deletes the entry and writes `working` with the refusal, so silence applies as it does to any working lead.
      - A thread that is running (`ErrRunning`) is retried each pass.
      - Only a send that fails for another reason for an hour publishes `blocked`.
  - **Its state writes.** Every state the daemon writes for the lead (`landing` after a release, `working` after a refusal) is a compare-and-set at the revision it read: a new `mesh` write. That way it never overwrites a `progress` the lead just sent. Each write also publishes a daemon event (`landing`, `land-refused`), as `silent` does, so the map follows.
- **The `landing` state.**
  - Transitions: `working → landing`, then `landing → held | done | working | blocked`, and `held → landing`.
  - Its text follows land's newest `land` event, which the daemon writes, e.g. `review job <id> running on <host>`.
  - It is never called silent, and it is not in the needs-you band or `[notify] push`. It gets a calm badge on the map, a count on the strip (`1 landing`), and its age in mesh-status.
- **Wakes.** The wake pass treats a lead with an entry, whether `landing` or `held`, as finished: only a `message` wakes it. A routine reply, the hold and the release don't.
  - A message first cancels the running step and waits for it to end, then deletes the entry, then wakes the lead. A message can change what landing needs (spec 044).
  - For a landing lead, this changes 048's "the release wakes it" into "the release resumes the daemon".
  - `bmesh node stop` cancels the step and deletes the entry, so a stopped thread stays stopped (spec 060). `node forget` deletes the entry, as it deletes holds.
- **Skills.**
  - The lead skill:
    - Section 7: after the blast-radius check, Needs from the operator and feedback, run `bmesh ready --text "<your done line>"` and end your turn. A wake carries the refusal, and the final wake asks for the closing line. `bmesh land` stays for landing in your own turn, or where `ready` refuses.
    - The Holds paragraph says a landing lead isn't woken by a hold.
    - One line: queue a long job and end your turn, since its reply wakes you; no repeated `routine wait`.
  - The build skill: for a landing lead, the release resumes the daemon. The mesh-status skill gains a `landing` group.
- **Docs.**
  - README: the `ready` command index row, plus Landing, wakes, silence and the state list.
  - Manual: a `bmesh ready` section with its refusals, the state list and transitions, and the land, `held` and wake sections.
  - Internals: `bm-ready` in the buckets table (created on demand), the `ready.<path>` lease, and the `ready`, `landing` and `land-refused` events.

Where it lives, the options weighed:
- **A mode of land in the lead's turn (079 alone).** Gets most of the tokens, but the session must outlive the wait, a hold costs turns, and a backgrounded land still brings a turn.
- **A routine kind `land`.** Routines run in their own worktree on whichever box of the pool takes the job. Land has to rebase the lead's branch in the lead's worktree, on the lead's box. Routine results are recorded by tree, and land isn't a check. A job would hold a routine slot through a review or a hold. Its reply would also go to the lead's inbox, which under today's wake rule wakes the lead.
- **The coordinator lands.** That is a model at full context doing the same steps.
- **A fresh session for refusals.** Ruled out: it costs 70-80k tokens to rebuild context.
- **The daemon.** Chosen: it already wakes leads, reads holds and runs `bmesh` as a subprocess, on the box that has the worktree.

Out: freeing a `[runner] max_leads` slot while a lead is landing (its session still holds its memory); batching landings; landing for executors or rounds.

Judge: opus, because it crosses the daemon, the state table, the wake rule, the map and three skills. The daemon must never act in a worktree whose lead is working, and it must converge across restarts.

## What you'll see
A lead ends with `ready: k16's daemon lands 3f2a9c1e; it wakes you only if land needs you`. `mesh-status` reads `buildermesh-080 landing 6m: review job … running on k16`. The phone gets the done push, `… landed 81d0b7a4, origin/main at 81d0b7a4 contains it`, and the thread ends with the lead's closing line. A conflict brings one wake that carries the refusal.

## Verification
The repo's verify, plus daemon tests on `meshtest.Start` with the runner fake. The land and done steps go through a test hook, as `StartFixer` does, so no binary is built. A `cmd/bmesh` test then runs `ready` and the step's commands end to end through `runCLI` over a bare origin. Cases:
- **Landing.** `ready` lands, the done event carries the text, and the final wake is sent once.
- **Restarts.** A daemon that restarts after the push runs `done` once and sends the final wake once. One that restarts mid-step never starts a second step while the claim is held.
- **Refusals.** A failing review or a conflict wakes the lead once with the refusal; the entry is gone and the state reads `working`. A running thread is retried. A send that keeps failing for another reason ends in `blocked` after an hour.
- **Holds.** A hold gives state `held` and runs no land and no wake. The unhold resumes landing, and no wake comes before the final one.
- **Withdrawal.** A message cancels the running step, then wakes the lead. The lead's own `progress` withdraws the request, and a daemon write never overwrites it. `--cancel` reports the outcome. `node stop` cancels the step, and `node forget` deletes the entry.
- **`ready` refusals.** A non-lead, another box, an older daemon, and a second `ready` all refuse; `ready` while held is accepted.
- **Silence.** A `landing` lead is never marked silent.

The map seed gains a `landing` lead whose badge `scripts/check.mjs` sees, with no band row.

## Risks and the alternative considered
- **The daemon and the lead acting at once.** Guarded three ways: the claim, cancelling the step before any wake, and compare-and-set state writes. A lead the operator types into withdraws the request with its first event.
- **A daemon that is down.** The request waits, and mesh-status shows its age.
- **The final wake.** It costs one warm call (about 190k tokens of context at the cache-read price). The alternative, no wake, leaves the thread's last word at `ready`, which is short of the done rule's closing line.

## Upgrading
nothing: no key, and `bm-ready` is created on demand. A box's own daemon lands its leads once that box runs this release. Until then, `bmesh ready` refuses there, and its leads land in their own turn with `bmesh land`.

## Needs from the operator
