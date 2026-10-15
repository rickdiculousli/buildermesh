# buildermesh (bmesh)

A NATS-based mesh for coordinating coding agents: lead and executor threads report events, exchange inbox messages and are woken when they go quiet; routines run a repo's checks (shell commands or headless `claude -p` sessions) against commits and record results by tree, and a lead's `done` is checked against them.

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
- `tmux`: `git worktree add -b <thread> <worktrees>/<repo>/<thread>`, then `claude` in window `<thread>` of session `<tmux_session>` (created when missing; the window gets `BMESH_CONFIG` when the CLI has it, so the lead reports to the same mesh). It watches the window until claude's prompt is up, answering the dialogs a fresh worktree raises (the folder-trust question with yes, the first-run screens with Enter, an offer to try something with Escape), then types the brief in; after 90 s it types anyway. It can't tell whether claude is mid-turn.

`lead start` refuses with exit 3 `bmesh: N leads already working on this host` when `[runner] max_leads` is set and that many leads registered on this host are not `done` or `blocked`. `scripts/memcap.sh N` measures what a box can carry: it runs N leads on the tmux runner against a mesh of its own, each spawning three executors, and prints the peak memory.

## Executors

A lead fans out by making executors: subagents in worktrees of their own, on branches under the lead's, each a node under the lead's path.

```
bmesh executor new --name N [--base REF] [--model M] [--json]
bmesh executor list [--json]
bmesh executor remove <name> [--force]
```

`new` runs as the lead (or as an executor fanning out again): it makes `<[runner] worktrees>/<repo>/<lead>.<name>` with `git worktree add -b <lead branch>-<name>` from the lead's HEAD (`--base` picks another commit), registers node `<lead>.<name>` (`role: executor`, `parent`, branch, worktree, repo, host, model) with an inbox and state `idle`, and prints `{"path","branch","worktree"}` followed, unless `--json`, by the executor header: the paragraph to paste at the top of the subagent's prompt, which names the path, worktree and branch, says to carry `--node <path>` on every `bmesh` command, to run `bmesh started` first and to invoke the `buildermesh:executor` skill. A name matches `^[a-z0-9][a-z0-9-]*$`; a second `new` with a name whose node is not `done` or `blocked`, or whose branch still exists, is refused (exit 3). When `git worktree add` fails, nothing is registered and the branch is removed. The branch must not exist before.

`list` prints the lead's tree (every node under the lead): path, state, branch, head (when the worktree is on this host) and unread count. `remove` refuses (exit 3) while the branch is not merged into the lead's HEAD unless `--force`; it then removes the worktree (`--force` also drops uncommitted changes) and the branch (`-d`, or `-D` with `--force`), forgets the worktree in the node and marks it `done`. The node and its branch name stay in `bm-nodes` for the map; the name can be used again.

An executor's `ask` is sent to its parent's inbox as a message `ask from <path>: <text>` as well as published as its own `ask` event, since only leads reach the operator; the digest lists an executor's ask, silence, block or completion nested under its lead's line and counts only leads under "asking". An executor's `done` needs `--commit` and never reads the spec.

### The skills

The repo is a Claude Code plugin, `buildermesh`, with two skills: `lead` (own one spec, decide the shape, executors, checkpoints, merge, verify, land, ask) and `executor` (your worktree and branch only, checkpoints, routines, report, ask goes to the lead, never merge or push). Install it from the repo:

```
claude plugin marketplace add rickdiculousli/buildermesh && claude plugin install buildermesh@buildermesh
```

or from a checkout: `claude plugin marketplace add ~/repos/buildermesh && claude plugin install buildermesh@buildermesh`. A lead's brief says "follow the `buildermesh:lead` skill" and adds the operator's own conventions; the executor header names the executor skill. The skills name no repo convention; `docs/agents-snippet.md` is the paragraph a repo's AGENTS.md gets.

## Routines

A routine runs one of a repo's checks against a commit: a shell command or a headless `claude -p` session. A kind says how; a job asks for one run; a result records it by tree.

### Kinds

A kind is `.bmesh/routines/<kind>.toml` in the repo (the directory is `[routines] dir` in the repo's `.bmesh/config.toml`). Its name is the file name and matches `^[a-z0-9][a-z0-9-]*$`. Every key, with its default:

| Key | Default | Meaning |
|---|---|---|
| `mode` | `"pooled"` | `inline`, `fresh` or `pooled` (see Modes) |
| `deterministic` | `false` | the same tree always gives the same result: a recorded result answers a repeat job |
| `slots` | `[]` | slot names (from the repo's `[slots]`) a run holds a lease on |
| `timeout` | `"20m"` | a run is killed (its whole process group) after this |
| `command` | `""` | run with `sh -c` in the checkout at the commit; a job's args are appended as `"$@"` |
| `model` | `""` | a model id: the kind runs `claude -p` with it |
| `models` | `[]` | several model ids; a job's `--model` picks one, default the first |
| `prompt` | `""` | model kinds: the setup prompt, sent once per session ahead of the first job |
| `allow` | `[]` | model kinds: extra `--allowedTools` entries, e.g. `"Bash(npm test:*)"`; `Read`, `Grep`, `Glob` and `LS` are always allowed |
| `pool` | `1` | pooled: instances per model; fresh: runs at once |
| `batch_idle` | `"30s"` | pooled: start on held jobs after this long with no new one and no go |
| `recycle_uses` | `20` | pooled model: new session after this many jobs |
| `recycle_context` | `120000` | pooled model: new session when the last reply's context tokens (input + cache read) exceed this |
| `recycle_idle` | `"55m"` | pooled model: new session after this long idle (the prompt-cache TTL) |

Exactly one of `command`, `model` and `models` is set; an unknown key is an error. A command has trailing spaces, newlines and `;` trimmed, and its last line may not end in a comment (the appended `"$@"` would be commented out). A model id may not contain spaces or `;` (it must fit a `verified` entry); `result record`, `routine queue` and `routine run` refuse such a `--model` with exit 2. A recycle key of `0` is off.

A command runs with `BMESH_JOB`, `BMESH_KIND`, `BMESH_COMMIT`, `BMESH_TREE`, `BMESH_FROM` (the path the job runs as: the instance, or the requester for an inline run) and `BMESH_REPLY` (the requester's path) in its environment. A model kind gets the job on stdin as a header line (`job: <id> commit: <sha> tree: <tree>`), the job's `--text` and its args, and runs `claude -p --model M --output-format json --session-id|--resume <uuid> --allowedTools <list> --max-turns 40`. Its run is ok unless the reply's last `VERDICT:` line says otherwise (`VERDICT: ok` or `VERDICT: fail`), or claude errs. Markdown around the line is ignored: leading `*`, `_`, `#`, `` ` ``, `>` and spaces, and the same around the value, so `**VERDICT: fail**`, `**VERDICT:** fail` and `VERDICT: **ok**` all read. When the kind's prompt contains `VERDICT:` (it asks for one), a reply with no verdict line fails; otherwise it passes by claude's exit code. A run's output is stdout and stderr (a command) or the reply text (a model).

`examples/routines/`:

- `verify.toml`: `mode = "inline"`, `deterministic = true`, `command = "npm test"`.
- `review.toml`: `mode = "pooled"`, `models = ["claude-opus-5-5", "claude-fable-5-1"]`, `allow = ["Bash(git diff:*)", "Bash(git log:*)"]` and a prompt that reviews `git diff <base>..HEAD` (the job text carries `base: <sha>`, e.g. `--text "base: $(git merge-base origin/main HEAD)"`) and asks for findings as `### <severity>: <what breaks>` sections ending in `VERDICT: ok` or `VERDICT: fail`.
- `surface.toml`: `mode = "fresh"`, `deterministic = true`, `slots = ["browser"]`, `command = "npx playwright screenshot"`; the job args `<url> <file>` are appended as `"$@"`.

### Modes

| Mode | Runs | Where |
|---|---|---|
| `inline` | `bmesh routine run` in the requester's own process; no daemon | the requester's worktree, at HEAD |
| `fresh` | the daemon starts a new instance per job, `routine.<kind>.f<id8>`, and removes its checkout afterwards | a checkout of its own |
| `pooled` | the daemon keeps `pool` instances per model, `routine.<kind>.<model-short>-<n>` (`cmd-<n>` for a command), that drain the kind's inbox in batches | one checkout per instance, kept |

`id8` is the job id's first 8 characters, lowercased. `model-short` is the model id lowercased with a leading `claude-` stripped, every character outside `[a-z0-9]` made `-`, runs of `-` collapsed to one and leading and trailing `-` trimmed (`claude-opus-5-5` is `opus-5-5`, `claude-opus-5-5[1m]` is `opus-5-5-1m`). Pooled instances are numbered from 1 per model across every repo that defines the kind.

A pooled instance holds the jobs it takes until `batch_idle` passes with nothing new, or a go arrives (`bmesh routine go <kind>`, or a job queued with `--go`), then runs them in order and runs any job queued meanwhile at once. A model instance keeps one session across jobs (`--session-id` on its first job, `--resume` after) and starts a new one when `recycle_uses`, `recycle_context` or `recycle_idle` is reached, or when the kind's definition at a job's commit differs from the one its session started with. A run whose output does not parse as claude's JSON result (including a non-zero exit with no JSON), first or resumed, drops its session whatever the recycle settings: the next job starts a new one with the prompt.

Instances are nodes (`role: routine`, with repo, host and model). The daemon writes their state (`working` during a job, `idle` between, `done` when it stops: a fresh run after its job, a pooled one when its pool is stopped or restarted) and publishes `started`, `progress` (`job <id> started` before a run and `job <id> ok|fail <ms>` after it, both with `to` = the requester's path; or `recycled`) and `done` for them on `bm.<instance path>.<event>`. A node event with a `to` counts as activity of that path too, so a lead waiting on a routine is not marked silent.

The daemon reads each `[repos]` repo's kinds every minute from its main (`origin/main` after a fetch when the repo has an `origin`, else `main`) and starts, restarts or stops pools to match. Routines are off when `[runner] checkouts` is empty.

### Jobs

```
bmesh routine list [--json]
bmesh routine status [<kind>] [--json]
bmesh routine queue <kind> [--commit C] [--model M] [--go] [--rerun] [--text T] [-- args...]
bmesh routine go <kind>
bmesh routine wait <job id> [--timeout D]
bmesh routine run <kind> [--commit HEAD] [--model M] [--here] [--rerun] [--text T] [-- args...]
```

`list` prints the cwd repo's kinds with their instances and queue depth (waiting plus in-flight jobs); `status` prints a kind's (or every kind's) instances with state and model.

A job is an inbox message (`kind: "job"`) to `routine.<kind>`, with the job id as its `Nats-Msg-Id`, so a job queued twice is stored once (`queue` prints `duplicate <id>`). It names a commit (`--commit`, default `HEAD`, resolved in the cwd repo to a full SHA with its tree) and a repo: the requester node's `repo` when it is a registered node naming one; else the `[repos]` name whose path shares the cwd's git common dir (so a linked worktree, where leads live, names its main repo); else the main repo directory's base name. The daemon resolves it through its own `[repos]`. The kind's inbox consumer is `inbox-routine--<kind>`, with `MaxAckPending` = the kind's instance count (`pool` per model per repo for pooled, `pool` for fresh) and `AckWait` = the kind's `timeout` + 1m. The daemon keeps a running job in progress, so a job is redelivered only when its daemon dies.

The daemon runs a job with the kind as defined at the job's commit (`git show <commit>:.bmesh/routines/<kind>.toml`), falling back to main's definition only when the file is absent at that commit. A deterministic kind's job is answered from its recorded result, ok or failed, without a run unless `--rerun`; `queue` checks the record first and prints `answered from result <key> (job <id>)`. A job whose commit is not in the daemon's clone (queued from another box) gets one `git fetch origin` before it is answered `not a commit`, so push the branch before queueing to a daemon elsewhere. A job whose slot is busy goes back to the inbox for 30 s. An unknown repo, a kind that a configured repo defines but not the job's repo's main, an inline kind, an unknown model and a lost lease are answered `ok=false` with that reason. A kind that no configured repo defines on its main has no feed: its jobs wait unanswered until `routine wait` times out.

A reply is an inbox message (`kind: "reply"`) to the requester carrying `{"job","kind","tree","ok","output","result_key","by"}`; the output is cut at 64 KiB with a trailing `[truncated]` line. `wait` reads only that reply from this node's inbox, leaving other messages unread, prints its output and exits 0 when ok, 1 when failed, 5 on `--timeout` (default 20m).

`queue`, `go` and `wait` need a node identity (`--node`, `BMESH_NODE` or the registered worktree): the reply is addressed to it. `run`, `lease` and `result record` use the node when one resolves and otherwise act as the host name; `list`, `status` and `result find` need none.

### Checkouts

Fresh and pooled runs never touch a requester's worktree. Each instance has a detached worktree of the `[repos]` path at `<[runner] checkouts>/<repo directory name>/<instance path>`, moved before each job with `git checkout --detach <commit> && git clean -fd`; the daemon reads the repo's main in a sibling checkout named `main`.

`routine run` is the inline run: it runs HEAD of the cwd worktree (`--commit` must name HEAD) and refuses a fresh or pooled kind unless `--here`. Every inline run records a result for HEAD's tree, so it refuses a dirty tree: exit 3 `bmesh: worktree is dirty; commit first`.

### Leases

```
bmesh lease acquire <slot> [--ttl D]
bmesh lease renew <key> [--ttl D]
bmesh lease release <key>
bmesh lease run <slot> [--ttl D] -- <cmd...>
```

A slot is a resource only so many runs may use at once. Its capacity is in the repo's `.bmesh/config.toml` (default 1):

```toml
[slots]
browser = 1
[routines]
dir = ".bmesh/routines"
```

Leases live in KV `bm-leases`, key `<slot>.<n>` (`n` below the capacity), value `{"holder","expires_at"}`; an expired entry is free. `--ttl` defaults to 10m and is at least 3 s. `acquire` prints the key (exit 3 when the slot is busy); `renew` and `release` refuse a lease held by someone else (exit 3). A holder renews every ttl/3; when a renew fails the lease is lost: the run is killed and the job answered `ok=false` with `lease lost` (nothing is recorded; `lease run` exits 1). `lease run` runs the command in its own process group, kills the group and releases on SIGINT or SIGTERM, and exits with the command's code. Kinds take leases on their `slots` for `timeout` + 1m (inline runs and the daemon alike).

### Results

```
bmesh result find <kind> [--commit C] [--model M] [--json] [-- args...]
bmesh result record <kind> --ok|--fail [--commit C] [--model M] [--output FILE|-] [-- args...]
```

Results live in KV `bm-results`, key `<kind>.<tree>[.<args8>][.<model-short>]`: `tree` is the commit's full tree hash, `args8` the first 8 hex of the sha256 of the args joined by NUL (only when there are args), `model-short` as in Modes (only for a model). The record:

```json
{"kind":"verify","tree":"<tree>","commit":"<sha>","args":[],"ok":true,"output":"...","at":"2026-01-02T03:04:05Z","by":"app-012","model":"claude-opus-5-5"}
```

`model` is omitted for a command kind; `output` is cut at 64 KiB. Every run that names a commit records: inline runs and daemon runs, deterministic or not (a kind's newest run at a tree replaces the last). Answered-from-record jobs, refused jobs, lost leases and runs cut off by a stopping daemon record nothing. `result record` stores a result by hand for any kind name, with or without a kind file; `--commit` defaults to `HEAD`, and a result for HEAD is refused while the worktree is dirty (exit 3 `bmesh: worktree is dirty; commit first`). `result find` prints `<ok|fail> <tree> <at> <by>` (exit 3 when none).

### `verified`

A spec's frontmatter carries one line of proof, `;`-separated, one entry per check:

```
verified: verify ok t:3f2a9c1e; review ok t:3f2a9c1e m:claude-opus-5-5
```

Each entry is `<kind> <ok|fail> t:<tree first 8>[ m:<model>]`.

`bmesh spec verified [--spec NNN] [--write]` composes the line from the results recorded for the tree of HEAD in the cwd worktree: one entry per (kind, model), the newest result winning, whatever its args, sorted by kind then model. It prints `verified: ...`, or exits 3 with `bmesh: no results for HEAD's tree; run the routines first`. `--write` sets the line in the spec's file in this worktree (found by `[spec] glob`; the spec is `--spec`, else this node's), replacing a `verified:` line or adding one before the closing `---`; a line that would not read back as the same entries is not written.

`bmesh done` refuses unless the spec reads `done` on main with a `merged` commit that main contains (`git merge-base --is-ancestor <merged> <ref>`, `ref` = `origin/main` when the repo has an origin, else `main`): exit 3 `bmesh: not done: merged <sha> is not on <ref>`. For a lead it further requires a non-empty `verified` line with no `fail` entry, whose every entry is for the tree of `merged` and is matched by a record of that kind at that tree with the same `ok` (and the same model when the entry names one), whatever its args. It refuses with exit 3:

- `bmesh: not done: verified is empty`
- `bmesh: not done: verified has a failing entry <kind>`
- `bmesh: not done: verified entry <kind> is for tree <t>, merged tree is <m>`
- `bmesh: not done: verified names <kind> but no result for tree <t>`

An executor's `done` skips the spec check.

## Names

Paths match `^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)*$`; the first token is never `inbox` or `daemon` (`routine` is for the routine pool: `routine.<kind>` and `routine.<kind>.<instance>`), and no token contains `--` (the inbox consumer name maps `.` to `--`, so `a--b` and `a.b` would collide).

| Kind | Name | Subjects / keys |
|---|---|---|
| stream | `bm-mesh` | `bm.>` (30 days, file store, 2m duplicate window) |
| event subject | | `bm.<path>.<event>` |
| inbox subject | | `bm.inbox.<path>` |
| daemon subject | | `bm.daemon.<host>.<event>` |
| consumer per inbox | `inbox-<path, "." as "--">` | filter `bm.inbox.<path>` |
| kind inbox subject | | `bm.inbox.routine.<kind>` (jobs and gos) |
| consumer per kind | `inbox-routine--<kind>` | filter `bm.inbox.routine.<kind>`; MaxAckPending = instances, AckWait = timeout + 1m |
| routine event subject | | `bm.routine.<kind>.<instance>.<event>` (`started`, `progress`, `done`) |
| KV | `bm-nodes` | `<path>` to Node |
| KV | `bm-state` | `<path>` to State |
| KV | `bm-runner` | `<path>` to RunnerSample |
| KV | `bm-results` | `<kind>.<tree>[.<args8>][.<model-short>]` to Result |
| KV | `bm-leases` | `<slot>.<n>` to `{"holder","expires_at"}` |

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
worktrees = "~/.bmesh/worktrees"            # executors' worktrees, and the tmux runner's
checkouts = "~/.bmesh/checkouts"            # routine instances' checkouts; "" = routines off
claude = "claude"                           # claude command for model routines, split on spaces
max_leads = 0                               # leads at once on this host (lead start refuses past it); 0 = no cap
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
| 1 | error; a failed run (`routine run`, `routine wait`); a lost lease; an interrupted `lease run` |
| 2 | usage |
| 3 | refused (a `done` the spec or its `verified` line doesn't support, a reserved or taken path, a send to a running thread, an executor name or branch in use, an `executor remove` of an unmerged branch, a dirty worktree for a deterministic inline run, an unknown kind, a fresh or pooled kind run inline without `--here`, an inline run of a commit other than HEAD, a busy slot, a lease held by someone else, no result for `result find` or `spec verified`) |
| 5 | timeout (`watch`, `routine wait`) |

`lease run` exits with its command's code.

## License

MIT.
