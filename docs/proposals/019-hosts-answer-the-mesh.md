---
id: 019
title: Every box answers the mesh: sync, prune and nix repos from any box
kind: feature
size: L
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-15
---

## Opportunity
Keeping repos level and clean across boxes works only in the direction SSH happens to go. command-center's `repo_sync.py` and `prune_worktrees.py` reach each box with `ssh <host> bash -lc`, so they run only from a box that can SSH to every other one. In a setup like this fleet's, where the Mac reaches k16 but k16 can't reach the Mac, that box is the Mac (command-center AGENTS.md: "run it from the Mac; k16 can't SSH to the Mac"). Leads run and land on k16, so the Mac's checkouts lag until someone opens a session there. That includes the plugins every session loads from checkouts (buildermesh, pstack from `~/repos/pstack-claude/plugin`, rickdiculousli-tools). bmesh has the same blind spot. `ideate` reaches the other host over ssh, and `node forget --worktree` refuses a worktree on another host with `run it there`. Yet every box on the mesh already hears every subject: the Mac is a leaf of k16's hub. Nothing on the Mac listens, though, because no daemon runs there.

## Proposal
In:
- **Opt-in at setup.** `bmesh init`'s multi-box answer asks one more question: `Let other boxes on the mesh sync, prune and remove repos on this one? [y/N]`. Yes writes `[hosts] accept = ["sync", "prune", "nix"]`; the default `[]` keeps the box deaf. Re-setup is `bmesh init --force`, which now offers the existing `config.toml`'s values as each question's default, so answering again changes only what is answered differently. A single box never asks.
- **A daemon on every box that accepts.** `bmesh serve` on a box with a remote hub runs as a host agent: no routines unless `[runner] checkouts` is set, and the same unit `init --service` already writes (launchd on macOS). Every daemon keeps `bm-hosts` (KV, `<host>` to `{host, os, rev, accept, repos: [names whose path exists here], seen}`), refreshed each minute. `bmesh hosts [--json]` prints it, with `asleep` for a box not seen for 3 minutes.
- **Host jobs** go over a work-queue stream, `bm-host-jobs`, on subject `bm.host.<host>.job`. A job carries `{id, from, kind, repo, args, dry_run, expires}`, and a sleeping box takes it on wake unless it has expired (`--ttl`, default 1h). The reply goes to `bm.host.<host>.reply.<id>` in `bm-mesh`. A daemon runs a kind only when the kind is in its `accept`; any other kind is answered `refused: <host> does not accept <kind>`. No SSH is involved anywhere.
- **`bmesh sync [--repo R | --all] [--hosts a,b] [--dry-run] [--wait 30s]`** is `repo_sync.py` in Go, and it gives the same answer whichever box runs it. A `state` job reports each box's branch, head, ahead, behind and dirty files. The caller plans with repo_sync's rules: the one box that is ahead pushes, the rest fast-forward, and nothing changes when a box is dirty, has diverged, is on another branch, or when two boxes are ahead. A pinned row moves every box to its tag. The caller then sends `push` and `ff` jobs. After a box's checkout moves, it refreshes the plugins it loads from that checkout. When the checkout is a Claude Code marketplace (`.claude-plugin/marketplace.json`), that means `claude plugin update` for the user-scope installs that lag, and `codex plugin add` for the Codex copies. A row key `after_sync = []` names any further commands. A box that doesn't answer within `--wait` is reported `asleep: queued until <expires>`, and its jobs run on wake. That ordering is safe because `ff` is fast-forward only.
- **`bmesh prune [--repo R | --all] [--hosts] [--apply]`** is `prune_worktrees.py` in Go with the same keep and remove rules, run as a `prune` job on each box. Each box asks its own runner about thread state.
- **Nix.** `bmesh repos remove <name> [--checkouts] [--apply]` drops the registry row. With `--checkouts`, a `nix` job deletes the checkout on every box that has one. A box refuses (naming the reason, touching nothing) when its checkout is dirty, has commits that aren't on `origin`, or has a worktree whose branch isn't merged.
- `node forget --worktree` on another host's node sends that host a `prune` job for the one worktree instead of refusing.

Out: starting leads or rounds on another box over the mesh (`ideate` and `build` keep ssh); NATS leaf setup, which a box keeps as it has it; folding command-center (020).

Judge: opus, because the planning rules and refusals are fixed by repo_sync, but job expiry, a box waking mid-sync and the opt-in path need judgment.

## What you'll see
On k16, after a lead lands on buildermesh and a pstack fix is pushed, `bmesh sync --all` prints `k16 ahead, pushed; ricks-macbook asleep: queued until 15:40`. When the Mac wakes, `bmesh hosts` shows it seen, its checkouts are at the new heads and `pstack@pstack-claude` is at the new version. `bmesh repos remove TCGCharting --checkouts --apply` removes the checkout on both boxes, or names the box that refused and why. A box set up single, or with the question answered no, never runs anything it is sent.

## Verification
The repo's verify. New tests, against `meshtest` with two daemons named as hosts:
- the sync plan, one test per refusal;
- a job queued for a stopped daemon runs after it starts and is dropped after `expires`;
- an un-accepted kind is refused;
- `nix` refuses a dirty checkout, an unpushed one and one with an unmerged worktree;
- `init --force` keeps the earlier answers as defaults;
- plugin refresh against a fake `claude` and `codex` (`BMESH_CLAUDE_COMMAND`, a new `BMESH_CODEX_COMMAND`).

## Risks and the alternative considered
Alternatives: a reverse SSH tunnel from the Mac to k16, or Tailscale SSH in both directions. Both widen access to the box. The mesh's own transport needs nothing new on the network and survives sleep. The risk is that a queued `ff` lands after the operator has changed something on the Mac. The job re-checks the checkout before it moves it, under the same rules, and refuses as repo_sync would.

## Needs from the operator
