---
name: sync
description: Levels a registry repo's checkouts across the mesh's boxes with bmesh sync, showing first what each box would do. Use when the operator asks to sync or level repos, push one everywhere, or whether the boxes are level.
---

# Sync

`bmesh sync` levels a repo's checkouts: this box's against origin (a fast-forward, or a push when it's ahead and its mode allows), then a `level` to every box with the repo, each judging its own checkout. A box never moves a checkout that's dirty, on another branch or diverged (`refused: <why>`), and a secure box holds a repo its sessions load until the operator approves it there. `sync` has no dry run, so look first.

1. **Which repos?** "Sync my repos" is `--all`. A named one is matched against `bmesh repos` (its name, or the repo its `remote` points at); ask only when two rows match, or when no repo is named and "all" isn't said.
2. **What would it do?** If `bmesh hosts` says hosts are off on this box, sync can't run: say so and stop.
   - This box: in each checkout (`bmesh repos show <repo> --json`, its `path`), `git fetch origin`, then `git status -sb` and `git status --porcelain`. Each is up to date, behind (it fast-forwards), ahead (it pushes if its mode allows and the newest commit is 10 minutes old), or refused (dirty, other branch, diverged).
   - The other boxes: from `bmesh hosts --json`, those whose `accept` has `level` and whose presence lists the repo, with their state (an asleep box takes it when it wakes, within 24 hours) and the `secure` ones marked.
   Show one short row per box and repo.
3. **Run it once:** `bmesh sync --all` or `bmesh sync --repo <repo>`. It prints `<box>: <answer>` per box; a box that answers later is read with `bmesh hosts replies`.
4. **Report per box:** what moved; what's held (the operator approves at that box: `bmesh hosts held`, then `bmesh hosts approve <id>`); what was refused and why (a dirty or diverged checkout is the operator's to settle: never stash, reset or merge it here); and which boxes have yet to answer.

End with one line: how many checkouts moved on how many boxes, and what waits on the operator (holds to approve at a secure box, refusals to settle, boxes asleep), each named in plain words.
