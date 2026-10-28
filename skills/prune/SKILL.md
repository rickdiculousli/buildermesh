---
name: prune
description: Removes the worktrees and branches the mesh has finished with, on every box, dry run first. Use when the operator asks to clean up or prune worktrees, or to free disk.
---

# Prune

Look before you cut. `bmesh prune` is a dry run until `--apply`, and each box judges its own worktrees by its own nodes: an executor's whose node is done, clean and merged goes, and so do local `bmesh/*` branches git deletes as merged; each answers a keep or remove row with its reason. A runner's own worktrees (T3's) are its to clean and never touched.

1. If `bmesh help` lists no `prune`, say this bmesh has none yet (`bmesh update` brings a newer one) and stop. If `bmesh hosts` says hosts are off on this box, prune can't reach any box: say so and stop.
2. `bmesh prune --all`, or `--repo <repo>` when one is named. Show per box what would go (path, branch, why), and in one line what stays and the commonest reasons (`dirty`, `not merged`, `not done`). A box asleep answers later; a `refused:` is reported as printed, never retried.
3. Nothing to remove: stop. Otherwise run the same command with `--apply`: a merged, clean worktree loses nothing that isn't on a branch or a remote, so the dry run you showed is the check.
4. Report per box what was removed, what was kept and why, and which boxes have yet to answer.

End with one line: how many worktrees and branches were removed on how many boxes, and what waits on the operator (a box asleep, a refusal, a worktree kept for a reason only they can settle, such as uncommitted work), each named in plain words.
