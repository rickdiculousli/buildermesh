---
id: 004
title: The method plugin and retiring superpowers
kind: maintenance
size: S
judge: opus
status: done
lens: eng
proposed_by: da6488fd-38bd-4c4c-994c-a7de257d9424
proposed_on: 2026-10-15
built_by: 1e0cd8ca-e09a-440f-bd51-9fc2fdcf6260
branch: t3code/331e824a
host: k16
verified: review ok t:7d426208 m:claude-fable-5-1; verify ok t:7d426208
merged: fb40ffb9ecb7770d1d7db9eb9587fc4e39c50abc
landed: rickdiculousli-tools d64ab48, command-center b82fdca
---

## Opportunity
Round two decision 8 in `~/repos/command-center/docs/superpowers/specs/2026-10-15-buildermesh-round2.md`. superpowers' hook and skills conflict with the mesh and pstack; the two skills Rick uses (grilling, writing-plans) are a local copy and an upstream plugin.

## Proposal
Independent of 001 to 003; may run beside 002 or 003. Lands in `~/repos/rickdiculousli-tools` under its AGENTS.md rules.
In:
- `plugins/method`: skills `grill-me` and `grilling` from `github.com/mattpocock/skills` and `writing-plans` from `github.com/obra/superpowers`, copied verbatim; `UPSTREAM` records each upstream's commit; `update.sh [--to <repo>=<ref>]...` fetches both repos (shallow, at the pins or the given refs) into a temp dir, copies the named skill directories, prints the diff per skill, records the new pins and reminds to bump the version; `--check` diffs only. No hook.
- Marketplace entry and version; install on both boxes through `repo_sync.py`; remove `~/.claude/skills/grill-me` and `grilling` on both boxes once installed; `claude plugin uninstall superpowers` at user scope on both boxes; `rm -rf ~/repos/superpowers` on both and its row from command-center's `projects.md`.
- command-center's CLAUDE.md and the round-two spec name `method:grilling` and `method:writing-plans` where they said superpowers.

Out: executing-plans and every other superpowers skill.

Judge: opus, because the spec pins every file.

## What you'll see
`/method:grill-me` and `/method:writing-plans` on both boxes; no superpowers hook text at session start; `update.sh --check` shows upstream drift.

## Verification
rickdiculousli-tools' verify (its AGENTS.md); `claude plugin list` on both boxes shows method and not superpowers; a session on each box starts without the superpowers hook; `update.sh --check` exits 0 right after the copy.

Built on k16 (2026-10-15): checked there; the Mac can't be reached from k16. Left for the Mac: `uv run tools/repo_sync.py` in command-center, `claude plugin install method@rickdiculousli-tools` and `codex plugin add method@rickdiculousli-tools`, `rm -rf ~/.claude/skills/grill-me ~/.claude/skills/grilling`, `claude plugin uninstall superpowers --scope user`, `rm -rf ~/repos/superpowers`, and `cp fleet/bmesh/repos.toml ~/.config/bmesh/repos.toml` (superpowers left the registry).

## Risks and the alternative considered
A gated port like pstack-claude: rejected, three short skills don't earn a gate.

## Needs from Rick
None.
