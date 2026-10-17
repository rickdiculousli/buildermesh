---
id: 057
title: choose-ideas warns when an accepted spec names a bmesh command another spec or main already uses differently
kind: refinement
size: S
judge: opus
status: proposed
lens: eng
proposed_by: dfdc9673-27b2-4f3f-9f17-95a5b436de66
proposed_on: 2026-10-17
from_feedback: buildermesh-build-7 (buildermesh-048)
---

## Opportunity
048 was written with `bmesh release <path>|--family F` to lift a hold. 046, accepted beside it, defined `bmesh release <repo>` for version tags and landed first. Nothing noticed until the coordinator read 048's started line mid-build and messaged the lead, which renamed the command to `bmesh unhold` as a scope change (feedback 1786). If 048's lead had run into it at rebase, it would have collided with a shipped command. Both specs were reviewed on the same day, by the same operator, in the same pitch.

## Proposal
In:
- **The names a spec defines.** A spec defines a command where its Proposal has a bold item that opens with a code span starting `bmesh <word>` (`**`bmesh release <repo> ...`**`). Those words are its commands.
- **`choose-ideas` checks them when it accepts.** For each spec turning `accepted`, it compares those commands with:
  - the binary's own command list on `main` (`bmesh help`, as the binary built from `main` prints it);
  - every other spec that is `accepted`, `adjusting` or `building`.

  A word another spec also defines, or that `main` already has, prints `warning: 048 defines bmesh release, which 046 (accepted) also defines`. The reply still lands, and the operator decides.
- **`bmesh build` repeats the warning** in its started line, so the coordinator sees it again at dispatch.

Out: flags and config keys (much noisier to read from prose); refusing the accept.

Judge: opus, because it reads command names out of prose, and a noisy warning gets ignored.

## What you'll see
Accepting 046 and 048 together prints the warning naming both. Accepting a spec that only uses an existing command, without defining it, prints nothing.

## Verification
The repo's verify, plus `cmd_spec_test.go` cases: two accepted specs that define one name warn; a spec defining a name `main` has warns; a spec that only mentions a command (not as a bold definition) doesn't.

## Risks and the alternative considered
A spec that redefines a command on purpose (a change to `bmesh update`) warns every time. The warning names the other spec or `main`, and the operator reads past it. Alternative: check at land time. By then the lead has built on the name.

## Needs from the operator
