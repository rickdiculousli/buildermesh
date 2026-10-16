---
id: 035
title: One command approves every hold, and the push names it
kind: refinement
size: S
judge: opus
status: proposed
lens: ux
proposed_by: operator
proposed_on: 2026-10-15
---

## Opportunity
A hold's push reads `ricks-macbook holds: buildermesh +99 commits (self-level)` and nothing else. To act on it the operator has to know `bmesh hosts held`, copy an id like `3vAeCDt93RbigprhvqIOxH`, and run `bmesh hosts approve <id>` once per hold, each with its own Keychain dialog. The first daemon start on the Mac held two repos at once (buildermesh and rickdiculousli-tools), and the operator's words were "I'm not going to remember the commands to run when I get notified next time". A trailing `# comment` pasted into an interactive zsh became extra arguments and printed only `usage: bmesh hosts approve <id>`, so the ids are a trap as well as a chore.

## Proposal
In:
- `bmesh hosts approve` with no id walks every hold on this box, oldest first: for each, the log as `showHold` prints it, then `Approve <repo> at <target8>? [y/N]`; `y` approves, anything else leaves it held (`left held: <id>`). The key is read once, before the first question, with the dialog announced as `approve <N> holds`; no holds prints `nothing held` and reads no key. Registry holds on the admin box are in the walk, asked the same way. With an id it works as today.
- A hold's push ends with `run at <host>: bmesh hosts approve` (the body, and the title when it fits in 200 characters), so the notification carries the one command.
- README: the Hosts section's `approve` entry and the hold's push text.

Out: `bmesh hosts deny` still takes an id (a walk that drops holds on a stray Enter is the wrong default); no approval from the phone or over SSH: approving stays at the box's own screen.

Judge: opus, because it changes how a signing key is read across several approvals.

## What you'll see
Notified of a hold, the operator types `bmesh hosts approve` at the Mac, answers `y` per repo after reading its log, and allows one Keychain dialog.

## Verification
The repo's verify commands. New tests in `cmd/bmesh` (through `BMESH_SECURITY_COMMAND` and `testdata/fake-security.sh`, never the real Keychain): no id with two holds approves the first and leaves the second on `n`, reading the key once (the fake's log shows one `find-generic-password -w`); no holds prints `nothing held` with no key read; a hold whose origin moved mid-walk asks again for that repo. In `internal/daemon`, the hold push's body ends with the command.

## Risks and the alternative considered
One key read signs several approvals: each is still its own `approve/v1` over its own job and target, checked by the daemon as now, and the key never outlives the command. Alternative: `bmesh hosts approve --all` with no questions; rejected, since the log per repo is the reason a secure box holds at all.

## Needs from the operator
