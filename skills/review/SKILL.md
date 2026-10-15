---
name: review
description: Use when the operator replies to a proposal round's pitch in plain words - "1 and 3 yes, 2 smaller, 4 no, 5 later, 3 on opus", "accept 012", "reject 2", "all yes", or just the verdicts right after a round's push. Lands the verdicts on the repo's main with bmesh spec review and reports one line per spec.
---

# Review

The reply is passed to the binary as written; it parses the verdicts, maps pitch positions to spec ids from the newest settled round, and lands them on `main`. Don't paraphrase, reorder or pre-parse the reply, and never edit a spec by hand.

1. **The repo** is the one the reply is about: the repo named in the push or the pitch the operator is answering, else the repo this session's thread belongs to. When neither settles it (two repos with a pitch outstanding, a reply with no repo in sight), ask which repo in one line before running anything.
2. **The command.** `bmesh spec review <repo> "<reply>"`, the reply verbatim, from this box (it reads the mesh and pushes `main`; no host hop). It prints one line per spec (`NNN title: old -> new [on judge] [(note)]`), `unmatched: ...` for a clause it could not place (a number matching nothing, a phrase without a verdict, an ambiguous position), and `landed <sha8> on main: Review: ...` or `nothing to land`. A refusal (exit 3: `main` would not fast-forward) is reported as printed; the review stays on its branch.
3. **Report** the per-spec lines as the binary printed them, and the unmatched items as questions for the operator, never guessed at.

End with one line: the review commit on `main` (or that nothing landed), and what waits on the operator: the unmatched items, and `buildermesh:build` for the accepted specs.
