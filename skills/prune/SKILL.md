---
name: prune
description: Use when the operator says "clean up worktrees", "prune worktrees", "prune X", "free some disk" or "remove finished worktrees" - X a repo in the bmesh registry. Runs bmesh prune dry first on every box that accepts it, shows the keep and remove rows per box, then applies and reports per box.
---

# Prune

`bmesh prune` asks every box on the mesh (the roster boxes that accept `prune`, this one included) to remove the worktrees and branches the mesh has finished with: an executor worktree whose node is done, that is clean and whose branch is merged, and local `bmesh/*` branches no worktree holds that git deletes as merged. Each box judges its own worktrees by its own nodes and runner, and answers one keep or remove row per worktree with its reason. A runner's own worktrees (T3's) are the runner's to clean and are never touched.

1. **Is it there?** `bmesh help` lists `prune` when this bmesh has it. When it doesn't, tell the operator that this bmesh has no prune yet (`bmesh update` brings a newer one when its main has it) and stop. When `bmesh hosts` says hosts are off on this box (`[hosts] enabled = false`), prune can't reach any box: say so and stop.
2. **Dry run.** `bmesh prune --all`, or `--repo <repo>` when the operator named one. It prints the rows per box as the answers arrive. A box that is asleep answers later (`asleep: queued until <expires>`); say which and carry on with the boxes that answered. A box that refused (`refused: <check>`) is reported as printed, never retried.
3. **Show the rows** per box: what would be removed (path, branch, why) and, in one line per box, how many are kept and the commonest reasons (`dirty`, `not merged`, `not done`). The whole list when it is short.
4. **Apply.** Run the same command with `--apply`. Removing a merged, clean worktree loses nothing that isn't on a branch or a remote, so the dry run shown is the check; when the dry run removed nothing, skip this step.
5. **Report per box**: what was removed, what was kept and why, and which boxes are still to answer.

End with one line: how many worktrees and branches were removed on how many boxes, and what waits on the operator (a box asleep, a refusal, a worktree kept for a reason only they can settle, such as uncommitted work).
