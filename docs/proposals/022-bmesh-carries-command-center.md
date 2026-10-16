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
- **The T3 adapter.** The `t3` runner runs `~/repos/command-center/tools/t3_dispatch.py` (517 lines), which hard-codes its session token at `~/.config/command-center/t3-session.json` (t3_dispatch.py:63). Its tests are command-center's `tests/test_t3_dispatch.py`.
- **The quota-reset reminder** is a separate script on a systemd timer (`reset_reminder.py`, posting to ntfy topic `quota-reset`). The daemon's own quota readings run only when the map is open (web/api.go:619).
- **Scaffold's markers.** Scaffold writes `<!-- command-center:... -->` markers into every ship repo (scaffold.go:31).
- **The session hook** that tells a session which box it is on lives in command-center (`.claude/hooks/this-box.sh`).
- **The design specs** AGENTS.md cites live there too.

A box with bmesh and no command-center can't use the t3 runner, gets no reminder, and isn't told where it is.

## Proposal
The adapter, reminder, marker, README and design-doc pieces build at once, in parallel with 019 and 020. The hook and notes wait for 019 `done`, and the two skills for 020 `done`; a lead may land in two parts. In, all in buildermesh:
- **The T3 adapter is embedded.**
  - `t3_dispatch.py` and its tests move into `internal/runner/t3/`; the tests run under the repo's verify through `uv run`, skipped with a message when `uv` is missing.
  - The script is embedded with `go:embed` and written to `<cache dir>/bmesh/t3_dispatch.py` before each use whenever those bytes differ, so a self-update never runs an old adapter.
  - `[runner] t3_dispatch = ""` (the new default) means the embedded copy, and a path set there still wins.
  - The script reads its token path from `BMESH_T3_SESSION`, which bmesh sets to `<config dir>/t3-session.json`.
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

Out: porting the T3 adapter to Go; moving any box's files (024).

Judge: opus, because each piece is named, but the hook, the README section and the marker migration must hold for a fleet that isn't this one.

## What you'll see
A box with only bmesh, once `bmesh init` has run, starts threads in T3 and gets reset reminders. With `[hosts]` on, every session opens with one line saying which box it is on. `bmesh scaffold buildermesh --check` shows the markers renamed and nothing else.

## Verification
The repo's verify, including the moved adapter tests. New tests:
- the embedded adapter is rewritten when its bytes change;
- a configured path wins over the embedded copy;
- `BMESH_T3_SESSION` reaches the fake;
- one reminder per window across two daemons with jittered resets;
- the topic is substituted into the ntfy URL;
- `hosts --here` with and without the file;
- scaffold migrates a `command-center:` block and leaves no duplicate.

Finally, `grep -rn command-center README.md skills docs/agents-snippet.md internal cmd --exclude='*_test.go'` finds only the legacy-marker reader and the adapter's history note.

## Risks and the alternative considered
Embedding a Python script keeps uv as a dependency of the t3 runner. A Go port would drop uv, but it means rewriting the one adapter for T3's undocumented API. Embedding keeps that API knowledge in one file, which can be ported later on its own.

## Needs from the operator
On k16: set `[quota] remind_topic = "quota-reset"` in `~/.config/bmesh/config.toml`, then disable `command-center-reset-reminder.timer`, in that order, once this lands. (024 removes the units.)
