# bmesh internals

How the mesh works inside: the NATS subjects, consumers and buckets bmesh uses, the record shapes it stores, how it signs messages between boxes, and how its daemons agree on who does what. The [manual](manual.md) covers everything you run, configure and see; each section here starts with a link to the manual section it belongs to.

## Quickstart

[Manual: `bmesh init`](manual.md#bmesh-init)

**The units `init --service` writes.** The systemd units carry `RestartPreventExitStatus=78`, so a refused config (exit 78) is not restarted. launchd has no per-code stop, so serve's launchd agent carries `ThrottleInterval` 300 instead.

**The embedded hub.** With `[mesh] nats = ""`, every command but `serve` dials `nats://<listen>` from `[mesh] listen`, a host of `0.0.0.0`, `::` or none read as `127.0.0.1`, in JetStream domain `hub`, with the same token handling as a configured `nats`.

**The supervisor.** `bmesh serve --restart` runs `bmesh serve` as a child from the binary path it read at start, with its own arguments less `--restart` and `--detach` (a `--config` given before `serve` is passed on) and `BMESH_SUPERVISED=1` in the environment. The restart delay is 5 seconds, as the unit's `RestartSec=5`. On Ctrl-C or SIGTERM it stops the child by SIGTERM on Unix and kills it after 30 s; on Windows it kills it. On Windows the child gets its own console with no window, and `--detach` starts the supervisor again with `CREATE_NO_WINDOW`, outside the starting process's job where the job allows it.

**The Windows task command.** Each path is quoted for `schtasks`, so one with spaces passes. `cmd` has no escape for the `\"` that quoting takes, so to `cmd` each path stands outside quotes; a path holding a character `cmd` reads there is refused (see [`bmesh init`](manual.md#bmesh-init)).

### Updates

[Manual: Updates](manual.md#updates)

**Resolving a release.** A binary installed as `go install <module>/cmd/bmesh@vX.Y.Z` names a version and no commit. Every update path resolves that tag to its commit in the repo it reads (fetched with its `v*` tags) before comparing. Revisions compare by prefix, in the hint and in every update below, so a module install's 12 hex match a full commit.

**Where `bmesh version` reads the commit.** From the first of:

1. the binary's build info;
2. the revision and commit time a self-update stamped into it;
3. the main module's pseudo-version, as `go install …/cmd/bmesh@latest` builds: `v0.0.0-20261016043719-ab7e0b543677` prints `bmesh ab7e0b54 2026-10-16T04:37:19Z`, the 12-hex suffix the revision, the timestamp the time.

`(devel)` names no commit. The release a self-update stamps is `git describe --tags --long --match 'v[0-9]*.[0-9]*.[0-9]*' --exclude '*-*' --exclude '*+*'` of the built commit (its nearest `vX.Y.Z` tag, pre-releases passed over) and goes with that commit only: a vcs revision that is another commit prints no release.

**The `started` event.** `serve` publishes `bm.daemon.<host>.started` on every start, `text` the full revision (`unknown`), `body` its commit time (RFC 3339; empty when unknown). The restart line and the hint read it:

- `bmesh update` prints its restart line when it reaches the hub and this box's daemon's newest `started` names another revision than the binary now at the path; with no `started` for this box, no line.
- Every command that connects to the hub reads this box's daemon's newest `started` once, and the hub's newest `bm.daemon.*.started` only when this box has none (it runs no daemon, so a Mac daemon that doesn't self-update never tells another box's leads to update). The restart variant of the hint comes when the daemon's commit is older than the binary's by the commit time `started` carries against the one the binary is stamped with. Another box's daemon, a newer one, or a commit time missing on either side (a `started` from a daemon older than this rule has none) keeps `run bmesh update`.

**The daemon's self-update.** On each kind rescan (once a minute) the daemon compares the target on that repo's main checkout, `<[runner] checkouts>/<repo directory name>/main` at `origin/main` (below, the daemon's main checkout), with the binary at its path: that binary's `version`, so a newer binary `bmesh update --latest` swapped in is never built over; when `version` names no commit, its own revision or its release tag resolved there. When the target contains that revision and is ahead:

1. It builds `./cmd/bmesh` at the target, the checkout moved there and back after, with `go build -ldflags "-X <module>/internal/selfupdate.stampRevision=<commit> -X <module>/internal/selfupdate.stampTime=<commit time, RFC 3339 UTC> [-X <module>/internal/selfupdate.stampRelease=<vX.Y.Z[+N]>]" -o <temporary file beside the binary>`, since go writes no VCS stamp in a git worktree; the release stamp only when a tag is in the commit's history. Under a systemd unit the build runs in a transient `systemd-run --user --scope`, so it doesn't share the unit's `MemoryMax`; inline otherwise.
2. After the swap it publishes `bm.daemon.<host>.update`, `text` `<old> -> <new>` (`v0.1.0 -> v0.2.0`; `commit` the new revision).
3. The drain: every kind's feed stops taking jobs, every running job's process group is killed, and every job it held or ran goes back to the inbox (a NAK) for the restarted daemon.
4. It stops serve as on SIGTERM (map, daemon, client, embedded hub) and re-execs the binary with the same arguments: `exec` on Unix, so the service manager sees the same process; on Windows, which has no exec, it exits 0 and `serve --restart` starts the new binary 5 seconds later.

A crashed update is found at start: a daemon whose newest `update` has no `started` after it and named another revision than its own pushes `bmesh update failed on <host>` before publishing `started`.

On Windows a running binary can be renamed, not removed, so the swap works as on Unix; a `bmesh.prev` that can't be removed (the supervisor's own image, from the update before) goes to `bmesh.prev.old` first. The supervisor check is `BMESH_SUPERVISED=1` in the environment.

**Where `bmesh update` builds.** It fetches the repo's main into the daemon's main checkout when `[runner] checkouts` is set, else a clone (of the row's `origin`, else of its path) under `<user cache dir>/bmesh/update/<R>`. The daemon and `update` each hold the advisory lock `<checkout>.lock` while moving that checkout and building from it, so neither builds from a tree the other is moving; on Windows the lock does nothing.

With no repo row:

1. It fetches `refs/heads/main` and the `v*` tags of `https://<module path>` into the bare repo `<user cache dir>/bmesh/update/upstream` (under the same lock).
2. It runs `GOBIN=<temporary dir beside the binary> go install -ldflags "<the same -X stamp of the target's commit and its time>" <module>/cmd/bmesh@<the release tag, or the code head's commit>` (scoped as the daemon's build; stamped because a tagged build names no revision).
3. It refuses or swaps in the result as the daemon does, keeping `bmesh.prev`.

**The notice's check** runs in the routine loop (so it needs `[runner] checkouts`):

1. It reads the target from `[update] repo`'s main checkout when that row is set and checked out here, else from a fetch of the module's `main` and tags into `<user cache dir>/bmesh/update/upstream`, as `bmesh update` does.
2. It compares the target with the binary at its own path as the self-update does (so a current binary `bmesh update` swapped in reads as current).
3. When the target contains that revision and is ahead, it writes `<state dir>/update.txt` through a temporary file and a rename.

The count is the commits in `<own>..<code head>` that change the repo's code (the `code` of the row `bmesh update` reads, so both find one code head). The specs are those of the `NNN: done at <sha8>` subjects in `<own>..main` whose merged commit `<sha8>` the target has and the binary hasn't (or the repo doesn't know), oldest first, each with the `title` of `docs/proposals/NNN-*.md` on `main`.

An upstream install (`[update] upstream = true`) publishes `update`, drains and re-execs exactly as the self-update from a row does.

## Upgrading

[Manual: Upgrading](manual.md#upgrading)

**Which releases a walk covers** is read from the running binary's revision, not guessed: the release tags in the target's history that its own history lacks.

**How the new binary walks.** Once built, before the swap, `update` runs `<new> update --walk --before-swap --from <running revision> --to <target> --dir <main's repo>`, which checks the config and answers the asks. After the swap, the new binary runs `update --walk` for the steps. `<state dir>/upgrade.json` holds the walk (its ends and the steps done), so a rerun resumes after the last step done. A finished walk writes `<state dir>/installed`.

**The record.** `bmesh init` and every finished walk write `<state dir>/installed`, `<version> <revision> <time>` (the version this binary's release or `<rev8>`): a record, not the source of truth. A binary swapped in without a walk finds notes pending when its release is past the record's. The line pushed for pending notes is kept in `<state dir>/pending-pushed`. The `update` that walks them starts from the record's revision, else from `<state dir>/previous` (the revision of the binary a swap replaced, written by `update` and the daemon), else from v0.1.0; finding the binary current with nothing pending and no record, it writes one.

**The daemon's check before a swap** is the new binary's `config check` and `update --plan --json --from <running> --to <target> --dir <main's checkout>`. A walk with nothing to do writes `installed` for the new binary.

## Several boxes

[Manual: Several boxes](manual.md#several-boxes)

**Stale hub detection.** `bmesh hub` and `bmesh serve` (the daemon, not the `--restart` supervisor) write `<state dir>/hub.json` and `<state dir>/serve.json` at start, `{"pid","revision","release","time"}` of their binary, removed on a clean stop. The hub is stale when its pid is alive and its binary is a different, older commit than serve's (by commit time when both have one, else any difference).

**The hub server.** JetStream runs in domain `[mesh] domain` (default `hub`), stored in `[hub] store` (default `<state dir>/hub/jetstream`). A leaf port takes a user and password, not a bare token, which is why leaves connect as user `leaf`. On SIGTERM or Ctrl-C the server stops after JetStream flushes.

**The leaf** is a server without JetStream that dials `remote`, redialing every second while the link is down; JetStream requests cross to the hub's domain.

**The token file** is made as 24 random bytes in hex.

**Wake detection.** The daemon ticks each minute; two ticks 3 minutes or more apart are a wake.

## Runners

[Manual: Runners](manual.md#runners)

Each runner implements `start`, `status`, `send` and `stop` behind one interface. The `t3` runner's `send` reads `status` first and returns `ErrRunning` mid-turn, so nothing is sent; the daemon's wake treats that as "not delivered" and tries again on its next poll. `claude` queues the send; `tmux` sends regardless.

#### `t3`

**Protocol.** Each call reads `<t3_server>/.well-known/t3/environment` (a `2` is trusted for a minute) and needs `orchestrationProtocolVersion` 2. A 1 (T3 before 0.1.0) is refused at start, status, send and stop; another version, with an error naming it.

**Calls** are bounded at 120 s and use T3's MCP tools at `<t3_server>/mcp` (streamable HTTP, JSON-RPC, the `mcp-session-id` header, re-initialized once when T3 drops the session), with the sign-in's token.

**`start`.**

1. Finds the T3 project whose `workspaceRoot` is the repo (`t3_project_list`) or registers it (`t3_project_create`), which needs a full-access sign-in.
2. Calls `t3_thread_launch` with the title, the brief as first message, `runtimeMode` `t3_runtime_mode`, model `{provider: "claudeAgent", model, options: {effort}}` and workspace `{type: "worktree", baseRef: <base or main>, branch: "bmesh/<thread id>"}` or, for a checkout bmesh made, `{type: "existing_worktree", worktreePath, branch}`.
3. T3 names the thread (`mcp:<uuid>`) and the worktree (`<T3 home>/worktrees/<repo>/bmesh-<thread id>`).

A launch has no retry key: after a lost or timed-out reply, `start` reads the project's newest threads with that title (`t3_thread_list`) for one on branch `bmesh/<thread id>`.

**`status`** is `t3_thread_read`:

- **Running:** `activeRunId` set, or the newest run `preparing`, `queued`, `starting`, `running` or `waiting`.
- **Reply:** the newest `assistant_message` since the last read.
- **Logged once per change:** a newest run `waiting` on a pending request (`t3: <path> waits on a question or approval`); one `failed` on `usage_limit` (`t3: <path> hit a usage limit; it resets at <time>`).
- **Stop:** a newest run `interrupted` with none active, as your interrupt in T3 or `stop` leaves it.
- **Not a stop:** `cancelled` (T3's restart recovery) or `failed` (a crash).

**`send`** is `t3_thread_send` in `queue` mode (never steering into a turn started since the status read), with `clientRequestId` `bmesh-<sha256 of thread, text and minute>`, so a retried wake is sent once.

**`stop`** is `t3_thread_interrupt`; an idle thread's `no_active_run` counts as success, as does a thread T3 no longer has.

#### `tmux`

1. `git worktree add -b <thread> <worktrees>/<repo>/<thread>`.
2. `claude [--model M] [--effort E]` in window `<thread>` of session `<tmux_session>` (created when missing), with the CLI's `BMESH_CONFIG`.
3. Waits for claude's prompt, answering a fresh worktree's dialogs: folder trust yes, first-run screens Enter, an offer to try something Escape.
4. Types the brief, pausing a second before Enter (claude reads a burst of text as a paste); after 90 s it types anyway.

`stop` presses Escape in the window.

#### `claude`

1. `git worktree add -b <thread> <worktrees>/<repo>/<thread> <branch or main>` (a round's thread: the checkout bmesh made).
2. Each turn is a detached headless `<claude> -p --output-format json --dangerously-skip-permissions` there: the first with `--session-id <thread> [--model M] [--effort E]` and the brief on stdin, later ones with `--resume <thread>` and the same `--model` and `--effort` (a resume restores neither).

`<user cache dir>/bmesh/claude/<thread>/` holds `worktree`, `model`, `effort`, `pid`, `turn`, and per turn `turn-<n>.in`, `.json`, `.err`.

- **`status`:** a turn runs while its pid is alive and its `.json` is not yet claude's result; it replies with the newest result.
- **`send`** mid-turn queues the text under `queue/`; `status` starts the oldest once the turn ends (the daemon's minute poll drains it).
- **`stop`** drops queued sends and kills the turn's process group.

### T3 sign-in

[Manual: `bmesh t3 login`](manual.md#bmesh-t3-login)

**OAuth.** `bmesh t3 login` registers through `<server>/.well-known/oauth-authorization-server`: dynamic registration, PKCE S256, grant `authorization_code`. T3 binds the code to the origin the browser approved it on (`iss`), and the exchange carries that origin, which is why a browser that reached T3 as `localhost` through a tunnel still signs in a bmesh on `127.0.0.1`.

The runner reads its token through one credential interface, so a self-renewing source could replace the login without touching the runner.

**The token file** `<config dir>/t3-token.json` holds `server`, `token`, `expiresAt`, `scopes` and `access`, the ceiling from the token's claims.

**Expiry pushes once only.** `<state dir>/t3-login-pushes.json` (see Config: State dir) records the pushes sent for the current expiry, so a restart repeats none and a new login starts over.

**Renamed branches** are read with `git rev-parse --abbrev-ref HEAD` in the node's `worktree`.

## Proposal rounds

[Manual: `bmesh ideate`](manual.md#bmesh-ideate)

**Nodes.** The round node has role `round`; `N` is one past the repo's highest round in `bm-nodes`. Its children have role `engineer` and `parent` the round. With `--idea` the round node carries `"idea": true`.

**Briefs.** Each brief carries its thread's UUID as `proposed_by`, and as `next_id` the first id of the block claimed for its child in `bm-specids` (five, three for an `--idea` thread; see Spec ids), claimed after the refusals and before the round node is written. The child form on another host is passed that id and claims nothing.

**Spec ids.** KV `bm-specids` holds one key per repo, its node token (see Names), whose value is the highest id claimed there, in decimal. A claim of `N` reads the key and its revision, writes `max(value, main's highest) + N` with `Create` (no key yet) or `Update` at that revision, and on a conflict (another writer's claim in between) reads again; it returns the first id of the block. `spec next-id --peek` reads it and writes nothing. The bucket is created by `Provision` or on first claim, has history 1 and no age limit, and stores to file like `bm-nodes`. The commands that land the ledger (`choose-ideas` and the `steps` edits through their worktree, `spec set`, `build`'s dispatch record and `land` after its rebase) read the ledger they are about to push; a duplicate id there is the scan's `LedgerError` with both files in `Shared`, refused with exit 3.

**Registration.** The round's `started` event opens it as `working`.

**Another host** is reached as `ssh -o BatchMode=yes <host> bash -lc 'bmesh ideate <repo> --child eng|ux --round N --next-id N --thread-id UUID --model M [--effort E] [--context <text>] [--idea]'`: the child form, which makes that one worktree, brief and node on its own box (the registry is the same everywhere) and prints its child's line. Both hosts are checked first with `bmesh help` over ssh. The child form refuses `--idea` without `--context` (exit 2) like the full form.

**Settling.** A settlement's event carries a `Nats-Msg-Id` of the round, state and text, so two daemons settling the same round store and push it once.

### Holds

[Manual: Holds](manual.md#holds)

`hold` writes KV `bm-holds` `<path>` to `{"gate","text","by","at"}`; a new hold replaces the key. `unhold` deletes the key, and `node forget` deletes it with the node. `bmesh land` reads `bm-holds` before it takes the lease and again once it holds it; `bmesh held` reads the lead's own key; `build --hold` writes the key before the runner starts the lead.

## Feedback

[Manual: `bmesh feedback`](manual.md#bmesh-feedback)

`feedback` publishes `{"from","family","repo","spec","commit","where","fixed","text","at"}` on `bm.feedback.<family>` in `bm-mesh`, `commit` the node's worktree HEAD when it is on this box. There is no consumer: a message is pending exactly as long as it is in the stream, until drained or the stream's 30 days pass.

`drain <family>` deletes the family's messages from `bm-mesh` with a subject-filtered purge up to and including `--through`; only that family's subject is touched, and nothing about the messages is kept on the mesh. `drain --seq` reads each sequence from the stream first and refuses the whole call when one's subject is not under `bm.feedback.`, so nothing is deleted.

### The fixer

[Manual: The fixer](manual.md#the-fixer)

The daemon decides on every feedback message and on each minute's pass, from the issues and fixer nodes that exist, never from what an earlier pass meant to do.

1. It claims the start by publishing `bm.daemon.<host>.fixer` (`to` the fixer's path, `<repo>-fix-<N>`; `text` `<N> open for <repo>`) with `Nats-Msg-Id` `fixer-<path>`, as a push is claimed. A duplicate reply means another daemon has it; a failed start is tried again once the stream's two-minute duplicate window has passed.
2. It runs `bmesh fix` with its own binary; output and any failure go to the daemon's log.

## Executors

[Manual: `bmesh executor`](manual.md#bmesh-executor)

**The node record.** `executor new` registers `<parent path>.<name>` with `role: executor`, `parent`, branch, worktree, repo, host and model. `executor remove` leaves the node and its branch name in `bm-nodes` until `node forget`.

**Report messages.** The ids are `ask-<path>-<hash of path and text>`, `blocked-<path>-<hash of path and text>` and `done-<path>-<sha>`: the event's own id, so a retry within the stream's two-minute duplicate window is one message. The message is sent after the move is checked against the node's state and before the event is published, so a refused event sends nothing.

## Finished nodes

[Manual: `bmesh node forget`](manual.md#bmesh-node-forget)

**Routine node cleanup.** A fresh run's node, state and runner sample are deleted once its run ends and its checkout is removed; its `started`, `progress` and `done` events stay in the stream for the map, and its reply's `by` still names it. The daemon's kind rescan, once a minute, deletes the node, state and runner sample of every routine instance on its host that is not running and has been `done` for `[status] keep_done` (`0`: never), a fresh run's leftovers included.

**What `node forget` deletes:** the node's `bm-nodes` entry, its state, its runner sample and its inbox consumer; its events stay in the stream. Its idle checks read the node record: "published an event of its own" is an event in `bm-mesh` whose `from` is the node's path; "on another box" is a `host` that is not this box, "another runner" a `runner.kind` that is not this box's runner; the thread status is asked once, for `runner.thread`.

### Stopping a node

[Manual: `bmesh node stop`](manual.md#bmesh-node-stop)

`node stop` writes the `blocked` state before it interrupts the thread, so the daemon's poll finds the node `blocked` and never puts its own stop over the reason; a thread that reported between the two steps is written again.

A stopped node is resumed only by a message whose `at` is later than the stopped state's (both to the second), which is how a STOP sent while the thread ran doesn't undo the stop.

An interrupt reported by the runner marks the node with the run as its id, and the runner sample keeps the run (`stopped_run`), so the same run never marks the node again, while a later interrupted run does.

## Routines

[Manual: Routines](manual.md#routines)

### Modes

[Manual: Modes](manual.md#modes)

**Instance names.** A fresh instance is `routine.<kind>.f<id8>`, a pooled one `routine.<kind>.<host>-<model-short>-<n>` (`<host>-cmd-<n>` for a command kind):

- `id8`: the job id's first 8 characters, lowercased.
- `model-short`: the model id lowercased, a leading `claude-` stripped, every character outside `[a-z0-9]` made `-`, runs of `-` collapsed, leading and trailing `-` trimmed (`claude-opus-5-5` is `opus-5-5`, `claude-opus-5-5[1m]` is `opus-5-5-1m`).
- `host`: the box's host name (`[host] name`, else the short host name, lowercased), `.` and `_` written `-`, so each box's instances are nodes of their own (`routine.review.k16-opus-5-5-1`, `routine.review.ricks-macbook-opus-5-5-1`).
- `n`: from 1 per model across this box's repos defining the kind.
- The checkout, local to the box, drops the host: `<checkouts>/<repo>/routine.<kind>.<model-short>-<n>`.

**Sessions.** A model instance passes `--session-id` on its first job and `--resume` after. A run whose output doesn't parse as claude's JSON result (a non-zero exit with no JSON included) drops the session whatever the recycle settings.

**The map** draws one box per kind (every kind a configured repo defines, plus any with instances), not per instance. Its second line counts `N queued · N busy · N idle · N spent` (queue depth, then `working`, `idle` and `done` instances of either mode; zero counts left out), or shows the mode (`inline`, `fresh`, `pooled`) when all are zero. A busy kind pulses as `working`; the box links to the routines page.

**The routines page** has a card per kind, its instances under a heading per repo: each row the name without `routine.<kind>.` (the full path in its tooltip), `working` first, then `idle`, the `done` ones folded into one `spent (N)` row per repo that opens on a tap.

### Jobs

[Manual: Jobs](manual.md#jobs)

A job is answered once the kind's consumer acked it or the requester's inbox holds its reply. `status` reads KV `bm-jobs` (below) for `held` and `running`; the jobs of a daemon whose entry wasn't rewritten within the kind's `AckWait` read `waiting`.

#### Queueing

[Manual: `bmesh routine queue`](manual.md#bmesh-routine-queue)

A job is an inbox message (`kind: "job"`) to `routine.<kind>` with the job id as `Nats-Msg-Id`, so one queued twice is stored once.

**The consumer** is `inbox-routine--<kind>`, shared by every box running the kind.

- `MaxAckPending` is -1 (no limit). A daemon fetches only while fewer of the kind's jobs than its capacity are in flight on it: its instance count for the kind (`pool` per model per repo for pooled, `pool` for fresh), at least 1.
- `AckWait` is the kind's longest `timeout` + 1m on the box that wrote it last; a daemon writes it when its feed for the kind starts and when its own value changes. On every other rescan it reads the consumer and, when `MaxAckPending` reads anything else (an old daemon's count), writes `MaxAckPending` -1 back, keeping the `AckWait` it read.
- A daemon marks each job it holds or runs in progress every 20 s, a third of that 1m, so the job isn't redelivered while the daemon lives.
- A stopping daemon hands its jobs back with a NAK. Only a daemon that dies leaves jobs unacked until `AckWait` passes.
- KV `bm-jobs` holds each daemon's unsettled jobs of a kind under `<kind>.<host>`, rewritten on every change and heartbeat, emptied when the daemon starts.

#### Running

[Manual: `bmesh routine run`](manual.md#bmesh-routine-run)

**Deferral.** A daemon that can't judge a job NAKs it with a delay of 2 s × 2^(n-1) (`n` the delivery count), at most 1m, less a random part of up to a quarter so redelivery doesn't fall in step with its own next fetch; sends no reply; and logs `routines: deferred job <id>: <repo> <why> here` once per job. A deferred job uses none of the box's capacity. A kind no configured repo defines on its main has no feed.

**A go** is one message, which one box fetches. Every box holding jobs of the kind looks in the stream for a go past its oldest held job (and the last go it acted on) after each empty fetch and, while its capacity is full, every second, so a go any box fetched starts every box's held batch.

#### Replies

[Manual: Jobs](manual.md#jobs)

A reply is an inbox message (`kind: "reply"`) to the requester carrying `{"job","kind","tree","ok","output","result_key","by"}`.

`wait` reads the reply from this node's inbox subject in the stream, not its consumer, so acked messages are found too for the stream's 30 days. It scans that history, then follows the subject until the reply arrives. A reply still unread is acked once found (one no-wait batch from the consumer that acks it and returns every other message unread), so no wake follows and `inbox` doesn't print it again; one further back than 100 unread messages stays unread and wakes the node once. The kind of a job id is looked up in the kinds' inboxes for the `--timeout` default.

### Setup

[Manual: Setup](manual.md#setup)

**The marker** is `<git dir>/bmesh-setup` (`git rev-parse --path-format=absolute --git-dir`: a linked worktree's own `.git/worktrees/<name>`, so every worktree keeps its own), one line of hex: the sha256 of `command <command>\0`, then for each path `git ls-files -z -- <inputs>` lists, in byte order, `file <path> <sha256 of its working-tree content, or missing>\0`. No inputs, no `ls-files`. A run removes the marker first and writes it after it succeeds (and, where the tree must stay clean, after `git status --porcelain -z --untracked-files=all` came back empty), hashing the inputs as they stand after the run.

**The run** goes through the platform shell in the worktree's root, its own process group killed at `timeout` and again after it exits, with the caller's environment and `BMESH_SETUP=1`. Its output is captured whole; a failure carries the last 30 lines. An inline run's setup is bounded by its own timeout, not the command's one-minute deadline. The daemon runs a job's setup under its loop's context, so a stopping daemon kills it and hands the job back.

### Leases

[Manual: Leases](manual.md#leases)

Leases live in KV `bm-leases`, key `<slot>.<n>` (`n` below the capacity), value `{"holder","expires_at"}`; an expired entry is free.

### Results

[Manual: Results](manual.md#results)

Results live in KV `bm-results`, key `<kind>.<tree>[.<args8>][.<model-short>]`: `tree` the commit's full tree hash, `args8` the first 8 hex of the sha256 of the args joined by NUL (only with args), `model-short` as in Modes (only for a model). The record:

```json
{"kind":"verify","tree":"<tree>","commit":"<sha>","args":[],"ok":true,"output":"...","at":"2026-01-02T03:04:05Z","by":"app-012","model":"claude-opus-5-5","patch_id":"<sha1>"}
```

- `model` is omitted for a command kind.
- `patch_id` is the `git patch-id --verbatim` (`--stable`, whitespace kept, so a re-indented hunk is another diff) of the diff a model was asked to read, from the commit the job text's `base: <sha>` names to the job's commit (`git diff-tree -r -p --binary --full-index --no-renames`, so no user config changes it). Omitted for a command kind, a job text naming no base and an empty diff.
- `output` is cut at 64 KiB.
- Only a run that judged its job is stored. A command's run always did (its exit code is the verdict); a model's only when claude returned a parsed result with no `is_error`. An unjudged model reply (killed, timed out, a non-zero exit, no result, an error result) is never stored, by the daemon or by `routine run`: it is answered `model run failed: <why>; nothing recorded`.

`bmesh serve` provisions `bm-results` with `[routines] keep_results` as the stream's age.

### `verified`

[Manual: The verified line](manual.md#the-verified-line)

`bmesh done` matches a carried entry by a model record at the tree its `c:` names (first 8 hex), with the same `ok` and model, whose `patch_id` is the diff `merged` landed: that of `<a>..<merged>` for one of the nearest 100 first-parent ancestors `<a>` of `merged`. `spec verified` carries a result whose `patch_id` equals that of HEAD's diff from its merge-base with `main` (`origin/main` when there is an origin).

## Landing

[Manual: `bmesh land`](manual.md#bmesh-land)

The land lease is `bm-leases` key `land.<repo>.0`, one place per repo.

## Scaffold

### The rules block

[Manual: `bmesh scaffold`](manual.md#bmesh-scaffold), [Rule sets](manual.md#rule-sets)

The sets are embedded from `internal/scaffold/rules/<name>.md`. The opening marker is `<!-- bmesh:rules <name>... <hash> -->`, its words split on whitespace: a lone `customized` is a kept block; else a last word of 8 lowercase hex is the hash and the rest are set names. The hash is the first 8 hex of the sha256 of the body scaffold wrote (the text between the two markers), with `\r\n` read as `\n` and surrounding whitespace trimmed, so a CRLF checkout or an editor's final newline doesn't read as an edit. `scaffold.Drift`, which `land` runs, compares only the `done`, `proposals` and `mesh` blocks.

## Hosts

[Manual: Hosts](manual.md#hosts)

**The join handshake.** The new box sends `{name, key, approve_key, ts, salt}` on `bmhosts.join.<id>` (core NATS request/reply, outside the stream: nothing stored), MACed with HMAC-SHA256 under `HKDF-SHA256(secret, salt=id, info="bmesh join request v1")`. The admin burns the invite on the first request whose MAC verifies; a request whose MAC fails is dropped without burning, and the third such failure burns it. The reply is `{roster, admin_key}` under the same MAC (`info="bmesh join reply v1"`, bound to the request's salt). The new box takes the first reply whose MAC verifies (ignoring junk) and checks that the roster carries its key before pinning the admin key and storing the roster.

**The roster** is KV `bm-roster`, key `roster` (outside `bm.>`): `{type, version, admin_key, hosts: [{name, key, secure, approve_key, accept}], issued_at, sig}`, signed by the admin key. The admin takes the next version from its own `roster.json` (after taking any newer roster its key signed), never from the hub, saves it there and writes the hub with compare-and-set on the key's revision. Every daemon takes a newer `roster.json` first (one the admin couldn't write to the hub, or a fresh invite's pin) and never writes an older roster over it; it reads the roster key each minute and before each job.

**Presence** is KV `bm-hosts`, key `<host>`, written each minute: `{type, host, rev, os, routines, repos: [{name, path, origin}], notes, ts, sig}`, signed with the host key.

**Jobs and replies** travel in `bm-mesh`: a job for a box on `bm.inbox.host.<host>`, whose durable consumer `inbox-host--<host>` (AckWait 10m, `InProgress` every minute while a job runs, MaxAckPending 1) resumes where it stopped; a reply on `bm.hostreply.<caller>.<job id>`. The map, `watch`, status and the activity reader skip both subjects.

**Signed messages.** `sig` is the hex Ed25519 signature over the SHA-256 of the message's bytes, deterministic (RFC 8032). The bytes are every field in a fixed order, starting with the type tag: a scalar is a 4-byte big-endian length then its bytes (a string as UTF-8, an integer in decimal ASCII, a boolean `0` or `1`); a list is a 4-byte big-endian count then each item; a struct is its fields in order. Lists of names (`accept`) are sorted and deduplicated; the roster's hosts and presence's repos are sorted by name. `ts`, `expires` and `issued_at` are RFC 3339 UTC to the second; a `salt` is 16 random bytes in hex; keys and signatures are lowercase hex. The type tags and fields:

- job (`bmesh/job/v1`): `type, id, by, to, op, repo, args, roster_version, ts, expires, salt`;
- reply (`bmesh/reply/v1`): `type, job_id, job_salt, by, to, ok, text, ts, salt`;
- approve (`bmesh/approve/v1`): `type, job_id, job_salt, job_sha256, by, decision, ts, salt`;
- roster (`bmesh/roster/v1`): `type, version, admin_key, hosts (name, key, secure, approve_key, accept), issued_at`;
- registry (`bmesh/registry/v1`): `type, version, admin_key, rows, issued_at`;
- presence (`bmesh/presence/v1`): `type, host, rev, os, routines, repos (name, path, origin), notes, ts`.

**The receive check**, in order, stopping at the first failure:

1. `by` is in the roster (an op that needs the admin is checked against the admin key).
2. The signature verifies.
3. `to` is this box.
4. `roster_version` isn't ahead of this box's roster (else it naks the job for a minute, until it has caught up; a hub it can't read naks too).
5. `ts` is at most 5 minutes ahead of its clock (else a nak for a minute).
6. `expires` hasn't passed and `expires - ts` is at most 24h.
7. The op is in this box's `accept`.
8. The salt's record in `<state dir>/salts` (one file per salt, kept until the job expires, surviving a restart):
   - none: it writes `started` and runs the job;
   - `done`: it acks and sends the stored reply again, never re-running, logged only;
   - `started` (a redelivery after a crash or sleep): it resumes an idempotent op and answers any other `interrupted`;
   - a salt another job used is refused as `replay`.

Roster, signature, `to` and replay failures are published as `bm.daemon.<host>.refused` (`text` `refused <check> from <sender>: <op> job <id>`, `body` `<sender> <check>`), which a daemon pushes through the `push-<seq>` claim (the push claim, see Pushes). The caller checks a reply likewise: signed by the job's `to`, naming the job's id and salt.

**Levelling.** The repo's lock is `<state dir>/locks/<repo>.lock`, a flock that waits for its holder. A hold is `<state dir>/held/<job id>.json`: the job (a self-level's is the box's own, signed with its host key), the repo, its head and target, and the log. The box publishes `bm.daemon.<host>.held` (`text` `<host> holds: <repo> +<N> commits (<sender or self-level>)`, `body` the hold's id), pushed once through the push claim (title the text). `bmesh hosts approve` signs `approve/v1` over the job's hash with `decision` `approve <target>` and sends it to the daemon by core NATS request/reply on `bmhosts.approve.<host>` (outside `bm.>`); the daemon checks that the approval names the hold's job (id, salt and hash) and target. A late level's refusal or failure is published as `bm.daemon.<host>.level` (`text` `<host>: <repo> <answer> (job from <sender>)`) and pushed once through the push claim (title the text).

**The registry** is KV `bm-roster`, key `registry`: `{type, version, admin_key, rows, issued_at, sig}`, signed by the admin key; `rows` is the admin's `repos.toml` as written. A box that takes one writes the document as `<state dir>/registry.json` beside the rows it writes as `repos.toml`; every daemon reads the registry key each minute and puts its own `registry.json` back when the hub's copy is missing, unsigned or lower. `bmesh repos publish` takes the next version from the box's own `registry.json`, never the hub's. A registry a box can't read is published as `bm.daemon.<host>.registry` (`text` `<host>: refused: registry v<N> needs a newer bmesh (<error>)`), pushed once per version through the push claim. A held registry proposal is `<state dir>/held/<job id>.json` with the row diff, published as `held`.

**Removing a repo everywhere.** `bmesh repos remove --checkouts` keeps the boxes it asked and their jobs in `<state dir>/nix/<name>.json` until each has answered.

## Names

[Manual: Names](manual.md#names)

The stream, subjects and buckets built on node paths:

| Kind | Name | Subjects / keys |
|---|---|---|
| stream | `bm-mesh` | `bm.>` (30 days, file store, 2m duplicate window) |
| event subject | | `bm.<path>.<event>` |
| inbox subject | | `bm.inbox.<path>` |
| daemon subject | | `bm.daemon.<host>.<event>` (`started`, `update`, `wake`, `silent`, `digest`, `push`, `push-failed`, `refused`, `held`, `level`, `registry`, `fixer`) |
| consumer per inbox | `inbox-<path, "." as "--">` | filter `bm.inbox.<path>` |
| kind inbox subject | | `bm.inbox.routine.<kind>` (jobs and gos) |
| consumer per kind | `inbox-routine--<kind>` | filter `bm.inbox.routine.<kind>`, shared by every box that runs the kind; MaxAckPending -1 (each box fetches up to its own instance count), AckWait = timeout + 1m (the last writer's) |
| routine event subject | | `bm.routine.<kind>.<instance>.<event>` (`started`, `progress`, `done`) |
| feedback subject | | `bm.feedback.<family>` (a build family's pending feedback; no consumer, drained by purge) |
| host job subject | | `bm.inbox.host.<host>` (signed jobs for a box; see Hosts) |
| consumer per host | `inbox-host--<host>` | filter `bm.inbox.host.<host>`; MaxAckPending 1, AckWait 10m |
| host reply subject | | `bm.hostreply.<caller>.<job id>` |
| join subject | | `bmhosts.join.<id>` (core NATS, outside the stream: an invite's request and reply) |
| KV | `bm-nodes` | `<path>` to Node (a build's lead carries `family`; a runner that names its own thread, as T3 does, gets the id bmesh chose as `runner.ref`, recorded as the briefs' `proposed_by` and the spec's `built_by`) |
| KV | `bm-state` | `<path>` to State |
| KV | `bm-runner` | `<path>` to RunnerSample |
| KV | `bm-results` | `<kind>.<tree>[.<args8>][.<model-short>]` to Result (`[routines] keep_results`, default 30 days) |
| KV | `bm-leases` | `<slot>.<n>` to `{"holder","expires_at"}` |
| KV | `bm-roster` | `roster` to the admin's signed roster, `registry` to its signed registry (see Hosts) |
| KV | `bm-hosts` | `<host>` to that box's signed presence |
| KV | `bm-claims` | `quota.<provider>.<reset unix>` to the claiming daemon (`daemon.<host>`); age limit 8 days |
| KV | `bm-jobs` | `<kind>.<host>` to `{"kind","host","at","jobs":[{"seq","job","from","holder","state","since"}]}` (that daemon's held and running jobs of the kind) |
| KV | `bm-holds` | `<path>` to `{"gate","text","by","at"}` (a coordinator's hold on a lead, until `unhold`; see Holds) |
| KV | `bm-specids` | `<repo token>` to the highest spec id claimed in that repo, in decimal (see Spec ids); no age limit |

The map, `watch`, status, the push loop and the activity reader skip subjects whose first token is one of the five reserved words (`mesh.IsMeshInternal`), each keeping the inbox and daemon events it reads; host traffic (`bm.inbox.host.*`, `bm.hostreply.*`) never reaches the map or `watch`. JetStream is addressed in `[mesh] domain` (default `hub`).

## Config

### config.toml

[Manual: config.toml](manual.md#configtoml)

**Daemon events.** Each digest is also published as a `digest` event on `bm.daemon.<host>.digest` (title as `text`, body as `body`). A wake's text is published as a `wake` event on `bm.daemon.<host>.wake`; a node marked silent is published as `silent` on `bm.daemon.<host>.silent`.

**Reading the wake text.** The daemon reads the items only when the wake is due, from the inbox subject in the stream above the consumer's ack floor, never by fetching, so it holds or acks none of the lead's messages. The count is the consumer's; a reply `routine wait` already acked (see Jobs) can sit above the floor, so the list can be longer than the count.

### Pushes

[Manual: Pushes](manual.md#pushes)

**Claims.** Before posting, a daemon claims the event by publishing `bm.daemon.<host>.push` (`to` the lead, `text` the title, `seq` the event's stream sequence) with `Nats-Msg-Id` `push-<seq>`; a duplicate reply means another daemon has it. On start it pushes, in order, the unclaimed listed events no older than `catch_up` (by the event's `at`, else its stored time), then follows live. A live event stored over a minute ago (the backlog a hub reconnect delivers) is dropped the same way when older than `catch_up` or when any daemon's `push` claim names its seq.

**Failures.** A push that still fails after retries at 5 s and 30 s is logged and recorded as `push-failed`; its claim stands, so it is never repeated. At shutdown the push being posted gets 10 s; a claimed push still queued or awaiting retry is logged and recorded as `push-failed` with body `shutdown`.

### Quota reminders

[Manual: Quota reminders](manual.md#quota-reminders)

The reset is rounded to the minute because readings jitter by fractions of a second. Once per window, across daemons and restarts: before posting, a daemon creates `quota.<provider>.<rounded reset unix>` in `bm-claims`; a key already there means another daemon (or this one, an hour ago) has it. A failed post deletes its claim, so the next reading retries.

### Add-ons

The add-ons are `addons.Registry`, one package each under `addons/` (`addons/t3done`), by the names `config.AddonNames` lists; a test keeps the two equal. The daemon starts each name `[addons] enable` lists in a goroutine of its own, with the config, the host and its own state dir, `<state dir>/addons/<name>`.

#### t3-done

[Manual: t3-done](manual.md#t3-done)

It opens T3's database through `modernc.org/sqlite` (pure Go, so every OS builds it) as `file:///<path>?mode=ro` with a 2 s busy timeout, once per read, and closes it, so a database T3 replaces is read anew.

| Table | Columns | Payload fields (`payload_json`) |
|---|---|---|
| `orchestration_v2_projection_threads` | `thread_id`, `project_id`, `title`, `deleted_at` | `createdBy`, `lineage.parentThreadId` |
| `orchestration_v2_projection_runs` | `thread_id`, `ordinal`, `provider_thread_id`, `status`, `requested_at`, `completed_at` | `userMessageId` |
| `orchestration_v2_projection_provider_threads` | `provider_thread_id` | `pendingBackgroundTasks` |
| `orchestration_v2_projection_runtime_requests` | `thread_id`, `status`, `created_at` | |
| `orchestration_v2_projection_messages` | `message_id`, `thread_id`, `role`, `created_at` | `text`, `notification` |
| `projection_projects` | `project_id`, `title` | |

The start-of-read check runs `pragma_table_info` on each table and `json_type` of each field on its table's newest row (by rowid; for messages, the newest `assistant` one, and not `notification`, which only a wake-up's message carries). An empty table passes.

**The rule for done.** Every turn end is a run row whose `completed_at` is set, a wake-up included: a message whose `notification.source.kind` is `background_task` starts a new run. What tells them apart is the run's provider thread (`provider_thread_id`): its `pendingBackgroundTasks` still lists the task while it runs, and T3 clears it as soon as the task ends. T3's API (`t3_thread_read`, `t3_thread_list`) doesn't return that field, so the add-on reads the database. A run posts done when `status` is `completed` and `json_array_length(pendingBackgroundTasks)` is 0 (no provider thread counts as 0); failed when `status` is `failed`, whatever is pending. The list is read when the run is read, within a tick of its end.

**Busy.** A done posts only when `completed_at` minus the stretch's start is at least `min_busy`. The stretch starts at the `requested_at` of the newest run of the thread, at or before this one by `ordinal`, whose `userMessageId` message has no `notification` (a person's message, not a background task's; a message not found counts as a person's).

**Rows read.** Runs with `completed_at` after the `runs` mark, and requests with `status = 'pending'` and `created_at` after the `requests` mark, oldest first, each joined to its thread where `deleted_at` is null, `lineage.parentThreadId` is null and `createdBy` isn't `agent`. The reply is the thread's newest `assistant` message created at or before the row's time. A mark is T3's own timestamp (`2006-01-02T15:04:05.000Z`, compared as text), written after each row through a temp file and a rename.

### Map

[Manual: Map](manual.md#map)

#### Blocks and the shelf

Ties in need order keep `started_at` order, so a running lead keeps its place. Only the lead's own state decides the shelf: a live lead whose executors are all done stays a tree, and its `done` event moves it to the shelf on that redraw.

#### The strip

The summary row is computed on every render from the loaded tree, in the order `working`, `asking`, `blocked`, `silent`, `held`, `done`, zero counts dropped, each word coloured as its badge, `all idle` when none of the six has a node; a live event that restyles a node updates it at once. The strip shows whenever the tree has nodes, quota or digest or not. The digest fold is worded as the hourly digest was, not as the present. Under `?check=1` the title's `strip=` counts the strip's rows (summary included), `held=` the `held` boxes on the map and `bandHeld=` the `held` rows in the needs-you band (always 0); `node scripts/check.mjs` exits 1 when `bandHeld=` is non-zero.

#### The recent-events drawer

A chip's hue is a hash of the path's top segment over eight hues; an executor's is a lighter step of its lead's. A chip is labelled without the prefix of an ancestor that has rows of its own (`eng`; the full path in the tooltip). Each node with rows takes a lane, in order of first appearance from the newest row, 14 px apart: a faint line in its hue down the list, its chip at the lane's offset. Past six lanes (three under 700 px) the rest share the last lane, its line grey. Hovering a chip or its line also brightens the line; a pin holds across new events, and clicking another chip moves it.

#### The needs-you band

A band row leaves live or on the next tree read. The operator rows read each repo's ledger as the lead page reads a spec (local `origin/main`, cached a minute, fetched in the background); `/api/tree` carries them as `operator`, `[{"repo","id","title","open","total"}]`, omitted when none. An `asking` or `blocked` box's second line replaces its unread count with as much of the words as fits; a `silent` box draws at full opacity with a dashed stroke in the blocked colour; a `held` box has a solid stroke. The band's words, the drawer's rows and the lead page's inbox and timeline rows clamp to three lines and open on a tap. The timeline names a child by its last segment (`eng`, not `buildermesh-round-1.eng`), the full path in the tooltip.

#### The lead page

`/api/lead/<path>` serves the page for any node and never fetches in the request. It carries `children`, `[{node, state, unread}]`, the node's direct children in the tree `/api/tree` draws (a lead's executors, a round's engineers), and for a round `specs`, `[{id, title, status, text}]`, the specs on that `origin/main` whose `proposed_by` is one of its children's runner threads (or a child's runner `ref`), by id: `[]` before any lands, `null` for other nodes. The ledger is cached a minute per repo, like the spec; an unparseable file is left out. The `tree` card is absent without children. In the one-column layout (narrower than 1000px) a card with nothing to show sorts after cards with content. Under `?check=1` the title also names `firstCard`, the card drawn highest.

## Build and test

### Testing

[Manual: Testing](manual.md#testing)

These variables swap what bmesh runs or reads (commands split on spaces):

| Variable | Replaces | Tests set |
|---|---|---|
| `BMESH_TMUX_COMMAND` | the tmux binary | `sh <repo>/testdata/fake-tmux.sh` |
| `BMESH_T3_TIMEOUT` | the t3 runner's 120 s per-call timeout (a Go duration) | |
| `BMESH_CLAUDE_COMMAND` | `[runner] claude` for routines and the claude runner; the `claude` a level's plugin refresh runs | `sh <repo>/testdata/fake-claude.sh`; plugin refresh: `sh <repo>/testdata/fake-claude-plugin.sh` |
| `BMESH_SSH_COMMAND` | the `ssh` `bmesh ideate` reaches another host with | `sh <repo>/testdata/fake-ssh.sh` |
| `BMESH_GO_COMMAND` | the `go` an update builds with | `sh <repo>/testdata/fake-go.sh` |
| `BMESH_UPSTREAM_URL` | `https://<module path>`, where an update without a row and the notice read the module's `main` | a local repo |
| `BMESH_SECURITY_COMMAND` | the macOS `security` the Keychain key store runs | `sh <repo>/testdata/fake-security.sh`: items under `$FAKE_SECURITY_DIR`, access list from `$FAKE_SECURITY_ACL` |
| `BMESH_CODEX_COMMAND` | the `codex` a level's plugin refresh runs (unset: only when on `PATH`) | `sh <repo>/testdata/fake-codex.sh` |
| `BMESH_KEY_STORE` | the key store default (`keychain` or `file`) when `[hosts] key_store` is unset | `file` |
| `BMESH_CACHE_DIR` | the user cache dir (`os.UserCacheDir`: `~/.cache` or `$XDG_CACHE_HOME` on Linux, `~/Library/Caches` on macOS), an absolute path, where briefs and a claude session's files go | |

The `cmd/bmesh` tests set `BMESH_UPSTREAM_URL`, `XDG_STATE_HOME`, `BMESH_KEY_STORE`, `BMESH_SECURITY_COMMAND`, `BMESH_CLAUDE_COMMAND`, `BMESH_CODEX_COMMAND` and `BMESH_CACHE_DIR` package-wide (a test's own setting wins), so no test reaches the network, the box's notice or cache, the real Keychain dialog, or the box's own `claude` and `codex`. Leave all ten unset outside tests.

The t3 runner, `bmesh t3 login`, the expiry pushes and the `lead start`, `build`, `ideate` and `node forget` tests run against `internal/runner/t3mcp/t3fake`, an `httptest` T3 0.1.0 (the environment document, the MCP OAuth endpoints and the MCP tools) that answers with shapes recorded from a live 0.1.0 server, kept in `testdata/t3v2/`.

**Go fixtures** in `internal/meshtest`:

- `Start`: an embedded server.
- `Git(t, dir, args...)`: git with a fixed identity and no global config, hooks or signing; returns its output, trimmed.
- `CommitFiles(t, repo, files)`: writes and commits them, returns the commit.
- `SyncBuffer`: a log sink goroutines and the test share.
- `ForceState(t, c, path, mesh.State{...})`: writes a node's state past the transition table, for a fixture that needs `done` or `silent` in one step.
- `QueueJob(t, c, kind, from, job)`: creates the kind's consumer, then sends the job, so it counts toward the kind's depth.
