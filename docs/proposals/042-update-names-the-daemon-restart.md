---
id: 042
title: bmesh update says when this box's daemon still runs the old binary, and how to restart it
kind: refinement
size: S
judge: opus
status: done
lens: eng
proposed_by: 56979e66-321a-467e-9cbc-a2165df787ff
proposed_on: 2026-10-16
from_feedback: buildermesh-build-3 (the operator's Mac update during the family)
built_by: 5e40fd56-b2f9-4b2b-8bde-110f2110102d
host: k16
family: buildermesh-build-4
branch: bmesh/5e40fd56-b2f9-4b2b-8bde-110f2110102d
merged: 26e3e60787f3589a9727a4d6ecee82eb0105e725
verified: review ok t:25ba756e m:claude-opus-5-5 c:2a0ca2ef; verify ok t:25ba756e
---

## Opportunity
On 2026-10-16 the operator ran `bmesh update` on the Mac after spec 036 landed. The binary at `~/.local/bin/bmesh` was swapped (`bmesh version` printed `1555f80e`), but the launchd daemon `com.buildermesh.bmesh` kept running the copy it started the day before (pid start Oct 15 23:19, binary mtime Oct 16 13:23), and every command still printed `bmesh: ricks-macbook runs 16d6b58f, this is 1555f80e; run bmesh update`. That hint (`cmd/bmesh/cmd_version.go:75`) tells the operator to do what they had just done; the daemon only took the new binary after `launchctl kickstart -k gui/$(id -u)/com.buildermesh.bmesh`. `bmesh update` (README, Self-update) builds and swaps but never touches the running daemon, and says nothing about it. A daemon with `[update] repo` re-execs itself; one without (the Mac's) stays on the old code until someone restarts it, which matters most for fixes that hold only once both boxes run them (spec 037).

## Proposal
In:
- **`bmesh update` names the restart.** After a swap, and on `up to date`, when this host's own daemon's newest `bm.daemon.<host>.started` names another revision than the binary now at the path, it prints one line after its own output: `this box's daemon still runs <old8>; restart it so it runs <new8>: <command>`, where `<command>` is the service manager's restart for the unit `bmesh init --service` writes (`systemctl --user restart bmesh` on Linux, `launchctl kickstart -k gui/$(id -u)/com.buildermesh.bmesh` on macOS, the scheduled task on Windows), the same text `cmd_init_hosts.go:88` already prints for the hosts layer. It never restarts the daemon itself: a restart kills running jobs, and the drain that hands them back is the daemon's own (self-update). No hub, or no `started` for this host: no line.
- **The hint tells a stale daemon from a stale binary.** When the hint names this host's own daemon and the daemon's revision is older than this binary's (by the commit times the binary is stamped with and the `started` event carries; add the time to `started` if it doesn't), the hint reads `bmesh: this box's daemon runs <old8>, this binary is <new8>; restart the daemon: <command>`. Every other case (another host's daemon, a newer daemon, an unknown time) keeps today's `run bmesh update`.
- README Self-update and the hint's paragraph say both lines.

Out: `bmesh update` restarting the daemon, and `[update]` changes.

Judge: opus, because it reads mesh state and service managers across three OSes and a wrong hint misleads the operator the same way today's does.

## What you'll see
On the Mac, `bmesh update` ends with `this box's daemon still runs 16d6b58f; restart it so it runs 1555f80e: launchctl kickstart -k gui/$(id -u)/com.buildermesh.bmesh`, and until the restart every command's hint says to restart the daemon rather than to update.

## Verification
The repo's verify. New tests: `update` against a hub whose own-host `started` names an older revision prints the restart line with the OS's command (one case per GOOS through the command builder); the same with a matching revision prints none; the hint picks the restart text for an older own daemon and keeps `run bmesh update` for another host's daemon and for a newer one.

## Risks and the alternative considered
A daemon run by hand (not under a service) gets a service command that doesn't apply; the line names the unit `init --service` writes, which is the documented way to run it. Alternative: `bmesh update` sends its own daemon a restart request over the hub and lets it drain and re-exec as self-update does. That fixes it outright but adds a control message every box must trust; this spec names the step first.

## Needs from the operator
