---
id: 002
title: Proposal rounds and review in bmesh
kind: feature
size: L
judge: fable
status: accepted
lens: eng
proposed_by: da6488fd-38bd-4c4c-994c-a7de257d9424
proposed_on: 2026-10-15
---

## Opportunity
Round two decisions 3, 4, 6 and the Rounds section in `~/repos/command-center/docs/superpowers/specs/2026-10-15-buildermesh-round2.md`. Rounds run from command-center's `propose` skill and `brief.py`; results land as files with no push, and Rick's reply only works inside the proposing thread.

## Proposal
Builds on 001 (the registry).
In:
- Round nodes: `<repo>-round-<N>` with children `eng` and `ux` (role `engineer`), `N` from the nodes bucket. The round is done when both children are done, blocked when one is; the daemon pushes `<repo> round N proposed` with the merged ranked pitch (eng first, then ux, by rank) and an `Open thread` action for the eng thread; `bmesh status` shows the round with its children.
- `bmesh propose <repo> [--model m]`: refuses five or more `proposed` specs; makes two bmesh worktrees of the repo under `[runner] worktrees/<repo>/<repo>-round-<N>-eng|ux` from main; renders the engineer and ux-engineer briefs (next free IDs, ux five past eng, the thread id as `proposed_by`); starts both through the runner on the rows' `host` and `ux_host` (a remote host through that box's runner as `lead start` does; an unreachable host is refused with a message). With the t3 runner the thread's T3 project is the buildermesh repo and its cwd the bmesh worktree.
- Skills `buildermesh:propose-engineer` and `buildermesh:propose-ux`: the round's method from command-center's engineer and ux-engineer briefs (what to read, the lenses, three to five ranked half-pagers, screenshots of the row's `surfaces` for ux, landing on main, `bmesh done` with the ranked list as text), with "if pstack is installed, `exhaust-the-design-space` and `attack-the-premise` before ranking" and a written default.
- `bmesh spec review <repo> "<reply>"`: today's grammar (yes/no/later, adjust words, "on fable|opus|sonnet", ranks or IDs), refuses to move a spec not in proposed/later/adjusting, edits an adjusting body, commits `Review: ...` in a fresh bmesh worktree, fast-forwards main (`fetch . <branch>:main` when the main checkout is elsewhere), pushes, removes the worktree, prints one line per spec and the unmatched items. `bmesh spec list|next-id|set|template` replace `proposals.py`'s functions, same refusals.
- Embedded templates (engineer, ux-engineer, build, half-pager) under `internal/brief`, ported from command-center's `templates/` with the standard closing and done rule rendered from the row; a repo overrides one with `.bmesh/briefs/<name>.md`; a placeholder without a value is an error.

Out: `bmesh build` (003); a map panel for proposals.

Judge: fable, because the brief and skill texts carry the round's judgment.

## What you'll see
`bmesh propose party-buzzer` starts two threads under the buildermesh T3 project; a phone push arrives with the ranked pitch; a reply typed in that thread (or `bmesh spec review` anywhere) lands the verdicts on main.

## Verification
Routine `verify`; tests for the round state machine, the push body, the reply grammar (ported from command-center's `tests/test_proposals.py`), the template rendering and the override; one real round on a ship repo with the daemon's push observed.

## Risks and the alternative considered
Proposal threads in the target repo's T3 project (today): rejected, the thread has no code diff and gains nothing from T3's worktree. Merging two rankings: concatenation this round.

## Needs from Rick
None.
