---
id: 023
title: The admin's registry, and pruning and removing across boxes
kind: feature
size: L
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
built_by: dae7b2d4-6538-46a8-9d14-117b1a4301d5
host: k16
family: buildermesh-build-2
branch: bmesh/dae7b2d4-6538-46a8-9d14-117b1a4301d5
merged: b700eead912267bc0c2109688748e13929d5d44d
verified: verify ok t:08634917
---

## Opportunity
After 020, boxes stay level, but three things still need SSH or a person at the box:
- **Cleaning up worktrees** is command-center's `prune_worktrees.py` (257 lines), driven over SSH. `node forget --worktree` refuses another host's worktree with `run it there` (cmd_node.go:103).
- **Getting rid of a repo everywhere** has no tool at all.
- **The registry** reaches each box by `repo_sync.py` copying command-center's file. It is not harmless config: a row's `verify` is shell that every lead runs and `bmesh scaffold` writes into `.bmesh/routines/verify.toml` (scaffold.go:214), and its `path` decides what is deleted. Any box able to change it on another box can run shell there.

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
- It removes a bmesh executor worktree whose node is `done`, that is clean, and whose branch is merged into `main` or its lead's branch (`prune_worktrees.py`'s executor rule), and deletes local `bmesh/*` branches that no worktree holds and that `git branch -d` accepts as merged.
- T3 worktrees are T3's own: on T3 0.1.0 (029), its storage cleanup removes a thread's worktree once it has no commits beyond the default branch (`worktreeUnchanged`), keeping one with uncommitted changes. `prune` doesn't touch them. T3 leaves the branch behind, and the `bmesh/*` sweep above takes it.
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

Out:
- starting builds or rounds on another box (`ideate` and `build` keep SSH; a signed `start` op is a later spec);
- the `agent` op, an agent request run on another box, which is deferred to its own spec. With T3 0.1.0 and an operator's T3 MCP sign-in, a session can already launch a thread on another box's T3 directly;
- pruning T3 worktrees (T3's storage cleanup, above).

Judge: opus, because each op's refusals are listed, but the registry handover and the nix ordering need judgment.

## What you'll see
- On k16, `bmesh prune --all --apply` prints the rows for k16 and, once the Mac answers, the Mac's.
- On the Mac, `bmesh repos remove TCGCharting --checkouts --apply` raises the Keychain dialog, removes both checkouts, then prints `registry v8: TCGCharting removed`.
- A `verify` change proposed from k16 shows up as a hold on the Mac and changes nothing until it is approved.

## Verification
The repo's verify. New tests against `meshtest` with two daemons as hosts:
- the registry: a lower version, a non-admin signature, an unknown key (kept old, pushed), a proposal held and approved;
- `prune`'s keep and remove rules, and the `bmesh/*` sweep leaving an unmerged branch;
- `forget-worktree` remote and asleep;
- every `nix` refusal, including a path or origin mismatch, and the registry published only after every box answered;

## Risks and the alternative considered
Alternatives:
- A registry any box may push: one compromised box could rewrite every box's verify.
- `nix` taking a path from the job, or from presence, which any token holder can write: a wrong directory could be deleted.

## Needs from the operator
Publish the first registry from the Mac (`bmesh repos publish`) after 019's join.

On each box's T3 (0.1.0), turn on the worktree cleanup that removes a worktree with no commits beyond the default branch (`storageCleanup.worktreeUnchanged`), keeping worktrees with uncommitted changes, so merged leads' T3 worktrees go without `prune`.
