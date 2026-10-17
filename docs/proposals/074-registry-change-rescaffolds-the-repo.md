---
id: 074
title: A published row change reaches the repo by itself: the row's host rescaffolds main and pushes
kind: refinement
size: M
judge: opus
status: proposed
lens: eng
proposed_by: 9079c88e-3b4d-4243-afcb-44802f8c604a
proposed_on: 2026-10-17
---

## Opportunity
A row's `verify` and `setup` take effect only through files in the repo: the verify kind's `command` in `.bmesh/routines/verify.toml`, the done block of `AGENTS.md`, and `[setup]` in `.bmesh/config.toml`. bmesh runs the `[setup]` of the commit it works on, never the row's (070). Publishing the row changes none of those files. Someone has to run `bmesh scaffold <repo>` on a checkout, commit what it writes and push. Until then every build, verify-main and routine job runs the old commands, and nothing says so.

It happened on 2026-10-17. The 001 lead of opengym-rick reported that `scripts/build-api-docs.mjs` needs the root `js-yaml` (feedback #2167). The operator added `npm ci` at the root to the row's `setup` and published it from the Mac. k16's daemon took the new registry within a minute, but `.bmesh/config.toml` on `main` still had the old command. An agent had to notice, run `bmesh scaffold opengym-rick`, and commit and push `898fb511`. The operator expected publishing to be the whole job. Both 070's and 043's operator steps write the same hand step into the procedure ("then `bmesh scaffold <repo>` and commit and push what it writes").

## Proposal
In:
- **Who.** Only the daemon of the row's `host` box acts, and only for a `ship` row with a checkout on that box whose `AGENTS.md` has a `<!-- bmesh:` marker. A repo never scaffolded keeps its first scaffold as the operator's. One box per repo, so two daemons never race to push the same commit.
- **When.** Once a minute, after `syncRegistry` (`internal/daemon/registry.go:29`), whether the daemon took the registry itself or found one another process saved (the second branch there). For each such row, the daemon compares `origin/main`'s files with what the row writes. It acts only when they differ and it hasn't already acted for this registry version and repo. A new file in the state dir, `scaffolded.json`, records the repo and version.
- **What it writes.** Only the targets the row alone determines:
  - the verify kind's `command` (`scaffold.FollowVerify`);
  - `[setup]` (`scaffold.FollowSetup`);
  - `AGENTS.md`'s done block (`scaffold.Done`).

  The proposals and mesh blocks come from the binary and its snippet, which another box's daemon may hold in an older version. Those, and creating a missing file, stay with `bmesh scaffold`. For a tree whose `go.mod` declares bmesh's own module, `AGENTS.md` isn't written: as in `land`'s check, that tree renders its own blocks.
- **How it lands.** The same way a ledger edit lands, a bmesh worktree from levelled `main`:
  - It goes through `editOnMain` (`cmd/bmesh/cmd_spec.go:288`), moved, with `levelMain`/`addWorktree`/`removeWorktree` from `gitwork.go`, into an internal package the daemon can call.
  - It stages only the changed targets, commits as `bmesh` with `Scaffold: <repo> follows registry v<N>` (body: the row fields that changed), and pushes.
  - It runs no verify. The commit only carries out what the operator published, and the next build's verify-main judges the new commands.
- **Telling the operator.** Each attempt sends one mesh event, and with `[notify]` a push:
  - success: `<host>: scaffolded <repo> for registry v<N> at <sha8> (<files>)`;
  - failure: `<host>: scaffold <repo> for registry v<N> failed: <why>`. Causes: main not level, a push refused, a verify kind on several lines, or a `[setup]` scaffold can't rewrite (the errors `bmesh scaffold` already has).

  A failed attempt is retried each minute until it lands or a newer version replaces it. The failure push is sent once per version.
- **A row whose host has no checkout** gets one push per version: `<host>: registry v<N> changes <repo>'s scaffold but <path> has no checkout; run bmesh scaffold <repo> where it has one`.
- **Docs.** The manual's registry section, after "Every daemon re-reads the hub's registry each minute", and `bmesh scaffold` (rescaffolding by hand after a publish is no longer needed). The `scaffold` and `build` skills. The procedure line in 043 and 070 ("then `bmesh scaffold <repo>` and commit and push") is removed from the template's guidance and the manual.

Out:
- Rescaffolding lead branches in flight. `land` compares only what a branch changed (043), so a mid-build publish never refuses a landing, and the branch picks up the new files when it rebases onto `main`.
- The mesh and proposals blocks, a repo's first scaffold, and `fork` or other non-`ship` rows.
- Letting a lead or a non-admin box change the row.

Judge: opus, because the daemon gains an unattended push to every ship repo's `main`, and a wrong rule there rewrites files on a repo nobody is watching.

## What you'll see
The operator publishes a row with a new `setup` or `verify`. Within about a minute, `main` has a `Scaffold: <repo> follows registry v<N>` commit from `bmesh`, and the operator gets the `scaffolded` push. The next build installs and verifies with the new commands, with no hand step. `bmesh scaffold --check <repo>` is clean.

## Verification
The repo's verify, plus `internal/daemon` tests over a bare origin and a fake hub registry:
- A newer registry changing a ship row's `setup` makes the host's daemon commit and push `[setup]` only. A second minute does nothing.
- A `verify` change rewrites the verify kind's `command` and the done block, and leaves the mesh and proposals blocks byte-for-byte unchanged.
- A daemon on a box other than the row's `host` writes nothing.
- A row with no `bmesh:` marker writes nothing. So does a `fork` row.
- A tree declaring bmesh's own module gets `[setup]` and the verify kind, never `AGENTS.md`.
- A push refused (origin moved) is retried and lands the next minute. A failure is pushed once per version.
- A registry that doesn't change the row's scaffold writes nothing.
- `editOnMain`'s existing `cmd_spec_test.go` and `cmd_steps_test.go` cases still pass after the move.

## Risks and the alternative considered
- **An unattended push to `main`.** It is limited to three row-fed targets and lands only what the admin signed, the same trust a published row already carries (briefs and verify-main read it).
- **A hand edit to a scaffolded file on `main`** is overwritten at the next row change, as `bmesh scaffold` would. The row has been the source since 043.
- **Lag.** A build dispatched in the minute between publish and rescaffold runs the old commands, as today.
- **Alternative: read `[setup]` and the verify kind from the row instead of the commit.** That reverses 070's rule that a box on another registry version runs the commit's setup. It would also leave the files in the repo stale, which `land`'s drift check and agents read.
- **Alternative: have `bmesh repos publish` rescaffold on the admin box.** The admin often has no checkout of the repo (the Mac doesn't of opengym-rick), and a proposal approved there would still need the host to do it.

## Upgrading
- The daemon of each ship repo's `host` box must run this bmesh for a published row change to reach that repo. On an older daemon, run `bmesh scaffold <repo>`, then commit and push, by hand as before.

## Needs from the operator
