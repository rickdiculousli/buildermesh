---
id: 073
title: After a bmesh update run by hand, the daemon restarts itself onto the new binary, so no one runs the restart command
kind: time-cost
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-17
built_by: 251205bf-8416-4916-ab37-4b57152ae1ea
host: k16
family: buildermesh-build-17
branch: bmesh/251205bf-8416-4916-ab37-4b57152ae1ea
---

## Opportunity
A `bmesh update` run by hand (the Mac's only way to update, and any box after `bmesh update --latest`) swaps the binary and then prints `this box's daemon still runs <old8>; restart it so it runs <new8>: <command>`. Until someone runs that command, the daemon runs the old code. Every command on the box also ends with the hint `this box's daemon runs <old8>, this binary is <new8>; restart the daemon: <command>`. So each manual update needs a second manual step, and on the Mac (where `launchctl kickstart -k …` is the command) it is the one most often left undone.

`update` doesn't restart the daemon on purpose (`cmd/bmesh/cmd_version.go`, `restartLine`): a restart from outside kills the daemon's running jobs, and only the daemon's own drain hands them back to the inbox. The daemon's own update already has that drain and restart (`internal/daemon/update.go`, `checkUpdate` then `restartInto`). But that path only runs when the release target is ahead of the binary at its path. After a hand-run update, the binary at the path is already current, so `checkUpdate` returns at `case selfupdate.Current, selfupdate.Ahead` and the daemon never notices it runs older code than the file it was started from.

## Proposal
In: on its minute rescan, every daemon (with `[update] repo`, `upstream`, notice-only, or none) checks the binary at its path. It runs that binary's `version` (`r.installed`, the same read the self-update makes) only when the file's modification time or size changed since the last check, so an unchanged binary costs a `stat`, not a process. When that binary names a revision whose history contains the daemon's, and that revision is a different commit, the daemon restarts onto it with the self-update's own steps 4 and 5: drain (stop taking jobs, kill running jobs, NAK them back), stop serve, re-exec. It logs `update: <old> -> <new> (swapped in by bmesh update); restarting`. It doesn't publish `bm.daemon.<host>.update` again, since no build happened, but its `started` follows as after any restart.

- **History is read where `bmesh update` reads it.** The daemon judges "contains" in the repo `update` builds from (`updateSource`): `[update] repo`'s main checkout, else the row of its own module (`ModuleRepo`), else the bare upstream under the cache dir (`selfupdate.UpstreamDir`), fetched only when the binary changed and its revision isn't there yet. A daemon with none of these (no row, no module, no cache dir) can't tell newer from older, so it doesn't restart, and the restart line stays.
- **Not mid-walk.** A new advisory lock, `<state dir>/update.lock`: `bmesh update` takes it before `selfupdate.Install` (so before the swap) and holds it to the end of its walk. The existing `<checkout>.lock` (`selfupdate.Lock`) isn't enough, since a daemon with no `[update] repo` doesn't know which checkout `update` used. The daemon tries the new lock without waiting and, while it is held, tries again at the next rescan. So the restart comes after the release-note steps (a step may change the config the new daemon reads). On Windows the lock does nothing, as `selfupdate.Lock` does there today: a Windows daemon may restart during the steps.
- **Only a binary whose walk finished and whose config loads.** The self-update's gate refuses a binary that would walk asks, or steps that ask first (`internal/daemon/upgrade.go`, `gate`). A hand-run `update` can end with its walk unfinished: an ask unanswered without `--defaults`, or a step that failed. So before restarting, the daemon requires that no walk is pending for the new binary (`upgrade.Pending(<state dir>, <new>)` is empty) and that `<new> config check` passes against this box's config. If either fails, it doesn't restart. It logs `update: <new8> is at <exe> but <why>; not restarting onto it` once per binary, writes the notice, and pushes once with `[notify]`, as `hold` does. It checks again when the binary or the walk record changes. A daemon that restarted onto a config it can't load would crash-loop under systemd or launchd.
- **Only a newer binary of known revision.** A binary with no revision (a `go install` from a worktree), one on another line (`Apart`), or one older than the daemon (a rollback by hand) leaves the daemon as it is. The restart line and the hint still name the command then.
- **Windows** restarts only under `serve --restart`, as the self-update does. Without it the line stays.
- `update`'s restart line becomes `this box's daemon restarts onto <new8> within a minute` only when the running daemon will take it. `update` reads that from the daemon itself, not from its own build: `started`'s `can` list (which already carries `land`) gains `restarts`, set by daemons with this change when they can restart themselves (Linux or macOS, or Windows under `serve --restart`, and a repo to read history from). `update` prints the new line when the daemon's newest `started` has `restarts` in `can`, the new binary's revision is known, and its walk finished. Otherwise, including a daemon older than this change, it prints the line as today. A binary from an earlier update that nobody restarted for therefore never claims a restart the running daemon won't make. The hint stays as it is: it reads true until the restart.
- **`update`'s hub line compares the hub with the running serve.** On 2026-10-23 a hand-run `bmesh update` on the Mac, v0.4.2 -> v0.4.3, printed `hub runs v0.4.2, serve v0.4.3: restart bmesh-hub` while serve still ran v0.4.2: `cmd/bmesh/cmd_update.go` passes the binary it just installed to `selfupdate.HubLine` as `serve`. Since 072 a hub follows the serve it runs under, so after the daemon restarted the leaf re-execed onto v0.4.3 within a minute and nothing needed typing. `update` passes the running serve's record (`serve.json`, as `hosts` and `mesh-status` do), so the hub line reads true and appears only when the hub is behind the serve that is already running, the case where a person must act.

Out: restarting the daemon from `update` through systemctl or launchctl. That would bypass the drain, and under systemd it kills the unit's whole cgroup, the runner's sessions included. Also out: the hub's own restart onto a new release, which is spec 072. With 072, a hand-run update on the Mac restarts the daemon and then the leaf with no command typed.

Judge: opus, because the restart must never come mid-walk or kill a job without handing it back.

## What you'll see
On the Mac, `bmesh update` ends `v0.3.2 -> v0.3.3` … `this box's daemon restarts onto <rev8> within a minute`. A minute later the daemon's log shows `update: v0.3.2 -> v0.3.3 (swapped in by bmesh update); restarting`, `bmesh hosts` reads the Mac awake on the new release, and no command prints the restart hint.

## Verification
Failing first: a daemon test (in `internal/daemon`, next to the self-update's tests) starts a daemon whose binary at its path is swapped for a newer fake build, as `update` would leave it. Today the daemon stays and asks serve for no restart. After the change, it drains (a running job is NAKed back, not answered) and asks serve to restart within one rescan. The same test sees no restart while `update.lock` is held, and none for a binary with no revision, an older one, or one on another line. It also sees none while a walk is pending for the new binary, and none when the new binary's `config check` fails: one log line and one push, then nothing more until the binary changes. It also sees no `version` run for an unchanged binary. A `cmd/bmesh` test checks `update`'s restart line in both forms: the new line for a `started` whose `can` has `restarts`, the old line for one without (an older daemon) and for an unfinished walk. Live: a hand-run `bmesh update` on the Mac to the next release, then the log line above and `bmesh hosts` showing its new release. The manual's `bmesh update` (step 7, the restart line), Updates (the hint, the daemon's self-update) and internals' self-update section say when the daemon restarts itself.

## Upgrading
nothing: no config or registry changes. `started`'s `can` gains a `restarts` entry, which older readers ignore. A daemon on an older release doesn't restart itself, so the update that brings this in still needs the restart command once.

## Needs from the operator
- [ricks-macbook] Once the release after the one that brings this in is out, run `bmesh update` there: it ends `this box's daemon restarts onto <rev8> within a minute`, and a minute later the daemon's log shows `update: <old> -> <new> (swapped in by bmesh update); restarting`. check: bmesh hosts
