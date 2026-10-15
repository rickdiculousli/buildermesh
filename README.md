# buildermesh (bmesh)

A NATS-based mesh for coordinating coding agents: lead and executor threads report events, exchange inbox messages and are woken when they go quiet.

## Build and test

```
GOBIN=~/.local/bin go install ./cmd/bmesh
TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test ./...
```

Tests never use `/tmp`: `meshtest.Start` fails when `TMPDIR` is `/tmp`.

### Testing

Runner tests drive shell fakes instead of T3 and tmux. `BMESH_T3_COMMAND`, when set, is split on spaces and replaces `uv run <t3_dispatch>` (tests set it to `sh <repo>/testdata/fake-t3.sh`); `BMESH_TMUX_COMMAND` replaces the tmux binary the same way (`sh <repo>/testdata/fake-tmux.sh`). `BMESH_T3_TIMEOUT` (a Go duration) replaces the t3 runner's 120 s per-call timeout. `BMESH_CLAUDE_COMMAND` replaces `[runner] claude` the same way (`sh <repo>/testdata/fake-claude.sh`). Leave all four unset outside tests.

## Runners

`bmesh lead start --repo R --spec NNN --brief FILE [--model M] [--title T] [--path P]` starts a lead through `[runner] kind` and registers it as node `<repo>-<NNN>` (state `idle`), printing `{"path","thread","branch","worktree"}`. A path whose node is not `done` or `blocked` is refused (exit 3); a `done` or `blocked` path is reused and its state reset to `idle`. The runner's start runs under the runner's own timeout, not the CLI's one-minute deadline; if it times out, or registration fails after it, the error names the thread id (and branch and worktree when known) so the session can be found.

- `t3`: `uv run <t3_dispatch> start|status|send|stop`, 120 s per call. `send` runs `status` first and never sends to a running thread; no `--force`, `--queue` or `wait-idle`.
- `tmux`: `git worktree add -b <thread> <worktrees>/<repo>/<thread>`, then `claude` in window `<thread>` of session `<tmux_session>` (created when missing), with the brief typed in. It can't tell whether claude is mid-turn.

## Names

Paths match `^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)*$`; the first token is never `inbox` or `daemon` (`routine` is for the routine pool: `routine.<kind>` and `routine.<kind>.<instance>`), and no token contains `--` (the inbox consumer name maps `.` to `--`, so `a--b` and `a.b` would collide).

| Kind | Name | Subjects / keys |
|---|---|---|
| stream | `bm-mesh` | `bm.>` (30 days, file store, 2m duplicate window) |
| event subject | | `bm.<path>.<event>` |
| inbox subject | | `bm.inbox.<path>` |
| daemon subject | | `bm.daemon.<host>.<event>` |
| consumer per inbox | `inbox-<path, "." as "--">` | filter `bm.inbox.<path>` |
| KV | `bm-nodes` | `<path>` to Node |
| KV | `bm-state` | `<path>` to State |
| KV | `bm-runner` | `<path>` to RunnerSample |

JetStream is addressed through the domain from `[mesh] domain` (default `hub`).

## Config

`~/.config/bmesh/config.toml` (`BMESH_CONFIG` overrides the path). Keys and defaults:

```toml
[mesh]
nats = ""                                   # "" = embedded server; else e.g. "nats://localhost:4222"
domain = "hub"
token_file = ""                             # "" = no token
store = "~/.local/state/bmesh/jetstream"    # embedded server's store
listen = "127.0.0.1:4222"                   # embedded server's bind
[host]
name = ""                                   # "" = `hostname -s`
[serve]
listen = "127.0.0.1:3774"
[runner]
kind = "t3"                                 # t3 | tmux
t3_dispatch = "t3_dispatch.py"              # path; run as `uv run <path>`
tmux_session = "bmesh"
worktrees = "~/.bmesh/worktrees"            # tmux runner's worktree root
checkouts = "~/.bmesh/checkouts"            # routine instances' checkouts
claude = "claude"                           # claude command for model routines, split on spaces
[notify]
ntfy = ""                                   # "" = no digest; else "http://k16:2586/bmesh"
command = ""                                # test hook: receives the POST body on stdin instead of HTTP
digest_every = "1h"                         # command runs via `sh -c`, body on stdin, title in $BMESH_TITLE
[liveness]
grace = "20m"
poll_every = "60s"
wake_every = "10m"
[repos]                                     # name = path
[spec]
glob = "docs/proposals/{id}-*.md"
[models]
lead = "claude-fable-5-1"
```

## Exit codes

| Code | Meaning |
|---|---|
| 0 | ok |
| 1 | error |
| 2 | usage |
| 3 | refused (a `done` the spec doesn't support, a reserved or taken path, a send to a running thread) |

## License

MIT.
