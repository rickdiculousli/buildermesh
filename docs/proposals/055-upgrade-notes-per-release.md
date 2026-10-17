---
id: 055
title: bmesh's own releases carry upgrade notes the coordinator distils, and bmesh update walks a box through them, agent or human
kind: feature
size: L
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-17
built_by: c1a518fb-e214-4cbe-94bc-e533d2578bed
host: k16
family: buildermesh-build-8
branch: bmesh/c1a518fb-e214-4cbe-94bc-e533d2578bed
---

## Opportunity
v0.1.0 (tagged at 61587e32) is the first release. The README describes only how things work now, the tag message lists spec titles, and the steps an existing install needs are scattered through each spec's "Needs from the operator", written for this fleet's two boxes. Someone running yesterday's build who runs `bmesh update`:
- hits a config error on every command, `serve` included, if they set `[runner] t3_server = ""` (039). Their daemon restarts in a loop, because nothing loads the config with the new binary before it replaces the old one;
- has leads refused if their T3 Code is older than 0.1.0 (039);
- keeps a hand-run `nats-server`, with no written way to `bmesh hub`. The README covers a fresh setup only, and `init` won't edit an existing `config.toml` (040);
- runs a `bmesh hub` that never updates, with nothing saying it is behind (040);
- has boxes that quietly stop following `main` (047), and builds and landings that gain refusals (043, 044, 045), with no note anywhere.

Specs also overlap: 039 drops the `t3_dispatch` key that 041 then retires along with the file it pointed at, and 040 moves a token that 041's checklist names again. The sum of every spec's steps is longer than what an install actually has to do.

## Proposal
In:
- **Only for bmesh's own repo, with no key.** bmesh knows when it is building itself: the repo's `go.mod` declares the binary's own module, the same test land's scaffold check already makes (`A tree whose go.mod declares this binary's own module`). Everything in the next three bullets applies only there. Every other repo builds and releases exactly as today, with no config, because this ceremony is how bmesh dogfoods its own releases, not a rule for the repos it builds.
- **Each spec says what an existing install must do.** In bmesh's own repo, `bmesh spec template` and the build brief add `## Upgrading`: the steps an install from the previous release needs, written for any fleet, or `nothing`. The lead fills it as built, never with this fleet's paths. A new config key is written as an **ask**: one line saying what changed, the question, the choices and the default.
- **The coordinator distils the notes per release.** Before `bmesh release`, the coordinator (the `build` skill, step 7) reads the `Upgrading` sections of every spec landed since the last tag. It collapses them into the fewest true steps: a key one spec adds and another drops never appears, two moves of the same file become one, and an ask whose default keeps today's behaviour stays an ask. It writes `docs/upgrading/vX.Y.Z.toml`. The coordinator has the final say over what goes in, and the spec sections are its input, not the notes. Each step is one of:
  - `ask`: `key`, `file` (config or registry), `changed` (one line), `question`, `choices`, `default`, and `when` (a config condition, e.g. `[hub] role != ""`);
  - `run`: `command`, `why`, `os` and `when`, and `confirm = true` for anything destructive;
  - `check`: `command` and what its output must show.

  In bmesh's own repo, `bmesh release` refuses (exit 3) when a spec landed since the last tag has a non-empty `Upgrading` section but `docs/upgrading/<the new tag>.toml` is missing. It refuses as well when that file doesn't parse or names a key the new binary doesn't know. The tag message carries the notes rendered as text.
- **v0.1.0's notes, written after the fact: start over.** v0.1.0 was tagged before this spec, so it shipped with no notes. This spec's lead writes `docs/upgrading/v0.1.0.toml` retroactively, and it is the one exception to incremental steps. An install from before v0.1.0 is uninstalled and initialized again, nats included. Its steps:
  1. Check that no lead or executor is live (`bmesh mesh-status`), and finish or forget any that are.
  2. Stop and remove the `bmesh` and `bmesh-hub` services and any `nats-server` service, hub or leaf (systemd user units, launchd agents, the Windows logon task).
  3. Remove `nats-server` if it was installed for bmesh, along with its `nats.conf` and JetStream store.
  4. Move the config directory aside as `<config dir>.pre-0.1.0` (its `repos.toml` is kept, to publish again) and remove the state directory.
  5. Install `go install github.com/rickdiculousli/buildermesh/cmd/bmesh@v0.1.0`.
  6. Run `bmesh init` on the hub box first (hub role, `--hosts --admin`, `--service`), then on every other box with an invite.
  7. Have the admin publish the kept `repos.toml` (`bmesh repos publish`).

  The notes say what a fresh start loses: the results and events in the old JetStream store, and the roster and keys. Every spec's `verified` line keeps its record in git. README gains `## Upgrading`, which leads with this and says that every later release is incremental.
- **`bmesh update` walks a box through the notes, and the new binary does the walking.** The binary being replaced may predate this spec, so it can't be trusted to know the walk. After `update` builds the new binary and before it swaps it in, it runs `<new> update --walk --from <the running binary's version>`, and that does the following:
  - **checks the config.** It loads this box's config with the new binary (`config check`, a new subcommand). On failure it refuses, names the key and keeps the old binary, so a box never restarts in a loop.
  - **runs the asks.** For every release between the `--from` version and the target, in order (a box that jumps from v0.1.0 to v0.3.0 walks v0.2.0's notes, then v0.3.0's), it reads the notes from `docs/upgrading/` on the upstream's `main`, not the tag, so notes written after a tag still apply. In a terminal, each ask is one line, `track: boxes now update to releases, not every landing. Follow releases or every landing? [release]/latest`, and Enter takes the default. Answers are written into the config, with the old file kept as `config.toml.prev`.
  - **shows the run and check steps** that apply to this box (its `os` and the `when` conditions) and runs them with `--apply`, asking first for each `confirm` step.
- **Driven by an agent.** `bmesh update --plan --json` prints the applicable steps, asks included with their defaults, and changes nothing. `bmesh update --answer key=value ...` answers asks, `--defaults` takes every default, and `--apply` runs the run and check steps without a terminal. A `confirm` step without a terminal needs `--yes`. A step that fails stops the walk and names it, and `bmesh update` run again resumes from there (`<state dir>/upgrade.json` holds the version and the last step done).
- **The hub keeps up.** `bmesh update`, `bmesh hosts --here` and `mesh-status` name a `bmesh hub` running an older binary than `serve` on this box: `hub runs v0.1.0, serve v0.2.0: restart bmesh-hub`. A notes step can restart it with a `run`.
- **Where a box starts is read from its binary, not guessed.** `--from` is the running binary's revision, as `update` already resolves it (`Same`, `Contains`). If v0.1.0's commit isn't an ancestor of that revision, the box predates v0.1.0, and the walk starts with v0.1.0's start-over notes before any incremental ones. `init`, and every completed walk, write `<state dir>/installed` (the version and the time). It is a record, not the source of truth.
- **The first hop from old code.** A binary up to and including v0.1.0 swaps in the new one without running `--walk`, and the daemon's self-update from such a binary does the same. So the new binary checks at its first start: when `<state dir>/installed` is missing or older than its own version, `serve` (and every command run in a terminal) prints, and the daemon logs and pushes once, `upgrade notes pending since <installed or v0.1.0>: run bmesh update`. That `update` walks from the recorded version, or, with no record, from the version that the previous binary's revision resolves to (kept as `<state dir>/previous` by the swap, else v0.1.0). `serve` runs `config check` at startup, and a failure exits with a code launchd and systemd don't restart in a loop (`RestartPreventExitStatus`, a launchd exit-timeout guard) and names the key.

Out: rolling back to an older release; notes for repos other than bmesh's own (a ship repo's releases); an automatic walk inside the daemon's self-update. The daemon keeps its config check and its swap, and when a step needs an answer or `confirm`, it logs and pushes `update to v0.2.0 waits on bmesh update (2 asks)` rather than guessing.

Judge: opus, because it changes the spec template, `release` and `update`, which every box and every lead depends on, and its v0.1.0 steps have to be proved on a real install.

## What you'll see
- On a box at v0.1.0 after v0.2.0 is out, `bmesh update` prints `v0.1.0 -> v0.2.0: 1 ask, 1 step`, asks one line, shows `restart bmesh-hub (it runs v0.1.0)`, runs it with `--apply` and ends `at v0.2.0`.
- An agent runs `bmesh update --plan --json`, then `bmesh update --defaults --apply --yes`, and reports the same.
- A config the new binary rejects leaves the box on its old binary with `config check failed: [runner] t3_server is empty ...`.

## Verification
- The repo's verify, plus:
  - `cmd_release_test.go`: in bmesh's own repo, release refuses when a landed spec has `Upgrading` steps and the notes file is missing, and when the file names an unknown key; in any other repo, release and `spec template` behave as today;
  - `cmd_update_test.go` over a bare origin with two tags and notes: `--plan --json` lists the asks and steps between the versions; `--defaults` writes the defaults and keeps `config.toml.prev`; `--answer` overrides one; a failing `run` stops, and the next run resumes after the last step done; a config the new binary rejects keeps the old binary; `--walk --from` a pre-v0.1.0 revision starts with v0.1.0's notes, and from v0.1.0 to a third tag walks both later notes in order; a new binary started with no `installed` record prints the pending line, and `serve` with a bad config exits naming the key;
  - the hub version line, with a fake hub process.
- **Live, by the lead:** the v0.1.0 notes walked end to end on a sandboxed HOME on k16 (a pre-0.1.0 install from commit 9a5e181a, nats-server included, uninstalled and initialized again by following `docs/upgrading/v0.1.0.toml` with `--plan --json` then `--apply`), recorded in the closing commit.

## Risks and the alternative considered
- **A binary from before v0.1.0 that updates itself** runs its own old update code, which knows neither the walk nor the start-over, and swaps in the newest code. The new binary's first start finds no `installed` record and names the pending notes. Its walk starts from v0.1.0 unless the swap left `<state dir>/previous`, and old code never does. The README's Upgrading section therefore leads with the start-over, and `config check` at `serve` startup names the keys from before v0.1.0 (`t3_server = ""`) instead of restarting in a loop.
- **A distilled note can drop a step a spec needed.** The coordinator's collapse is checked by `release` (every key it names must exist), and the notes walk on a sandbox before a tag is the lead's live check from the second release on.
- **Alternative: concatenate the specs' sections.** That is mechanical and nobody has to judge it, but it hands an install every intermediate step: keys added then dropped, files moved twice. The operator ruled that out: the coordinator teases out the minimal steps.

## Upgrading
- The units `bmesh init --service` writes gain a guard against a restart loop on a config bmesh refuses (`serve` and `hub` exit 78): the systemd units `RestartPreventExitStatus=78`, the launchd `serve` agent `ThrottleInterval` 300. A box with units from before runs `bmesh init --service` again, which leaves each new unit beside the old as `<unit>.new`; it takes the new one (diff, move over, `systemctl --user daemon-reload` or `launchctl bootout` then `bootstrap`).
- Nothing else for an install at v0.1.0 beyond what the notes walk does itself. The template's new section appears only in bmesh's own repo.

## Needs from the operator
