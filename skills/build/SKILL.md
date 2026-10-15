---
name: build
description: Use when the operator says "build X 3", "build 004", "ship 004", "start X 3", "do what's accepted in X" or "build what's accepted in X" - X a repo in the bmesh registry, the number a spec id. Dispatches the accepted spec with bmesh build, watches the lead with bmesh watch, relays its asks and reports the thread.
---

# Build

`bmesh build` does the checking (a ship row, a spec that reads `accepted`, a free node path, the host's lead cap) and records `building` on `main` before the lead starts; relay its refusal rather than redo it.

1. **Where it runs.** `bmesh repos show <repo> --json` gives the row; `host` is where the lead runs. On this box run the command here; on another box run it as `ssh -o BatchMode=yes <host> "bash -lc '<command>'"` (ssh joins its arguments into one remote command line, so the inner command is quoted twice). When that host can't be reached from here, say so and stop.
2. **The command.** One spec: `bmesh build <repo> <NNN> [--model <m>]` (a rebuild of a path whose node is still live: `--path <repo>-<NNN>-2`, as the refusal says). Everything accepted: `bmesh build --accepted <repo> [--model <m>]`, which prints `started`, `waits` or `not started` per spec. The JSON line carries the lead's `path`, `thread`, `branch`, `worktree`, `model` and the `building` commit.
3. **The Fable fallback.** When the runner rejects the model (an error naming the model, consent or credits; the spec is back to `accepted`, `Dispatch NNN: did not start`), run that one spec once more as `bmesh build <repo> <NNN> --model claude-opus-5-5` and tell the operator what the runner said. After `--accepted`, only the rejected specs are rerun, one by one: a `--model` on `--accepted` would override every other spec's judge. Any other failure is reported as printed, never retried.
4. **Watch the leads.** `bmesh watch --nodes <path>[,<path>...] --timeout 30m` prints one line per event (`<at> <path> <event> <text>`) and `watch-done` once every node is `done`, `blocked` or `silent`; exit 5 is the timeout, re-arm it with `--since <the last line's time>`. Where the harness has a Monitor, arm it on the watch and let it wake you; otherwise run the watch in the background and read its output when it exits. Never poll the runner (T3, tmux, the session files) in a loop.
5. **Relay asks.** An `ask` line is a question only the operator can answer: put it to the operator in their words, and send the answer back with `bmesh --node operator send <path> "<answer>"`. A `blocked` line is reported with its reason; `done` with its commit.
6. **Report** the started threads (path, thread id, host, model; say if it fell back to Opus), and what `--accepted` left waiting.

End with one line: what is started (or landed, when the watch saw `done`), and what waits on the operator: an unanswered ask, a block, or nothing but the push.
