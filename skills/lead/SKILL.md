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
2. Read the spec, then the parts of the repo it touches.
3. Decide the shape of the work, and write it down in one paragraph before building:
   - your own hands for the core, or anything whose pieces depend on each other;
   - executors for pieces that can run apart: separate files, separate concerns, each with a done condition you can check;
   - routines for review, verify and captures (`bmesh routine list` shows the repo's kinds).
   Fan out only when a piece can really run apart. One executor doing the whole thing is a worse you; three executors fighting over one file is worse than one. Fan-out is delegation in whatever shape fits, one layer or several, not necessarily parallel.

## 3. Executors

An executor is your subagent in its own worktree on its own branch, with a node of its own under yours.

1. `bmesh executor new --name <name> [--model <m>]` makes the worktree and branch (`<your branch>-<name>`) from your HEAD, registers the node and prints a JSON line and the executor header.
2. Spawn it with the Agent tool, in the background, model of your choice. The prompt is the header, pasted verbatim at the top, then a brief that names: the piece, the files it owns (and the ones it must not touch), its done condition, and how to report: `bmesh --node <its path> send <your path> "branch <b> at <sha>: <one line>"` then `bmesh --node <its path> done --commit <sha>`.
3. Resume it with SendMessage when you have a question or a correction; it keeps its context. Start a fresh one for a new piece.
4. `bmesh executor list` shows your tree: state, branch, head, unread.

## 4. Checkpoints

Run `bmesh inbox` at every checkpoint: after every milestone of your own, after every harness notice that a subagent finished, and before any question to the operator. The harness's notice is not a mesh message; the inbox carries the executor's report (its branch and commit), replies from routines, and messages from the operator. Reading the inbox acks it, so the daemon will not wake you for the same message. Answer an executor's question with SendMessage, not through the mesh.

## 5. Merge

When an executor reports done: `git merge <its branch>` into your branch in your worktree (resolve conflicts yourself; it is your branch), then `bmesh executor remove <name>`, which refuses while the branch is unmerged (`--force` only for a piece you are discarding). Never leave an executor's worktree behind.

## 6. Verify

On committed work, in this order, once the code is final:

1. `bmesh routine run verify` (an inline kind runs in your worktree and records the result for HEAD's tree). In a repo without a `verify` kind, run its verify commands yourself and record the outcome: `bmesh result record verify --ok` or `--fail`.
2. One review by a model of a different family than yours: `bmesh routine queue review --model <m> --go --text "base: $(git merge-base origin/main HEAD)"`, then `bmesh routine wait <job id>`. Fix what it finds; a second pass only when the fixes were large.
3. One `surface` job per touched UI surface when the repo has that kind (`bmesh routine queue surface --go -- <url> <file>`), or the operator's equivalent from your brief.
4. `bmesh spec verified --write` composes the spec's `verified` line from the results recorded for HEAD's tree. Commit it with the spec's status change.

Results are keyed by tree, so re-run what the final rebase changed.

## 7. Land

Land per the repo's definition of done, on `main` only, never from an executor's branch. Last of all, `bmesh done --commit <sha> --text "<one line>"` with the commit on `main`: it checks the spec on `origin/main` and refuses until the spec reads done with that commit merged and every `verified` entry has its result. Nothing else counts as finished.

## 8. Ask and blocked

- `bmesh ask --text "<question>"` only for what no experiment, no reading and no reasonable assumption can settle; it reaches the operator, who may be away for hours. Read your inbox first; the answer may be there.
- `bmesh blocked --text "<why>"` when the spec cannot be met as written; say what would unblock it.
- `bmesh progress --text "<one line>"` after a milestone, never more than one every ten minutes; it keeps you from being marked silent while you work quietly.
