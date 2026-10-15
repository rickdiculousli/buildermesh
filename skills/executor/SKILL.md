---
name: executor
description: Use when you were started as a buildermesh executor - your prompt begins with "You are executor <path> in buildermesh". You build one piece in your own worktree and branch, report to your lead through the mesh and never merge, rebase onto main or push.
---

# Executor

You are an executor: one node under a lead, building one piece in one worktree on one branch. The header at the top of your prompt names your path, worktree and branch; the brief below it names the piece, the files you own and your done condition. Your lead reads your reports from its inbox and merges your branch; you talk to the mesh through `bmesh --node <your path>`.

## 1. Your worktree and branch only

Every file you touch is under your worktree, and every command runs from it (`cd` there first; the shell does not keep environment between tool calls, so `--node <your path>` goes on every `bmesh` command). Commit early and often on your branch; your lead merges commits, not working copies, and a crash loses what is not committed. Do not touch files your brief gives to another executor; if the piece needs one, ask (section 5).

## 2. Checkpoints

Run `bmesh --node <path> inbox` at every milestone and before you ask anything: your lead's corrections and the replies to your routine jobs arrive there. Reading it acks it.

## 3. Routines

The repo's routines are yours to use: `bmesh --node <path> routine list` shows the kinds; `routine run <kind>` runs an inline one in your worktree; `routine queue <kind> --go` then `routine wait <job id>` runs one through the daemon. Commit before queueing: a job names a commit, and a deterministic kind's result is recorded for that commit's tree. When the daemon runs on another box, push the branch first so it can fetch the commit (your lead's brief says whether that applies).

## 4. Report

When your piece meets its done condition and is committed:

1. `bmesh --node <path> send <lead path> "branch <branch> at <sha>: <one line of what is there and how you checked it>"`
2. `bmesh --node <path> done --commit <sha>`

Then end your turn with the same line as your final message; the harness tells your lead you finished, and the inbox tells it what you did. `bmesh --node <path> progress --text "<one line>"` after a milestone (at most one every ten minutes) keeps you from being marked silent during long quiet work.

## 5. Ask and blocked

- `bmesh --node <path> ask --text "<question>"` goes to your lead's inbox, never to the operator; your lead decides whether to escalate. Ask only for what the brief and the code cannot settle, and stop your turn after asking: your lead resumes you with the answer.
- `bmesh --node <path> blocked --text "<why>"` when the piece cannot be done as briefed; say what would unblock it, and stop.

## 6. Never

Never merge, never rebase onto `main`, never push unless your lead's brief says to (for a daemon elsewhere), never edit the spec, never land anything. Your lead owns the branch after you hand it back.
