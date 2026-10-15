---
id: 006
title: Operator skills in the plugin: propose, review, build, align, status
kind: ux
size: S
judge: fable
status: building
lens: eng
proposed_by: da6488fd-38bd-4c4c-994c-a7de257d9424
proposed_on: 2026-10-15
built_by: 21e658a3-361d-4582-b120-6c85e86e5404
host: k16
branch: t3code/742d9014
---

## Opportunity
Round two's spec 005 deleted command-center's `propose`, `dispatch` and `align-repo` skills on the reasoning that a wrapper restates the binary's help. That was wrong (Rick, 2026-10-15): on the phone the chat is the interface, and a CLI with no skill in front of it is unreachable there. The plugin ships skills for the threads it starts (lead, executor, propose-engineer, propose-ux) but none for the operator who starts them.

## Proposal
In, all in `skills/` of this plugin (so anyone who installs buildermesh gets them; nothing of any particular fleet):
- `buildermesh:propose`: "proposals for X", "run a round on X", "ideas for X" → `bmesh propose <repo> [--model m]` on the repo's `host` from `bmesh repos show <repo> --json` (locally when it is this box, else over ssh to that host; say so when the host can't be reached from here), the Fable fallback (`--model claude-opus-5-5` once, telling the operator what T3 said), then report the round path and both thread ids, and the status line: the round is started, the pitch arrives as a push.
- `buildermesh:review`: Rick's reply to a round in plain words ("1 and 3 yes, 2 smaller, 4 no, 5 later, 3 on opus", or just the verdicts after a push) → `bmesh spec review <repo> "<reply>"`; the repo comes from the push, the thread, or is asked for when ambiguous; report its one line per spec and the unmatched items, then the status line (the review commit, and `build` for the accepted).
- `buildermesh:build`: "build X 3", "build what's accepted in X", "ship 004" → `bmesh build <repo> <NNN>` or `--accepted <repo>` on the repo's host, the Fable fallback, then arm `bmesh watch --nodes <paths>` (a Monitor on the watch when the harness has one, re-armed with `--since` on exit 5; never a poll of T3), relay each `ask` to the operator and answer it with `bmesh --node operator send`, and report started threads and the status line.
- `buildermesh:align`: "align X" → `bmesh align <repo> --check`, show the diff, then `bmesh align <repo>`, the repo's verify, commit `Align on the shared definition of done and proposals`, push where there is a remote, and the status line.
- `buildermesh:status`: "what's building", "mesh status", "where is X" → `bmesh status` (and `bmesh spec list --repo` for a named repo), summarised: working, asking, silent, done since the last look.
- Each skill's description lists the trigger phrases; each body is short: the command, where it runs, what to report, and the status line rule ("end with one line: what is started or landed, and what waits on the operator"). The README's Builds section names the skills as the chat entry points.
- command-center's CLAUDE.md Skills section points at `buildermesh:propose|review|build|align|status` for the lifecycle (one sentence; land it directly on command-center `main`).

Out: a `t3_dispatch`-specific or ntfy-specific step inside the skills; anything the binary should do instead (refusals, host resolution beyond reading the row).

Judge: fable, because the trigger phrasing and what each skill says back are judgment.

## What you'll see
From the phone, "proposals for party-buzzer" in any session with the plugin starts the round; "1 and 3 yes, 2 no" after the push lands the review; "build what's accepted in party-buzzer" starts the leads and the session watches them. No command typed.

## Verification
Routine `verify`; each skill's frontmatter parses (`claude plugin validate .` or the repo's existing skill check); a real `buildermesh:status` and `buildermesh:align --check` run from a fresh session on k16.

## Risks and the alternative considered
Keeping the skills in command-center: rejected, the lifecycle is the plugin's and a stranger's install must have the same entry points.

## Needs from Rick
None.
