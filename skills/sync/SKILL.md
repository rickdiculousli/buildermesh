---
name: sync
description: Use when the operator says "sync my repos", "sync X", "level X", "push X everywhere", "are my boxes level" or "catch the other boxes up" - X a repo in the bmesh registry. Shows first what bmesh sync would do on each box, then runs it and reports per box.
---

# Sync

`bmesh sync` levels a repo's checkouts across the mesh: it levels this box's checkout against origin (fast-forwarding, or pushing when this box is ahead and allowed to), then sends `level` to every box that has the repo, and each box judges its own checkout against origin. A box never moves a dirty checkout, one on another branch or one that has diverged: it answers `refused: <why>`. A secure box holds a level of a repo its sessions load (a plugin checkout) until the operator approves it at that box. `sync` has no dry run of its own, so the first step reads what it would touch.

1. **Which repos.** "sync my repos" is `--all`: every registered repo this box has a checkout of. A named repo is its registry name: match the operator's word against `bmesh repos` (a name, or the repo its `remote` points at), and ask only when two rows match. With no repo named and no "all", ask which.
2. **Dry run: what it would do.**
   - **This box.** For each repo, in its checkout (`bmesh repos show <repo> --json`, its `path`): `git fetch origin`, then `git status -sb` (branch, ahead, behind) and `git status --porcelain` (dirty). Name each as `up to date`, `behind <N>` (will fast-forward), `ahead <N>` (will push if its mode allows and the newest commit is 10 minutes old), or what will refuse it (`dirty`, `other branch`, `diverged`).
   - **The other boxes.** `bmesh hosts --json`: the boxes whose `accept` has `level` and whose verified presence lists the repo get a `level`. Show each with its state (an asleep box takes it when it wakes, within 24 hours) and mark the `secure` ones: there, a repo sessions load waits for the operator's approval.
   - When `bmesh hosts` says hosts are off on this box, `sync` can't run: say so and stop.
   Show this as one row per box and repo, short.
3. **Apply.** `bmesh sync --all`, or `bmesh sync --repo <repo>`, with `--wait 30s` (the default). It prints a line per box, `<box>: <answer>` (`<repo>: <box>: <answer>` with several repos): `up to date`, `fast-forwarded <N>`, `pushed <N>`, `ahead <N>, not pushed: <why>`, `held for approval (+<N> commits)`, `moved to <tag>` (a pinned row), `refused: <why>`, `failed: <why>`, `asleep, queued until <when>` or `no answer in <wait>`, with the plugins it refreshed after a `; `. Run it once; a box that answers later is read with `bmesh hosts replies`.
4. **Report per box**: what moved, what was held (the operator approves at that box: `bmesh hosts held`, then `bmesh hosts approve <id>`), what was refused and why (a dirty or diverged checkout is the operator's to settle; never stash, reset or merge it here), and which boxes are still to answer.

End with one line: how many checkouts moved on how many boxes, and what waits on the operator (holds to approve at a secure box, refusals to settle, boxes asleep). Say what each thing in it is in plain words (a spec or issue by what it does, its number after it in brackets if at all): the line is final and reads on its own, with nothing the operator has to look up.
