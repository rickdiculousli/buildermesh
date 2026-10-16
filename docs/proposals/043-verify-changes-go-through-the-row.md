---
id: 043
title: A spec that changes a repo's verify commands changes its registry row, and land notices when it didn't
kind: refinement
size: S
judge: opus
status: building
lens: eng
proposed_by: 56979e66-321a-467e-9cbc-a2165df787ff
proposed_on: 2026-10-16
from_feedback: buildermesh-build-3 (buildermesh-034)
built_by: 1987097a-9507-4022-980a-02fbdc9f9043
host: k16
family: buildermesh-build-5
branch: bmesh/1987097a-9507-4022-980a-02fbdc9f9043
---

## Opportunity
Spec 034 added `GOOS=windows go vet ./...` to buildermesh's verify by editing `.bmesh/routines/verify.toml` and AGENTS.md by hand. Both are generated: a registry row's `verify` "is shell every lead runs and `bmesh scaffold` writes into `.bmesh/routines/verify.toml`" (README, The registry), and AGENTS.md's done and mesh blocks are scaffolded from the same list. The row (the admin's `repos.toml`) still has three commands, so `bmesh scaffold --check buildermesh` now shows drift in both blocks, the next `bmesh scaffold buildermesh` would silently drop the Windows vet, and the briefs leads get (`brief.VerifyText(r.Verify)`) don't name it (feedback 1164). A lead can't fix it: with `[hosts]` on only the admin box changes the row; a lead can only `bmesh repos propose`, which waits for the operator's approval. Nothing in the spec template, the lead skill or `bmesh land` says a verify change goes through the row.

## Proposal
In:
- **Land checks the scaffold.** `bmesh land` runs the `scaffold --check` comparison for the repo on the rebased tree before it records; on drift it refuses (exit 3) with the diff's first lines and `the row's verify is the source: bmesh repos propose with the new verify, or restore the scaffolded files`. A repo that was never scaffolded (no `bmesh:` markers) is skipped.
- **The lead skill and the template say it.** The lead skill's verify section and the template's Verification guide line: a spec that changes the verify commands changes the row (through `repos propose`, named in the spec's "Needs from the operator") and lets `bmesh scaffold` write the files.
- **Fix buildermesh's row**: named in Needs from the operator, since only the admin publishes it.

Out: moving the source of truth from the row to `verify.toml` (the row also feeds briefs on boxes without the checkout), and letting leads publish the registry.

Judge: opus, because land gains a refusal and a false one would block every landing on a scaffolded repo.

## What you'll see
A lead that edits `verify.toml` by hand is stopped at `bmesh land` with the drift and the `repos propose` line; `bmesh scaffold --check buildermesh` is clean once the row carries the Windows vet.

## Verification
The repo's verify. New `cmd_land_test.go` cases: a branch whose `verify.toml` differs from the row is refused with the drift; one matching the row lands; an unscaffolded repo lands. The skill and template wording are checked by reading.

## Risks and the alternative considered
A row changed and published while a lead is mid-build makes its branch drift through no edit of its own; the refusal's diff shows that, and the fix is to rescaffold on the branch. Alternative: have `scaffold --check` run in each repo's verify. That puts a registry read in every repo's tests and fails on boxes without the row.

## Needs from the operator
Add `"GOOS=windows go vet ./..."` after `"go vet ./..."` in buildermesh's `verify` in the admin's `repos.toml` (the Mac's `<config dir>/repos.toml`, kept in `command-center/fleet/bmesh/repos.toml`), then `bmesh repos publish`.
