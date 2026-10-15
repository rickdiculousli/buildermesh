---
name: align
description: Use when the operator says "align X", "align repo X", "is X aligned", "give X its done rule" or "X's AGENTS.md drifted" - X a ship repo in the bmesh registry. Diffs with bmesh align --check, writes with bmesh align, verifies, commits and pushes.
---

# Align

`bmesh align` gives a ship repo what a build expects of it from its registry row and the binary (the three marked `AGENTS.md` sections, `docs/proposals/README.md`, the routine kinds the row names) and nothing else. It runs from this box against the repo's checkout (`path` in `bmesh repos show <repo>`).

1. **Show the diff.** `bmesh align <repo> --check` prints the unified diff and exits 1 when there is one; exit 0 and `<repo> is aligned` means stop here and say so. Show the operator the diff (the whole thing when short, else the files and a line per section).
2. **Write it.** `bmesh align <repo>`, which prints `wrote <file>` per file. A line on stderr about a routine with no example names a kind file to write by hand; report it, don't invent the file.
3. **Verify** in the repo's checkout with the row's `verify` commands (`bmesh repos show <repo> --json`, the `verify` array, run in order with `&&`). A failure is reported with its output and nothing is committed.
4. **Commit and push.** In that checkout, with only the written files staged: `git add` what `align` printed, commit `Align on the shared definition of done and proposals`, and `git push` where the row has a `remote`. The checkout must be on `main` with nothing else staged; otherwise report it, never stash, reset or switch branches there.

End with one line: what landed (the commit, pushed or local-only), or that the repo was already aligned, and what waits on the operator (a kind file to write by hand, a verify failure).
