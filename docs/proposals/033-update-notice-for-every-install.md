---
id: 033
title: Every install learns when bmesh moved on, and can take it
kind: feature
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
built_by: 4bcd6572-6f0b-4ada-84ea-a8cdd0f85540
host: k16
family: buildermesh-build-3
---

## Opportunity
Self-update (011, 031) only serves a box that registers buildermesh itself: `[update] repo` and `bmesh update` both need a registry row for this repo. Someone who installs from the Quickstart (`go install …/cmd/bmesh@latest` plus the marketplace plugin) has neither, and nothing ever tells them they're behind:

- Their binary prints `bmesh unknown` (README Updates: "as `go install …@latest` builds"). `FromBuildInfo` reads only `vcs.*` settings and the `-X` stamp, but a module install carries its commit in `bi.Main.Version`, for example `v0.0.0-20261016043719-ab7e0b543677` (issue #1). An unknown revision gets no hint and never updates.
- `bmesh update` refuses: `no repo to update from: pass --repo or set [update] repo`.
- The plugin and the binary drift apart. The friend ran plugin 0.6.0 and `main` is at 0.7.0. The skills name commands and flags that only a matching binary has.

Main moved 31 specs in two days. Every fix (032's included) reaches outside users only if they happen to reinstall.

## Proposal
In:
- **The version from the module.** `FromBuildInfo` falls back to `bi.Main.Version` when it is a pseudo-version: the 12-hex suffix is the revision and the timestamp is the time. Revisions compare by prefix wherever the hint and update compare them, so a 12-character revision matches its full one.
- **An upstream with no row.** When no `--repo`, `[update] repo` or own-module row applies, `bmesh update` reads the module's `main` (`git ls-remote https://<module path> refs/heads/main`). It runs `GOBIN=<temp dir beside the binary> go install <module>/cmd/bmesh@<sha>`, then the same `version` check and the same `.prev` swap as `Install`. It prints `<old8> -> <new8>` and, once the swap succeeds, one line telling them to update the plugin too (`/plugin marketplace update buildermesh`, or whatever Claude Code's current command is; the lead checks).
- **The notice.** The daemon checks every `[update] check_every` (default `6h`; `0` turns it off) when it isn't self-updating. Its source is `[update] repo`'s main checkout when that is set, else a bare fetch of the module's `main` into `<user cache dir>/bmesh/update/upstream`. When `main` is ahead of the running revision, it writes `<state dir>/update.txt` with one line: `bmesh: 7 commits behind main (landed since: 030 …, 031 …); run bmesh update, then update the buildermesh plugin`. The specs come from the `NNN: done at` commit subjects in `<own>..main`. The file is removed when the binary is current. The plugin's SessionStart hook prints that file next to `hosts --here` (`bmesh update --notice`: it reads the file, never touches the network, and prints nothing when the file is absent). `mesh-status` prints it as a closing line.
- **Opt-in auto for outsiders.** `[update] upstream = true` lets the daemon self-update from the module as `[update] repo` does from a row: the same drain and re-exec. It is refused on Windows, logged once and left on notice only: there, the exit 0 relies on a service manager that `init --service` doesn't write (issue #1.3).
- README Updates and Config (`check_every`, `upstream`), and the hook's description.

Out: tagged releases and a changelog file (the `done at` subjects are the changelog); updating the plugin from bmesh (Claude Code owns that); pushes through `[notify]` for the notice.

Judge: opus, because the swap path gains a second source that must refuse exactly as the first does, and the version comparison changes under the existing hint.

## What you'll see
On an install from the Quickstart, `bmesh version` prints `bmesh ab7e0b54 2026-10-16T04:37:19Z`. A new Claude Code session opens with `bmesh: 9 commits behind main (landed since: 032 The Quickstart works as written, …); run bmesh update, then update the buildermesh plugin`. `bmesh update` brings the box current, and the next session is silent. k16 and the Mac are unchanged except that the Mac's sessions also get the notice when its binary lags.

## Verification
The repo's verify. New tests:
- `FromBuildInfo` with a pseudo-version, a tagged version (`unknown` still) and `(devel)`;
- the prefix comparison in the hint;
- `update` with no row against a fake `go` and a local bare repo standing in for the module, covering the swap and the refusal when `version` names another commit;
- the notice written with the right count and spec list from a fixture history, and removed when current;
- `update --notice` silent without the file;
- `upstream = true` refused on Windows (a build-tag test).

A live check on k16: install `@<an older sha>` into a scratch `GOBIN` and run `update` from there.

## Risks and the alternative considered
Every install calls GitHub every six hours. One `ls-remote` or a small fetch is cheap, and `check_every = 0` stops it. The module proxy can lag a fresh commit by minutes: `go install @<sha>` asks for that exact commit, and a failure is logged once per commit and retried on the next check.

Alternative: GitHub releases with prebuilt binaries. Rejected for now: decision 10 keeps them their own spec, and `go` is already a Quickstart prerequisite. Alternative: the hint on every CLI command. Rejected: it means a network call per command or a cache anyway, and the session start is where the operator is looking.

## Needs from the operator
Whether the notice is on by default (`check_every = "6h"`), as written, or opt-in.
