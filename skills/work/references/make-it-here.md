# Making it here

A reported problem is a change to make: no spec, lead or ledger commit.

1. **Where:** a worktree of your own on a branch from `origin/main` (the harness's, or under `~/.bmesh/worktrees/<repo>/`), never the repo's checkout, where an unpushed commit holds every lead's land. A repo with no checkout here is made on its row's `host`.
2. **Make it:** the smallest change that does what was asked, by the repo's `AGENTS.md`, with the docs it keeps exact. A change that grows to L while you make it stops: it's a plan for their ok.
3. **Verify:** commit, then `bmesh routine run verify` until `verify ok` (a repo with no `verify` kind: its verify commands).
4. **Review:** your diff by the `ponytail-review` skill, at your own effort; fix its Must fix and Should fix findings once and verify again. An M or larger gets a second review: Opus in a fresh subagent, at medium effort for an M and high for an L, given the operator's words, the diff and the verify line, told to review by `ponytail-review` and to start nothing.
5. **Land:** rebase onto a fresh `origin/main` (verify again if it brought commits) and `git push origin HEAD:main`, again whenever `main` moved. Bring the repo's checkout level when it's on `main` and clean, then remove your worktree and branch. Never move commits on `main` that aren't yours. Release only when they want it running now ([release.md](release.md)).

A change you hand a subagent is its to verify, review and land; it reports one line (the commit and its push state), and you read that, not its diff.
