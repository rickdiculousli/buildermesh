---
id: 073
title: After a bmesh update run by hand, the daemon restarts itself onto the new binary, so no one runs the restart command
kind: time-cost
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-17
---

## Opportunity
A `bmesh update` run by hand (the Mac's only way to update, and any box after `bmesh update --latest`) swaps the binary and then prints `this box's daemon still runs <old8>; restart it so it runs <new8>: <command>`. Until someone runs that command, the daemon runs the old code. Every command on the box also ends with the hint `this box's daemon runs <old8>, this binary is <new8>; restart the daemon: <command>`. So each manual update needs a second manual step, and on the Mac (where `launchctl kickstart -k …` is the command) it is the one most often left undone.

`update` doesn't restart the daemon on purpose (`cmd/bmesh/cmd_version.go`, `restartLine`): a restart from outside kills the daemon's running jobs, and only the daemon's own drain hands them back to the inbox. The daemon's own update already has that drain and restart (`internal/daemon/update.go`, `checkUpdate` then `restartInto`). But that path only runs when the release target is ahead of the binary at its path. After a hand-run update, the binary at the path is already current, so `checkUpdate` returns at `case selfupdate.Current, selfupdate.Ahead` and the daemon never notices it runs older code than the file it was started from.

## Proposal
In: on its minute rescan, every daemon (with `[update] repo`, `upstream`, notice-only, or none) compares the binary at its path (`r.installed`, the same `version` read the self-update makes) with its own revision. When that binary names a revision whose history contains the daemon's, and that revision is a different commit, the daemon restarts onto it with the self-update's own steps 4 and 5: drain (stop taking jobs, kill running jobs, NAK them back), stop serve, re-exec. It logs `update: <old> -> <new> (swapped in by bmesh update); restarting`. It doesn't publish `bm.daemon.<host>.update` again, since no build happened, but its `started` follows as after any restart.

- **Not mid-walk.** `bmesh update` holds an advisory lock, `<state dir>/update.lock`, from the swap to the end of its walk. The daemon tries it without waiting and, while it is held, tries again at the next rescan. So the restart comes after the release-note steps (a step may change the config the new daemon reads). On Windows the lock does nothing, as `selfupdate.Lock` does there today: a Windows daemon may restart during the steps.
- **Only a newer binary of known revision.** A binary with no revision (a `go install` from a worktree), one on another line (`Apart`), or one older than the daemon (a rollback by hand) leaves the daemon as it is. The restart line and the hint still name the command then.
- **Windows** restarts only under `serve --restart`, as the self-update does. Without it the line stays.
- `update`'s restart line becomes `this box's daemon restarts onto <new8> within a minute` when the daemon will take it (Linux or macOS, or Windows under `serve --restart`, revision known). Otherwise it is the line as today. The hint stays as it is: it reads true until the restart.

Out: restarting the daemon from `update` through systemctl or launchctl. That would bypass the drain, and under systemd it kills the unit's whole cgroup, the runner's sessions included. Also out: the hub's own restart onto a new release, which is spec 072. With 072, a hand-run update on the Mac restarts the daemon and then the leaf with no command typed.

Judge: opus, because the restart must never come mid-walk or kill a job without handing it back.

## What you'll see
On the Mac, `bmesh update` ends `v0.3.2 -> v0.3.3` … `this box's daemon restarts onto <rev8> within a minute`. A minute later the daemon's log shows `update: v0.3.2 -> v0.3.3 (swapped in by bmesh update); restarting`, `bmesh hosts` reads the Mac awake on the new release, and no command prints the restart hint.

## Verification
Failing first: a daemon test (in `internal/daemon`, next to the self-update's tests) starts a daemon whose binary at its path is swapped for a newer fake build, as `update` would leave it. Today the daemon stays and asks serve for no restart. After the change, it drains (a running job is NAKed back, not answered) and asks serve to restart within one rescan. The same test sees no restart while `update.lock` is held, and none for a binary with no revision, an older one, or one on another line. A `cmd/bmesh` test checks `update`'s restart line in both forms. Live: a hand-run `bmesh update` on the Mac to the next release, then the log line above and `bmesh hosts` showing its new release. The manual's `bmesh update` (step 7, the restart line), Updates (the hint, the daemon's self-update) and internals' self-update section say when the daemon restarts itself.

## Upgrading
nothing: no config, registry or record changes. A daemon on an older release doesn't restart itself, so the update that brings this in still needs the restart command once.
