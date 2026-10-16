---
id: 052
title: bmesh map draws the live mesh as a coloured, pulsing tree in the terminal, readable on a phone over ssh
kind: feature
size: M
judge: opus
status: later
lens: ux
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
The T3 mobile app comes with an ssh terminal into the operator's boxes, but the map is a web page on `[serve] listen` (loopback by default). Seeing it from the phone needs a tunnel or a browser on the tailnet. In the terminal today, the closest things are:
- `bmesh mesh-status`: a table that fills the screen with every done lead (40-odd rows on k16 now). It is a snapshot, and its columns wrap on a 45-column phone screen;
- `bmesh watch`: a log of lines, not a picture.

The operator wants to "see in a glance the map of active nodes" by starting one live command from the phone's terminal, with colour, some aesthetic, and pulsing.

## Proposal
In:
- **`bmesh map [--repo R] [--all] [--once]`**: a full-screen live view in the terminal, built on Bubble Tea and Lip Gloss. Both are pure Go, build for Windows, and fold into the single binary. They are the repo's first UI dependencies, pinned in `go.mod`.
- **Data.** It reads the mesh from any box the CLI can dial:
  - `bm-nodes`, `bm-state` and `bm-runner`, plus unread counts, are shaped by the map's own `web.Tree`, so the terminal and the web page agree on what a tree is. It doesn't need `bmesh serve` on the box.
  - It subscribes to `bm.>` as `watch` does, skipping the same internal subjects.
  - A node event restyles a row at once. Anything it can't place re-reads the tree, debounced as the page does.
- **The picture**, top to bottom:
  - One header line: `bmesh · k16 · 2 working · 3 asking · 1 blocked`, each count in its state colour, plus the quota when it is known (`claude 15%`).
  - A needs-you section: one row per `asking`, `blocked` or `silent` node, with its first line of words cut to the width.
  - The live tree in `tree` style, with `├─` and `└─` guides: repo, then build family (once the coordinator layer, 050, has landed; a lead with no family hangs from its repo), then lead, then executors. A round draws with its engineers.
  - A routines line: `routines · review 12 idle · verify inline`.
  - One folded line: `done 41 · newest buildermesh-045 12m`.

  Live means any state but `done`, and `--all` unfolds the done ones. Each row is a state glyph, the last name of its path, and a dim tail with the age and unread count (`2 unread`).
- **Colour and motion.**
  - The state colours follow the web map's palette, in 256-colour and truecolor, and degrade to bold or dim under `NO_COLOR` or a dumb terminal.
  - `working` rows breathe: the glyph steps through a soft brightness cycle, about 1.6 s per cycle, as the web box's pulse does.
  - `asking` and `blocked` hold steady and bright.
  - An inbox message makes the recipient's row show `← <sender>` in the message kind's colour. It fades over about 10 s, the same timing as the web map's pulse (050).
  - The view repaints at most 10 times a second, and only changed lines are sent, so it stays light over a phone's ssh.
- **Phone first.**
  - At 40 columns nothing wraps: paths cut from the left of the prefix shared with their parent, tails drop before names.
  - It needs no keys to be useful. Keys and a tap add: tap or `enter` opens a row's last event text in place, `d` toggles done, `/` filters by repo, and `q` or `ctrl-c` quits.
  - It restores the terminal on exit and on a dropped connection.
- **`--once`** prints one frame without the alternate screen and exits, for agents, tests and a quick look. Its colour follows `NO_COLOR` and whether stdout is a terminal.
- **README:** the command in the CLI list, a short section under Map, and the dependency named in Build and test.

Out:
- Acting on nodes from the TUI (send, hold, land). It is a view, and actions stay commands.
- The lead page's spec and inbox cards, and the routines page.
- A web-to-terminal bridge through `bmesh serve`'s HTTP.

Judge: opus, because it is a new package with a live event loop and terminal handling across platforms, with taste kept to the palette and motion the web map already set.

## What you'll see
On the phone, in the T3 app's terminal to k16, `bmesh map` fills the screen with:
- a coloured header;
- two asking leads at the top in amber;
- a small tree of the live family, its working executors gently breathing in blue;
- one grey `done 41` line.

When the coordinator sends a lead a message, `← operator` lights up beside it and fades. `q` drops back to the shell.

## Verification
The repo's verify (`GOOS=windows go vet ./...` covers the new dependency on Windows). New tests in the new package:
- the model renders golden frames under `NO_COLOR` at 40 and 100 columns from a fixed tree: needs-you rows, a family, executors, routines and the done fold;
- a node event restyles its row without a re-read;
- an inbox message adds its `←` tail and drops it after the fade;
- nothing wraps at 40 columns.

In `cmd/bmesh`, `bmesh map --once` against `meshtest.Start` with seeded nodes prints the expected frame. A manual look runs `scripts/seed.sh`, then `bmesh map` against its CLI config, in an 80-column and a 45-column terminal.

## Risks and the alternative considered
The new dependencies, Bubble Tea and Lip Gloss and what they pull in, grow the binary and the update surface. They are widely used, pure Go and pinned. Alternatives:
- Hand-rolled ANSI on `golang.org/x/term`, already a dependency: no new modules, but the diffing, resize handling, mouse and Windows console are what Bubble Tea gives for free.
- `watch` with colour: still a log, not a glance.

## Needs from the operator
