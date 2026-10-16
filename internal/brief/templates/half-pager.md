---
id: {{id}}
title: {{title}}
kind: {{kind}}
size: {{size}}
judge: {{judge}}
status: proposed
lens: {{lens}}
proposed_by: {{thread}}
proposed_on: {{date}}
# Guide lines; delete them in a real spec. Lines starting with # are ignored.
# id: three digits, the next free number in this repo (`bmesh spec next-id`). File: docs/proposals/NNN-slug.md
# kind: feature | refinement | ux | test-speed | token-cost | time-cost | maintenance
# size: XS (one file) | S (one module) | M (crosses modules) | L (changes an interface others depend on). Never hours.
# judge: who should build it. opus: the default, for anything with real judgment or complexity. fable: only exceptional taste or nuance that opus would visibly miss, never because the work is hard. sonnet: trivial, a careful junior could do it from the spec.
# status: proposed | accepted | adjusting | later | rejected | building | blocked | done
# lens: eng | ux
# Set later, never by the proposer:
# built_by: <thread id>       set by dispatch
# branch: <branch>            set by dispatch, corrected by the build thread if the runner renamed it
# host: <box>                 set by dispatch
# merged: <sha>               set by the build thread
# verified: <entries>         set by the build thread (bmesh spec verified --write)
# reason: <one line>          rejected or blocked only
# scope_grew: [<ids>]         set by the build thread when landed specs widened it
---

## Opportunity
What's weak or missing, with the evidence seen: a timing, a screenshot, a repeated manual step, a principle in the product doc it falls short of.

## Proposal
The change. In: ... Out: ...

Judge: <opus, fable or sonnet>, because <one clause>.

## What you'll see
The visible result when it's done.

## Verification
Which verify commands prove it; anything new that's needed.

## Risks and the alternative considered

## Needs from the operator
Empty unless a taste call really is theirs, or a config key some box must set to turn the change on: name the key and the file it lives in.
