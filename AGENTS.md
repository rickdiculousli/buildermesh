# Working in buildermesh

`bmesh` is a Go CLI and daemon; the README is the reference for every command, config key, bucket and event, and is kept exact: a change to behaviour changes the README in the same commit. Tests never use `/tmp` (`meshtest.Start` refuses it), so run them with `TMPDIR=$HOME/.cache/bmesh-scratch/tmp`. Runner tests drive the shell fakes in `testdata/`, never a real T3, tmux or `claude`.

The skills in `skills/` are the plugin every node runs on; `docs/agents-snippet.md` is the paragraph other repos quote. Both must stay true for someone who has only `bmesh` and this README: nothing of any particular fleet, coordinator repo or optional plugin is assumed there.

Design and decisions live in `~/repos/command-center/docs/superpowers/specs/2026-10-14-buildermesh.md` and `2026-10-15-buildermesh-round2.md`; the specs built here are in `docs/proposals/`.

After a change lands on `main`, install it: `GOBIN=~/.local/bin go install ./cmd/bmesh` on both boxes and `systemctl --user restart bmesh` on k16.

<!-- command-center:done -->
## Definition of done
A change is done when all of these hold: the repo's verify commands pass (`cd ~/repos/buildermesh && TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test -count=1 ./...`, `go vet ./...`, `test -z "$(gofmt -l .)"`); a validation agent that didn't write the change reviewed it against its spec and the findings were fixed; the branch is rebased onto `main` with verify re-run; it is merged fast-forward to `main` and pushed to `rickdiculousli/buildermesh`. If the rebase conflicts, resolve it: read your spec and the specs that landed since you branched, make every intent hold, widen your spec if a landed one changes what yours must look like, re-validate, then merge. Stop only when the widened scope can't be met from the specs alone; then mark your spec `blocked` with the reason, push, and report. End with one line: what's done (commit on main) and what, if anything, waits on the operator.
<!-- /command-center:done -->

<!-- command-center:proposals -->
## Proposals
Half-page specs live in `docs/proposals/NNN-slug.md` (the template is `bmesh spec template`; the lifecycle is in `docs/proposals/README.md`). The build thread owns its spec's status: `building` when dispatched, `done` with the merge SHA when merged, `blocked` with a reason if it can't finish. Rejected specs stay so they aren't re-proposed. Accepted specs are built by buildermesh leads (see Buildermesh below).
<!-- /command-center:proposals -->

<!-- command-center:mesh -->
## Buildermesh
Builds here run as nodes on a buildermesh mesh through the `bmesh` CLI: a lead owns one spec in its own worktree, executors build pieces on branches under the lead's and hand them back through the mesh, and routines run this repo's checks. The repo's routine kinds live in `.bmesh/routines/<kind>.toml` (`verify` runs `cd ~/repos/buildermesh && TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test -count=1 ./...`, `go vet ./...`, `test -z "$(gofmt -l .)"`; `review` asks a model of another family for findings; add a kind rather than a script when a check should be recorded). Commit before queueing a job: a job names a commit, and every result is recorded under that commit's tree hash, so a deterministic kind answers a repeat run from its record. A spec is done only when its frontmatter carries a `verified` line (`verify ok t:<tree8>; review ok t:<tree8> m:<model>`), composed by `bmesh spec verified --write` from the results recorded for the landed tree, and `bmesh done` is the last word: it refuses until the spec on `origin/main` reads `done` with a merged commit and a result for every entry.
<!-- /command-center:mesh -->
