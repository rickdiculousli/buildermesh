# Working in buildermesh

`bmesh` is a Go CLI and daemon; the README is the reference for every command, config key, bucket and event, and is kept exact: a change to behaviour changes the README in the same commit. Tests never use `/tmp` (`meshtest.Start` refuses it), so run them with `TMPDIR=$HOME/.cache/bmesh-scratch/tmp`. Runner tests drive the shell fakes in `testdata/`, never a real T3, tmux or `claude`.

The skills in `skills/` are the plugin every node runs on; `docs/agents-snippet.md` is the paragraph other repos quote. Both must stay true for someone who has only `bmesh` and this README: nothing of any particular fleet, coordinator repo or optional plugin is assumed there.

Design and decisions live in `docs/design/` (`2026-10-14-buildermesh.md` and `2026-10-15-buildermesh-round2.md`); the specs built here are in `docs/proposals/`.

After a change lands on `main`, k16 updates itself (its daemon has `[update] repo = "buildermesh"`: it rebuilds and restarts within a couple of minutes, `journalctl --user -u bmesh` shows `update: <old8> -> <new8>`); on the Mac, run `bmesh update`.

<!-- bmesh:done -->
## Definition of done
A change is done when all of these hold: the repo's verify commands pass (`TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test -count=1 ./...`, `go vet ./...`, `test -z "$(gofmt -l .)"`); a change carrying risk verify can't catch (new logic, concurrency, quoting or escaping, a state or data migration, an interface others depend on) was reviewed once against its spec by a model that didn't write it and the findings were fixed, while a rote change (a rename, config values, wording, a mechanical edit across a few files that verify covers) is done on verify alone; the branch is rebased onto `main` with verify re-run; it is merged fast-forward to `main` and pushed to `rickdiculousli/buildermesh`. If the rebase conflicts, resolve it: read your spec and the specs that landed since you branched, make every intent hold, widen your spec if a landed one changes what yours must look like, re-validate, then merge. Stop only when the widened scope can't be met from the specs alone; then mark your spec `blocked` with the reason, push, and report. End with one line: what's done (the commit on `main`), its push state from a fresh fetch (pushed, with the commit `origin/main` is at and that it contains yours, or not pushed and why), and what, if anything, waits on the operator. Say what each thing is: the first time a message names a spec, issue, PR, commit, job or numbered item, it says in plain words what that is, with the number after it in brackets if at all, so the operator never has to look anything up. The last line is final: it reads on its own, names nothing not explained above it, and says in plain words what waits on the operator and what they'd do about it.
<!-- /bmesh:done -->

<!-- bmesh:proposals -->
## Proposals
Half-page specs live in `docs/proposals/NNN-slug.md` (the template is `bmesh spec template`; the lifecycle is in `docs/proposals/README.md`). The build thread owns its spec's status: `building` when dispatched, `done` with the merge SHA when merged, `blocked` with a reason if it can't finish. Rejected specs stay so they aren't re-proposed. Accepted specs are built by buildermesh leads (see Buildermesh below).
<!-- /bmesh:proposals -->

<!-- bmesh:mesh -->
## Buildermesh
Builds here run as nodes on a buildermesh mesh through the `bmesh` CLI: a lead owns one spec in its own worktree, executors build pieces on branches under the lead's and hand them back through the mesh, and routines run this repo's checks. The repo's routine kinds live in `.bmesh/routines/<kind>.toml` (`verify` runs `TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test -count=1 ./...`, `go vet ./...`, `test -z "$(gofmt -l .)"`; `review` asks Opus, in a session that never saw the change, for findings; add a kind rather than a script when a check should be recorded). Commit before queueing a job: a job names a commit, and every result is recorded under that commit's tree hash, so a deterministic kind answers a repeat run from its record. A spec is done only when its frontmatter carries a `verified` line (`verify ok t:<tree8>; review ok t:<tree8> m:<model>`), composed by `bmesh spec verified --write` from the results recorded for the landed tree, and `bmesh done` is the last word: it refuses until the spec on `origin/main` reads `done` with a merged commit and a result for every entry.
<!-- /bmesh:mesh -->
