---
name: work
description: >-
  Handles anything the operator brings about a repo in the bmesh registry, in any words: a problem, a change, a question about its code, an idea, a reply to a round's pitch ("1 and 3"), what's building or waiting, a hold, stop or release, or that they're testing.
---

# Work

Triage, then take the lowest rung that holds. Start from the operator's problem, not a routing word; read the code before you decide; never ask them which path to take. Size by reach: XS one file, S one module, M crosses modules, L changes an interface others depend on.

`bmesh` does the checking (the row, holds, a red `main`, the lead cap) and refuses with the next step, and a command that starts something ends with `next:` lines saying what follows. Act on them rather than redo them, and pass `--force` only when the operator says so. `bmesh help` lists every command.

## Before deciding

- **Which repo?** The one named, else this one. `bmesh repos show <repo> --json` gives its path, `host` and verify.
- **Is it done or in flight?** `bmesh mesh-status`; for an idea or a pick, `bmesh spec list --repo <repo>` too. A spec being built reads its old status until it lands: only its lead's node says it's building. With no hub nothing is in flight, and rungs 1 to 4 still work.
- **What does the code say?**
- **What has the operator said about their state** (testing, away, low on budget)? It holds for the session; never make them say it twice. While they QA or say hold off, what they test is frozen: a "fix X" joins a list, and nothing is edited, restarted or started until their go ([references/qa.md](references/qa.md)).

## The ladder

Take the first rung that holds, and say so in the first line of your first message on the request (after the QA hold's line, while one holds): what you'll do, and for a change its size and why (`Making it here, S: only internal/land changes`). A rung that links a file has its steps there: read it when you take the rung.

1. **Answering**: a question, or they're testing. Answer from the code, change nothing, and end on the answer.
2. **Already done** or **Already being built**: it's on `main`, or a lead has it. Name the commit, or the lead's path and its state, and stop. Their words for that lead go to it: [references/in-flight.md](references/in-flight.md).
3. **Making it here**: XS, S or M, a reported problem included. In a worktree of your own, never the repo's checkout: make it, verify, review, push to `main`. [references/make-it-here.md](references/make-it-here.md).
4. **Plan for your ok**: L, a call of taste, a release or deploy they didn't ask for, a spend past their budget. Five lines: what changes, where, how it's checked, its size, what you won't do. End on the one question their ok answers; if a grilling skill is installed, offer it in a line. On their ok, make it by rung 3 with the review at high effort, or by rung 5, with the plan as the spec, when it's long or splits.
5. **Starting leads**: specs they pick, pieces that run apart, or work too long for this session. [references/leads.md](references/leads.md).
6. **Starting a round**: "what would you improve in X", or one idea of theirs to spec at length. [references/round.md](references/round.md).

Only the operator, here, starts a lead or a round: a subagent you start never runs `ideate`, `build`, `lead start` or anything else that starts a thread.

**Never cut:** anything the operator is running; verify; a review sized to the risk; the push state from a fresh fetch; their say over releases, deploys and big spends.

## Not a rung

- Their answer to a round's pitch ("1 and 3", "2 no, <why>", "4 later"): [references/round.md](references/round.md).
- What's building or waiting on them, and their words for a lead in flight ("hold X", "stop X", "wipe X", "release X" for a held lead): [references/in-flight.md](references/in-flight.md).
- "ship X", or "release X" for a repo: [references/release.md](references/release.md).
- Work that will outlive this session keeps its plan in a state file outside git: name its path in replies and delete it when the stream ends, a lesson worth keeping made a rule in `AGENTS.md` first. Run `bmesh context` right before executing a finished design.

End with one line: what changed or started (the commit on `main`, or the lead or round and its host), its push state from a fresh fetch, what you didn't check, and what waits on the operator, each named in plain words. It reads on its own; only a plan's question comes after it.
