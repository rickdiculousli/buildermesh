---
id: 097
title: A proposal round is one thread that ranks the engineering and UI/UX ideas in one list; a second thread only when the app runs on another box
kind: token-cost
size: M
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity
Every `bmesh ideate` starts two threads, `eng` and `ux` (`cmd/bmesh/cmd_ideate.go:150`). They are the article's tightly coupled pair, split by problem type:
- `skills/ideate-engineer` and `skills/ideate-ux` are about 85% the same text.
- Both read the same repo, docs and ledger, and each claims its own block of five spec ids, so numbering has gaps by design.
- Their lists are joined, not ranked: `Pitch` (`internal/daemon/round.go:62`) prints the engineer's lines, then the UI/UX lines, with no dedupe and no order across them.
- A repo with no surfaces (opengym-rick, rickdiculousli-tools) still gets a UX thread, whose skill can only say "no screenshot, no proposal".
- Every active registry row has `ux_host` equal to `host` or empty; only the dormant eggnogg-rework splits them.
- The UX lens produced 12 of 92 lensed specs here and 1 of 16 in opengym-rick, for one of a round's two threads. A round thread's start alone is 36% of its reads (083's measurement).

## Proposal
In:
- **One thread per round** when `ux_host` is empty or equals `host`. It reads the repo once and ranks three to six ideas in one list across both lenses, best first. It runs the UI/UX method (run the app, screenshot each surface at its viewport, no screenshot no UI claim) only when the row lists surfaces; a row without surfaces never mentions it. One block of spec ids.
- **A second thread only when `ux_host` names another box** and the row has surfaces: it runs only the UI/UX part, on that box, as today.
- **One method, written once.** The two skills and the two brief templates become one of each; the second-box thread is that skill told by its brief to do only the UI/UX part. No method text in two places.
- The daemon settles a round of one child as it settles two; the pushed pitch is the thread's list as reported.
- `--idea` is unchanged.
- **A round case in the skill test**: the round brief for `shop` (no surfaces), scored on specs written for its M ideas with valid frontmatter, pitch lines for its XS and S ones, nothing outside `docs/proposals/` changed, the specs on origin's `main`, and `done` carrying a ranked pitch of three to six lines.
- The manual, internals (the round node's children), the README's index and `docs/agents-snippet.md` where they name the two threads.

Out: how the round decides what to propose; `--idea`; rounds already open when this lands, which settle as they started.

Judge: opus, because the one skill must keep both methods' rules (measure before claiming; no screenshot, no UI proposal) without carrying either where it doesn't apply.

## What you'll see
`bmesh ideate party-buzzer` starts one thread; its push is one ranked list mixing engineering and UI/UX ideas, numbered without gaps.

## Verification
The repo's verify, with tests for: one child for a same-host row with and without surfaces, two for a row whose `ux_host` differs, the daemon settling a one-child round, and the brief with and without surfaces. The skill test's round case, base (main before this) against cand, `--runs 2`: numbers in the closing commit.

## Risks and the alternative considered
- **UI ideas get crowded out** by engineering ones in one ranking. The method keeps "at least one that isn't a feature" and the round case shows the mix; a real round on party-buzzer after landing is the check.
- **Alternative: keep two threads and merge their lists in the daemon by rank.** Still two orientations and two id blocks, and the daemon can't dedupe ideas it doesn't understand.

## Upgrading
A repo's own `.bmesh/briefs/ux-engineer.md` is no longer read: every thread of a round renders `engineer`, which takes `{{title}}`, `{{role}}` and `{{surfaces_line}}` beside `{{round_ask}}`, `{{round_partner}}` and `{{ux_ids}}`. Carry what a repo's own `ux-engineer.md` said into its `engineer.md`, or delete it. Rounds open when a box takes this release settle as they started. A round whose thread starts on another box over ssh gets that box's brief, so run this release on every box a registry row names as `host` or `ux_host` before starting rounds there.

## Needs from the operator
- [k16] Once k16 runs this release, start a round on party-buzzer (`bmesh ideate party-buzzer`), which now runs as one thread with the UI/UX part, and read whether its UI/UX ideas held their place in the one ranked list (the risk this spec names).
