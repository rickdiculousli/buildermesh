---
id: 038
title: bmesh land keeps a branch's review after the lead rebases by hand
kind: refinement
size: S
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
Spec 032 landed with `verified: verify ok t:bff0a401` although three Opus reviews of it passed. The lead's first `bmesh land` stopped on a conflict with 036, as land does. The lead then rebased by hand, as the brief says to, and ran `bmesh land` again. That run read the branch "as it stands" (`prepare`, `cmd/bmesh/cmd_land.go:165`): its required kinds are verify plus the kinds with a result on HEAD's tree. The rebased tree had no review, so review wasn't required, nothing was carried and nothing was refused. The review fell off the line without a word, and `bmesh done` accepted it (feedback 1059).

The same path is the one every conflict takes, so any lead whose landing conflicts loses its review this way. The only repair left is typing the line by hand, which the session's safety classifier rightly blocks as editing an audit record.

## Proposal
In:
- **Land finds the branch as it was before a hand rebase.** When HEAD's tree has no result for a kind, `prepare` walks the branch's reflog (`git reflog <branch>`, newest first) for the newest entry whose tree has recorded results and whose commit is not an ancestor of HEAD: the head the lead rewrote. It takes that entry as the old head, its tree as the old tree and its fork point from `main` as the old base, exactly as if land had done the rebase itself. Its kinds join the required ones, and the existing rules apply unchanged: a result whose diff the rebase left unchanged is carried (`c:<old tree8>`), and one whose diff changed is refused with the existing `queue review --again` line. An entry that is an ancestor of HEAD (fixes committed after a review) changes nothing, so the one-pass rule stays: a review's findings are fixed and landed without a second review.
- **Land says what it requires.** Before the checks it prints `required: verify (the repo's verify kind), review (t:<tree8> before the rebase at <sha8>)`, so a kind left out is visible in its output.
- **`bmesh spec verified --commit C`** composes the line from the results for C's tree instead of HEAD's; `--write` still writes into this worktree's spec file. A line can then be rebuilt by the tool for a commit already merged, never typed.
- **Repair 032's line** with it as part of this build: `bmesh spec verified --spec 032 --commit d842de47 --write` gives `review ok t:bff0a401 m:claude-opus-5-5; verify ok t:bff0a401` from the recorded results, committed on this spec's branch and landed with it.
- README Landing and the lead skill's section 7 say that land recovers a hand rebase, and how.

Out: requiring the row's `routines` list on every landing. A rote change lands on verify alone by design, and a reviewed change whose findings were fixed lands without a second review; land can't tell those from a dropped review by the row alone. Out too: results recording who asked for them (`from`), which this doesn't need.

Judge: opus, because it changes what land requires and carries, and a mistake either drops reviews again or blocks the one-pass rule.

## What you'll see
A lead's landing that conflicts, is rebased by hand and run again prints `required: verify (…), review (t:… before the rebase at …)`. It then either carries the review onto the line or stops and says to queue it again. Spec 032's `verified` line names its Opus review.

## Verification
The repo's verify. New tests in `cmd_land_test.go`:
- a branch reviewed ok, rebased by hand onto a main that conflicts, then landed: refused with the `--again` line when the diff changed, and carried (`c:`) when the resolution left the diff unchanged;
- a branch reviewed, then fixed with a commit on top (an ancestor): lands on verify alone, as today;
- the `required:` line names each kind and where it came from;
- `spec verified --commit` composes the line for that commit's tree.

## Risks and the alternative considered
The reflog is local and expires (90 days by default), so a branch rebased on another box, or long ago, falls back to today's behaviour. Leads rebase in their own worktree on their own box within the hour, so that covers the case that happens.

Alternative: record the requesting node on every result (`from`) and require every kind the lead's node ever ran. That needs a field in `bm-results` and still can't tell a fixed review from a dropped one without the commit graph, which the reflog walk uses anyway.

## Needs from the operator
