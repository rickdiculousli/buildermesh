---
id: 090
title: choose-ideas takes spec ids and flags, not a reply in plain words, so the agent reads the operator and the binary never guesses
kind: refinement
size: M
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-23
---

## Opportunity
`bmesh choose-ideas <repo> "<reply>"` parses the operator's plain words itself (`internal/spec/review.go`, 463 lines, plus `PitchRanks` and `latestRanks`, which read the newest settled round off the mesh to turn `1` into a spec id). That makes its input non-deterministic twice over: the same words mean different specs depending on mesh state, and the grammar keeps growing. Of the 13 commits that touch `review.go`, 10 are review fixes to that grammar (clause splitting, verdicts said first, a short number that is both an id and a position, one-line pitches, `v1.2` in titles). The manual's section on it is longer than the one on `bmesh build`.

The agent in front of the operator already understands the words, but the binary second-guesses it. "accept all" is refused with `all: there is no pitch to answer; name the specs` whenever no settled round sits on the mesh (specs written in a session, an idea thread, a round wiped, the mesh unreachable). The agent then asks the operator which specs, which is exactly the back-and-forth the operator doesn't want.

## Proposal
The binary takes only explicit ids and verdict flags, and applies them as given. Reading the operator's words is the agent's job, and it never asks a question that the ledger can answer.

**`bmesh choose-ideas <repo> [--accept IDS] [--later IDS] [--reject IDS[=reason]]... [--adjust NNN=instruction]... [--judge IDS=fable|opus|sonnet]...`**

- `IDS` is a comma list of three-digit spec ids (`012,014`) and nothing else: no `all`, no positions, no ranges. The binary never decides which specs a word covers.
- `--reject` and `--adjust` repeat; the text after `=` is the reason or the instruction, kept as written. `--judge` repeats and sets `judge` with or without a move.
- Every value is checked before anything is written. Nothing lands, with exit 2 and every problem listed on one line each, if: an id isn't in the ledger; a spec isn't `proposed`, `later` or `adjusting`; one spec gets two moves; a judge isn't one of the three; or no flag was given. There is no `unmatched:`, no "last clause wins" and no partial landing.
- Everything else stays as it is: levelling `main`, the review worktree, the command-clash warnings, the `Review: ...` commit, the push and the per-spec output lines. The output loses only the `note: ... spec ids only` and `unmatched:` lines.

Out: `ParseReply`, `PitchRanks`, `latestRanks`, the reply grammar, its tests and the manual's grammar section are deleted, not kept beside the new form. A positional reply argument is refused with usage (exit 2). `bmesh spec set` is unchanged.

**The skills.** `choose-ideas` (and the reply paragraphs in `ideate-engineer` and `ideate-ux`) say that the agent maps the operator's words to flags itself. A position like `1 and 3` is that line of the pitch or list shown in this conversation, and pitch lines start with their ids. "all" or "everything" is every id of that pitch or list and nothing more. A `proposed` spec that never came up in the conversation is never swept in. `smaller` and other non-verdict words become an `--adjust` instruction. The agent then runs the command without confirming. It asks only when the words can't be tied to ids discussed here, for example "accept all" in a session that showed no pitch. It asks in one line, listing the open specs from `bmesh spec list --status proposed`, so the reply can be ids.

Judge: opus, because the deletion crosses the spec package, the command, three skills and the manual, and the skills' mapping rules are what keep the agent from asking questions.

## What you'll see
After a pitch of 091 to 095 in this session, `accept all` becomes `bmesh choose-ideas buildermesh --accept 091,092,093,094,095`, with no question asked, and 096, proposed elsewhere and never discussed, stays `proposed`. `1 and 3 yes, 2 smaller` after a pitch of 091 to 093 becomes `--accept 091,093 --adjust "092=smaller"`. A typo'd id refuses the whole review, naming the id, and nothing lands.

## Verification
The repo's verify commands. Table tests through the command (`cmd/bmesh/cmd_spec_test.go`) for: ids accepted with no mesh; `all` refused as an unknown id; each refusal (unknown id, wrong status, two moves, bad judge, no flags, a positional reply) leaving `main` untouched; reject with and without a reason; adjust writing the `Adjust (<date>):` note; judge alone; and the clash warning still printed. `grep -rn "ParseReply\|PitchRanks\|latestRanks" .` returns nothing. The manual section, the README index row and `bmesh help` show the new synopsis.

## Risks and the alternative considered
- **The agent maps a position wrongly.** Pitch lines start with ids, and the per-spec output names each spec's title, so a wrong mapping shows up in the report. Today's binary can make the same mistake with mesh state, and nobody can see it happening.
- **Considered:** keeping the grammar and teaching it `all` without a pitch. That fixes the one refusal and keeps the 460-line parser and its long run of fixes going.
- **Considered:** an `all` keyword meaning every `proposed` spec. Rejected: it would accept specs the operator never saw (another round's, one written in another session). "all" means what was discussed, which only the agent in the conversation knows.

## Upgrading
Any script or saved prompt calling `bmesh choose-ideas <repo> "<reply>"` must switch to the flags. The old form is refused with usage (exit 2), and the plugin's skills change in the same release.

## Needs from the operator
