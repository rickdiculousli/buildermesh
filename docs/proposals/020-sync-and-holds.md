---
id: 020
title: Boxes keep each other level: sync, plugin refresh and approval holds
kind: feature
size: L
judge: opus
status: accepted
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
Several repos are checked out on more than one box, and some are plugins every session loads from its checkout: buildermesh, pstack from `~/repos/pstack-claude/plugin`, and rickdiculousli-tools (Claude reads a folder marketplace in place; repo_sync.py's docstring). Keeping them level is command-center's `repo_sync.py` (336 lines), driven over SSH from the one box that reaches all the others. When leads land on k16, the Mac's checkouts and plugins lag until someone runs repo-sync on the Mac. With 019 every box hears signed jobs. This spec makes a change made on any box reach every box, whichever box drives it, without letting one box put code on another that the operator hasn't seen.

## Proposal
Builds on 019 (`done` first). Nothing here runs unless `[hosts] enabled = true`.

**One lock per repo.** `level`, the daemon's routine rescan fetch (routines.go `discover`) and a lead's landing (`merge --ff-only` in the main checkout) all take `<state dir>/locks/<repo>.lock` (a flock, as `selfupdate.Lock` does), so they never collide on `index.lock` or a ref lock.

**`level <repo> <sha>`**, a built-in op, judged by the receiving box alone:
- **The target.** The box runs a plain `git fetch origin` itself. The target is `origin/<current branch>`, or the row's `pin` tag for a pinned row. The job's sha only says "at least this": it must be an ancestor of the target, and is refused otherwise (`refused: <sha> not on origin/<branch>`). A side branch or a fork's commit can't be named.
- **The checkout check.** The box moves nothing and answers `refused: dirty|other branch|diverged|in progress` when its checkout has uncommitted or untracked changes, is on another branch, has diverged, or has a `MERGE_HEAD`, a `rebase-*` directory or an `index.lock`.
- **Behind only.** The box fast-forwards, except as held below.
- **Ahead only.** The box pushes only when all of these hold:
  - the row's mode is in `[hosts] push_modes` (default `["ship", "fork"]`);
  - the newest local commit is at least 10 minutes old.
  
  It then pushes the branch and the tags that point at the pushed commits (`git push origin <branch> <tags…>`; lightweight tags included, as pstack's `update.sh` makes). Otherwise it answers `ahead N, not pushed: <why>`.
- **Plugin refresh.** After the checkout moves, the box refreshes what it loads from it: `claude plugin update` for each lagging user-scope install whose marketplace is that checkout, and `codex plugin add` for the Codex copies when `codex` is on PATH.

**The hold.** On a secure box, a `level` that would move a repo sessions load is held for the operator, whether it arrived as a job or came from the box's own self-level. Such a repo is either:
- a checkout that is a Claude Code or Codex marketplace source (`.claude-plugin/marketplace.json`, or a Codex install from it);
- the `[update] repo`.

How a hold works:
- The job waits in `<state dir>/held/<id>.json` with `git log --stat HEAD..<target>` (job, target sha, diff summary).
- `bm.daemon.<host>.held` is published, so the phone hears `ricks-macbook holds: pstack-claude +3 commits (k16)`.
- `bmesh hosts held` lists the holds. `bmesh hosts approve <id>` shows the log again, raises the announced Keychain dialog for the approval key (019) and signs `approve/v1` over the job's hash. The daemon re-checks the job's signature, that its sender is still in the roster, and that the target hasn't moved, then levels. If the target moved, it shows the new log and asks again.
- `bmesh hosts deny <id>` drops it. A hold expires with its job (24h), or after 24h for a self-level.
- On a box that isn't secure, nothing is held.

**`bmesh sync [--repo R | --all] [--wait 30s]`:**
- It levels this box first, which pushes if it is ahead and allowed to.
- It then sends `level` with origin's new head to every roster box whose `accept` has `level` and whose signed presence lists the repo. Jobs live 24h.
- It prints each reply that arrives within `--wait` (`k16: fast-forwarded 3`, `ricks-macbook: held for approval`, `ricks-macbook: asleep, queued until <expires>`).
- It works the same from any box, because each box judges its own checkout against origin.

**Self-level.** Each daemon with `[hosts] enabled` levels every registered repo it has, on start and on wake (019), as a fast-forward only, never a push. Refusals are logged, not pushed. A Mac that was asleep therefore catches up as soon as it opens. Holds apply as above.

**Pushes.** A refusal or failure of a received `level` that no caller is waiting for (an asleep box answering later) is pushed once, through the push claim 019 uses. Two boxes ahead at once leave the second push refused as not a fast-forward, and the phone hears `diverged`.

**The `buildermesh:sync` skill** (022 writes it) is the chat entry point.

Out:
- prune, the registry, removing checkouts and agent requests (023);
- running any command named by a job or the registry;
- merging diverged checkouts (refused, as repo_sync refused them).

Judge: opus, because the refusal rules come from repo_sync, but the hold, the lock and a box waking mid-sync need judgment.

## What you'll see
On k16, after a lead lands on buildermesh and a pstack fix is pushed, `bmesh sync --all` prints `k16: up to date` for both and `ricks-macbook: asleep, queued until tomorrow 09:12`. When the Mac opens, its other repos fast-forward within a minute. The phone shows `ricks-macbook holds: pstack-claude +1 commit`, and after `bmesh hosts approve` at the Mac, `pstack@pstack-claude` shows the new version. On the Mac, `bmesh sync` pushes what the Mac committed and k16 fast-forwards. A single-box user's daemon never pushes or pulls anything.

## Verification
The repo's verify. New tests against `meshtest` with two daemons as hosts:
- `level`, one test per refusal, plus a sha on a side branch and a pinned tag;
- every condition of the push gate, a lightweight tag pushed with its commit, and the lock against a concurrent landing;
- a queued `level` run when the box starts;
- a self-level on wake that never pushes;
- on a secure box: a hold, approve, deny, a target that moved, and expiry;
- plugin refresh against fake `claude` and `codex` (a new `BMESH_CODEX_COMMAND`), and no codex on PATH;
- `[hosts] enabled = false` does nothing.

## Risks and the alternative considered
A box that isn't secure takes whatever origin has, as a `git pull` would. A box that can write to origin can therefore reach it, and the operator marks a box secure where that matters. A held level waits for the operator. Plugin repos on a secure Mac move a little later as a result, which is the point.

Alternatives:
- Every peer message as an agent request: slow, costs quota, and isn't deterministic for a fast-forward.
- A command list in the registry: any roster box could then run shell on every box.

## Needs from the operator
Approve holds at the Mac (`bmesh hosts held`, `bmesh hosts approve <id>`).
