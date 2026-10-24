---
id: 095
title: The binary carries its plugin, so a box's skills always match its bmesh
kind: maintenance
size: L
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity

A box's skills and its binary come from two places, so they drift both ways. Sessions load the plugin from a directory marketplace on the operator's own checkout of this repo, `~/repos/buildermesh` on both of the operator's boxes; they read it live. The binary comes from a release, through `bmesh update` or the daemon's update.

On 2026-10-24:

- **k16's skills ran ahead of its binary.** Its checkout had the `work` skill's new `bmesh build` forms while the binary was still v0.4.7, which refused them.
- **The Mac's skills lagged behind.** After v0.4.8 its binary was current, but its sessions had no `work` and no `ponytail-review` skill. The Mac is secure, so its self-level held the checkout, 62 commits behind, until the operator pulled it by hand.

`bmesh update` never moves that checkout: it builds in a checkout of its own. So whether a box's skills match its binary depends on who last pulled where.

The operator decided (2026-10-24): skills go live when he runs `bmesh update`.

## Proposal

The plugin is part of the binary, and each box's sessions load the copy its own binary wrote.

In:

- **Embed the plugin** in the binary from the repo, as root `assets.go` already embeds `docs/agents-snippet.md` and the examples: `.claude-plugin/`, `skills/`, `hooks/` and `third_party/ponytail/LICENSE`. That's about 150 KB. The files stay in the repo for reading and editing; the embed means the two never differ.
- **Write it to a plugin dir** of bmesh's own, for example under the state dir or `~/.bmesh/plugin`, with a config key if the path should be settable.
  - It's written whole, through a temporary dir and a rename, so a session never reads half of one.
  - It's rewritten only when its stamp, the revision of the binary that wrote it, differs from the running binary's.
  - The binary writes it:
    - after `bmesh update` swaps it in, as the new binary;
    - when the daemon starts, which covers a daemon that updates itself and a fresh `go install`.

  Whatever moves the binary moves the skills, on a secure box too: the operator's decision above. Nothing about it is held.
- **Point Claude Code at it** with one idempotent command that a person, `bmesh init` and the upgrade notes all run, for example `bmesh plugin --install`. It:
  - writes the dir;
  - removes a `buildermesh` marketplace that reads another path;
  - adds the dir as the `buildermesh` marketplace;
  - installs `buildermesh@buildermesh` at user scope;
  - prints what it did.

  The plugin's name and skill names (`buildermesh:work`, ...) don't change. `bmesh plugin` with no flag says which dir the plugin is in, which revision wrote it, and whether Claude Code reads it.
- **What counts as a repo sessions load** becomes a checkout that some installed marketplace reads (Claude Code's, or a Codex install's), not any checkout with a `.claude-plugin/marketplace.json` in it. After the switch, the operator's checkout of this repo is no longer such a repo, so a secure box levels it like any other. Then re-examine the `[update] repo` part of the hold rule: `bmesh update` builds in a checkout of its own and never from that working tree. If nothing that sessions or the binary load comes from it, drop that part; either way, say why in the report.
- **Docs:**
  - the README's install and the manual's Install, Updates and The plugin sections;
  - the hold rule in Levelling;
  - internals: the embedded plugin and its dir;
  - AGENTS.md's paragraph on how a landing reaches a box, which then holds for skills too;
  - the work skill's Releases: a release now moves every box's skills when the box updates.

  Trying skills before a release is the skill test's cand arm, or `claude --plugin-dir <checkout>`.

Out:
- a Codex plugin of bmesh's own (there is none);
- the skills' text;
- how a box updates its binary.

Judge: opus, because it changes how every box loads what its sessions follow, and the switch must leave no box without the plugin.

## What you'll see

- **Install:** `go install ...` then `bmesh init`, or `bmesh plugin --install`. `claude plugin list` shows `buildermesh@buildermesh` read from bmesh's plugin dir.
- **After the operator runs `bmesh update` on the Mac,** a new session there has the release's skills, with nothing to pull or approve.
- **On k16,** a skill change that lands on main reaches its sessions at the next release, together with the binary.
- **On a secure box,** the operator's checkout of this repo no longer waits on an approval.

## Verification

- **The repo's verify.**
- **Tests:**
  - The written dir equals the embedded files, byte for byte.
  - A stale stamp rewrites the dir; a current one leaves it alone; a write that fails leaves the old dir whole.
  - The daemon writes the dir at start, and `update` writes it after a swap.
  - `--install` against a shell fake of `claude` in `testdata/` records the remove, add and install, and changes nothing on a second run.
  - The new "loads plugins" rule holds a checkout a marketplace reads, and levels the same checkout once nothing reads it.
- **On the real thing, without touching any box's own plugin:**
  1. Build the binary.
  2. Run `bmesh plugin --install` with `CLAUDE_CONFIG_DIR` set to a scratch dir.
  3. Run `claude plugin list` with the same `CLAUDE_CONFIG_DIR`: it must show the plugin read from bmesh's plugin dir.
  4. Run a headless `claude -p` there that lists its `buildermesh:` skills.

## Risks and the alternative considered

- **The switch removes and re-adds the marketplace.** A failure in between could leave a box without the plugin. So `--install` checks the plugin is installed before it exits, and says how to put the old marketplace back if it isn't. Each box's upgrade notes run it once.
- **Developing a skill no longer changes the developer's own sessions until a release.** That's the point. The skill test measures a skill change before it lands.
- **Alternative considered:** a release checkout that `bmesh update` moves to the tag, with the marketplace reading it. It needs git and tags on every box, and on a secure box its move is another hold. The embedded copy is the binary's own, so there's nothing to keep level.

## Upgrading

Each box with Claude Code switches once. Run on every OS, after the update: `bmesh plugin --install`. It writes the plugin this binary carries to `<state dir>/plugin`, removes a `buildermesh` marketplace that reads anything else (a checkout of this repo, or the GitHub repo), adds the dir as the `buildermesh` marketplace and installs `buildermesh@buildermesh` at user scope; a second run changes nothing. A box without `claude` on PATH has nothing to switch: the command is refused there (exit 3) with the dir written, so the step runs only where `claude` is. Check: `bmesh plugin` prints `claude: reads it (buildermesh@buildermesh, user scope)`. A new session loads the plugin from the dir; sessions already open keep what they loaded.

## Needs from the operator

- Cut a release when you want the boxes to switch (this build lands, it doesn't release); each box's `bmesh update` runs `bmesh plugin --install` from the release's upgrade notes (on a box whose daemon updated itself, `bmesh update` walks the notes it left pending). check: bmesh plugin
