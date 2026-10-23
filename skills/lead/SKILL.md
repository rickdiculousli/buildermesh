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
2. Work by the repo's `AGENTS.md`, its engineering rules included.
3. Read the spec, then the parts of the repo it touches. If the spec's `size` is L or larger and `method:writing-plans` is in this session's skill list, run it on the spec before any code; otherwise write the plan yourself: the pieces in order, each with the files it touches and how you will know it is done.
4. Decide the shape of the work. Name the two or three ways the change could be cut, pick one and say why, then write the strongest case that the spec as written is the wrong change and answer it (or ask, section 8). Write the shape down in one paragraph before building:
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

Run `bmesh inbox` at every checkpoint: after every milestone of your own, after every harness notice that a subagent finished, before any question to the operator, and before `bmesh land`, as its own command, never chained into the land: landing is the one step that can't be taken back, and the operator's messages (a review bar, a scope cut) are the ones most likely to change what it needs. The harness's notice is not a mesh message; the inbox carries the executor's report (its branch and commit), its questions, and messages from the operator. A wake from the daemon names what waits (`bmesh: 2 unread: done from <path> (branch <b> at <sha8>); reply to job <id> (review fail). ...`), so you know what you are coming back to; read the inbox all the same, since the wake's list is a hint and the inbox is the record. Reading the inbox acks it, so the daemon will not wake you for the same message (a wake that crosses a notice is one spare turn: read the inbox and carry on). A routine reply shows here as one line; `bmesh routine wait <job id>` prints it whole, before or after the inbox read it. Answer an executor's question with SendMessage, not through the mesh.

**Holds.** A coordinator stops you at a gate with `bmesh hold`, not with words: a message `hold from <who> (land|now): <why>` at any checkpoint. At `now`, every inbox read prints `held now by <who> ... ago: <why>` first: stop at that checkpoint. At `land`, keep building; `bmesh land` stops with `held before land by <who> <age> ago: <why>` and pushes nothing. Either way run `bmesh held` (it publishes your state `held`, which the daemon never calls silent) and end your turn, doing nothing else. The release (`released by <who>: <words>`) wakes you: read the inbox, carry on with your next `progress` or `started` (which makes you `working` again), and land when you get there. A hold is never answered with `ask`; `bmesh held` refused (`not held`) means the release came first: carry on.

## 5. Merge

When an executor reports done: `git merge <its branch>` into your branch in your worktree (resolve conflicts yourself; it is your branch), then `bmesh executor remove <name>`, which refuses while the branch is unmerged (`--force` only for a piece you are discarding). Never leave an executor's worktree behind.

## 6. Verify

When the repo names a setup command (`.bmesh/config.toml`'s `[setup]`; your brief opens with it), run `bmesh setup` first, before anything else, and again after you change dependencies; read its output when it fails. A verify runs it too when your installs are stale, and refuses, recording nothing, when it fails or leaves files the repo doesn't ignore.

On committed work, in this order, once the code is final:

1. Commit, then `bmesh routine run verify`: that run is the verify, so don't run the verify commands by hand first (they would run twice, and a hand run records nothing). An inline kind runs in your worktree, records the result for HEAD's tree and ends with its verdict, `verify ok|fail t:<tree8> (recorded)`; when only commits outside the repo's code came since a verified one, nothing runs and it ends `verify ok t:<tree8> (carried from t:<old8>: nothing under code changed)`, which is the verify too. In a repo without a `verify` kind, run its verify commands yourself and record the outcome: `bmesh result record verify --ok` or `--fail`.
2. Size the review by the spec's `size`. An XS or S change gets none: skip this step, the `verified` line carries verify alone, and your final message says `review: skipped, <size>`. A change is sized as built: one whose scope grew (a `scope_grew` entry, or a change that crossed modules once built) is reviewed at the size it grew to, so an S that grew to M is reviewed as an M and one that grew to L as an L. An M gets one review at medium effort, an L or larger one at high effort, on Opus in a fresh session (the kind's first model; a pooled session never saw your change, so an Opus lead is reviewed by Opus too): `bmesh routine queue review --go --effort <medium|high> --text "base: $(git merge-base origin/main HEAD)"`, then `bmesh routine wait <job id>`. One reviewer is the sized count for every size of change: on a seeded six-defect fixture, eight single reviewers of either family each found all six, so a second one buys nothing, and neither does a second family. One pass, not a loop: a review's findings are fixed and then landed, the second pass only after a rebase changed the code, and a taste finding (a wording, a name, a shape the reviewer would have chosen) is noted in the final message, not re-reviewed. `routine queue review` enforces it: a branch that already has a review result is refused a second job without `--again`, which is for the rebased code only.
3. One `surface` job per touched UI surface when the repo has that kind (`bmesh routine queue surface --go -- <args>`, with the args the kind's own comment names: a repo's `surface.toml` says what it takes), or the operator's equivalent from your brief.
4. A kind whose `os` line names another OS than this box's runs only on a box of that OS: push before queueing it (`git push origin HEAD`; `queue` refuses an unpushed commit and prints `queued <id> (runs on <os>)`). `bmesh land` needs its result at the rebased tree, so a landing whose rebase changed the code waits for that box to be awake, and the rebased commit must be pushed (`git push --force-with-lease origin HEAD`) before the kind is queued again.
5. The spec's `verified` line is composed from the results recorded for the landed tree; `bmesh land` (section 7) writes it.

Results are keyed by tree, so re-run what the final rebase changed; `bmesh land` runs the inline kinds again on the rebased tree itself.

The verify commands are the repo's registry row's, and `bmesh scaffold` writes them into the verify kind and `AGENTS.md` (and the row's setup into `.bmesh/config.toml`'s `[setup]`); never edit those by hand. A spec that changes the verify commands changes the row: `bmesh repos propose` with the new `verify`, named in the spec's "Needs from the operator", since only the admin publishes the registry; once it is published, `bmesh scaffold <your worktree>` writes the files on your branch and you commit them. `bmesh land` refuses a branch whose scaffolded files differ from what the row writes.

## 7. Land

Before landing, list every caller, config key, document and test the diff touches or invalidates (`git diff main...HEAD --stat`, then grep for each changed name) and check each one is updated or unaffected. Make the spec's `## Needs from the operator` true as built, written as a list, one step per `-` line (a box in brackets first when the step is one box's, `check: <command>` last when a command can tell it is done): `bmesh land` copies it into the spec's `operator_steps`, which stay on `mesh-status` until the operator ticks them. Land per the repo's definition of done, on `main` only, never from an executor's branch, with one command from your worktree, its work committed: `bmesh land`. It takes the lease `land.<repo>` (waiting, and saying who holds it, while another lead lands), fetches, refuses while the main checkout's `main` has commits `origin` doesn't (someone's unpushed work: wait, never push it), rebases onto `origin/main`, checks the scaffolded files the branch changed against the repo's row, runs the inline kinds (verify) on the rebased tree and requires the others' results there, writes the `verified` line, sets the spec `done` with `merged` and the branch your worktree is on (`git branch --show-current`, never the name in your brief: the runner may have renamed it), commits that, pushes to `main` and fast-forwards the main checkout; when `main` moved before the push it starts again from the fetch, once. It prints a line per step and `landed <sha8>`. When it stops, nothing is pushed and it says why:

- held (`held before land by ...`): run `bmesh held` and end your turn; the release wakes you, then `bmesh land` again (see Holds in section 4);
- unread messages from the operator: run `bmesh inbox`, act on them, then `bmesh land` again (other unread mail doesn't stop it: land names it on one `unread:` line and goes on);
- a conflict land's rules resolve: it prints `rebase: resolved <file> (<rules>)` and goes on. A failing verify right after that line is land's merge, and the branch is left rebased: fix it forward, commit, push the branch and queue the review again with `--again`, then `bmesh land` again;
- a conflict it can't resolve (it names the files that need you and those a rule resolves): the rebase is aborted. Rebase onto `origin/main` yourself and resolve it: read your spec and every spec that landed since you branched, rework the change so every intent holds (widen your spec when a landed one changes what yours must look like), commit, re-validate, and run `bmesh land` again. Land recovers a hand rebase: when HEAD is the commit your rebase made, it reads the branch's reflog for the head the rebase rewrote and requires what that head had, as if it had rebased itself, so a review carries when your resolution left the diff unchanged and is refused, to be queued `--again`, when it changed it. Its first line, `required: ...`, names each kind and where it came from; resolve inside the rebase (`git rebase --continue`), since a commit made on top of the rebase makes land read the branch as it stands;
- a review (or another non-inline kind) with no result on the rebased tree when the rebase changed your diff: queue it again with `--again` at the same `--effort`, wait for it, then `bmesh land` again; when the rebase left the diff unchanged, the review carries: it reviewed the job text's `base:` to its commit, and that diff's patch-id is HEAD's, so land puts it on the `verified` line as `review ok t:<landed tree8> m:<model> c:<reviewed tree8>` and goes on;
- scaffold drift (the diff it prints, then `the row's verify and setup are the source`): the branch changed the verify kind, `AGENTS.md`'s marked blocks or `.bmesh/config.toml`'s `[setup]` by hand. Restore them from `origin/main`, or, when the spec changes the verify or setup commands, `bmesh repos propose` the new `verify` or `setup`, `ask` the operator to publish it, then `bmesh scaffold <your worktree>`, commit and `bmesh land` again;
- a failing result: fix it, commit, `bmesh land` again;
- `main` moving twice: run it again.

Friction outside your spec that another build would hit (a `bmesh` command, a skill, a brief, the repo's tooling) is worth one `bmesh feedback "<what you hit and what you did about it>" [--repo <the repo it is in; buildermesh for the mesh itself>] [--where <file|command>] [--fixed <sha>]` each, once you have decided you are done; your own spec's work is not. Last of all, `bmesh done --commit <sha> --text "<one line>"`, `<sha>` the commit `bmesh land` printed as `landed` (`git rev-parse HEAD` in your worktree: the follow-up commit that records `done`, whose parent is `merged`); it prints `done <path> <sha8>`, and a second run with the same commit prints `already done at <sha8>`. It checks the spec on `origin/main` and refuses until the spec reads `done` with a `merged` commit that is on `main` and every `verified` entry has its result for that commit's tree. Nothing else counts as finished. After `done`, a wake for a message is a question about your work: answer it with `bmesh send <its from> "<answer>"`, and report nothing else.

## 8. Ask and blocked

- `bmesh ask --text "<question>"` only for what no experiment, no reading and no reasonable assumption can settle; it reaches the operator, who may be away for hours. Before any ask, write down what you tried, what the code and the specs say, and the assumption you would proceed on, and ask only if that assumption could make the work useless. Read your inbox first; the answer may be there.
- `bmesh blocked --text "<why>"` when the spec cannot be met as written; say what would unblock it.
- `bmesh progress --text "<one line>"` after a milestone, and while you build at least one every 15 minutes (a node quiet for `[liveness] grace`, 20m by default, is marked silent), never more than one every ten.
