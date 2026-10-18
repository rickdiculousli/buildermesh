---
id: 079
title: bmesh land queues the review the spec's size calls for, waits for it and for main, and stops only for what needs the lead
kind: token-cost
size: M
judge: opus
status: proposed
lens: eng
proposed_by: c3029b53-10b6-4fab-83cb-465887cb0b73
proposed_on: 2026-10-18
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
In, all in `cmd/bmesh/cmd_land.go`, before the lease unless said otherwise:
- **Verify first.** Land runs the inline kinds on HEAD. They are answered from the record when the lead already ran them, so a review never reads code that fails its tests.
- **The review, sized by the spec.** When the repo has a `review` kind and the branch has no review result, land queues one on HEAD. "No review result" is the same check `routine queue` uses to refuse a second review.
  - Effort: `medium` for size M, `high` for L or larger, none for XS or S. This is bmesh's done rule, `internal/brief`'s `sizedText`.
  - The job text is `base: <merge-base origin/main HEAD>`, as the lead skill queues it today.
  - `--size XS|S|M|L|XL` states the size as built when the scope grew.
  - Land prints one of: `review: queued <id> (medium, size M)`, `review: none (size S)`, or `review: on the branch already (ok|fail at t:<tree8>)`. A branch whose failed review was fixed queues none, so the one-pass rule stands.
- **It waits for the node's jobs on HEAD.** This covers the review it queued and any other job the node queued for HEAD's commit, such as a `surface` job or a kind on another OS. Land names each once: `waiting for job <id> (review, running on <host>)`.
  - A failing reply refuses, as a failing result does today, and prints the reply's output (a review's findings), so the lead never needs a `routine wait`.
  - A model job that never answered (spec 071's `nothing recorded`) is queued once more.
- **A review the rebase invalidated is queued again by land.** Land releases the lease, queues `--again` at the same effort with the new `base:`, waits, then starts again from the fetch.
- **It waits instead of refusing for what clears by itself.**
  - A moved `main` starts again from the fetch while time remains (today it retries once).
  - A main checkout with unpushed commits is re-checked every 15s. Land still never pushes it.
- **Limits and events.**
  - `--wait D` bounds all the waiting. The default stays 30m (today it covers the lease alone). Past it, land exits 5 with `timed out waiting for <what>`.
  - Each wait publishes a `land` event under the node. It sets no state, so a lead waiting in land isn't called silent, and `watch` shows where it is.
- **What still stops it** (exit 3, nothing pushed, as today): a hold, unread operator mail (checked again before the lease and under it), a conflict, a failing verify or review, scaffold drift, or a spec that isn't `building`. These are the refusals that need a model.
- **Docs and skill.**
  - The lead skill, sections 6-7: commit, then `bmesh land` in the background (it waits on the review, and its exit brings the lead back). The review-sizing paragraph becomes `--size`, and the refusal list shrinks to the ones above.
  - The manual's land section and the README's land row change to match, and the internals get land's steps.

Out:
- Taking the landing off the lead's turn: that is spec 080.
- Batching several leads into one verify. Gas Town's merge queue batches and bisects, but the lease and carried verify keep a landing near 2 minutes, and blaming a red batch is a model's job.
- Avoiding conflicts: the other thread of this round covers that.

Judge: opus, because land gains a review it queues itself and waits around the lease. A mistake either lands an M change unreviewed or holds the lease through a review.

## What you'll see
A lead on an M spec runs one `bmesh land` and reads `review: queued <id> (medium, size M)`, `waiting for job <id> (review, running on k16)`, `lease land.buildermesh`, … `landed <sha8>`, with `review ok` on the `verified` line. A rebase that changes the diff prints `review: queued <id> again (the rebase changed the diff)` and still ends in `landed`.

## Verification
The repo's verify, plus new `cmd_land_test.go` cases over a bare origin, with a review kind on `testdata/fake-claude.sh`:
- **Sizing.** An M spec with no review gets one at medium and lands with it on the line. S queues none, and `--size L` queues high.
- **One pass.** A branch reviewed ok, or reviewed fail then fixed, queues none. The fixed one lands on verify alone.
- **Failing and unanswered replies.** A failing review refuses and prints the findings. An unanswered model reply is queued once more, then refuses.
- **The rebase.** A rebase that changes the diff queues `--again` and lands after it.
- **Waits.** `main` moving twice lands. A main checkout ahead waits, then lands once it's pushed. A hold or an operator message during the wait refuses before the lease. `--wait` exceeded exits 5. Each wait publishes a `land` event.

## Risks and the alternative considered
- **The lease.** Land never holds it through a review: the review is waited for before the lease, and a re-review releases it.
- **The tool limit.** A wait past the Bash tool's 10-minute limit is why the skill runs land in the background. Spec 080 takes it off the turn.
- **Sizing.** The frontmatter `size` can understate a change that grew; `--size` covers that, as the lead's own sizing does today.
- **Alternative: leave land alone and put this logic in the daemon (080 alone).** Then a lead landing in its own turn, or a box without the daemon, gets none of it. Keeping it in land keeps one landing path, which 080 runs as a subprocess.

## Upgrading
nothing: no key or record changes. Land now queues the review a spec sized M or larger calls for when the branch has none, and waits where it used to refuse.

## Needs from the operator
