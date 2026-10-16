---
id: 020
title: Boxes act for each other: sync, prune, nix and agent requests
kind: feature
size: L
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
Several repos are checked out on more than one box. Some are plugins every session loads from its checkout: buildermesh, pstack from `~/repos/pstack-claude/plugin`, and rickdiculousli-tools. Keeping them level and clean is command-center's `repo_sync.py` (336 lines) and `prune_worktrees.py` (257). Both reach each box over SSH, so they run only from a box that can SSH to every other one. When leads land on k16, the Mac's checkouts and plugins lag until someone runs repo-sync on the Mac. `node forget --worktree` refuses another host's worktree with `run it there`. With 019, every box hears signed jobs. This spec decides what a box will do when it gets one, so that a change made on any box reaches every box whichever box drives it.

## Proposal
Builds on 019 (`done` first). Two tiers.

**Tier 1: operations built into the binary.** A job names an op, a repo and, for `level`, a commit. It never carries a command, and `repos.toml` has no command key that any op runs. The receiving box runs its own code, and only the ops in its `[hosts] accept`.
- **`level <repo> <sha>`.**
  - The box fetches `origin` itself, and the job's sha must be on origin. A sender can't bring in commits; it can only make the box catch up with origin.
  - The box refuses (`refused: dirty|other branch|diverged|not on origin`) and moves nothing when its checkout has uncommitted changes, is on another branch or has diverged.
  - When the box is only ahead, it pushes with `--follow-tags`; when it is only behind, it fast-forwards. A pinned row moves to its tag, which must be on origin.
  - After the checkout moves, the box refreshes what it loads from it: `claude plugin update` for every lagging user-scope install whose marketplace is that checkout, and `codex plugin add` for the Codex copies when `codex` is on PATH.
- **`prune [<repo>] [<worktree>]`** follows `prune_worktrees.py`'s keep and remove rules, judged by this box's own runner and nodes.
- **`registry`.** The box takes a sent `repos.toml` only when its own loader parses it, keeping the old one as `repos.toml.prev`. A file it can't read is refused, and the refusal is pushed.
- **`nix <repo> <path>`** needs the admin's signature (019, check 1).
  - The box deletes the checkout at the path the job names. It refuses when the checkout has a ref not on any remote (`git log --branches --tags --not --remotes`), a stash, ignored files (unless `--ignored-ok`), no origin, or a worktree whose branch isn't merged.
  - On success the box drops its own registry row.

**Tier 2: agent requests.**
- **`agent <repo> "<request>"`.** The box starts a session through its runner in a fresh worktree of the repo, with the request and who sent it as the brief. The session registers as node `<repo>-ask-<hex6>` and reports like a lead.
- **Integrity check first.** The repo's main checkout must be clean and level with origin; otherwise the request is refused.
- **A secure box holds every agent request for the operator.**
  - The request waits in `<state dir>/held/<id>` and is pushed to the phone (`k16 asks ricks-macbook: <request>`).
  - `bmesh hosts approve <id>` on that box starts it, after the Keychain dialog. `bmesh hosts deny <id>` drops it.
  - A held request expires with its job.
  - On a box that isn't secure, the request starts at once.

**Commands.**
- **`bmesh sync [--repo R | --all] [--wait 30s]`.**
  - It levels this box first, which pushes if it is ahead.
  - It then sends `level` with origin's head to every roster box that accepts `level` and has the repo, and prints the replies that arrive within `--wait`. The rest print as `asleep: queued until <expires>`; jobs live 24h.
  - It works the same from any box, because each box judges its own checkout against origin.
- **Each daemon levels its own repos** on start and when it wakes (a gap of 3 minutes or more between its minute ticks), so a Mac that was asleep pushes what it committed and catches up without being asked.
- **Pushes.** A refusal or failure that no caller is waiting for is pushed. Two boxes ahead at once leave the second push refused as not a fast-forward, and the phone hears `diverged`.
- **`bmesh prune [--repo R | --all] [--apply]`** sends `prune` to every roster box and prints each box's rows.
- **`bmesh repos push`** sends `registry` to every box that accepts it.
- **`bmesh repos remove <name> --checkouts [--apply]`** runs on the admin box. It sends `nix` with each box's path from its presence entry. The admin drops its own row only after every box has replied; otherwise it prints the boxes still pending.
- **`node forget --worktree`** on another host's node sends that host `prune` for the one worktree, waits for the reply, then forgets the node.
- **`bmesh hosts ask <host> --repo R "<request>"`** sends an agent request.

Out: starting builds or rounds on another box (`ideate` and `build` keep SSH; a signed `start` op is a later spec); running any command named by a job or the registry.

Judge: opus, because the refusal rules come from repo_sync, but a box waking mid-sync, redeliveries and the secure box's held requests need judgment.

## What you'll see
On k16, after a lead lands and a pstack fix is pushed, `bmesh sync --all` prints `k16: pushed buildermesh; ricks-macbook: asleep, queued until tomorrow 09:12`. When the Mac opens, it is level within a minute and `pstack@pstack-claude` shows the new version. On the Mac, `bmesh sync` makes k16 fast-forward. `bmesh hosts ask ricks-macbook --repo investments "rebuild the index"` from k16 reaches the phone as a request, and nothing starts on the Mac until it's approved there.

## Verification
The repo's verify. New tests against `meshtest` with two daemons as hosts:
- `level`, one test per refusal, plus a sha not on origin;
- a queued `level` run on the box's next start, and a self-level on wake;
- two boxes ahead give one `diverged`;
- every `nix` refusal, and the admin-only signature;
- the secure hold, approve, deny and expiry, and the non-secure immediate start;
- plugin refresh against fake `claude` and `codex` (a new `BMESH_CODEX_COMMAND`), and no codex on PATH;
- a registry the receiver can't parse is refused.

## Risks and the alternative considered
A box can still be made to run code it didn't have before, through a `level` that pulls a plugin change. That code is whatever is on origin, as it would be after any `git pull`, never what the sender wrote.

Alternatives:
- Every peer message as an agent request: slow, costs quota, and isn't deterministic for what is a fast-forward.
- A registry key of commands to run after a sync: it would let any roster box run shell on every box.

## Needs from the operator
Mark the Mac secure in `init --hosts` (019).
