---
name: propose
description: Use when the operator asks for "proposals for X", "run a round on X", "ideas for X", "suggestions for X" or "what would you improve in X" - X a repo in the bmesh registry. Starts a buildermesh proposal round (an engineer and a UI/UX engineer thread) with bmesh propose and reports the round.
---

# Propose

You are the operator's hands on a box with `bmesh`: the chat is the interface, so run the command and say what happened. `bmesh propose` does the checking (a ship row, fewer than five proposed specs, a level `main`, reachable hosts); do not redo it, relay its refusal.

1. **Where it runs.** `bmesh repos show <repo> --json` gives the row; its `host` is where the round runs. When `host` is this box (`hostname -s`, lower case), run it here. Otherwise run it there: `ssh -o BatchMode=yes <host> bash -lc 'bmesh propose <repo> [--model M]'`. When that host can't be reached from here, say so and stop: nothing is started.
2. **The command.** `bmesh propose <repo> [--model <m>]` (`--model` only when the operator named one). It prints one JSON line: the round's `path` and, under `eng` and `ux`, each child's `path`, `thread`, `branch`, `worktree` and `host`.
3. **The Fable fallback.** When the runner rejects the model (an error naming the model, consent or credits), run the same command once more with `--model claude-opus-5-5` and tell the operator what the runner said. Any other failure is reported as printed, never retried.
4. **Report** the round path (`<repo>-round-<N>`), both thread ids with their hosts and the model (say if it fell back to Opus), and, on exit 1, which child did not start and why.

End with one line: the round is started, the ranked pitch arrives as a push (`<repo> round N proposed`) when both threads finish, and the operator's review of it waits on that (`buildermesh:review`).
