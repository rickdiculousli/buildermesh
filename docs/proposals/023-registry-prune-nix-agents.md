---
id: 023
title: The admin's registry, and pruning, removing and asking across boxes
kind: feature
size: L
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
After 020, boxes stay level, but four things still need SSH or a person at the box:
- **Cleaning up worktrees** is command-center's `prune_worktrees.py` (257 lines), driven over SSH. `node forget --worktree` refuses another host's worktree with `run it there` (cmd_node.go:103).
- **Getting rid of a repo everywhere** has no tool at all.
- **The registry** reaches each box by `repo_sync.py` copying command-center's file. It is not harmless config: a row's `verify` is shell that every lead runs and `bmesh scaffold` writes into `.bmesh/routines/verify.toml` (scaffold.go:214), and its `path` decides what is deleted. Any box able to change it on another box can run shell there.
- **Asking another box to do something** that isn't a built-in op can't be done at all.

## Proposal
Builds on 020 (`done` first). Nothing here runs unless `[hosts] enabled = true`.

**The registry is the admin's signed document.**
- KV `bm-roster`, key `registry`, holds `repos.toml`'s rows as a `registry/v1` document `{version, rows}`, signed by the admin key.
- A box accepts a registry only when the pinned admin key signed it, its version is higher, and the box's own loader reads the rows. Unknown keys are refused, so a box with an older binary keeps its old file and pushes `refused: registry v7 needs a newer bmesh` instead of breaking.
- It then writes `repos.toml` (keeping `repos.toml.prev`) and `<state dir>/registry.json`.
- Daemons re-put the newest valid registry as they do the roster (019), and read it at start and on wake.
- `bmesh repos publish`, on the admin box, diffs the local `repos.toml` against the current registry, prints the row diff, raises the announced Keychain dialog and publishes the next version.
- On any other box, `bmesh repos propose` sends the diff to the admin box as a held proposal (020's hold): the admin sees the diff in `bmesh hosts held` and approves it into the next version.
- With `[hosts]` off, `repos.toml` is a plain local file as today.

**`prune [<repo>]`**, a built-in op:
- It removes worktrees with `prune_worktrees.py`'s rules: a T3 worktree whose branch is merged into `main`, that is clean, and whose thread is known, settled and not running; a bmesh executor worktree whose node is `done`, that is clean, and whose branch is merged into `main` or its lead's branch.
- Each box judges by its own runner and nodes, and reports each worktree as a keep or remove row with its reason.
- It is a dry run unless the job says `apply`.
- `bmesh prune [--repo R | --all] [--apply]` sends it to every roster box that accepts it and prints the rows per box.

**`forget-worktree <path>`** runs `removeNodeWorktree` (cmd_node.go:97) on the node's host, with the local rules (a `done` or `blocked` node, a clean worktree, the branch merged by ancestry or `git cherry`). `node forget --worktree` on another host's node sends it, waits for the reply (`--wait 30s`) and forgets the node only on success. An asleep host prints `queued; the node stays until <host> answers`.

**`nix <repo>`**, a built-in op that needs the admin's signature:
- **The job** carries the repo name and the row's `origin` from the current registry, never a path.
- **The path** comes from the receiving box's own registry row. The box refuses unless that directory's basename is the row's path basename and its `origin` URL matches.
- **The box also refuses**, touching nothing, when the checkout has:
  - uncommitted or untracked changes, or a linked worktree that is dirty or on a branch not merged into `main`;
  - a ref not on any remote (`git log --branches --tags --not --remotes`);
  - a stash;
  - ignored files, unless the job says `ignored-ok`;
  - no origin at all.
- **On success** it removes the checkout and `<[runner] checkouts>/<repo>/*`, then answers.
- **`bmesh repos remove <name> --checkouts [--apply]`** on the admin box:
  - It sends `nix` to every roster box whose signed presence lists the repo, and waits.
  - Only after every box has answered `removed` or `absent` does it publish the next registry without the row.
  - Until then it prints the boxes still pending, and running it again re-asks only those.

**`agent <repo> "<request>"`**, an agent request:
- **The integrity check comes first.** The repo's main checkout must be clean and level with origin; otherwise the request is refused.
- **On a box that isn't secure** it starts at once through the runner, in a fresh worktree of the repo, with a brief that holds the request and names the box that sent it.
  - The node is `<slug>-ask-<hex6>`, where `<slug>` is the registry name lowercased with every character outside `[a-z0-9-]` made `-`, so `openGym` and `medical_history` give valid paths.
  - It counts against `[runner] max_leads`; at the cap the job is nak'd for 5 minutes until a lead finishes.
  - Each sender gets at most 3 requests an hour per box; past that, a request is refused.
- **On a secure box** the request is held as 020 holds a level, with its text shown, and starts only after `bmesh hosts approve <id>`.
- **Redelivery.** On a `started` redelivery (019, check 8), the box first looks for the node and answers with it if it exists.
- `bmesh hosts ask <host> --repo R "<request>"` sends one.

Out: starting builds or rounds on another box (`ideate` and `build` keep SSH; a signed `start` op is a later spec).

Judge: opus, because each op's refusals are listed, but the registry handover, the nix ordering and the agent cap need judgment.

## What you'll see
- On k16, `bmesh prune --all --apply` prints the rows for k16 and, once the Mac answers, the Mac's.
- On the Mac, `bmesh repos remove TCGCharting --checkouts --apply` raises the Keychain dialog, removes both checkouts, then prints `registry v8: TCGCharting removed`.
- A `verify` change proposed from k16 shows up as a hold on the Mac and changes nothing until it is approved.
- `bmesh hosts ask k16 --repo openGym "rebase the PR branch"` from the Mac starts `opengym-ask-3fa9c1` on k16.

## Verification
The repo's verify. New tests against `meshtest` with two daemons as hosts:
- the registry: a lower version, a non-admin signature, an unknown key (kept old, pushed), a proposal held and approved;
- `prune`'s keep and remove rules;
- `forget-worktree` remote and asleep;
- every `nix` refusal, including a path or origin mismatch, and the registry published only after every box answered;
- the agent request: refused when dirty, the cap, the rate limit, the slug, the secure hold, and redelivery finding the node.

## Risks and the alternative considered
A box that isn't secure starts an agent request from any roster box. That is a box running unattended agents by design, like k16, and the operator marks a box secure where it isn't.

Alternatives:
- A registry any box may push: one compromised box could rewrite every box's verify.
- `nix` taking a path from the job, or from presence, which any token holder can write: a wrong directory could be deleted.

## Needs from the operator
Publish the first registry from the Mac (`bmesh repos publish`) after 019's join.
