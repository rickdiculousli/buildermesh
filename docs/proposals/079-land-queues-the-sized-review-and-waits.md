---
id: 079
title: bmesh land queues the review the spec's size calls for, waits for it and for main, and stops only for what needs the lead
kind: token-cost
size: L
judge: opus
status: building
lens: eng
proposed_by: c3029b53-10b6-4fab-83cb-465887cb0b73
proposed_on: 2026-10-18
built_by: ad0476f3-d146-42fc-8288-f1044d1c4304
host: k16
family: buildermesh-build-14
branch: bmesh/ad0476f3-d146-42fc-8288-f1044d1c4304
---

## Opportunity
`bmesh land` rebases, verifies, carries the review and pushes in one command, but everything around it is the lead's own turns, and each turn re-reads a context of about 190k tokens. Over the 40 leads of specs 017-071 that reached a review or a land (k16 transcripts, scripts in `~/.cache/bmesh-scratch/analysis/r6eng/`), the stretch from the first review queue to the end is 1,139 model calls and 214M context tokens: 45% of lead context tokens are spent after the build. In that stretch, 138 runs of back-to-back calls do only landing steps: `routine queue review`, `routine wait`, `sleep`, `inbox`, `land`, `git fetch`/`push`, `done`. Collapsing each run to one call saves 185 calls and 33M tokens, 6.9% of all lead context tokens.

Land sends the lead back for several things it could finish alone:
- **A review the rebase invalidated.** Land prints the `queue review --again` line and stops, three times in these leads.
- **A `main` that moved twice.** Land retries once, then stops: both of 055's runs ended this way, at 357k tokens of context.
- **A main checkout with unpushed commits.** Land refuses, and the lead has to wait by hand.

Land also never queues the review itself. An M change whose lead forgot the review lands on verify alone, unnoticed. Spec 038 left this open, because land couldn't tell a dropped review from a fixed one by the row alone.

The first count for this round read three land outputs as "stale binary" refusals. They are the version hint every command prints on stderr (`k16 runs …; run bmesh update`): two of those runs landed and one hit a conflict. Land has no stale-binary refusal, so there is nothing to handle there.

## Proposal
In. Everything here happens before the lease unless it says otherwise. The code is `cmd/bmesh/cmd_land.go`, plus a `Commit` field on `mesh.QueuedJob`.
- **Verify first.** Land runs the inline kinds on HEAD. They are answered from the record when the lead already ran them, so a review never reads code that fails its tests.
- **The review, sized by the spec.** Land queues a review on HEAD only when all of these hold:
  - the repo has a `review` kind;
  - the branch has no review result. This is `priorReview`'s check, widened to the heads `rebasedFrom` returns, so a hand rebase (spec 038) carries its review instead of getting a second one;
  - the node has no unanswered `review` job for a commit of the branch. When it has one, land waits for that job instead. This covers a re-run after exit 5, and a lead that queued the review by hand.

  How the job is queued:
  - Effort: `medium` for size M, `high` for L, none for XS or S. This is bmesh's done rule, `internal/brief`'s `sizedText`.
  - `--size XS|S|M|L` states the size as built when the scope grew. A spec with no `size` and no `--size` refuses: `land needs --size: spec NNN has no size`.
  - The job goes on the kind's first model, with `Go` set and the text `base: <merge-base origin/main HEAD>`, as the lead skill queues it today.

  Land prints one of `review: queued <id> (medium, size M)`, `review: none (size S)`, `review: waiting for job <id>`, or `review: on the branch already (ok|fail at t:<tree8>)`. A failed review that was then fixed queues nothing, so the one-pass rule stands.
- **It waits for the node's jobs on HEAD.** Land checks `KindJobs` for each of the repo's non-inline kinds, keeping the jobs whose `From` is the node and whose `Commit` is HEAD. Jobs on an earlier commit are not waited for: land then requires what it requires today. Each wait is named once, e.g. `waiting for job <id> (review, running on <host>)`.
  - Land acks every reply it consumed, as `routine wait` does with `FetchMatch`, so no wake follows.
  - A failing reply refuses, as a failing result does today, and prints the reply's output (a review's findings), so the lead never needs a `routine wait`.
  - A reply that recorded nothing because its model run never answered (no result key, output starting `model run failed:`, spec 071's `Unjudged`) is queued once more. A second one, or any other unrecorded failure, refuses with the reply's output.
- **A review the rebase invalidated is queued again by land.** Land releases the lease, queues `--again` at the same effort with the new `base:`, waits, then starts again from `prepare`, so what it requires is worked out again. Land's own rebase leaves the worktree rebased, and its reflog entry is what `rebasedFrom` reads.
- **It waits instead of refusing for what clears by itself.**
  - A moved `main` starts again from the fetch while time remains. Today it retries once.
  - A main checkout with unpushed commits is fetched and checked again every 15s, both before the lease and under it. Land still never pushes it.
- **While it waits, it watches.** Every poll reads the node's hold and its mail again. A hold refuses at once, and so does any operator message sent since land started, read or unread, because a lead woken mid-land can't act on it before the push. That extends spec 044. Under the lease, land checks again that the worktree is clean and that HEAD is what it waited on (or its own rebase of it); otherwise it refuses: `HEAD moved while land waited`.
- **Limits and events.**
  - `--wait D` bounds all of the waiting. The default stays 30m (today it covers the lease alone). Past it, land exits 5 with `timed out waiting for <what>`.
  - While it waits, land publishes a `land` event under the node at the start and then once a minute. It sets no state (`land` is not in `eventStates`) but counts as activity, so a lead waiting in land isn't called silent, and `watch` shows where it is.
- **Land run outside a node** queues no review, waits on no jobs and publishes no events, just as `gate` and `mail` skip today.
- **What still stops it** (nothing pushed, exit codes as today): a hold, operator mail, a conflict, a failing verify (exit 1) or a failing review, scaffold drift, a spec that isn't `building`, or a HEAD that moved (all exit 3). These are the refusals that need a model.
- **The lead skill, sections 6-7.**
  - Commit, then run `bmesh land` in the background, with a timeout 15 minutes above `--wait` (45m at the default), since a killed land can leave a rebase half done.
  - While land runs, a wake means: read the inbox and leave the worktree alone until land exits. A hold or a message makes land refuse by itself.
  - The review-sizing paragraph becomes `--size`, and the refusal list shrinks to the ones above.
- **Docs.**
  - README: the command index row for land, and the landing prose under "Proof before main" and "How you drive the work".
  - Manual, land section: its steps, and `--wait`'s new meaning in the flag table. In the refusals table, drop `main moved again` and the unpushed-main row; add `timed out waiting for <what>`, the unanswered-twice refusal, `HEAD moved while land waited` and `land needs --size`.
  - Internals: land's steps and the `land` event.

Out:
- Taking the landing off the lead's turn: that is spec 080.
- Batching several leads into one verify. Gas Town's merge queue batches and bisects, but the lease and carried verify keep a landing near 2 minutes, and blaming a red batch is a model's job.
- Avoiding conflicts: the other thread of this round covers that.

Judge: opus, because land gains a review it queues itself and waits around the lease. A mistake either lands an M change unreviewed, queues a second Opus review, or holds the lease through one. Size L, not M: land's refusals, outputs and `--wait` are what the lead skill and 080 depend on.

## What you'll see
A lead on an M spec runs one `bmesh land` and reads `review: queued <id> (medium, size M)`, `waiting for job <id> (review, running on k16)`, `lease land.buildermesh`, … `landed <sha8>`, with `review ok` on the `verified` line. A rebase that changes the diff prints `review: queued <id> again (the rebase changed the diff)` and still ends in `landed`.

## Verification
The repo's verify, plus new `cmd_land_test.go` cases over a bare origin. As in `cmd_routine_test.go`, no routine daemon runs in these tests: jobs are answered by hand with `mesh.SendReply`.
- **Sizing.** An M spec with no review gets one at medium and lands with it on the line. S queues none, `--size L` queues high, and a spec with no size refuses.
- **No second review.** A review already queued or running is waited for, not queued again, including on a re-run after exit 5. A branch reviewed ok, or reviewed fail then fixed, queues none, and the fixed one lands on verify alone. A branch reviewed and then rebased by hand queues none, and the review carries.
- **Replies.** A failing review refuses and prints the findings. An unjudged reply is queued once more, then refuses. Another kind of unrecorded failure refuses at once. The replies land consumed are acked.
- **The rebase.** A rebase that changes the diff queues `--again`, works out what it requires again, and lands after it.
- **Waits.** `main` moving twice lands. A main checkout ahead waits, then lands once it's pushed. A hold during the wait refuses, and so does an operator message during the wait, even one already read. A commit made during the wait refuses under the lease. `--wait` exceeded exits 5. `land` events repeat each minute.
- **Outside a node.** Land queues and waits on nothing.

## Risks and the alternative considered
- **The lease.** Land never holds it through a review: the review is waited for before the lease, and a re-review releases it.
- **The tool limit.** A wait past the Bash tool's 10-minute limit is why the skill runs land in the background with a longer timeout. Spec 080 takes it off the turn.
- **Sizing.** The frontmatter `size` can understate a change that grew; `--size` covers that, as the lead's own sizing does today.
- **Alternative: leave land alone and put this logic in the daemon (080 alone).** Then a lead landing in its own turn, or a box without the daemon, gets none of it. Keeping it in land keeps one landing path, which 080 runs as a subprocess.

## Upgrading
nothing: no key or record changes. Land now queues the review a spec sized M or larger calls for when the branch has none, and waits where it used to refuse.

## Needs from the operator
