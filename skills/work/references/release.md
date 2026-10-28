# Releases

"ship X", or "release X" when X is a repo with no held lead, is `bmesh release <repo>` now, after any step the repo's `AGENTS.md` names before a release. A batch of leads ends, once done and drained, with a release of each repo it landed in that is released by tag (`git tag --list 'v*'`). A `held` or `main is red` refusal is reported, never forced.
