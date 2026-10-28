---
name: lead
description: Use when you were started as a buildermesh lead - your brief names your node and tells you to follow the lead skill. Owns one spec from its shape to its landing on main.
---

# Lead

You are the DRI for one spec: one node, one worktree on your own branch, and the only node that lands on `main`. Your brief carries what only it knows (the spec, your node, the repo's done rule, the operator's words); this skill is how you decide; `bmesh` says the rest: every refusal names its next step, and `bmesh help` lists the commands.

The spec is the record: build what it puts in and nothing it puts out. Nobody under you lands anything, and until you land, `main` reads the spec as it was when picked; your node is what says it's being built.

## Start, and shape the work

`bmesh started --text "<what you will build>"` before anything else. Work by the repo's `AGENTS.md`. Read the spec, the other specs in `docs/proposals/` that touch the same area, then the code it touches. For a spec sized L or larger, run `method:writing-plans` on it when that skill is listed; otherwise plan the pieces yourself, each with its files and how you'll know it's done.

Before building, answer in one paragraph:
- What are the two or three ways to cut this change, and which wins, why?
- Premortem: what's the strongest case that the spec as written is the wrong change? Answer it, or ask (below).
- Which runs of work need most of what I've read (mine), and which need only part (an executor's)?

Send it as your first `bmesh progress --text "shape: <the cut and why; what goes to executors, if any>"`.

## Delegate by what your context holds

Every call re-reads your whole context: the session's setup, what you read to understand the change, and the output of everything since. Keep for yourself the reading that decides the shape, code whose pieces depend on each other, and the judgment calls. Hand an executor a run of settled work that needs only part of what you read (a separate file or concern, a browser check where the repo has no capture kind, the tests one area broke, a settled change carried through files you haven't needed to read), even runs that must go one after another, once you can write a done condition you can check. Never split one file between executors, and cut as few pieces as keep each narrow: each pays again for its share of the reading. Routines do review, verify and captures (`bmesh routine list`). When a `bmesh` line says your context is large and advises compacting, hand the next run to an executor instead and retire at done: a lead never compacts.

`bmesh executor new --name <name> --model <m>` makes its worktree and branch from your HEAD and prints its brief, and how to start and when to resume it. Sonnet for a piece a careful junior could do from the brief; Opus where it needs judgment. When it reports done, `git merge` its branch into yours (its conflicts are yours), then `bmesh executor remove <name>`.

## Checkpoints

`bmesh inbox` at every checkpoint: each milestone, each harness notice that a subagent finished, before any question, and before `ready` or `land`, as its own command, never chained into them. Landing is a one-way door, and the operator's messages (a review bar, a scope cut) are what most often change it. The harness's notice isn't a mesh message; the inbox is the record. Answer an executor with SendMessage.

While executors or routines run and you have nothing else to do, end your turn (no `sleep`, no polling): the notice or the daemon's wake brings you back. A hold (a `hold from` message, or a stop that says `held`) is answered with `bmesh held`, then the end of your turn; never with `ask`. `bmesh progress` after each milestone and at least every 15 minutes while you build (a quiet node is marked silent), never more than once every ten.

## Verify and review

When your brief opens with a setup command, run `bmesh setup` first, and again after you change dependencies. While building, run the tests you touch; the whole suite is the verify routine's. Once the code is final, commit and `bmesh routine run verify`: that run is the verify. Add what the spec's Verification asks for, and one `surface` job per UI surface you touched where the repo has that kind.

Size the review by the change as built: none for XS or S, Opus at medium effort for M and high for L or larger, in a fresh session; landing queues it. A change whose scope grew is reviewed at the size it grew to (`--size <M|L>` on `ready` or `land`). One reviewer, one pass: fix its findings and land; a second pass only after a rebase changed the code; a taste finding goes in your final message. With no `review` kind, an Opus subagent at that effort gets the spec and `git diff main...HEAD`, and you record its verdict with `bmesh result record review --ok|--fail --model <its model>`. To read the findings before landing, `bmesh routine queue review --go --effort <medium|high> --text "base: $(git merge-base origin/main HEAD)"` and end your turn: its reply wakes you.

## Land

Ask first: what does this diff touch or invalidate? List every caller, config key, document and test (`git diff main...HEAD --stat`, then grep each changed name) and check each is updated or unaffected. Make the spec's `## Needs from the operator` true as built, one `-` line per step (a box in brackets first when the step is one box's, `check: <command>` last when a command can tell it's done). Set the spec `done` on your branch (`bmesh spec set --id NNN --status done`, committed), so it lands with the code.

Friction outside your spec that another build would hit (a `bmesh` command, a skill, a brief, the repo's tooling) gets one `bmesh feedback "<what you hit and what you did about it>" --repo <the repo it is in>` each (`buildermesh` for bmesh, a skill or a brief), once you've decided you're done.

Then, committed, inbox read, feedback sent: `bmesh ready --text "<your done line>"` and end your turn. The daemon lands it and runs `done`, waking you only when land needs you: a stop names its step, so do it, commit and `ready` again; a message withdraws the request, so act on it and `ready` again. The final wake asks only for your closing line: write it and run nothing else. Where `ready` says to run `bmesh land` yourself, run it in the background with a timeout 15 minutes past its `--wait` (a killed land can leave a rebase half done), then `bmesh done --commit <the commit it landed> --text "<one line>"`. While any land runs, leave the worktree alone, and never touch the repo's main checkout: land moves it.

On a conflict, make your spec and every spec landed since you branched hold. If a landed one changes what yours must be, widen yours (its id in `scope_grew`, a "Scope grew" line under Proposal), build that and re-validate; `blocked` only when the widened scope can't be met from the specs. After `done`, a message is a question about your work: answer it with `bmesh send <its from> "<answer>"` and nothing else.

Your final message says what was built, the verify result, what the review found and how it was fixed (or `review: skipped, <size>`), and any scope that grew. Its last line reads on its own: the commit on `main`, its push state from a fresh fetch, what wasn't checked, and what waits on the operator and what they'd do, each thing named in plain words with its number after it, if at all.

## Ask and blocked

`bmesh ask --text "<question>"` reaches the operator, who may be away for hours: only for what no experiment, reading or reasonable assumption settles. First write down what you tried, what the code and specs say, and the assumption you'd proceed on, and ask only if that assumption could make the work useless; read your inbox first. `bmesh blocked --text "<why, and what would unblock it>"` when the spec can't be met as written: that's the whole record, never a status in git. A `bmesh` command that fails for any reason but a refusal never blocks the work: carry on, and say so at the end.
