---
id: 022
title: bmesh carries what command-center still does for it
kind: maintenance
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
built_by: f41b60d8-c71a-44bf-ba58-d575e87ada5c
host: k16
family: buildermesh-build-2
branch: bmesh/f41b60d8-c71a-44bf-ba58-d575e87ada5c
---

## Opportunity
After 005, command-center still holds parts bmesh can't run without:
- **The quota-reset reminder** is a separate script on a systemd timer (`reset_reminder.py`, posting to ntfy topic `quota-reset`). The daemon's own quota readings run only when the map is open (web/api.go:619).
- **Scaffold's markers.** Scaffold writes `<!-- command-center:... -->` markers into every ship repo (scaffold.go:31).
- **The session hook** that tells a session which box it is on lives in command-center (`.claude/hooks/this-box.sh`).
- **The design specs** AGENTS.md cites live there too.

A box with bmesh and no command-center gets no reminder and isn't told where it is. (The T3 adapter was on this list. 029 replaces it with a Go client for T3 0.1.0's MCP tools, and 024 deletes the V1 adapter, so nothing of it moves here.)

## Proposal
It builds after 029 is `done`, which gives bmesh its T3 client. The reminder, marker, README and design-doc pieces then build at once, in parallel with 019 and 020. The hook and notes wait for 019 `done`, and the two skills for 020 `done`; a lead may land in two parts. In, all in buildermesh:
- **The quota reminder is the daemon's.** With `[quota] remind_topic` set, the daemon takes a quota reading once an hour itself, through the same 5-minute gate the map's readings use, so the usage page isn't asked more often.
  - For each provider whose 5-hour window has usage, it posts one ntfy message with the 5-hour window's reset (as `reset_reminder.py` does; the body adds the weekly percentage).
  - The post goes to `remind_topic`: the last path segment of `[notify] ntfy`'s URL is replaced by that topic. It is scheduled for the reset with ntfy's `At` header, and the reset is rounded to the minute, since Claude's reset time jitters.
  - The window is claimed with a KV create on `bm-claims`, key `quota.<provider>.<rounded reset unix>` (age limit 8 days), so two daemons never both post.
- **The skills** `buildermesh:sync` (after 020) and `buildermesh:prune` (after 023's `prune`; until then it says prune is not yet there) are the chat entry points: "sync my repos", "sync pstack", "clean up worktrees". Each runs dry first, shows the rows, then applies and reports per box. They name nothing of any fleet.
- **The session hook** is in the plugin's `hooks/hooks.json`. It runs `bmesh hosts --here`, which reads only `<state dir>/this-box.txt` (written each minute by the daemon) and prints it, for example `this box is k16; secure: no; on the mesh: ricks-macbook (asleep 2h, secure, admin)`. It prints nothing when the file is missing or `[hosts]` is off. It never dials the hub, so headless routine sessions don't wait on it.
- **Notes.** `<config dir>/notes.md` (up to 16 KB) travels in the box's signed presence (019). `bmesh hosts --notes` prints every box's verified notes. Box-specific session guidance that `this-box.sh` carried today goes there.
- **Scaffold's markers.** Scaffold reads both `command-center:` and `bmesh:` markers and writes `bmesh:`. On its next run it rewrites an old block in place, so no repo gets a duplicate block.
- **README "Several boxes"** covers:
  - a hub on a tailnet, and a box joining as a leaf;
  - `init --hosts`, the admin and secure boxes, and the Keychain dialog;
  - sleeping boxes;
  - the NATS config and token kept in `<config dir>`.
  
  Nothing in it is about any one fleet.
- **The design specs** `2026-10-14-buildermesh.md` and `2026-10-15-buildermesh-round2.md` move to `docs/design/`, and AGENTS.md points there.

Out: the T3 adapter (029 for V2, 024 removes V1); moving any box's files (024).

Judge: opus, because each piece is named, but the hook, the README section and the marker migration must hold for a fleet that isn't this one.

## What you'll see
A box with only bmesh, once `bmesh init` has run, gets reset reminders. With `[hosts]` on, every session opens with one line saying which box it is on. `bmesh scaffold buildermesh --check` shows the markers renamed and nothing else.

## Verification
The repo's verify. New tests:
- one reminder per window across two daemons with jittered resets;
- the topic is substituted into the ntfy URL;
- `hosts --here` with and without the file;
- scaffold migrates a `command-center:` block and leaves no duplicate.

Finally, `grep -rn command-center README.md skills docs/agents-snippet.md internal cmd --exclude='*_test.go'` finds only the legacy-marker reader and the V1 `t3_dispatch` default that 024 removes.

## Risks and the alternative considered
The adapter was to be embedded here as a Python script run through `uv`. T3 0.1.0 removed the API it speaks, so 029 writes the V2 client in Go and this spec depends on 029 instead of carrying the adapter.

## Needs from the operator
On k16: set `[quota] remind_topic = "quota-reset"` in `~/.config/bmesh/config.toml`, then disable `command-center-reset-reminder.timer`, in that order, once this lands. (024 removes the units.)
