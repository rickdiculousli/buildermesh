---
name: work
description: >-
  Handles anything the operator brings about a repo in the bmesh registry, in any words: a problem, a change, a question or an idea, a reply to a round's pitch ("1 and 3"), what's building or waiting, a hold, stop or release, or that they're testing.
---

# Work

Triage, then take the lowest rung that holds. Start from the operator's problem, not a routing word; read the code before you decide; never ask them which path to take. Size by reach: XS one file, S one module, M crosses modules, L changes an interface others depend on.

`bmesh` does the checking (the row, holds, a red `main`, the lead cap) and refuses with the next step, and a command that starts something ends with `next:` lines saying what follows. Act on them rather than redo them, and pass `--force` only when the operator says so. `bmesh help` lists every command.

## Answer these before deciding

- **Which repo?** The one named, else this one. `bmesh repos show <repo> --json` gives its path, `host` and verify.
- **Is it done or in flight?** `bmesh mesh-status`; for an idea or a pick, `bmesh spec list --repo <repo>` too. A spec being built reads its old status until it lands: only its lead's node says it's building. With no hub nothing is in flight, and rungs 1 to 4 still work.
- **What does the code say?**
- **What has the operator said about their state** (testing, away, low on budget)? It holds for the session; never make them say it twice.

## Name the rung

The first message you write on a request opens with its rung's line, given below, and for a change its size with one clause of evidence: once you know, or as the first line of your report when you act before writing. Only the QA hold's line (`Holding for your QA: ...`) goes before it. Once per request, and again only when their answer starts a rung (an ok on a plan, a pick); words about a lead in flight or a release name none. `bmesh audit` sets the rung you named beside the one your actions imply.

## The ladder

1. **A question, or they're testing.** `Answering, no change`. Answer from the code, change nothing, and end on the answer (ending on a question is a plan's). While they QA or say hold, what they test is under a code freeze: no edit, restart, rebuild or deploy of it, nothing started. Keep a numbered list, a line each: `3 noted: <cause>; fix: <change> (XS)`. A "fix X" joins the list as picked unless they say now. Their go ("fix 1 and 3") makes the picked items as one change by rung 3: a commit each, one verify, one review, one push.
2. **Already done or being built.** `Already done: <the commit>` or `Already being built: <the lead's path>`, with its state, and stop. Their words for that lead go to it: `bmesh --node operator send <path> "<their words>"`.
3. **Make it here: XS, S or M.** `Making it here, <size>: <evidence>` (`Making it here, S: only internal/land changes`). A reported problem is a change to make: no spec, lead or ledger commit.
   - **Where:** a worktree of your own on a branch from `origin/main` (the harness's, or under `~/.bmesh/worktrees/<repo>/`), never the repo's checkout, where an unpushed commit holds every lead's land. A repo with no checkout here is made on its row's `host`.
   - **Make it:** the smallest change that does what was asked, by the repo's `AGENTS.md`, with the docs it keeps exact.
   - **Verify:** commit, then `bmesh routine run verify` until `verify ok` (a repo with no `verify` kind: its verify commands).
   - **Review:** your diff by the `ponytail-review` skill, at your own effort; fix its Must fix and Should fix findings once and verify again. An M gets a second review: Opus at medium effort in a fresh subagent, given the operator's words, the diff and the verify line, told to start nothing.
   - **Land:** rebase onto a fresh `origin/main` (verify again if it brought commits) and `git push origin HEAD:main`, again whenever `main` moved. Bring the repo's checkout level when it's on `main` and clean, then remove your worktree and branch. Never move commits on `main` that aren't yours. Release only when they want it running now (Releases, below).
4. **It needs their eyes first:** L, a call of taste, a release or deploy they didn't ask for, a spend past their budget. `Plan for your ok, <size>: <evidence>` (`Plan for your ok, L: changes the brief every lead reads`), then five lines: what changes, where, how it's checked, its size, what you won't do. End on the one question their ok answers; if a grilling skill is installed, offer it in a line. On their ok, make it by rung 3 with the review at high effort, or by rung 5 with the plan as the spec. A change that grows to L while you make it stops here.
5. **Leads**, for specs they pick, pieces that run apart, or work too long for this session. Open with the line `build`'s last `next:` gives. `bmesh build <repo> <NNN>...` on the row's `host` (elsewhere over `ssh -o BatchMode=yes <host> "bash -lc '<command>'"`; an unreachable host is said, and nothing starts). Their words beyond the pick go in `--context -` through a quoted heredoc. A model named for one spec is that spec's own `build --model <m> --family <F>`, since `--model` on a batch overrides every judge. Specs that edit the same files go one after another in one family. Report each `warning:` as printed. A spec you write for a lead lands first (`bmesh spec next-id`, `bmesh spec template`, `status: proposed`, `Propose NNN: <title>`), written as what can be checked, and you name its file before the lead starts, while it's cheap to change. Then do what `build`'s `next:` lines say: watch, relay asks in their words, and drain the family's feedback when `watch-done` names it.
6. **What to do next** ("what would you improve in X"). `Starting a round on <repo>`, then `bmesh ideate <repo>`, watched as its `next:` says; give the pitch as it arrives. One idea of theirs to spec at length: `bmesh ideate <repo> --idea --context -`. Map their answer to the newest pitch they saw: "1 and 3" builds those (rung 5; an XS or S line with no spec by rung 3), "2 no, <why>" is `bmesh spec set --repo <repo> --id NNN --status rejected --field reason="<why>" --land`, "4 later" the same with `--status later`. Decline a spec a live lead is building only once they've said to stop it. A number that could mean two things: ask in one line.

Only the operator, here, starts a lead or a round: a subagent you start never runs `ideate`, `build`, `lead start` or anything else that starts a thread. A change you hand a subagent is its to verify, review and land; it reports one line (the commit and its push state), and you read that, not its diff.

**Never cut:** anything the operator is running; verify; a review sized to the risk; the push state from a fresh fetch; their say over releases, deploys and big spends.

## Leads in flight, status, releases

- **Their words for a lead**, as `--node operator`: "hold X" is `bmesh hold` (`--gate now` to stop it at once; "land none until I say" is `build --hold land`), "release X" for a held lead is `unhold`, "stop X" is `node stop` (never a message: mail wakes a thread), and "wipe X" a stop, then `bmesh node forget <path> --wipe`, `--dry-run` first.
- **"What's building", "what's waiting on me":** `bmesh mesh-status`, asks first. A box's hold (`<box>: <repo> waits for your approval`) is the operator's: name it and the `bmesh hosts approve <id>` they run in a terminal on that box; never run it yourself. Then `bmesh spec steps`, then any `bmesh update --notice` lines word for word.
- **Releases:** "ship X", or "release X" when X is a repo with no held lead, is `bmesh release <repo>` now, after any step the repo's `AGENTS.md` names before a release. A batch of leads ends, once done and drained, with a release of each repo it landed in that is released by tag (`git tag --list 'v*'`). A `held` or `main is red` refusal is reported, never forced.

## Long sessions

A stream's plan lives in a state file outside git that outlives the session: name its path in replies and delete it when the stream ends, a lesson worth keeping made a rule in `AGENTS.md` first. When your context grows, a `bmesh` command's last line says so and what to do; run `bmesh context` right before executing a finished design.

End with one line: what changed or started (the commit on `main`, or the lead or round and its host), its push state from a fresh fetch, what you didn't check, and what waits on the operator, each named in plain words. It reads on its own; only a plan's question comes after it.
