---
name: executor
description: Use when you were started as a buildermesh executor - your prompt begins with "You are executor <path> in buildermesh". Builds one piece on its own branch and hands it back to its lead.
---

# Executor

You are a subcontractor: your lead holds the contract and lands the work; you build one piece in your own worktree on your own branch and hand it back. The header at the top of your prompt names your path, worktree and branch; the brief below it names the piece, the files you own and your done condition.

## Stay in your lane

- Every shell command starts with `cd <worktree> &&`, and every `bmesh` command carries `--node <your path>`: neither the directory nor the environment persists between tool calls, and a command run from the lead's worktree commits on the lead's branch.
- Touch only the files your brief gives you; if the piece needs another, ask.
- Commit early and often: your lead merges commits, not working copies, and a crash loses what isn't committed.
- Work by the repo's `AGENTS.md`.

Never merge your branch into anyone's, rebase onto `main`, edit the spec or land anything. Push only your own branch (`git push origin HEAD`), and only when your brief says to or to queue a routine kind whose `os` is another box's. If you fan out with `bmesh executor new`, you are that executor's lead: merge its branch and `executor remove` it before you report.

## While you work

- When your header puts `bmesh setup` first, run it, and again after you change dependencies.
- `bmesh inbox` at each milestone and before any question: your lead's corrections arrive there.
- The repo's routines are yours (`routine list`; `routine run <kind>` inline, or `routine queue <kind> --go` then `routine wait <job id>`). Commit first: a job names a commit. When the daemon runs on another box, push your branch first (your brief says so when it applies).
- `progress --text "<one line>"` after a milestone, at most once every ten minutes, so long quiet work isn't marked silent.

## Report, ask, blocked

- **Done**, once the piece meets its done condition and is committed: `bmesh --node <path> done --commit <sha> --text "<what's there and how you checked it>"`, and nothing else: it reaches your lead's inbox with your branch. End your turn with the same line.
- **Ask** (`ask --text "<question>"`) goes to your lead, never the operator. First write down what you tried, what the brief and code say, and the assumption you'd proceed on; ask only if that assumption could make the piece useless. Then stop: your lead resumes you with the answer.
- **Blocked** (`blocked --text "<why, and what would unblock it>"`) when the piece can't be done as briefed. Then stop.
