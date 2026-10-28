---
name: scaffold
description: Gives a ship repo in the bmesh registry the AGENTS.md sections, rule sets, proposals folder and routine kinds a build expects, asking the operator which rules it gets. Use when asked to scaffold a repo, whether it's scaffolded, or to change its rules.
---

# Scaffold

`bmesh scaffold <repo>` writes what a build expects of a ship repo, from its registry row and the binary, into the repo's checkout (`path` in `bmesh repos show <repo>`). A published change to a row's `verify` or `setup` is rescaffolded by the row's host box on its own; this skill is for the rest: a first scaffold, the rules, a stale done block, a host box with no checkout or an older bmesh, and a rescaffold whose push says it failed.

1. **Show the diff.** `bmesh scaffold <repo> --check`. Exit 0 with no output: it's scaffolded; say so and stop, unless they asked to change its rules (step 2). Exit 1 prints the diff: show it (whole when short, else the files and a line per section). A `bmesh:` line on stderr and no diff is an error: report it as printed and stop.
2. **The rules interview**, before anything is written, when `AGENTS.md` has no `<!-- bmesh:rules` block or they asked to change its rules.
   - Show each set's lines from the Rule sets section of bmesh's manual (`docs/manual.md`) and ask which they want: several, one or none. Recommend `core`, plus `tests` when the repo has tests, `design` when it holds stateful or concurrent code and `ponytail` when they want agents to write the least code a change needs, each with one clause of evidence ("`tests`: it has 40 `_test.go` files").
   - Write the pick as an empty block at the end of `AGENTS.md`, `<!-- bmesh:rules <sets> -->` and on the next line `<!-- /bmesh:rules -->` (`<!-- bmesh:rules -->` for none), or rewrite the existing opening marker to that. Put no text of your own in it: step 3 fills it.
   - A block whose marker reads `customized`: ask whether to keep it as written (the default) or go back to bmesh's sets.
   - Tell them the text stays theirs to edit: scaffold keeps an edited block and marks it `customized`.
3. **Write it.** `bmesh scaffold <repo>` prints `wrote <file>` for each. A stderr line saying the row lists no verify commands is a fact about the row to pass on, not a failure.
4. **Clear conflicts out of the repo's own prose**, after an interview. Redline it: read the repo's instructions outside bmesh's markers (`AGENTS.md`, `CLAUDE.md`, and any file `AGENTS.md` says to read at the start) and set each line that restates a picked rule, contradicts one, or rests on a set they didn't pick beside the rule it meets. A restatement is deleted: the block carries it. For the rest, ask which wins: the repo's line, with the rule cut or reworded in the block, or the rule, with the repo's line deleted. Change only what they answer. After an edit to the block, run `bmesh scaffold <repo>` again so its marker reads `customized`.
5. **Verify** in the checkout with the row's `verify` commands (`bmesh repos show <repo> --json`), in order. A failure is reported with its output, and nothing is committed.
6. **Commit and push**, staging only the files scaffold wrote and those steps 2 and 4 changed: `Scaffold the shared definition of done, proposals and rules`, then `git push` where the row has a `remote`. The checkout must be on `main` with nothing else staged; otherwise report it, and never stash, reset or switch branches there.

End with one line: what landed (the commit, pushed or local-only, and the rule sets the repo now has), or that it was already scaffolded, and what waits on the operator (a kind file to write by hand, a verify failure), each named in plain words.
