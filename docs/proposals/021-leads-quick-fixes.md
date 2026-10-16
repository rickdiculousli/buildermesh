---
id: 021
title: Every command says what it did, and the brief stops the small detours leads keep taking
kind: refinement
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-15
from_feedback: retrospective of 007-016 and 018 (buildermesh-007, -008, -009, -010, -011, -012, -013, -014, -015, -016, -018)
built_by: c184e544-9943-465a-b943-da1ef21989da
host: k16
family: buildermesh-build-1
branch: t3code/813bdeda
---

## Opportunity
Eleven leads were asked after landing what friction another build would hit. These are the issues that have one cause and one obvious change, each reported by two or more leads:

- `bmesh done`, `started`, `progress` and an empty `inbox` print nothing on success (010, 011, 012, 013, 014, 015). Leads ran `mesh-status` to confirm, and two called `done` again and got `refused: illegal state transition done -> done` (exit 3), which reads like a failure.
- The version-skew line (`k16 runs X, this is Y; run bmesh update`) prints before the command's output, so a `| tail` or `grep -v` hid the result and broke exit codes (007, 008, 012).
- `bmesh routine run verify` ends on `go test`'s last line, not on a verdict, and needs a separate `result find` (018). `result find <kind>` reports no result unless `--model` is given (012). `result record` with no flags prints only `see bmesh help` (016).
- The lead, executor and ideate skills say "if the pstack plugin is installed, run `pstack:<name>`". The plugin was installed while those skills were not invocable, so every one of the eleven leads stopped at each step to decide which branch applied.
- Leads ran the verify commands by hand and then `routine run verify` ran them again, about 2 minutes per lead for no record (009, 010). A lead quiet for 20 minutes while building is marked `silent` (012). After the landing rebase, `git push origin HEAD` is refused as non-fast-forward (009). Which sha `done --commit` takes was a guess (015).
- The review instance tries to run `go test`, is refused, and spends turns on it (014).
- The k16 daemon logs `routines: read kinds … cannot change to <path>` every minute for each registered repo with no checkout on k16 (011).
- `scripts/seed.sh` has no stop; `pkill -f` on its command line killed the lead's own shell four times (014, 015, 016).
- 011 landed inert on k16 because it needed a config key the spec never named as an operator need (011).

## Proposal
In:
- `done` prints `done <path> <sha8>` on success; a `done` for the same commit on a node already `done` exits 0 with `already done at <sha8>`. `started` and `progress` print one line. `inbox` with nothing waiting prints `inbox: empty`.
- The version-skew line goes to stderr, once, after the command's own output, and never changes its exit code.
- `routine run <kind>` ends with `<kind> ok|fail t:<tree8>` and whether it was recorded. `result find <kind>` without `--model` lists every model's result for the tree. Each `result` subcommand's usage error names its flags.
- The lead, executor, ideate-engineer and ideate-ux skills condition each pstack step on that skill being in the session's skill list, not on the plugin being installed. The lead skill names `pstack:control-ui` (when listed) for a browser check.
- The lead skill and build brief: commit, then `routine run verify` is the verify (don't run the commands by hand first); publish `progress` at least every 15 minutes while building; don't push the branch after the landing rebase; name exactly which commit `done --commit` takes, checked against what `bmesh done` verifies.
- `review.toml`'s prompt says verify already ran the tests and the reviewer should not try.
- The daemon's rescan skips a registered repo whose path doesn't exist on this box, logging it once per daemon start.
- `scripts/seed.sh` writes a pidfile and takes `stop`.
- The spec template's "Needs from the operator" guide line says a config key some box must set to turn the change on counts, naming the file it lives in.

Out: `bmesh land`, carrying a review across a rebase, Nak-on-shutdown for held jobs, a committed browser-check harness, a shared subject parser and shared test helpers. Each of those needs a design decision and goes to a round.

Judge: opus, because it touches many commands and skills at once and each output line is an interface leads script against.

## What you'll see
Every `bmesh` command that changes state ends with one line saying what it did; a second `done` is calm; the skew warning no longer swallows output; leads no longer report the pstack branch as a doubt; the k16 journal is quiet between real events.

## Verification
The repo's verify. New `cmd/bmesh` tests drive `runCLI` for: `done` twice on the same commit, empty `inbox`, `routine run` on an inline kind ending in its verdict line, `result find` without `--model`, and the skew line on stderr after stdout. A daemon test for a registry row with a missing path. README updated in the same commit for every changed output.

## Risks and the alternative considered
Scripts that parse today's silence break; nothing in this repo or the skills does. An idempotent `done` must still refuse a `done` for a different commit.

## Needs from the operator
None.
