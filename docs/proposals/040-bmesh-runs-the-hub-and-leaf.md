---
id: 040
title: bmesh hub runs a mesh's NATS hub or leaf itself, so several boxes need no nats-server of their own
kind: feature
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
bmesh is standalone on one box: `serve` embeds the hub (`internal/mesh/embedded.go`, nats-server v2.15.1 as a library). For several boxes, README "Several boxes" sends you to install `nats-server` yourself, hand-write a `nats.conf` with the token pasted in, and run it as a service. That is the one part of a two-box mesh bmesh doesn't own, and it is why this fleet's NATS files still sit outside bmesh:
- k16's hub is `nats.service` running `~/.local/bin/nats-server -c ~/.config/command-center/nats.conf`, its JetStream store in `~/.local/state/rickdiculousli-tools/lanes/jetstream` (4 MB, named for a project two renames ago).
- The Mac's leaf is the launchd agent `io.nats.leaf`, reading `~/.config/command-center/nats.conf`, and that file's header comment holds the token in plain text.
- Both boxes' `[mesh] token_file` is `~/.config/command-center/nats.token`.

Both servers are v2.15.1, the version bmesh embeds, so the store moves as is.

## Proposal
In:
- **`bmesh hub`**, a long-running command that runs the embedded server as this box's hub or leaf, from a new `[hub]` table:
  ```
  [hub]
  role = ""          # "" = no hub here (one box: serve embeds it; or a client of another box's hub) | "hub" | "leaf"
  listen = ""        # hub: "<tailnet ip>:4222"; leaf: "127.0.0.1:4222"
  leaf_listen = ""   # hub only: "<tailnet ip>:7422"
  remote = ""        # leaf only: "nats://<hub tailnet ip>:7422"
  store = ""         # hub only; "" = <state dir>/hub/jetstream
  max_file_store = "8GB"
  max_memory_store = "256MB"
  ```
  The server name is `[host] name`, the JetStream domain `[mesh] domain`, the token `[mesh] token_file` (client auth by token; leaf auth user `leaf`, the token as password, as the README's conf does today). A hub runs JetStream; a leaf has none and dials `remote`, reconnecting forever. It refuses to start with `role` unset, a `listen` of `0.0.0.0`, `::` or an empty host, or a missing token file, each naming the key.
- **Its own process, not part of `serve`.** k16's `serve` re-execs on every landing on main (38 times on 2026-10-16); a hub inside it would drop every box on each one. `bmesh hub` runs the binary it started with until its service is restarted by hand (out: updating the hub automatically).
- **The service.** `bmesh init --service` with `[hub] role` set also writes the hub's unit: a systemd user unit `bmesh-hub.service` or a launchd agent `com.buildermesh.bmesh-hub`, `Restart=always` / `KeepAlive`, since the tailnet address may not exist at boot. It builds on the service code spec 034 (Windows keeps serve running) is changing; whichever lands second rebases onto the other.
- **`init`'s multi-box answer** asks hub, leaf or client and writes `[hub]` and `[mesh] nats` to match (`nats://<listen>` on the hub, `nats://127.0.0.1:4222` on a leaf), and generates `<config dir>/nats.token` (0600) on the hub when there is none.
- **README "Several boxes"** says `bmesh hub` and `[hub]`, with the hand-run `nats-server` kept as a one-line alternative; Config lists `[hub]`.
- **k16, by the lead:** stop `nats.service`; move the store to `<state dir>/hub/jetstream` and `nats.token` to `~/.config/bmesh/`; set `[hub]` and `[mesh] token_file`; `init --service`; start `bmesh-hub`; check that `bmesh hosts`, a `result find` of a known result and the roster read as before, and that the Mac's leaf reconnects; then disable and remove `nats.service` and `~/.local/bin/nats-server`. One hub stop, between leads.

Out: running the hub inside `serve`; clustering several hubs.

Judge: opus, because it moves the live store every box depends on.

## What you'll see
A second box joins with `bmesh init` and `bmesh init --service`: no `nats-server` to install, no `nats.conf` to write. On k16 `systemctl --user status bmesh-hub` runs and `nats.service` is gone; on the Mac `launchctl list` shows `com.buildermesh.bmesh-hub` instead of `io.nats.leaf`.

## Verification
The repo's verify, plus tests that start a hub and a leaf with `bmesh hub`'s code on loopback ports, publish and read JetStream through the leaf, stop and restart the leaf and see it reconnect, and that each refusal names its key. The live k16 move above, recorded in the closing commit.

## Risks and the alternative considered
The store move is the one irreversible step: the lead copies the store, starts the new hub on the copy, and only removes the old directory once the checks pass. The Mac's leaf keeps working through the move, since it only dials the hub's leaf port.

The alternative was the hub inside `serve`, one process per box. It drops the mesh on every self-update, which is many times a day on k16.

## Needs from the operator
On the Mac, after the lead's k16 move: `launchctl unload ~/Library/LaunchAgents/io.nats.leaf.plist` and delete it, move `~/.config/command-center/nats.token` to `~/.config/bmesh/`, set `[hub] role = "leaf"`, `listen` and `remote` and `[mesh] token_file` in `~/.config/bmesh/config.toml`, run `bmesh init --service`, then delete `~/.config/command-center/nats.conf` (it holds the token in a comment) and `brew uninstall nats-server`.
