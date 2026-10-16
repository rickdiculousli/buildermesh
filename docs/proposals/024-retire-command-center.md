---
id: 024
title: Retire command-center: every file and rule to its new home, then the repo goes
kind: maintenance
size: M
judge: opus
status: rejected
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
reason: superseded by 039 and 040 and 041
---

## Opportunity
Once 019 through 023, 029 and rickdiculousli-tools' 001 are done, and both boxes run T3 0.1.0, nothing in bmesh needs command-center. It is still a checkout on every box:
- AGENTS.md and the user-level CLAUDE.md still send sessions there.
- It holds the only copies of the box configs, the NATS files, the fleet notes and the per-repo rules in `projects.md` ("TCGCharting: never dispatch work here", "pstack-claude: never public").
- Live config still points into it. k16's `~/.config/bmesh/config.toml:10` and the Mac's set `[runner] t3_dispatch = "~/repos/command-center/tools/t3_dispatch.py"`, and `nats.service` and the Mac's leaf plist read `~/.config/command-center/`.
- On k16 its `cache/` holds 896 MB, and four T3 worktrees sit on unmerged branches.

The operator asked for it to fold completely.

## Proposal
Adjust (2026-10-16): also: the nats conf and token sit under ~/.config/command-center only because they predate bmesh. Move them to bmesh's config dir as README Several boxes describes and say so plainly in the
Adjust (2026-10-16): tweak: the operator is already on the new T3 so drop the wait for both boxes to report protocol version two and remove the V1 path straight away. The operator doesn't use pr-comment or tooling-editor or pr_watch.py: they go to the archive with the repo and the spec no longer waits on rickdiculousli-tools' github plugin spec. On NATS: bmesh embeds a hub only for a single box and a two-box mesh runs its own nats-server
Runs alone after 023, 029 and rickdiculousli-tools' 001 are `done` and both boxes' T3 are on 0.1.0 (a barrier, like 005). It is a checklist: the lead does the k16 and repo steps, and the Mac's go to the operator.

**First, what can't be recovered from git.** The lead lists, for the operator to keep or drop, before anything else moves:
- `cache/`, by directory and size (`research/t3code` 896 MB, `dispatch`, `propose`, `pr-watch`, `quota`, `reset-reminder`, `opengym-312`, `leftover-quota`);
- the four T3 worktrees on unmerged branches (`mac-pickup-readiness`, `lanes-part-3-dry-run`, `build-lanes-go-part1`, `lanes-landing-gate-judge`).

It waits for the answer through `bmesh ask`. `pr-watch/` moves to the `github` plugin's state dir if kept and `pr_watch.py` moved there.

**Where every file and rule goes:**

| command-center | goes to |
|---|---|
| `tools/t3_dispatch.py` and its tests | deleted, with the `t3` runner's V1 path (below); 029's V2 client replaces it |
| `reset_reminder.py`, `.claude/hooks/this-box.sh` | bmesh (022) |
| `tools/repo_sync.py`, `prune_worktrees.py`; skills `repo-sync`, `prune` | bmesh `sync`, `prune`; `buildermesh:sync`, `buildermesh:prune` (020, 023, 022) |
| skills `pr-comment`, `tooling-editor` | rickdiculousli-tools' `github` plugin and AGENTS.md (its 001) |
| `tools/pr_watch.py` | T3's `watch_pull_request` if it covers what openGym 312's tracker uses (the head moving, replies on our review threads, resolved or outdated threads); only otherwise the `github` plugin |
| `projects.md` "What a row can't say" | each rule into that repo's AGENTS.md (gametrainer, rickdiculousli-tools, Sparks, openGym's tracker note, eggnogg-rework); for a repo that takes no commits (TCGCharting, ghidra-mcp) or has no AGENTS.md (pstack-claude), into `~/.claude/CLAUDE.md` on both boxes; buildermesh's post-land steps are already its `[update]` |
| AGENTS.md conventions (anchor out-of-repo paths at `~` or `/`; name the box when a file lives on one) | `~/.claude/CLAUDE.md` on both boxes |
| `fleet/bmesh/*.toml` | each box's `<config dir>`, already installed; a copy with secrets removed in that box's `notes.md` |
| `fleet/nats/*.conf`, nats.token | `~/.config/bmesh/` on each box |
| `fleet/systemd/*`, `fleet/mac/*` | recorded as installed in each box's `notes.md`; bmesh's own unit comes from `init --service` |
| `fleet/machines.md`, `k16-bringup.md`, `maintenance.md`, `notifications.md`, `setup-a-box.md` | box facts into each box's `notes.md` (k16's by the lead, the Mac's by the operator); what holds for any fleet is README "Several boxes" (022) |
| Ghidra pin procedure, the k16 git hub | k16's `notes.md` |
| `docs/projects/openGym*.md` | `<config dir>/notes/openGym.md` on k16 |
| the two buildermesh design specs | buildermesh `docs/design/` (022) |
| everything else: `projects.md` modes prose, other `docs/superpowers/`, `docs/leftover-quota.html`, `.bmesh/`, `tests/` | the archived GitHub repo only |

**On k16, by the lead:**
- **The V1 path, in buildermesh.** Once both boxes' T3 report `orchestrationProtocolVersion` 2, the lead removes the `t3` runner's V1 path (029): the `t3_dispatch.py` call, `[runner] t3_dispatch` (an old config that still sets it loads, with a warning that it is ignored), `BMESH_T3_COMMAND` and the V1 tests. A protocol-1 server is then refused with `bmesh: T3 <version> speaks protocol 1; bmesh needs T3 0.1.0 or later`.
- **Config and services.**
  - Delete `[runner] t3_dispatch` from `~/.config/bmesh/config.toml`, and delete `~/.config/command-center/t3-session.json` (V1's token; 029's sign-in lives in `<config dir>/t3-token.json`).
  - Move `nats.conf` and `nats.token` to `~/.config/bmesh/`, point `nats.service` and `[mesh] token_file` there, and restart NATS once, between leads.
  - Remove the reminder timer and service (022 already turned them off).
- **Rules and notes.**
  - Move the per-repo rules as the table says.
  - Edit eggnogg-rework's AGENTS.md proposals block (line 58 still points at `command-center/templates/proposal.md`) and commit it to its hub.
  - Write k16's `notes.md` and remove the command-center lines from `~/.claude/CLAUDE.md`.
  - Update this project's memory note `project-k16-config-from-command-center` to say where k16's config lives now.
- **The check.** With both `~/.config/command-center` and `~/repos/command-center` moved aside, confirm that `bmesh hosts`, a t3 `lead start` against the live V2 server (a one-line demo lead, then `node forget`), a reminder, `bmesh sync --all` and `bmesh prune --all` work. Only then, on the operator's word, delete the moved-aside copies.
- **The registry.** Leave the `command-center` row in place: the Mac's `repos remove` below needs it.

Out: the GitHub repo's fate, which is the operator's.

Judge: opus, because the steps are listed, but each move has to be checked live on a running box.

## What you'll see
`ls ~/repos` on both boxes has no command-center, and `~/.config/command-center` is gone. `bmesh hosts --notes` shows what `fleet/machines.md` did. TCGCharting's "never dispatch here" is in `~/.claude/CLAUDE.md`. Every service still runs.

## Verification
No verify covers this. The lead checks each table row and each k16 step by hand, records them in the spec's closing commit, and runs the live check above.

## Risks and the alternative considered
Moving the NATS token while `nats.service` runs drops the hub for a few seconds; restart it once, between leads.

Keeping command-center as a docs-only repo leaves a second checkout every box must have. Box notes in XDG, travelling in signed presence, replace it.

## Needs from the operator
On the Mac:
- Delete `[runner] t3_dispatch` from `~/.config/bmesh/config.toml` and `~/.config/command-center/t3-session.json`.
- Move `~/.config/command-center/nats.*` to `~/.config/bmesh/`, edit `io.nats.leaf.plist` and the bmesh `token_file`, and reload the leaf.
- Write the Mac's `notes.md`, and move the conventions and rules into the Mac's `~/.claude/CLAUDE.md`.
- Answer the lead's keep-or-drop list.
- Run `bmesh repos remove command-center --checkouts --apply` (the admin's; Keychain dialog).

Then archive or delete `rickdiculousli/command-center` on GitHub.
