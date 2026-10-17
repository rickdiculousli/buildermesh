---
id: 061
title: A repo names its code paths, and a change outside them (specs, docs, art, the README) never costs a verify, in land, build, release or update
kind: time-cost
size: M
judge: opus
status: building
lens: eng
proposed_by: dfdc9673-27b2-4f3f-9f17-95a5b436de66
proposed_on: 2026-10-17
from_feedback: buildermesh-build-8 (buildermesh-055)
built_by: 22ce3cc7-f589-4ee1-8cd4-de418070885a
host: k16
family: buildermesh-build-9
branch: bmesh/22ce3cc7-f589-4ee1-8cd4-de418070885a
---

## Opportunity
Landing the upgrade-notes spec (055) took `bmesh land` three runs. Each run fetched, rebased and re-ran `verify` (about 2 minutes), and both times the coordinator's ledger-only pushes (`Review`, `Propose` and `Dispatch` commits, which touch only `docs/proposals/`) moved `main` while the verify ran. The push was refused, `main moved; again from the fetch`, and the second move refused the landing outright. The code under test was the same every time. Only the tree hash differed, because results are recorded by tree, and the tree holds everything.

`build`, `release` and `update` already step around the ledger. They read main's code head, the newest commit that changes anything outside the spec directory (`selfupdate.CodeHead`, `rev-list -- . :(exclude)<spec dir>`). That rule is an exclusion, and it is wrong both ways:
- **Too narrow.** The operator is about to add SVGs and other resources and rework the README. Each such commit is a new code head to `build`, `release` and `update`, and a new tree to `land`, so each costs a verify, a held build or a rebuilt binary, while no test reads those files.
- **Too wide.** The spec directory isn't all ledger. In bmesh's own repo `docs/proposals/README.md` is compiled into the binary (`assets.go` embeds it, along with `docs/agents-snippet.md` and `examples/routines/*.toml`). A rule that skips the spec directory would let a change to it land, release and update on a stale verify.

The operator's ruling is that the rule names what is code, not what isn't.

## Proposal
In:
- **A registry row names its code: `code = ["<pathspec>", ...]`,** git pathspecs relative to the repo root (`cmd/**`, `*.go`, `go.mod`). A change is code when it touches a path one of them matches.
  - A row without `code` keeps today's rule: everything but the spec files themselves. "The spec files" are the paths `[spec] glob` matches (`NNN-*.md`), not the whole directory, so a `README.md` beside them is code. That is a narrowing of today's rule, and it fixes the embedded-file case for every repo.
  - `bmesh repos show` prints the key. `scaffold` doesn't write it, and the README's registry section documents it.
  - bmesh's own row is set by `bmesh repos propose`, under "Needs from the operator" below.
- **One function decides it.** `CodeHead` becomes "the newest commit of ref that touches the row's code", with `git rev-list -1 <ref> -- <pathspecs>`. A new `CodeSame(a, b)` says whether two trees differ only outside the code (`git diff --quiet <a> <b> -- <pathspecs>`). `build`'s main-verify gate, `release`'s verify check and code head, and `update`'s and the daemon's target all go through the row's `code`, so all four agree on what code is.
- **Land carries a deterministic kind across a change outside the code.** In step 4, when a required deterministic kind (`verify`) has no result for the rebased tree, `land` takes an ok result for a tree that is `CodeSame` as the rebased one. That is the branch's tree before the rebase, or the last tree verified in this run. `land` stores the result again for the new tree, so `done` and `spec verified` match it like any recorded result. The line reads `verify ok t:<new8> (carried from t:<old8>: nothing under code changed)`.
  - The push retry (step 7, `main moved; again from the fetch`) uses the same rule, so a landing that raced a coordinator's ledger commit, or a README commit, goes through on the retry.
  - A change to the code re-runs the kind as today.
- **The verify kind's own file is always code.** A change to `.bmesh/routines/*.toml` re-runs whatever `code` says, because it changes what verify is.
- The README's Builds (the gate), Releases, Updates, land steps 4 and 7, and The registry say it.

Out:
- carrying a review, which already carries by patch-id;
- per-kind code paths (one list per repo);
- what `release` tags: it still tags the code head, now by `code`.

Judge: opus, because a wrong `code` list or carry lands, releases or deploys code that no verify saw.

## What you'll see
- A README rework, an SVG or a spec commit lands with `verify ok t:<new8> (carried from t:<old8>: nothing under code changed)` in seconds.
- `build` answers `verify main <sha8>: ok ... (answered from the record)` without a new run.
- Neither the daemon nor `update` rebuilds the binary for any of them.
- A change to `docs/proposals/README.md` in bmesh's repo re-runs verify.

## Verification
The repo's verify, plus, over a bare origin with a row that sets `code`:
- **`cmd_land_test.go`:**
  - a commit outside `code` pushed between the fetch and the push is carried, and the retry lands with the stored result;
  - a commit inside `code`, or to `.bmesh/routines/verify.toml`, re-runs verify;
  - with no `code`, a spec file carries and a `README.md` in the spec directory doesn't;
  - `done` accepts the carried entry.
- **`internal/selfupdate`:** `CodeHead` with and without `code`.
- **`cmd_build_test.go` and `cmd_release_test.go`:** a README-only head is answered from the code head's record.

## Risks and the alternative considered
- **A `code` list that misses a file the tests or the binary read** carries a stale verify. bmesh's own list is written from its `go:embed` lines and its tests' reads. A repo without the key keeps a rule that only ever skips spec files.
- **Alternative: exclusion globs (`ignore = ["docs/**", "*.svg"]`).** That was the operator's first thought, and they ruled it out. A new kind of file is code until someone remembers to exclude it, which is safe but slow; with inclusion, it isn't code until it's listed, which is fast but needs the list kept. The embedded `docs/` files show exclusion is the one that goes wrong silently.

## Upgrading
- The registry rows gain an optional key, `code` (git pathspecs of the repo's code). A row without it keeps today's rule, narrowed to the spec files themselves (`[spec] glob`), so a `README.md` beside them now counts as code. Nothing to set on any box.
- A binary from before this release refuses a registry whose row sets `code` (an unknown key), and keeps the registry it had. Set `code` on a row, and publish it, only once every box runs this release.

## Needs from the operator
Set bmesh's own row once every box runs this release, with the admin's `bmesh repos propose buildermesh` (or edit and publish):
`code = ["*.go", "go.mod", "go.sum", "cmd/**", "internal/**", "testdata/**", "examples/**", "docs/agents-snippet.md", "docs/proposals/README.md", "docs/upgrading/**", "skills/**", ".bmesh/**", "scripts/**", "hooks/**"]`.
Checked against the `go:embed` lines (`assets.go`: `docs/agents-snippet.md`, `docs/proposals/README.md`, `examples/routines/*.toml`; `internal/brief` and `internal/web` embed files under `internal/`) and the tests' reads (`testdata/`, `examples/routines/`, `skills/*/SKILL.md` in `internal/brief`'s tests, `docs/upgrading/*.toml` in `internal/upgrade`'s). Two entries were added to the list this spec proposed: `skills/**` and `docs/upgrading/**`, both read by tests, so a change to either alone must run verify.
