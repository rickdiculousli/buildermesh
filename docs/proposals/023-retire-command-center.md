---
id: 023
title: Retire command-center: every file to its new home, then the repo goes
kind: maintenance
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
Once 019, 020 and 022 are done, nothing in bmesh needs command-center. It is still a checkout on every box, and AGENTS.md and the user-level CLAUDE.md still send sessions there. It holds the only copies of the box configs, the NATS files and the fleet notes. Some k16 services still read `~/.config/command-center` (nats.conf, nats.token, t3-session.json; `nats.service`; the reminder timer). The operator asked for it to fold completely.

## Proposal
Runs alone after 022 is `done`, like 005 (a barrier). It is a checklist: the lead does the k16 and repo steps, and the Mac steps go to the operator.

**Where every file goes:**

| command-center | goes to |
|---|---|
| `tools/t3_dispatch.py`, `repo_sync.py`, `prune_worktrees.py`, `reset_reminder.py` | bmesh (022, 020) |
| `.agents/skills/prune`, `repo-sync` | `buildermesh:prune`, `buildermesh:sync` (022) |
| `tools/pr_watch.py`, skill `pr-comment` | a new `github` plugin in rickdiculousli-tools, released under that repo's own done rule |
| skill `tooling-editor` | rickdiculousli-tools' AGENTS.md, using `bmesh sync` |
| `.claude/hooks/this-box.sh` | the plugin's hook (022) |
| `fleet/bmesh/*.toml` | each box's `<config dir>`, already installed; a redacted copy in that box's `notes.md` as the backup |
| `fleet/nats/*.conf`, nats.token | `~/.config/bmesh/` on each box, with `nats.service` and the Mac's leaf plist pointed there |
| `fleet/systemd/*`, `fleet/mac/*` | bmesh's unit comes from `init --service`; the rest are recorded in the box's `notes.md` as installed |
| `fleet/machines.md`, `k16-bringup.md`, `maintenance.md`, `notifications.md`, `setup-a-box.md` | box facts to each box's `notes.md` (k16's by the lead, the Mac's by the operator); what holds for any fleet is already in README "Several boxes" (022) |
| the Ghidra pin procedure, the k16 git hub | k16's `notes.md` (ghidra-mcp is pinned to an upstream release and takes no commits) |
| `docs/projects/openGym*.md` | `<config dir>/notes/openGym.md` on k16 |
| `projects.md`, `docs/superpowers/` (apart from the two design specs), `docs/leftover-quota.html`, `.bmesh/`, `tests/` | the archived GitHub repo only |
| the two buildermesh design specs | buildermesh `docs/design/` (022) |

**On k16, by the lead:**
- Move `nats.conf`, `nats.token` and `t3-session.json` to `~/.config/bmesh/`. Edit `nats.service` and `config.toml`'s `token_file`, then restart.
- Set `[quota] remind_topic = "quota-reset"`, and disable and remove the reminder timer and its service.
- Write k16's `notes.md`.
- Remove the command-center line from `~/.claude/CLAUDE.md`.
- Edit the `command-center` row out of the registry (`bmesh repos push`), and archive its T3 project.
- Check that `bmesh hosts`, the t3 runner, a reminder and `bmesh sync --all` all work with `~/.config/command-center` moved aside.

**Needs from the operator** (below) holds the Mac's part.

Out: the GitHub repo's fate, which is the operator's.

Judge: opus, because the steps are listed, but each move has to be checked live on a running box.

## What you'll see
`ls ~/repos` on k16 has no command-center. `~/.config/command-center` is gone. `bmesh hosts --notes` shows what `fleet/machines.md` did. Every service still runs.

## Verification
No verify covers this; the lead checks each row of the table and each k16 step by hand and records it in the spec's closing commit. rickdiculousli-tools' own verify runs for the `github` plugin.

## Risks and the alternative considered
Moving the NATS token while `nats.service` runs can drop the hub for a few seconds. Restart it once, between leads.

Keeping command-center as a docs-only repo leaves a second checkout every box must have. Box notes in XDG, travelling in `bm-hosts`, replace it.

## Needs from the operator
On the Mac:
- Move `~/.config/command-center/nats.*` to `~/Library/Application Support/bmesh/` (or the kept `~/.config/bmesh`), edit the leaf plist and reload it.
- Write the Mac's `notes.md`.
- Remove the command-center line from `~/.claude/CLAUDE.md`.
- Run `bmesh repos remove command-center --checkouts --apply` (the admin's; it raises the Keychain dialog).
- Then archive or delete `rickdiculousli/command-center` on GitHub.
