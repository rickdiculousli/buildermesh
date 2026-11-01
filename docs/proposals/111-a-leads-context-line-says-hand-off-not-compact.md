---
id: 111
title: A lead's context line says to hand off, not to compact
kind: refinement
size: M
judge: opus
status: done
lens: eng
proposed_by: buildermesh-fix-10
proposed_on: 2026-11-01
ruled: [root, structure, failing-first, smallest]
from_feedback: opengym-rick-build-13 (opengym-rick-022) #3805
---

## Opportunity
A lead at 306k context got bmesh's context line `Compact at your next milestone, ...` with how to compact (094's line, `internal/session` `Advice`), while the lead skill says the opposite: "When a `bmesh` line says your context is large and advises compacting, hand the next run to an executor instead and retire at done: a lead never compacts." (`skills/lead/SKILL.md`, the paragraph on what to keep for yourself). The lead followed the skill (feedback #3805 from lead `opengym-rick-022`). The first wrong decision is the binary's: it gives every main-thread session one advice, so the skill has to override it in prose. Under `structure` the warning moves into the binary, which tells a lead's session apart, and the sentence goes.

## Proposal
In:

- **The mark.** `started` (`recordSession` in `cmd/bmesh/cmd_events.go`), for a node with no parent whose record's `role` is `lead`, marks its Claude Code session a lead's in the session's context state, `<state dir>/context/<session id>`: the line `<level> <tokens>` gains a third word, ` lead`. The mark is written before the record's `session` check (a `started` from the session already recorded still marks it), and an error writing it is `started`'s error. `internal/session` gets `MarkLead(dir, id) error` (reads the state, sets the mark, writes it through the existing temp-file rename; a no-op when already marked) and `IsLead(dir, id) bool`; `state` gains `lead bool`, read and written by one `readState` / `writeState` pair that `Heard` uses too, so `Heard` keeps the mark on every write. A file without the word reads as not a lead's.
- **The advice.** `Advice(c, level, lead bool)`: from `compact_from` (and from `compact_soon`), a lead's session is told `A lead never compacts: hand your next run of settled work to an executor, and retire at done.`, without how to compact; below `compact_from` and for every other session the advice is as it is. `Heard` passes its state's mark; `bmesh context` (`contextAdvice`) passes `session.IsLead(config.StateDir(), r.ID)`. No mesh read: the line stays offline, as `bmesh context` needs no hub.
- **The skill.** The lead skill's sentence above goes; none replaces it.
- **Docs.** The manual's `bmesh context` advice table gains the row `from compact_from, to a lead`; its sentence `A lead hands its next run of work to an executor instead of compacting (its skill says so).` becomes one saying a lead's session is the one a lead's `started` ran from, linking `bmesh started`; its state-file sentence names the mark; `bmesh started`'s "The session" paragraph says a lead's `started` marks its session. `docs/internals.md`'s `<state dir>/context/<session id>` line names the ` lead` word.

Out: a fixer, round or coordinator session (the fix skill and others say nothing against compacting, so they keep the compact advice); a mesh lookup of the role at each line.

Judge: opus, because the state-file change must keep `Heard`'s compaction and config-change rules intact.

## What you'll see
A lead's session past 300k hears `bmesh: this session's context is 304k. A lead never compacts: hand your next run of settled work to an executor, and retire at done.`; `bmesh context` there says the same. A fixer's session at 304k still hears the compact advice.

## Verification
The repo's verify commands. Fails before, passes after: a test in `cmd/bmesh/cmd_context_test.go` through the real commands (`setup`'s mesh, `putNode` a lead `app-012` and a fixer `app-fix-3`; each runs `started` from a Claude session at 200k, then `inbox` at 304k must end its stderr with the lead advice for the lead and `milestoneAdvice` for the fixer, and `context` at 412k must print the lead advice for the lead and `Compact at the next safe point.` with how to compact for the fixer). Before the change the lead's `inbox` ends with the compact advice (run once by the fix lead: it failed so). `internal/session`'s `TestLevelAndAdvice` passes `false` and keeps its rows; add a row or test that a marked session's `Heard` keeps the mark across a compaction and a level rise. The skill change is measured with `scripts/skilltest --cases lead-s,lead-exec` against `main` (one run each, by the fix lead: all four cells passed, no flip, $0.19 to $0.33 a cell on both arms); the lead cases never reach a context line, so this shows only that removing the sentence changes nothing else.

## Risks and the alternative considered
A lead session that never ran `started` (none should: it is a lead's first command) keeps the general advice. A state file pruned after 7 days unchanged loses the mark; a lead active that long rewrites it with each level. Considered: reading the node's role from the mesh when a line is owed; it needs a hub at every measured command (and a dial from commands that don't connect), and `bmesh context` promises none.

## Upgrading
nothing

## Needs from the operator
