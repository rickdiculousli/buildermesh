---
id: 005
title: command-center keeps only ops
kind: maintenance
size: M
judge: opus
status: done
lens: eng
proposed_by: da6488fd-38bd-4c4c-994c-a7de257d9424
proposed_on: 2026-10-15
built_by: e301c2c6-470c-4773-b907-8054c9f6ce42
host: k16
branch: t3code/b0ee2f6d
verified: review ok t:1d3d3a2a m:claude-fable-5-1; verify ok t:1d3d3a2a
merged: c4d9137cc9f58163dfd3557c3977ff64534699ca
---

## Opportunity
Round two decisions 1, 2 and 11 and "What goes, what stays" in `~/repos/command-center/docs/superpowers/specs/2026-10-15-buildermesh-round2.md`. Once 001 to 004 are done, command-center still carries the lifecycle's Python, skills and templates, and the registry table.

## Proposal
Runs alone, after 001 to 004 are `done` (a reorg barrier: nothing else building in command-center).
In, all in `~/repos/command-center`:
- Delete `tools/brief.py`, `tools/proposals.py`, `tools/projects.py`, `tools/align_repo.py`, `templates/`, their tests, and the skills `propose`, `dispatch`, `align-repo` (both the `.agents/skills` dirs and the `.claude/skills` symlinks).
- `projects.md`: the table goes; it keeps the modes prose and says the registry is `fleet/bmesh/repos.toml` (`bmesh repos`). `repo_sync.py` and `prune_worktrees.py` read `bmesh repos --json` (done in 001; remove any projects.py fallback).
- CLAUDE.md: the Tools, Skills and Projects sections describe what stays and point the lifecycle at `bmesh` (propose, spec review, build, align, repos, init); the Which-box and Fleet sections unchanged; the `bmesh` paragraph updated for the registry and the claude runner.
- `fleet/bmesh/`: `repos.toml` beside the two box configs, with install notes in `fleet/systemd/README.md`.
- Final align: `bmesh align` on every ship repo (party-buzzer, gametrainer, command-center, rickdiculousli-tools, computer-use, buildermesh), committing and syncing each.
- Record this round: the round-two spec's status in command-center, and `docs/superpowers/plans/` untouched.

Out: anything in buildermesh.

Judge: opus, because every deletion and pointer is named.

## What you'll see
`ls tools` shows repo_sync, prune_worktrees, pr_watch, reset_reminder, t3_dispatch; `ls .agents/skills` shows prune, repo-sync, tooling-editor, pr-comment; every ship repo has routine kinds; the test suite passes without the deleted modules.

## Verification
`uv run -m unittest discover -s tests -t .`; `repo_sync.py --all --dry-run` reports every repo; `bmesh align --check` prints no diff on each ship repo; `grep -r projects.py tools .agents` finds nothing.

## Risks and the alternative considered
Keeping thin wrapper skills: rejected (decision 1), they restate the binary's help.

## Needs from Rick
None.
