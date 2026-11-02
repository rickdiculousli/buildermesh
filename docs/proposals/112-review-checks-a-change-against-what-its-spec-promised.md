---
id: 112
title: Review checks a change against what its spec promised, and the lead and its executors show what they ran
kind: refinement
size: M
judge: opus
status: proposed
lens: eng
proposed_by: c510cd5e-a483-4a86-bbea-b4542577dcf4
proposed_on: 2026-11-02
---

## Opportunity
Verify proves the tests pass, not that the promised behaviour exists, and nothing else checks the promise. A change can pass every check and miss its spec, and only the lead's word covers it:
- **The review never reads the spec.** The review prompt (`examples/routines/review.toml`, and this repo's copy of it) reads `git diff <base>..HEAD` and hunts bugs. The spec is in that diff (the lead sets it `done` on its branch), but the prompt never mentions it, and land's job text is `base: <sha>` alone (`cmd/bmesh/cmd_land.go:765`).
- **The lead never shows the promises held.** The lead skill's "Verify and review" and "Land" never ask it to show each `What you'll see` line holds. Its done line is free text: 13 of the 112 lead done lines on the mesh name a command or an output.
- **An executor's done is free text** ("what's there and how you checked it"). 14 of its 40 done lines name a command or an output; the rest say "tests pass" or describe the piece.
- **The promises are many and often outside git.** The 35 landed specs sized M or larger average 1.8 lines of `What you'll see` (one paragraph, often several claims) and 5.2 of `Verification`. 15 of them ask for something only a run shows: a real skill-test run, a Mac soak, "numbers in the closing commit".
- **A seed prompt doesn't reach the other repos.** All four other ship repos (opengym-rick, gametrainer, party-buzzer, rickdiculousli-tools) have a hand-edited review kind that matches no version of the seed, and none has the last seed change (35c0c07, `git show` allowed, 2026-10-24). A spec check that lives only in the seed's prompt would reach this repo and new scaffolds, not opengym-rick, where most builds now run.

**It happens.** An Opus subagent audited the 12 latest landed M and L specs (093 to 111) against `main`; 100, 101 and 102 were checked again by hand. Of the 35 promises git can check, 4 went unkept, in 4 specs, and each landed on verify and its lead's word:
- **101**, "`mesh-status` shows each lead's shape", which is the visibility the operator asked for. Its table prints PATH to UNREAD and never the progress text (`cmd/bmesh/cmd_status.go:202`).
- **102**, "`bmesh watch` prints `settled <family>: <n> threads`". Watch ends at `watch-done` once every node is finished (`cmd_status.go:467`), which is before the daemon settles their threads.
- **100**, "a skill about half the size". Its landing commit says "2,642 words to 1,944".
- **094**, the context line on a coordinator's watch. The line prints only while the main thread waits on its own call, and the work skill runs watch in the background.

A reviewer holding the spec could find three of the four from the diff and `git show` of a file it names. Of the six specs that asked for numbers in the closing commit:
- 104's closing commit has none;
- 101's records a case that passed on base and failed on cand, which its Verification ruled out, and it landed `done`.

Limen (github.com/overment/limen) keeps a task note per job, checked before finish ("Done means: … Shown by: …", "Easy to miss: …", "Not proven yet: …"), and its scout packets end `Verified: <command> -> <real output line> | none`. This takes the prose discipline, not the machinery.

## Proposal
In, three parts:

**(a) The review checks the spec's promises.** The check rides in the review job's text, which bmesh writes, so it reaches every repo whatever its review kind says, and every reviewer box whatever its bmesh:
- A new embedded brief, `internal/brief/templates/review.md`, rendered with `{{base}}` and `{{spec_path}}`; embedded only, like `fix` and `executor`. It renders as `base: <sha>`, then `spec: <path>`, then the check:
  - read the spec whole (`git diff -U100000 <base>..HEAD -- <path>` shows every line, under either allow list in the fleet);
  - check each promise in `## What you'll see`, each `## Verification` item past the repo's verify, and each `## Shown` line against the code;
  - a promise the code doesn't keep is `Must fix`, its case the spec's words and what the code does instead, with the file and lines;
  - a `Shown` line the code couldn't produce (an output no path prints, a test that doesn't exist or doesn't check it) is `Must fix`; check the `Easy to miss:` line the same way;
  - a promise only a run outside git can show is `Not checked` when its `Shown` line carries evidence or a `Not shown:` line names it, and `Should fix: no evidence for <promise>` when neither does;
  - a promise the code keeps but `Shown` leaves out is at most `Nice to have`.
- `bmesh land`'s `queueReview` sends the rendered text in place of `base: <sha>`. `bmesh routine queue review`, run from a lead whose node names a spec, renders the same text when its `--text` has a `base:` line and no `spec:` line; any other text is sent as given. `BaseFromText` already finds the `base:` line anywhere, so the patch-id carry (027, 038) is unchanged.

**(b) The lead shows each promise in the spec.** The half-pager gains `## Shown` after `## Verification`, empty when proposed. The lead skill says, before the review is queued and in the commit that sets the spec `done`:
- one line per promise of `What you'll see` and per `Verification` item past the verify run: `- <the promise, in a few words>: <evidence>`;
- evidence is a command it ran and one line that command printed, copied (`` `cmd` -> `line` ``), or a file in the tree (a test by name); never a command it didn't run or a line it didn't see;
- `- Easy to miss: <the one rule of the spec the quickest finish would skip>: <evidence>`;
- `- Not shown: <promise>: <why>` for what it couldn't show.

The spec is where the evidence lives, because the reviewer, limited to git, reads it in the diff, and it lands on `main` with the code. The ready text is read by no reviewer and goes to `watch` and the phone, where lead done lines run a median 169 characters; a lead landing in its own turn has none. The commit message is one of several the lead writes, and the spec outlives it. Written in the commit that sets the spec `done`, which the lead already makes after verify, it adds no verify run. The done line ends `shown <n> of <m>` and names each `Not shown` promise; the closing message quotes the section.

**(c) An executor's done carries what it ran.** The executor skill's done text and the executor brief's report line become `<what's there>; Verified: <command> -> <a line it printed> | none`, never a command it didn't run or output it didn't see. The lead skill: an executor's `Verified: none` is a piece the lead checks itself before merging.

**Docs.** The manual's `land` review step, `routine queue`, the review kind example, the brief templates list and `spec template`; internals' job text; the skill test's README, for its new cases and expectations. The README index gains nothing: no command or flag changes.

**Measured with `scripts/skilltest`.**
- The review harness renders the arm's `review` brief into the job text for a case with a spec. An arm without that brief sends `base:` alone, as before.
- `testdata/review-spec/`: a base with its own spec sized M, `proposed`, whose `What you'll see` makes three promises, and two changes. Both pass the fixture's tests and plant no bug, and each sets the spec `done` with a `Shown` section:
  - `miss/` keeps two promises and drops the third in a way only the spec says is wrong, and its `Shown` is silent on it;
  - `met/` keeps all three.
- Two cases with reference replies, as `review` has:
  - `review-spec-miss` passes when a finding names the dropped promise (its file or flag, and a cue from its sheet), the reply ends `VERDICT: fail`, and no finding short of `Nice to have` is about a kept promise;
  - `review-spec-met` passes when the reply ends `VERDICT: ok`.
- The lead cases (`lead-s`, `lead-stop`, `lead-exec`) gain `shown`: the branch's spec has a `## Shown` line for its promise with a backticked command and `->`, or a test or file named, and an `Easy to miss:` line.
- `lead-exec` gains `executor_verified`: the executor's `done --text` has `Verified: none` or `Verified: <command> -> <line>`.

Out:
- parsing transcripts to confirm a command ran;
- a second model family for review;
- changes to the land gate's mechanics: land refuses, waits and records as it does;
- a machine check that `Shown` or `Verified:` exists, which review is the check for. If audits later show leads skipping `Shown`, a check is the next step.
- the review kind's prompt in any repo;
- a fixture where `Shown` claims evidence the code can't produce;
- spec checks for work made on rung 3 with no spec.

Judge: opus, because the check must fail a dropped promise without failing a kept one, and the fixture's miss must be one only the spec shows.

## What you'll see
- A lead's review job text reads `base: <sha>`, `spec: docs/proposals/NNN-….md`, then the spec check, in every repo whatever its review kind says.
- A change that passes verify but drops a promise of its spec gets a `Must fix` finding: the spec's words, what the code does instead, and `VERDICT: fail`.
- Every spec a lead lands carries a `## Shown` section: each promise with a command and a line it printed, or a file; one `Easy to miss:` line; any `Not shown:` promise, which the lead's done line names.
- An executor's done line ends `Verified: <command> -> <line it printed>` or `Verified: none`.
- `go run ./scripts/skilltest --arm base=<main before> --arm cand=. --cases review-spec-miss,review-spec-met` opens with `review-spec-miss run 1: base fail, cand pass`: the old review passes the change, and the new one fails it. `review-spec-met` passes on both arms.

## Verification
- The repo's verify.
- Unit tests:
  - land's review job carries the spec's path and the check (the `j.Text != "base: "+base` assertions in `cmd/bmesh/cmd_land_test.go` change);
  - `routine queue review` from a lead adds them, and leaves alone a text that has a `spec:` line or comes from a node without a spec;
  - `ReviewedPatch` reads the base from the new text;
  - the brief renders with no placeholder left.
- `go test ./scripts/skilltest` plays each new case's references:
  - a reply naming the dropped promise passes;
  - one naming only kept promises, or ending `VERDICT: ok`, fails `review-spec-miss`;
  - a `Must fix` on a kept promise fails `review-spec-met`;
  - `shown` and `executor_verified` have good and bad end states.
- One real run, base = `main` before this change and cand = the branch, `--cases review,review-spec-miss,review-spec-met,lead-s,lead-exec --runs 2`. It passes when:
  - `review-spec-miss` flips from base to cand;
  - `review-spec-met` and `review` pass on cand;
  - every lead-case expectation that passed on base passes on cand.

  The flips and each arm's tokens go in this spec's `## Shown`.

## Shown

## Risks and the alternative considered
- **False fails cost a lead a wake at full context.** A review that fails a kept promise sends the lead through another land cycle. `review-spec-met` measures it, and an unconfirmable promise goes to `Not checked`, never a finding, when the lead showed it or said why not.
- **A lead can write a plausible output line it never saw.** The reviewer checks the line against the code, not that the command ran; confirming runs is out by the operator's call.
- **More text to load.** The job text grows by about 1 KB per review, against a 2.3 KB prompt and the diff it reads. The lead skill grows by about 700 bytes on 7.8 KB, read by every lead, and the executor skill and brief by about 200 bytes on 4.5 KB.
- **Alternative: put the check in the review kind's prompt.** It is smaller, but it reaches only this repo and new scaffolds; the other four ship repos' kinds already miss the last seed change.
- **Alternative: the reviewer's box adds the check.** It depends on that box's bmesh, and a Mac updated by hand would review without it, silently.

## Upgrading
nothing. A repo whose `.bmesh/briefs/half-pager.md` overrides the template keeps its own; a lead adds `## Shown` to a spec that lacks it.

## Needs from the operator
