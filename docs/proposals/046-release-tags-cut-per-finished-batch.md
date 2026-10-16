---
id: 046
title: A finished build batch cuts a release tag, starting at v0.1.0
kind: feature
size: M
judge: opus
status: accepted
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
`bmesh` has no releases. The repo has no tags (`git tag` and `git ls-remote --tags origin` are both empty), and `bmesh version` prints only a commit and time (`cmd/bmesh/cmd_version.go`). As a result, "a version" means "whatever `main` is". `main` moves on bookkeeping as much as on code: of the 58 commits on 2026-10-16, 34 changed only spec files under `docs/proposals/` (dispatch, review and `done` records). It also moves in the middle of a build batch, between one lead's landing and the next. Nothing names the point where a batch's work is all in and verified, the state worth running on every box.

## Proposal
In:
- **`bmesh release <repo> [--minor|--patch] [--dry-run]`.** It runs from the coordinator once a batch (a build family) is finished. It tags the repo's code head (the newest commit on `main` that changes anything besides spec files, as 045 defines it) as `vX.Y.Z`, as an annotated tag, and pushes the tag.
  - **The first release.** A repo with no `v*` tag yet gets `v0.1.0`.
  - **Later releases.** The minor version goes up when any spec landed since the last tag is a `feature`; otherwise the patch version does. `--minor`/`--patch` overrides that.
  - **The tag message** lists the specs landed since the last tag (`NNN: done at` commits, as the update notice reads them).
- **When it refuses (exit 3).** Holding a release reuses 045's check: no release while a lead of the repo is still working or a family of the repo has pending feedback. It also refuses while `verify` at the code head isn't ok (045's verify step), and when nothing but spec files changed since the last tag (`nothing to release since v0.1.2`).
  - The batch increments the version once, when it is done, never per lead.
  - A tag already on the code head is reported, not re-tagged.
- **The `build` skill's step 7** ends with `bmesh release` for each repo where the family landed code and that already has a `v*` tag. A repo starts releasing with its first tag, which the operator or a lead cuts by hand, so no registry key is needed.
- **`bmesh version`** prints `bmesh v0.1.0 (9a5e181a 2026-10-16T22:38:04Z)` on a tagged commit, and `v0.1.0+3 (<rev8> ...)` three commits past it, read from the binary's stamp (`git describe` at build time, through `stampFlags`).
- **This spec's own lead** cuts `v0.1.0` on buildermesh after it lands, which starts the line.
- README gains a Releases section and the `version` line.

Out: what boxes update to. That is 047, the next spec, built after this one. Also out: GitHub release pages, changelog files, and tags on repos that never had one.

Judge: opus, because the bump rule and the hold read the ledger and mesh state, and a wrong tag is public and hard to take back.

## What you'll see
After a batch's feedback is drained, the coordinator reports `released buildermesh v0.2.0 (046, 047)`. On a box that ran `bmesh update` afterwards, `bmesh version` prints `bmesh v0.2.0 (...)`.

## Verification
The repo's verify, plus new `cmd_release_test.go` cases on `meshtest.Start` with a bare origin:
- an untagged repo gets `v0.1.0`;
- a `feature` spec landed since the last tag bumps the minor version, and a refinement bumps the patch version;
- spec-only commits since the last tag refuse with `nothing to release`;
- a working lead or pending feedback refuses;
- a failing verify refuses;
- `--dry-run` prints the tag and pushes nothing;
- `version` prints the tag and the `+N` form.

## Risks and the alternative considered
- **A pushed tag is public.** `--dry-run` and the verify gate are the guard, and a bad release is followed by the next one, never moved.
- **Deriving the bump from spec kinds trusts the ledger**, and `--minor` corrects it.
- **Alternative: tag on every lead's landing.** That versions half-done batches, which the operator ruled out.
- **Alternative: a release branch that `main` merges into.** It adds a second branch for every box to follow, while a tag gives the same pin with no merges.

## Needs from the operator
