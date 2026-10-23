---
id: 072
title: bmesh hub restarts itself onto a new release once serve runs it, so no box's hub is left on an old binary
kind: time-cost
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-17
built_by: 5619efb3-36f6-411c-841a-1eade42b3b95
host: k16
family: buildermesh-build-13
---

## Opportunity
`bmesh hub` runs the binary it started with until someone restarts its service by hand. On 2026-10-17 k16's session hook printed `hub runs v0.3.0, serve v0.3.1: restart bmesh-hub`, a release after the hub last started. Every release that touches the hub needs a `run` step in its upgrade notes to restart it (v0.3.0's notes carry one each for systemd, launchd and Windows). Between releases the stale line just sits on `hosts --here`, `mesh-status` and `update` until a person acts.

Spec 040 (the hub) put this out on purpose: k16's `serve` re-exec'd on every landing on `main` (38 times on 2026-10-16), and a hub inside that cycle would have dropped every box each time. Spec 047 since moved boxes to `[update] track = "release"` (the default, and k16's), so a box's binary now changes once per release. The premise of 040's exclusion no longer holds for a box that follows releases.

## Proposal
In: `bmesh hub` checks once a minute whether this box's `serve` runs a newer binary than itself. That is the comparison `selfupdate.HubLine` already makes from `<state dir>/serve.json` and `hub.json`. It also requires serve's record to name a release. When both hold, the hub logs `bmesh hub: serve runs <release>, restarting as <exe>`, shuts the NATS server down cleanly (the JetStream store is flushed, as on SIGTERM) and re-execs its own executable, as `serve` does after a swap (`reexec`, `cmd/bmesh/reexec_unix.go`). The new process writes `hub.json` again. Every client and leaf reconnects on its own: nats.go clients redial by default, and a leaf redials forever (040).

- Only a release moves the hub. On a box with `track = "latest"`, or after `bmesh update --latest`, serve's binary carries no release, so the hub stays put and the stale line still prints, keeping 040's reason where it still applies.
- Windows stays as it is: no service restarts a hub there (`init` writes none), and `reexec` there exits rather than replacing the process, so the line stays and the notes' Windows step remains.
- Release notes stop needing a hub-restart step for Linux and macOS. The `build` skill's step 7 doesn't name one, so nothing there changes.

Out: restarting a hub whose binary is older but has no release to go to; timing the restart around running leads (the gap is about a second, and the mesh's clients already ride through a hub restart); clustering hubs so one can restart under load.

The cost, for the operator to weigh: each release drops every box's connection to the hub for about a second, the way a hand restart does now. A JetStream request in flight at that moment (a `result` read, a KV put) fails once and the command reports it.

Judge: opus, because the restart has to leave the store and the leaves whole.

## What you'll see
After the next release, within a minute of k16's `serve` restarting, `journalctl --user -u bmesh-hub` shows `bmesh hub: serve runs v0.3.3, restarting as /home/rick/.local/bin/bmesh`. The Mac's leaf relinks, and `hub runs …: restart bmesh-hub` no longer prints anywhere.

## Verification
Failing first: a test in `cmd/bmesh/cmd_hub_test.go` starts the hub on loopback ports with the re-exec stubbed, then writes a `serve.json` naming a newer release. Today nothing happens. After the change, the stub is called within one check and the hub shut down first. The same test with serve's record carrying a revision and no release sees no call. A test reads back a JetStream key written before the re-exec, through a hub started again on the same store. Live on k16 after the next release: the hub's journal line above, `bmesh hosts --here` printing no stale-hub line, and `bmesh hosts` reading the Mac awake. The manual's Stale hub paragraph, the `bmesh hub` section and internals say when the hub restarts itself.

## Upgrading
nothing: no config, registry or record changes. A hub on an older release doesn't restart itself, so the hub has to be restarted by hand once more (the restart that brings in this release).
