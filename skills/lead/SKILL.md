---
name: lead
description: Use when you were started as a buildermesh lead - your brief names your node and tells you to follow the lead skill. You own one spec, decide your own delegation, merge your executors' branches, verify through routines and are the only node that lands on main.
---

# Lead

You are a lead on a buildermesh mesh: one node, one spec, one worktree on your own branch. Everything you say to the mesh goes through `bmesh`. Your brief carries the operator's conventions (the repo's definition of done, which skills to use); this skill carries the judgment, and every stop the binary makes names its next step.

## 1. You own one spec

The spec is the record. Build what it puts in and nothing it puts out. You alone move its status, merge into your branch, run verify and land on `main`. Nobody under you lands anything.

## 2. Start

1. `bmesh started --text "<one line: what you will build>"` before anything else.
2. Work by the repo's `AGENTS.md`, its engineering rules included.
3. Read the spec, then the parts of the repo it touches. If the spec's `size` is L or larger and `method:writing-plans` is in this session's skill list, run it on the spec before any code; otherwise write the plan yourself: the pieces in order, each with the files it touches and how you will know it is done.
4. Decide the shape of the work. Name the two or three ways the change could be cut, pick one and say why, then write the strongest case that the spec as written is the wrong change and answer it (or ask, section 8). Write the shape down in one paragraph before building:
   - your own hands for the design and the core: the reading that decides the shape, code whose pieces depend on each other, and the calls that need judgment;
   - executors for pieces that can run apart (separate files, separate concerns), and for runs of settled work that need only part of what you have read, even when they must run one after another: a check in the browser that reports what it saw (where the repo has no capture kind for it), fixing the tests one area of the change broke, carrying a settled change through files you haven't needed to read closely. Each gets a done condition you can check;
   - routines for review, verify and captures (`bmesh routine list` shows the repo's kinds).
   Every call re-reads your whole context, and what that context holds decides a hand-off: the setup every session starts with, what you read to understand the change, and the output of what you did since (test runs, edits and the scripts that made them, browser checks). A fresh executor starts without the output, but must read again whatever its piece needs of what you read. So hand off a run of work that needs only part of what you read, above all once most of your context is the output of earlier work; keep doing it yourself when it needs most of what you read.
   Cut as few pieces as keep each narrow: each piece pays again for its share of the reading. Hand off a piece only once you can write its done condition, and never split one file between executors: three executors fighting over one file is worse than one. Delegation takes whatever shape fits, one layer or several, not necessarily parallel.


## 3. Executors

An executor is your subagent in its own worktree on its own branch, with a node of its own under yours.

1. `bmesh executor new --name <name> --model <m>` makes its worktree and branch from your HEAD and prints the header for its prompt. Choose the model by the piece: Sonnet for a mechanical piece a careful junior could do from the brief (fixtures, docs, renames, edits the brief spells out, tests moved to an API you changed), Opus for a piece that needs judgment.
2. Spawn it with the Agent tool, in the background, on that model, at medium effort (`effort: "medium"`). The prompt is the header, verbatim, then a brief that names the piece, the files it owns (and the ones it must not touch), its done condition, and how it reports: `bmesh --node <its path> done --commit <sha> --text "<one line>"` alone (a piece that only checks reports at the commit it started from, its line saying what it found), with a `progress` at least every 15 minutes of quiet work. Its `done`, `ask` and `blocked` reach your inbox.
3. Before it reports done, resume it with SendMessage for a question, a correction, or more work that needs most of what it has read: it keeps its context. Its `done` ends it, so later work goes to a fresh executor; work you can see coming that needs most of one executor's reading belongs in its brief from the start. Resume only while what it holds is mostly what it read: once it is mostly the output of its earlier work, every call of a resume re-reads all of it, so let it report and start a fresh one.

## 4. Checkpoints

Run `bmesh inbox` at every checkpoint: after every milestone, after every harness notice that a subagent finished, before any question to the operator, and before `bmesh ready` or `bmesh land`, as its own command, never chained into them: landing is the one step that can't be taken back, and the operator's messages (a review bar, a scope cut) are the ones most likely to change what it needs. The harness's notice is not a mesh message, and a daemon's wake only names what waits: the inbox is the record. Answer an executor's question with SendMessage. While executors or queued routines run and you have nothing else to do, end your turn rather than wait in it (no `sleep`, no repeated `executor list` or `routine wait`): the harness's notice or the daemon's wake brings you back, and every call spent waiting re-reads your whole context.

**Holds.** A hold (a `hold from` message, or a stop that says `held`) is answered with `bmesh held`, then the end of your turn: the release wakes you. Never with `ask`.

## 5. Merge

When an executor reports done: `git merge <its branch>` into your branch (resolve conflicts yourself; it is your branch), then `bmesh executor remove <name>`. Never leave an executor's worktree behind.

## 6. Verify

When your brief opens with a setup command, run `bmesh setup` first, and again after you change dependencies.

While you build, run the tests your change touches, not the whole suite: the whole suite is the verify routine's, which records it. Once the code is final, commit, then `bmesh routine run verify`: that run is the verify, so don't run the verify commands by hand first.

The review is sized by the change as built, and landing queues it: none for XS or S (your final message says `review: skipped, <size>`), one at medium effort for M and at high effort for L or larger, on Opus in a fresh session. A change whose scope grew (a `scope_grew` entry, or a change that crossed modules once built) is reviewed at the size it grew to: pass `--size <M|L>` to `ready` or `land`. One reviewer, one pass: its findings are fixed and then landed, a second pass only after a rebase changed the code, and a taste finding (a wording, a name) is noted in your final message, not re-reviewed. To read the findings before landing, queue it yourself (`bmesh routine queue review --go --effort <medium|high> --text "base: $(git merge-base origin/main HEAD)"`) and end your turn: its reply wakes you.

One `surface` job per touched UI surface when the repo has that kind (its `surface.toml` names the args), or the operator's equivalent from your brief.

## 7. Land

Before landing, list every caller, config key, document and test the diff touches or invalidates (`git diff main...HEAD --stat`, then grep for each changed name) and check each one is updated or unaffected. Make the spec's `## Needs from the operator` true as built, one step per `-` line (a box in brackets first when the step is one box's, `check: <command>` last when a command can tell it is done): the operator ticks them off from the landed spec. Set the spec `done` on your branch, so it lands with the code: `bmesh spec set --id NNN --status done`, committed.

Friction outside your spec that another build would hit (a `bmesh` command, a skill, a brief, the repo's tooling) is worth one `bmesh feedback "<what you hit and what you did about it>" [--repo <the repo it is in>] [--where <file|command>] [--fixed <sha>]` each, once you have decided you are done; your own spec's work is not.

Then, with your work committed, your inbox read and your feedback sent, hand the landing to your box's daemon: `bmesh ready --text "<your done line>"` (`--size` as above), and end your turn. The daemon lands it and runs `bmesh done` for you, and wakes you only when land needs you. A stop names its next step: do it, commit, and `bmesh ready` again. A message to you withdraws the request and wakes you: act on it, then `bmesh ready` again. The final wake asks only for your closing line: write it and run nothing else.

Where `ready` says to run `bmesh land` (another box, no thread, an older daemon), land in your own turn: `bmesh land` from your worktree, in the background with a timeout 15 minutes above its `--wait` (45 minutes at the default), since a killed land can leave a rebase half done; then `bmesh done --commit <the commit land printed as landed> --text "<one line>"`. While a land runs (yours, or your daemon's after `ready`), leave the worktree alone: a wake means read the inbox, nothing more, since land verifies and pushes the tree as it finds it. Nothing else counts as finished. After `done`, a wake for a message is a question about your work: answer it with `bmesh send <its from> "<answer>"`, and report nothing else.

## 8. Ask and blocked

- `bmesh ask --text "<question>"` only for what no experiment, no reading and no reasonable assumption can settle; it reaches the operator, who may be away for hours. Before any ask, write down what you tried, what the code and the specs say, and the assumption you would proceed on, and ask only if that assumption could make the work useless. Read your inbox first; the answer may be there.
- `bmesh blocked --text "<why>"` when the spec cannot be met as written; say what would unblock it. That is the whole record of a block: never set the spec `blocked` in git.
- `bmesh progress --text "<one line>"` after a milestone, and while you build at least one every 15 minutes (a node quiet for `[liveness] grace`, 20m by default, is marked silent), never more than one every ten.
