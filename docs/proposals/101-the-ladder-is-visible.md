---
id: 101
title: A reply names the rung it took and why, a lead's shape reaches the mesh, and bmesh audit shows what a session read and did beside what it said
kind: refinement
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity
The work skill's ladder decides how much a request gets (answer, make it here, plan for an ok, leads), and the skills now load procedure only where a step needs it (100, 099). The operator can see neither in a live session (operator, 2026-10-24: "the ladder walking rules are opaque and I can't verify that you are exercising those qualities"):
- **The rung is silent.** The skill asks a reply to name only a QA hold on its first line (`skills/work/SKILL.md:9`). Which rung a request took, and the size behind it, stays in the session.
- **A lead's shape stays in its thread.** The lead skill has it write the shape in one paragraph (`skills/lead/SKILL.md:19`), but `mesh-status` and `watch` show only `started`, `progress` and `ready` lines.
- **Measurement is lab-only.** `scripts/skilltest` scores rung choice on scripted cases; no tool reads a real session. The 2026-10-18 to 2026-10-24 studies did, with one-off scripts outside the repo (what a lead read first, its token share per phase), so each answer cost a study.
- **A claim is not proof.** A line a session writes about itself can be wrong; only the transcript says what it read and did.

## Proposal
In:
- **The work skill: name the rung.** A reply that decides a request opens with the rung in plain words and the size with one clause of evidence: `Answering, no change`, `Making it here, S: only internal/land changes`, `Plan for your ok, L: changes the brief every lead reads`, `Starting leads for 012 and 014`. Said once per request, not on every reply that follows; the QA hold line stays as it is.
- **The lead skill: the shape goes on the mesh.** The shape paragraph (the cut chosen and why, what goes to executors if any) is the lead's first `bmesh progress`, so `mesh-status` and `watch` carry it.
- **`bmesh started` records the session.** Run from a Claude Code session (`CLAUDE_CODE_SESSION_ID` set, as the context line (094) reads it), `started` keeps the session id on the node record, so a node's transcript can be found later on its box.
- **`bmesh audit [<node path> | --session <id>]`**, this session when neither is given, `--json` for the record. It reads the transcript (the main thread's, skipping `isSidechain` lines; subagents summed apart) and prints:
  - **What it said:** the rung each reply named, by the work skill's words.
  - **What it did:** the rung its actions imply, by fixed rules: a `bmesh build` or `lead start` is leads; `ideate` is a round; a push to `main` with no new file under `docs/proposals/` is made here; a reply ending in a question with nothing pushed is a plan; none of these is an answer. A named rung that differs from the implied one is printed as `mismatch`.
  - **What it read before it acted:** skills loaded (name, turn, words), each file read (words, whole or a range), each `bmesh` command's output (words), and the context in tokens at the first action that changes something (an Edit or Write, a commit, a push, a `bmesh` command that starts or changes something).
  - **The rules it had:** the rule sets on the `bmesh:rules` marker of the session's repo.
  - A node on another box: the audit runs there, as other per-box reads do, or names the box.
- **The skill test**: each chat case also scores that the rung the reply named matches the rung the case's end state implies, through the same rules as `audit` (one function, used by both). The fake answers `started` and `audit` as the binary does.
- The manual (`audit`, `started`'s record), internals (the node record field, the audit's rules) and the README index.

Out: scoring review findings by ponytail's lean classes; auditing Codex sessions; changing any rung or what it decides; a stored history of audits.

Order: after 100 (it rewrites the work skill this edits) and 096 (it changes the skill test's cases and summary).

Judge: opus, because the implied-rung rules must hold on real transcripts, not only the fixture's, and a wrong `mismatch` teaches the operator to ignore the line.

## What you'll see
A reply's first line says which rung it took and why. `mesh-status` shows each lead's shape. `bmesh audit` on this session, or on a lead's path, prints what it read before acting, the rung it named, the rung its actions imply and any mismatch.

## Verification
The repo's verify; `audit`'s rules tested on transcripts cut from real sessions of each rung (kept as testdata, nothing personal in them), including one whose named rung is wrong. Skill test, base (main before this) against cand, every chat case and `lead-s`, `--runs 2`: no case that passes on base fails on cand, the new rung expectation passes on cand, and tokens per arm in the closing commit. `bmesh audit` run on this session and on one live lead, its output in the closing commit.

## Risks and the alternative considered
- **The rung line becomes noise.** One line per request, not per reply; the audit is where it gets checked.
- **The implied-rung rules misread a session** (a rung-3 change pushed by a lead, a plan that asks nothing). The rules are few and printed with each verdict; a mismatch shows both sides, never a pass or fail.
- **Alternative: keep the studies as scripts outside the repo.** They answer one question each, run only on k16 and can't be checked by the skill test.

## Upgrading
nothing

## Needs from the operator
