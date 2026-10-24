# Build {{spec_path}} in {{repo}}

You are building one spec the operator picked in `{{repo}}` (`{{repo_path}}`), dispatched {{date}}. You are in a fresh worktree of `{{repo_path}}` on your own branch. Nothing about this build is recorded on main: your node on the mesh says you are building the spec, which reads there as it did when it was picked (`proposed`, say). The spec is yours to set `done` on your own branch, so it lands with the code: before you land, run `bmesh spec set --id {{spec_id}} --status done` in your worktree and commit it (land refuses a spec that doesn't read `done`). A block is reported with `bmesh blocked`, never recorded in git.

## The spec, `{{spec_path}}`

----- begin spec -----
{{spec}}
----- end spec -----

{{operator_words}}## Read first

1. `AGENTS.md`. Its "Definition of done" section governs this work; where it and this brief differ, it wins. The repo's registry row says: {{done_rule}}
2. The other specs in `docs/proposals/` that touch the same area, so your change stays consistent with them.

## How to work

- You are a buildermesh lead; follow the `buildermesh:lead` skill (installed as a plugin on this box). Your node is `{{node}}`. The skill owns the shape of the work: your own hands or executors, checkpoints on `bmesh inbox`, merging executors' branches, verifying through routines and the `verified` line. This brief adds what is this repo's.
- Build what the Proposal section puts In, and nothing it puts Out.{{upgrading}}
- Verify on committed work. When the repo has a `verify` kind (`bmesh routine list` shows it), commit, then run `bmesh --node {{node}} routine run verify`: that run is the verify, so don't run the commands by hand first; it ends with `verify ok|fail t:<tree8> (recorded)`. Otherwise run the repo's verify commands yourself, {{verify}}, and record the outcome with `bmesh --node {{node}} result record verify --ok` (or `--fail`). Add what the spec's Verification section says is needed.
- The review is sized by the spec's `size`: an XS or S change lands on verify alone, and your final message says so; an M gets one review on Opus at medium effort, an L or larger one at high effort. A change is sized as built: one whose scope grew (a `scope_grew` entry, or a change that crossed modules once built) is reviewed at the size it grew to. For a review, the daemon fetches the commit it reviews, so push your branch first (`git push origin HEAD`), then `bmesh --node {{node}} routine queue review --go --effort <medium|high> --text "base: $(git merge-base origin/main HEAD)"` prints `queued <job id>` and `bmesh --node {{node}} routine wait <job id>` prints the findings and records the result. In a repo without a `review` kind, an Opus subagent with fresh context, at the same effort, gets the spec, the diff (`git diff main...HEAD`) and the verify output, and looks for where the change falls short of the spec or breaks something; record its verdict with `bmesh --node {{node}} result record review --ok|--fail --model <m>`, `<m>` the reviewer's model. One pass: a review's findings are fixed and then landed, the second pass only after a rebase changed the code (`routine queue review` refuses a second job for a reviewed branch without `--again`), and a taste finding is noted in the final message, not re-reviewed. After the final rebase verify re-runs, and the review re-runs only when the rebase changed the code.

## Report on the mesh

Your node is `{{node}}`. The coordinator that dispatched you is not reading your console; it is woken by the events you publish on the mesh with `bmesh`; `--node {{node}}` names your node in every command. Publish these, each as one command, and keep working:

- Now, before reading further: `bmesh --node {{node}} started --text "<one line: what you will build>"`.
- When you stop for anything only the operator can decide: `bmesh --node {{node}} ask --text "<the question>"`. When you are blocked: `bmesh --node {{node}} blocked --text "<the reason>"`.
- After a milestone (verify green, review done), and while you build at least one every 15 minutes so you aren't marked silent: `bmesh --node {{node}} progress --text "<one line>"`; never more than one every ten minutes.
- As the last thing after the push in Landing it: `bmesh --node {{node}} done --commit <sha> --text "<one line of what landed>"`, `<sha>` the commit `bmesh land` printed as `landed` (`git rev-parse HEAD` in your worktree: your last commit, reworded to end with the trailers `Spec: {{spec_id}}` and `Verified: <entries>`). It prints `done <path> <sha8>`; a second run with the same commit prints `already done at <sha8>`. bmesh done checks that commit and refuses until it is on origin/main, with a `Verified` trailer that has no failing entry and every entry matched by a result recorded for the commit's tree, and a `Spec` trailer naming spec {{spec_id}}; nothing else counts as finished.

Run `bmesh --node {{node}} inbox` when you finish a milestone and before you ask the operator anything. If `bmesh done` refuses, the commit isn't on `origin/main` yet, or its trailers are missing, name another spec, or name a failing entry or a result that isn't recorded for its tree: finish Landing it (fix what failed, re-run what is missing on that tree), then run it again. If a `bmesh` command fails for any other reason (the mesh unreachable, say), say so in your final message and carry on; it never blocks the work.

## Landing it

1. Commit your work. Then hand the landing to your box's daemon: `bmesh --node {{node}} ready --text "<your done line>"`, and end your turn. The daemon runs the `land` below and the `done` above for you. It wakes you only when land refuses (the wake carries the refusal: act on it as steps 2 and 3 say, commit, then `ready` again), and once at the end for your closing line. Where `ready` refuses, or to land in your own turn, from your worktree run `bmesh --node {{node}} land`, in the background with a timeout 15 minutes above its `--wait` (45 minutes at the default 30). It refuses at once while spec {{spec_id}} in your worktree doesn't read `done`. It runs verify, queues the review your spec's size calls for when the branch has none (`--size <XS|S|M|L>` when the scope grew past the spec's) and waits for it, prints a failing review's findings and stops there. It waits while `main` in `{{repo_path}}` has commits `origin` doesn't (someone's unpushed work: never push or reset it yourself) and while another lead lands (the lease `land.<repo>`). Then it fetches, rebases onto `origin/main`, runs verify on the rebased tree and requires the review's result there (one from before the rebase carries onto the line, as `c:<the tree it read>`, when the rebase left your diff unchanged, and is queued again by land when the rebase changed it), composes the verified line and rewords your last commit so its message ends with the trailers `Spec: {{spec_id}}` and `Verified: <entries>` (no file changes, so the tree the results are recorded for is the tree that lands), pushes `main` and fast-forwards `main` in `{{repo_path}}`; when main moved before the push it takes the reword back and starts again from the fetch. It prints a line per step and `landed <sha8>`. While it runs, leave the worktree alone. Don't push your branch after it; never check out, switch, reset or stash in `{{repo_path}}`.
2. If it stops on a conflict (the rebase is aborted, nothing pushed), resolve it; don't report it. Rebase onto `origin/main` yourself, read your spec and every spec that landed on main since you branched (`git log --oneline $(git merge-base HEAD origin/main)..origin/main -- docs/proposals/`) and rework the change so every intent holds. If a landed spec changes what yours must look like, widen your spec: add the landed ID to `scope_grew` and a "Scope grew" line under Proposal naming it, do that work, then re-validate (verify, and the review for the new part). Done means consistent with everything on main. Commit and run `bmesh land` again.
3. If verify or the review fails, fix it, commit and run `bmesh land` again (a fixed review lands on verify alone); if it timed out (exit 5), run it again.

Stop only when the widened scope can't be met from the specs alone. Then report it with `bmesh --node {{node}} blocked --text "<one sentence>"`; nothing is recorded in git.

## Final message

What was built, the verify results, what the reviewer found and how it was fixed, and any scope that grew. End with the closing below; its last line names the commit on main.

## Closing

{{standard_closing}}
