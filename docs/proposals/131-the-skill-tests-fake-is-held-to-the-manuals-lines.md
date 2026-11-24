---
id: 131
title: Every line the skill test's fake bmesh prints is checked against the line the manual quotes for it, so a line changed in the binary and the manual fails the test until the fake says it too
kind: maintenance
size: M
judge: opus
status: done
lens: eng
proposed_by: buildermesh-fix-23
proposed_on: 2026-11-24
ruled: [root, failing-first, structure, smallest]
fix_of: 121
from_feedback: buildermesh-build-28 (buildermesh-121) #6375
---

## Opportunity
`scripts/skilltest/fake/bmesh` answers the skills "as the binary does" by printing hand copies of the binary's lines, and nothing compares those copies with the binary or the manual. The tests that drive the fake (`TestFakeLead`, `TestFake`, the feedback tests) compare its output with a further hand copy in the test. So a line changed in `cmd/bmesh` and `docs/manual.md`, as `AGENTS.md` requires, leaves the fake saying the old line, and every test still passes. Skill-test sessions are then measured against a binary that no longer exists.

- **What 121's lead hit.** Spec 121 (land says where it is) changed `ready`'s line to end `; bmesh mesh-status shows where it is` (`cmd_ready.go:154`, the manual's `bmesh ready`). The lead also edited `TestFakeLead`'s copy of the line, so the test failed at the full verify, and then it edited the fake's copy by hand (commit b539e2c). Had it not edited the test, nothing would have failed and the fake would still print the old line. Its feedback (#6375): "scripts/skilltest/fake/bmesh keeps its own copy of bmesh's output lines (ready's among them), so changing a line in cmd/bmesh breaks TestFakeLead only at the full verify; I updated the copy by hand".
- **It has already happened unseen.** A prototype of the check below, run over today's `go test ./scripts/skilltest`, found the fake's `feedback list <family>` `next:` lines out of date (`fake/bmesh:777-779`). The fake says `next: an issue naming bmesh's own repo is its fixer's: leave it pending. ...` and ends its last line `dropped: <why> or to the fixer`. Since commit c990f0f (feedback naming bmesh skips the coordinator) the binary (`drainSteps`, `cmd_feedback.go:175`) and the manual (`bmesh feedback`, `list <family>`) instead print `next: merge duplicates and drop what main already covers ...`, or the GitHub-issue line when an issue names bmesh's module. Their last line ends `filed as <issue url>, not filed, or dropped: <why>`. The binary also leaves an issue naming bmesh's own repo on this box out of a coordinator's list (`familyFeedback`). So the `drain` case's premise is stale: its #52 (naming `buildermesh`) is shown to the coordinator with an instruction the binary no longer gives, and its references reply "52 is bmesh's and waits for its fixer".

Under `structure`, this drift has now been met twice (121's line, c990f0f's lines), so it becomes a check. Under `root`, the first wrong decision is that the fake's tests hold its output to their own copies, not to the reference the binary is held to.

## Proposal
In:

- **The check.** The `fake` test helper (`skilltest_test.go`), which every test that drives the fake goes through, takes each line the fake printed on stdout or stderr, less a leading `bmesh: `. It then finds the lines the manual quotes (`` `x` ``, or ``` ``x`` ``` when the line holds a backtick, read one manual line at a time) whose fixed start, before the first placeholder, the printed line begins with. When there is any such quote, the printed line must match one of them in full, or the test fails naming the printed line and the manual's quote. A printed line that begins no quote is not checked.
  - The matcher reads `<x>` as any text, `NNN` and `N` as digits, `[...]` as optional, and `...` or `…` as any text, with `\|` unescaped.
  - The prototype also flagged quotes that are shorthand, not the binary's line. `started NNN <title>: <path> on <model> (thread, branch, family F)` is one: the binary prints `(thread <id>, branch <branch>, family <F>...)`, `cmd_build.go:540`. Others are `this box is ...`, `usage: bmesh ...`, `would release ... (...)`, `verify ok t:<tree8> (recorded)` against the carried line, and Go's own `build: flag provided but not defined`. Each of these is cured either by making the manual's quote exact, which `AGENTS.md` already requires, or by the matcher's grammar. A test-side list of exempt lines is the last resort, and each entry on it says why the manual has no line for it.
- **The drift it finds, fixed toward the binary.** The fake prints the binary's `feedback list <family>` `next:` lines, choosing between them as `drainSteps` does. It leaves out of a coordinator's list what the binary leaves out. The `drain` case, its references and its row in `scripts/skilltest/README.md` follow what the binary does with its #52 today, so the case still measures something a coordinator does with that list. The lead decides what it measures, and says so in the README row. Fix any other drift the check finds the same way.
- **One copy fewer.** `TestFakeLead`'s and `TestFake`'s expectations of lines the check now holds to the manual keep only enough to tell the call apart (`ready: `). A line change then touches the binary, its own test, the manual and the fake, and the check catches a missed fake.

Out: generating the fake's lines from the binary or the manual (the fake is a POSIX script run per arm, and an old arm's manual is not this tree's); checking a fake's lines against an old arm's manual.

Judge: opus, because the matcher's grammar and the `drain` case's new premise are judgment calls.

## What you'll see
`go test ./scripts/skilltest` fails, naming the line, whenever the fake prints a line that the manual quotes differently. The `drain` case's coordinator sees the `next:` lines the real binary prints.

## Verification
The repo's verify, plus these failing-first runs, recorded in Shown:
- Put back the fake's old `ready` line (before b539e2c). The old `TestFakeLead` passes with it, which is the silent drift. With this change, the test fails and names the manual's `ready: <host>'s daemon lands <sha8>; ...` quote.
- On today's tree, the check fails on the fake's `feedback list` `next:` lines before the fake is fixed, and passes after.
- `go test ./scripts/skilltest -run TestReferences` passes with the `drain` case's updated references.

## Shown
- The check names a line the manual quotes differently (the old `ready` line, put back in the fake only): `go test -run 'TestFakeLead$' ./scripts/skilltest` -> `bmesh ready --text x printed "ready: k16's daemon lands 884b549d; it wakes you only if land needs you", which the manual quotes as ["ready: <host>'s daemon lands <sha8>; it wakes you only if land needs you; bmesh mesh-status shows where it is"]`
- The silent drift before this change (main's tests, the old `ready` line in the fake and in `TestFakeLead`): `go test -run 'TestFakeLead$' ./scripts/skilltest` -> `ok  	github.com/rickdiculousli/buildermesh/scripts/skilltest	0.673s`
- The check fails on the fake's `feedback list` `next:` lines before the fake is fixed: `go test ./scripts/skilltest` -> `feedback_test.go:54: bmesh feedback list shop-build-2 printed "next: an issue naming bmesh's own repo is its fixer's: leave it pending. Of the rest, merge duplicates and drop what main already covers (a spec, or a commit naming its #<seq>)", which the manual quotes as ["next: an issue naming <module> is bmesh's own, whose repo isn't on this box: ...`; after: `ok  	github.com/rickdiculousli/buildermesh/scripts/skilltest`
- The `drain` case's coordinator sees the binary's `next:` lines, the GitHub-issue line first as #52 names bmesh's module: `TestFakeDrainNext` in `scripts/skilltest/feedback_test.go`, its lines held to the manual by the check
- The `drain` case's references hold under its new premise: `go test -run 'TestReferences/drain' -v ./scripts/skilltest` -> `--- PASS: TestReferences/drain (3.21s)`
- The shorthand quotes the check met are made exact in `docs/manual.md`, with no exempt list: `started NNN <title>: <path> on <model> (thread <thread>, branch <branch>, family <family>[, held <gate>])` and the rest; the matcher's grammar is `TestManualQuotes` in `scripts/skilltest/manual_test.go`
- Easy to miss: one copy fewer, the tests keep only enough of a held line to tell the call apart: `expect("ready: ", 0, "ready", "--text", "x")` in `TestFakeLead`

## Risks and the alternative considered
A loose matcher passes drift, and a strict one forces exemptions. The prototype found 7 false candidates against one real drift, and making the manual's quotes exact fixes most of them while keeping the manual's promise. The alternative was to check only `next:` and `refused:` lines. That misses 121's own `ready:` line, so it lost.

## Upgrading
nothing

## Needs from the operator
