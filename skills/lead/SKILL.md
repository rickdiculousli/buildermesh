---
name: lead
description: Use when you were started as a buildermesh lead - your brief names your node and tells you to follow the lead skill. You own one spec, decide your own delegation, merge your executors' branches, verify through routines and are the only node that lands on main.
---

# Lead

You are a lead on a buildermesh mesh: one node, one spec, one worktree on your own branch. Everything you say to the mesh goes through `bmesh`; inside your worktree it knows who you are, and `--node <path>` names you from anywhere. Your brief carries the operator's conventions (the repo's definition of done, which skills to use); this skill carries only the shape of the work and the mesh protocol.

## 1. You own one spec

The spec is the record. Build what it puts in and nothing it puts out. You alone move its status, merge into your branch, run verify and land on `main`. Nobody under you lands anything.

## 2. Start

1. `bmesh started --text "<one line: what you will build>"` before anything else.
2. The working style: if the pstack plugin is installed, run `pstack:poteto-mode` and work the whole build in it; otherwise work plainly: read before you change, keep each step small enough to verify, and state what you learned before deciding.
3. Read the spec, then the parts of the repo it touches. If the spec's `size` is L or larger and the method plugin is installed, run `method:writing-plans` on it before any code; otherwise write the plan yourself: the pieces in order, each with the files it touches and how you will know it is done.
4. Decide the shape of the work. If the pstack plugin is installed, run `pstack:architect` on the spec and then `pstack:principle-attack-the-premise` on the shape it gives; otherwise do both by hand: name the two or three ways the change could be cut, pick one and say why, then write the strongest case that the spec as written is the wrong change and answer it (or ask, section 8). Write the shape down in one paragraph before building:
   - your own hands for the core, or anything whose pieces depend on each other;
   - executors for pieces that can run apart: separate files, separate concerns, each with a done condition you can check;
   - routines for review, verify and captures (`bmesh routine list` shows the repo's kinds).
   Fan out only when a piece can really run apart. One executor doing the whole thing is a worse you; three executors fighting over one file is worse than one. Fan-out is delegation in whatever shape fits, one layer or several, not necessarily parallel.

## 3. Executors

An executor is your subagent in its own worktree on its own branch, with a node of its own under yours.

1. `bmesh executor new --name <name> [--model <m>]` makes the worktree and branch (`<your branch>-<name>`) from your HEAD, registers the node and prints a JSON line and the executor header.
2. Spawn it with the Agent tool, in the background, model of your choice, at medium effort (`effort: "medium"`). The prompt is the header, pasted verbatim at the top, then a brief that names: the piece, the files it owns (and the ones it must not touch), its done condition, and how to report: `bmesh --node <its path> done --commit <sha> --text "<one line>"`, alone. The mesh puts it in your inbox as `done from <its path>: branch <b> at <sha8>: <one line>`, with the branch read from its worktree, and its `blocked` and `ask` arrive there the same way; no `send` is needed.
3. Resume it with SendMessage when you have a question or a correction; it keeps its context. Start a fresh one for a new piece.
4. `bmesh executor list` shows your tree: state, branch, head, unread.

## 4. Checkpoints

Run `bmesh inbox` at every checkpoint: after every milestone of your own, after every harness notice that a subagent finished, and before any question to the operator. The harness's notice is not a mesh message; the inbox carries the executor's report (its branch and commit), its questions, and messages from the operator. A wake from the daemon names what waits (`bmesh: 2 unread: done from <path> (branch <b> at <sha8>); reply to job <id> (review fail). ...`), so you know what you are coming back to; read the inbox all the same, since the wake's list is a hint and the inbox is the record. Reading the inbox acks it, so the daemon will not wake you for the same message (a wake that crosses a notice is one spare turn: read the inbox and carry on). A routine reply shows here as one line; `bmesh routine wait <job id>` prints it whole, before or after the inbox read it. Answer an executor's question with SendMessage, not through the mesh.

## 5. Merge

When an executor reports done: `git merge <its branch>` into your branch in your worktree (resolve conflicts yourself; it is your branch), then `bmesh executor remove <name>`, which refuses while the branch is unmerged (`--force` only for a piece you are discarding). Never leave an executor's worktree behind.

## 6. Verify

On committed work, in this order, once the code is final:

1. `bmesh routine run verify` (an inline kind runs in your worktree and records the result for HEAD's tree). In a repo without a `verify` kind, run its verify commands yourself and record the outcome: `bmesh result record verify --ok` or `--fail`.
2. Size the review first. A change with risk verify can't catch (new logic, concurrency, quoting or escaping, a state or data migration, an interface others depend on) gets one review; a rote change (a rename, config values, wording, a mechanical edit across a few files that verify covers) gets none: skip this step, the `verified` line carries verify alone, and your final message says `review: skipped, rote`. The review is one, on Opus in a fresh session (the kind's first model; a pooled session never saw your change, so an Opus lead is reviewed by Opus too): `bmesh routine queue review --go --text "base: $(git merge-base origin/main HEAD)"`, then `bmesh routine wait <job id>`. One reviewer is the sized count for every size of change: on a seeded six-defect fixture, eight single reviewers of either family each found all six, so a second one buys nothing, and neither does a second family. One pass, not a loop: a review's findings are fixed and then landed, the second pass only after a rebase changed the code, and a taste finding (a wording, a name, a shape the reviewer would have chosen) is noted in the final message, not re-reviewed. `routine queue review` enforces it: a branch that already has a review result is refused a second job without `--again`, which is for the rebased code only.
3. One `surface` job per touched UI surface when the repo has that kind (`bmesh routine queue surface --go -- <args>`, with the args the kind's own comment names: a repo's `surface.toml` says what it takes), or the operator's equivalent from your brief.
4. `bmesh spec verified --write` composes the spec's `verified` line from the results recorded for HEAD's tree. Commit it with the spec's status change.

Results are keyed by tree, so re-run what the final rebase changed.

## 7. Land

Before landing, if the pstack plugin is installed, run `pstack:blast-radius` on the change; otherwise list by hand every caller, config key, document and test the diff touches or invalidates (`git diff main...HEAD --stat`, then grep for each changed name) and check each one is updated or unaffected. Land per the repo's definition of done, on `main` only, never from an executor's branch. Land the branch your worktree is on (`git branch --show-current`), never the name in your brief: the runner may have renamed it since dispatch. The `spec set` that writes `done` and `merged` adds `--field branch=<that name>` when it differs from the spec's `branch`, so the ledger names the branch that was merged. Last of all, `bmesh done --commit <sha> --text "<one line>"` with the commit on `main`: it checks the spec on `origin/main` and refuses until the spec reads `done` with a `merged` commit that is on `main` and every `verified` entry has its result for that commit's tree. Nothing else counts as finished.

## 8. Ask and blocked

- `bmesh ask --text "<question>"` only for what no experiment, no reading and no reasonable assumption can settle; it reaches the operator, who may be away for hours. Before any ask, if the pstack plugin is installed, run `pstack:figure-it-out` on the question; otherwise write down what you tried, what the code and the specs say, and the assumption you would proceed on, and ask only if that assumption could make the work useless. Read your inbox first; the answer may be there.
- `bmesh blocked --text "<why>"` when the spec cannot be met as written; say what would unblock it.
- `bmesh progress --text "<one line>"` after a milestone, never more than one every ten minutes; it keeps you from being marked silent while you work quietly.
