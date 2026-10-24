---
id: 098
title: A landed commit's Verified trailer names the review that ran, even when later commits followed it
kind: refinement
size: M
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity
093 (M), 094 (M) and 095 (L) landed on 2026-10-24 with `Verified: verify ok t:<tree8>` and nothing else. All three were reviewed: 093's and 094's reviews were ok with small notes, and 095's found one Should fix, which its lead fixed. Each lead committed after its review, so the review's tree isn't the landed tree and the line drops it. 087 made `land` print `left behind by later commits: review ok t:<tree8> at <sha8> (not on the verified line)`, but only in land's output; the commit, which is what lasts, can't tell "reviewed, then fixed or polished" from "never reviewed". The operator decided (2026-10-24) that the proof on a landed commit shows a review that ran.

## Proposal
In:
- **The trailer carries the review that ran.** When the branch has a review result on an earlier commit and none at the landed tree, `land` adds it to the `Verified:` entries in a form that can't be read as a review of the landed tree: the tree it read, its verdict and model, and the commit it read (for example `review ok t:3f2a1b0c m:claude-opus-5-5 at:882e6c94`). A failing review that later commits fixed shows as it failed.
- **`bmesh done`** checks such an entry against the result recorded for its own tree, as it checks the others, and treats it as a record of what ran: it neither refuses the landing for a failing one nor counts it as the landed tree's review. The rules for what a landing requires are unchanged (one pass; a second only after a rebase changed the code).
- **Readers that parse the trailer** (`release`, `spec steps`, the daemon, the map) read the new entry or skip it, and an older binary reading it is checked: the spec says what happens there.
- The manual's `land` and `done`, and internals' Verified grammar.

Out: requiring a review at the landed tree; changing what a review checks; rewriting landed history.

Judge: opus, because every reader of the trailer must keep telling a landed-tree result from a record of an earlier one.

## What you'll see
`git log -1 <landed>` on 095 as it would land now: `Verified: verify ok t:d562677c; review fail t:<tree8> m:claude-opus-5-5 at:<sha8>`, and `bmesh done` accepts it.

## Verification
Failing first, on `cmd/bmesh`'s bare-origin land fixture: a branch with a review ok at commit A and a later commit B lands with the review entry naming A; the same with a failing review; a branch reviewed at HEAD lands as today. `bmesh done` accepts each, and refuses an entry whose recorded result doesn't match. The repo's verify.

## Risks and the alternative considered
- **A reader counts the entry as the landed tree's review.** The `at:` form exists so none can; each reader has a test.
- **Alternative: say it only in the lead's final message.** That is today, and it doesn't last.

## Upgrading
Update `bmesh` on every box (`bmesh update`, or the daemon's own update) before a lead lands with this release: an older `bmesh done` reads a `Verified:` trailer's `at:` entry as malformed (`verified entry ... is not ...`) and refuses the landed commit that carries one. Nothing else reads the entry: `release`, `spec steps` and the daemon never read the `Verified:` trailer, and an older map shows it as text.

## Needs from the operator
