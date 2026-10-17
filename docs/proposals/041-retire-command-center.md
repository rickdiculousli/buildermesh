---
id: 041
title: Retire command-center: its last files and rules to their homes, then the repo goes
kind: maintenance
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
built_by: fca6fddf-cc75-4fe1-8f42-1118396f8fac
host: k16
family: buildermesh-build-7
branch: bmesh/fca6fddf-cc75-4fe1-8f42-1118396f8fac
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

Scope grew (the operator, 2026-10-17, during the build): re-scaffold gametrainer, party-buzzer and rickdiculousli-tools, whose `AGENTS.md` still had `command-center:` blocks, with `bmesh scaffold`, each verified by its row and pushed; computer-use, which lives only on the Mac, the coordinator re-scaffolds there.

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

## Built on k16 (2026-10-17)
**Keep or drop.** k16's `cache/` (896 MB: `research/t3code` 896M, a clone of public `pingdotgg/t3code`; `dispatch` 436K; `propose` 72K; `opengym-312` 44K; `leftover-quota`, `pr-watch`, `quota`, `reset-reminder` 8K each) and its four T3 worktrees (`mac-pickup-readiness`, `lanes-part-3-dry-run`, `build-lanes-go-part1`, `lanes-landing-gate-judge`: all clean, every commit already on command-center's `main`, so not "unmerged" after all). The operator: drop all, the Mac's `cache/` and its 16 worktrees too.

**The table, row by row.**
- `projects.md` per-repo rules: below.
- AGENTS.md conventions: "anchor out-of-repo paths at `~` or `/`, name the box" is k16's `~/.claude/CLAUDE.md` "Replies"; "which box you're on" was already its "This box" and the session hook (022).
- `fleet/*.md`, the Ghidra pin procedure, the k16 git hub: k16's `~/.config/bmesh/notes.md` (8.7 KB; `bmesh hosts --notes` prints it): hardware, access, every service as it runs since 040 (`bmesh-hub`, not `nats.service`), Ghidra and the pin move (now a registry publish and `bmesh sync`, not `repo_sync.py`), the git hub, repos and toolchains, limits and upkeep. What holds for any fleet was already README "Several boxes"; the bring-up log, the setup runbook and the purchase notes stay in the archive.
- `fleet/bmesh/*.toml`, `fleet/systemd/*`, `fleet/mac/*`: recorded as installed in k16's notes; k16's `config.toml` is ahead of `k16.toml` and is the source. `fleet/nats/*` went with 040.
- `docs/projects/openGym*.md`: `~/.config/bmesh/notes/openGym.md` and `openGym-312-review.md`, the posted comment bodies from `cache/opengym-312/` beside them in `openGym-312-comments/`; openGym's untracked `CLAUDE.local.md` points there.
- `tools/pr_watch.py`, `reset_reminder.py`, skills `pr-comment` and `tooling-editor`, the rest: the archived repo only.

**`projects.md`, rule by rule.**
- Modes: companion repos stay on the Mac, never cloned elsewhere without a decision, done by their own filing rules (diy's README status, decision log and open questions included); a fork's work goes upstream and its tracker lists what to do; a dormant row isn't dispatched without asking: k16's `~/.claude/CLAUDE.md` "Repos that need care". Ship and publish done rules: each repo's scaffolded definition of done and its row's verify.
- Proposals and the registry paragraphs: the README (Proposals, The registry) and each ship repo's scaffolded blocks.
- `gametrainer`'s `node tests/test_workbench.cjs` after a build: already in its AGENTS.md (Build line).
- `command-center`'s own docs rule: retired with it.
- `buildermesh`'s install and restart after landing: obsolete; k16 updates itself (`[update] repo`), as this repo's AGENTS.md says.
- `rickdiculousli-tools`' `docs/verification.md`: already in its AGENTS.md.
- `eggnogg-rework`: `prepare.py`, the local hub, nothing public and the Mac-only native runs were already in its AGENTS.md; dormant is its row's `mode`; its proposals block pointed at `command-center/templates/proposal.md` and now names `bmesh spec template` and `docs/proposals/README.md` (eggnogg-rework `566095c`, pushed to its hub).
- `Sparks-of-a-Tempering-Mind`: the publish command is its row's verify; "`site/` matches what's live" is the operator's, below (the repo is only on the Mac).
- `openGym`, `TCGCharting`, `ghidra-mcp`, `pstack-claude`: k16's `~/.claude/CLAUDE.md` "Repos that need care".
- The standard closing for ship threads: `bmesh build`'s brief carries it.

**k16 steps.**
- `~/.claude/CLAUDE.md`: the "central coordinator repo" line is now a pointer to the box notes and the registry; no command-center line is left.
- The memory note `project-k16-config-from-command-center` became `project-k16-config-is-the-source`, with no command-center line.
- The reminder: 022's handover step had not been taken (`[quota] remind_topic` was unset, so the timer was still the only reminder). `remind_topic = "quota-reset"` was set in k16's `config.toml` and `bmesh.service` restarted (`remind: scheduled provider=claude`); then `command-center-reset-reminder.timer` was disabled and both units deleted. The 01:30Z window was posted by both, once.
- The check, with `~/repos/command-center`, `~/.config/command-center` and the worktrees moved aside: `bmesh hosts` read roster v3 with both boxes awake and k16's notes; `bmesh sync --all` levelled nine repos; `bmesh prune --all` listed 106 worktrees with no error and no command-center row; a t3 `lead start` of a one-line demo lead (`buildermesh-041-demo`, Haiku) started a T3 thread that ran `bmesh` in its worktree, and `node forget --worktree` removed node, worktree and branch. Every unit stayed active (`t3code`, `bmesh-hub`, `bmesh`, `ntfy`, `ghidra-mcp`). On the operator's word the moved-aside copies (915 MB) were deleted.
- After the check, `~/.config/command-center/t3-session.json` came back: an SSH session from the Mac ran command-center's `t3_dispatch.py` path at 00:22:49Z and issued a new T3 token "command-center dispatch". The three such sessions in k16's T3 (issued 2026-10-11, 10-16 and 10-17, never used) were revoked and the directory removed again.
- Re-scaffolds (scope grew): gametrainer `7220c3c`, party-buzzer `011cd78`, rickdiculousli-tools `3e691f8`, each `bmesh scaffold` turning the `command-center:` blocks into `bmesh:` ones, its row's verify recorded ok (party-buzzer's first run failed on a flaky test and passed on the re-run), pushed.
- Left: the registry row, `~/.bmesh/checkouts/command-center` (the `repos remove` below takes it: k16 answers `absent` and removes its routine checkouts), and k16's T3 project `command-center` with its old threads (the app's to remove, if wanted).

**The Mac's steps (the operator's).**
1. Before anything else, stop running command-center's tools (`repo_sync.py`, `prune_worktrees.py`, the `repo-sync` and `prune` skills): they SSH to k16 and issue T3 tokens there. `bmesh sync` and `bmesh prune` replace them.
2. Write `~/.config/bmesh/notes.md`: the Mac's facts from `fleet/machines.md` ("Remote access to `ricks-macbook`", as 040 left it: the leaf is `bmesh hub`) and `fleet/notifications.md` (the ntfy subscriber and growlrrr).
3. Copy the "Repos that need care" and "Replies" sections of k16's `~/.claude/CLAUDE.md` into the Mac's, and replace its "central coordinator repo" line as k16's was.
4. Add to Sparks-of-a-Tempering-Mind's AGENTS.md: after `uv run _tools/publish.py <draft>`, `site/` matches what's live.
5. `bmesh sync --all`, to bring the Mac's gametrainer, party-buzzer and rickdiculousli-tools up to the re-scaffolds.
6. Drop the Mac's command-center `cache/` and its 16 worktrees, then `bmesh repos remove command-center --checkouts --ignored-ok --apply` (`--ignored-ok` because `cache/` and `__pycache__` are ignored files the remove otherwise refuses).
7. Archive or delete `rickdiculousli/command-center` on GitHub.
