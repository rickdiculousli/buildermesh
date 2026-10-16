---
id: 026
title: bmesh land: rebase, record and fast-forward under a main lease
kind: feature
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
from_feedback: retrospective of 007-016 and 018 (buildermesh-008, -016)
---

## Opportunity
Landing is eight hand-chained steps (fetch, rebase, verify, `spec verified --write`, `spec set done` with `merged` and `branch`, commit, ff-merge, push), and verify (~90s) sits between the rebase and the merge. When another lead lands meanwhile the fast-forward is refused and everything after the rebase is redone: 008 once, 016 three times (about 15 minutes, and one push of someone else's local main). Leads that land often in one repo will hit this every day.

## Proposal
In: `bmesh land [--spec NNN]` from a lead's worktree takes the lease `land.<repo>` (`bm-leases`, `lock-lease`), then: fetch; refuse if the local main checkout is ahead of origin; rebase onto `origin/main`; require (or run) verify and the spec's other result kinds for HEAD's tree; `spec verified --write`; `spec set done --field merged=<sha> --field branch=<current>`; commit; push to `main` fast-forward; release. A refused push retries from fetch once. It prints one line per step and the final `landed <sha8>`. The lead skill's Landing section becomes this command plus what to do on a conflict. Out: resolving conflicts (it stops and says so).

Judge: opus, because it composes most of the CLI and is the interface every lead lands through.

## What you'll see
A lead lands with one command, and two leads landing at once queue on the lease instead of redoing each other's work.

## Verification
Verify, plus `cmd/bmesh` tests via `runCLI` against a bare origin: a clean land, a moved main (one retry), a held lease (waits), a conflict (stops, nothing pushed).

## Risks and the alternative considered
A lead holding the lease through a slow verify blocks others for its length; the lease TTL bounds it. The alternative, a merge queue in the daemon, is more machinery than one lease.

## Needs from the operator
None.
