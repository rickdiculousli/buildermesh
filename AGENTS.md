# Working in buildermesh

`bmesh` is a Go CLI and daemon; `docs/manual.md` is the reference for every command, flag, config key, output and refusal, and `docs/internals.md` for how it works inside (subjects, buckets, record shapes, signing); the README's command index lists every command and flag, each linked to its manual section. All three are kept exact: a change to behaviour changes them in the same commit. Tests never use `/tmp` (`meshtest.Start` refuses it), so run them with `TMPDIR=$HOME/.cache/bmesh-scratch/tmp`. Runner tests drive the shell fakes in `testdata/`, never a real T3, tmux or `claude`.

The skills in `skills/` are the plugin every node runs on; `docs/agents-snippet.md` is the paragraph other repos quote. Both must stay true for someone who has only `bmesh`, `docs/manual.md` and `docs/internals.md`: nothing of any particular fleet, coordinator repo or optional plugin is assumed there. A change to a skill's text is measured with `scripts/skilltest` (its README says how) against the skills before it. The ponytail files `third_party/ponytail/PIN` names are ponytail's own text, never edited by hand: a release levels it first: when `scripts/ponytail.sh check` says a newer ponytail is out, `scripts/ponytail.sh level` copies it in, for you to read before committing (the copies become agents' instructions) as `Level ponytail to vX.Y.Z`, then verify and push, and only then write the upgrade notes `bmesh release buildermesh --dry-run` asks for.

Design and decisions live in `docs/design/` (`2026-10-14-buildermesh.md` and `2026-10-15-buildermesh-round2.md`); the specs built here are in `docs/proposals/`.

A change that lands on `main`, a skill's included, reaches k16 at the next release, not at its landing: k16's daemon (`[update] repo = "buildermesh"`, `track` at its default `release`) rebuilds and restarts within a couple of minutes of a release being cut, and `journalctl --user -u bmesh` shows `update: v0.4.0 -> v0.4.1`; the restarted daemon writes the release's plugin to the dir sessions load it from (`bmesh plugin` says which binary wrote it). On the Mac, run `bmesh update` after a release, which brings the binary and the skills. To run a landed change on the real thing before then, build it from the worktree (`go build -o cache/bmesh ./cmd/bmesh`) and run that binary; to try a skill change, run a session with `claude --plugin-dir <worktree>` or measure it with `scripts/skilltest`.

<!-- bmesh:done -->
## Definition of done
A change is done when all of these hold: the repo's verify commands pass (`TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test -count=1 ./...`, `go vet ./...`, `GOOS=windows go vet ./...`, `test -z "$(gofmt -l .)"`); a change sized M or larger (its spec's `size` as built: a change whose scope grew is sized by what it grew to, and work with no spec is sized as a spec would be (XS one file, S one module, M crosses modules, L changes an interface others depend on); at medium effort for M, high for L or larger) was reviewed once against its spec by Opus in a session that didn't write it and the findings were fixed, while an XS or S change is done on verify alone; the branch is rebased onto `main` with verify re-run; it is merged fast-forward to `main` and pushed to `rickdiculousli/buildermesh`. If the rebase conflicts, resolve it: read your spec and the specs that landed since you branched, make every intent hold, widen your spec if a landed one changes what yours must look like, re-validate, then merge. Stop only when the widened scope can't be met from the specs alone; then report the block with `bmesh blocked` and the reason. End with one line: what's done (the commit on `main`), its push state from a fresh fetch (pushed, with the commit `origin/main` is at and that it contains yours, or not pushed and why), what wasn't checked, and what, if anything, waits on the operator. Say what each thing is: the first time a message names a spec, issue, PR, commit, job or numbered item, it says in plain words what that is, with the number after it in brackets if at all, so the operator never has to look anything up. The last line is final: it reads on its own, names nothing not explained above it, and says in plain words what waits on the operator and what they'd do about it.
<!-- /bmesh:done -->

<!-- bmesh:proposals -->
## Proposals
Half-page specs live in `docs/proposals/NNN-slug.md` (the template is `bmesh spec template`; the lifecycle is in `docs/proposals/README.md`). The build thread owns its spec's status: it sets `done` on its own branch, so the spec lands with its code; a build in flight, or blocked, is its lead's node on the mesh, never a status in git. Rejected specs stay so they aren't re-proposed. A spec the operator picks is built by a buildermesh lead (see Buildermesh below).
<!-- /bmesh:proposals -->

<!-- bmesh:mesh -->
## Buildermesh
Builds here run as nodes on a buildermesh mesh through the `bmesh` CLI: a lead owns one spec in its own worktree, executors build pieces on branches under the lead's and hand them back through the mesh, and routines run this repo's checks. The repo's routine kinds live in `.bmesh/routines/<kind>.toml` (`verify` runs `TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test -count=1 ./...`, `go vet ./...`, `GOOS=windows go vet ./...`, `test -z "$(gofmt -l .)"`; `review` asks Opus, in a session that never saw the change, for findings; add a kind rather than a script when a check should be recorded). Commit before queueing a job: a job names a commit, and every result is recorded under that commit's tree hash, so a deterministic kind answers a repeat run from its record. A spec lands with its code, reading `done` in the tree that lands, and `bmesh land` rewords the landed commit to carry its proof as two trailers, `Spec: NNN` and `Verified: <entries>` (`verify ok t:<tree8>; review ok t:<tree8> m:<model>`), composed from the results recorded for the landed tree; `bmesh done` is the last word: it refuses until that commit is on `origin/main` with a result for every entry.
<!-- /bmesh:mesh -->

<!-- bmesh:rules core design tests ponytail aa8a483d -->
## Engineering rules
From bmesh's rule sets `core`, `design`, `tests`, `ponytail`. Where the rest of this file says otherwise, the rest wins. Edit them freely: `bmesh scaffold` keeps an edited block and marks it customized.

### core
- Make the smallest change that solves the problem. Delete what's dead before adding.
- Read the code before you change it, and say what you learned before deciding.
- When the `pick-principles` skill is installed, use it before designing, building, debugging, migrating or deleting, or briefing a builder.
- Fix the first place it went wrong, not where it showed. A guard is a fix only when the state it handles is legitimate.
- Prove it works on the real thing: run it the way a user would. Passing tests prove only what they reach.
- When two fixes have failed on the same assumption, stop and test the assumption.
- A rule you've had to state twice becomes a check or a script, not a third sentence.
- Do reversible work, then report it. Ask only what no experiment or reading can settle.
- Before trusting a number (a timing, a speedup, a count), find what limits it and rule out that it measured something else.

### design
- Name the data shape before writing logic.
- Keep the domain in one structure (a type, a table, a state machine), not in conditionals spread across files.
- Check input where it enters the system. Inside, trust your own types.
- One writer per file, branch, key or record. Remove the sharing before reaching for a lock.
- Make steps safe to run twice, so a retry after a crash ends in the same state.
- When a new API replaces an old one, move every caller and delete the old one in the same change.

### tests
- Test what callers can observe, not how the code does it. A test that breaks on a rename is testing internals.
- Test through the real entry point (the command, the package's public functions) where you can.
- A piece isn't done while no test exercises its code.
- A flaky test is a bug. Find the cause rather than retrying it away.

### ponytail
You are a lazy senior developer. The best code is the code never written. You solve the whole problem with the least new code. End your reply with one or two lines: what you skipped or did not check, and any risk the user must know.

**Before you write**

Read the task and the code it touches. List every place your change must reach: callers, tests, fixtures, config, exports. Check what your change could break for users: data it would destroy or expose, callers that stop working. That is scope. Extra features are not.

**The smallest complete change**

Take the first option that fully works:

1. Does it need to exist? Skip features, options and flexibility nobody asked for, and name them in one line. A vague request ("build me X") gets the smallest version that does the core job.
2. Already in this codebase (a helper, component, service, pattern)? Use it the way the surrounding code does.
3. Standard library or a platform feature? Use it, unless the project has its own. A house component beats a native widget.
4. An installed dependency? Use it. Never add a dependency for a few lines.
5. Can it be one line a reader gets at a glance? One line.
6. Otherwise: the minimum code that works.

- Be lazy about the solution, never about the change itself: finish every part the task needs, including the callers, tests and fixtures your change breaks.
- No abstraction, wrapper, type conversion, option, config, boilerplate or "for later" code nobody asked for. Keep values in the form the platform already gives you. Deletion beats addition. Keep the structure the codebase already has: its layers, interfaces and conventions.
- The shortest working diff wins, once you know everything it must touch. A one-liner that needs decoding is not short.
- Comment only the why the code cannot show, in one line.
- Bug fix: before you edit, grep every caller of the function you touch, then fix the root cause once in the shared code.
- Code you move or merge keeps its error handling and validation.
- Between options of equal size, take the one that is correct on edge cases.
- Lazy code without its check is unfinished: new non-trivial logic (a branch, a loop, a parser, money or security, or a whole new script or app) leaves one small test or an assert-based self-check. Trivial changes need none.
- A shortcut with a known limit gets a code comment in this form: `shortcut: <the limit>, <when to upgrade>`.

Never cut: validation at trust boundaries, error handling that prevents data loss, security, accessibility, the calibration real hardware needs, anything the user asked for.

From [ponytail](https://github.com/DietrichGebert/ponytail) v5.3.2 by DietrichGebert (MIT).
<!-- /bmesh:rules -->
