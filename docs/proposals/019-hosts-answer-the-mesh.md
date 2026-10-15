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
- **Each box has an identity.** When a box opts in, `init` generates an Ed25519 key pair. The private key goes to `<config dir>/host.key` (mode 0600, never published). The fleet's public keys go to `<config dir>/trust.toml` (`[hosts.<name>] key, endorsed_by, added`). The first box to opt in trusts only itself.
- **A one-time credentials exchange.** A trusted box runs `bmesh hosts invite`, which prints a code: 128 random bits as text, good for 10 minutes and for one attempt. The new box's `init` asks for the code (or takes `--invite`).
  - The new box publishes `{host, key, ts, salt}` on `bm.pair.<inviter>`, with an HMAC-SHA256 over its canonical bytes. The HMAC key is derived from the code with HKDF-SHA256, salted with the inviter's name.
  - The inviter burns the code on the first attempt, whether it verifies or not, then checks the HMAC, `ts` and `salt`.
  - On success the inviter signs an endorsement of the new key. It answers with that endorsement and its `trust.toml`, under the same HMAC, so the new box knows the list came from the inviter.
  - The new box stores the list, and the inviter publishes the endorsement in `bm-hosts`. Every other box adds a key to its own `trust.toml` only when the key's endorsement is signed by a key it already trusts.
  - `bmesh hosts revoke <name>` publishes a signed revocation the same way, and every box drops that key. Re-setup with `--force` keeps `host.key`, `trust.toml` and every answer unless asked to rotate (`--new-key`, which needs a fresh invite).
- **Every job and reply is signed, deterministically.**
  - The signed bytes are the job's canonical encoding: its fields in a fixed order, each as length-prefixed UTF-8, with `to` (the target box), `ts` (RFC 3339 UTC, seconds) and `salt` (16 random bytes, hex) among them.
  - The signature is Ed25519 over SHA-256 of those bytes, carried in `sig` beside `by` (the signing box). Ed25519 signs deterministically (RFC 8032), so one job gives one signature on any box, and a test can pin the exact bytes.
  - A receiving box checks in a fixed order and stops at the first failure:
    1. `by` is in its `trust.toml` and not revoked;
    2. the signature verifies;
    3. `to` is itself;
    4. `ts` is no more than 5 minutes ahead of its clock;
    5. `expires` hasn't passed, and `expires - ts` is at most 24h;
    6. it has not seen `salt` before. It keeps the salts it has seen until each one's `expires`, in `<cache dir>/bmesh/salts`, which survives a restart.
  - A failure is answered `refused: <check>`, never run, and published as `bm.daemon.<host>.refused`, which the daemon pushes as `ask`s are pushed.
  - A reply is signed by the box that ran the job, with the job's salt in it, and the caller checks it the same way, so an answer can't be forged or replayed either.
- **`bmesh sync [--repo R | --all] [--hosts a,b] [--dry-run] [--wait 30s]`** is `repo_sync.py` in Go, and it gives the same answer whichever box runs it. A `state` job reports each box's branch, head, ahead, behind and dirty files. The caller plans with repo_sync's rules: the one box that is ahead pushes, the rest fast-forward, and nothing changes when a box is dirty, has diverged, is on another branch, or when two boxes are ahead. A pinned row moves every box to its tag. The caller then sends `push` and `ff` jobs. After a box's checkout moves, it refreshes the plugins it loads from that checkout. When the checkout is a Claude Code marketplace (`.claude-plugin/marketplace.json`), that means `claude plugin update` for the user-scope installs that lag, and `codex plugin add` for the Codex copies. A row key `after_sync = []` names any further commands. A box that doesn't answer within `--wait` is reported `asleep: queued until <expires>`, and its jobs run on wake. That ordering is safe because `ff` is fast-forward only.
- **`bmesh prune [--repo R | --all] [--hosts] [--apply]`** is `prune_worktrees.py` in Go with the same keep and remove rules, run as a `prune` job on each box. Each box asks its own runner about thread state.
- **Nix.** `bmesh repos remove <name> [--checkouts] [--apply]` drops the registry row. With `--checkouts`, a `nix` job deletes the checkout on every box that has one. A box refuses (naming the reason, touching nothing) when its checkout is dirty, has commits that aren't on `origin`, or has a worktree whose branch isn't merged.
- `node forget --worktree` on another host's node sends that host a `prune` job for the one worktree instead of refusing.

Out: encrypting job bodies (the hub's token and Tailscale carry them; the signature proves who sent a job, not who can read it); starting leads or rounds on another box over the mesh (`ideate` and `build` keep ssh); NATS leaf setup, which a box keeps as it has it; folding command-center (020).

Judge: opus, because the planning rules and refusals are fixed by repo_sync, but the exchange, the canonical encoding, the order of checks, job expiry and a box waking mid-sync need judgment.

## What you'll see
On k16, after a lead lands on buildermesh and a pstack fix is pushed, `bmesh sync --all` prints `k16 ahead, pushed; ricks-macbook asleep: queued until 15:40`. When the Mac wakes, `bmesh hosts` shows it seen, its checkouts are at the new heads and `pstack@pstack-claude` is at the new version. `bmesh repos remove TCGCharting --checkouts --apply` removes the checkout on both boxes, or names the box that refused and why. On the Mac, `bmesh init` with k16's invite code prints `trusted by k16; 2 boxes in trust.toml`. A box set up single, or with the question answered no, never runs anything it is sent. A job edited in flight, replayed, sent to the wrong box or signed by an unknown key is refused, and the refusal shows up on the phone.

## Verification
The repo's verify. New tests, against `meshtest` with two daemons named as hosts:
- the sync plan, one test per refusal;
- a job queued for a stopped daemon runs after it starts and is dropped after `expires`;
- an un-accepted kind is refused;
- `nix` refuses a dirty checkout, an unpushed one and one with an unmerged worktree;
- `init --force` keeps the earlier answers as defaults;
- plugin refresh against a fake `claude` and `codex` (`BMESH_CLAUDE_COMMAND`, a new `BMESH_CODEX_COMMAND`);
- golden vectors for the signatures: a fixed key, job, `ts` and `salt` give fixed canonical bytes and a fixed signature;
- one refusal test per check (a field changed after signing, an unknown or revoked key, the wrong `to`, a future `ts`, an expired job, a TTL over 24h, a salt seen before, including after a daemon restart);
- the exchange: a reused code, an expired code, a wrong code (which burns it), an endorsement from an untrusted key, and a forged trust list.

## Risks and the alternative considered
Alternatives: a reverse SSH tunnel from the Mac to k16, or Tailscale SSH in both directions. Both widen access to the box. The mesh's own transport needs nothing new on the network and survives sleep. On authenticity, a shared HMAC secret for the whole fleet would have been simpler, but one leaked box could then forge jobs from any box. NATS nkeys per box authenticate a box to the hub, not one box's job to another, so they don't help with jobs that wait on the hub. Per-box Ed25519 keys, endorsed from one invite, make a key revocable on its own. The risk is that a queued `ff` lands after the operator has changed something on the Mac. The job re-checks the checkout before it moves it, under the same rules, and refuses as repo_sync would.

## Needs from the operator
