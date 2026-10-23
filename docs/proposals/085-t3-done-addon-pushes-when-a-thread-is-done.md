---
id: 085
title: An optional t3-done add-on pushes to ntfy when a T3 thread a person started is done, failed or asking
kind: feature
size: M
judge: opus
status: accepted
lens: eng
proposed_by: operator
proposed_on: 2026-10-22
---

## Opportunity
bmesh pushes a lead's ask, blocked and done, but nothing tells the operator when a T3 thread they started themselves finishes. T3's own desktop notifications reach only the box running the app. Today a hand-written Python script (`t3-ntfy`, a systemd service on one box) covers that gap by polling T3's database.

The script's first version showed how T3 actually signals "done". Every turn end is a run row that ends `completed`. That includes a wake-up from a background task: a message with `notification.source.kind = "background_task"` starts a new run. On 2026-10-23 a build coordinator watching its lead with `bmesh watch` posted three "done"s in 40 seconds while T3's sidebar showed it waiting. The difference is `pendingBackgroundTasks` on the thread's provider record (`orchestration_v2_projection_provider_threads.payload_json`). It still listed the coordinator's monitor, and T3 cleared it as soon as the task ended. T3's API (`t3_thread_read`, `t3_thread_list`) doesn't return that field, as a probe task showed: the database listed it and the API didn't.

## Proposal
In:
- **Add-ons.** A new `addons/` directory holds optional parts compiled into `bmesh` (one Go package each, registered by name). `bmesh serve` runs an add-on only when the box's `config.toml` lists it in `[addons] enable`. The default `[]` runs none. An unknown name refuses at config load. Each add-on's settings live in `[addons.<name>]`.
- **`addons/t3done`, name `t3-done`.**
  - **Settings:** `[addons.t3-done] ntfy = "<scheme://host/topic>"` (required when enabled; its own address, so turning it on never starts `[notify]`'s digest) and `every` (default `5s`).
  - **What it reads:** this box's T3 database, `~/.t3/userdata/statev2.sqlite`, read-only through a pure-Go SQLite library. At start it checks that the tables and columns it reads exist. If any is missing (a T3 update changed them), it logs one line naming it and stays idle.
  - **When it queries:** each tick it compares the database file's and its `-wal` file's modification time and size with the last tick, and queries only when either changed. An idle T3 costs one file check per tick.
  - **What posts:**
    - "done": a run ends `completed` and its provider thread has no `pendingBackgroundTasks`;
    - "failed": a run ends `failed`;
    - "asking": a runtime request turns `pending`.
    
    `interrupted`, `cancelled` and `rolled_back` post nothing.
  - **Which threads:** top-level threads a person started. Skipped are threads with a `lineage.parentThreadId` and threads whose `createdBy` is `agent` (bmesh's leads and rounds, which push on their own).
  - **Once each:** a high-water mark per kind (run `completed_at`, request `created_at`) lives in the state dir. Every row read moves it, posted or not, so a run skipped as a wake-up never posts late. A fresh start begins at the current time.
  - **The message:** the title is the thread's title. The body is `done in <project> on <box>: <the start of the last assistant reply, ~140 chars>` (likewise `failed in …` and `asking for input in …`). The tag is `white_check_mark`, `x` or `raising_hand`. When `[console] thread_url` is set, a `Click` header opens the thread through it.
- **Docs.** The manual covers `[addons]`, `t3-done`'s keys, what it posts, and its idle-on-mismatch line; the README's index lists the add-on. Internals covers the tables and fields it reads and the rule for "done".

Out:
- Add-ons as separate programs the daemon starts and supervises.
- T3's API as the source, until it returns `pendingBackgroundTasks`. The add-on moves to the API then.
- Other runners' threads (claude, tmux).

Judge: opus, because the "done" rule turns on T3 internals and a wrong rule fails quietly, as the script's first version did.

## What you'll see
- With `[addons] enable = ["t3-done"]` and an ntfy address, a thread you started posts once when it ends a turn with nothing left in the background. A coordinator waiting on `bmesh watch` posts nothing until its watch ends and it reports.
- A failed run or a question to you posts at once.
- bmesh's leads, rounds and delegated children never post on this topic.
- Without the config lines, nothing changes.

## Verification
The repo's verify, plus tests:
- **`addons/t3done`, against a fixture database** made with T3's layout (the tables and columns read, with real payload shapes).
  - A `completed` run with an empty `pendingBackgroundTasks` posts once.
  - The same run with a pending monitor posts nothing, and it doesn't post later when the list empties.
  - `failed` posts, `interrupted` doesn't, and a `pending` request posts "asking".
  - A child thread and an agent-created thread don't post.
  - A missing column gives the idle line and no posts.
  - An unchanged file runs no query.
  - The message's title, body, tags and `Click` header are as above (against `testdata/fake-ntfy.sh`).
- **`internal/config`.** `[addons]` parses, an unknown name refuses, and `t3-done` without `ntfy` refuses.
- `GOOS=windows go vet ./...` still passes with the SQLite library.

## Risks and the alternative considered
- **T3's database layout is private.** A T3 update can rename what the add-on reads. The start check turns that into one idle line, not wrong pushes.
- **Binary size.** A pure-Go SQLite library adds several MB to `bmesh` on every box, including boxes that never enable the add-on. The builder reports the measured size in its done.
- **A wake-up that arms a new background task** before its run ends still reads as waiting, which is right. A thread whose background task ends without a new run stays silent until its next turn.
- **Alternative: T3's API only.** It can't tell a wake-up from the end. It either posts every wake-up or misses the "done" that follows a watch.
- **Alternative: keep the hand-written script.** It works, but it lives outside the repo, untested, on one box.

## Upgrading
- bmesh has optional add-ons, all off by default. Turn on `t3-done`, which pushes to ntfy when a T3 thread you started on this box is done, failed or asking? Choices: `[addons] enable = ["t3-done"]` with `[addons.t3-done] ntfy = "<ntfy URL with its topic>"`, or leave it unset. Default: off.

## Needs from the operator
- [k16] In `~/.config/bmesh/config.toml`, set `[addons] enable = ["t3-done"]` and `[addons.t3-done] ntfy = "http://127.0.0.1:2586/t3"`, then stop the hand-written script: `systemctl --user disable --now t3-ntfy.service && rm ~/.config/systemd/user/t3-ntfy.service ~/.local/bin/t3-ntfy && rm -r ~/.local/state/t3-ntfy`. check: ! systemctl --user is-enabled t3-ntfy.service
- [ricks-macbook] In `~/.config/bmesh/config.toml`, set `[addons] enable = ["t3-done"]` and `[addons.t3-done] ntfy = "http://k16:2586/t3"`, then remove the script and its saved copies: `rm ~/.local/bin/t3-ntfy ~/.config/bmesh/notes/t3-ntfy ~/.config/bmesh/notes/sh.ntfy.t3-done.plist`.
- [k16] Update the box notes: replace the `t3-ntfy.service` line with the add-on.
- [ricks-macbook] Update the box notes: replace the T3 paragraph under Notifications with the add-on.
