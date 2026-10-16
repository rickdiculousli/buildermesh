---
id: 047
title: Boxes update to the newest release, and any box can go to the latest code on demand
kind: refinement
size: L
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
built_by: d6b8c608-8215-44b7-ba93-8a7487704bd7
host: k16
family: buildermesh-build-5
---

## Opportunity
A box with `[update] repo` set, as k16 is, rebuilds and restarts its daemon whenever its copy of `main` moves (`checkUpdate`, `internal/daemon/update.go`, checked every `kindRescan`, one minute). On 2026-10-16, k16 logged 28 `update: <old> -> <new>` restarts. Many of them came from commits that changed only spec files. `Review: 045 accepted` (9a5e181) rebuilt k16 with no code change at all, and 34 of that day's 58 commits on `main` were ledger-only. Each restart drains running jobs. Boxes without `[update] repo`, like this Mac, are told `N commits behind main` (`selfupdate.Notice`) and so chase every commit too. Once 046 cuts a release per finished batch, a box should land on releases, not on whichever lead landed last.

## Proposal
In:
- **What a box follows.** `[update] track = "release" | "latest"`, default `release`.
  - `release` follows the newest `vX.Y.Z` tag on origin.
  - `latest` follows the code head of `main`, the newest commit that changes anything besides spec files (045). So even `latest` never rebuilds for a ledger commit.
  - The daemon's update check, the `check_every` notice and `bmesh update` all read the same target.
- **`bmesh update [--latest]`.** It goes to the target in `track`. `--latest` jumps to the code head this once, whatever `track` says. That is "target latest to get there", at any time, without waiting for the batch's release.
  - The next `bmesh update` follows `track` again. It never goes back to an older release than the binary already runs ("ahead of v0.2.0, up to date").
- **The notice** names releases: `bmesh: v0.2.0 is out (you run v0.1.0: 046, 047); run bmesh update`. Under `latest` it keeps the commit count, counting only code commits.
- **Tagged installs know their commit.** A binary installed as `go install <module>@v0.2.0` has a version but no revision (`fromPseudo`: "a tagged version ... names none"). So `Same` and `Contains` would treat it as unknown and stop updating it. The tag is resolved to its commit through the upstream bare repo (`FetchUpstream`) before comparing, and the module install uses `@vX.Y.Z` (`@latest` under `--latest` is the code-head commit).
- **No tag yet.** A repo with no `v*` tag under `track = "release"` keeps today's behaviour with the code head and says so once in the log, so boxes are never stranded before 046's first release.
- README `[update]` and `update` sections name `track` and `--latest`.

Out: cutting releases (046, which this spec waits on), rolling back to an older release, and per-repo tracks.

Judge: opus, because every box's update path changes, and a box that can't resolve its own version stops updating silently.

## What you'll see
k16's log shows one `update: v0.1.0 -> v0.2.0` per finished batch instead of a restart per commit. On the Mac, `bmesh update` prints `v0.1.0 -> v0.2.0`, and `bmesh update --latest` prints `v0.2.0 -> v0.2.0+2 (<rev8>)` when two code commits have landed since.

## Verification
The repo's verify, plus new cases in `internal/selfupdate` and `internal/daemon` tests over a bare origin with tags:
- under `release`, a ledger-only commit and an untagged code commit both leave the daemon alone, and a new tag rebuilds it once;
- under `latest`, a code commit rebuilds and a ledger commit doesn't;
- `update --latest` moves past the newest tag, and a later `update` keeps the newer binary;
- a tag-only module install resolves its commit and updates;
- a repo without tags follows the code head.

## Risks and the alternative considered
- **Fixes wait for the batch.** A fix landed mid-batch reaches `release` boxes only at the batch's release. `--latest` and `track = "latest"` are the way past that.
- **Two boxes on different tracks run different code.** The existing version hint (`versionHint`) already names that.
- **Alternative: keep following `main`, but skip ledger-only commits.** That alone stops the restarts the ledger causes, and it is part of `latest` here. It still versions half-done batches, though, which the operator ruled out, so `release` stays the default.

## Needs from the operator
After this lands, k16 follows releases, because its config sets no `track`. Add `[update] track = "latest"` to k16's `~/.config/bmesh/config.toml` only if k16 should pick up each landed lead's code before the batch's release, as it does today.
