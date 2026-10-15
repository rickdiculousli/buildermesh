---
id: 020
title: command-center folds into bmesh, and nothing of it is left
kind: maintenance
size: L
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-15
---

## Opportunity
After 005, command-center is a second control plane that bmesh still depends on:
- bmesh's `t3` runner runs `~/repos/command-center/tools/t3_dispatch.py` (517 lines; `fleet/bmesh/mac.toml`).
- The registry reaches each box only when `repo_sync.py` copies `fleet/bmesh/repos.toml` there.
- The tokens live in `~/.config/command-center/`.
- The hourly quota-reset reminder is its own systemd timer.
- Its AGENTS.md (23 KB) is loaded by every session in it, and much of that restates bmesh's README.

Every box has to clone it, and a box with bmesh but no command-center can't run the t3 runner or get the registry. buildermesh is meant to drive any fleet once it is set up, with its config in XDG, assuming Tailscale and nothing else about the fleet.

## Proposal
Runs after 019 is `done`, alone, like 005 (a barrier). Where each part goes:
- **`t3_dispatch.py`** is embedded in the binary (`go:embed`) and written to `<user cache dir>/bmesh/t3_dispatch.py` when it's first used. `[runner] t3_dispatch` defaults to it, and a path set there still wins. Its token moves to `<config dir>/t3-session.json`.
- **`repo_sync.py` and `prune_worktrees.py`** are already replaced by `bmesh sync` and `bmesh prune` (019).
- **The registry.** Each box's `repos.toml` stays in XDG. `bmesh sync --registry` sends this box's copy to every box that accepts `sync`, keeping the old copy as `repos.toml.prev`. The `bmesh repos` editing commands do the same on write when `[hosts]` is set.
- **`reset_reminder.py`** becomes the daemon's: `[quota] remind_resets = true` schedules one ntfy push per provider window with usage, for its reset (ntfy's `At` header), from the readings `[quota] command` already takes, once per window across daemons (claimed like a push).
- **Skills.** `prune` and `repo-sync` become `buildermesh:prune` and `buildermesh:sync`, the chat entry points ("clean up worktrees", "sync my repos", "sync pstack"). `pr-comment`, `tooling-editor` and `pr_watch.py` belong to no mesh, so they move to rickdiculousli-tools as a `github` plugin.
- **`this-box.sh`** becomes the plugin's SessionStart hook: `this box is <host>; on the mesh: <bmesh hosts, one line>`.
- **`fleet/`.** Box configs stay only where they are installed (`<config dir>/config.toml`). Live facts (OS, revision, runner, `max_leads`, repos) come from `bm-hosts`. Each box's free-form notes go in `<config dir>/notes.md`, published in its `bm-hosts` entry and shown by `bmesh hosts --notes`. Runbook content that holds for any fleet (a hub on a tailnet, a leaf box, a sleeping box) becomes a README section, "Several boxes". Per-repo facts move into their repo's AGENTS.md: the Ghidra pin procedure into ghidra-mcp's, and the k16 git hub into eggnogg-rework's and pstack-claude's.
- **Docs.** The two buildermesh design specs move to `docs/design/`, and AGENTS.md points there. A fork tracker (`docs/projects/openGym*.md`) goes to `<config dir>/notes/<repo>.md`, sent with the registry.
- **Retiring it.** The `command-center` row is removed with `bmesh repos remove command-center --checkouts --apply` (019) and its T3 project archived. `~/.claude/CLAUDE.md` on each box loses its command-center line, the Mac's from the Mac.

Out: replacing the standalone nats-server or ntfy units (a box keeps its hub as it has it; README "Several boxes" says how to set one up); porting `t3_dispatch.py` to Go.

Judge: opus, because every move is named, but the hook text and the README section must hold for a fleet that isn't this one.

## What you'll see
A box with only bmesh, after `bmesh init`, runs the t3 runner, gets the registry and is told which box it is. `ls ~/repos` on k16 has no command-center. `bmesh hosts --notes` shows what `fleet/machines.md` used to. Nothing under `~/.config/command-center` is read.

## Verification
The repo's verify. New tests:
- the embedded dispatch is written and run (a fake `uv`);
- a configured `t3_dispatch` wins over the embedded one;
- `sync --registry` against two meshtest hosts;
- the reset reminder schedules once per window across two daemons;
- the hook's output with and without `[hosts]`.

Then `grep -r command-center README.md skills docs/agents-snippet.md internal cmd` finds nothing outside `docs/design/` and `docs/proposals/`.

## Risks and the alternative considered
Alternatives: keep command-center as a fleet-docs-only repo, or put the configs on the hub. The first keeps a second checkout every box must have. The second makes a box's config unreadable while the hub is down. Porting t3_dispatch to Go would remove uv, but it rewrites the one adapter to T3's undocumented API. Embedding the script keeps that API knowledge in one file.

## Needs from the operator
Archive or delete `rickdiculousli/command-center` on GitHub once this lands (archive keeps the history).
