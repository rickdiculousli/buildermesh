---
id: 041
title: Retire command-center: its last files and rules to their homes, then the repo goes
kind: maintenance
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
bmesh no longer needs command-center once 039 (the t3 runner drops V1 and `t3_dispatch.py`) and 040 (`bmesh hub` runs the NATS hub and leaf) are done. The repo is still checked out on both boxes and still holds things nothing else does:
- the per-repo rules in `projects.md` ("TCGCharting: never dispatch work here", "ghidra-mcp: pinned to a release", "pstack-claude: never public", eggnogg-rework's local hub and Mac-only runs);
- the box notes in `fleet/` (`machines.md`, `k16-bringup.md`, `maintenance.md`, `notifications.md`, `setup-a-box.md`, `systemd/`, `mac/`, `bmesh/`);
- AGENTS.md conventions, and `~/.claude/CLAUDE.md` on both boxes, which still call it "the central coordinator repo";
- `cache/` (896 MB on k16, 560 KB on the Mac) and T3 worktrees on unmerged branches (4 on k16, 16 on the Mac).

This supersedes 024, which tied the V1 removal and the NATS move to this; those are now 039 and 040. The operator doesn't use the PR tools (`pr-comment`, `tooling-editor`, `tools/pr_watch.py`), so they stay in the archive with the rest.

## Proposal
Runs alone after 039 and 040 are `done` (a barrier, like 005). A checklist: the lead does k16 and the repos, the Mac's steps go to the operator.

**First, what git can't bring back.** The lead lists `cache/` by directory and size and every worktree on an unmerged branch, per box, and waits for the operator's keep or drop through `bmesh ask` before anything moves.

**Where each thing goes:**

| command-center | goes to |
|---|---|
| `projects.md` per-repo rules | that repo's AGENTS.md (gametrainer, rickdiculousli-tools, Sparks, eggnogg-rework, openGym's tracker note); for a repo that takes no commits (TCGCharting, ghidra-mcp) or has no AGENTS.md (pstack-claude), `~/.claude/CLAUDE.md` on both boxes |
| AGENTS.md conventions (anchor out-of-repo paths at `~` or `/`; name the box when a file lives on one) | `~/.claude/CLAUDE.md` on both boxes |
| `fleet/*.md`, Ghidra pin procedure, the k16 git hub | box facts into each box's `notes.md` (k16's by the lead, the Mac's by the operator); what holds for any fleet is already README "Several boxes" |
| `fleet/bmesh/*.toml`, `fleet/systemd/*`, `fleet/mac/*` | already installed by `init` and 040; recorded as installed in each box's `notes.md` |
| `docs/projects/openGym*.md` | `<config dir>/notes/openGym.md` on k16 |
| `tools/pr_watch.py`, skills `pr-comment`, `tooling-editor`, `reset_reminder.py`, everything else | the archived GitHub repo only |

**On k16, by the lead:**
- Move the rules and conventions as the table says; edit eggnogg-rework's AGENTS.md proposals block (it points at `command-center/templates/proposal.md`) and commit it to its hub.
- Write k16's `notes.md`; remove every command-center line from `~/.claude/CLAUDE.md` (the "central coordinator repo" line included) and from the project memory note `project-k16-config-from-command-center`.
- Remove the reminder timer and service (022 turned them off).
- **The check.** With `~/repos/command-center` and `~/.config/command-center` moved aside, confirm `bmesh hosts`, a t3 `lead start` (a one-line demo lead, then `node forget`), `bmesh sync --all` and `bmesh prune --all` work. Only then, on the operator's word, delete the moved-aside copies.
- Leave the registry's `command-center` row: the Mac's `repos remove` below needs it.

Out: the GitHub repo's fate, which is the operator's.

Judge: opus, because each step is listed but has to be checked live on a running box.

## What you'll see
`ls ~/repos` on both boxes has no command-center, `~/.config/command-center` is gone, `bmesh hosts --notes` shows what `fleet/machines.md` did, and TCGCharting's "never dispatch here" is in `~/.claude/CLAUDE.md`. Every service still runs.

## Verification
No verify covers this. The lead checks each table row and k16 step by hand, records them in the spec's closing commit, and runs the live check above.

## Risks and the alternative considered
A rule lost in the move is silent: the lead diffs `projects.md`'s rule lines against where each landed and lists them in the closing commit.

Keeping command-center as a docs-only repo leaves a second checkout every box must have; box notes in the config dir, travelling in signed presence, replace it.

## Needs from the operator
On the Mac: answer the keep-or-drop list; write the Mac's `notes.md`; move the conventions and rules into the Mac's `~/.claude/CLAUDE.md` and remove its command-center lines; run `bmesh repos remove command-center --checkouts --apply` (the admin's; Keychain dialog). Then archive or delete `rickdiculousli/command-center` on GitHub.
