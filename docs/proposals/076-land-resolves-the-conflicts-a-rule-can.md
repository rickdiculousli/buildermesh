---
id: 076
title: bmesh land resolves the rebase conflicts a rule can, and leaves the lead only the lines both sides rewrote
kind: token-cost
size: M
judge: opus
status: accepted
lens: eng
proposed_by: acc7c4cb-5a7b-4a96-bc3b-bf2508d0beac
proposed_on: 2026-10-18
---

## Opportunity
k16's lead transcripts for specs 017-071 hold 12 real `bmesh land` refusals for a rebase conflict, in 11 of the 48 leads (scripts: `~/.cache/bmesh-scratch/analysis/r5_*.py` on k16). The refusal line's other matches are test output (`cmd_land_test`'s `a.txt` and `AGENTS.md` fixtures) and the manual's own wording. From a refusal to the land that pushed took 2 to 8 minutes and 6 to 28 calls at about 200k context: 25.4M context tokens in all, 5.1% of all lead context tokens. Three of the 12 needed a fresh review.

Each refusal was replayed from the branch's reflog head and the `origin/main` that land fetched (`git merge-tree`, diff3 style). The replays hold 22 conflict regions:
- **None is in the README's command index.** The index was written on 2026-10-17, after all 12. The 10 README regions sat in the old reference that is now `docs/manual.md`. Three of them were its Exit codes row, one line of over 3,000 characters listing every refusal. The README rewrites of 2026-10-17 shortened that row, so they have already removed 2 refusals (039, 046).
- **Most of the rest are git calling touching changes a conflict:**
  - One side changed a line and the other inserted lines beside it (054: a rename, with new lines after it).
  - Both sides appended at one point (new tests at a file's end in 057 and 032, new struct fields in 020).
  - Each side edited a different sentence of one paragraph. The docs keep a paragraph per line (the manual has 187 lines over 300 characters), and 037's case was a re-wrapped doc comment.

Resolving each region by the rules below, then building the result:
- 4 refusals resolve, and the trees pass `go build ./... && go vet ./...` (037, 054, 057 and the first of 056's two).
- 2 more resolve as text but don't build, because both sides moved the same code. 059 declares `ModulePath` twice, and 061's branch drops an import that main's new code uses. Verify catches these, and the lead fixes a compile error rather than rebasing by hand.
- 4 rewrite the same sentence or line on both sides (020, 032, 058, the second 056) and stay the lead's.

## Proposal
In:
- **When land's rebase stops on a conflict,** land checks each conflicted file out again in diff3 style (`git checkout --conflict=diff3`). It resolves each marked region from its three sides, in order:
  1. **Additions at one point** (the base side is empty): keep both, main's lines first.
  2. **Changes to different lines** (a conflict only because they touch), counting lines both sides deleted as agreed: apply both.
  3. **Prose a sentence apart.** This covers a markdown region outside code fences and tables, and a region whose lines are all comments (`//`, `#`). Each side is split one sentence per line, and rules 1 and 2 are applied to the sentences. Then the sentences are joined back into the lines they came from, and a comment is re-wrapped at its old width. A side that doesn't split and rejoin to exactly itself is left alone.

  Any other region, and anything in a spec file's frontmatter, stays a conflict.
- **When every region resolves,** land adds the files and continues the rebase, repeating at each stop. It prints `rebase: resolved <file> (kept both, by sentence, ...)` for each file, then goes on as today:
  - The inline kinds run on the rebased tree, so a resolution that doesn't build fails verify. The branch is left as land rebased it, and the lead fixes forward.
  - Nearly every resolution changes the diff's patch-id, which covers three lines of context. A changed diff needs a new review: spec 079 re-queues it itself, and without 079 today's refusal applies.
- **When any region is left,** land aborts the rebase as today, and the refusal says which files a rule resolves and which need the lead.
- **Where:** a new `internal/merge` package (text in, resolved text or a conflict out, no git), plus land's rebase step in `cmd/bmesh/cmd_land.go`.
- **No burden on anyone else.** The rules are land's own code, so every box and repo gets them with the release. Nothing is installed or configured, and nothing is written into a repo: no merge tool, no merge driver, no `.gitattributes`, no git config. No one changes how they write docs or code.
- **Docs:** the manual's land step 2 and Refusals. The lead skill's conflict line says that land resolves what a rule can, and that a failing verify right after `rebase: resolved` is land's merge, to be fixed forward.

Out: any merge tool or driver, a region both sides rewrote, carrying a review across a resolution, a merge by Go syntax, generating the README index, and holding specs at dispatch.

The options weighed, by how many of the 12 refusals each removes:
- **Generate the README index from the manual, and have land regenerate it: 0.**
  - No conflict touched the index, and every row but `bmesh help` matches a manual usage line today.
  - A future index conflict is likelier two rows added at one point, which rule 1 resolves, than one row edited twice.
  - A row edited twice also conflicts in the manual's usage line, and no generator can rebuild that.
  - A merge driver, moving the index elsewhere, or a check-only test do no better for the same reasons.
- **One sentence per line in the docs** (semantic line breaks, as Asciidoctor and sembr.org advise): rule 3 gets the same merges at land. It needs no one-off rewrite of the manual's 279 KB, a rewrite that would itself conflict with every branch in flight.
- **Mergiraf** (a syntax-aware merge driver): it handles Go but not Markdown, and it is a second binary on every box. A declaration-level Go merge, tried here, resolved only what rule 2 does: 059 and 061, which then fail to build.
- **`merge=union` in `.gitattributes`:** rule 1 for whole files, but it silently keeps both sides of a real rewrite too.
- **Holding overlapping specs at dispatch** (MCP Agent Mail's file reservations):
  - Even knowing every file each spec would change, it holds 16 of the 56 concurrent pairs to prevent 7 conflicts. Using only the paths the specs name, it catches 1 of 8.
  - A hold costs its pair's build window (median 23 minutes). A conflict costs about 4 minutes and 2.3M tokens.
  - Agent Mail reserves files for agents sharing one checkout. Here each lead has its own worktree, and the land lease already serializes the one place they meet.

Judge: opus, because a wrong rule lands a quiet bad merge, and verify reads no prose.

## What you'll see
Take a branch that adds a test at the end of `cmd_spec_test.go` while main adds another. `bmesh land` prints `rebase: resolved cmd/bmesh/cmd_spec_test.go (kept both)` and lands. A paragraph that both main and the branch added a sentence to reads `(by sentence)`. Fewer leads see `refused: rebase onto origin/main conflicts in`, and under spec 080 fewer are woken for one.

## Verification
The repo's verify, plus:
- `internal/merge` tests, using the three sides of each measured region as fixtures:
  - kept both: 057's tests and 020's struct fields;
  - applied both: 054's rename beside new lines, and 059's shared deletion;
  - by sentence: 037's comment and 061's `SKILL.md` paragraph;
  - still conflicts: 020's comment, 032's Modes line, 058's Builds line and 056's `statusRow` comment;
  - a frontmatter key added on both sides stays a conflict, and a line that doesn't round-trip through the sentence split is left alone.
- `cmd_land_test.go` over a bare origin:
  - two appended tests land with no refusal;
  - a resolution that breaks the build is refused by verify, with the branch left rebased;
  - a line rewritten on both sides is aborted, and the refusal names the file;
  - a rebase that stops twice resolves both stops.

## Risks and the alternative considered
- **A merge that is clean but wrong.** Verify runs on every rebased tree, and the review runs again whenever the diff changed, which it nearly always does here. The rules only keep both sides' lines or sentences and never write one.
- **Order.** Rule 1 puts main's lines first. Where the order matters, the review sees it.
- **Land changes the branch more than it did.** Its output names each file and rule.
- **Build order.** 079 rewrites the same function, `lander.once` in `cmd_land.go`. Built at once, 076 and 079 would make the kind of conflict measured here, so build them one after the other.
- **Alternative: leave the rebase stopped at what is left,** so the lead resolves only those regions. Land never hands back a worktree mid-rebase today, and a lead that runs land again there is refused on a detached HEAD.

## Upgrading
nothing

## Needs from the operator
