---
id: 086
title: AGENTS.md says k16 takes a change at its release, not at its landing
kind: refinement
size: XS
judge: sonnet
status: building
lens: eng
proposed_by: buildermesh-fix-3
proposed_on: 2026-10-23
ruled: [root, failing-first, smallest]
fix_of: 082
from_feedback: buildermesh-build-13 (buildermesh-082) #2339
built_by: 72dff5cc-ace8-494d-a75c-3e105a43b594
host: k16
family: buildermesh-fix-3
---

## Opportunity
`AGENTS.md` (the "After a change lands on `main`" paragraph) tells every session in this repo that k16 rebuilds and restarts within a couple of minutes of a landing, logging `update: <old8> -> <new8>`. That has not been true since `[update] track` arrived: its default is `release` (manual, Updates: "A box on `release` restarts once per release, on `latest` once per code landing"), and k16's `config.toml` sets `[update] repo = "buildermesh"` with no `track`. The lead of spec 082 (model routines start claude with only their kind's tools) relied on the sentence, waited for k16's daemon to run its landed change, and it never did; it drove `instance.Model.Run` from a throwaway test instead (feedback #2339).

Evidence, 2026-10-23 on k16: `journalctl --user -u bmesh` shows the last update `update: v0.3.2 -> v0.4.0` at 03:26Z; 082 merged at 04:40Z (`6064dfb3`); 21 commits sit on `main` past `v0.4.0` and the daemon logged no update since. The log line itself is wrong too: on `release` it names releases (`update: v0.4.0 -> v0.4.1`), not eight-character revisions.

The first wrong decision is that sentence (`root`); `grep` finds no other place in the repo (skills, README, `docs/agents-snippet.md`) that claims a box updates at a landing.

## Proposal
In: replace that one sentence of `AGENTS.md` with one that says what holds (`smallest`: the paragraph every session loads stays the same length): k16's daemon (`[update] repo = "buildermesh"`, `track` at its default `release`) rebuilds and restarts within a couple of minutes of a release being cut, not of a landing, and `journalctl --user -u bmesh` shows `update: v0.4.0 -> v0.4.1`; on the Mac, `bmesh update` after a release. To run a landed change on the real thing before its release, build it from the worktree (`go build -o cache/bmesh ./cmd/bmesh`) and run that binary. The sentence's exact words are the builder's, as long as it says those three things and names no release number as current.

Out: changing k16's `track` (a box's config, outside `main`: `reversible`), or any code: the manual's Updates section already describes the behaviour exactly.

Judge: sonnet, because it is one sentence checked against the manual's Updates section.

## What you'll see
A session in this repo reads that a landed change reaches k16's daemon at the next release, and how to run it before then.

## Verification
Failing first: the run above. Before, `AGENTS.md` predicts an `update: <old8> -> <new8>` line after 082's landing and `journalctl --user -u bmesh --since 2026-10-23T04:40Z | grep 'update:'` prints nothing. After, the sentence predicts an update at the next release, and once the fix lead cuts it (`bmesh release buildermesh`), the same `journalctl` shows `update: v0.4.0 -> <that release>` within a couple of minutes. The verify commands pass unchanged.

## Risks and the alternative considered
Alternative: set `track = "latest"` on k16 so the sentence becomes true. Rejected here: it is a box's config (`reversible`), and the release track was chosen so boxes restart once per batch, not per landing.

## Upgrading
nothing

## Needs from the operator
