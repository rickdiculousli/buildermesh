---
id: 066
title: A spec's number is claimed through the mesh, per repo, so two writers never land the same one
kind: maintenance
size: M
judge: opus
status: building
lens: eng
proposed_by: dfdc9673-27b2-4f3f-9f17-95a5b436de66
proposed_on: 2026-10-17
built_by: 1f5f05e4-23b0-4ba9-8de5-ebd8c9ddff20
host: k16
family: buildermesh-build-10
branch: bmesh/1f5f05e4-23b0-4ba9-8de5-ebd8c9ddff20
---

## Opportunity
`bmesh spec next-id` is "one past the highest id" on the ledger as the writer's checkout has it. Nothing reserves the number. Two writers who ask before either lands get the same one.

On 2026-10-17 at 04:01 this happened. The coordinator wrote the operator's key-dialog spec as 064 at the Mac, while bmesh's own fix lead (`buildermesh-fix-1`, on k16) wrote its spec from the Mac soak's feedback as 064. Both landed. A ledger with two specs sharing an id is an error for every command (README, The ledger), so at 04:02 the fix lead's own read failed (`fixer: read ... share id 064`). It renumbered to 065 a minute later, but the collision would just as well have landed during a build or a release, and stalled it until someone renumbered.

There used to be one writer at a time: a coordinator, or a proposal round whose ids `ideate` hands out (`--next-id N` per child). Since 058 and 059, the fix lead writes specs whenever feedback arrives, beside the coordinator and any round.

The operator's ruling: the number comes through the mesh, per repo.

## Proposal
In:
- **A counter per repo on the mesh.** A KV bucket, `bm-specids`, has one key per repo, `<repo>` (the registry name in lower case, `.` and `_` as `-`, as node paths spell it). Its value is the highest id claimed. It has no age limit, and it is replicated like `bm-nodes`.
- **`bmesh spec next-id [--repo R] [--count N]` claims.**
  - It reads `main`'s highest id (as today, from `origin/main` after a fetch, else the checkout) and the counter.
  - It sets the counter to `max(counter, main's highest) + N` with a compare-and-swap, retrying on a conflict.
  - It prints the first claimed id (`066`), or the range with `--count` (`066-070`).
  - A claimed id that is never landed is a gap, and gaps are allowed. Nothing re-issues it.
- **`--peek`** prints what the next claim would get and claims nothing, for a reader.
- **Rounds claim their blocks.** `bmesh ideate` claims five ids per started child (`--count 5`) and passes each child the first of its block (`--next-id`, as today). An idea thread (`--idea`) gets three. A child that writes fewer leaves a gap.
- **Every writer uses it.** These say "claim the id with `bmesh spec next-id`, then land it" and nothing else:
  - the `build` skill's step 7;
  - the `fix` skill;
  - the `ideate` skill (an idea written in session);
  - the half-pager template's guide line;
  - the brief of anyone else who writes a spec.
- **With no mesh**, `next-id` falls back to today's reading of `main` and says so on stderr: `next-id: no mesh, so 066 is not claimed: another writer may take it`.
- **A backstop on the commands that land the ledger.** `choose-ideas`, `spec set`, `build`'s dispatch commit and `land` check that the ledger they are about to push has no duplicate id. On a duplicate, they refuse naming both files (`refused: 064-a.md and 064-b.md share id 064; renumber the newer with bmesh spec next-id`) rather than push a ledger nobody can read.
- The README's The ledger, Buckets and the ideate section say it.

Out:
- Renumbering a duplicate automatically.
- Ids across repos.
- Reserving a slug or title.

Judge: opus, because the claim has to hold across boxes and restarts, and a counter that falls behind `main` would hand out a taken number.

## What you'll see
- Two `bmesh spec next-id --repo buildermesh` run at the same moment, one at the Mac and one on k16, print `066` and `067`.
- `bmesh ideate buildermesh` gives its engineer 068 and its UI/UX engineer 073.
- A ledger with a duplicate never reaches `main` from a bmesh command.

## Verification
The repo's verify, plus:
- **`cmd_spec_test.go` on `meshtest.Start`:**
  - concurrent claims, from two goroutines with two clients, never return the same id;
  - the counter catches up when `main` is ahead of it (a spec landed by plain git);
  - `--count` returns a contiguous block;
  - `--peek` claims nothing;
  - with no mesh, the fallback line is printed.
- **`cmd_ideate_test.go`:** the children's `--next-id` values come from claimed blocks that don't overlap.
- **`choose-ideas`, `spec set` and `land` tests:** a ledger with a duplicate id is refused before any push.

## Risks and the alternative considered
- **Specs written by plain git, without `next-id`** (an operator by hand) can still collide. The backstop on the landing commands and the catch-up to `main`'s highest id narrow that to a refusal before push. The duplicate is never pushed by bmesh.
- **Alternative: no counter, only the backstop and renumbering at land.** That's what the fix lead did by hand. It keeps the race and only moves where it is found, and a spec's number may be cited in briefs and messages before it lands.

## Upgrading
nothing: `bm-specids` is created on first use, and `next-id` without a mesh behaves as before.

## Needs from the operator
