---
id: 022
title: bmesh carries what command-center still does for it
kind: maintenance
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
After 005, command-center still holds parts bmesh can't run without:
- The `t3` runner runs `~/repos/command-center/tools/t3_dispatch.py` (517 lines), and that script hard-codes its session token at `~/.config/command-center/t3-session.json` (t3_dispatch.py:63).
- The quota-reset reminder is a separate script on a systemd timer (`reset_reminder.py`, posting to ntfy topic `quota-reset`), and the daemon's quota readings run only when someone has the map open (internal/web/quota.go).
- Scaffold writes `<!-- command-center:... -->` markers into every ship repo (internal/scaffold/scaffold.go:30).
- The SessionStart hook that tells a session which box it is on lives in command-center (`.claude/hooks/this-box.sh`).
- The design specs that AGENTS.md cites live there too.

A box with bmesh and no command-center can't use the t3 runner, gets no reminder, and isn't told where it is. bmesh is meant to run any fleet from XDG config.

## Proposal
Builds on 020 (`done` first). In, all in buildermesh:
- **The T3 adapter is embedded.** `t3_dispatch.py` moves into `internal/runner/t3/` and is embedded with `go:embed`. Before each use it is written to `<cache dir>/bmesh/t3_dispatch.py` whenever those bytes differ, so a self-update never runs an old adapter. `[runner] t3_dispatch = ""` (the new default) means the embedded copy, and a path set there still wins. The script reads its token path from `BMESH_T3_SESSION`, which bmesh sets to `<config dir>/t3-session.json`.
- **The quota reminder is the daemon's.** With `[quota] remind_topic` set, the daemon runs `[quota] command` once an hour by itself. For each provider window with usage, it posts one ntfy message to that topic, scheduled for the reset (ntfy's `At` header). A window is claimed with a KV create on `bm-claims`, key `quota.<provider>.<reset unix>` (age limit 8 days), so two daemons never both post.
- **The skills `buildermesh:sync` and `buildermesh:prune`** are the chat entry points ("sync my repos", "sync pstack", "clean up worktrees"). Each dry-runs first, shows the rows, then applies and reports per box.
- **The SessionStart hook** is in the plugin. Each minute the daemon writes `<state dir>/this-box.txt`, for example `this box is k16; secure: no; on the mesh: ricks-macbook (asleep 2h, secure, admin)`. The hook prints that file and prints nothing when it is missing. It never dials the hub, so headless routine sessions don't wait on it.
- **Notes.** `<config dir>/notes.md` (up to 16 KB) travels in the box's `bm-hosts` entry. `bmesh hosts --notes` prints every box's notes, which is where box facts live now.
- **Scaffold markers.** Scaffold reads both `command-center:` and `bmesh:` markers, writes `bmesh:` markers, and on its next run rewrites an old block in place. No repo gets a duplicate block.
- **A README section, "Several boxes".** It covers a hub on a tailnet, a box joining as a leaf, `init --hosts`, the admin and secure boxes, sleeping boxes, and the NATS config and token kept in `<config dir>`. Nothing in it is about any one fleet.
- **The design specs** `2026-10-14-buildermesh.md` and `2026-10-15-buildermesh-round2.md` move to `docs/design/`, and AGENTS.md points there.

Out: porting the T3 adapter to Go; moving any box's files (023).

Judge: opus, because each piece is named, but the hook, the README section and the marker migration must hold for a fleet that isn't this one.

## What you'll see
A box with only bmesh, once `bmesh init` has run, starts threads in T3, gets reset reminders, and opens every session with one line saying which box it is on. `bmesh scaffold buildermesh --check` shows the markers renamed and nothing else.

## Verification
The repo's verify. New tests:
- the embedded adapter is rewritten when its bytes change;
- a configured path wins over the embedded copy;
- `BMESH_T3_SESSION` reaches the fake;
- one reminder per window across two daemons;
- the hook with and without `this-box.txt`;
- scaffold migrates a `command-center:` block and leaves no duplicate.

Finally, `grep -rn command-center README.md skills docs/agents-snippet.md internal cmd` finds only the legacy-marker reader.

## Risks and the alternative considered
Embedding a Python script keeps uv as a dependency of the t3 runner. A Go port would drop uv, but it means rewriting the one adapter for T3's undocumented API. Embedding keeps that API knowledge in one file, which can be ported later on its own.

## Needs from the operator
