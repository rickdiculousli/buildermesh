---
id: 045
title: bmesh build holds while the repo's last build run is still going, then verifies main before it dispatches
kind: refinement
size: M
judge: opus
status: accepted
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
`bmesh build` never asks what else is under way on the repo before it starts a lead (`cmd/bmesh/cmd_build.go`). One spec checks only that its own node path is free and that this host is under `[runner] max_leads`. `--accepted` also caps the repo at three working leads, but it fills them beside another family's leads (a family is the set of leads one coordinator dispatched together). It also never checks whether that family's feedback has been drained yet. So far the build runs have not overlapped (build-1 to build-4: 02:42–03:24, 04:13–05:44, 19:56–21:26, 21:55–22:04 on 2026-10-16), but that is only because the coordinator waited by hand. Nothing in the binary or in the `build` skill made it wait. A lead started beside a running family branches from a `main` about to move under it, and it misses the quick fixes that family's feedback is about to produce.

Nothing checks that `main` is healthy before a lead branches from it, either. `land` verifies each lead's rebased tree, but commits made by hand and ledger commits land with nothing recorded. None of the last four commits on `main` has a `verify` result (`bmesh result find verify --commit origin/main~N`, N = 0 to 3: `no result`), and two of them are code (7959ae5, 5d5b92a). Run on this Mac today, the verify fails: `TestInitPrintsWhatTheBoxIsSetTo` (`cmd/bmesh/cmd_init_test.go:286`) reads the real `~/Library/LaunchAgents` service, because it sandboxes only `XDG_CONFIG_HOME`. Every lead dispatched from here inherits that red, and in the first minutes of its verify it finds a failure that isn't its own. One verify of this repo takes 107 s (tests 104 s, of which `cmd/bmesh` 68 s and `internal/daemon` 99 s in parallel; vet and gofmt 3 s). A lead that runs into a red `main` costs far more than that.

## Proposal
In:
- **Hold on the last run.** Before anything is recorded, `build` (both forms) reads the repo's leads on every host, as `repoLeads` already does. Any lead outside `--family` that is not `done` or `blocked` holds the build. So does a family with a lead of the repo that still has pending feedback (`mesh.PendingFeedback`). A coordinator's own `--family` never holds itself. A single `build` is refused with exit 3: `held: buildermesh-build-4 is still running (buildermesh-042 working); watch it with bmesh watch --nodes buildermesh-042` or `held: buildermesh-build-4 has 2 feedback pending; drain it with bmesh feedback list/drain`. `--accepted` prints `held <family>: <why>` once and starts nothing.
- **Then verify main.** The commit to check is the newest one on `main` that changes anything outside the spec directory, so the `Dispatch`, `Review` and `done` ledger commits don't make every dispatch re-verify. If a `verify` result is recorded for its tree, that result answers. If none is, `build` runs the repo's `verify` kind inline in a bmesh worktree at that commit, as `land` does (`runInline`), and records the result. It prints `verify main <sha8>: ok t:<tree8>` (or `(recorded)`). When verify fails, the build is refused with exit 3: `main is red at <sha8> <subject>: verify fail t:<tree8>`, followed by the tail of the output, and nothing is dispatched. A repo whose row has no verify kind prints `verify: <repo> defines none` and goes on.
- **Then continue.** Once both pass, dispatch runs as it does today. With `--accepted` the verify runs once for the batch.
- **`--force`** gets past both checks, the way `ideate --force` gets past an open round. The operator uses it to dispatch beside a running family, or to dispatch the spec that fixes a red `main`. The output says what it got past.
- **The `build` skill** (step 2): a `held` refusal is not a failure. The coordinator tells the operator what holds the build and arms `bmesh watch` on the named nodes. At `watch-done` it drains its own family (step 7) and runs `build` again. A family another coordinator owns is reported to the operator, who decides whether to wait or pass `--force`. A `main is red` refusal is reported with its failing test, never retried. The README's Builds section names the hold, the verify line and `--force`.

Out: holding on proposal rounds (they land specs, not code), on another box's daemon running an older bmesh (that stays a hint, see `versionHint`), and fixing the red test above. That fix is its own XS change.

Judge: opus, because the hold reads mesh state across hosts and families, and a wrong hold stalls every build.

## What you'll see
`bmesh build buildermesh 043` run while family build-4 is still working prints `held: ...` naming the node to watch. Once that family is done and drained, the same command prints `verify main 5d5b92a4: fail` with the init test's failure. After the fix lands it prints `verify main <sha8>: ok`, then the lead's JSON line.

## Verification
The repo's verify, plus new `cmd_build_test.go` cases on `meshtest.Start` with the claude runner fake:
- a working lead of another family holds the build and records nothing on `main`;
- the build's own `--family` doesn't hold it;
- pending feedback holds the build until it is drained;
- a recorded ok `verify` result at the code head answers without running verify, and a ledger-only commit on top doesn't change that;
- a failing verify refuses with the tail of its output;
- `--force` dispatches past both, and `--accepted` prints a single `held` line.

## Risks and the alternative considered
- **A silent or forgotten lead holds builds until someone settles it.** The refusal names the node, and `bmesh node forget` or `--force` gets past it.
- **Verify is skipped for the spec directory.** A spec file that breaks a test isn't caught until the next code commit, but the spec writer validates every file it writes.
- **Verifying the code head adds about 107 s to the first build after a code commit.** Later builds of that commit are answered from the record.
- **Alternative: leave the hold to the skill alone.** Then any coordinator that skips a step, or a `build` someone types by hand, gets past it, and the binary is where the other checks already live.
- **Alternative: a daemon routine that verifies `main` on every push.** It would record results without anyone asking, but `build` would still need to read and enforce them, so it adds a second moving part without removing the check.

## Needs from the operator
