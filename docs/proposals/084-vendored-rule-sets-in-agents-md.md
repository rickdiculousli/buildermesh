---
id: 084
title: bmesh ships small engineering rule sets a repo picks on one marker line, scaffold writes them into AGENTS.md, and the pstack steps that never run come out of the skills
kind: feature
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-22
built_by: af32e96b-1ffc-46ef-bf07-666a2547f12b
host: k16
family: buildermesh-build-11
branch: bmesh/af32e96b-1ffc-46ef-bf07-666a2547f12b
---

## Opportunity
The lead, executor and both round skills name pstack skills at nine moments, each "if `pstack:<skill>` is in this session's skill list" (`skills/lead/SKILL.md:17,19,52,62,76`, `skills/executor/SKILL.md:14,36`, `skills/ideate-engineer/SKILL.md:30,58`, `skills/ideate-ux/SKILL.md:29,59`). That test never passes: in the installed pstack (0.15.30), 56 of its 57 skills set `disable-model-invocation: true`, so only `setup-pstack` is ever listed, and 083 found `poteto-mode`, `architect` and `blast-radius` in none of 49 leads' listings. `control-ui` (lead:52) is not in pstack at all. This is the third change resting on one premise, that pstack's skills are model-invocable: 003 added the moments, 021 moved the gate from "installed" to "listed". The manual's table of them and its pstack link (`docs/manual.md:3211-3226`) point at a fork the operator has since deleted; pstack ships natively now.

So no node gets engineering principles from anywhere. What each session does get is the repo's `AGENTS.md`, which scaffold already writes in part (`internal/scaffold/scaffold.go:125`), and which the fix brief already names as where "the code-design principles (types, boundaries, domain modelling, reader load)" are judged (`internal/brief/templates/fix.md:24`). The operator wants a few vendored sets any bmesh user can pick from and combine, with the text left theirs to tweak.

## Proposal
In:
- **Five rule sets, embedded** (`internal/scaffold/rules/<name>.md`), in bmesh's own words, loosely after pstack's principles (MIT, Lauren Tan, credited in the manual). Exactly:
  - `core` (777 B):
    - Make the smallest change that solves the problem. Delete what's dead before adding.
    - Read the code before you change it, and say what you learned before deciding.
    - Fix the first place it went wrong, not where it showed. A guard is a fix only when the state it handles is legitimate.
    - Prove it works on the real thing: run it the way a user would. Passing tests prove only what they reach.
    - When two fixes have failed on the same assumption, stop and test the assumption.
    - A rule you've had to state twice becomes a check or a script, not a third sentence.
    - Do reversible work, then report it. Ask only what no experiment or reading can settle.
    - Before trusting a number (a timing, a speedup, a count), find what limits it and rule out that it measured something else.
  - `design` (501 B):
    - Name the data shape before writing logic.
    - Keep the domain in one structure (a type, a table, a state machine), not in conditionals spread across files.
    - Check input where it enters the system. Inside, trust your own types.
    - One writer per file, branch, key or record. Remove the sharing before reaching for a lock.
    - Make steps safe to run twice, so a retry after a crash ends in the same state.
    - When a new API replaces an old one, move every caller and delete the old one in the same change.
  - `tests` (334 B):
    - Test what callers can observe, not how the code does it. A test that breaks on a rename is testing internals.
    - Test through the real entry point (the command, the package's public functions) where you can.
    - A piece isn't done while no test exercises its code.
    - A flaky test is a bug. Find the cause rather than retrying it away.
  - `tdd` (214 B), an add-on apart from `tests` because the cycle, not the text, is what costs tokens:
    - Write the failing test first, and watch it fail for the reason you expect.
    - Write the least code that makes it pass, then tidy with the test passing.
    - Give each behaviour the change names its own failing test.
  - `prose` (367 B):
    - Say what a thing does, with the mechanism or a number, not how it feels.
    - Short sentences, one idea each, in the active voice.
    - Plain words: "use", not "leverage". "Is", not "serves as".
    - A comment says why, when the code can't show it. Delete comments that narrate what the code does.
    - Cut filler, hedges and closing summaries that repeat the text above them.
- **A fourth `AGENTS.md` block, `rules`, appended after `mesh`.** Its opening marker carries the pick and a hash: `<!-- bmesh:rules core design tests a1b2c3d4 -->`, the hash the first 8 hex of the sha256 of the body scaffold wrote. The body is a `## Engineering rules` heading, one line ("From bmesh's rule sets `core`, `design`, `tests`. Where the rest of this file says otherwise, the rest wins. Edit them freely: `bmesh scaffold` keeps an edited block and marks it customized."), then each set's lines under its name, in the marker's order.
  - **The pick is the marker.** A marker with set names and no hash is a fresh pick, and scaffold renders it. A missing block is added as `core`. An empty list (`<!-- bmesh:rules -->`) writes no block body and stays off. A name scaffold doesn't know is refused (exit 1, `AGENTS.md: unknown rule set <name>; bmesh has core, design, tests, tdd, prose`).
  - **An edit is kept.** When the body's hash differs from the marker's, scaffold keeps the body, rewrites the marker to `<!-- bmesh:rules customized -->` and prints `AGENTS.md: rules are customized; kept as written`. A `customized` block is never rewritten. Putting set names back on the marker, or deleting the block, returns it to bmesh's text on the next run.
  - **Only this block.** `done`, `proposals` and `mesh` say what the binary enforces and are rewritten as today.
  - **`land` doesn't check it.** `scaffold.Drift` leaves the rules block out, so a branch that edits or re-picks its rules lands; `scaffold --check` shows a customized block as kept, not as a diff.
- **The `scaffold` skill interviews the operator about the rules.** Before step 2 writes, when the repo's `AGENTS.md` has no rules block (its first scaffold, or the first after this change), or when the operator asks to change a repo's rules ("change X's rules"):
  - it shows each set's lines, from the manual's Rule sets section, and asks which to pick (several may be chosen, or none). It recommends `core`, plus `tests` when the repo has tests and `design` when it holds stateful or concurrent code, each with the one clause of evidence it saw;
  - it writes the pick as an empty block, `<!-- bmesh:rules <sets> -->` and its closing marker, at the end of `AGENTS.md`, or rewrites the existing opening marker, and `bmesh scaffold` fills it;
  - on a `customized` block it says so and asks whether to keep it as written or go back to bmesh's sets, keeping it by default;
  - it says the text can be edited afterwards and scaffold will keep the edit;
  - **then it clears conflicts out of the repo's own prose.** It reads the repo's instruction text outside bmesh's markers (`AGENTS.md`, `CLAUDE.md`, and any file `AGENTS.md` says to read at the start) for lines that restate a picked rule or contradict one, and shows each beside the rule line it meets. A restatement is deleted, since the block now carries it. A contradiction asks which wins: keep the repo's line and cut or reword the rule in the block (which marks it customized), or delete the repo's line. It also flags lines that rest on a set the operator didn't pick (a "write the failing test first" with no `tdd`) and asks the same. It changes only what the operator answers, and stages those files with the ones `bmesh scaffold` wrote, in the same commit.

  The "the rest wins" line in the block settles anything the interview missed: a repo's own words always beat bmesh's defaults.

  The binary's default stays `core`, for scaffolds no one is asked about (a lead's own branch, a rescaffold by the row's host).
- **bmesh's own `AGENTS.md`** gets `<!-- bmesh:rules core design tests ... -->` (the operator's pick: no `tdd`).
- **The skills lose every pstack moment**, and the written default becomes the step:
  - lead:17 and executor:14: "Work by the repo's `AGENTS.md`, its engineering rules included." The executor's hand-TDD sentence goes: test-first now comes only from a repo's `tdd` set, and "no piece done without a test" from `tests`.
  - lead:19, ideate-engineer:30, ideate-ux:29: the by-hand widen-and-attack text, unconditional.
  - lead:52: the `control-ui` clause goes. lead:62: the by-hand caller list, unconditional. lead:76, executor:36, ideate-engineer:58, ideate-ux:59: the `figure-it-out` clause goes, its by-hand text kept where there is one.
  - `method:writing-plans` (lead:18) stays: it is listed, and leads ran it 5 times in 083's sample.
  - **No skill or brief restates a rule.** bmesh's own text carries the mesh's process, and the sets carry engineering principle. A grep of `skills/`, `internal/brief/templates/` and `docs/agents-snippet.md` for the sets' subjects finds two restatements, both cut above: lead:17's "read before you change, keep each step small enough to verify, state what you learned" (`core` 1-2) and executor:14's test-first cycle (`tdd`). The round skills' widen-and-attack step is how a round proposes, not a rule for a repo's code, and stays. The fix rulings overlap `core` by design and stay (Out). The build repeats the grep and lists in its report what it found and did.
- **Docs.** Manual: scaffold's step 1 and refusals, and the `scaffold` skill's interview in the Skills section; a "Rule sets" section with each set's exact text and the pstack credit; land's scaffold check; the plugin section's optional-skills table and pstack link replaced by one line on `method`; the plugin-refresh example (`docs/manual.md:3061`) without `pstack@pstack-claude`. Internals: the marker's hash. README's scaffold line.

Out: named sets in a repo (`.bmesh/rules/`) or per box (`~/.config/bmesh/rules/`), so a repo's own rules are free text in `AGENTS.md`; a registry or config key; a `bmesh rules` command; the fix rulings (069), which judge fixes to bmesh itself and stay separate; rendering rules into briefs.

Judge: opus, because the block's hash and the land exemption decide whether an operator's edit survives, and the skill edits touch every lead and executor.

## What you'll see
"Scaffold party-buzzer" shows the five sets and asks which to pick, recommending `core` and `tests` because the repo has tests. With that pick, `bmesh scaffold` prints `wrote AGENTS.md`, and the file ends with a `bmesh:rules core tests` block. The skill then shows that the repo's `CLAUDE.md` says "keep diffs minimal" (`core` 1 restates it: delete?) and "mock the database in every test" (`tests` 2 says test through the real entry point: which wins?), and commits what the operator settles. Change the marker to `core tests tdd`, rerun, and the tdd set appears. Reword a line, rerun: `AGENTS.md: rules are customized; kept as written`, and the marker reads `customized`. A lead's skill no longer mentions pstack; its first turns read the rules from `AGENTS.md`. bmesh's own `AGENTS.md` grows by about 1.8 KB.

## Verification
The repo's verify, plus:
- `internal/scaffold`: a missing block appends as `core`; a marker's sets render in order with the right hash; a second run changes nothing; an edited body is kept and its marker becomes `customized`; a `customized` block is never rewritten; an empty marker writes nothing; an unknown name refuses; `Drift` ignores a changed rules block and still catches a changed `done` block.
- `cmd/bmesh`: `land` on a branch whose rules block was edited passes its scaffold check.
- A docs test: `docs/manual.md` contains each embedded set's text.
- A skills test: no file under `skills/` names `pstack`; the `scaffold` skill names the rules interview, the conflict pass over the repo's own prose, and the manual's Rule sets section; no file under `skills/` or `internal/brief/templates/` holds "test that fails" or "small enough to verify".
- Whether an executor sees the block can't be tested with the runner fakes: the build records, from one real lead's executor transcript, whether its first turn shows the rules block (Risks).

## Risks and the alternative considered
- **Executors may not read `AGENTS.md`.** An executor is an Agent-tool subagent of its lead; whether Claude Code gives a subagent the project's instruction files is unverified. If it doesn't, the lead skill tells the lead to quote the rules block in each executor brief, and the build says which it found. Found in the build: it does. A subagent this build's lead spawned listed the worktree's `AGENTS.md` among its project instructions, as the lead's session had loaded it at start (a rules block added mid-session wasn't in it), so an executor works by the rules its lead started with, which come from `main`; the lead skill needs no quoting step.
- **A rule set grows every session's context.** `core` is 777 B in every session of every ship repo; bmesh's pick about 1.6 KB plus the heading. 083 measured instructions at 9.1 KB of a lead's first 101 KB request.
- **Executors stop test-driving in repos without `tdd`.** That is the operator's call above: today they test first only because pstack never loads.
- **Alternative: render the sets into briefs.** It reaches only threads bmesh starts, not executors reliably or the operator's own chat.
- **Alternative: a row key for the pick.** A change would need `bmesh repos publish`; the marker travels with git and sits beside its text.

## Upgrading
Scaffold each ship repo through the `scaffold` skill ("scaffold <repo>"), which asks which rule sets it gets, and commit the result. A plain `bmesh scaffold <repo>` gives it `core`. To pick other sets, edit the marker's names and run it again; to have none, empty the marker. Update the buildermesh plugin after `bmesh update`: the old skills still name pstack moments, which are harmless (they never run).

## Needs from the operator
