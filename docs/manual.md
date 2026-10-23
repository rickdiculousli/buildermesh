# bmesh manual

Every `bmesh` command with its flags, output and refusals, then the model behind them, the config files and exit codes. The [README](../README.md) has the quickstarts and an index of all commands; [internals](internals.md) covers NATS subjects, buckets, record shapes and signing. bmesh's own messages cite this manual by section name (`docs/manual.md: Several boxes`).

## Contents

- [Synopsis](#synopsis)
- [Setup and updates](#setup-and-updates): [`bmesh init`](#bmesh-init) · [`bmesh serve`](#bmesh-serve) · [`bmesh hub`](#bmesh-hub) · [`bmesh config check`](#bmesh-config-check) · [`bmesh t3 login`](#bmesh-t3-login) · [`bmesh version`](#bmesh-version) · [`bmesh update`](#bmesh-update)
- [Proposals and builds](#proposals-and-builds): [`bmesh ideate`](#bmesh-ideate) · [`bmesh choose-ideas`](#bmesh-choose-ideas) · [`bmesh spec`](#bmesh-spec) · [`bmesh build`](#bmesh-build) · [`bmesh lead start`](#bmesh-lead-start) · [`bmesh hold`, `unhold`, `held`](#bmesh-hold-unhold-held) · [`bmesh feedback`](#bmesh-feedback) · [`bmesh fix`](#bmesh-fix) · [`bmesh release`](#bmesh-release) · [`bmesh scaffold`](#bmesh-scaffold)
- [Nodes](#nodes): [`bmesh whoami`](#bmesh-whoami) · [`bmesh started`, `progress`, `blocked`, `ask`, `done`](#bmesh-started-progress-blocked-ask-done) · [`bmesh send`](#bmesh-send) · [`bmesh inbox`](#bmesh-inbox) · [`bmesh thread-status`](#bmesh-thread-status) · [`bmesh mesh-status`](#bmesh-mesh-status) · [`bmesh watch`](#bmesh-watch) · [`bmesh executor`](#bmesh-executor) · [`bmesh node`](#bmesh-node)
- [Checks and landing](#checks-and-landing): [`bmesh routine`](#bmesh-routine) · [`bmesh result`](#bmesh-result) · [`bmesh lock-lease`](#bmesh-lock-lease) · [`bmesh setup`](#bmesh-setup) · [`bmesh land`](#bmesh-land) · [`bmesh ready`](#bmesh-ready)
- [Boxes](#boxes): [`bmesh hosts`](#bmesh-hosts) · [`bmesh sync`](#bmesh-sync) · [`bmesh prune`](#bmesh-prune) · [`bmesh repos`](#bmesh-repos)
- [Concepts](#concepts): [Several boxes](#several-boxes) · [Updates](#updates) · [Upgrading](#upgrading) · [Runners](#runners) · [Build families](#build-families) · [Holds](#holds) · [Briefs](#briefs) · [The fixer](#the-fixer) · [The ledger](#the-ledger) · [Routines](#routines) ([Setup](#setup)) · [Hosts](#hosts) · [The plugin](#the-plugin) · [Names](#names)
- [Config](#config): [config.toml](#configtoml) · [repos.toml](#repostoml) · [Pushes](#pushes) · [Quota reminders](#quota-reminders) · [Add-ons](#add-ons) ([t3-done](#t3-done)) · [Map](#map) · [Upgrade notes](#upgrade-notes)
- [Exit codes](#exit-codes)
- [Build and test](#build-and-test): [Testing](#testing)

## Synopsis

```
bmesh [--config FILE] [--node PATH] [--json] <command> [args]
```

`bmesh help` (or `-h`) prints the usage text, one line per command, on stdout. `bmesh` with no command prints it on stderr and exits 2. An unknown command exits 2 with ``unknown command "<name>" (run `bmesh help`)``.

| Flag | Default | Meaning |
|---|---|---|
| `--config FILE` | `$BMESH_CONFIG`, else `config.toml` in the config dir | The config file. `repos.toml` and the setup flag sit beside it (see [Config](#config)). |
| `--node PATH` | resolved as below | Act as this node path. |
| `--json` | off | Print JSON, for a command that has a JSON form. |

The three global flags are accepted before or after the command.

**How a command finds its node.** A command that acts as a node (`whoami`, the events, `send`, `inbox`, `land`, `executor`, and others) resolves the node path in this order:

1. `--node`;
2. else `BMESH_NODE`;
3. else the registered node whose worktree is this checkout (`git rev-parse --show-toplevel`) on this host.

A path that does not parse exits 2 with `node "<path>": <why>`. When nothing resolves, the command exits 2 with `no node for this directory; pass --node or set BMESH_NODE`. A command a person may run outside any node (`hold`, `unhold`, `node stop`, `result`, `lock-lease`) acts as this host's name instead.

**The setup flag.** `<config dir>/.setup-complete` is the file [`bmesh init`](#bmesh-init) writes last. Any other command but `help`, `version`, `hosts --here` and `update --notice` checks for it, or for a `config.toml`, first. When it finds neither:

- At a terminal, it prints one line (`bmesh: not set up yet (no <path>); running bmesh init first`) and runs `init` first.
- Outside a terminal, it is refused (exit 3, `` not set up (no <path>): run `bmesh init` ``).

**Per OS.**

| What | Linux | macOS | Windows |
|---|---|---|---|
| Config dir (`os.UserConfigDir`) | `~/.config/bmesh` | `~/Library/Application Support/bmesh` | `%AppData%\bmesh` |
| State dir | `~/.local/state/bmesh` | `~/Library/Application Support/bmesh/state` | `~/.local/state/bmesh` |
| Cache dir (`os.UserCacheDir`) | `~/.cache` | `~/Library/Caches` | `%LocalAppData%` |
| A configured command line runs through | `sh` | `sh` | `cmd /C` |

- An existing `~/.config/bmesh` is kept as the config dir on any OS. See [Config](#config).
- `$XDG_STATE_HOME/bmesh` is the state dir on any OS when `XDG_STATE_HOME` is set.
- The embedded hub's store is in the OS cache dir (`os.UserCacheDir`), at `bmesh/jetstream`.
- Under `cmd /C`, every argument is quoted, and an argument holding a quote or `%` is refused.
- Nothing assumes tmux.
- Linux and macOS are run. Windows is built and vetted on every change (`GOOS=windows go vet ./...` is in verify) and run by users.

## Setup and updates

Set a box up, run its daemon and hub, and keep bmesh current.

### `bmesh init`

```
bmesh init [--service] [--force] [--role hub|leaf|client] [--hub URL] [--token-file F] [--runner K] [--no-repo]
bmesh init --hosts [--admin]
```

Sets this box up: it writes `config.toml`, registers the current repo in `repos.toml`, and with `--service` writes the service units. You run it once per box, then again with `--hosts` to join the roster.

| Flag | Default | Meaning |
|---|---|---|
| `--service` | off | Also write the unit that keeps `bmesh serve` running, and with `[hub] role` set the one for `bmesh hub` (see below). |
| `--force` | off | Rewrite an existing `config.toml` and this repo's registry row; with `--service`, replace a unit that differs from what `init` writes. |
| `--role hub\|leaf\|client` | prompted | Multi-box: this box's part (see [Several boxes](#several-boxes)). |
| `--hub URL` | prompted | Multi-box: the hub's URL. `--hub` alone makes a client. |
| `--token-file F` | prompted | Multi-box: the hub's token file. |
| `--runner K` | the first of `claude`, `t3`, `tmux` on PATH; `claude` when none is | The runner kind (see [Runners](#runners)). |
| `--no-repo` | off | Set up without registering the current directory's repo. |
| `--hosts` | off | Join this box to the mesh with a one-time invite code (asked at the prompt), or make it the admin with `--admin` (see With `--hosts`, below, [Hosts: Joining](#joining) and [`bmesh hosts invite`](#bmesh-hosts-invite)). |
| `--admin` | off | With `--hosts`: answer "be the admin". Without `--hosts` it is a usage error (exit 2). |

**What it does**, in order:

1. Checks the repo it registers, before anything is written (see Refusals).
2. Prompts for each setting not passed as a flag (`--role`, `--hub`, `--token-file`, `--runner`, `--no-repo`). It reads stdin, and an empty line, or no stdin, takes the default, so a script can drive it. The first prompt is single box or multi-box (default single). The multi-box answer adds a prompt for this box's part: hub, leaf or client (default client).
3. Writes `config.toml`. It comments each key it writes.
4. Registers the repo (prompt `Register <name> (<path>) in <repos.toml>? [Y/n]`).
5. With `--service`, writes the units.
6. Records the box's release (see [Upgrading](#upgrading)).
7. Writes the setup flag last (see [Synopsis](#synopsis)).

**Multi-box setup.** With the multi-box answer, `init` writes `[mesh]` and `[hub]` for this box's part:

- **Hub:** `init` prompts for the address (`<tailnet ip>:4222`, default this box's in `100.64.0.0/10`), the leaf port (`<the same ip>:7422`) and the token file (default `<config dir>/nats.token`, made if missing, mode 0600). It writes:

```toml
[mesh]
nats = "nats://<tailnet ip>:4222"
token_file = "<config dir>/nats.token"
[hub]
role = "hub"
listen = "<tailnet ip>:4222"
leaf_listen = "<tailnet ip>:7422"
```

- **Client:** `init` prompts for the hub's URL and the same token file (`--hub`, `--token-file`; `--hub` alone makes a client) and writes them to `[mesh]`.
- **Leaf:** `init` prompts for the hub's leaf URL and writes:

```toml
[mesh]
nats = "nats://127.0.0.1:4222"
token_file = "<config dir>/nats.token"   # the hub's token, copied here (mode 0600); init says so when it isn't there yet
[hub]
role = "leaf"
listen = "127.0.0.1:4222"
remote = "nats://<hub tailnet ip>:7422"
```

**`--service`** writes the unit that keeps `bmesh serve` running: a systemd user unit on Linux, a launchd agent on macOS. With `[hub] role` set, it writes one for `bmesh hub` too. It prints how to enable each. On Windows it writes nothing and prints two commands for `cmd` (see Details).

| Manager | Hub unit | Enable |
|---|---|---|
| systemd | user unit `bmesh-hub.service` (`ExecStart=<bmesh> hub`, `Restart=always`, `RestartSec=5`, `RestartPreventExitStatus=78`) | `systemctl --user daemon-reload && systemctl --user enable --now bmesh-hub` |
| launchd | agent `com.buildermesh.bmesh-hub` (`KeepAlive`, its log `~/Library/Logs/bmesh-hub.log`) | `launchctl bootstrap gui/$(id -u) <plist>` |
| Windows | none | run `bmesh hub` yourself |

- The units restart always, since the tailnet address may not exist at boot.
- The systemd units don't restart on exit 78, a refused config (see [Config](#config)). launchd has no per-code stop: a bad config restarts serve at most every 5 minutes, and the hub agent at launchd's default rate.
- A unit already there with the same bytes is left alone (`unchanged <file>`).
- A unit that differs (tuned by hand, or by an older bmesh) is kept and gets no enable command. The new body goes beside it as `<file>.new`: `kept <file> (it differs from what init writes); the new one is <file>.new: diff them, and move it over to take it`. `--force` replaces it.

**On an existing `config.toml`**, `init` edits nothing (nor a registry row it has) without `--force`. It still writes the setup flag and, with `--service`, a missing unit.

**The repo it registers** must have a `main` branch: `origin`'s as last fetched, else the local one. An unborn `main` from `git init -b main` counts. Nothing is fetched.

**With `--hosts`.** The prompts and invite codes are in [Hosts: Joining](#joining).

- **What it writes.** Last, after the keys and the join, it writes the `[hosts]` table (`enabled`, `admin`, `secure`) and, with routines off, `[runner] checkouts = ""`. Turning routines back on drops a `checkouts = ""` line. Other `config.toml` lines stay, the old file is kept as `config.toml.prev`, and `repos.toml` is not touched. `--force` still starts over. Restart the daemon afterwards.
- **Output.** The admin prints `admin: <host> <fingerprint>, roster v<N>`. If the hub holds a roster signed by another key, it prints `a roster signed by <fingerprint> exists; becoming admin replaces it on every box you invite again`. A joining box prints `joined: roster v2, admin <host> <fingerprint>`.

**Output.** `init` prints `wrote <file>` for each file it wrote. Last, before `Next:`, it prints every setting in effect (defaults included) with its value, meaning and key. On an existing config, it prints the same block for what is there:

```
What this box is set to:
  setup: single box. This box runs everything; `bmesh init --force --hub <url>` joins a hub other boxes share instead ([mesh] nats)
  hub: embedded on 127.0.0.1:4222. bmesh serve runs the message hub itself; nothing to install ([mesh] nats, listen)
  runner: claude. Each lead and proposal round is a claude session started for you ([runner] kind)
  repo: your-repo at /home/you/repos/your-repo. Registered: bmesh ideate your-repo proposes changes to it, bmesh build your-repo <NNN> builds one (repos.toml)
  worktrees: /home/you/.bmesh/worktrees. Where bmesh makes the separate checkouts its nodes work in, so yours is never touched ([runner] worktrees)
  map: http://127.0.0.1:3774/. A page showing every running node; open it while bmesh serve runs ([serve] listen)
  proposers: claude-opus-5-5 at xhigh effort. The two sessions of a proposal round, which read the repo and write specs ([models] proposer, proposer_effort)
  leads: claude-opus-5-5 at high effort, unless the spec's judge names another. The session that builds one spec and lands it on main ([models] lead, lead_effort)
  executors: chosen by each lead. Helpers a lead starts for pieces of its spec (bmesh executor new --model)
  updates: notice every 6h, release. The daemon checks for bmesh's newest release and each session says when this binary is behind; bmesh update takes it ([update] check_every, upstream, repo, track)
  service: none. Run bmesh serve yourself and keep it running, or have the OS do it (bmesh init --service)
  routines: on. This box runs any pushed commit's routine commands for every box on the mesh ([runner] checkouts)
```

- One line per registered repo (`repo: none` with none).
- A multi-box config shows `setup: several boxes` and the hub's URL (a password in it as `xxxxx`) with its token file.
- `service` names the unit or plist when the file `--service` writes exists.

Then `set up (<setup flag>). Next:` and the commands to run next: the enable commands with `--service`, else `bmesh serve` (and `bmesh hub` with `[hub] role` set), then `bmesh repos` and `bmesh mesh-status`.

**Refusals.** The repo's refusals come before anything is written.

| When | Exit | Message |
|---|---|---|
| The repo has no `main` branch | 3 | `bmesh: <path> has no main branch; bmesh builds on main (rename the branch, or see https://github.com/rickdiculousli/buildermesh/issues/1; --no-repo sets up without it)` |
| The repo's name has a node token another row already has (see [Names](#names)) | 3 | `repos <a> and <b> are both <token> in node paths; rename one` |
| `--runner` outside `claude`, `t3`, `tmux` | 2 | `runner "<kind>" is not one of claude, t3, tmux` |
| `--role` outside `hub`, `leaf`, `client` | 2 | `role "<role>" is not one of hub, leaf, client` |
| `--hosts`: no `config.toml` (run `bmesh init` first) | 3 | `` no <path>: run `bmesh init` first, then `bmesh init --hosts` `` |
| `--hosts`: the `[host] name` (or host name) isn't one subject token: `^[a-z0-9][a-z0-9-]*$`, no `--`, at most 63 characters | 3 | `host name "<name>" isn't one subject token: set [host] name in <path>` |
| `--hosts`: a join with no valid reply (it stores nothing) | 3 | `join: no answer to invite <id>: the code is wrong, used or expired` |
| `--hosts`: the admin refused the join (it stores nothing) | 3 | `join: refused: <reason>` |
| On Windows, `init --service`: the config path holds a character `cmd` reads there (`"`, `%`, `&`, `^`, `\|`, `<` or `>`) | 1 | see below |
| On Windows, `init --service`: a task command over the 261 characters `/TR` takes | 1 | `init --service: the task's command is <n> characters, more than the 261 schtasks takes: <command>` |

The Windows path message: `init --service: "<path>" cannot be passed through cmd (it holds one of " % & ^ | < >); move it and run init --service again`.

**Details.**

- A first box, from install to the registry, takes four commands. A box needs Go and Claude Code (`claude` on PATH):

```
go install github.com/rickdiculousli/buildermesh/cmd/bmesh@latest   # or, from a checkout, GOBIN=~/.local/bin go install ./cmd/bmesh
cd ~/repos/your-repo && bmesh init     # asks single or multi-box, writes config.toml, registers this repo, picks the claude runner, says what it set
bmesh serve                            # the hub (embedded on a single box), the daemon and the map at http://127.0.0.1:3774/; keep it running
bmesh repos                            # the registry; then bmesh scaffold your-repo, bmesh ideate your-repo, bmesh choose-ideas, bmesh build your-repo 001, bmesh mesh-status
```

- **On Windows**, `bmesh init --service` ends with three lines for `cmd` (not PowerShell, which reads the quotes differently):

```
schtasks /Create /TN buildermesh /SC ONLOGON /RL LIMITED /F /TR "\"C:\Users\you\go\bin\bmesh.exe\" serve --restart --detach --config \"C:\Users\you\AppData\Roaming\bmesh\config.toml\""
schtasks /Run /TN buildermesh
rem in cmd: bmesh serve then starts at every logon with no window; its log is C:\Users\you\.local\state\bmesh\serve.log
```

- The first line makes a task that runs the supervisor (see [`bmesh serve`](#bmesh-serve)) at every logon with your rights. The second starts it now. The config path goes in `--config`, because a task sets no environment. A path with spaces passes.
- The task's process returns at once (`--detach`), so no console window stays open and the task's run time limit never applies. `taskkill /IM bmesh.exe /F` stops it (and every other bmesh on the box). `schtasks /Delete /TN buildermesh /F` removes the task.

See also: [Several boxes](#several-boxes), [Hosts](#hosts), [Config](#config), [The plugin](#the-plugin).

Internals: [internals](internals.md#quickstart)

### `bmesh serve`

```
bmesh serve [--listen ADDR] [--restart [--detach]]
```

Runs the daemon and the map, and on a single box the hub too. Run one `bmesh serve` on every box, and keep it running.

| Flag | Default | Meaning |
|---|---|---|
| `--listen ADDR` | `[serve] listen` (the map at `http://127.0.0.1:3774/`) | The map's HTTP address. |
| `--restart` | off | Supervise serve: run it as a child and start it again after every exit but 78. |
| `--detach` | off | Windows only, with `--restart`: start the supervisor with no window and return. Alone it is a usage error (exit 2). |

**What it does.**

1. Prints `bmesh serve: upgrade notes pending since v0.1.0: run bmesh update` when notes are pending (see [Upgrading](#upgrading)).
2. Checks the config at start (see [`bmesh config check`](#bmesh-config-check)); a refused config exits 78.
3. With `[mesh] nats` `""` (a single box), embeds the hub on `[mesh] listen` (default `127.0.0.1:4222`). Every other command connects to it there. The embedded hub's store is in the cache dir (see [Synopsis](#synopsis)).
4. Runs the daemon and the map. The daemon keeps the binary current, or writes the notice (see [Updates](#updates)).

**Keeping serve running.** `bmesh serve --restart` supervises `bmesh serve` on any OS, a Linux box without systemd too. It runs serve as a child and starts it again after every exit but 78:

- Exit 78 (a refused config): it logs `serve exited 78 (config check failed); not restarting: fix the config and start it again` and exits 78 itself.
- Otherwise after 5 seconds, or 60 once five runs in a row lasted under a minute each, until a run outlasts that.

Ctrl-C or SIGTERM stops the child, then the supervisor. On Windows the jobs the child held go back to the inbox when their ack wait runs out.

The supervisor's own lines (`<time> bmesh serve --restart: started <exe> (pid N)`, `serve exited (exit N)`, `starting again in 5s`) go beside the child's output: on stderr on Unix. On Windows (no journal) they go to `<state dir>/serve.log`, which is moved to `serve.prev.log` (replacing it) before it passes 10 MB.

`--detach` starts the supervisor again with no window, prints `bmesh serve: supervisor started with no window (pid N); log in <path>` and returns at once.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| `[hub] role` is set and `[mesh] nats` is empty: it would embed a second server beside `bmesh hub` | 3 | `serve: [hub] role is <role>, so bmesh hub runs this box's server: set [mesh] nats to it (nats://<[hub] listen>) rather than embed a second` |
| The config check refuses the config | 78 | `bmesh: config check failed: <why>` |
| `--detach` without `--restart` | 2 | `usage: bmesh serve [--listen ADDR] [--restart [--detach]]` |

**Details.**

- On a single box, a command run with nothing listening fails with `bmesh: no hub at 127.0.0.1:4222: is bmesh serve running?`.
- A `listen` on port 0 (`serve` picks a free port) is refused, since the other commands can't know which port.
- [`bmesh init`](#bmesh-init) `--service` writes the unit that runs serve (on Windows, the logon task running `serve --restart --detach`).

See also: [`bmesh hub`](#bmesh-hub), [Updates](#updates), [Map](#map).

### `bmesh hub`

```
bmesh hub
```

Runs this box's NATS hub or leaf from `[hub]` until stopped. It is nats-server v2.15.1 as a library, configured from `[hub]` (see [Config](#config)); there is no `nats.conf` to write. It runs as its own process, apart from `bmesh serve` (see [Several boxes](#several-boxes)).

**What it does.**

- **Role hub:** it serves clients on `listen` with the token, and leaves on `leaf_listen` (user `leaf`, the token as password). It stores JetStream in `[hub] store` within `max_file_store` and `max_memory_store` (defaults in [Config](#config)).
- **Role leaf:** it serves this box's clients on `listen` and dials the hub at `remote` forever.
- It is named `[host] name`, logs on stderr (the journal under systemd) and stops on SIGTERM or Ctrl-C.
- **It follows serve to each release** (not on Windows). Once a minute it reads this box's `bmesh serve` record. When serve runs a release (`v0.3.3`, not a commit past one such as `v0.3.2+3`) newer than the hub's binary, and the file at the hub's executable path is no longer the one it started from, it logs `bmesh hub: serve runs v0.3.3, restarting as /home/me/.local/bin/bmesh`, stops the server as on SIGTERM (JetStream flushes its store) and re-execs that path with the same arguments. Every client and leaf reconnects by itself within about a second; a JetStream request in flight at that moment fails once.

**Refusals.** `bmesh hub` refuses to start (exit 3), naming the key, with:

- `[hub] role` unset: `hub: [hub] role is unset: set it to hub or leaf (docs/manual.md: Several boxes); a single box needs no bmesh hub, since bmesh serve embeds it`;
- `listen` unset, or `listen` or a hub's `leaf_listen` on every interface (`0.0.0.0`, `::` or no host): `hub: [hub] <key> is unset: want <tailnet ip>:port on a hub, 127.0.0.1:4222 on a leaf`, or `hub: [hub] <key> "<addr>" listens on every interface: name the tailnet address (or 127.0.0.1 on a leaf), since the token is the only auth`;
- a leaf without `remote`: `hub: [hub] remote is unset: a leaf dials the hub's leaf port, nats://<hub tailnet ip>:7422`;
- `[mesh] token_file` unset, missing or empty: `hub: [mesh] token_file is unset: the hub and every box share one token (docs/manual.md: Several boxes)`, `hub: [mesh] token_file <path>: <error>` when it can't be read, or `hub: [mesh] token_file <path> is empty`.

A config the config check refuses exits 78.

**Details.**

- **Stale hub.** Off a release, `bmesh hub` keeps its starting binary until its service is restarted: a box on `[update] track = "latest"`, after `bmesh update --latest`, or on Windows, where it never restarts itself. When it runs an older binary than `bmesh serve`, [`bmesh update`](#bmesh-update) (after its other lines), [`bmesh hosts --here`](#bmesh-hosts---here) and [`bmesh mesh-status`](#bmesh-mesh-status) print `hub runs v0.1.0, serve v0.2.0: restart bmesh-hub` (a release when stamped, else eight characters of revision). On Linux and macOS the line lasts until the hub's next check once serve runs a release; on Windows a release's notes can restart it with a `run` step (see [Upgrade notes](#upgrade-notes)).
- [`bmesh init`](#bmesh-init) `--service` writes its unit on Linux and macOS. On Windows you run `bmesh hub` yourself.

See also: [Several boxes](#several-boxes), [`bmesh serve`](#bmesh-serve).

### `bmesh config check`

```
bmesh config check
```

Loads this box's `config.toml` and `repos.toml` with this binary, as every command does, and prints whether this binary takes them. [`bmesh update`](#bmesh-update) runs it with the new binary before the swap.

**Output.** `config ok: <path>`. A key this binary ignores gets a line on stderr.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| This binary refuses the config | 3 | `config check failed: <why>`, naming the key |

**Details.** `bmesh serve` (its supervisor too) and `bmesh hub` run the same check at start. Refused, they print `bmesh: config check failed: <why>` and exit 78 (`EX_CONFIG`). The systemd units `init --service` writes don't restart on it; launchd restarts serve at most every 5 minutes (see [`bmesh init`](#bmesh-init) and [Config](#config)).

See also: [Config](#config), [Upgrading](#upgrading).

### `bmesh t3 login`

```
bmesh t3 login [--server URL] [--callback-port 52299] [--timeout D]
bmesh t3 login --status
```

Signs bmesh in to this box's T3 as an MCP client, for the `t3` runner. Only T3's OAuth sign-in gives a session a runtime-mode ceiling above `approval-required`, picked by the operator when approving. So a person approves each box's sign-in, and again every 30 days (the token has no refresh).

| Flag | Default | Meaning |
|---|---|---|
| `--server URL` | `[runner] t3_server` | The T3 server. |
| `--callback-port N` | `52299` | The local port the browser is sent back to. The fixed port keeps the tunnel command the same every month. |
| `--timeout D` | `1h` | How long to wait for the approval. |
| `--status` | off | Print the sign-in and change nothing. |

**What it does**, in order:

1. Registers bmesh as a public OAuth client with T3.
2. Prints the authorize URL and how to approve it from a phone or laptop:
   - `ssh -N -L <port>:localhost:<port> <box>` for the callback, plus the server's port when it listens on loopback (`ssh -N -L 3773:127.0.0.1:3773 -L 52299:localhost:52299 k16`);
   - a pairing code from `t3 auth pairing create` on the box, if that browser isn't paired with its T3;
   - the access level: **Auto**, or **Full access** on a box whose `t3_runtime_mode` is `full-access`.
3. Takes the redirect on `localhost:<port>` (127.0.0.1 and ::1), or the browser's final URL pasted in. It refuses another login's state. A tunnel that shows T3 as `localhost` still signs in a bmesh on `127.0.0.1`.
4. Writes `<config dir>/t3-token.json` beside `config.toml` (mode 0600) and prints `signed in: <host>: <access>, 30 days left (token in <path>)`.

**Output.** `--status` prints `<host>: <access>, N days left`, then the server, scopes and expiry.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| The approved ceiling is below `t3_runtime_mode` | 3 | `this sign-in allows <access>, below [runner] t3_runtime_mode <mode>: leads won't start; run bmesh t3 login and approve <mode>` |
| `--server` is not a URL with a host | 2 | `--server "<url>": want a URL like http://127.0.0.1:3773` |
| No approval within `--timeout` | 5 | `no sign-in within <timeout>` |
| `--status` with no sign-in | 3 | `no T3 sign-in on <host>; run bmesh t3 login` |
| `--status` with an expired sign-in (after its lines) | 3 | `T3 sign-in missing or expired; run bmesh t3 login` |

**What needs a sign-in.** With the `t3` runner, [`bmesh lead start`](#bmesh-lead-start), [`bmesh build`](#bmesh-build) and [`bmesh ideate`](#bmesh-ideate) refuse (exit 3, before anything is recorded) a sign-in that is:

- missing or expired: `bmesh: T3 sign-in missing or expired; run bmesh t3 login`;
- for a server other than `t3_server`;
- below `t3_runtime_mode`: `bmesh: T3 sign-in allows auto, below [runner] t3_runtime_mode full-access; run bmesh t3 login and approve full-access`.

A lapsed sign-in stops new leads on that box, not running ones, though the daemon's poll of them fails until the next login.

**Expiry warnings.** The daemon of the box holding the token pushes `T3 sign-in on <host> expires in N days: bmesh t3 login` (tag `key`) 7, 3 and 1 days before expiry, once each across restarts. It checks hourly with `[notify] ntfy` or `command` set, whatever `push` lists (see [Pushes](#pushes)). A daemon down past a mark sends only the nearest, and a new login starts over. `bmesh mesh-status` closes with a line for it under 7 days, or `expired` (see [`bmesh mesh-status`](#bmesh-mesh-status)).

See also: [Runners](#runners).

Internals: [internals](internals.md#t3-sign-in)

### `bmesh version`

```
bmesh version
```

Prints the commit the binary was built from: `bmesh <rev8> <vcs.time> [modified]`, or `bmesh unknown` when it has none (a `(devel)` build).

**Output.**

- A tagged module install (`go install …/cmd/bmesh@v0.2.0`, or `@latest` once the module has a release, which go resolves to its tag) prints the release alone, `bmesh v0.2.0`.
- A self-update at or past a release tag (see [`bmesh release`](#bmesh-release)) prints the release first: `bmesh v0.1.0 (9a5e181a 2026-10-16T22:38:04Z)` on the tagged commit, `bmesh v0.1.0+3 (<rev8> <time>)` three past it.

**Details.** `version` reaches no hub and never runs `init`. Each daemon's digest title carries its revision (`bmesh <host> <rev8>: …`).

See also: [Updates](#updates).

### `bmesh update`

```
bmesh update [--repo R] [--latest] [--answer KEY=VALUE]... [--defaults] [--apply [--yes]]
bmesh update --plan [--json]
bmesh update --walk --from REV [--to REV] [--dir D] [--before-swap]
bmesh update --notice
```

Builds the target (see [Updates](#updates)) over this binary and swaps it in, on any box, daemon or not. It runs the same build and swap as the daemon's self-update, on demand.

| Flag | Default | Meaning |
|---|---|---|
| `--repo R` | `[update] repo`, else the registry row whose `go.mod` declares this binary's module | The registry name of the repo to build from. |
| `--latest` | off | Go to the code head this once, whatever `track` says, to take a fix before the batch's release. |
| `--answer KEY=VALUE` | none | Answer an upgrade ask. The key is as the ask names it, `[update] track`, or `track`. Repeatable. |
| `--defaults` | off | Take every ask's default. |
| `--apply` | off | Run the steps without asking. |
| `--yes` | off | With `--apply`: also run a `confirm` step without a terminal. |
| `--plan` | off | Print the walk to the target and change nothing. |
| `--json` | off | With `--plan`: print the walk as JSON. |
| `--walk` | off | Walk a range of notes by hand with this binary. |
| `--from REV` | none | With `--walk` or `--plan`: the revision or release walked from. |
| `--to REV` | this binary's revision | With `--walk` or `--plan`: the revision walked to. |
| `--dir D` | the repo `update` reads main from | With `--walk` or `--plan`: the repo the notes are read from. |
| `--before-swap` | off | With `--walk`: check the config and answer the asks only, as before the swap. |
| `--notice` | off | Print the notice file only (see Details). |

**What it does**, in order:

1. Picks the repo. It builds from a bmesh-owned checkout of the repo's main, never the operator's.
2. Finds the target. A binary at the target, or past it, prints `up to date`, or past the newest release `ahead of v0.2.0, up to date`, and goes on from step 6. After `--latest`, the next `update` follows `track` again without going back.
3. Builds the target and runs the build's `version`.
4. The new binary checks the config and answers the asks:
   - **The config check** loads this box's `config.toml` and `repos.toml` with the new binary ([`bmesh config check`](#bmesh-config-check)). A refused config keeps the old binary, so a box never restarts into a binary that won't load its config. A start-over walk skips the check: it moves the config aside.
   - **The asks** come one line each at a terminal: `track: boxes now update to releases, not every landing. Follow releases or every landing? [release]/latest`. Enter takes the default; an answer outside the choices is asked again. Without a terminal every ask needs `--answer <key>=<value>` or `--defaults`; else nothing is written and `update` refuses, keeping the old binary.
   - Answers go into `config.toml` (the old file kept as `config.toml.prev`, and put back when the new binary then refuses it). A registry ask's answer goes into every row of `repos.toml` (kept as `repos.toml.prev`), which the admin publishes ([`bmesh repos publish`](#bmesh-repos-publish)) or another box proposes.
5. Moves this binary to `bmesh.prev` beside it, puts the build at its path, and prints `<old> -> <new>`.
6. Runs the notes' steps for this box (see Output). A run on the new binary, now `up to date`, resumes after the last step done.
7. Prints the restart line (below), then the stale-hub line when the hub runs older (see [`bmesh hub`](#bmesh-hub)).
8. Once the binary is current (`up to date`, `ahead of …`, or after a swap from either source), removes the notice file.

**No repo row** (no `--repo`, `[update] repo` or own-module row, as after a first install): `update` installs from the module's `main` with `go install` instead, keeping `bmesh.prev` as above. After that swap it prints `<old> -> <new>`, then `update the buildermesh plugin too: claude plugin marketplace update buildermesh && claude plugin update buildermesh@buildermesh (then restart Claude Code)`, since the skills name commands only a matching binary has.

**Output.**

- The swap: `<old> -> <new>` (`v0.1.0 -> v0.2.0`; under `--latest`, `v0.2.0 -> v0.2.0+2 (<rev8>)` two commits past the release), or `up to date`.
- The walk opens with `<from> -> <to>: 1 ask, 1 step` (a start-over adds `<release> starts this box over; it loses ...`). After the swap and the usual `<old> -> <new>`, the new binary runs the steps.
- Each run or check step for this box (by its `os`, and its `when` against the config as it now is) is printed as `v0.2.0#2 restart bmesh-hub: ` plus its command. A `when` that doesn't hold prints `skip <release>#<n>: ...`.
- The step runs in `sh -c` (`cmd /C` on Windows) with `BMESH_CONFIG`, `BMESH_CONFIG_DIR`, `BMESH_STATE_DIR`, `BMESH_STORE` (the embedded hub's store, as the config names it), `BMESH_FROM` and `BMESH_TO` set.
  - With `--apply` it runs without asking.
  - At a terminal without `--apply`, each step asks `Run it? [Y/n]`.
  - With neither, the walk stops there: `the steps from v0.2.0#2 wait: run bmesh update --apply (with --yes for the ones that ask first)` (exit 0).
  - A `confirm` step asks `Run it? [y/N]` at a terminal and needs `--yes` without one.
  - A check step's output must show its `expect`.
- A finished walk ends `at v0.2.0`.
- **The restart line.** When this box's daemon runs another revision than the binary now at the path, `update` prints `this box's daemon still runs <old8>; restart it so it runs <new8>: <command>`. `<command>` restarts the unit `init --service` writes:
  - Linux: `systemctl --user restart bmesh`;
  - macOS: `launchctl kickstart -k gui/$(id -u)/com.buildermesh.bmesh`;
  - Windows, whose supervisor runs detached from the logon task: `taskkill /F /IM bmesh.exe & schtasks /Run /TN buildermesh` (for `cmd`; it stops every `bmesh.exe`, supervisor included, then starts the task again);
  - another OS: `stop bmesh serve and start it again`.
- No hub, no daemon on this box, or a daemon of unknown revision: no restart line. A daemon run by hand gets a command that doesn't apply; stop and start it yourself.
- `--plan` without `--json`: `<from> -> <to>: 1 ask, 1 step` and a line per step.
- `--plan --json`: `{"from","to","releases","start_over","config","asks","steps","confirm","plan"}`. `config` is `ok`, the refusal, or `start-over`. `plan` holds every step for this box's OS with its `id` (`v0.2.0#1`), `release`, `kind` and fields, `done`, its `answer` once given, and `applies` (whether its `when` holds now).

**Refusals.**

| When | Exit | Message |
|---|---|---|
| The target doesn't contain this binary (a rollback, or another line) | 3 | `<repo>'s main (<target>) does not contain this binary's <label>; left alone` |
| The build's `version` doesn't name the target's commit | 3 | `bmesh: update: built binary reports <x>, not <new8>; keeping this binary` |
| No repo row, and the binary doesn't say its module | 3 | `no repo to update from: pass --repo or set [update] repo` |
| The new binary refuses the config | 3 | `bmesh: config check failed: config <path>: [runner] t3_server is empty: ...` then `bmesh: keeping this binary` |
| An ask has no answer, without a terminal | 3 | `1 ask waits on an answer ([update] track): pass --answer <key>=<value> for each, or --defaults; nothing changed` |
| A `confirm` step without `--yes`, without a terminal | 3 | `step v0.1.0#1 asks first: pass --yes to run it` |
| A step fails; the walk stops | 1 | `step v0.3.0#1 failed: ...; fix it and run bmesh update again, which goes on from there` |
| A flag combination that doesn't go together (below) | 2 | the usage line |

The flags that don't go together: `--notice` with any other `update` flag; `--before-swap` without `--walk`; `--walk` with `--plan`; `--yes` without `--apply`; `--from`, `--to` or `--dir` without `--walk` or `--plan`; `--walk` (or `--plan` with `--from`, `--to` or `--dir`) with `--repo` or `--latest`.

**Details.**

- `update` never restarts the daemon: a restart kills its running jobs, and only the daemon's own drain hands them back (see [Updates](#updates)). A daemon with `[update] repo` takes the target by itself within a couple of minutes; one without (a Mac's) runs the old code until restarted.
- On Windows a `bmesh update` run by hand can race the daemon's own update.
- A binary installed as `go install <module>/cmd/bmesh@vX.Y.Z` whose release `main` lacks: `update` installs the target as for an unknown revision.
- `update` prints its restart line instead of the revision hint (see [Updates](#updates)).
- **Driven by an agent:** `bmesh update --plan --json` prints the walk, then `bmesh update --defaults --apply --yes` (or `--answer` for each ask) walks it without a terminal.
- **`--walk`** (`bmesh update --walk --from <revision or release> [--to <revision>] [--dir <repo>]`) walks a range by hand with this binary: what `update` runs on the new binary. `--plan` takes the same `--from`, `--to` and `--dir`.
- **`bmesh update --notice`** only prints the notice file, `<state dir>/update.txt` (see [Updates](#updates)), and the pending-notes line (see [Upgrading](#upgrading)). It uses no network, runs no `init`, and prints nothing (exit 0) when the file is absent. The plugin's session hook runs it after `hosts --here` (see [The plugin](#the-plugin)).

See also: [Updates](#updates), [Upgrading](#upgrading), [Upgrade notes](#upgrade-notes), [`bmesh version`](#bmesh-version).

## Proposals and builds

Propose specs, answer them, build them, and release what landed.

### `bmesh ideate`

```
bmesh ideate <repo> [--idea] [--model M] [--effort E] [--context <text|->] [--force]
```

Starts a proposal round on one ship repo. A round is two threads, an engineer and a UI/UX engineer. Each thread writes three to five ranked half-page specs into `docs/proposals/` and lands them on `main` as `proposed`.

| Flag | Default | Meaning |
|---|---|---|
| `--idea` | off | The round is your one idea, given in `--context`; only the engineer starts. |
| `--model M` | `[models] proposer` | The model both threads run on. |
| `--effort E` | `[models] proposer_effort` | The threads' effort. |
| `--context <text\|->` | none | What you ask of the round; `-` reads stdin. |
| `--force` | off | Start a round while another round of the repo is still open. |

**What it does.** `ideate` needs the repo's registry row (`host`, `ux_host`, `verify`, `surfaces`). Before anything starts, it checks the refusals below, both hosts included. Then, from `main`, it:

1. makes `<[runner] worktrees>/<repo>/<repo>-round-<N>-eng` and `-ux` on branches of those names (an existing branch or path is refused, untouched);
2. claims a block of spec ids for each child on the mesh, as [`bmesh spec next-id --count 5`](#bmesh-spec-next-id) does (three for an `--idea` thread), and renders the two briefs, each with the first id of its child's block, so they never collide with each other or with any other writer; a child that writes fewer leaves a gap;
3. starts both threads through the runner, the engineer on the row's `host`, the UI/UX engineer on `ux_host` (`""` = `host`; `host` `""` = this box), with `--model` (default `[models] proposer`) at `--effort` (default `[models] proposer_effort`), titled `[ideate] <repo> round <N>: engineer|UI/UX`;
4. prints the round as JSON (see Output).

**Output.** `{"path","round","eng":{"path","thread","branch","worktree","host"},"ux":{...}}`. With `--idea` the output has no `ux`.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| The repo has no registry row (a path without one) | 2 | `<name> is not in repos.toml; a round needs its row (host, ux_host, verify, surfaces)` |
| `--idea` without `--context` | 2 | `--idea needs the idea itself in --context` |
| The row's `mode` is not `ship` | 3 | `refused: <repo> is <mode>; proposal rounds are for ship repos only` |
| An open round (a live node) | 3 | `refused: <repo>-round-<N> is still open (eng working, ux working); wait for it, or pass --force to start another` |
| A host it cannot reach (both hosts are checked before anything starts) | 3 | `refused: host <host> (the eng\|ux thread's) is unreachable from <this box>: <error>` |
| A `main` it cannot level with origin | 3 | `refused: main in <path> is not level with origin: <git error>` |
| Five or more `proposed` specs on `main`; they wait on review | 3 | `refused: <repo> has <n> proposed specs waiting on review (<NNN, NNN>); review them first` |
| A round branch that already exists | 3 | `refused: branch <b> already exists in <path>` |
| A round worktree path that already exists | 3 | `refused: <wt> already exists; remove it first` |

To level `main`, `ideate` runs `pull --ff-only origin main` when the main checkout is on `main`, else `fetch origin main:main`. It never resets or stashes.

**Details.**

- `--force` starts a round anyway, with blocks of its own, running the repo's tests beside the first round.
- Briefs are kept under `<user cache dir>/bmesh/briefs/<round>-<child>.md`.
- On another host, `ideate` makes the thread, worktree and brief there over ssh. A child whose node is live or whose branch exists is refused there too.
- Each child starts whatever happens to the other. A child that doesn't start is marked `blocked` (`did not start: <error>`), the round reads `blocked` naming it, and the command exits 1. The other thread runs on, and its pitch arrives when the daemon resettles the round.
- With the `t3` runner, a round's thread runs in the bmesh worktree, filed under the target repo's T3 project: T3 binds only a project's own worktrees.

**Nodes.** The round is node `<repo>-round-<N>`. `<repo>` is the registry name's node token (see [Names](#names)), and `N` is one past the repo's highest round. Its children are `<repo>-round-<N>.eng` and `.ux`. Each child has an inbox, a thread and a bmesh worktree.

**`--context`** carries what you asked of the round (`-` reads stdin). Both briefs get `## The operator's focus` with the words as given, trimmed, before their closing. A repo's own template without `{{operator_context}}` gets the section appended. No words, no section.

**`--idea`** makes the round your one idea, given in `--context` (refused without it, exit 2):

- Only the engineer starts (`ux_host` is neither checked nor prompted for).
- Its brief gets `## The operator's idea` instead of the focus section: write that idea's spec, one half-pager or two or three in build order, and nothing else. Its opening, partner and numbering lines match (`{{round_ask}}`, `{{round_partner}}`, `{{ux_ids}}` for a repo's own engineer template).
- The `started` text reads `round <N> on <repo>: one idea, eng on <host>`, and the output has no `ux`.
- The round settles on the engineer alone; its pitch is the engineer's line.

**Reporting.** An engineer's node reports (`started`, `progress`, `ask`, `blocked`, `done`). The daemon polls it and wakes it like a lead. Its `done` needs only `--commit` (the commit on `main`) and carries its ranked list as `--text`.

**Settling.** Each minute the daemon settles every open round:

- **Both children `done`:** the round goes `done`, its text the merged pitch (the engineer's list, then the UI/UX engineer's), and the daemon publishes `done` from the round.
- **One child `blocked`:** the round goes `blocked` (`<child> blocked: <text>`, then the other's pitch once done).

The daemon rechecks a blocked round each minute. When the other child's pitch arrives, the daemon publishes `blocked` again with it. When a blocked child is unblocked and both children are `done`, the round goes `done` after all.

**Pushes.** The round's `done` pushes as `<repo> round N proposed` (priority default, tag `memo`, the pitch as body). Its `blocked` pushes as `<repo> round N blocked` (high, `no_entry`). Each push opens the engineer's thread on a tap when `[console]` resolves it and, like a lead's push, needs the event in `[notify] push`. An engineer's `ask` pushes like a lead's (`<path> asks`, with its thread's link). Its `done` and `blocked` reach you through the round.

See also: [`bmesh choose-ideas`](#bmesh-choose-ideas), [The ledger](#the-ledger), [Pushes](#pushes), [Briefs](#briefs).

Internals: [internals](internals.md#proposal-rounds)

### `bmesh choose-ideas`

```
bmesh choose-ideas <repo> "<reply>"
```

Applies your reply to a round to the repo's specs and lands it on `main`. You type the reply in plain words: `"1 and 3 yes, 2 smaller, 4 no, 5 later, 3 on opus"`.

**Clauses.** The reply splits into clauses at commas, semicolons and line breaks. Once a clause's phrase has begun, it also splits at a number followed by a spoken verdict: `3 yes and 4 no` and `11 smaller and 12 no` are two clauses each; `4 no because 5 covers it` is one. A clause is its leading numbers (joined by `and` or the like), which name specs, and the phrase after them.

**Numbers.**

- Three or more digits (`012`, as ids are written) name that spec.
- One or two digits are a position in the pitch when there is one, else a spec id. `1 and 3 yes` to a pitch of 012 to 016 means its first and third items. `45 yes` to a five-item pitch is reported as no position, so the spec is `045`. `6` to a pitch of 001 to 003 and 006 to 010 is both a pitched id and the sixth item, and is reported as ambiguous.
- `all` among the numbers names every spec of the pitch (`all yes`, `all later on opus`); with no pitch it is reported.

**The pitch** is the text of the repo's newest settled round (`done` or `blocked`) on the mesh, the one you saw last. The command reads it whole, line by line, so a position means the same spec in every reply. A line whose spec has moved on is reported as left unchanged.

- A line is the id it starts with. A line holding two or more items of the shape `NNN title (kind, size, judge):` after sentence ends (a `.;!?` before a space; `v1.2` is fine), with the title free of sentence ends, is a list typed on one line and yields them all. An id cited inside a why is not an item.
- An open round, even with a finished child, is not a pitch yet. A settled round whose text names no spec (one blocked before a pitch) is passed over for an older one.
- With no mesh, or no settled round with a pitch, only ids count, and the output's first line says so.

**The phrase's first word decides.**

| First word | Moves the spec to |
|---|---|
| `yes`, `accept`, `ok` | `accepted` |
| `no`, `reject` | `rejected`, the rest of the phrase as `reason` |
| `later` | `later` |
| `on fable\|opus\|sonnet` alone | nothing: it sets `judge` |
| any other | `adjusting`: the whole phrase is an instruction, recorded as `Adjust (<date>): <phrase>` under its Proposal heading, for the thread that reworks the body |

Reasons and instructions are kept as written, so a verdict word or number inside one is just a word (`2 make it work with no network` adjusts 002). `on <judge>` right after `yes` or `later` sets the judge as well. Any further words after `yes`, `later` or `on <judge>` (`yes but smaller`) make the phrase an adjustment.

**A verdict said first** is `yes`, `accept`, `build`, `go`, `approve`, `no`, `reject` or `later` (optionally followed by `to`), or `work on`, then its numbers and, for `reject`, the reason (`accept 012`, `reject 2 too big`, `work on 1, 2 and 3`, `yes to all`).

- It carries to the clauses of bare numbers after it.
- A phrase after its numbers that is itself a verdict said first is a clause of its own (`accept 012 and reject 013`).
- After two or more numbers, a phrase that is not the verdict's own belongs to the last number, and the verdict covers the others. After `reject`, that is a phrase starting with a spoken verdict; after `yes` or `later`, anything but `on <judge>`. So `reject 2 and 3 yes` and `accept 012 and 3 smaller` split, while `reject 2 and 3 too big` rejects both and `yes 1 and 3 on opus` accepts both on opus.

**What moves.**

- A clause of bare numbers that no verdict said first reaches takes the verdict of the clause right after it (`1, 2 and 3 yes`). A clause with no number in between leaves them unmatched.
- A spec named in several clauses gets the last move and every judge.
- Only `proposed`, `later` and `adjusting` specs move. A spec in another status is reported and left alone.
- A clause with no number, no phrase, or a number matching nothing is printed as `unmatched: ...`, never guessed at.

**What it does**, in order:

1. levels `main` (as [`bmesh ideate`](#bmesh-ideate) does);
2. makes a worktree `<[runner] worktrees>/<repo>/<repo>-review-<hex8>` from it and applies the verdicts there;
3. checks the commands accepted specs define (below);
4. commits `Review: 012 accepted, 013 later`;
5. fast-forwards `main` (`merge --ff-only` in the main checkout when it is on `main`, else `fetch . <branch>:main`) and pushes to origin;
6. removes the worktree and its branch.

**Commands an accepted spec defines.** Before the commit, the command checks these for each spec turned `accepted`.

- A spec defines a command where its Proposal has a bold run that is a bmesh synopsis and nothing else: ``**`bmesh release <repo> [--minor]`**``, optionally closed inside the bold by `.`, `:`, `,` or `;`. The command is the synopsis's leading bare words: `bmesh spec verified [--commit C]` defines `spec verified`.
- A bold run that goes on in words (``**`bmesh build` repeats the warning**``), a plain code span, and a bold run with a backtick right outside it (a quote inside a code span) define nothing.
- A synopsis going straight to a flag (`bmesh build --hold land|now`) adds a flag; flags aren't checked.

Each command is compared with the commands this binary's `bmesh help` lists (`main`'s, on an up-to-date box) and with every other spec `accepted`, `adjusting` or `building` after the review. A match prints `warning: 048 defines bmesh release, which 046 (accepted) also defines` or `warning: 046 defines bmesh version, which main already has` after the unmatched items. Two specs accepted in one reply that define one command warn once, from the higher id. The reply still lands, and you decide. A spec that changes a command on purpose warns at every accept, naming `main`. `build` repeats the warnings at dispatch (see [`bmesh build`](#bmesh-build)).

**Output.** One line per spec (`NNN title: old -> new [on judge] [(note)]`), then the unmatched items, then the warnings below, then `landed <sha8> on main: Review: ...` (or `nothing to land`).

**Refusals.**

| When | Exit | Message |
|---|---|---|
| `main` can't be levelled with origin | 3 | `refused: main in <path> is not level with origin: <git error>` |
| `main` won't fast-forward; the review is kept on its branch | 3 | `refused: main in <path> did not fast-forward: <git error>; the review is committed on branch <branch>` |
| Two specs share an id, on `main` or in the commit about to land | 3 | `refused: <file> and <file> share id NNN; renumber the newer with bmesh spec next-id` |

See also: [`bmesh ideate`](#bmesh-ideate), [The ledger](#the-ledger), [`bmesh spec set`](#bmesh-spec-set).

### `bmesh spec`

```
bmesh spec list [--repo R] [--status S] [--json]
bmesh spec next-id [--repo R] [--count N] [--peek]
bmesh spec set --id NNN --status S [--repo R] [--from A,B] [--field K=V]...
bmesh spec template [--repo R]
bmesh spec verified [--spec NNN] [--commit C] [--write]
bmesh spec steps [<NNN>] [--repo R] [--tick N,N [--note W] | --check [--wait D]]
```

Reads and writes a repo's ledger of specs (see [The ledger](#the-ledger)). `list`, `next-id`, `set` and `steps NNN` read the frontmatter and refuse a ledger that breaks its rules (exit 1).

`--repo` is a registry name or a path (default the current directory; a row's path counts as that row, as in every command taking a repo).

#### `bmesh spec list`

```
bmesh spec list [--repo R] [--status S] [--json]
```

Prints the repo's specs.

| Flag | Default | Meaning |
|---|---|---|
| `--repo R` | the current directory | The repo. |
| `--status S` | every status | Only specs in this status; a status outside the ledger's is refused (exit 2). |
| `--json` | off | Print JSON. |

**Output.** One row per spec, `NNN  status  size  kind  lens  title` (`?` for a missing field), sorted by id. `--json` prints an array of each spec's frontmatter plus `path`.

#### `bmesh spec next-id`

```
bmesh spec next-id [--repo R] [--count N] [--peek]
```

Claims the repo's next spec id on the mesh and prints it (`066`), or a block of `N` with `--count` (`066-070`). Every writer of a spec claims its id this way, then lands the spec, so two writers never land the same number.

| Flag | Default | Meaning |
|---|---|---|
| `--repo R` | the current directory | The repo; a worktree counts as its main checkout. |
| `--count N` | 1 | Claim a contiguous block of `N` ids; under 1 is refused (exit 2). |
| `--peek` | off | Print what the next claim would get, claiming nothing. |

**What it does.**

1. Reads the highest id on `main`: `origin/main`'s after a `git fetch origin main` when the repo has an origin and the fetch works, else the checkout's ledger (`000` for an empty one). A ledger that breaks its rules is an error (exit 1).
2. Sets the repo's counter in `bm-specids` (created on first use) to `max(counter, main's highest) + N` with a compare-and-swap, retrying when another writer moved it first, and prints the first id it claimed. A counter behind `main` (a spec landed by plain git) catches up to it.

A claimed id that is never landed is a gap; nothing hands it out again.

**With no mesh** (the hub can't be reached) it prints `main`'s next id, claims nothing and says so on stderr: `next-id: no mesh, so 066 is not claimed: another writer may take it` (`066-070 are not claimed: another writer may take them` for a block).

#### `bmesh spec set`

```
bmesh spec set --id NNN --status S [--repo R] [--from A,B] [--field K=V]...
```

Sets a spec's status and fields in its file.

| Flag | Default | Meaning |
|---|---|---|
| `--id NNN` | required | The spec. |
| `--status S` | required | The new status. |
| `--repo R` | the current directory | The repo. |
| `--from A,B` | any status | The statuses the spec may move from. |
| `--field K=V` | none | A field to set; repeatable. |

**What it does.** It rewrites the status and the given fields in place: an existing key keeps its line, a new one is added last, and every other byte stays. Then it prints the row.

**Refusals.** A refusal with exit 1 changes nothing.

| When | Exit | Message |
|---|---|---|
| `--id` is not a number | 2 | `--id "<x>" is not a number` |
| `--status` is outside the ledger's statuses | 2 | `--status "<s>" is not one of ...` |
| No spec with that id | 1 | `no spec NNN in <dir>` |
| Two specs share an id | 3 | `refused: <file> and <file> share id NNN; renumber the newer with bmesh spec next-id` |
| With `--from`, a status not listed (named) | 1 | `spec NNN is <status>, not <A or B>; left unchanged` |
| A write the ledger would read back differently (a duplicated `status:` key) | 1 | `spec NNN: writing status <S> would read back as <X>; left unchanged` |
| A key that isn't a word, or a value with a line break | 1 | `can't set "<key>" to "<value>": keys are words and values are one line` |

#### `bmesh spec template`

```
bmesh spec template [--repo R]
```

Prints the half-pager: the repo's `.bmesh/briefs/half-pager.md`, else the embedded one.

**Details.** In bmesh's own repo (its `go.mod` declares this binary's module), it adds an `## Upgrading` section before `## Needs from the operator` unless the text has one. The section holds the steps an install from the previous release needs, for any fleet, or `nothing`; a new config key is written as an ask (what changed, question, choices, default). `release` reads the section (see [`bmesh release`](#bmesh-release)).

#### `bmesh spec steps`

```
bmesh spec steps [<NNN>] [--repo R] [--tick N,N [--note W] | --check [--wait D]]
```

Reads, ticks and checks a landed spec's steps for the operator (the `operator_steps` field, see [The ledger](#the-ledger)).

| Flag | Default | Meaning |
|---|---|---|
| `--repo R` | the current directory; with no id, every registered repo | The repo. |
| `--tick N,N` | none | Tick off these step numbers. |
| `--note W` | none | Words to record with the ticks; only with `--tick`. |
| `--check` | off | Run the open steps' checks and tick those that pass. |
| `--wait D` | `30s` | How long to wait for another box's checks. |

**Reading.** `steps NNN` reads the spec from `main` (`origin/main` after a fetch). It prints `NNN <title>: X of Y open`, then each step as `<n>. [<box>] <text> (check: <command>) -- done <time> <box>: <note>` with the parts it has, or `NNN <title>: no operator steps`.

With no id, it prints `operator: <repo> NNN <title>: X of Y open (bmesh spec steps NNN --repo <repo>)` per landed spec with open steps (as `mesh-status` ends). It reads `--repo`'s ledger, or else every registered repo with a checkout here, from its local `origin/main`. It fetches a repo again when the repo has open steps, so a step ticked from another box drops out.

**Ticking.** `--tick 2,3` records `done: <time> <this box>` (with `--note`, the words as `note`) on those steps. It lands the change on `main` as `choose-ideas` lands a review (worktree `<repo>-steps-<hex8>`, commit `Steps: NNN 2, 3 done`) and prints `landed <sha8> on main: Steps: NNN 2, 3 done; X of Y open`. A step already done is named (`step 2 already done <when>`) and left alone. When every step named is done, nothing lands.

**Checking.** `--check` runs the check of each open step that has one. It runs the check here, in the repo's checkout, when the step names no box or this one. Else it sends a `check-step` job to the step's box (see [Hosts](#hosts)) and waits `--wait` (default 30s).

- It prints `<n>. <box>: exit 0`, `exit <code>: <the output's last line>`, `timed out after 2m0s`, a refusal, or, for a box that hasn't answered, `asleep` or `no answer in <wait>` followed by `; run --check again once it answers`.
- It ticks each step whose check exited 0 with `done: <time> <the box that ran it>` and the note `check passed`, one landing per box. When the other boxes can't be asked (no hub, a box not joined), the passes here still land, and the failure is reported after them.
- With `[hosts]` off, a step of another box is not run (`not run: hosts are off here; run bmesh spec steps NNN --check on <box>`).

**Refusals.**

| When | Exit | Message |
|---|---|---|
| `--tick` with `--check`, `--note` without `--tick`, or `--tick` or `--check` with no id | 2 | the usage line |
| A `--tick` number past the last step | 2 | `spec NNN has no step <k> (it has <n>)` |
| `--tick` on a spec with no steps | 3 | `refused: spec NNN has no operator steps` |

**Details.** Open steps block nothing: `done` and `release` never wait on them.

#### `bmesh spec verified`

```
bmesh spec verified [--spec NNN] [--commit C] [--write]
```

Composes a spec's `verified` line from the results recorded for the tree of the cwd worktree's HEAD, or of commit `C`. A lead runs it. `bmesh land` composes the same line at its step 5.

| Flag | Default | Meaning |
|---|---|---|
| `--spec NNN` | this node's spec | the spec whose file `--write` sets |
| `--commit C` | `HEAD` | compose from the results for this commit's tree; HEAD below reads as `C` |
| `--write` | off | set the line in the spec's file in this worktree |

**What it does.**

- It writes one entry per (kind, model). The newest result wins, whatever its args. Entries are sorted by kind, then by model.
- A (kind, model) with no result at the tree takes the newest model result whose patch-id equals HEAD's. HEAD's patch-id is that of the diff from HEAD's merge-base with `origin/main` (`main` without an origin) to HEAD. The entry is written `t:<HEAD's tree8>` and `c:<its own tree8>`: a review of the same diff on another base.
- A changed diff carries nothing, however small. A command kind's result (verify) never carries, because it is about its tree. Nothing carries when HEAD is on main.
- `--write` finds the spec's file by `[spec] glob`. It replaces a `verified:` line, or adds one before the closing `---`, whichever commit the line was composed for. So a line for a merged commit is rebuilt from its records, not typed. A line that wouldn't read back as the same entries isn't written.

**Output.** `verified: ...`, then `wrote <file>` under `--write`.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| `--commit` is not a commit | 2 | `--commit "<C>" is not a commit in <worktree>` |
| no results for the tree | 3 | `bmesh: no results for HEAD's tree; run the routines first` (`no results for <C>'s tree t:<tree8>` under `--commit`) |
| `--write` with no `--spec` and no node spec | 2 | `spec verified --write needs --spec NNN (no node spec resolves)` |
| `--write`, and the glob doesn't match one file | 3 | `spec <id> matches <n> files in <top>` |

See also: [The verified line](#the-verified-line), [Results](#results), [`bmesh land`](#bmesh-land).

### `bmesh build`

```
bmesh build <repo> <NNN> [--model M] [--effort E] [--path P] [--family F] [--hold land|now] [--force]
bmesh build --accepted <repo> [--model M] [--effort E] [--family F] [--hold land|now] [--force]
```

Dispatches an accepted spec to a lead: it records `building` on `main`, writes the build brief and starts the lead as [`bmesh lead start`](#bmesh-lead-start) does. `--accepted` dispatches every accepted spec of the repo. A coordinator runs it (the `build` skill's session, or the operator).

| Flag | Default | Meaning |
|---|---|---|
| `--accepted` | off | Dispatch every accepted spec, lowest id first, three leads at a time (see **Details**). |
| `--model M` | the spec's `judge` | The lead's model. A `judge` of `fable`, `opus` or `sonnet` reads as `claude-fable-5-1`, `claude-opus-5-5` or `claude-sonnet-5-5`. |
| `--effort E` | `[models] lead_effort` | The lead's effort. |
| `--path P` | `<repo>-<NNN>` | The lead's node path (single form only). |
| `--family F` | a fresh `<repo>-build-<N>` | Join build family `F`: one subject token (letters, digits, `-`, `_`); anything else is exit 2. |
| `--hold land\|now` | no hold | Write each lead's hold before the runner starts it (see **Details**). |
| `--force` | off | Pass the hold and the verify of `main` (see **What it does**). |
| `--json` | off | `--accepted` only: print its lines as JSON. A single `build` always prints JSON. |

**What it does**, in order. Nothing is recorded before steps 1 to 7 pass.

1. **Row.** No registry row is exit 2; a row not `ship` is exit 3.
2. **Spec.** It levels `main` as `ideate` does (see [`bmesh ideate`](#bmesh-ideate)) and reads the ledger there. A missing spec is exit 1. A spec not `accepted` is refused (exit 3), naming its status.
3. **Rulings.** A single `build` checks two of the fixer's rulings here; `--accepted` checks them per spec after steps 4 and 5 (see **Details**). `--force` doesn't pass them. See **Refusals**.
4. **Hold.** Once the spec reads `accepted`, it reads the repo's leads on every box and holds while the repo's last build run is still going (which families hold a build is in [Build families](#build-families)). A held single `build` is refused (exit 3). `--accepted` prints `held <family>: <why>` once (`--json`: `[{"state":"held","family","reason"}]`), exits 0 and starts nothing.
5. **Verify `main`.**
   1. The commit checked is main's code head (see [Updates](#updates)): a `Dispatch`, `Review` or `done` ledger commit, or with `code` set a README, document or art commit, gets no verify of its own.
   2. A `verify` result recorded for its tree answers: `verify main <sha8>: ok t:<tree8> (answered from the record)`. With none, one is carried from the commits between `main`'s tip and the code head, as `routine run` carries it (see [Carried verify](#bmesh-routine-run)), so a landing that ended in a docs commit answers from that commit's verify: `verify main <sha8>: ok t:<tree8> (carried from t:<old8>: nothing under code changed)`.
   3. Otherwise the repo's `verify` kind, as that commit defines it, runs inline in a bmesh worktree at that commit (`<[runner] worktrees>/<repo>/<repo>-verify-<hex8>`, removed after), as `land` runs it, and records the result: `verify main <sha8>: running at t:<tree8>`, then `... (recorded)`. When that commit names a `[setup]` (see [Setup](#setup)), it runs in the fresh worktree first; a setup that fails refuses the build (exit 3) with the tail of its output and records nothing, so a flaky registry never turns `main` red.
   4. A failing verify refuses the build (exit 3, nothing dispatched; see **Refusals**). A `main` with no verify kind prints `verify: <repo> defines none` and the build goes on.
6. **Model.** The model is `--model`, else the spec's `judge`; with neither, the build is refused.
7. **Node.** The path is `--path`, else `<repo>-<NNN>`. A node there that is neither `done` nor `blocked` (live, from here on) is refused (exit 3, `use --path <repo>-<NNN>-2`). A box at `[runner] max_leads` is refused too.
8. **Building.** `building` lands on `main` with `built_by` (the lead's thread id), `host`, `family`, and `branch` when the runner names branches ahead (`claude` and `tmux` do). The commit is `Dispatch NNN: building on <host>`, made in a bmesh worktree, fast-forwarded and pushed as `choose-ideas` lands (see [The ledger](#the-ledger)).
9. **Brief.** The brief (the spec file, the row's done rule and verify commands, the node path, the standard closing) is written to `<user cache dir>/bmesh/briefs/<path>.md` (see [Briefs](#briefs)). When `main` names a `[setup]`, its first section is `bmesh setup`.
10. **Start.** The lead starts through the runner with that thread id, titled `[build] <repo> <NNN>: <title>`, as `lead start` would.

A runner that names the branch itself (`t3`) gets a second commit, `Dispatch NNN: branch <b>`. A failed start puts the spec back to `accepted` (`Dispatch NNN: did not start`), exit 1.

`--force` passes the hold and the red `main` (as `ideate --force` passes an open round), runs no verify and prints what it passed: `--force: past held: ...`, `--force: past main is red at ...` or `--force: past verify main <sha8>: no result for t:<tree8>`.

**Output.** A single `build` prints `{"path","thread","branch","worktree","model","spec","building","family"}` (plus `hold` under `--hold`). `building` is the commit of step 8; `warnings` is what `choose-ideas` warned at accept (a command `main` or another spec already defines), read again from the ledger. The verify lines of step 5 go to stderr; under `--accepted`, they go to stdout among its lines.

**Refusals.** Each message prints after `bmesh: `.

| When | Exit | Message |
|---|---|---|
| `--hold` other than `land` or `now` | 2 | `--hold "<g>": want land or now` |
| `--family` not one subject token | 2 | `--family: invalid family "<f>": want letters, digits, - or _, starting with a letter or digit` |
| No registry row | 2 | `<name> is not in repos.toml; a build needs its row (verify, remote, routines)` |
| Row not `ship` | 3 | `refused: <name> is <mode>; builds are for ship repos only` |
| No such spec on `main` | 1 | `no spec NNN on main in <path>` |
| Two specs share an id on `main`, or in the dispatch commit about to land | 3 | `refused: <file> and <file> share id NNN; renumber the newer with bmesh spec next-id` |
| Spec not `accepted` | 3 | `refused: spec NNN is <status>, not accepted` |
| Spec `ruled` and sized `L` or larger | 3 | the sized message below |
| `fix_of` chain three deep | 3 | the `fix 3` message below |
| Held | 3 | one of the `held:` messages below |
| `main` red | 3 | the `main is red` message below, then the last 30 lines of output |
| The setup before verifying `main` failed | 3 | `verify main <sha8>: setup failed: <why>; nothing recorded`, then the last 30 lines of its output (`<why>` is `exit status <n>`, `timed out after <d>`, or for a setup that dirtied the tree, `verify main <sha8>: setup dirtied the tree: <paths>; nothing recorded`) |
| No `--model` and no usable `judge` | 3 | `` refused: <err>; pass --model, or `NNN on <judge>` to bmesh choose-ideas `` |
| Live node at the path | 3 | `refused: path <p> is taken (state <s>); use --path <p>-2` |
| Box at `[runner] max_leads` | 3 | `<N> leads already working on this host` |
| Start failed | 1 | `<runner error>; the spec reads accepted again`, or `<runner error>; and the spec is left building on main: <err>` when putting it back failed |

- Held: `held: <family> is still running (<path> <state>, ...); watch it with bmesh watch --nodes <paths>` or `held: <family> has <N> feedback pending; drain it with bmesh feedback list/drain <family>`.
- `main` red: `main is red at <sha8> <subject>: verify fail t:<tree8> (<how>); build its fix with --force`.
- Sized: `refused: spec NNN is ruled and sized <size> (reversible: anything sized L is proposed, never built); set it proposed`, for example `refused: spec NNN is ruled and sized L (reversible: anything sized L is proposed, never built); set it proposed`.
- Fix chain: a spec's `fix_of` chain is each spec's `fix_of` (its first three-digit id) followed until a spec without one. Three deep is refused with `refused: spec NNN is fix 3 on one premise (NNN fixes MMM fixes KKK fixes JJJ) (premise: no third fix on one premise); set it proposed with the belief the fixes assumed`. Two deep builds.

**Details.**

*`--accepted`.* It dispatches every accepted spec, lowest id first, through the same steps; one refusal doesn't stop the next. It holds and verifies once per batch, and only when a spec is accepted. Then each spec in turn takes the rulings check, the two `waits` checks below, and then the model and node checks and the start. It prints one line per spec:

- `started NNN <title>: <path> on <model> (thread, branch, building, family F)`, then `; warning: <warning>` per warning;
- `waits NNN <title>: on 004 (building)`: its body names a lower id (`004`, or a run `001 to 004`) whose spec is not yet `done`, `rejected` or `later`; a higher id is a pointer, not a dependency;
- `waits NNN <title>: 3 leads working on <repo>`: three leads of the repo (on any box) are live, counting those just started;
- `not started NNN <title>: <the refusal>`, the two ruling refusals included.

With no accepted spec it prints `no accepted specs in <repo>`. When any started, a last line names their family: `family <repo>-build-<N>`. `--json` prints the lines as objects, each with `family`.

*Families.* Without `--family`, each invocation mints `<repo>-build-<N>` (`<repo>` the repo's node token, see [Names](#names)), `N` one past the highest such family in `bm-nodes` or still holding feedback. The lead's node and spec record it as `family`. A coordinator dispatching several commands or repos passes the first family to the rest (over ssh, on the command line). See [Build families](#build-families).

*`--hold land|now`* (both forms; "build these, land none until I say"). It writes each lead's hold before the runner starts it (`by` the sender, text `held at dispatch (build --hold <gate>)`) and sends the hold message once the lead has started. A failed message goes to stderr, `build: <path> is held before land, but the hold message failed: <err>`, and the start stands. The JSON carries `hold`; `--accepted`'s started line ends `, held before land` (`, held now`). A failed start deletes the hold. A build without `--hold` deletes a hold an earlier lead left on its path. See [Holds](#holds).

*Models.* The binary has no model fallback: when the runner rejects a model, you retry with `--model`.

See also: [Build families](#build-families), [Holds](#holds), [`bmesh feedback`](#bmesh-feedback), [The fixer](#the-fixer), [`bmesh release`](#bmesh-release).

### `bmesh lead start`

```
bmesh lead start --repo R --spec NNN --brief FILE [--model M] [--effort E] [--title T] [--path P]
```

Starts a lead through `[runner] kind`, registers its node as `idle` with its model and effort, and prints where it runs. `bmesh build` starts its leads the same way.

| Flag | Default | Meaning |
|---|---|---|
| `--repo R` | required | The repo, a registry name or a path. |
| `--spec NNN` | required | The spec id, for example `012`. |
| `--brief FILE` | required | The file that holds the lead's first prompt. |
| `--model M` | `[models] lead` | The lead's model. |
| `--effort E` | `[models] lead_effort` | `low`, `medium`, `high`, `xhigh` or `max`; `""` is the runner's own. |
| `--title T` | `[build] <repo> <spec>` | The thread title. |
| `--path P` | `<repo>-<NNN>` | The node path. `<repo>` is the registry name's node token (see [Names](#names)). |

**Output.** `{"path","thread","branch","worktree"}`.

**Refusals.** Each message prints after `bmesh: `.

| When | Exit | Message |
|---|---|---|
| A live node (neither `done` nor `blocked`) at the path | 3 | `refused: path <p> is taken (state <s>); use --path <p>-2` |
| `--path` whose first token is reserved (see [Names](#names)), a bare `routine` included | 2 | `path "<p>": invalid path "<p>": "<tok>" is a reserved first token` |
| `--path` of the form `routine.<kind>` | 3 | `refused: path <p>: the first token routine is for the routine pool` |
| `[runner] max_leads` live leads already registered here | 3 | `<N> leads already working on this host` |
| The start timed out | 1 | `start timed out; thread id <id> may exist` (with `t3`, the branch message under **Details**) |
| The thread started but registering the node failed | 1 | `thread <id> started (branch <b>, worktree <w>) but registering the node failed: <err>` |

**Details.**

- **Paths.** A finished node at the path is reused and reset to `idle`.
- **Timeouts.** The start runs under the runner's timeout, not the CLI's one-minute deadline. A timeout, or a registration failure after it, names the thread id (and the branch and worktree when known). With `t3`, whose threads T3 names, it names the branch: `start timed out; a thread on branch bmesh/<id> may exist`.
- **Lead cap.** With `[runner] max_leads` set, the refusal reads in full `bmesh: N leads already working on this host`. A lead counts once its runner has started it. A finished lead's session (a tmux window, a T3 thread) never counts, though it holds memory until you close it. `scripts/memcap.sh N` prints the peak memory of N tmux leads, each with three executors.

See also: [Runners](#runners), [`bmesh build`](#bmesh-build).

### `bmesh hold`, `unhold`, `held`

```
bmesh hold <path>|--family F [--gate land|now] --text "<why>"
bmesh unhold <path>|--family F [--text "<words>"]
bmesh held
```

`hold` stops a lead at a gate until someone releases it with `unhold`. The lead runs `held` when it reaches its gate. What a hold is and what each gate does is in [Holds](#holds).

| Flag | Default | Meaning |
|---|---|---|
| `--family F` | | `hold`: hold every live lead of build family `F`. `unhold`: release every held lead of the family. |
| `--gate land\|now` | `land` | `hold` only: where the lead stops. `land`: `bmesh land` refuses. `now`: its next inbox. |
| `--text T` | required for `hold` | `hold`: why, as the lead reads it. `unhold`: words the lead reads with the release. |
| `--json` | off | Print JSON. |

**`hold`** records the hold (`by`: `--node`, else this node, else the host name) and sends the lead a message of kind `hold`, `hold from <by> (<gate>): <why>`, which wakes an idle lead. A new hold replaces one already there. It prints `holding <path> before land: <why>` (`now: <why>` at gate `now`) per lead; `--json` prints `[{"path","gate","text","by","at"}]`.

**`unhold`** removes the hold, sends the lead a message of kind `release`, `released by <by>` (`: <words>` with `--text`), which wakes it, and prints `released <path>`. A lead not held prints `<path>: not held`, sends nothing and exits 0. `--family F` with nothing held prints `<F>: nothing held`. `--json` prints `[{"path","released"}]` instead of the lines. (`bmesh release` cuts a release tag, see [`bmesh release`](#bmesh-release).)

**`held`**, run by the lead at its gate, reads the lead's hold, publishes `held` with the text `before land by <by>: <why>` (`now by ...`), moves the node to state `held` and prints `held <path> <text>; end your turn: the release wakes you`. For a lead with a ready request, its daemon runs `held` for it (see [`bmesh ready`](#bmesh-ready)).

**Refusals.** Each message prints after `bmesh: `.

| When | Exit | Message |
|---|---|---|
| `hold` with a gate other than `land` or `now` | 2 | `--gate "<g>": want land or now` |
| `hold` without `--text` | 2 | the `hold` usage line |
| `hold` on an executor | 3 | `refused: <path> is an executor; executors are never held: hold its lead <lead>` |
| `hold` on another role | 3 | `refused: <p> is a <role>; only a lead is held` |
| `hold` on a done or blocked lead | 3 | `refused: <p> is <state>; there is nothing to hold` |
| `hold` on a path with no node | 3 | `refused: no node <p>` |
| `hold --family F` with no live lead | 3 | `refused: no live lead in family <F>` |
| `held` with arguments | 2 | `held takes no arguments: the hold says why` |
| `held` from a state that can't move to `held` | 3 | `refused: illegal state transition <from> -> held` |
| `held` with no hold (a release came first) | 3 | `refused: <path> is not held (released already?); run bmesh inbox and carry on` |

See also: [Holds](#holds), [`bmesh build`](#bmesh-build) (`--hold`), [`bmesh land`](#bmesh-land).

Internals: [internals](internals.md#holds)

### `bmesh feedback`

```
bmesh feedback "<text>" [--repo R] [--where <file|command>] [--fixed SHA]
bmesh feedback list <family>|--repo R [--json]
bmesh feedback drain <family> [--through SEQ]
bmesh feedback drain --seq SEQ[,SEQ...]
```

A lead runs `feedback` to leave one line per issue of friction outside its spec that another build would meet too. It runs it once it has decided it is done, just before `bmesh done`. Its own spec's work is not feedback. Executors never write feedback: they report to their lead, which decides what affects other builds. A coordinator lists and drains a family's feedback once the family is done.

| Flag | Default | Meaning |
|---|---|---|
| `--repo R` | the node's repo | `feedback`: where the friction is, a registry name or path; `buildermesh` for the mesh itself (bmesh, a skill, a brief). `list`: every family's feedback naming `R`. |
| `--where <file\|command>` | | The file or command the friction was met in. |
| `--fixed SHA` | | A fix the lead already landed, leaving the general case. |
| `--through SEQ` | the last one there when it starts | `drain`: the last sequence to delete. |
| `--seq SEQ[,SEQ...]` | | `drain`: delete exactly these sequences, whatever their family. |
| `--json` | off | Print JSON. |

**`feedback "<text>"`** reads the family, repo and spec from the node. It publishes the issue to the family's feedback queue, with the node's worktree commit when the worktree is on this box, and prints `feedback <seq> in <family>` (`--json`: the record with `seq`). An issue is pending until it is drained or 30 days pass.

**`list <family>`** prints the family's pending feedback, oldest first: `<seq> <from> (<repo> <spec>[, where W][, fixed F]): <text>` (`--json`: an array of the records, each with `seq`). With nothing pending it prints nothing.

**`list --repo R`** prints every family's pending feedback naming `R`, in stream order, each line naming its family: `<seq> <from> (<repo> <spec>, family <F>[, where W][, fixed F]): <text>`. It leaves out an issue whose sequence a spec on `R`'s `main` names in `from_feedback` (`#<seq>`), whatever the spec's status.

**`drain <family>`** deletes the family's pending feedback up to and including `--through` and prints `drained N from <family>`. A message published after `--through` stays.

**`drain --seq`** deletes exactly the feedback messages at those sequences, whatever their family, and prints `drained N`, then `not pending: <seqs>` for any already gone.

**Refusals.** Each message prints after `bmesh: `.

| When | Exit | Message |
|---|---|---|
| `feedback` from a node with no family (not started by `build`) | 3 | `refused: <path>: no build family` |
| `feedback` from an unregistered node | 1 | `node <path> is not registered` |
| `drain --seq` with a sequence that holds anything but feedback | 3 | `refused: <seq> is <subject>, not feedback` (nothing is deleted) |

**Draining a family.** The coordinator (the `build` skill) reads the queue once `watch` prints `watch-done` for a family:

1. Issues naming bmesh's own repo are the fixer's (see [The fixer](#the-fixer)): it reports the fixer `mesh-status` names on its `fixes:` line and leaves them pending.
2. The rest it lists, merges duplicates, drops what a spec on `main` already covers (any status) or a `--fixed` commit settled, and sorts:
   - A quick fix (one cause, one obvious change, XS or S) becomes a spec landed as `accepted` (`Feedback NNN: accepted from <family>`, `proposed_by` its thread, `from_feedback` the family and leads). It is built in a new family once this family is drained, with no operator review.
   - Anything that needs thought becomes a `proposed` spec (`Feedback NNN: proposed from <family>`, the same `proposed_by` and `from_feedback`) for the operator's review, never a round.
   - An issue from a lead built from a `from_feedback` spec is always proposed.
3. It drains the issues it ruled on with `drain --seq` and leaves the fixer's, so the specs' `from_feedback` (`<family> (<leads>) #<seq>`) is the only record left.
4. It reports one line per issue: `fixed by NNN (started)`, `proposed NNN`, `already NNN`, `dropped: <why>` or `to the fixer`.

A coordinator that dies between list and drain leaves the family pending, and `mesh-status` shows it. `mesh-status` shows pending feedback as `feedback: N pending in <family>` per family, and issues naming bmesh's own repo instead as `fixes: N pending for <repo> (...)` (see [The fixer](#the-fixer) and [`bmesh mesh-status`](#bmesh-mesh-status)).

See also: [Build families](#build-families), [The fixer](#the-fixer), [`bmesh build`](#bmesh-build).

Internals: [internals](internals.md#feedback)

### `bmesh fix`

```
bmesh fix [--model M] [--effort E]
```

Starts the fixer on bmesh's own repo: a fix lead that rules on the feedback naming that repo and builds the fixes (see [The fixer](#the-fixer)). It takes no repo. The daemon also starts a fixer on its own, so no session has to run `fix`.

| Flag | Default | Meaning |
|---|---|---|
| `--model M` | Opus | The fixer's model. |
| `--effort E` | `[models] lead_effort` | The fixer's effort. |
| `--json` | off | Print JSON. |

**What it does**, in order:

1. It levels `main`.
2. It starts the fixer through the runner as `lead start` starts a lead: node, family and path `<repo>-fix-<N>` (`N` one past the highest in `bm-nodes`, as a node or a fix lead's family, or still holding feedback), role `fixer`.
3. It runs on the row's `host`: another box runs `bmesh fix` over ssh.

**Output.** `fix: <path> started (thread <id>, <model>, on <host>)` (`--json`: `{"path","thread","branch","worktree","model","family","host"}`).

**Refusals.** Each message prints after `bmesh: `.

| When | Exit | Message |
|---|---|---|
| No registry row holds bmesh's module | 3 | `refused: no registry row holds the module bmesh was built from` |
| The row is not `ship` | 3 | `refused: <repo> is <mode>; builds are for ship repos only` |
| A live fixer of the repo | 3 | `refused: fixer <path> is <state>; one fixer writes the queue` |

**Details.** `[runner] max_leads` doesn't count the fixer, only the fix leads it starts. The daemon wakes and watches a fixer as it does a lead.

See also: [The fixer](#the-fixer), [`bmesh feedback`](#bmesh-feedback).

### `bmesh release`

```
bmesh release <repo> [--minor|--patch] [--dry-run]
```

Tags the repo's code head (see [Updates](#updates)) with an annotated `vX.Y.Z` tag and pushes it to `origin`. You run it, or the `build` skill runs it at the end of a batch (see Details).

| Flag | Default | Meaning |
|---|---|---|
| `--minor` | off | Bump the minor version, whatever kinds the specs are. |
| `--patch` | off | Bump the patch version, whatever kinds the specs are. |
| `--dry-run` | off | Run the checks and print the tag and its message; tag and push nothing. |
| `--json` | off | Print JSON (global flag). |

**What it does**, in order:

1. needs the repo's registry row (no row: exit 2, `<name> is not in repos.toml; a release needs its row (verify, remote)`);
2. fetches `origin` with its tags;
3. reads `origin/main` (`main` without an `origin`; the tag then stays local);
4. runs the checks under Refusals;
5. tags the code head with an annotated `vX.Y.Z` tag and pushes it to `origin`. A failed push deletes the local tag.

**The version.** With no `vX.Y.Z` tag in `main`'s history, the version is `v0.1.0`. Otherwise the highest tag's minor goes up (patch `0`) when any spec landed since that tag is a `feature`, else its patch goes up. `--minor` or `--patch` overrides this (both: exit 2).

**The specs a release carries** are those of the `NNN: done at <sha8>` subjects after the last tag (every one, for the first release), less any whose `merged` commit is already in that release's history. A release tags the code head, so the `done` commit of a spec it carries comes after its tag.

**The tag's message.**

1. `<repo> vX.Y.Z`, a blank line;
2. one `NNN <title>` per spec the release carries (`no specs landed since <tag>` when none);
3. in bmesh's own repo, with upgrade notes: a blank line, `Upgrading to vX.Y.Z (docs/upgrading/vX.Y.Z.toml):`, the summary, what a start-over loses, one numbered line per step (see [Upgrade notes](#upgrade-notes)).

**Output.** `released <repo> vX.Y.Z at <sha8> (NNN, NNN)`, or `(no specs)`. Without an origin, `; no origin, so it is local only` is appended. `--json` prints `{"repo","tag","commit","specs","pushed"}`; under `--dry-run` it adds `"dry_run": true`, with `pushed` false. A last tag on the code head itself (as a tag `release` cut is) prints `already released <repo> vX.Y.Z at <sha8>`, exit 0.

**Refusals** (exit 3, tagging nothing):

- `nothing to release since vX.Y.Z: nothing under code changed`: no commit since the last tag changes the repo's code (its row's `code`).
- `held: <family> is still running (...)` or `held: <family> has N feedback pending; ...`: the hold `build` reads (see [`bmesh build`](#bmesh-build)), a live lead (neither `done` nor `blocked`) or pending feedback of the repo's. `release` has no family of its own, so the coordinator's own family and both pipelines hold it. Feedback naming bmesh's own repo holds neither command.
- `main is red at <sha8> <subject>: verify fail t:<tree8> (...); release once main is green`, then the tail of the output: `verify` at the code head is not ok. The result is answered from the record, carried, or run and recorded as `build` does. A repo with no `verify` kind says so and goes on.
- In bmesh's own repo only (see [The fixer](#the-fixer); `land`'s scaffold check makes the same test; no key turns this on elsewhere):
  - `refused: 055, 057 have Upgrading steps and docs/upgrading/vX.Y.Z.toml is missing: distil them into it (the build skill's step 7), commit it to main and release again`: a spec landed since the last tag has `## Upgrading` steps (any text, unless the section is empty or its first word is `nothing`, in any case), and the ref read has no notes for the new tag;
  - `refused: docs/upgrading/vX.Y.Z.toml: <what doesn't parse>`;
  - `refused: docs/upgrading/vX.Y.Z.toml names keys this binary doesn't know: [runner] foo`: an ask or a `when` names a key this binary's config or registry lacks.

  Notes that exist are checked whether or not any spec has steps.

**Details.**

- `--dry-run` runs the same checks and prints `would release <repo> vX.Y.Z at <sha8> (...); --dry-run tags nothing`, a blank line and the tag's message. It pushes nothing.
- A pushed tag is public, so a bad release is followed by the next one, never moved.
- The first tag is cut by hand or with `release`. From then on, the `build` skill ends each batch, once its family is done and drained, with `bmesh release <repo>` for every repo with a `v*` tag where the family landed code.
- A box on `[update] track = "release"` (the default) updates to each release (see [Updates](#updates)). `track = "latest"` and `bmesh update --latest` follow the code head.

See also: [Upgrade notes](#upgrade-notes), [`bmesh spec template`](#bmesh-spec-template), [`bmesh update`](#bmesh-update).

### `bmesh scaffold`

```
bmesh scaffold <repo> [--check]
```

Writes into a ship repo what a build expects, from the row and the binary, and nothing else. `<repo>` is a name or a path: the row's checkout, or a linked worktree of it given by path (the row whose checkout shares the worktree's git common dir). It writes in place, so a lead scaffolds its own branch without touching the main checkout.

| Flag | Default | Meaning |
|---|---|---|
| `--check` | off | Write nothing; print the unified diff of what would change and exit 1 when there is any. |

**What it does**, in order:

1. **`AGENTS.md`**: three blocks between `<!-- bmesh:done -->`, `<!-- bmesh:proposals -->` and `<!-- bmesh:mesh -->` markers, in that order: the definition of done from the row's verify commands and remote; the proposals convention; the Buildermesh paragraph of `docs/agents-snippet.md` (its verify placeholder filled from the row). Only the text between the markers is rewritten. A missing block is appended at the end, and a missing `AGENTS.md` is created. A block between `command-center:` markers (an earlier scaffold's) is rewritten in place under `bmesh:` markers.

   Then a fourth block, `rules`, after `mesh`: the engineering rules the repo picked, from bmesh's [rule sets](#rule-sets). Its opening marker carries the pick and a hash, `<!-- bmesh:rules core design tests a1b2c3d4 -->` (see [internals](internals.md#the-rules-block)); the body is a `## Engineering rules` heading, one line naming the sets and saying that where the rest of the file says otherwise the rest wins, then each set's lines under a `### <set>` heading, in the marker's order. Unlike the other three, this block is the repo's own:
   - **The pick is the marker.** Set names with no hash are a fresh pick, rendered. A missing block is appended as `core`. An empty marker, `<!-- bmesh:rules -->`, keeps the block empty: no rules. Names are read in order and a repeated one once.
   - **An edit is kept.** When the body's hash differs from the marker's, scaffold keeps the body, rewrites the marker to `<!-- bmesh:rules customized -->` and prints `AGENTS.md: rules are customized; kept as written`. A `customized` block is never rewritten, and the line is printed on every run (with `--check` too, above the diff). Putting set names back on the marker, or deleting the block, returns it to bmesh's text on the next run.
   - **A marker with names and a matching hash** is rendered again, so a bmesh release that rewords a set reaches every repo that didn't edit it.

   Only the first `rules` block is read. [`bmesh land`](#bmesh-land) never checks it. The `scaffold` skill asks the operator which sets a repo gets before it writes (see [The plugin](#the-plugin)); a plain `bmesh scaffold` gives a repo with no block `core`.
2. `docs/proposals/README.md` (this repo's, embedded) and `docs/proposals/assets/.gitkeep`, when missing.
3. A kind file for each of the row's `routines` the repo lacks, seeded from `examples/routines/` (`verify` inline and deterministic, its `command` the row's verify commands joined with ` && `). A row with no verify commands gets no verify kind and a line on stderr, `bmesh: the row lists no verify commands, so the verify kind was not written`.
4. `.bmesh/config.toml`'s `[setup]`, when the row has a `setup`: `command`, then `inputs` and `timeout` when the row names them (see [Setup](#setup)). It is merged in: a `[setup]` that already reads the same (as the loader reads it, defaults included) is left as written; another is replaced in place, from its header to the next table's (the comment lines right above that table kept); none is appended. `[slots]`, `[routines]` and the rest of the file are kept, and the file is created when missing. A row with no `setup` leaves the file alone.

An existing kind is never touched, except the verify kind's `command` when the row has verify commands. A `command` that (as the kind loader reads it) is not those commands joined with ` && ` gets its one `command = ...` line rewritten and the rest kept, whether or not the row's `routines` name it. A verify kind that runs a model is left alone. To change the verify commands, change them in the row (see [Hosts: The registry](#the-registry)) and run `scaffold` again.

**Output.** `wrote <file>` per file, or `<repo> is scaffolded`.

**Refusals.** Each message prints after `bmesh: `.

| When | Exit | Message |
|---|---|---|
| `--check` finds a change | 1 | the diff |
| A path without a row | 2 | `<name> is not in repos.toml; scaffold needs its row (verify, remote, routines)` |
| A row not `ship` | 2 | `<name> is <mode>; only ship repos are scaffolded` |
| A routine with no example | 1 | `no example for kind <k>; write .bmesh/routines/<k>.toml yourself` |
| A verify kind whose command spans lines | 1 | `<file>: its command is not on one line scaffold can rewrite; set it to the row's verify (<cmd>) by hand` |
| A `[setup]` written as other than one table (dotted keys, say) | 1 | `.bmesh/config.toml: its [setup] is not a table scaffold can rewrite; set it to the row's setup by hand` |
| The rules marker names a set bmesh doesn't ship | 1 | `AGENTS.md: unknown rule set <name>; bmesh has core, design, tests, tdd, prose` |

**Details.** A second run changes nothing. Run it again after the row or the snippet changes, and commit what it wrote.

See also: [`bmesh repos`](#bmesh-repos), [Routines](#routines), [The plugin](#the-plugin).

### Rule sets

The engineering rules `bmesh scaffold` can write into a repo's `AGENTS.md`, one set per name on the `rules` marker; pick any, combine them, or none. They are in bmesh's own words, loosely after the principles of [pstack](https://github.com/cursor/plugins/tree/main/pstack) by Lauren Tan (MIT). Once written, the text is the repo's to edit: scaffold keeps an edited block (see [`bmesh scaffold`](#bmesh-scaffold)), and where the rest of `AGENTS.md` says otherwise, the rest wins.

**`core`**, for every repo (the default):

- Make the smallest change that solves the problem. Delete what's dead before adding.
- Read the code before you change it, and say what you learned before deciding.
- Fix the first place it went wrong, not where it showed. A guard is a fix only when the state it handles is legitimate.
- Prove it works on the real thing: run it the way a user would. Passing tests prove only what they reach.
- When two fixes have failed on the same assumption, stop and test the assumption.
- A rule you've had to state twice becomes a check or a script, not a third sentence.
- Do reversible work, then report it. Ask only what no experiment or reading can settle.
- Before trusting a number (a timing, a speedup, a count), find what limits it and rule out that it measured something else.

**`design`**, for code that holds state or runs concurrently:

- Name the data shape before writing logic.
- Keep the domain in one structure (a type, a table, a state machine), not in conditionals spread across files.
- Check input where it enters the system. Inside, trust your own types.
- One writer per file, branch, key or record. Remove the sharing before reaching for a lock.
- Make steps safe to run twice, so a retry after a crash ends in the same state.
- When a new API replaces an old one, move every caller and delete the old one in the same change.

**`tests`**, for a repo with tests:

- Test what callers can observe, not how the code does it. A test that breaks on a rename is testing internals.
- Test through the real entry point (the command, the package's public functions) where you can.
- A piece isn't done while no test exercises its code.
- A flaky test is a bug. Find the cause rather than retrying it away.

**`tdd`**, an add-on to `tests`, apart from it because the cycle, not the text, is what costs tokens:

- Write the failing test first, and watch it fail for the reason you expect.
- Write the least code that makes it pass, then tidy with the test passing.
- Give each behaviour the change names its own failing test.

**`prose`**, for a repo whose product is text (docs, skills, briefs):

- Say what a thing does, with the mechanism or a number, not how it feels.
- Short sentences, one idea each, in the active voice.
- Plain words: "use", not "leverage". "Is", not "serves as".
- A comment says why, when the code can't show it. Delete comments that narrate what the code does.
- Cut filler, hedges and closing summaries that repeat the text above them.

## Nodes

What a lead, executor or engineer runs to report, and what you run to see and stop them.

### `bmesh whoami`

```
bmesh whoami
```

Prints the path of the node this command acts as. A session runs it to check its own identity before it reports.

**What it does.** It resolves the node in this order:

1. `--node`;
2. else the `BMESH_NODE` environment variable;
3. else the registered node whose worktree is this checkout (`git rev-parse --show-toplevel`) on this host.

Every node command (`started`, `send`, `inbox`, `executor new`, ...) resolves its node the same way.

**Output.** The node's path, one line.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| An argument is given | 2 | `whoami takes no arguments` |
| No node resolves | 2 | `no node for this directory; pass --node or set BMESH_NODE` |
| The `--node` path is not a valid path (see [Names](#names)) | 2 | `node "<path>": <why>` |

### `bmesh started`, `progress`, `blocked`, `ask`, `done`

```
bmesh started|progress|blocked|ask [--text T]
bmesh done --commit SHA [--text T]
```

Reports an event of this node: the command publishes the event and moves the node's state. Leads, engineers and executors run them; the operator reads leads' events (in `mesh-status`, `watch`, the map and pushes).

| Flag | Default | Meaning |
|---|---|---|
| `--text T` | none | what the node says: the work, the question, the block or the result |
| `--commit SHA` | none | the commit the event refers to; required for `done` |

Each event sets a state. The move must be legal from the node's current state:

| Event | Sets | Legal from |
|---|---|---|
| `started`, `progress` | `working` | `idle`, `working`, `blocked`, `asking`, `silent`, `held`, `landing` |
| `ask` | `asking` | `working`, `asking`, `silent`, `held` |
| `blocked` | `blocked` | `working`, `asking`, `silent`, `held`, `landing` |
| `done` | `done` | `working`, `blocked`, `asking`, `silent`, `held`, `landing` |
| `held` ([`bmesh held`](#bmesh-hold-unhold-held)) | `held` | `working`, `asking`, `silent`, `held`, `landing` |
| `ready` ([`bmesh ready`](#bmesh-ready)) | `landing` | `working`, `held` |

A node never registered reads as `idle`. Nothing leaves `done`. A `landing` lead (one that ran `bmesh ready`) is moved on by its box's daemon: to `held` at a hold, back to `landing` from `held` at the release, to `done`, to `working` after land refused, and to `blocked` when the wake carrying a refusal fails for an hour. It moves to `working` on its own `started` or `progress`, which withdraws the request. The daemon rewrites its text, `landing` to `landing`, as land goes. It is never called silent, never pushed (`[notify] push` takes only `ask`, `blocked` and `done`), and never in the map's needs-you band.

**What it does**, in order:

1. Resolves the node as [`bmesh whoami`](#bmesh-whoami) does.
2. For `done` on a node already `done`: compares `--commit` with the newest `done` event's commit. The same commit (equal, or one an abbreviation of at least 7 characters of the other) prints `already done at <sha8>`, exits 0 and publishes nothing; another commit is refused, exit 3, `bmesh: refused: <path> is already done at <sha8>, not <sha8>`.
3. For a lead's `done`: checks the spec. A lead's `done` is refused (exit 3, `bmesh: not done: <why>`) until the spec on `origin/main` reads `done` with a merged commit and a recorded result for every entry of its verified line; see [`bmesh land`](#bmesh-land); the checks, in order, with their messages, are in [The verified line](#the-verified-line). An executor's `done` and an engineer's `done` need only `--commit` and never read the spec.
4. Checks the state move against the table above. A refused event publishes nothing and sends no message.
5. For an executor's `ask`, `blocked` and `done`: puts a message in its parent's inbox (see Details). A fix lead's (a lead in the fixer's family) go to the fixer's inbox the same way (see [The fixer](#the-fixer)).
6. Publishes the event and writes the node's new state.

**Output.** `started`, `progress`, `ask` and `blocked` print `<event> <path>`. `done` prints `done <path> <sha8>` (`<sha8>` the `--commit` given), or `already done at <sha8>` for a repeat.

**Refusals.** Each message prints on stderr after `bmesh: `.

| When | Exit | Message |
|---|---|---|
| A positional argument is given | 2 | `<event> takes no arguments (use --text)` |
| `done` without `--commit` | 2 | `done needs --commit SHA` |
| No node resolves | 2 | `no node for this directory; pass --node or set BMESH_NODE` |
| The move is not in the table | 3 | `refused: illegal state transition <from> -> <to>` |
| `done` for another commit than the node is done at | 3 | `refused: <path> is already done at <sha8>, not <sha8>` |
| A lead's spec is not landed and verified | 3 | `not done: <why>` (see [`bmesh land`](#bmesh-land)) |
| `done` from a node that is not registered | 1 | `node <path> is not registered` |
| An executor with no parent sends `ask`, `blocked` or `done` | 1 | `executor <path> has no parent to <event>` |

**Details.**

A lead's `--commit` is the commit `main` points at after its push, the follow-up commit recording `done` (the `landed <sha8>` that [`bmesh land`](#bmesh-land) prints). The spec check reads `merged` and the results, not this sha.

**What reaches the lead.** An executor's `ask`, `blocked` and `done` are published as events and also put in its parent's inbox, since only leads reach the operator; `started` and `progress` are events only.

- `ask`: a message, `ask from <path>: <text>`;
- `blocked --text T`: a message of kind `report`, `blocked from <path>: <T>`;
- `done --commit SHA [--text T]`: a `report`, `done from <path>: branch <b> at <sha8>: <T>`; it needs `--commit` and never reads the spec.

A report carries `{"event","from","branch","commit"}` (branch and commit for `done`) in its `report` field, which `inbox --json` prints.

With no text, `blocked` and `done` end before their `: <T>`; `ask` always ends with `: ` and the text. The branch is the worktree's current one (`rev-parse --abbrev-ref HEAD`), or the recorded one when the worktree is detached, gone or on another host; the sha is `--commit` resolved in the worktree, else as given. A retry within two minutes is one message, and a refused event sends none. The digest nests an executor's ask, silence, block or completion under its lead's line and counts only leads under "asking".

See also: [`bmesh inbox`](#bmesh-inbox), [`bmesh executor`](#bmesh-executor), [`bmesh hold`, `unhold`, `held`](#bmesh-hold-unhold-held) (the `held` event).

### `bmesh send`

```
bmesh send <path> <text|->
```

Puts a message in a node's inbox. Any node sends, and the operator sends as `operator` (`bmesh --node operator send <path> <text>`).

| Argument | Meaning |
|---|---|
| `<path>` | the node that gets the message |
| `<text\|->` | the message; `-` reads it from stdin, with trailing newlines dropped |

**What it does.** `bmesh send <path> <text>` resolves the sender as [`bmesh whoami`](#bmesh-whoami) does, gives the message a fresh id and always publishes it to the node's inbox. It never checks the node's thread. Only the daemon's wake checks the thread: see [Runners](#runners) for what each runner does with a wake sent mid-turn. The daemon wakes only nodes with a thread (leads, fixers and engineers). It wakes a `done` or `blocked` one only when its unread mail holds a message of kind `message` (a `bmesh send`, or an executor's `ask`), and a stopped one only for such a message from outside its tree sent after the stop (see [`bmesh node stop`](#bmesh-node-stop)).

**Output.** `sent <id>`, or `duplicate <id>` for a retried id.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| Not exactly two arguments | 2 | `usage: bmesh send <path> <text\|->` |
| `<path>` is not a valid path | 2 | the parse error |
| No sender node resolves | 2 | `no node for this directory; pass --node or set BMESH_NODE` |

### `bmesh inbox`

```
bmesh inbox [--peek] [--json]
```

Reads this node's unread messages and acks them. Leads and executors run it when they wake; the daemon's wake text tells them to (see [Config: Wake text](#wake-text)).

| Flag | Default | Meaning |
|---|---|---|
| `--peek` | off | leave the messages unread |
| `--json` | off | print every message whole, one JSON object per line |

**What it does.** It returns what the inbox holds at once. When the node has a hold at the gate `now`, it prints that hold first, on its own line, on every read while the hold stands (see [Holds](#holds)).

**Output.**

- A hold at `now`: `held now by <by> <age> ago: <why>; stop here: run bmesh held, end your turn, and wait for the release` (under `--json`, first, a message of kind `held` from `<by>`).
- A message: `<at> <from>: <text>`.
- A routine's reply: `<at> <from>: reply to job <id>: <kind> ok|fail, <n> lines; bmesh routine wait <id> prints it` (see [`bmesh routine wait`](#bmesh-routine-wait)).
- Nothing waiting: `inbox: empty` (nothing under `--json`).

A reply [`bmesh routine wait`](#bmesh-routine-wait) found is not printed by `inbox` again. Under `--json`, an executor's report carries its `report` field (see [What reaches the lead](#bmesh-started-progress-blocked-ask-done)).

**Refusals.**

| When | Exit | Message |
|---|---|---|
| A positional argument is given | 2 | `inbox takes no arguments` |
| No node resolves | 2 | `no node for this directory; pass --node or set BMESH_NODE` |

### `bmesh thread-status`

```
bmesh thread-status [<path>] [--json]
```

Prints one node's state: the named node's, else this node's (resolved as [`bmesh whoami`](#bmesh-whoami) does).

| Flag | Default | Meaning |
|---|---|---|
| `--json` | off | print the state record, `{"state","at","text","by"}` |

**Output.** `<state> since <at>: <text>`; ` since <at>` and `: <text>` are left out when empty. A path with no state reads `idle`.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| More than one argument | 2 | `usage: bmesh thread-status [<path>]` |
| No path, and no node resolves | 2 | `no node for this directory; pass --node or set BMESH_NODE` |
| `<path>` is not a valid path | 2 | the parse error |

### `bmesh mesh-status`

```
bmesh mesh-status [--all] [--json]
```

Prints every node's state, runner and unread count, then the lines that wait on the operator. The operator runs it, or the `buildermesh:mesh-status` skill does (see [The plugin](#the-plugin)).

| Flag | Default | Meaning |
|---|---|---|
| `--all` | off | list every node, finished ones too |
| `--json` | off | print every node as JSON, whatever `--all` |

**Output.** A table of nodes sorted by path, with the columns `PATH`, `ROLE`, `STATE`, `SINCE`, `RUNNER` and `UNREAD`. An empty role or since prints `-`. The runner column is `running`, `idle`, or `?` when the runner can't tell.

The table leaves out a node `done` for longer than `[status] keep_done` (default `24h`; `0` leaves none out) and closes with `… N finished nodes older than 24h (--all shows them)` (`node` for one; the duration is the `[status] keep_done` value). A `blocked` node is never left out, and a round goes only with all its children.

Then, in order:

1. `<path> held before land by <by> <age> ago: <why>` per hold (see [Holds](#holds));
2. `<path> landing <age>: <text>` per ready request, its age since `bmesh ready` and the lead's `landing` text (`: held, waiting for the release` while held; `landed <sha8> (ready <age> ago); the final wake is pending` or `land refused (ready <age> ago): <refusal>; the wake carrying it is pending` while the daemon owes the lead its wake; see [`bmesh ready`](#bmesh-ready));
3. `fixes: N pending for <repo> (<fixer> <state>)`, the open issues naming bmesh's own repo (see [The fixer](#the-fixer)): the parentheses read `(<fixer> <state>)` while a fixer has them (`fixes: N pending for <repo> (<fixer> working)`), `(starts within a minute)` when the daemon starts one on its next pass, `(<fixer> blocked)` while the newest fixer's block waits on the operator, and `(<fixer> done; they wait on you)` when the fixer had them and left them;
4. `feedback: N pending in <family>` per family with other feedback pending (see [`bmesh feedback`](#bmesh-feedback));
5. `t3 sign-in on <host>: expires in N days (bmesh t3 login)` with the `t3` runner and this box's T3 sign-in under 7 days from expiry (`expires in 1 day` in the singular, and `t3 sign-in on <host>: expired (bmesh t3 login)` once it has expired; see [`bmesh t3 login`](#bmesh-t3-login));
6. the behind-main notice (see [Updates](#updates));
7. the hub line when this box's `bmesh hub` runs an older binary than its `bmesh serve` (see [Several boxes](#several-boxes));
8. the `operator: ...` lines `bmesh spec steps` prints with no id (see [`bmesh spec steps`](#bmesh-spec-steps) and [The ledger](#the-ledger)).

`--all` lists every node; `--json` carries every node whatever the flag, as the map's `/api/nodes` does: an array of `{"node","state","runner","unread"}`, a held node's with `hold`, a node with a ready request with `ready` (`{"phase","head","at"}`). `--json` prints only the array, none of the lines after the table.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| A positional argument is given | 2 | `usage: bmesh mesh-status [--all] [--json]` |

See also: [`bmesh node`](#bmesh-node) (how nodes leave the mesh), [`bmesh watch`](#bmesh-watch).

### `bmesh watch`

```
bmesh watch --nodes a,b [--since T] [--timeout D]
```

Follows the named nodes until every one is `done`, `blocked` or `silent`. A coordinator (the `build` and `ideate` skills) runs it on its leads or a round. It needs no daemon.

| Flag | Default | Meaning |
|---|---|---|
| `--nodes a,b` | required | the node paths to follow, comma-separated |
| `--since T` | now | replay events from this RFC3339 time first |
| `--timeout D` | `30m` | give up after this long (exit 5) |

**What it does.**

1. When every named node is already `done`, `blocked` or `silent`, it prints `watch-done` and exits 0.
2. Else it prints the events since `--since` in the trees of the named nodes' leads, then follows new ones live, and prints each named node's runner change.
3. It reads the states again on every event and every 5 seconds (the daemon marks silence without an event), and stops with `watch-done` once every named node is `done`, `blocked` or `silent`.

**Output.** One line per event, `<at> <path> <event> <text>` (with the event's `to` path before the text when it has one); the daemon's events addressed into a watched tree print too. A runner change prints `<at> <path> runner running|idle`. A held lead shows as the `held` event, `<at> <path> held <text>`, and the watch goes on (see [Holds](#holds)). Host jobs and replies never show. The last line is `watch-done`.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| No `--nodes`, or a positional argument | 2 | `usage: bmesh watch --nodes a,b [--since RFC3339] [--timeout 30m]` |
| A path is not valid | 2 | the parse error |
| `--since` is not RFC3339 | 2 | `--since: <why>` |
| `--timeout` passes first | 5 | `watch timed out` |

### `bmesh executor`

```
bmesh executor new --name N [--base REF] [--model M] [--json]
bmesh executor list [--json]
bmesh executor remove <name> [--force]
```

A lead fans out by making executors: subagents in worktrees of their own, on branches under the lead's, each a node under the lead's path.

An executor reports with the event commands; its `ask`, `blocked` and `done` also reach its lead's inbox (see [What reaches the lead](#bmesh-started-progress-blocked-ask-done)). The `buildermesh:executor` skill is its method (see [The plugin](#the-plugin)).

Internals: [internals](internals.md#executors)

#### `bmesh executor new`

```
bmesh executor new --name N [--base REF] [--model M] [--json]
```

Makes a worktree, branch and node for one executor. The lead runs it (or an executor fanning out again; the caller is the parent).

| Flag | Default | Meaning |
|---|---|---|
| `--name N` | required | the executor's name, the last token of its path |
| `--base REF` | the parent's HEAD | the commit the branch starts from |
| `--model M` | none | the model the executor runs on, recorded on its node |
| `--json` | off | print only the JSON, no header |

**What it does**, in order:

1. makes `<[runner] worktrees>/<repo>/<parent path>.<name>` with `git worktree add -b <parent's branch>-<name>` from the parent's HEAD (`--base` picks another commit; the parent's branch is its worktree's current one, renamed or not);
2. registers node `<parent path>.<name>` with an inbox and state `idle`;
3. prints `{"path","branch","worktree"}` and, unless `--json`, the executor header to paste at the top of the subagent's prompt: the path, worktree and branch. The header says to start every shell command with `cd <worktree> &&`, pass `--node <path>` on every `bmesh` command, run `bmesh started` first (after `cd <worktree> && bmesh setup` when the commit it starts from names a `[setup]`, see [Setup](#setup)), report with `bmesh done --commit <sha> --text "<one line>"` alone, and invoke the `buildermesh:executor` skill.

When `git worktree add` fails (a hook, an unwritable root), nothing is registered and the worktree and branch are removed.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| The name doesn't match `^[a-z0-9][a-z0-9-]*$`, or contains `--` | 2 | `name "<N>": want lowercase letters, digits and hyphens, no "--"` |
| `--model` holds a space or `;` | 2 | `model "<M>": want no spaces or ";"` |
| `--base` is not a commit | 2 | `--base "<REF>" is not a commit in <worktree>` |
| The caller is a routine path | 3 | `refused: <path> is a routine path; executors belong to leads` |
| The executor's node is live (neither `done` nor `blocked`) | 3 | `refused: executor <path> exists (state <s>); remove it or pick another name` |
| The branch exists | 3 | `refused: branch <b> already exists; remove it or pick another name` |

#### `bmesh executor list`

```
bmesh executor list [--json]
```

Prints every node under the lead: path, state, branch, head (when the worktree is on this host) and unread count. An executor sees its lead's whole tree.

| Flag | Default | Meaning |
|---|---|---|
| `--json` | off | print the rows as JSON |

**Output.** A table with the columns `PATH`, `STATE`, `BRANCH`, `HEAD` and `UNREAD`; a missing branch or head prints `-`.

#### `bmesh executor remove`

```
bmesh executor remove <name> [--force]
```

Removes a merged executor's worktree and branch. The executor's parent runs it.

| Flag | Default | Meaning |
|---|---|---|
| `--force` | off | remove an unmerged branch and drop uncommitted changes |

**What it does.** It removes the worktree (`--force` also drops uncommitted changes) and the branch (`-d`, or `-D` with `--force`), forgets the worktree in the node, drains its inbox and marks it `done`. The node stays on the mesh until [`bmesh node forget`](#bmesh-node-forget); the name can be used again.

**Output.** `removed <path>`.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| A branch not merged into the caller's HEAD, without `--force` | 3 | `refused: branch <b> is not merged into <parent branch>; merge it or pass --force` |
| No such executor with a worktree under the caller | 3 | `refused: no executor <path> under <parent> with a worktree` |

### `bmesh node`

```
bmesh node forget <path> [--worktree | --wipe [--dry-run]] [--wait 30s]
bmesh node stop <path> [--text "<why>"] [--wait 30s]
```

Removes a finished node from the mesh, or stops a running one. [`bmesh mesh-status`](#bmesh-mesh-status) lists the nodes these act on.

**Routine nodes** leave the mesh by themselves: a fresh run's node goes once its run ends, and a pooled instance that stops (its pool restarted or shrunk, its kind or repo gone) is marked `done` and removed after `[status] keep_done` (`0`: never). Their events stay for the map. Every other node stays until forgotten.

#### `bmesh node forget`

```
bmesh node forget <path> [--worktree | --wipe [--dry-run]] [--wait 30s]
```

Removes a node from the mesh: its state, inbox, hold and ready request. Its events stay, and nodes under it are not forgotten with it, unless `--wipe` removes it and every node under it as if they never ran.

| Flag | Default | Meaning |
|---|---|---|
| `--worktree` | off | also remove the node's clean worktree and merged branch |
| `--wipe` | off | remove the node and every node under it, with their events, inboxes, key history, briefs, worktrees and branches (below) |
| `--dry-run` | off | with `--wipe`, print what it would remove and change nothing |
| `--wait D` | 30s | with `--worktree` or `--wipe`, how long to wait for another box's answer |

**What it takes:**

- a `done` or `blocked` node;
- a round whose children are all gone (forgotten before the daemon settled it), whatever its state;
- an `idle` node that never reported and whose thread isn't running (a lead whose session died before its first event).

A lead between its runner start and its first `started` is such an idle node; the runner status check below refuses to forget it.

**What it does with `--worktree`.** It first removes the node's worktree (`git worktree remove` in its repo; one already gone from disk is pruned) and deletes its branch (the one checked out there, renamed or not; else the node's).

- For another box's node, with `[hosts]` on and this box joined, `bmesh node forget --worktree` sends a `forget-worktree` job to that box (see [Other ops](#other-ops)) and waits `--wait` (default 30s). That box must accept the job. It forgets the node only once that box answers that it removed the worktree, printing `<box>: <answer>` and `forgot <path>`. A refusal is printed and exits 3, forgetting nothing. A box that doesn't answer in time prints `queued; the node stays until <box> answers`; on waking it takes the job and forgets the node itself. With `[hosts]` off it is refused (`run it there`).
- A dirty worktree (`git status --porcelain` prints anything) or a branch not merged into `main` (an executor's: or into its parent's branch, while that exists) is refused (exit 3, naming the cause, nothing touched).
- Merged means the tip is an ancestor, or every commit has an equivalent there (`git cherry`, as after a rebase before landing). The branch is deleted with `-D`, since `-d` would also require it merged by ancestry into its upstream.

**What it does with `--wipe`.** It takes the nodes above, and refuses any other state with `stop it first: bmesh node stop <path>`. For the node and every node under it (a lead's executors, a round's children: every path in `bm-nodes` that starts with `<path>.`), deepest first:

1. Everything `--worktree` does, with its refusals: the worktree and branch, here or, for another box's node, through a `forget-worktree <path> wipe` job to that box, sent together for every such node (see [Other ops](#other-ops)). A refusal exits 3 with nothing purged from the mesh. A box that doesn't answer within `--wait` prints `queued; nothing is wiped from the mesh until <box> answers: run it again then`, exit 0: the mesh is untouched until every box has answered, and the same command, run again once it has, finishes the wipe.
2. Its brief, `<user cache dir>/bmesh/briefs/<path, "." as "->.md`, on the box that ran it (another box's through the same job).
3. From `bm-mesh`, a subject-filtered purge of its events (`bm.<path>.>`, its own and those of every node under it) and its inbox (`bm.inbox.<path>` and `bm.inbox.<path>.>`).
4. A subject-filtered purge of the whole history of its keys (`<path>` and `<path>.>`) in `bm-nodes` (first), `bm-state`, `bm-runner` and `bm-holds`, so no entry and no purge marker is left.
5. Its inbox consumer.
6. The pending feedback of its family (`bm.feedback.<family>`), when no node outside the wipe is in that family.

Every filter names the path as whole tokens: `buildermesh-06` and `buildermesh-066-2` beside `buildermesh-066` keep everything. A routine's path is refused (its inbox is its kind's queue). Then it publishes `<path> by <who>` on `bm.daemon.<this box>.wiped`: a record that a wipe happened, never of what the node did.

Out of its reach: the spec's commits on `main`, the runner's thread (T3, tmux, claude), a push already sent, the daemons' own lines that name the node (`bm.daemon.<host>.wake`, `push`, `silent`, which expire with the stream after 30 days) and results in `bm-results`, keyed by tree.

**Output with `--wipe`.** One line per node, the named one first: `wiped <path>: <N> events, <K> keys, inbox, <F> feedback, brief, worktree, branch`, each part only when there was one (`nothing left` when none), e.g. `wiped buildermesh-066: 1 event, 3 keys, inbox, brief, worktree, branch`. With `--dry-run`, the same lines as `would wipe ...`, and nothing changes; another box's worktree and branch are read from the node's record there, and its brief isn't listed. Afterwards `bmesh watch --nodes <path> --since <before it started>` prints nothing.

**Output.** `forgot <path>`. `node forget` deletes a node's hold with the node (see [Holds](#holds)). Without `--worktree` the disk is untouched, and a node with a worktree prints `worktree <path> kept`. With `--worktree` it first prints `removed worktree <path>` and `deleted branch <b>`.

**Refusals.** Each exits 3.

| When | Message |
|---|---|
| Any state but those above | `refused: <path> is <state>; only a done or blocked node is forgotten` (with `--wipe`: `refused: <path> is <state>; stop it first: bmesh node stop <path>`) |
| A live node under it | `refused: <child> under <path> is <state>` (with `--wipe`, the same `; stop it first: ...`) |
| `--wipe` of a routine's path | `refused: <path> is a routine's; its inbox is its kind's queue` |
| `--wipe` with a node on another box and `[hosts]` off | `refused: <child> ran on <host>; run it there` |
| `--wipe` with a node on a box not in the roster | `refused: <child> ran on <host>, which isn't in the roster; run it there` |
| An idle node that published an event of its own | `refused: <path> is idle but published <event>; only an idle node that never reported is forgotten` |
| An idle node that names no box | `refused: <path> is idle and names no host` |
| An idle node on another box | `refused: <path> is idle and its thread is on <host>; run it on <host>` |
| An idle node whose runner is not this box's | `refused: <path> is idle and its runner is "<kind>", not this box's <runner>; run it on <host>` |
| An idle node whose thread is running | `refused: <path> is idle and its thread <thread> is running; stop it first` |
| An idle node whose runner can't tell (tmux) or that names no thread | `refused: <path> is idle and its thread may be running; stop it first` |
| The runner's status check fails | the same, with the error in parentheses before `; stop it first` |
| A dirty worktree or unmerged branch under `--worktree` | names the cause; nothing is touched |
| No node at the path | `refused: no node <path>` |
| `--worktree` for a node on a box not in the roster | `refused: worktree <path> is on <host>, which isn't in the roster; run it there` |
| Another box refuses the `forget-worktree` job | `<box>: <answer>` |

For an idle node, bmesh asks the runner once for its thread's status; anything but known and not running refuses it.

Internals: [internals](internals.md#finished-nodes)

#### `bmesh node stop`

```
bmesh node stop <path> [--text "<why>"] [--wait 30s]
```

`node stop <path>` stops a node and keeps it stopped. The operator runs it, or a coordinator skill does on "stop X"; a message would wake the node instead.

| Flag | Default | Meaning |
|---|---|---|
| `--text "<why>"` | none | the reason, written after `stopped by <who>` |
| `--wait D` | 30s | how long to wait for other boxes' answers |

**What it does.** It stops the nodes under it first, deepest first (a round's children, a lead's executors; those already `done` or `blocked` are left alone), then the node itself. For each:

1. It writes the node `blocked` with `stopped by <who>: <why>` (`stopped by <who>` without `--text`) and publishes `blocked` as the node's own would be (a lead's pushes). It deletes a lead's ready request, so its daemon cancels a running land step and the stopped thread stays stopped (see [`bmesh ready`](#bmesh-ready)).
2. It stops the thread through its runner (`stop`: T3's `t3_thread_interrupt`, claude's turn and queue, tmux's Escape; a thread the runner no longer has counts as stopped), printing `stopped <path>`.

`<who>` is `--node`, else this node, else the host name. A node with no thread (an executor) is only written.

**Output.** `stopped <path>` per node here; `<box>: stopped <path>` per node on another box; for a round, `<round> <state>` or `<round> stays <state> until its children are stopped`; `already <state>` for a node already finished.

**Refusals.** The no-node, `[hosts]` and runner refusals come before anything is stopped. The roster refusal and another box's refusal come after the threads here are stopped; the roster check sends no job.

| When | Exit | Message |
|---|---|---|
| No node at the path | 3 | `refused: no node <path>` |
| `[hosts]` off, and a thread is on another box | 3 | `refused: <path> runs on <host>; run it on <host>` |
| A thread's runner is not this box's | 3 | `refused: <path>'s runner is "<kind>", not this box's <runner>; run it on <host>` |
| A thread is on a box not in the roster | 3 | `refused: <path> runs on <host>, which isn't in the roster; run it on <host>` |
| Another box refuses the `stop-thread` job | 3 | `<box>: <answer>` |
| An interrupt fails | 1 | `... (it is marked stopped; run node stop again to stop its thread)` |

**Details.**

- A failed interrupt exits 1 with the node already marked (`... (it is marked stopped; run node stop again to stop its thread)`).
- Another box's thread gets a `stop-thread` job there (see [Other ops](#other-ops)), all sent at once after the threads here and answered within one `--wait`, printing `<box>: <answer>` for each (`<box>: stopped <path>`). Each box must accept the job; a refusal is printed and exits 3. A box that doesn't answer in time has the node marked stopped from the sender, printed `queued; <path> is marked stopped and <box> stops its thread when it answers`; on waking it takes the job and interrupts the thread. With `[hosts]` off it is refused before anything is stopped (exit 3, `refused: <path> runs on <host>; run it on <host>`), as is a thread whose runner is not this box's (`refused: <path>'s runner is "<kind>", not this box's <runner>; run it on <host>`).
- A round is then settled from its children as the daemon does (see [`bmesh ideate`](#bmesh-ideate)), printing `<round> <state>` (`<round> blocked` once its threads are stopped); one whose children are all gone is written `blocked` directly with the same text, and one with children on an asleep box prints `<round> stays <state> until its children are stopped` (the daemon settles it once they are).
- A node already `done` or `blocked` prints `already <state>`, exit 0, stopping nothing but a stopped node's own thread on this box, which is interrupted again (harmless to an idle thread). A stopped node is `blocked`, so [`bmesh node forget`](#bmesh-node-forget) takes it.

**A stopped node** is a `blocked` one whose text starts `stopped by ` (`node stop`) or `stopped in ` (an interrupt in its runner). The daemon never wakes it for silence, a report, a reply or a message from a node under it; only a `bmesh send` from outside its tree after the stop resumes it, so a message from before the stop (a STOP sent while the thread ran) doesn't. The wake reads `bmesh: resumed by <sender>; 1 unread: message from <sender>. Run ...` (see [Config: Wake text](#wake-text)). It stays `blocked` until it reports again.

**An interrupt in the runner is a stop.** When the runner reports the newest run of a lead's (or engineer's) thread ended by the user (T3: the run `interrupted`; see [Runners](#runners)), the daemon's minute poll writes the node (in any state from `idle` through `working`) `blocked` with `stopped in <runner> by the operator` and publishes that `blocked` once; the same run never marks the node again, while a later one does. It is not woken for silence meanwhile, so interrupting a thread in T3 is enough. With a runner that can't tell (tmux, claude), the daemon still wakes an interrupted thread for silence; stop it with `node stop`.

Internals: [internals](internals.md#stopping-a-node)

## Checks and landing

Run checks, record their results, and land a branch on `main`.

### `bmesh routine`

Queues, runs and inspects a repo's routines. A kind says how a check runs; a job asks for one run (see [Routines](#routines)).

The subcommands differ in the node identity they need (`--node`, `BMESH_NODE` or the registered worktree; it is the reply's address):

| Subcommands | Node identity |
|---|---|
| `queue`, `go`, `wait` | needed |
| `run` | the node when one resolves, else the host name |
| `list`, `status` | none |

#### `bmesh routine list`

```
bmesh routine list [--json]
```

Prints the cwd repo's kinds with their instances and queue depth (waiting plus in-flight jobs).

| Flag | Default | Meaning |
|---|---|---|
| `--json` | off | print JSON |

See also: [Kinds](#kinds).

#### `bmesh routine status`

```
bmesh routine status [<kind>] [--json]
```

Prints a kind's (or every kind's) instances with host, state and model, then their unanswered jobs in queue order under `JOB KIND STATE HOLDER FROM`.

| Flag | Default | Meaning |
|---|---|---|
| `--json` | off | instances as `members`, `[{"path","host","state","model"}]`, and jobs as `jobs`, `[{"id","from","commit","state","holder","host","seq"}]` (`host` the box of the daemon holding it) |

**Output.** A job's state and holder:

| State | Means | Holder |
|---|---|---|
| `waiting` | in the inbox: never taken, handed back, or held by a daemon that died until its `AckWait` passes | `-` |
| `held` | taken by a daemon, not yet running | `daemon.<host>` until a pooled instance takes it into its batch, then that instance, or a fresh run's path |
| `running` | being run | the instance |

**Details.** A job is answered once a daemon acked it or the requester's inbox holds its reply. `AckWait` is the kind's longest `timeout` + 1m. The jobs of a daemon that has not reported within it read `waiting`.

See also: [Jobs](#jobs), [Modes](#modes).

#### `bmesh routine queue`

```
bmesh routine queue <kind> [--commit C] [--model M] [--effort E] [--go] [--rerun] [--again] [--text T] [-- args...]
```

Queues a job: one run of `<kind>` against a commit, run by a daemon that runs the kind. A lead or executor runs it; the reply reaches its inbox.

| Flag | Default | Meaning |
|---|---|---|
| `--commit C` | `HEAD` | the commit the job runs against, resolved in the cwd repo to a full SHA and tree |
| `--model M` | the kind's first model | one of the kind's `models` |
| `--effort E` | the model's | a model kind's effort for this job: `low`, `medium`, `high`, `xhigh` or `max` |
| `--go` | off | start the batch now (as [`bmesh routine go`](#bmesh-routine-go)) |
| `--rerun` | off | run even when a result is recorded or would carry |
| `--again` | off | review: queue although this branch already has a review result |
| `--text T` | none | text for a model kind |
| `-- args...` | none | the job's args, appended to a command or sent to a model |

**What it does**, in order:

1. Checks that the cwd worktree's `.bmesh/routines/` defines the kind.
2. Checks that `--model` is a model id and one of the kind's models, and that `--effort` fits the kind.
3. Checks that the kind isn't inline.
4. For `review` without `--again`, checks that the branch has no review yet. A review is one pass per branch: `queue review` is refused when the branch already has a `review` result for a commit that is an ancestor of HEAD but not of `main` (or `origin/main` when the repo has an origin, so a landed spec's review doesn't count). Only `--again` queues it, for code a rebase changed.
5. For a deterministic kind, checks the record. A recorded result, ok or failed, answers the job without a run, unless `--rerun`.
6. Checks the kind at the job's commit (`git show`, not the working copy). When that kind sets an `os`, `queue` compares it with the kind on `origin/main` (`main` without an origin) and refuses when main's `os` differs, when main lacks the kind (it has no feed anywhere), or when main can't be read. A commit with no `os` in the kind, or no kind file, isn't checked: it runs where main sends it.
7. For an OS other than this box's, checks that the commit is on an `origin` branch (`git branch -r --contains <commit> --list 'origin/*'` prints something).
8. Resolves the job's repo, the first of:
    1. the requester node's `repo`, when a registered node names one;
    2. the registry name whose path shares the cwd's git common dir (so a linked worktree, where leads live, names its main repo);
    3. else it refuses (exit 3).
9. Stores the job. A job queued twice is stored once. The daemon resolves the repo through its own registry.

**Output.**

| When | Prints |
|---|---|
| a recorded result answers the job | `answered from result <key> (job <id>)` |
| the job was queued before | `duplicate <id>` |
| the job runs on another OS | `queued <id> (runs on darwin)` |

**Refusals.**

| When | Exit | Message |
|---|---|---|
| the repo is not in the registry (named by the main repo directory's base name); a node's repo the registry lacks is refused the same way, as no daemon would run its job | 3 | `bmesh: <repo> is not in the registry` |
| the cwd worktree doesn't define the kind | 3 | `no kind <kind> in <worktree>` |
| the kind is inline | 3 | `bmesh: <kind> is inline: run it with bmesh routine run` |
| main's `os` differs from the commit's | 3 | (e.g.) `bmesh: <kind> runs on darwin at <commit8> but anywhere on main, so a job could reach a linux box: land the os change first, or give it a kind name of its own` |
| main lacks the kind | 3 | `bmesh: <kind> is not on main: land it first` |
| main can't be read | 3 | `bmesh: cannot read kind <kind> on <ref>: <why>` |
| another OS, and the commit is on no `origin` branch | 3 | `bmesh: push first: <kind> runs on darwin, which fetches the commit from origin` |
| the branch already has a `review` result, without `--again` (naming that result, its verdict, commit and time) | 3 | `this branch already has a review: <result key> (<ok\|fail>, commit <sha8>, at <at>). A review's findings are fixed and then landed; the second pass only after a rebase changed the code, with --again` |
| a `--model` with spaces or `;` | 2 | `model "<m>": want no spaces or ";"` |
| a `--model` the kind lacks | 2 | `kind <k> has no model "<m>"` |
| an `--effort` outside the five values | 2 | `effort "<e>": want one of low, medium, high, xhigh, max` |
| any effort for a command kind | 2 | `kind <k> runs a command: --effort is for a model kind` |

**Details.** A commit not in the daemon's clone (queued from another box) gets one `git fetch origin` before the job is answered `not a commit`: push the branch before queueing to a daemon elsewhere.

See also: [Jobs](#jobs), [`bmesh routine wait`](#bmesh-routine-wait), [Results](#results).

Internals: [internals](internals.md#queueing)

#### `bmesh routine go`

```
bmesh routine go <kind>
```

Tells the kind's pooled instances to start on the jobs they hold, without waiting for `batch_idle`. A go any box fetched starts every box's held batch.

**Output.** `sent <id>`.

See also: [Modes](#modes).

#### `bmesh routine wait`

```
bmesh routine wait <job id> [--timeout D]
```

Waits for a job's reply and prints its output. It finds the reply whether or not `inbox` already read it, for the stream's 30 days.

| Flag | Default | Meaning |
|---|---|---|
| `--timeout D` | 20m of queueing plus the kind's `timeout` plus the daemon's 1m when the cwd repo defines the kind (41m for a 20m kind), plus the setup's `timeout` for a command kind when the repo names a `[setup]`, else 20m | give up after this long |

**Output.** The job's output. It exits 0 when the job is ok, and 1 when it failed. A failed job's output is the daemon's reason when the daemon answered it `ok=false`.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| `--timeout` passes | 5 | `bmesh: timed out waiting for <id>; the job may still be queued or running, and its reply stays in the stream for 30 days: run bmesh routine wait <id> again rather than queueing it twice` |

**Details.** A reply `wait` found is not printed by `inbox` again and wakes no one. A reply further back than 100 unread messages stays unread and wakes the node once. A job only boxes without its repo see waits until `routine wait` times out, as does one of a kind no configured repo defines on its main.

See also: [Jobs](#jobs), [`bmesh inbox`](#bmesh-inbox).

#### `bmesh routine run`

```
bmesh routine run <kind> [--commit HEAD] [--model M] [--effort E] [--here] [--rerun] [--text T] [-- args...]
```

Runs a kind inline, in the requester's process, on the cwd worktree's HEAD, and records its result. No daemon is involved.

| Flag | Default | Meaning |
|---|---|---|
| `--commit HEAD` | `HEAD` | the commit to run; it must be HEAD |
| `--model M` | the kind's first model | one of the kind's `models` |
| `--effort E` | the model's | a model kind's effort: `low`, `medium`, `high`, `xhigh` or `max` |
| `--here` | off | run a fresh or pooled kind in this worktree |
| `--rerun` | off | run even when a result is recorded |
| `--text T` | none | text for a model kind |
| `-- args...` | none | the run's args |

**Output.** The run's output, then its verdict line, `<kind> ok|fail t:<tree8>[ m:<model>] (recorded)`, or `(answered from the record)` when a deterministic kind's record answered it (the recorded output is printed above). A `verify` with no record for HEAD's tree is answered from the nearest verify at or below HEAD that changed nothing under the repo's code (see **Carried verify**), and nothing runs: `verify ok t:<new8> (carried from t:<old8>: nothing under code changed)`. The line is a `verified` entry, not part of the recorded output. It exits 0 when the run is ok, and 1 when it failed. A job text's `base: <sha>` that isn't a commit records no patch-id; `routine run` says so on stderr.

**Carried verify.** A commit that changes nothing under the repo's code (its row's `code`, see [repos.toml](#repostoml)) has the code of its first parent. So `verify`, a deterministic kind with no record for the tree, looks down HEAD's first parents to the newest commit that changes code (`git rev-list --first-parent -1 HEAD -- <code>`), at most 100 commits, and takes the nearest one with a `verify` record of its own under the same args. A record carried there is passed over for the commits below it. If that record is ok and its tree differs from HEAD's only outside the code, it carries: it is stored for HEAD's tree, unless a record is already there, with HEAD as its commit and its output led by `carried from t:<old8>: nothing under code changed`, so `done` and `spec verified` match it as any record. If it is a fail, verify runs: a fail over the same code is a flake or a file `code` misses, and an older ok doesn't hide it. A branch with only docs since `main` reaches `main`'s verified commits this way. A change under the code, or to `.bmesh/`, runs it, and so does `--rerun`. No other kind carries.

**Refusals**, checked in this order:

| When | Exit | Message |
|---|---|---|
| the cwd worktree's `.bmesh/routines/` doesn't define the kind | 3 | `no kind <kind> in <worktree>` |
| the kind's `os` isn't this box's, `--here` included, since the result would stand as the kind's for the tree and answer every later job for it | 3 | `bmesh: mac-build runs on darwin; this box is linux: queue it` |
| a fresh or pooled kind without `--here` | 3 | `kind <kind> is <mode>; queue it, or pass --here to run it in this worktree` |
| a dirty tree, since every inline run records a result for HEAD's tree | 3 | `bmesh: worktree is dirty; commit first` |
| a `--commit` other than HEAD | 3 | `inline runs run HEAD; check out <commit> or queue the job` |
| a command kind's setup failed (see Details) | 3 | `setup failed: <why>; nothing recorded`, then the last 30 lines of its output |
| a command kind's setup left the tree dirty | 3 | `setup dirtied the tree: <paths>; nothing recorded` |
| a model kind's claude never answered (see Model kinds in [Kinds](#kinds)) | 3 | `model run failed: <why>; nothing recorded` |

It also refuses (exit 2) a `--model` with spaces or `;`, a `--model` the kind lacks, an `--effort` outside the five values, and any effort for a command kind, with the messages of [`bmesh routine queue`](#bmesh-routine-queue).

**Details.** An inline run leases the kind's `slots` for `timeout` + 1m, as the daemon does (see [Leases](#leases)). Before it (and after a record has answered nothing), a command kind runs the worktree's `[setup]` when its installs aren't current (see [Setup](#setup)), printing `bmesh: setup ran (<command>) in <duration>` on stderr. A setup that fails, or leaves tracked changes or untracked, unignored files, refuses the run: nothing runs or is recorded. A model kind runs no setup. `land` and the build's verify of `main` run inline kinds the same way.

See also: [Checkouts](#checkouts), [Results](#results), [Modes](#modes).

### `bmesh result`

Finds and records routine results. A `result` subcommand's usage error prints its own usage, flags included; `-h` on any subcommand names its flags.

#### `bmesh result find`

```
bmesh result find <kind> [--commit C] [--model M] [--json] [-- args...]
```

Prints the result of a kind at a commit's tree. It needs no node identity.

| Flag | Default | Meaning |
|---|---|---|
| `--commit C` | `HEAD` | the commit whose tree to look up |
| `--model M` | every model | the model of a model kind's result |
| `--json` | off | one object per line |
| `-- args...` | none | the args the result was recorded with |

**Output.** `<ok|fail> <tree> <at> <by>[ m:<model>]`: with `--model`, that model's result; else every result of the kind at the tree with those args, one line per model.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| no result | 3 | `no result for <result key>` |

See also: [Results](#results).

#### `bmesh result record`

```
bmesh result record <kind> --ok|--fail [--commit C] [--model M [--base B]] [--output FILE|-] [-- args...]
```

Stores a result by hand for any kind name, kind file or not. It uses the node when one resolves, else acts as the host name.

| Flag | Default | Meaning |
|---|---|---|
| `--ok` \| `--fail` | required | the run passed or failed |
| `--commit C` | `HEAD` | the commit the run checked |
| `--model M` | none | the model that ran, for a model kind |
| `--base B` | none | records the patch-id of `B..<commit>` as a review's; needs `--model` |
| `--output FILE\|-` | none | the file holding the run's output (`-` reads stdin) |
| `-- args...` | none | the run's args |

**Refusals.**

| When | Exit | Message |
|---|---|---|
| neither or both of `--ok` and `--fail` | 2 | `result record: exactly one of --ok and --fail; <usage>` |
| a result for HEAD while the worktree is dirty, as in `routine run` | 3 | `worktree is dirty; commit first` |
| `--base` without `--model` | 2 | `result record: --base needs --model (a model's review of the diff); <usage>` |
| `--base` gives an empty diff | 3 | `refused: <B>..<commit> is an empty diff` |
| a `--model` with spaces or `;` | 2 | `model "<m>": want no spaces or ";"` |

See also: [Results](#results), [The verified line](#the-verified-line).

### `bmesh lock-lease`

```
bmesh lock-lease acquire <slot> [--ttl D]
bmesh lock-lease renew <key> [--ttl D]
bmesh lock-lease release <key>
bmesh lock-lease run <slot> [--ttl D] -- <cmd...>
```

Takes, renews and gives back leases on a repo's slots (see [Leases](#leases)). It uses the node when one resolves, else acts as the host name.

| Flag | Default | Meaning |
|---|---|---|
| `--ttl D` | 10m | how long the lease lasts unrenewed; at least 3 s; for `acquire`, `renew` and `run` only |

**`acquire <slot>`** takes a place in the slot and prints the lease key. It exits 3 when the slot is busy.

**`renew <key>`** extends a held lease. It refuses a lease held by someone else (exit 3).

**`release <key>`** gives back a held lease. It refuses a lease held by someone else (exit 3).

**`run <slot> -- <cmd...>`** holds a lease while a command runs. It runs the command in its own process group, renews the lease every ttl/3, and exits with the command's code. On SIGINT or SIGTERM it kills the group, releases the lease and exits 1. When a renew fails, the lease is lost, the command is killed and `lock-lease run` exits 1.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| `--ttl` under 3 s | 2 | `lease <sub>: --ttl must be at least 3s` |
| a slot name that isn't valid | 2 | `invalid slot "<s>"` |
| the slot is busy | 3 | `slot <slot> is busy` |
| `renew` or `release` of a lease someone else holds | 3 | `lease <key> is not held by you` |
| `run`: the lease is lost | 1 | `lease lost` |
| `run`: SIGINT or SIGTERM | 1 | `interrupted` |

See also: [Leases](#leases).

Internals: [internals](internals.md#leases)

### `bmesh setup`

```
bmesh setup [--check] [--force]
```

Runs the cwd worktree's `[setup]` (its `.bmesh/config.toml`) when its installs aren't current, and marks them current (see [Setup](#setup)). A lead, executor or proposal thread runs it first in a repo that names a setup (its brief says so), and again after changing dependencies. It needs no mesh.

| Flag | Default | Meaning |
|---|---|---|
| `--check` | off | Run nothing: say whether the installs are current, and exit 1 when they are stale or missing. |
| `--force` | off | Run even when the installs are current. |

**Output.** The setup command's output as it runs, then `setup: ran (<command>) in <duration>`. `setup: current (<command>)` when there was nothing to run. `--check` prints `setup: current (<command>)`, or `setup: stale (<command>); run bmesh setup` (an input or the command changed since the last run) or `setup: missing (<command>); run bmesh setup` (never run here, or the last run failed) and exits 1. A worktree whose repo names no setup prints `setup: none (.bmesh/config.toml names no [setup])` and exits 0, `--check` too.

**Refusals.** Each message prints after `bmesh: `.

| When | Exit | Message |
|---|---|---|
| A positional argument, or `--check` with `--force` | 2 | `usage: bmesh setup [--check] [--force]` |
| Not in a git worktree | 2 | `bmesh setup runs in a worktree: <git error>` |
| The setup command failed or timed out | 1 | `setup failed: exit status <n>` or `setup failed: timed out after <d>` |

**Details.** Unlike a verify's setup, this one doesn't require the tree clean after it: the agent's own edits are its own. The hash is read from the working tree, so an uncommitted lockfile change makes the installs stale.

See also: [Setup](#setup), [`bmesh routine run`](#bmesh-routine-run).

### `bmesh land`

```
bmesh land [--spec NNN] [--size XS|S|M|L] [--ttl D] [--wait D]
```

Lands a lead's branch on `main` from its worktree in one command. It verifies, queues the review the spec's size calls for and waits for it, then lands under the lease `land.<repo>` (`<repo>` is the node's repo, else the registry row or base name of the main checkout), so two leads landing at once queue. It waits out what clears by itself and stops only for what needs the lead: a hold, the operator's mail, a conflict, a failing verify or review, scaffold drift, a spec that isn't `building`, or a HEAD that moved.

| Flag | Default | Meaning |
|---|---|---|
| `--spec NNN` | the node's spec | the spec it lands |
| `--size XS\|S\|M\|L` | the spec's `size` | the size the change was built at, when its scope grew past the spec's; it sizes the review land queues |
| `--ttl D` | 10m | how long the lease lasts unrenewed; at least 3 s |
| `--wait D` | 30m | how long `land` waits in all: for jobs, for the main checkout, for the lease and for `main` to stop moving (exit 5 after) |

The holder is the node, else the host name. A lead runs `land` in the background with a timeout 15 minutes above `--wait`, since a killed `land` can leave a rebase half done. A lead can instead hand its landing to its box's daemon with [`bmesh ready`](#bmesh-ready), which runs `land` for it.

**What it does.** Before the lease, `land`:

1. Stops (exit 3, nothing pushed) when run from a node held in `bm-holds` at either gate (see [Holds](#holds)).
2. Refuses (exit 3) a dirty worktree, and one not on a branch or on `main`.
3. Run from a node, reads its inbox as the daemon's wake does (see [Config: Wake text](#wake-text)), holding and acking nothing. An unread message sent by `operator` with `bmesh send` refuses it (exit 3); there is no `--force`. Anything else unread (a report, a reply, another node's message) is named on one line, each item as the wake names it, `unread: done from <path> (branch <b> at <sha8>); reply to job <id> (<kind> ok|fail); message from <path>`. Outside a node there is no inbox and no check.
4. Prints the kinds it will require of the landed tree and where each came from, `required: verify (the repo's verify kind), review (t:<tree8> at HEAD)`, or `review (t:<tree8> before the rebase at <sha8>)` for a result from before a rebase by hand (`required: nothing` when none). A kind it doesn't require that has a result on an earlier commit of the branch (its own first-parent commits back to its fork point from main, and those of a head a rebase by hand rewrote) is named after them, from the nearest such commit: `required: verify (the repo's verify kind); left behind by later commits: review ok t:<tree8> at <sha8> (not on the verified line)`, or `review fail ...` for a review that failed and was fixed. It isn't required because a review is one pass, its findings fixed in later commits and landed; a second pass (`routine queue review --again`) is for code a rebase changed.
5. **Verify first.** Runs the required inline kinds on HEAD, as step 4 under the lease runs them, so a review never reads code that fails its tests. A failing one prints its output and exits 1. Under the lease, a kind ok at the same tree here prints `<entry> (ran before the lease)` and isn't run again.
6. **The review, sized by the spec.** Run from a node, in a repo whose `review` kind is queued (not inline), prints one of:
    - `review: waiting for job <id>`: the node has an unanswered `review` job for a commit of the branch (one `land` queued before it timed out, or one the lead queued by hand); `land` waits for it instead of queueing another.
    - `review: on the branch already (ok|fail at t:<tree8>)`: a commit of the branch has a review result (the check `routine queue review` makes, including the heads a rebase by hand rewrote, so a hand rebase carries its review). It queues nothing, so a failing review that was then fixed lands on verify alone.
    - `review: none (size XS|S)`: the change is small enough to land on verify alone.
    - `review: queued <id> (medium|high, size M|L)`: it pushes the branch (`push origin <branch> <sha8>`, so a daemon on any box can fetch the commit; a failing push is said on stderr and `land` goes on) and queues `review` on HEAD on the kind's first model, started at once, its text `base: <merge-base origin/main HEAD>`, at `medium` for size M and `high` for L or larger, bmesh's done rule.

    The size is `--size`, else the spec's `size`. Only when `land` would queue does it need one: a spec with neither is refused (exit 2, `land needs --size: spec NNN has no size`), and so is a repo not in the registry, whose review no daemon would take (exit 3). Outside a node, or in a repo with no queued review kind, this step prints nothing.
7. **The node's jobs.** Waits for the reply to every unanswered job the node queued on HEAD of the repo's queued kinds, and to the review job of step 6. A job on an earlier commit is not waited for: its result isn't at HEAD's tree, and `land` requires what it would without it. Each job is named once, `waiting for job <id> (<kind>, waiting|held|running[ on <host>])`, and its reply looked for every 2 s. `land` sends each kind it waits on one `go`, as `routine go` does, so a job queued without `--go` starts. `land` acks each reply it reads, as `routine wait` does, so no wake follows.
    - An ok reply prints `job <id> (<kind>): ok t:<tree8>`.
    - A failing reply prints its output (a review's findings) and is refused (exit 3), so the lead never needs a `routine wait`.
    - A reply that recorded nothing because its model run never answered (no result, output `model run failed: <why>; nothing recorded`) is queued once more as it was, started at once: `job <id> (<kind>): model run failed: ...; queued <id> once more`. A second such reply, or any other that recorded nothing, prints its output and is refused (exit 3).

    Once the replies are in, `land` starts again from step 1, so the results they recorded are required (a second `required:` line). Outside a node it waits for no job.
8. **The main checkout.** While the main checkout's `main` has commits `origin/main` doesn't (someone's unpushed work, which `land` never pushes), prints `waiting for main in <path> to be pushed: it has <n> commits origin/main doesn't (someone's unpushed work; land never pushes it)` once per count and fetches again every 15 s.
9. While another holder has the lease, prints `waiting for land.<repo> (held by <holder>)` once per holder and retries every 2 s.

**Waiting.** Every wait (steps 7 to 9, and the main checkout and a moving `main` under the lease) reads the node's hold and its mail again at each poll: a hold refuses at once, as step 1 does, and so does an unread operator message, or any the operator sent since `land` started, read or not, since a lead woken mid-land can't act on it before the push (`refused: <n> message(s) from the operator since land started; ...`). `--wait` bounds all of the waiting together; past it, `land` exits 5 with `timed out waiting for <what>; nothing is pushed`, `<what>` the job, the main checkout or `main` it waited for (`timed out waiting for land.<repo>, held by <holder>` for the lease). Run again, it waits for the same jobs rather than queueing them twice. Run from a node, `land` publishes a `land` event under the node when it starts and then once a minute until it ends, its text what it is doing (`waiting for job <id> (review)`, `landing under land.<repo>`). It sets no state, but it is the node's activity, so a lead waiting in `land` isn't called silent, and `watch` shows where it is.

The lease lasts `--ttl` unrenewed and is renewed every ttl/3, so a dead lander blocks the next for at most that long. Holding it, `land` checks that the worktree is clean and that HEAD is the commit it waited on (or its own rebase of it), refusing `HEAD moved while land waited` (exit 3) otherwise, and reads the hold and the inbox again. A message arriving during the rebase and checks is not seen. It prints `lease land.<repo>`, then a line per step:

1. `fetch origin/main <sha8>`: `git fetch origin main`. While the main checkout's `main` has commits `origin/main` doesn't, `land` waits under the lease, as step 8 before it does. Without an origin the base is the local `main`.
2. `rebase onto <sha8>: N commits` (or `rebase: on <sha8> already`). When the rebase stops on a conflict, `land` resolves what a rule can (see Conflicts below): when every region of every conflicted file resolves, it adds the files, continues the rebase and prints `rebase: resolved <file> (<rules>)` per file, at each stop, `<rules>` from `kept both`, `applied both` and `by sentence`. Anything left aborts the rebase and is refused (exit 3), as is a rebase that fails for another reason. A branch with nothing beyond the base is refused (`nothing to land`), as are a spec the worktree doesn't have and a spec not reading `building` in the worktree.
3. `scaffold: follows the row`, for a repo with a `ship` row whose `AGENTS.md` (in the rebased tree) has a `<!-- bmesh:` marker. Each marked block of `AGENTS.md` that differs from the base's (`origin/main`'s), except the `rules` block, which is the repo's own (a branch that edits, re-picks or drops its rules lands), the verify kind's command when it differs from the base's, and, when the row has a `setup`, `.bmesh/config.toml`'s `[setup]` when it reads otherwise than the base's (a `[slots]` edit is the repo's own), is compared with what `scaffold` writes from the row and the binary. A file the branch didn't change isn't compared, so a row published mid-build, a newer snippet or another box's older binary never refuse a landing. A file `scaffold` would create isn't drift.
    - Drift prints the first 20 lines of its unified diff (`...` after them) and is refused (exit 3). Once the row is published with the new verify or setup, `bmesh scaffold <worktree>` writes the files on the branch.
    - A repo never scaffolded prints `scaffold: not scaffolded, not checked`; one with no `ship` row prints nothing.
    - A tree whose `go.mod` declares this binary's own module renders its own blocks, which this binary may predate, so only the verify kind is compared: `scaffold: the verify kind follows the row (AGENTS.md is rendered by this tree's own bmesh, not checked)`.
4. The results for the rebased tree. The required kinds are those with a result at the branch's tree before the rebase, and `verify` when the repo defines it.
    - **A branch rebased by hand** is read as it was before the rebase (see Details).
    - **An inline kind** runs as `routine run` does (a deterministic one is answered from its record; a command kind runs the worktree's `[setup]` first when its installs aren't current, and a failing setup refuses, exit 3, recording nothing) and prints its verdict line. A failing one prints its output and exits 1.
    - **A deterministic kind** (`verify`) with no result for the rebased tree first looks for an ok result at a tree that differs only outside the repo's code (its row's `code`, see [repos.toml](#repostoml); `git diff --quiet <old> <new> -- <code>`). It looks in this order: the last tree it was ok at in this run, the branch's tree before this pass's rebase, the tree its `required` line named. Then, for `verify`, the nearest record below the rebased HEAD and below each head from before the rebase, as `routine run` looks (see [Carried verify](#bmesh-routine-run)), so a branch with only docs since `main`, or a docs commit after the branch's verify, carries too. The first found is carried: stored again for the rebased tree and HEAD, with its output led by `carried from t:<old8>: nothing under code changed`, so `done` and `spec verified` match it as any record. It prints `verify ok t:<new8> (carried from t:<old8>: nothing under code changed)`, in seconds. A change under the code, or to `.bmesh/`, runs it.
    - **Another kind** (a review) prints the first that applies:
        1. its result for the rebased tree, `<entry> (recorded)`;
        2. a result recorded with the rebased diff's patch-id (see [The verified line](#the-verified-line)), `<kind> ok t:<new8> m:<model> c:<old8> (carried from t:<old8>: the diff is unchanged)`;
        3. an ok result from before the rebase with no patch-id whose diff the rebase left unchanged (the patch-id of the fork point to the old head equals that of the new base to the new head). Each model's result is stored again with that patch-id and lands on the line as a carried entry, `<kind> ok t:<new8> m:<model> c:<old8> (carried from t:<old8>: the rebase left the diff unchanged)`, which `done` matches as any carried entry. A result with no model has nothing `done` can match, so it carries off the line: `<kind> ok t:<old8> carried: the rebase left the diff unchanged (not on the verified line)`.

        A failing result, or a changed diff, is refused (exit 3) naming the kind and how to re-run it. A review whose diff the rebase changed, when `land` runs from a node in a repo with a queued review kind, is instead queued again by `land`: it prints `review has a result for t:<old8> but none for t:<new8>, and the rebase changed the diff`, releases the lease (`released land.<repo>`), pushes the branch and queues the review again on the rebased HEAD at the size's effort with the new `base:`, printing `review: queued <id> again (the rebase changed the diff)`, and starts again from step 1 before the lease. The worktree stays rebased, and the rebase's reflog entry is what step 4 before the lease reads, so what `land` requires is worked out again.
    - Any failing entry for the tree is refused too.
5. `verified: ...` and `spec NNN done: merged <sha8>, branch <b>`: the `verified` line as `spec verified --write` composes it, and `status: done` (from `building`) with `merged` the rebased HEAD and `branch` the worktree's branch. A spec whose `## Needs from the operator` section has steps also gets `operator_steps` (see Steps for the operator under [The ledger](#the-ledger)), printed `operator steps: N (bmesh spec steps NNN)`.
6. `commit <sha8> NNN: done at <merged8>`: the spec's change, committed on the branch.
7. `push origin main <sha8>`: `git push origin HEAD:refs/heads/main`, a fast-forward or nothing. When the push is refused because `origin/main` moved, `land` takes the record commit back off the branch, prints `main moved; again from the fetch` and restarts at step 1, as often as `main` moves while `--wait` lasts (past it, exit 5, `timed out waiting for main to stop moving; nothing is pushed`). Step 4 then carries the verify it just ran when what moved `main` is outside the code (a coordinator's ledger commit, or, with `code` set, a README commit). Any other failure takes the commit back and exits 1.
8. `main <sha8>`: the main checkout's `main` fast-forwarded as `choose-ideas` does (`merge --ff-only` when it is on `main`, else `fetch . <branch>:main`). When it can't, `land` says so on stderr and the landing stands, `origin/main` holding the commit. Without an origin this step is the landing, and a `main` that moved is retried as a refused push is.

**Output.** It ends `released land.<repo>` and `landed <sha8>`, the record commit `bmesh done --commit` names.

**Refusals.** When `land` stops at any step, nothing is pushed and the branch holds no record commit, so it runs again as it stands.

| When | Exit | Message |
|---|---|---|
| `--ttl` under 3 s | 2 | `land: --ttl must be at least 3s` |
| no `--spec` and no node spec | 2 | `land needs --spec NNN (no node spec resolves)` |
| `--spec` is not a number | 2 | `--spec "<NNN>" is not a number` |
| `--size` is none of XS, S, M, L (or XL and up) | 2 | `land: --size "<size>": want XS, S, M or L` |
| a review to queue and no size | 2 | `land needs --size: spec NNN has no size` |
| not a worktree of a repo with a main checkout | 3 | `refused: <top> is not a worktree of a repo with a main checkout` |
| a hold at either gate | 3 | `held <gate phrase> by <by> <age> ago: <why>; nothing is pushed: run bmesh held and end your turn, and run bmesh land again once the release wakes you` (e.g. `bmesh: held before land by <by> <age> ago: <why>; ...`) |
| a dirty worktree | 3 | `worktree is dirty; commit first` |
| not on a branch, or on `main` | 3 | `refused: land runs on a lead's branch, not <branch\|->` |
| an unread message from the operator | 3 | `bmesh: refused: <n> unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh land again` |
| a message from the operator since `land` started, read or not | 3 | `bmesh: refused: <n> message(s) from the operator since land started; nothing is pushed: run bmesh inbox, act on them, then run bmesh land again` |
| a review to queue in a repo not in the registry | 3 | `refused: <repo> is not in the registry, so no daemon takes its review; nothing is pushed` |
| a failing reply to a job it waited for | 3 | its output, then `refused: <kind> failed at t:<tree8> (job <id>); nothing is pushed: fix what it found, commit and run bmesh land again` |
| a reply that recorded nothing, a second time after a model run that never answered, or once for any other reason | 3 | its output, then `refused: job <id> (<kind>) recorded nothing; nothing is pushed: see its output above, then run bmesh land again` |
| HEAD or the worktree changed while it waited | 3 | `refused: HEAD moved while land waited (<sha8>, was <sha8>); nothing is pushed: run bmesh land again`, or `refused: HEAD moved while land waited: the worktree has changes not committed; ...` |
| `--wait` passes while another holder has the lease | 5 | `bmesh: timed out waiting for land.<repo>, held by <holder>` |
| `--wait` passes while it waits for anything else | 5 | `bmesh: timed out waiting for <what>; nothing is pushed`, `<what>` `job <id> (<kind>)`, `main in <path> to be pushed` or `main to stop moving` |
| the rebase fails | 3 | see below |
| nothing beyond the base | 3 | `refused: nothing to land: HEAD is on <base>` |
| the rebased ledger has two specs sharing an id (another writer landed the same number) | 3 | `refused: <file> and <file> share id NNN; renumber the newer with bmesh spec next-id` |
| the worktree has no such spec | 3 | `refused: no spec NNN in <top>` |
| the spec doesn't read `building` | 3 | `refused: spec NNN reads <status>, not building` |
| scaffold drift | 3 | see below |
| a failing inline kind | 1 | `<kind> failed at t:<tree8>; fix it, commit and run bmesh land again`; after a resolution, see below |
| an inline command kind's setup failed | 3 | as [`bmesh routine run`](#bmesh-routine-run) says |
| a failing result or a changed diff; a failing entry for the tree | 3 | names the kind and how to re-run it; see below |
| any other push failure | 1 | `push main: <git error>` |
| an interrupt (SIGINT or SIGTERM: Ctrl-C, or the daemon cancelling a [`bmesh ready`](#bmesh-ready) step) before the push | 1 | `interrupted; nothing is pushed`; under the lease, the rebase is aborted and the lease released first. An interrupt once the push to `main` has begun lets the push and the main checkout's fast-forward finish, and `land` reports what happened. |

- Rebase conflict: `bmesh: refused: rebase onto origin/main conflicts in <files>; it is aborted and nothing is pushed. Rebase yourself, resolve it, re-validate, then run bmesh land again`, `<files>` those no rule resolves. When a rule resolved other files, at this stop or an earlier one, `<files>` is followed by `, which no rule resolves (a rule resolves <files>)`. When no file conflicts, it reads `rebase onto <base> fails` in place of `rebase onto origin/main conflicts in <files>`.
- A failing inline kind after a resolution: `<kind> failed at t:<tree8>, after the rebase resolved <files> by rule: that is land's merge, and the branch is left rebased; fix it forward, commit, then run bmesh land again, which queues the review the new diff needs` (exit 1). Run again from a node, `land` finds no review on the branch (the reviewed commits are not under the fix) and queues one, as step 6 before the lease says.
- A required kind with no result for the tree: `refused: <kind> has no result for t:<tree8>; run it, then run bmesh land again`
- A required kind that failed before the rebase: `refused: <kind> failed at t:<tree8>, before the rebase; fix what it found, commit and run bmesh land again`
- A changed diff (a review that `land` doesn't queue again: run outside a node, or the repo's review kind is inline): `refused: <kind> has a result for t:<old8> but none for t:<new8>, and the rebase changed the diff: run it again on this tree (for review: bmesh routine queue review --again --go --text "base: <sha>", then routine wait), then run bmesh land again`
- No results at all: `refused: no results for HEAD's tree; run the routines first`
- A failing entry: `refused: <kind> fails at t:<tree8>; fix it, commit and run bmesh land again` (for a carried entry, `t:<c tree8>, the same diff`)
- Scaffold drift: `bmesh: refused: the branch's scaffolded files differ from what bmesh scaffold writes from <repo>'s row; the row's verify and setup are the source: bmesh repos propose with the new verify or setup, or restore the scaffolded files`

**Conflicts.** At each stop, every conflicted file is merged again from its three sides in diff3 style, and each conflict region is resolved by the first rule that applies:

1. **Kept both:** the base side is empty, so both sides only added lines at one point. Both are kept, main's lines first.
2. **Applied both:** the sides changed different lines, a conflict only because the changes touch. Lines both sides deleted count as agreed, and a change both sides made is made once. Both are applied. Lines both sides added at one point are kept as in rule 1. A change inside a run of lines the other side changed stays a conflict.
3. **By sentence:** the region is prose, either markdown outside code fences, tables and indented code, or lines that are all comments (`//` in Go and C-like files, `#` in shell, Python, TOML and YAML files) with one indent, holding no list. Each side is split one sentence per line (after `.`, `!`, `?`, `;` or `:` and before a capital, a quote, a backtick, an opening bracket or emphasis), and rules 1 and 2 are applied to the sentences. The sentences are then joined back into the lines they came from. A comment is wrapped again at the widest line any side had. A side whose line (or comment paragraph) doesn't split and join back to exactly itself is left alone.

Any other region stays a conflict, and so does every region in a markdown file's frontmatter, a spec's included. So does a file one side added or deleted, a symlink or submodule, and a binary file. The rules keep both sides' lines or sentences and never write one. Verify runs on every rebased tree, so a resolution that doesn't build fails it there. The branch is left as `land` rebased it, for the lead to fix forward. A resolution nearly always changes the diff's patch-id, so the review needs to run again, as step 4 says.

**Details.** A branch rebased by hand is read as it was before, by walking its reflog (`git reflog show refs/heads/<branch>`). When HEAD is the commit a rebase made (a `rebase (finish)` entry at HEAD; `rebase -i` and `pull --rebase` count), the entry before it, the head the rebase rewrote, counts as land's own head before the rebase. The walk goes on back while that head was itself made by a rebase. A commit made after the rebase ends the walk, and HEAD is read as it stands. HEAD is also read as it stands for a branch whose reflog has no rebase or has expired (`gc.reflogExpire`, 90 days), or that was rebased on another box. A kind with no result at HEAD's tree is required when that earlier head's tree has one, judged against that head and its fork point from main, as if land had done the rebase. So a review lost to a conflict the lead resolved by hand carries when the resolution left the diff unchanged, and is refused otherwise, as in step 4. A review's findings fixed in a commit and then rebased land on verify alone, because the fix's tree has no review; the review is named as left behind by later commits, as in step 4.

See also: [Holds](#holds), [`bmesh spec verified`](#bmesh-spec-verified), [The verified line](#the-verified-line), [`bmesh scaffold`](#bmesh-scaffold), [`bmesh done`](#bmesh-started-progress-blocked-ask-done).

Internals: [internals](internals.md#landing)

### `bmesh ready`

```
bmesh ready --text "<done line>" [--size XS|S|M|L]
bmesh ready --cancel
```

Hands a lead's landing to its box's daemon. The lead runs it once its work is committed and its last checks are done, then ends its turn. The daemon runs [`bmesh land`](#bmesh-land) and [`bmesh done`](#bmesh-started-progress-blocked-ask-done) for it, and wakes the lead only when land needs it, and once at the end for its closing line. While the request stands the lead's state is `landing`. A lead that would rather land in its own turn runs `bmesh land`, as it does where `ready` refuses.

| Flag | Default | Meaning |
|---|---|---|
| `--text "<done line>"` | required | the lead's done line; `done` carries it, followed by `; landed <sha8>, origin/main at <sha8> contains it` |
| `--size XS\|S\|M\|L` | the spec's `size` | passed to `land` as its `--size` |
| `--cancel` | off | withdraw the request (below) |

**What it does.** It checks the node, the worktree, the spec, the mail and the daemon, as the refusals below say. Then it writes the lead `landing` (`ready to land <sha8>`), writes `bm-ready` `<path>` with HEAD as the request's head, and publishes `ready`. It prints `ready: <host>'s daemon lands <sha8>; it wakes you only if land needs you`. A lead with a hold at either gate may run it: the daemon waits for the release.

**What the daemon does.** The daemon of the box the lead runs on acts on the `ready` event, on every message to an inbox and on each poll tick. It holds the claim `ready.<path>` in `bm-leases` while it works a request, so a second daemon, or one restarted beside a step still running, leaves it alone. A dead daemon's claim expires within 2 minutes. Each pass reads, in order:

1. **Landed.** The spec on `origin/main` reads `done` and its `merged` commit is an ancestor of the worktree's HEAD (a daemon that died after the push finds it here). The daemon fetches and runs `bmesh --node <path> done --commit <HEAD> --text "<done line>; landed <sha8>, origin/main at <sha8> contains it"`. `done` is idempotent, so a repeat records nothing twice. Then it sends the final wake, `bmesh: landed <sha8>, spec NNN done; origin/main at <sha8> contains it. Write your closing line; nothing else to run.` The request ends once the wake is delivered. A wake that still fails after an hour is logged and dropped, since `done` is the record. A `done` that refuses is handled as a refusal (step 5).
2. **Withdrawn.** The request ends, with nothing run, when it was cancelled, the node was stopped, or the lead's state is neither `landing` nor `held` (the lead acted itself). A message (`bmesh send`) sent after the request also ends it, and the daemon then wakes the lead for its mail, as the wake pass would. For a message or a cancel, a lead still `landing` or `held` is written `working`, `ready withdrawn: <why>`, so silence applies to it again.
3. **Held.** The lead has a hold: a `landing` lead is moved to `held` by running `bmesh --node <path> held`, which `watch` shows as the lead reaching its gate. Nothing else runs and nobody is woken. Once the release deletes the hold, the daemon writes the lead `landing` again and goes on.
4. **Land.** It runs `bmesh --node <path> land --wait 10m [--size S]` in the node's registered worktree, which must exist, and only while HEAD is the request's head or land's own rebase of it (read from the branch's reflog, as land reads a hand rebase). Exit 0: the next pass finds it landed. Exit 5 (timed out): the next pass runs it again, and land waits for the review job already out rather than queueing a second. A refusal because of a hold: step 3.
5. **Refused.** Any other refusal (a conflict, a failing verify or review, an operator message, scaffold drift, a HEAD that moved) keeps land's refusal and its last 40 lines, which carry a failing review's findings. The daemon wakes the lead at once, without waiting out `wake_every`: `bmesh: land refused and needs you: <refusal>`, then `land's last lines:` and the lines, then ``Fix it, commit, then run `bmesh ready` again.`` Once the wake is delivered, the request ends and the lead is written `working`, `land refused: <refusal>`, so silence applies as to any working lead. A thread that is running is tried again each pass. A send that fails for another reason for an hour writes the lead `blocked` and publishes it.

While land runs, the daemon reads the request, the lead's state and its mail every 2 s. It cancels the step when the request is cancelled or gone, the node is stopped, the lead reports an event of its own, or a message comes. It also writes the text of land's newest `land` event as the lead's `landing` text, for example `waiting for job <id> (review, running on <host>)`. A cancelled step is sent an interrupt. Land cleans up on its own context (a rebase in progress is aborted, the lease released) and is killed after 2 minutes. An update's restart cancels running steps the same way before the daemon re-execs. Every state the daemon writes for a lead is written only at the revision it read, so it never overwrites an event the lead reported meanwhile. Each write publishes a daemon event (`landing`, `land-refused`, `land-withdrawn`), so the map follows.

**`--cancel`** marks the request cancelled and waits, up to 3 minutes, until the daemon has cancelled any running step and ended it. With no daemon holding the claim it ends the request itself. It prints `cancelled: nothing is pushed; land in your own turn with bmesh land, or bmesh ready again`, and moves a `landing` or `held` lead to `working` (`progress`, `ready cancelled: ...`). A request land already refused is ended at once (`cancelled: land had refused: <refusal>; ...`). A step that had already pushed still gets `done` and the final wake: `too late: landed <sha8>; the daemon has recorded done and wakes you for your closing line` (exit 0).

**Refusals.**

| When | Exit | Message |
|---|---|---|
| no `--text`, or `--cancel` with `--text` or `--size` | 2 | `usage: bmesh ready --text "<done line>" [--size XS\|S\|M\|L] \| bmesh ready --cancel` |
| `--size` is none of XS, S, M, L | 2 | `ready: --size "<size>": want XS, S, M or L` |
| the node isn't a lead (a fixer, an executor, an engineer) | 3 | `refused: <path> is a <role>; only a lead's daemon lands it` |
| the node runs on another box | 3 | `refused: <path> runs on <host>, not this box (<this>); its daemon is there` |
| the node has no thread | 3 | `refused: <path> has no thread for the daemon to wake; run bmesh land` |
| the node's registered worktree isn't this checkout | 3 | `refused: <path>'s worktree is <worktree>, not this checkout <top>` |
| a dirty worktree | 3 | `refused: the worktree has changes not committed; commit them, then run bmesh ready again` |
| the spec doesn't read `building` | 3 | `refused: spec NNN reads <status>, not building` (or `refused: no spec NNN in <top>`) |
| an unread message from the operator | 3 | `refused: <n> unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh ready again` |
| a request already stands | 3 | `already ready; bmesh ready --cancel first` |
| this box's daemon doesn't land for leads: its newest `started` event has no `can` naming `land` (an older release, or no daemon ever started) | 3 | `this box's daemon doesn't land for leads; run bmesh land` |
| a state that can't move to `landing` | 3 | `refused: illegal state transition <from> -> landing` |
| `--cancel` with no request | 3 | `refused: <path> has no ready request` |
| `--cancel` and the daemon holds the claim for 3 minutes | 5 | `timed out waiting for the daemon to cancel the step; the request reads cancelled, and the daemon ends it` |

**Where a request shows.** `mesh-status` prints the state and, after the table, one line per request: `<path> landing 6m: <the landing text>` (or the wake still owed, `landed <sha8> ...` or `land refused ...`); `--json` carries the row's `ready`, `{"phase","head","at"}`. The map draws a `landing` lead with a calm badge, counts it on the strip (`1 landing`), and never puts it in the needs-you band (see [Map](#map)).

See also: [`bmesh land`](#bmesh-land), [Holds](#holds), [Wakes](#wakes).

Internals: [internals](internals.md#ready)

## Boxes

Join boxes, act on each other, and share the repo registry.

### `bmesh hosts`

```
bmesh hosts [--json]
```

Prints the roster joined with each box's presence. Any box on the mesh runs it. See [Hosts](#hosts) for the roster and presence.

| Flag | Default | Meaning |
|---|---|---|
| `--json` | off | print JSON |

**Output.** The first line is `roster v<N>, admin <host> <fingerprint>`. Then comes one row per roster box, with:

- its state: `awake`; `asleep` (the entry's server timestamp is over 3 minutes old); `unverified` (its signature fails; shown without notes); or `absent`;
- the `admin` and `secure` marks;
- its OS, revision, routines and repo count;
- `notes` when it has some;
- what it accepts, and when it was seen.

`--json` prints `{"roster_version","admin","fingerprint","hosts":[{"host":{row},"state","at","presence":{…}}]}`.

**Refusals.** These apply to every `bmesh hosts` command except [`--here`](#bmesh-hosts---here), and to [`bmesh sync`](#bmesh-sync):

| When | Exit | Message |
|---|---|---|
| `[hosts]` is off on this box | 3 | `` hosts are off on this box ([hosts] enabled = false): run `bmesh init --hosts` `` |
| this box hasn't joined | 3 | `` this box hasn't joined (no <state dir>/roster.json): run `bmesh init --hosts` `` |
| this box has no host key | 3 | `` no host key (<error>): run `bmesh init --hosts` `` |

See also: [Hosts: The roster](#the-roster), [Hosts: Presence](#presence).

#### `bmesh hosts --here`

```
bmesh hosts --here
```

Prints this box's session line, `<state dir>/this-box.txt`, and nothing else. The plugin's session hook runs it (see [The plugin](#the-plugin)). The daemon writes the file (see [Hosts: The session line](#the-session-line)).

**Details.**

- It never dials the hub and never runs or prompts for `init`.
- A file over 3 minutes old (its daemon stopped or can't reach the hub) gets `; as of <age> ago, not updated since`.
- With `[hosts]` off, or with no file, it prints nothing (exit 0).
- After the session line, with `[hosts]` on or off, comes the stale-hub line `hub runs <hub>, serve <serve>: restart bmesh-hub` (see [Several boxes](#several-boxes)).

#### `bmesh hosts --notes`

```
bmesh hosts --notes [--json]
```

Prints every box's notes in roster order. Notes hold a box's session guidance: what is local on it, what it can't reach, how it differs from the others (see [Hosts: Presence](#presence)).

| Flag | Default | Meaning |
|---|---|---|
| `--json` | off | print `{"<host>": "<notes>"}` |

**Output.** For each box: `## <host> (<state>)`, a blank line, then the notes. A blank line separates boxes. When no box has notes, it prints `no notes`. An unverified box's notes are withheld, with `bmesh: <host>: unverified, notes withheld` on stderr.

#### `bmesh hosts invite`

```
bmesh hosts invite <name> [--secure] [--accept a,b] [--replace]
```

On the admin box, prints a one-time code for a new box to join with, then waits. See [Hosts: Joining](#joining).

| Flag | Default | Meaning |
|---|---|---|
| `--secure` | off | the box is secure; its row gets an approve key |
| `--accept a,b` | `ping` | the ops the box accepts, comma-separated |
| `--replace` | off | the name may already be in the roster; the joining box replaces its row |

**Output.** `invite for <name>: <id>-<secret>`, then `` good once, until <hh:mm>: on <name> run `bmesh init --hosts` and type it at its prompt; waiting ``. The id (8 hex characters) isn't secret. The secret is 128 random bits in base32. The code works once, within 10 minutes. When the box joins, the command prints `joined: <name>, roster v<N>`.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| no name, or extra arguments | 2 | `usage: bmesh hosts invite <name> [--secure] [--accept a,b] [--replace]` |
| the name isn't one subject token | 2 | `host name "<name>": want lowercase letters, digits and single hyphens (at most 63)` |
| the name is the admin's own (its row carries the admin key) | 3 | `<name> is the admin box: its row carries the admin key, so no invite replaces it` |
| the name is already in the roster, without `--replace` | 3 | `<name> is already in the roster (invite it again with --replace)` |
| this box isn't the roster's admin | 3 | `only the admin box (<admin>) can <action>` |
| stdin isn't a terminal | 3 | `<action> needs the admin at this box's own screen (stdin is not a terminal)` |

Refused also as [`bmesh hosts`](#bmesh-hosts) is.

#### `bmesh hosts set`

```
bmesh hosts set <name> [--secure=true|false] [--accept a,b]
```

On the admin box, changes a box's row and publishes the next roster.

| Flag | Default | Meaning |
|---|---|---|
| `--secure=true\|false` | unchanged | mark the box secure or not; secure needs the box's approve key, which only a `--secure` invite gives |
| `--accept a,b` | unchanged | the ops the box accepts, comma-separated (`""` accepts none) |

**Output.** `roster v<N>: …`, in the form `roster v<N>: <name> <secure\|not secure>, accept <ops>`.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| no name, or neither flag given | 2 | `usage: bmesh hosts set <name> [--secure=true\|false] [--accept a,b]` |
| the name isn't in the roster | 3 | `<name> is not in the roster` |
| `--secure=true` for a box with no approve key | 3 | `<name> has no approve key: invite it again with --secure --replace` |
| this box isn't the roster's admin | 3 | `only the admin box (<admin>) can <action>` |
| stdin isn't a terminal | 3 | `<action> needs the admin at this box's own screen (stdin is not a terminal)` |

Refused also as [`bmesh hosts`](#bmesh-hosts) is.

See also: [Hosts: Other ops](#other-ops) (accepting `check-step`).

#### `bmesh hosts revoke`

```
bmesh hosts revoke <name>
```

On the admin box, publishes the next roster without the box. No other box can revoke a box.

**Output.** `roster v<N>: …`, in the form `roster v<N>: <name> revoked`.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| no name, or extra arguments | 2 | `usage: bmesh hosts revoke <name>` |
| the name isn't in the roster | 3 | `<name> is not in the roster` |
| the name is the admin's own | 3 | `the admin can't revoke itself` |
| this box isn't the roster's admin | 3 | `only the admin box (<admin>) can <action>` |
| stdin isn't a terminal | 3 | `<action> needs the admin at this box's own screen (stdin is not a terminal)` |

Refused also as [`bmesh hosts`](#bmesh-hosts) is.

See also: [Hosts: Threat model](#threat-model) (revocation is best-effort).

#### `bmesh hosts ping`

```
bmesh hosts ping [<host>] [--wait D]
```

Sends `ping` to every roster box (this box included), or to the one named, and prints each answer. A box answers `ping` with `<rev8>, roster v<N>`.

| Flag | Default | Meaning |
|---|---|---|
| `--wait D` | `10s` | how long to wait for answers |

**Output.** One line per box, `<host>: <answer>` (`k16: 1a2b3c4d, roster v2`). A silent box prints `<host>: asleep: queued until <YYYY-MM-DD hh:mm>`, with `no answer in <wait>` in place of `asleep` when its presence is awake.

Refused as [`bmesh hosts`](#bmesh-hosts) is.

See also: [Hosts: Jobs and replies](#jobs-and-replies).

#### `bmesh hosts replies`

```
bmesh hosts replies [--json]
```

Prints every reply this box's jobs got, each checked against the job in the stream and the roster.

| Flag | Default | Meaning |
|---|---|---|
| `--json` | off | print JSON |

**Output.** One line per reply: `<ts> <host> <op> <job id>: <text>`. A reply that fails the check shows as `unverified, withheld`.

Refused as [`bmesh hosts`](#bmesh-hosts) is.

#### `bmesh hosts held`

```
bmesh hosts held [--json]
```

Lists the levels this secure box holds for the operator and, on the admin box, the registry proposals. See [Hosts: Levelling](#levelling) and [Hosts: The registry](#the-registry).

| Flag | Default | Meaning |
|---|---|---|
| `--json` | off | print JSON |

**Output.** One line per hold:

- a level: `<id> <repo> +<N> commits to <target8>, from <sender>, expires <day hh:mm>`;
- a registry proposal: `<id> registry proposal, <N> row changes against v<base>, from <box>, expires <day hh:mm>`.

Refused as [`bmesh hosts`](#bmesh-hosts) is.

#### `bmesh hosts approve`

```
bmesh hosts approve <id>
```

At the box's own screen, approves a hold: a held level on a secure box, or a registry proposal on the admin box.

**What it does** for a held level:

1. It shows the log again.
2. It reads the approve key (the admin key on the admin box), through the announced Keychain dialog or the passphrase (see [Hosts: Keys](#keys)).
3. It signs an approval of the job at the target the operator saw, and sends it to the box's daemon.
4. The daemon checks that the approval is signed by the box's roster approve key, names the hold's job and target, and is within 5 minutes of its clock.
5. The daemon checks that the job's signature still verifies against its sender, and that the sender is still in the roster. Else it drops the hold.
6. The daemon levels. When origin moved since the hold, nothing moves: the daemon updates the hold to the new target and log, and the command shows them and prompts again (`Approve <repo> at <target8>? [y/N]`), signing again with the key it read.

Approved, it prints `<repo>: fast-forwarded <N> (approved)`. The job's sender gets this as a second reply. An answer other than yes to the prompt prints `left held: <id>` and exits 0; the hold stays.

**What it does** for a registry proposal, at the admin's screen: it shows the diff, reads the admin key and publishes the proposal as the next version, `registry v<N> published: proposal from <box> approved`. The proposer gets the reply `approved: registry v<N> (<N> row changes)`. While the admin's own `repos.toml` has changes the registry doesn't, the approval is refused and the proposal stays held.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| this box holds no hold with that id | 3 | `` no hold <id> on this box (`bmesh hosts held` lists them) `` |
| stdin isn't a terminal | 3 | `approving needs the operator at this box's own screen (stdin is not a terminal)` |
| the roster gives this box no approve key | 3 | `<host> has no approve key in roster v<N>: ask the admin to invite it again with --secure --replace` |
| no daemon runs on this box | 3 | `` no daemon on <host> answered: is `bmesh serve` running with [hosts] enabled? `` |
| the daemon answers that it did not level | 3 | `not levelled`, after the daemon's answer |
| the approve key read from the store isn't the roster's | 3 | `the <name> key in the <store> store isn't the approve key the roster names (<fingerprint>)` |
| no answer to the re-prompt (stdin closed) | 3 | `left held: no answer` |
| a registry proposal that expired, doesn't verify, or was superseded; the hold is dropped and the proposer told | 3 | `<reason>: hold <id> dropped` |
| a registry proposal while the admin's `repos.toml` has changes the registry doesn't; the proposal stays held | 3 | see below |

The message for a registry proposal while the admin's `repos.toml` has changes: `<path> has changes registry v<N> doesn't, which approving would overwrite: undo them, or publish them and have <box> propose again; the proposal stays held`.

Refused also as [`bmesh hosts`](#bmesh-hosts) is.

#### `bmesh hosts deny`

```
bmesh hosts deny <id>
```

Drops a hold: a held level, or a registry proposal. For a held level, the job's sender gets the reply `denied by the operator`.

**Output.** For a level, `denied <id>: <repo> stays at <head8>`. For a registry proposal, `denied <id>: the registry stays at v<N> without <box>'s proposal`.

**Refusals.** An id with no hold is refused as [`bmesh hosts approve`](#bmesh-hosts-approve) refuses it.

### `bmesh sync`

```
bmesh sync [--repo R | --all] [--wait 30s]
```

Levels this box's checkout of a repo with origin, then asks every other box that has the repo to level too. See [Hosts: Levelling](#levelling).

| Flag | Default | Meaning |
|---|---|---|
| `--repo R` | the registered repo of the current directory | the repo to level |
| `--all` | off | every registered repo this box has |
| `--wait D` | `30s` | how long to wait for the other boxes' answers |

**What it does**, in order:

1. It levels this box's checkout of the repo. Unlike a self-level, it may push when the checkout is ahead.
2. It sends `level` with origin's head to every roster box that accepts `level` and whose verified presence lists the repo.
3. It prints the answers.

**Output.** One line per box: `k16: fast-forwarded 3`, `ricks-macbook: held for approval (+1 commit)`, `ricks-macbook: asleep, queued until <day hh:mm>`. With several repos, each line starts with `<repo>: `.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| the registry doesn't name the repo | 3 | `<repo> is not in the registry` |
| this box hasn't checked the repo out | 3 | `no checkout of <repo> on this box` |

Refused also as [`bmesh hosts`](#bmesh-hosts) is.

### `bmesh prune`

```
bmesh prune [--repo R | --all] [--apply] [--wait 30s]
```

Judges this box's bmesh worktrees and `bmesh/*` branches (keep or remove, with the reason), then asks every other box that accepts `prune` to do the same. It is a dry run unless `--apply`. See [Hosts: Pruning](#pruning) for the rules.

| Flag | Default | Meaning |
|---|---|---|
| `--repo R` | the registered repo of the current directory | the repo to prune |
| `--all` | off | every registered repo |
| `--apply` | off | remove the remove rows |
| `--wait D` | `30s` | how long to wait for the other boxes' answers |

**What it does**, in order:

1. It prunes this box.
2. It sends `prune` to every other roster box that accepts it.
3. It prints each box's lines.

With `[hosts]` off it prunes this box only.

**Output.** Each box's lines as `<box>: <line>`, or `<box>: asleep, queued until <day hh:mm>`. The line forms are in [Hosts: Pruning](#pruning).

**Refusals.**

| When | Exit | Message |
|---|---|---|
| `--repo` names a repo the registry doesn't have | 3 | `<repo> is not in the registry` |
| no `--repo` or `--all`, outside a registered repo's checkout | 2 | `not in a registered repo's checkout: name one with --repo, or --all` |

**Details.**

- It exits 1 when a removal on this box failed.
- With `[hosts]` on but this box unable to act as a host (not joined, say), it prints `bmesh: <error>; pruning this box only` on stderr and prunes this box only.

### `bmesh repos`

```
bmesh repos [--json]
```

Prints the registry, `repos.toml`, as a table sorted by name, with the columns name, mode, remote, verify, host, ux_host, surfaces and pin. See [repos.toml](#repostoml) for the file and [Hosts: The registry](#the-registry) for the signed registry.

| Flag | Default | Meaning |
|---|---|---|
| `--json` | off | print the rows |

**Output.** `--json` prints an array of `{"name","path","mode","remote","host","ux_host","verify":[],"surfaces":[],"pin","routines":[],"code":[]}`, defaults filled in.

#### `bmesh repos show`

```
bmesh repos show <name> [--json]
```

Prints one row as `key: value` lines.

| Flag | Default | Meaning |
|---|---|---|
| `--json` | off | print one object, in the shape `bmesh repos --json` uses |

**Output.** A row without `code` prints `code: (everything but the spec files)`, and one without `setup` prints `setup: (none)`. A row with `setup` adds `setup_inputs:` (`(none: once per worktree)` when it names none) and `setup_timeout:` (`10m` when it names none).

**Refusals.**

| When | Exit | Message |
|---|---|---|
| an unknown name | 3 | `no repo "<name>" in <path> (bmesh repos lists them)` |

#### `bmesh repos publish`

```
bmesh repos publish
```

On the admin box, with `[hosts]` on, publishes this box's `repos.toml` as the next registry version, signed with the admin key.

**What it does**, in order:

1. It takes a newer registry from the hub first, when there is one, and then stops, refused (see the Refusals below).
2. It prints the row diff from the current registry to this box's `repos.toml`: `registry v<N> -> v<N+1>:`, then `+ <name>: <field> <value>, …`, `- <name>` and `~ <name>: <field>: <old> -> <new>` lines, or `(comments and layout only)`.
3. It reads the admin key (the announced dialog: `publish registry v<N+1>`).
4. It publishes the next version: `registry v<N+1> published`. The version comes from the box's own record, never the hub's.

**Output.** An unchanged file prints `registry v<N>: no change`.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| the hub had a newer registry, now written as this box's `repos.toml` | 3 | `took registry v<N> from the hub as <path> (your file is kept as repos.toml.prev): bring your edits over and run this again` |
| this box isn't the admin | 3 | `` only the admin box (<admin>) publishes the registry: run `bmesh repos propose` to send it this box's changes `` |
| the hub has a newer registry this bmesh can't read | 3 | `` the hub has registry v<N>, which this bmesh can't read: <error>; run `bmesh update` `` |
| two names share a node token | 3 | `bmesh: repos Foo and foo are both foo in node paths; rename one` (see [Names](#names)) |

#### `bmesh repos propose`

```
bmesh repos propose [--wait 30s]
```

On any box but the admin, with `[hosts]` on, sends this box's `repos.toml` to the admin box, which holds it for the operator's approval.

| Flag | Default | Meaning |
|---|---|---|
| `--wait D` | `30s` | how long to wait for the admin box's answer |

**What it does**, in order:

1. It takes a newer registry from the hub first, when there is one, as [`bmesh repos publish`](#bmesh-repos-publish) does, and stops there with the same message.
2. It prints the same diff as `publish`, against the registry it has: `proposed against registry v<N>:`. An unchanged file prints `no change against registry v<N>` and stops.
3. It sends the box's `repos.toml` to the admin box as a `propose` job (args `[<base version>, <rows>]`). The admin box's row must accept `propose`.
4. The admin's daemon refuses rows it can't read, and a proposal against an older version than its registry: `refused: proposed against registry v<N>; it is v<M> now: propose again`.
5. Otherwise the admin's daemon holds the proposal with its row diff, pushes `<admin> holds: registry proposal from <box>, <N> row changes` and answers `held for approval as <id> (<N> row changes against registry v<base>)`. A newer proposal from the same box supersedes it.

Nothing changes until the operator approves it with [`bmesh hosts approve`](#bmesh-hosts-approve) on the admin box. [`bmesh hosts held`](#bmesh-hosts-held) lists it. A proposal the registry has moved past, or past its job's expiry, is dropped and its proposer told so. [`bmesh hosts deny`](#bmesh-hosts-deny) drops it too.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| the roster names no admin box | 3 | `roster v<N> names no admin box` |
| this is the admin box | 3 | `` this is the admin box: publish with `bmesh repos publish` `` |
| the admin's daemon answered with a refusal (printed first) | 3 | `not proposed` |
| the hub has a newer registry this bmesh can't read | 3 | as [`bmesh repos publish`](#bmesh-repos-publish) |
| two names share a node token | 3 | as [`bmesh repos publish`](#bmesh-repos-publish) (see [Names](#names)) |

#### `bmesh repos remove`

```
bmesh repos remove <name> [--checkouts [--ignored-ok]] [--apply] [--wait 1m]
```

On the admin box, publishes the next registry without the row. With `--checkouts`, it first asks every box that lists the repo to remove its checkout (the `nix` op, see [Hosts: Other ops](#other-ops)).

| Flag | Default | Meaning |
|---|---|---|
| `--checkouts` | off | first send `nix` to every box that lists the repo, then remove the row |
| `--ignored-ok` | off | a checkout's ignored files go with it, rather than refusing it |
| `--apply` | off | do it; without it, print what would be done |
| `--wait D` | `1m` | how long to wait for the boxes' answers |

**What it does** with `--checkouts`, in order:

1. It takes a newer registry from the hub first, when there is one, as [`bmesh repos publish`](#bmesh-repos-publish) does, and stops there with the same message.
2. It sends `nix` to every roster box whose verified presence lists the repo, the admin box too. Each box must accept `nix`. One read of the admin key signs the jobs and the registry.
3. It prints each answer.
4. Once every box has answered `removed` or `absent`, it publishes the next registry without the row: `registry v<N>: <name> removed`.
5. Until then it prints `pending: <box>, …; the registry keeps <name> until each answers removed or absent (run this again to re-ask them)` and remembers the boxes and their jobs. Run again, it counts the answers that arrived since and re-asks only the boxes still pending.

Without `--apply` it prints the boxes it would ask and the version it would publish (`would publish registry v<N+1> without <name> (run again with --apply)`), and sends nothing. Without `--checkouts` it only publishes the registry without the row. With `[hosts]` off, `bmesh repos remove <name> [--apply]` takes the row out of this box's `repos.toml` (keeping `repos.toml.prev`): `removed <name> from <path>`, or without `--apply`, `would remove <name> from <path> (run again with --apply)`.

**Refusals.**

| When | Exit | Message |
|---|---|---|
| this box isn't the admin | 3 | `only the admin box (<admin>) removes a row from the registry` |
| the registry has no such row | 3 | `<name> is not in registry v<N>` |
| the row has no `remote` | 3 | `<name> has no remote in the registry: nix matches each checkout's origin against it` |
| this box's `repos.toml` has changes the registry doesn't | 3 | `` <path> has changes registry v<N> doesn't: publish them first (`bmesh repos publish`) `` |
| with `[hosts]` off, the row isn't in this box's `repos.toml` | 3 | `no repo "<name>" in <path>` |
| `--checkouts` with `[hosts]` off | 3 | `` --checkouts sends nix to every box: it needs [hosts] enabled (`bmesh init --hosts`) `` |
| the hub has a newer registry this bmesh can't read, or had a newer one it just took | 3 | as [`bmesh repos publish`](#bmesh-repos-publish) |

## Concepts

The model behind the commands.

### Several boxes

A mesh spans boxes through one NATS hub.

- **A single box** embeds the hub in `bmesh serve` (`[mesh] nats = ""`).
- **Several boxes:** [`bmesh hub`](#bmesh-hub) runs the hub on one box, and a leaf on any other box that wants one. Every box still runs its own `bmesh serve`.

`bmesh hub` is a separate process because the daemon restarts on every update (see [Updates](#updates)) and would drop every box each time. The hub restarts itself only once serve runs a new release, so on `[update] track = "latest"` it restarts only when you restart its service (see [`bmesh hub`](#bmesh-hub)).

**A box's part.** [`bmesh init`](#bmesh-init) sets it, with `--role`:

| Part | What it runs | For |
|---|---|---|
| **hub** | the mesh's hub | an always-on box |
| **leaf** | a local server on `127.0.0.1:4222` that relays to the hub at `remote` | a box that sleeps or roams: it always has a server to reach, and the leaf reconnects to the hub by itself |
| **client** | nothing; it dials the hub | any other box |

**Stale hub.** When `bmesh hub` runs an older binary than `bmesh serve`, some commands print a line naming it; see [`bmesh hub`](#bmesh-hub).

**The hub** runs on an always-on box, on its tailnet address, never a LAN or public one. The tailnet is the boundary, and the shared token is the only auth.

**The token** is one random value, the same on every box, in `[mesh] token_file`, owner-only.

**Your own `nats-server`** works instead of `bmesh hub`, from a `nats.conf` holding the token. bmesh needs only `[mesh] nats` and `token_file`.

- **Hub:** `listen`, `authorization { token }`, `jetstream { domain: hub, store_dir }` and `leafnodes { listen, authorization { user: leaf, password: <token> } }`.
- **Leaf:** `leafnodes { remotes = [ { url: "nats://leaf:<token>@<hub>:7422" } ] }`, no `jetstream`.

**Joining the roster.** Boxes that share the hub see each other's nodes. To act on each other (there is no SSH between them), they join the admin's roster (see [Hosts](#hosts)): `bmesh init --hosts --admin` on your box, `bmesh init --hosts` with the admin's one-time code ([`bmesh hosts invite <name>`](#bmesh-hosts-invite)) on each other box, then restart `bmesh serve`.

**Secure boxes.** A **secure** box gets an approve key: what other boxes ask of it waits for your approval at its screen. Mark secure the box whose checkouts and plugins matter most, usually the laptop you type on.

- **The admin and approve keys** are the two keys no agent can use. Their commands run at the box's own screen, never over SSH (see [Hosts: Keys](#keys)).
- **`secure` covers** roster ops (`level` holds and the approve key, see [Hosts](#hosts)), not routines. A box with routines on runs any pushed commit's kind command for any mesh member. Routines are on by default (`[runner] checkouts`, default `~/.bmesh/checkouts`; `""` turns them off). `init --hosts` prompts for them, default no on a secure box. A secure Mac meant to run `os = "darwin"` kinds answers yes.

**Sleeping boxes.** A sleeping laptop drops off the mesh and returns on its own. Its leaf reconnects, and its daemon detects the wake, re-reads the roster and takes the jobs it missed, in order. An expired job is answered and logged, not pushed (see [Hosts](#hosts)). Until then, `bmesh hosts`, every box's session line and `bmesh hosts ping` show the box `asleep`. Keep the hub on a box that doesn't sleep.

**Which box a session is on.** With `[hosts]` on, the plugin's session hook opens every session with the line the daemon writes each minute, e.g. `this box is hub-box; secure: no; on the mesh: laptop (asleep 2h, secure, admin)`. A box's notes for sessions, `<config dir>/notes.md`, travel signed in its presence. [`bmesh hosts --notes`](#bmesh-hosts---notes) prints every box's (see [Hosts](#hosts)).

Internals: [internals](internals.md#several-boxes)

### Updates

**What a box follows.** `[update] track` (default `release`) names the commit that every update path (the daemon's self-update, the notice, [`bmesh update`](#bmesh-update)) goes to: the target. The code head is the newest commit on `main` that changes the repo's code (its row's `code`, see [repos.toml](#repostoml)). It is the commit `build` verifies and `release` tags.

- `release`: the newest release, the highest `vX.Y.Z` tag in `main`'s history (see [`bmesh release`](#bmesh-release)). A pre-release such as `v0.2.0-rc1` is passed over. With no release tag yet, the target is the code head, and the daemon logs once: `update: no release tag on main yet; following its code head`.
- `latest`: the code head. A commit outside the code (a dispatch, review or `done` record; with `code` set, a README, a document or art) never causes a rebuild.

**Where a binary stands.**

- A binary at the target, or with the target in its history (built from `main`, or by `update --latest`, past the newest release), is current and is never moved back.
- A binary the target doesn't contain (a rollback, or another line) is left alone: the daemon logs it once, and `bmesh update` refuses it (exit 3).
- A binary installed as `go install <module>/cmd/bmesh@vX.Y.Z` names a release, which every path first resolves to its commit on `main`. For a release `main` lacks, the daemon logs once `update: this binary's release is not on main; left alone`.

Updates name both ends by release: `v0.1.0` for a tagged commit, `v0.1.0+2 (<rev8>)` two commits past it, `<rev8>` with no release in its history.

**The daemon's self-update.** With `[update] repo = "<registry name>"` (default `""`: off), the daemon keeps itself current. Once a minute (it needs `[runner] checkouts`), it compares the target on that repo's main checkout with the binary at its path. When the target is ahead, it:

1. Builds `./cmd/bmesh` at the target and runs the build's `version`. A build that doesn't name the target's commit is refused, `update: built binary reports <x>, not <new8>; keeping this binary`, logged once per target.
2. Checks the new binary's config and upgrade walk. When either needs the operator, it holds the update (see [Upgrading](#upgrading)).
3. Moves the running binary to `bmesh.prev` beside it and the build to its path, and logs `update: v0.1.0 -> v0.2.0`.
4. Drains: it stops taking jobs, kills every running job, and hands every job it held or ran back to the inbox for the restarted daemon (see [Routines: Jobs](#jobs)).
5. Stops serve and starts the new binary with the same arguments. On Windows `serve --restart` starts it 5 seconds later.

A box on `release` restarts once per release, on `latest` once per code landing. Clients reconnect to an embedded hub as after any restart.

- A failed build is logged once per target; the daemon runs on.
- On Windows, a `bmesh.prev` that can't be removed (the supervisor's own image) is moved to `bmesh.prev.old` first, so you may see that file beside the binary.
- A binary with neither a revision nor a release tag never updates.
- A daemon that finds its last update never started (the new build crashed at start; `bmesh.prev` was put back by hand) logs it and pushes `bmesh update failed on <host>` (priority `high`) through `[notify]` (see [Pushes](#pushes)).
- A Windows daemon not under `serve --restart` refuses `[update] repo`, since its exit would stop it for good. It logs once at start `update: [update] repo needs bmesh serve --restart on Windows, which starts the new binary (init --service sets it up); off`. The notice still runs from the row.

**The hint.** When this box's daemon (another box's, if this box runs none) runs a revision other than this binary's, every command that connects to the hub ends with one stderr line, after its output and error. The exit code is unchanged.

- `bmesh: k16 runs 1a2b3c4d, this is 9e8f7a6b; run bmesh update`;
- or, when this box's own daemon is the older of the two: `bmesh: this box's daemon runs 1a2b3c4d, this binary is 9e8f7a6b; restart the daemon: <command>`, with `<command>` as in [`bmesh update`](#bmesh-update)'s restart line.

There is no hint for a binary or daemon of unknown revision, for commands that reach no hub (`help`, `version`, `init`, `repos`, `spec list|set|template`, `scaffold`), or for `update`, which prints its restart line instead.

**The notice.** A daemon not self-updating from `[update] repo` checks the target every `[update] check_every` (default `6h`; `0` is off; it needs `[runner] checkouts`). When the target is ahead of the binary at its path, it writes one line to `<state dir>/update.txt`:

- for a release: `bmesh: v0.2.0 is out (you run v0.1.0: 046, 047); run bmesh update, then update the buildermesh plugin`;
- for a code head (`track = "latest"`, or no release yet): `bmesh: 9 commits behind main (landed since: 032 The Quickstart works as written, 033 …); run bmesh update, then update the buildermesh plugin`. It counts only commits that change the repo's code, and lists the specs landed since, oldest first, each with its title (the id alone without one).

The daemon removes the file when the binary is current, ahead, or outside the target's history (another line, or unknown revision). With no check (`check_every = 0`, self-updating from a row, or no checkouts root), it removes the file at start. A failed fetch is logged and tried again at the next check.

[`bmesh update --notice`](#bmesh-update) prints the file. [`bmesh mesh-status`](#bmesh-mesh-status) prints the line among its closing lines: after the T3 sign-in line and before the hub line.

**Installing from upstream.** With `[update] upstream = true` (and no `[update] repo`), the check also installs. When the target is ahead, it runs the same `go install` and swap as `bmesh update`, then drains and restarts as the self-update from a row does.

- A failed or refused install is logged once per target (`update: install failed; keeping this binary`), leaves the notice written, and is tried again at the next check. The module proxy can lag a fresh commit by minutes.
- On Windows it runs only under `serve --restart`. Without it, the daemon logs once at start `update: [update] upstream needs bmesh serve --restart on Windows, which starts the new binary (init --service sets it up); notice only` and stays on the notice.
- A binary without a revision, or without a path to replace, stays on the notice too, logged once.
- `check_every = 0` turns it off.

Internals: [internals](internals.md#updates)

### Upgrading

**From before v0.1.0: start over.** v0.1.0 (tagged at `61587e32`) predates upgrade notes. Its notes, `docs/upgrading/v0.1.0.toml`, written after it, are the one exception to stepping a box forward: an install from before v0.1.0 is uninstalled and set up again, NATS included. In order:

1. Check that no lead or executor is live (`bmesh mesh-status --all`). Finish or forget (`bmesh node forget <path>`) any that is.
2. Stop and remove the `bmesh` and `bmesh-hub` services and any `nats-server` service, hub or leaf (systemd user units, launchd agents, the Windows logon task). Stop a `bmesh serve` or `nats-server` run by hand yourself.
3. Remove the `nats-server` installed for bmesh, its `<config dir>/nats.conf`, and the JetStream `store_dir` that file names.
4. Move the config dir aside as `<config dir>.pre-0.1.0` (keep its `repos.toml`). Remove the state dir and the embedded hub's store.
5. Run `go install github.com/rickdiculousli/buildermesh/cmd/bmesh@v0.1.0` (or the release you go to; `bmesh update` has already done this when it walks the notes).
6. On the hub box first (several boxes, role hub), run `bmesh init --service`, then `bmesh init --hosts --admin`.
7. On every other box, run `bmesh init --service` (leaf or client) and `bmesh init --hosts` with an invite from the admin box (`bmesh hosts invite <box>`).
8. Put the kept `repos.toml` back and publish it from the admin box (`bmesh repos publish`).

A fresh start loses the old JetStream store's results and events, and the roster and its keys. Every spec's `verified` line keeps its record in git. Every later release is incremental: its notes hold only the steps an install at the release before needs.

**The notes walk.** [`bmesh update`](#bmesh-update) walks a box through the notes of every release between the binary it runs and the one it installs, oldest first (from v0.1.0 to v0.3.0: v0.2.0's notes, then v0.3.0's).

- Notes come from `docs/upgrading/<tag>.toml` on the update repo's `main`, not the tag, so notes written after a tag still apply. The format is in [Upgrade notes](#upgrade-notes).
- A binary without v0.1.0's commit in its history predates it; its walk starts with v0.1.0's start-over.
- The new binary does the walking, since the old one may predate walks. Before the swap it checks the config and answers the asks; after the swap it runs the steps. [`bmesh update`](#bmesh-update) lists each prompt, output and refusal.

**Where a box stands.** `bmesh init` and every finished walk record the box's release. A binary can be swapped in without a walk: by a binary up to and including v0.1.0, whose `update` and self-update know no walk, or by the daemon (below). At its first start it finds notes pending when its release is past the recorded one (v0.1.0 with no record):

- Every command run at a terminal (and `bmesh update --notice`, which the session hook runs) ends with `bmesh: upgrade notes pending since v0.1.0: run bmesh update`. `serve` prints it at start as `bmesh serve: upgrade notes pending since v0.1.0: run bmesh update`.
- The daemon logs it and pushes it once through `[notify]`.

That `update`, finding the binary current, walks the pending notes from the recorded release, else from the revision the swap replaced, else from v0.1.0. A box from before v0.1.0 that updated with its own old code is walked from v0.1.0, never through the start-over, since nothing recorded where it started. That is why a box from before v0.1.0 starts over, and why `serve` checks the config at start (exit 78, see [Config](#config)).

**The daemon's self-update** (`[update] repo`, or `upstream`) never walks. Before the swap it runs the new binary's config check and plans the walk, from where this box last walked to: the running binary, or, while notes are still pending, the recorded release (else the revision a swap replaced), so a second release swapped in before `bmesh update` keeps the first one's steps.

- The daemon logs and pushes a refused config (`update to v0.2.0: config check failed: ...; keeping this binary`) and a walk with an ask or `confirm` step (`update to v0.2.0 waits on bmesh update (2 asks)`). The daemon answers no ask itself. Either keeps the running binary, is written as the notice (`bmesh update --notice`), and is acted on once per target until `bmesh update` takes it.
- A walk with only plain steps is swapped in and left pending for the new binary to report.
- One with nothing to do records the new binary's release. While notes are pending, neither the record nor the revision a swap replaced moves, and an ask or `confirm` step still owed holds the next swap.

**The hub keeps up.** Within a minute of serve restarting onto a new release, `bmesh hub` re-execs onto it; off a release it keeps its starting binary until its service is restarted (see [`bmesh hub`](#bmesh-hub), Stale hub).

Internals: [internals](internals.md#upgrading)

### Runners

A runner starts a lead's thread, reports whether it is running, delivers wakes and stops it. Pick one with `[runner] kind`. [`bmesh lead start`](#bmesh-lead-start) and [`bmesh build`](#bmesh-build) start leads through it.

#### `t3`

A thread in the box's T3 Code 0.1.0 at `[runner] t3_server` (default `http://127.0.0.1:3773`; `""` is a config error naming the key), signed in as [`bmesh t3 login`](#bmesh-t3-login) describes. A T3 before 0.1.0 is refused at start, status, send and stop with `bmesh: T3 <version> at <t3_server> speaks protocol 1; bmesh needs T3 0.1.0 or later`. A thread id not of the form `mcp:<uuid>` is "no such thread".

**Runtime mode.** Every thread starts in `[runner] t3_runtime_mode`: `auto` (default; Claude Code's `auto` permission mode, where a classifier denies risky actions instead of asking anyone) or `full-access` (`bypassPermissions`). `approval-required` and `auto-accept-edits` are config errors: they stall an unattended lead.

**What you see.** The thread is in the T3 project whose `workspaceRoot` is the repo. Registering a missing project needs a full-access sign-in, so with `auto`, add the repo as a T3 project first. T3 names the thread (`mcp:<uuid>`) and the worktree (`<T3 home>/worktrees/<repo>/bmesh-<thread id>`), on branch `bmesh/<thread id>`. The daemon logs, once per change, a thread waiting on a question or approval (`t3: <path> waits on a question or approval`) and one that hit a usage limit (`t3: <path> hit a usage limit; it resets at <time>`). Interrupting the thread in T3 stops the node (see [`bmesh node stop`](#bmesh-node-stop)). A run that T3 cancelled on restart, or that crashed, is not a stop, so a dead session's lead is still woken.

**Wakes.** A thread mid-turn gets no wake that minute, logged `wake: not delivered, thread is running`, and the next poll tries again.

#### `tmux`

A `claude [--model M] [--effort E]` session in window `<thread>` of tmux session `<tmux_session>` (created when missing), in a worktree bmesh adds at `<worktrees>/<repo>/<thread>` on branch `<thread>`, with the CLI's `BMESH_CONFIG`. The runner answers a fresh worktree's first dialogs and types the brief. It can't tell whether claude is mid-turn or you stopped it. `stop` presses Escape in the window.

#### `claude`

No T3, no tmux: headless `claude -p` turns in a worktree bmesh adds at `<worktrees>/<repo>/<thread>` on branch `<thread>` (a round's thread: the checkout bmesh made), with `--dangerously-skip-permissions`, `--model` and `--effort` on every turn. Its state, including each turn's `turn-<n>.err`, is in `<user cache dir>/bmesh/claude/<thread>/`; read it to debug a lead. The session id is the thread id, so it is a UUID (`lead start` passes one). Turns inherit the CLI's environment, so `BMESH_CONFIG` reaches the lead. A wake sent mid-turn is queued and delivered as the next turn once the current one ends. `stop` drops queued wakes and kills the turn. The runner can't tell whether you stopped a turn.

#### Renamed branches

A runner may rename a lead's branch (T3 does). Each minute the daemon reads the branch of every lead, fixer and engineer not `done` whose worktree is on this host. A change rewrites the node's `branch` and logs `branch: <path> <old> -> <new>`. A detached or missing worktree changes nothing. `bmesh mesh-status`, the map and `bm-nodes` follow within the minute. The lead lands the branch its worktree is on (`git branch --show-current`), never the name in its brief.

Internals: [internals](internals.md#runners)

### Build families

A build family is the set of leads a coordinator dispatched together. Every lead that [`bmesh build`](#bmesh-build) starts is in one: `--family F` joins `F`, and without it each `build` mints `<repo>-build-<N>`. The lead's node and spec record it as `family`. A family's feedback is read once the family is done (see [`bmesh feedback`](#bmesh-feedback)).

A family holds a new build of the repo while the repo's last build run is still going. Both forms of `build` read the repo's leads on every box and hold while either is true:

- a live lead outside the build's family is running (a lead with no family is a family of its own);
- a family of the repo (a lead of the repo is in it, or it is named `<repo>-build-<N>` or `<repo>-fix-<N>`) has feedback pending.

The build's own family never holds it. The families form two pipelines that don't hold each other: the fixer's families (`<repo>-fix-<N>`, see [The fixer](#the-fixer)), and all others. Feedback naming bmesh's own repo holds no build (with no such row, every feedback holds). `[runner] max_leads` still caps the box across both pipelines, and the `land.<repo>` lease still serializes landing.

See also: [`bmesh build`](#bmesh-build), [`bmesh release`](#bmesh-release), [Holds](#holds).

### Holds

A hold stops a lead at a gate until a coordinator (the `build` skill's session, or the operator) releases it: for example, to land in order, to read a diff first, to wait out low quota, or to redirect it. [`bmesh hold`](#bmesh-hold-unhold-held) writes it, `unhold` releases it, and the lead runs `held` at its gate. `build --hold land|now` writes it at dispatch (see [`bmesh build`](#bmesh-build)).

Executors, rounds and routines are never held. A hold stays until someone releases it; it never times out. Any sender may hold, as any sender may `send`. `node forget` deletes a node's hold with the node (see [`bmesh node forget`](#bmesh-node-forget)).

**The two gates.**

- `land`: `bmesh land` checks for a hold before it takes the lease and again once it holds it, and stops (exit 3, nothing pushed) with `bmesh: held before land by <by> <age> ago: <why>; nothing is pushed: run bmesh held and end your turn, and run bmesh land again once the release wakes you`. A `now` hold stops it the same way (`held now by ...`).
- `now`: every `bmesh inbox` the lead runs while the hold stands prints it first, on its own line: `held now by <by> <age> ago: <why>; stop here: run bmesh held, end your turn, and wait for the release` (under `--json`, first, a message of kind `held` from `<by>`).

Ages are to the minute, whole days as `1d3h`.

**The held state.** A node reaches `held` from `working`, `asking`, `silent` or `landing`, and leaves it on its own next event: the release wakes the lead, and its next `started` or `progress` makes it `working`.

**A landing lead.** A lead that ran [`bmesh ready`](#bmesh-ready) isn't woken by a hold, a release or a routine reply: only a message wakes it. Its daemon reads the hold itself, runs `held` for it, and on the release writes it `landing` again and lands it, so for a landing lead the release resumes the daemon rather than waking the lead. The daemon never marks a `held` node silent (only `working` goes silent) and never pushes it (`held` can't be in `[notify] push`). The map's needs-you band has no row for it (see [Map](#map)).

**Where holds show.**

| Where | What |
|---|---|
| `mesh-status` | The state, and after the table one line per hold, `<path> held before land by operator 3m ago: <why>`; `--json`: the row's `hold`. |
| `watch` | The `held` event, `<at> <path> held <text>`; it goes on watching. |
| The hourly digest | Once per whole day of its age, `<path>: held before land by operator 1d0h ago: <why>`, in the pass whose window holds that mark. |

Internals: [internals](internals.md#holds)

### Briefs

A brief is a thread's first prompt. The engineer, UI/UX engineer, build, fix and half-pager templates are embedded in the binary (`internal/brief/templates/`). A repo overrides any but `fix` with `.bmesh/briefs/<name>.md` (`engineer`, `ux-engineer`, `build`, `half-pager`). The fix brief carries bmesh's rulings, which no repo sets. Rendered briefs are kept under `<user cache dir>/bmesh/briefs/<node>.md`.

A template is filled in one pass: `{{name}}` placeholders take values, and braces inside a value stay as written. A placeholder with no value is an error naming the template and the placeholder.

Every brief gets:

- `repo`, `repo_path`;
- `verify`: the row's commands in backticks, or that none are listed;
- `surfaces`: the row's, or what to do without them;
- `standard_closing`: the closing paragraph, verify commands inlined;
- `done_rule`: the definition of done the row implies: verify passes; a change sized M or larger as built (its spec's `size`, or the size its scope grew to; work with no spec sized as a spec would be) is reviewed by Opus in a session that did not write it, at medium effort for M and high for L or larger, while XS or S is done on verify alone; rebased, fast-forwarded to `main`, pushed to the row's `remote`.

Each kind of brief also gets its own values:

| Brief | Values |
|---|---|
| A round's | `node`, `round`, `round_n`, `worktree`, `branch`, `thread_id`, `next_id`, `date` and `operator_context` (the operator's focus section, or empty) |
| The fix brief | `node`, `date` and `plain_words` (the plain-words rule every closing ends with) |
| A build brief | `spec`, `spec_path`, `spec_id`, `node`, `date` and `upgrading` (empty, except in bmesh's own repo: a bullet asking the lead to fill the spec's `## Upgrading` section as built, with the template's rule) |

**Setup first.** When the repo names a `[setup]` (see [Setup](#setup)), a build, fix or round brief gets a section `## First: bmesh setup` before its first `## ` heading (at its end when it has none), a repo's own template included: run `bmesh setup` before anything else (`cd <worktree> && bmesh setup` in a round's brief, whose shell doesn't start there), read its output if it fails, and run it again after changing dependencies. A build or fix brief reads `[setup]` from `main`, which the lead's worktree is made from; a round's, from its worktree. The executor header has the same first step (see [`bmesh executor new`](#bmesh-executor-new)). The runner makes a T3 lead's worktree after the brief is written, so bmesh can't run the setup there itself; a T3 project setup that already runs `bmesh setup` makes the step a no-op.

The proposal briefs carry the repo's facts and point at the `ideate-engineer` and `ideate-ux` skills for the method. The build brief carries the spec, points at the `lead` skill, and says: the review is sized as `done_rule` says and queued with `--effort`; its findings are fixed and then landed, with a second pass only after a rebase changed the code; a taste finding is noted in the final message, not re-reviewed.

See also: [`bmesh build`](#bmesh-build), [`bmesh spec template`](#bmesh-spec-template).

### The fixer

The fixer is a fix lead whose builds neither hold nor are held by the repo's other builds (its own pipeline, see [Build families](#build-families)). On bmesh's own repo only (the registry row whose `go.mod` declares the module the binary was built from; the row `update` and `land` find), it rules on the feedback naming that repo under the rulings below. The fixes it rules are built without the operator reviewing them first. With no such row, nothing here is active. With no such row, or a binary that doesn't name its module, the daemon starts nothing. [`bmesh fix`](#bmesh-fix) starts one by hand.

**When the daemon starts one.** The daemon of the row's `host` (any daemon when the row names none) starts a fixer while the row is `ship` and all three hold:

1. None is live.
2. The newest isn't `blocked` (a block, a stop included, waits on the operator and is never restarted).
3. An open issue (pending feedback naming the repo that no spec on its `main` names in `from_feedback`; what `list --repo` prints) was published at or after the newest fixer's start, or there has been no fixer.

An issue a fixer had when it started and left is not started on again. The daemon starts one within a minute of the issue, once across every box. Its output and any failure go to the daemon's log.

**Who reaches the operator.** Only the fixer does. A lead in a `<repo>-fix-<N>` family (a fix lead) pushes nothing: its `ask`, `blocked` and `done` go to the fixer's inbox in the shapes an executor's take to its lead (see [`bmesh executor`](#bmesh-executor)). The fixer answers or relays them under the rulings, and reports them. The fixer's own `ask`, `blocked` and `done` push as a lead's do (see [Pushes](#pushes)). `mesh-status` shows the open issues on its `fixes:` line, with the fixer's state (see [`bmesh mesh-status`](#bmesh-mesh-status)).

**The rulings.** The fixer's brief, the embedded `fix` template, carries these rulings. Each spec it writes cites by slug the ones it rests on.

1. `root`: fix the first wrong decision, not the place it showed, then every other place with the same fault. A guard is a fix only when the state it handles is legitimate.
2. `failing-first`: name what fails before the fix and passes after: a test through the real command or package, or a run with its output. A rate or timing the fix rests on carries its run count. An issue that can't be made to fail is dropped, saying what was tried.
3. `smallest`: the smallest change that removes the cause, deleting before adding. A sentence added to a skill or brief every session loads names the sentence it replaces, or why none can go.
4. `structure`: a lesson met twice becomes a check. When a skill sentence already warns about the issue, the fix is a refusal, warning or test in the binary, and the sentence goes.
5. `premise`: no third fix on one premise. When two fixes of the same thing haven't held, write down the belief both assumed, test it, and propose it to the operator with the evidence.
6. `no-new-surface`: a fix adds no command, flag, config or registry key, frontmatter field, bucket, subject or event. Anything new the operator would have to learn is product direction, and is proposed.
7. `reversible`: build what can be undone inside the repo without asking, then report it. Proposed, never built: taste, product direction, anything sized L, and anything outside the repo's `main` (a step on another box, deleted records, the registry or a box's config).
8. `drop`: dropping is an outcome: what a spec on `main` already covers (any status), what a `--fixed` commit settled, and the reporting lead's own spec's work.

Code-design principles (types, boundaries, domain modelling, reader load) are left to the repo's `AGENTS.md` and its review. A migration is sized L and goes to the operator. What is proposed is designed by the ideate method. `build` checks the two rulings it can (see [`bmesh build`](#bmesh-build)), and `list --repo` hides what a spec already names.

**Its pass.** The fixer follows the `fix` skill, one pass:

1. `feedback list --repo <repo>`.
2. One ruling per issue. A fix is a spec with `status: accepted`, `ruled: [<slugs>]`, `fix_of: <the reporting lead's spec>` and `from_feedback: <family> (<lead>) #<seq>`. What `premise`, `no-new-surface` or `reversible` send on is a spec with `status: proposed` and the ruling named in its Opportunity, with no `ruled` or `fix_of`. A drop is a line in the report.
3. Each spec lands on `main` and its issues are drained at once (`drain --seq`).
4. Each fix is built one at a time (`bmesh build <repo> NNN --family <the fixer's>`) and watched. The fixer answers an `ask` the rulings settle, relays the rest, and lists again after each (a fix lead's own feedback is ruled on in the same pass).
5. Once the queue is empty and no fix lead runs, it releases the repo when the repo has a release tag, then runs `done`: one line per issue (`<seq>: fixed by NNN (root, failing-first)`, `<seq>: proposed NNN (no-new-surface: ...)`, `<seq>: dropped (drop: 044 covers it)`), what waits on the operator first, then the release.

Internals: [internals](internals.md#the-fixer)

### The ledger

A repo's specs live in `docs/proposals/NNN-slug.md` (`[spec] glob`). Each spec is a flat `key: value` frontmatter between `---` fences (lines starting with `#` are ignored) over a half-page body. `README.md` is not a spec. [`bmesh spec`](#bmesh-spec) reads and writes the ledger, and [`bmesh choose-ideas`](#bmesh-choose-ideas) lands your verdicts in it.

**Frontmatter rules.** The frontmatter is the whole record. So each of these is an error (exit 1) for every command, never skipped or counted around:

- a missing or non-numeric `id`, or one that differs from the file name's;
- a `status` outside `proposed`, `accepted`, `adjusting`, `later`, `rejected`, `building`, `blocked`, `done`;
- a `judge` outside `fable`, `opus`, `sonnet`;
- missing fences, or a line that is not `key: value`;
- two files sharing an id.

**Ids are claimed.** A writer claims a spec's id with [`bmesh spec next-id`](#bmesh-spec-next-id) before it lands the spec, and [`bmesh ideate`](#bmesh-ideate) claims a block per child: the counter on the mesh (`bm-specids`, one key per repo) never hands out a number twice. A spec written by plain git without a claim can still take a number another writer holds, so the commands that land the ledger (`choose-ideas`, `spec set`, `build`'s dispatch commit and `land`) refuse, before any push, a ledger in which two files share an id: `refused: <file> and <file> share id NNN; renumber the newer with bmesh spec next-id`. Renumbering is yours.

With no id, `bmesh spec steps` passes over a repo whose ledger can't be read.

**Fields the mesh writes.**

- `branch`: set by dispatch, corrected by the build thread when the runner renamed it. The `spec set` that writes `done` and `merged` adds `--field branch=<the branch landed>` when it differs.
- `family`: the build family the spec was dispatched in, set with `building`.
- `from_feedback: <family> (<the leads whose feedback it came from, comma-separated>) #<seq>`: on a spec a coordinator wrote from feedback. Several issues are comma-separated, each with its stream sequence (see [`bmesh feedback`](#bmesh-feedback)).
- `ruled: [<the rulings' slugs>]` and `fix_of: <the reporting lead's spec>`: on a fix the fixer accepted. `build` checks them (see [`bmesh build`](#bmesh-build) and [The fixer](#the-fixer)).
- `operator_steps`: the steps for the operator, below.

**Steps for the operator.** When [`bmesh land`](#bmesh-land) lands a spec (step 5 of the steps it runs holding the lease), it reads the spec's `## Needs from the operator` section as a list and copies it into the frontmatter as `operator_steps`.

- Each top-level `-`, `*`, `+` or numbered (`1.`, `1)`) line is a step, with the lines under it (indented, or running on with no blank line between; a nested list too).
- A step may start with a box in brackets, `[<host>]` (lowercase letters, digits and `-`). With `[hosts]` on and a roster read, a word naming neither a roster box nor this one is text (`[optional] ...`). A leading checkbox `[ ]` or `[x]` is dropped.
- A step may end with `check: <command>` (backticks dropped). The step is done when the command exits 0.
- A paragraph of prose is a step of its own, except one ending in `:` straight before a list (`On the Mac:`). Its words go in front of each of that list's steps, and its box, if any, applies to the steps that name none.
- A section that is absent, empty, or whose first word is `nothing` or `none` has no steps and gets no `operator_steps`. A spec landed again with steps from before gets `operator_steps: []`.

The value is JSON on one line, `[{"n":1,"box":"<host>","text":"...","check":"...","done":"","note":"..."}]` (`check` and `note` only when set). `done` is empty until the step is ticked, then `<RFC 3339 time> <box>`. A spec landed again keeps the ticks of the steps whose box and words are unchanged, each old tick at most once. [`bmesh spec steps`](#bmesh-spec-steps) reads, ticks and checks the steps.

### Routines

A routine runs one of a repo's checks against a commit: a shell command or a headless `claude -p` session. A kind says how; a job asks for one run; a result records the run by tree. The commands are [`bmesh routine`](#bmesh-routine), [`bmesh result`](#bmesh-result) and [`bmesh lock-lease`](#bmesh-lock-lease).

#### Kinds

A kind is `.bmesh/routines/<kind>.toml` (the directory is `[routines] dir` in the repo's `.bmesh/config.toml`). Its name, the file name, matches `^[a-z0-9][a-z0-9-]*$`.

| Key | Default | Meaning |
|---|---|---|
| `mode` | `"pooled"` | `inline`, `fresh` or `pooled` (see [Modes](#modes)) |
| `os` | `""` | the OS the run needs (Go's `GOOS`, as `bmesh hosts` prints it): `darwin` (macOS), `linux` or `windows`; `""` is any box (see [Jobs](#jobs)) |
| `deterministic` | `false` | same tree, same result: a recorded result answers a repeat job |
| `slots` | `[]` | slots (from the repo's `[slots]`) a run leases |
| `timeout` | `"20m"` | the run's process group is killed after this |
| `command` | `""` | run by the platform shell in the checkout at the commit, the job's args appended (`"$@"` under sh) |
| `model` | `""` | a model id: the kind runs `claude -p` with it |
| `models` | `[]` | several model ids; a job's `--model` picks one, default the first |
| `prompt` | `""` | model kinds: a setup prompt, sent once per session before the first job |
| `allow` | `[]` | model kinds: extra `--allowedTools` entries, e.g. `"Bash(npm test:*)"`; `Read`, `Grep` and `Glob` are always allowed. They are also all the session holds: its tools are the built-in ones these name, no skills unless `"Skill"` is named, no MCP servers unless an `"mcp__<server>__<tool>"` is |
| `pool` | `1` | pooled: instances per model; fresh: runs at once |
| `batch_idle` | `"30s"` | pooled: held jobs start after this long with no new job and no go |
| `recycle_uses` | `20` | pooled model: a new session after this many jobs |
| `recycle_context` | `120000` | pooled model: a new session when the last reply's context tokens (input + cache read) exceed this |
| `recycle_idle` | `"55m"` | pooled model: a new session after this long idle (the prompt-cache TTL) |

- Exactly one of `command`, `model` and `models` is set. An unknown key is an error.
- An `os` outside the three is refused, `kind <k>: os "<v>": want darwin, linux or windows (macOS is darwin)`. So is `os` on an inline kind, which runs in the requester's worktree. `os` states what the work needs; no key names a box.
- bmesh trims a command's trailing spaces, newlines and `;`. Its last line may not end in a comment, which would comment out the appended `"$@"`.
- A model id has no spaces or `;` (it must fit a `verified` entry).
- A recycle key of `0` is off.

**Environment.** A command gets `BMESH_JOB`, `BMESH_KIND`, `BMESH_COMMIT`, `BMESH_TREE`, `BMESH_FROM` (the path the job runs as: the instance, or the requester inline) and `BMESH_REPLY` (the requester's path).

**Model kinds** run `claude -p --model M --output-format json --session-id|--resume <uuid> [--effort E] --tools <names> [--strict-mcp-config] [--disable-slash-commands] --allowedTools <list> --max-turns 40` with the job on stdin: a header line `job: <id> commit: <sha> tree: <tree>`, the job's `--text` and its args.

- `--effort` is passed only when the job set one. The jobs of a pooled session each run at their own effort.
- `--allowedTools` is `Read,Grep,Glob` and the kind's `allow`: what the session may run. `--tools` is what it holds: the same entries, each cut at `(` (`Bash(git diff:*)` gives `Bash`), once each, without the `mcp__` ones. `--strict-mcp-config`, with no `--mcp-config`, loads no MCP servers and is left off when `allow` names an `mcp__` tool; `--disable-slash-commands` loads no skills and is left off when `allow` names `Skill`. Hooks and CLAUDE.md load as the box's settings make them.
- A run whose claude never answered judges nothing and records nothing: claude was killed or timed out, didn't start, exited non-zero, printed no result, or returned an `is_error` result (a failed sign-in, say). The daemon answers the job `ok=false`, and `routine run` refuses (exit 3), with `model run failed: <why>; nothing recorded`, `<why>` claude's error, `[timeout after <d>]`, `[bad claude output]`, `[exit status <n>]` when it exited saying nothing, or else the first line it printed, so a re-queued job records the real verdict.
- A run claude answered is ok unless the reply's last `VERDICT:` line reads `VERDICT: fail` (the line is `VERDICT: ok` or `VERDICT: fail`). Leading `*`, `_`, `#`, `` ` ``, `>` and spaces are ignored, around the value too: `**VERDICT: fail**`, `**VERDICT:** fail` and `VERDICT: **ok**` all parse.
- When the prompt contains `VERDICT:`, a reply without a verdict line fails (and is recorded). Otherwise it is ok.

A run's output is stdout and stderr (a command) or the reply text (a model).

**Examples**, in [`examples/routines/`](../examples/routines/):

- `verify.toml`: `mode = "inline"`, `deterministic = true`, `command = "npm test"`.
- `review.toml`: `mode = "pooled"`, `models = ["claude-opus-5-5"]` (a session that never saw the change), `allow = ["Bash(git diff:*)", "Bash(git log:*)"]`, and a prompt that reviews `git diff <base>..HEAD`. The job text carries `base: <sha>`, e.g. `--text "base: $(git merge-base origin/main HEAD)"`. The prompt asks for findings as `### <severity>: <what breaks>` sections ending in `VERDICT: ok` or `VERDICT: fail`, and says verify already ran tests, vet and formatting at that commit, so the reviewer reads instead of running them.
- `surface.toml`: `mode = "fresh"`, `deterministic = true`, `slots = ["browser"]`, `command = "npx playwright screenshot"`, the job args `<url> <file>` appended as `"$@"`. A commented `os = "darwin"` line shows how a check that needs a Mac says so.

#### Modes

| Mode | Runs | Where |
|---|---|---|
| `inline` | `bmesh routine run` in the requester's process; no daemon | the requester's worktree, at HEAD |
| `fresh` | the daemon starts an instance per job, `routine.<kind>.f<id8>`, and removes its checkout and node after | a checkout of its own |
| `pooled` | the daemon keeps `pool` instances per model, `routine.<kind>.<host>-<model-short>-<n>` (`<host>-cmd-<n>` for a command), draining the kind's inbox in batches | one checkout per instance, kept |

`id8` is the job id's first 8 characters. `host` and `model-short` are the box's host name and the model id as path tokens (`routine.review.k16-opus-5-5-1`), so each box's instances are nodes of their own. `n` counts from 1 per model.

**A pooled instance** holds the jobs it takes until `batch_idle` passes with no new job, or a go arrives (`bmesh routine go <kind>`, or a job queued with `--go`). Then it runs them in order, and runs any job queued meanwhile at once.

**A model instance** keeps one session across jobs. It starts a new one when `recycle_uses`, `recycle_context` or `recycle_idle` is reached, or when the kind's definition at a job's commit differs from the session's. A run whose output isn't claude's JSON result drops the session whatever the recycle settings; the next job starts one with the prompt.

**Instances are nodes** (`role: routine`, with repo, host and model): `working` during a job, `idle` between, `done` when stopped (a fresh run after its job; a pooled one when its pool stops or restarts; see [Finished nodes](#bmesh-node-forget) for when the node leaves). The daemon publishes on `bm.<instance path>.<event>`:

- `started`;
- `progress`: `job <id> started` before a run and `job <id> ok|fail <ms>` after it (`job <id> ok|fail <ms> ctx <tokens>` for a model instance), both with `to` = the requester's path; or `recycled`;
- `done`.

An event with a `to` counts as that path's activity too, so a lead waiting on a routine isn't marked silent. The map draws one box per kind in its routines column (see [Map](#map)).

**Reading kinds.** Every minute the daemon reads each registered repo's kinds (see [repos.toml](#repostoml)) from its main (`origin/main` after a fetch when the repo has an `origin`, else `main`). It starts, restarts or stops pools to match.

- A repo whose path doesn't exist here (another box's row) is skipped, logged once per daemon start: `routines: repo has no checkout on this box; skipped repo=<name> path=<path>`.
- A kind whose `os` on main isn't this box's gets no feed, instances or consumer settings here, so its jobs are never fetched here.
- A repo whose kinds can't be read (no `main`, say) keeps what it had, logged once per repo and main commit: `routines: read kinds repo=<name> err=<error>`.
- A box defers the jobs of a kind it can't judge for a repo (see [Jobs](#jobs)).
- Routines are off when `[runner] checkouts` is empty.

Internals: [internals](internals.md#modes)

#### Jobs

A job names a commit and a repo (see [`bmesh routine queue`](#bmesh-routine-queue)).

**Taking jobs.** Every box running the kind shares its queue. Each box fetches as many jobs as it has instances for the kind (at least 1, so a box whose repos can only refuse still answers). A stopping daemon (SIGTERM or SIGINT, and before an update's re-exec) kills every running job's process group and hands back every job it holds or ran, unanswered, so the next daemon takes it at once (a model job killed mid-run is paid for twice). Only a daemon that dies leaves jobs unacked until `AckWait` passes (see [`bmesh routine status`](#bmesh-routine-status)).

Internals: [internals](internals.md#queueing)

**Running.** The daemon runs a job with the kind as defined at its commit (`git show <commit>:.bmesh/routines/<kind>.toml`), falling back to main's only when the file is absent there.

- A deterministic kind's job is answered from its recorded result, ok or failed, without a run, unless `--rerun`.
- A command kind's job runs the checkout's `[setup]`, as the job's commit defines it, after the move and before its slots are leased, when its installs aren't current; with no `inputs`, on every job, since the move changed the commit under them (see [Setup](#setup)). A model kind's job runs none.
- A job whose slot is busy goes back to the inbox for 30 s.

The daemon answers `ok=false` with the reason (`routine wait` prints it and exits 1, as for any failed job) for:

- a kind that a configured repo defines but the job's repo's main doesn't, `kind <k> is not on <repo>'s main (origin/main at <sha8>)` (the commit that box last read main at; `main at` without an origin), so a kind only a branch has is answered at once by any box with the repo;
- an inline kind;
- an unknown model;
- a commit whose kind runs on another OS (see Another OS below);
- a setup that failed, timed out or dirtied the tree, `setup failed: <why>; nothing recorded` (or `setup dirtied the tree: <paths>; nothing recorded`) and the last 30 lines of its output;
- a model run whose claude never answered, `model run failed: <why>; nothing recorded` (see Model kinds in [Kinds](#kinds)); its `job <id> fail <ms>` progress event is sent as for any failed job;
- a lost lease.

**Deferred.** A daemon leaves a job it can't judge for a box that can, with no reply. It defers a job for a repo

- its registry doesn't name (it reads `repos.toml` only when `serve` starts, so restart `serve` after changing the registry);
- with no checkout (or no usable path) on that box;
- whose main failed to read and left no earlier definition of the kind;
- whose main gives the kind another OS.

It logs `routines: deferred job <id>: <repo> <why> here` once per job.

Internals: [internals](internals.md#running)

**Another OS.** A kind with an `os` runs only on a box of that OS; its jobs wait on the hub while that box sleeps. Boxes take a kind's jobs by main's definition, but a job runs on the `os` its commit gives the kind (else main's), whatever the working copy says. So a branch could give an `os` to a kind that boxes of both OSes run; `bmesh routine queue` checks for this before it queues. A daemon whose job's commit gives the kind another OS (queued by an old CLI, or main moved between queue and fetch) answers it `ok=false` right after loading that definition, before the record lookup and slot lease, `kind <k> at <commit8> runs on darwin; this box is linux`, and acks it. Nothing runs or is recorded there. A kind name is one queue across every repo and box. Since a box defers a repo whose main gives the kind another OS, kinds of different OSes may share a name.

**Replies.** A reply reaches the requester's inbox with the job's output, cut at 64 KiB with a trailing `[truncated]` line. [`bmesh routine wait`](#bmesh-routine-wait) prints it whole; [`bmesh inbox`](#bmesh-inbox) prints it as one line.

Internals: [internals](internals.md#replies)

#### Checkouts

Fresh and pooled runs never touch a requester's worktree. Each instance has a detached worktree of the registered repo at `<[runner] checkouts>/<repo directory name>/<instance path>`, moved before each job with `git checkout --detach <commit> && git clean -fd`, which keeps ignored files: a pooled instance's installs (`node_modules`) stay from job to job, and [Setup](#setup) says when they are brought current. The daemon reads the repo's main in a sibling checkout named `main`. An inline run uses the cwd worktree's HEAD (see [`bmesh routine run`](#bmesh-routine-run)).

#### Setup

A fresh `git worktree add` has none of what a repo gitignores, its installed dependencies among them, so a verify there fails for want of them (`vitest: not found`). A repo names the one command that installs them in its `.bmesh/config.toml`, which [`bmesh scaffold`](#bmesh-scaffold) writes from the registry row's `setup`, `setup_inputs` and `setup_timeout` (see [repos.toml](#repostoml)):

```toml
[setup]
command = "npm ci"                          # run by the platform shell at the worktree's root
inputs = [":(glob)**/package-lock.json"]    # git pathspecs; their tracked files decide staleness
timeout = "10m"                             # the default; the run's process group is killed after it
```

bmesh reads `[setup]` from the commit being worked on (the worktree's file, or the commit's for a job), never from the row. A repo with no `[setup]` runs nothing.

**Current.** A run that succeeds writes `bmesh-setup` into the worktree's own git dir (`git rev-parse --git-dir`, outside the tree): a sha256 of the command and of every tracked file the inputs match, path and content, read from the working tree after the run. The installs are current while that hash matches. Without `inputs`, the hash is the command's alone, so a worktree is current once setup has run in it. A failed run leaves no marker.

**Where it runs.**

- **Verify of `main`** ([`bmesh build`](#bmesh-build), [`bmesh release`](#bmesh-release)): in the fresh worktree, right before the verify kind runs; never when a record answers or under `--force`.
- **Inline kinds** ([`bmesh routine run`](#bmesh-routine-run), [`bmesh land`](#bmesh-land)): before a command kind runs, when not current. The tree must be clean after it (`git status --porcelain`): a setup that leaves tracked changes or untracked, unignored files fails with `setup dirtied the tree: <paths>`.
- **Routine jobs** ([Jobs](#jobs)): a command kind's, after the checkout moves to the job's commit and before its slots are leased; with no `inputs`, on every job. The tree must be clean after it, as inline.
- **Agent worktrees** (leads, executors, proposal threads, on every runner): the brief's first step is [`bmesh setup`](#bmesh-setup), and the agent reads its own failure (see [Briefs](#briefs)).
- **Never** in a model kind's run (a review reads diffs under its `allow` list), a ledger edit's worktree (`choose-ideas`, `spec set`, a dispatch's record), the daemon's `main` read checkout or an update's checkouts.

**Failure.** A setup that fails, times out or dirties the tree records nothing: the build of `main` is refused with the tail of its output (so a registry flake costs a retry, never a red `main`), an inline run is refused (exit 3), and a job is answered `ok=false` with the reason. Concurrent setups in two worktrees share the package manager's cache (npm's, pnpm's, uv's and Go's are built for that); bmesh keeps no store of its own.

#### Leases

A slot is a resource only so many runs may use at once. Its capacity is in the repo's `.bmesh/config.toml` (default 1):

```toml
[slots]
browser = 1
[routines]
dir = ".bmesh/routines"
[setup]                       # see Setup
command = "npm ci"
inputs = [":(glob)**/package-lock.json"]
timeout = "10m"
```

- An expired lease is free.
- A holder renews every ttl/3. A failed renew loses the lease: the run is killed and the job answered `ok=false` with `lease lost`, nothing recorded.
- Kinds lease their `slots` for `timeout` + 1m (inline runs and the daemon alike).

The command is [`bmesh lock-lease`](#bmesh-lock-lease).

#### Results

A result is kept per kind, tree, args and model, with its verdict, output (cut at 64 KiB), time and who ran it. A model's result also records the patch-id of the diff it was asked to read, from the commit the job text's `base: <sha>` names to the job's commit. This lets a review carry across a rebase (see [The verified line](#the-verified-line)). A base that isn't a commit records none (the daemon logs it).

Every run naming a commit records, inline or daemon, deterministic or not. A kind's newest run at a tree replaces the last. Answered-from-record jobs, refused jobs, failed setups, lost leases and runs cut off by a stopping daemon record nothing.

A result older than `[routines] keep_results` (default `30d`; `0` keeps them forever) is gone. A deterministic kind then runs again, and `bmesh done` for a spec landed longer ago needs its routines rerun on the merged tree (it names the missing result). The spec's `verified` line stays.

The commands are [`bmesh result find`](#bmesh-result-find) and [`bmesh result record`](#bmesh-result-record).

Internals: [internals](internals.md#results)

#### The verified line

A spec's frontmatter carries one line of proof, `;`-separated, one entry per check:

```
verified: review ok t:3f2a9c1e m:claude-opus-5-5; verify ok t:3f2a9c1e
```

Each entry is `<kind> <ok|fail> t:<tree first 8>[ m:<model>][ c:<tree first 8>]`. `c:` names the tree a model's result was recorded at when it carries to `t:` across a rebase that left the diff unchanged:

```
verified: review ok t:3f2a9c1e m:claude-opus-5-5 c:81d0b7a4; verify ok t:3f2a9c1e
```

[`bmesh spec verified`](#bmesh-spec-verified) composes the line and, with `--write`, sets it; [`bmesh land`](#bmesh-land) writes it as it lands.

`bmesh done` checks, in order, and refuses with exit 3 at the first that fails:

1. The spec reads `done` on main with a `merged` commit main contains (`git merge-base --is-ancestor <merged> <ref>`, `ref` = `origin/main` when the repo has an origin, else `main`). Else `bmesh: not done: merged <sha> is not on <ref>`.
2. For a lead only (other roles stop here): the `verified` line is non-empty (`bmesh: not done: verified is empty`) and has no `fail` entry (`bmesh: not done: verified has a failing entry <kind>`).
3. For a lead, each entry is for the tree of `merged` (`bmesh: not done: verified entry <kind> is for tree <t>, merged tree is <m>`) and is matched by a record of that kind at that tree with the same `ok` (and model, when the entry names one), whatever its args. A carried entry is matched by a model record at the tree its `c:` names, with the same `ok` and model, whose patch-id is the diff `merged` landed. Else `bmesh: not done: verified names <kind> but no result for tree <t>` (`<t>` is the `c:` tree for a carried entry).

An executor's `done` skips the spec check.

Internals: [internals](internals.md#verified)

### Hosts

Boxes that share the hub act on each other through it, with no SSH between them. The admin's signed roster lists which boxes belong. Every message between boxes is signed. A box runs only the ops its roster row accepts: `ping`, `level`, `propose`, `prune`, `forget-worktree`, `stop-thread`, `check-step` and `nix`, each below.

Nothing here runs unless `[hosts] enabled = true`, and only `bmesh init --hosts` sets it. Without it, the daemon never pushes or pulls anything. The commands are [`bmesh hosts`](#bmesh-hosts) and its subcommands, [`bmesh sync`](#bmesh-sync), [`bmesh prune`](#bmesh-prune) and [`bmesh repos`](#bmesh-repos).

#### Joining

A box joins with [`bmesh init --hosts [--admin]`](#bmesh-init). It prompts for:

1. join with an invite, or be the admin (`--admin` answers this);
2. whether this box is **secure** (see [Several boxes](#several-boxes));
3. whether its daemon runs routines (default: no on a secure box).

Run again, it defaults to the current answers, from `config.toml`, `host.key` and `<state dir>/roster.json`. Restart the daemon afterwards.

- **The admin** (`--admin`) makes the admin key (see [Keys](#keys)) if it is missing. It publishes roster v1 with its own row: its approve key is the admin key, and it accepts `ping`. Run again, it republishes only when its own row changed.
- **The invite.** On the admin, [`bmesh hosts invite`](#bmesh-hosts-invite) prints a one-time code and waits. Without `--accept`, the box's row accepts only `ping` (`--accept ping`). A name already in the roster needs `--replace`. The admin's own name is always refused, since its row carries the admin key.
- **The join.** On the new box, `bmesh init --hosts` prompts for the code. The code is never taken from argv, so it isn't in `ps` or the history. The box sends the admin a join request authenticated with a key derived from the code (the handshake is in [internals](internals.md#hosts)).
  1. The admin burns the invite on the first request that proves the code. A failing request is dropped without burning, and the third failure burns it.
  2. The name must be the invite's.
  3. The admin publishes the next roster with the box's row. The row gets the box's approve key only for a `--secure` invite.
  4. The admin replies with the roster and its key.
  5. The new box checks that the roster carries its key, pins the admin key and stores the roster.

A fresh invite re-pins a box pinned to another admin. A joined box may leave the code empty to keep its roster.

#### Keys

Every joined box has an Ed25519 host key in `<config dir>/host.key` (the seed in hex, mode 0600). Its agents can read it, so it proves only "this box".

The admin box, and every secure box, also has one key agents can't use:

| Key | Box | Signs | Public half |
|---|---|---|---|
| the admin key | the admin box | rosters, registries and `nix` jobs | `<config dir>/admin.pub` |
| the approve key | every secure box | approvals | `<config dir>/approve.pub` |

Only signing reads the key itself, from the store `[hosts] key_store` names:

- **`keychain`** (the default on macOS): the login Keychain, service `bmesh`, account `admin` or `approve`.
  - bmesh writes it through `security -i` with the key on stdin (never in `ps`) and an empty trusted-app list (`-T ""`), so every read raises the macOS login-password dialog.
  - bmesh prints `Keychain dialog follows for: <action>` first. Deny any unannounced dialog. Choose "Allow", never "Always Allow".
  - Before a read, bmesh checks the item's access list (`security dump-keychain -a`). It refuses when an app was added (what one "Always Allow" does) or every app is trusted: `the admin key's Keychain item trusts <app>; remove it in Keychain Access`.
  - Over SSH there is no dialog and the read fails. Run admin and approve commands at the Mac's own screen.
- **`file`** (the default elsewhere): `<config dir>/admin.key` (or `approve.key`), mode 0600, the seed sealed with XChaCha20-Poly1305 under `scrypt(passphrase, salt, N=32768, r=8, p=1)`. bmesh reads the passphrase from the terminal without echo (twice when it makes the key).

The admin commands also refuse (exit 3) when stdin isn't a terminal. The dialog and the passphrase are what keep agents from using these keys.

#### The roster

The roster is the admin's signed list of boxes. For each box it holds the name, host key, `secure` flag, approve key and the ops the box accepts. A fingerprint is the first 16 hex characters of a key's SHA-256.

- A box keeps its pinned admin key and newest valid roster in `<state dir>/roster.json`.
- A box accepts a roster only when the pinned key signed it and its `version` is higher.
- The admin alone publishes the next version: [`bmesh hosts invite`](#bmesh-hosts-invite), [`set`](#bmesh-hosts-set) and [`revoke`](#bmesh-hosts-revoke).
- Every daemon re-reads the hub's roster each minute and before each job. It puts its own back when the hub's copy is missing, unsigned or lower. So a roster a token holder deleted or rolled back is back within a minute.

#### Presence

Each daemon publishes its presence each minute, signed with its host key. Presence holds:

- its OS and revision;
- whether it runs routines;
- the registered repos it has checked out, each with its path and `origin` (the checkout's `origin` URL, else the row's `remote`);
- its notes.

The notes are `<config dir>/notes.md`, the box's session guidance (see [Several boxes](#several-boxes)). The daemon reads the file each minute, drops invalid UTF-8, cuts it to 16 KB on a character boundary (logged once) and trims it. Without the file the notes are `""`. bmesh never takes a path or a decision from presence.

[`bmesh hosts`](#bmesh-hosts) shows presence; [`bmesh hosts --notes`](#bmesh-hosts---notes) shows the notes.

#### The session line

Each minute, after presence, the daemon writes `<state dir>/this-box.txt`, through a temporary file and a rename:

`this box is <host>[ (admin)]; secure: yes|no; on the mesh: <host> (<state>[ <age>][, secure][, admin]), …`

It lists every other roster box with its presence state. An asleep box shows how long ago it was seen, `asleep 2h`: in minutes, hours, or days past two days. With no other box it says `no other box`. When any verified box has notes, it ends with ``; notes: `bmesh hosts --notes` ``. [`bmesh hosts --here`](#bmesh-hosts---here) prints the file.

#### Jobs and replies

A job for a box waits in its inbox on the hub until the box takes it. So a box that was asleep gets what it missed, in order. A job lives 24h unless said otherwise.

`sync`, `prune` and the `repos` commands print each answer as it arrives, `<box>: <answer>`. For a box silent past its `--wait`, they print an `asleep, queued until …` line (`no answer in <wait>` in place of `asleep` when the box's presence is awake). The box takes the job when it wakes. `ping` and `node` print their own forms. Host jobs and replies never show in the map, `watch`, status or the activity reader.

Every job, reply, approval, roster, registry and presence entry is signed with its author's key. A receiving box checks a job in this order and stops at the first failure. A refusal is answered `refused: <check>`:

1. The sender is in the roster (an op that needs the admin is checked against the admin key); else `refused: roster`.
2. The signature verifies; else `refused: signature`.
3. The job is addressed to this box; else `refused: to`.
4. The sender's roster version isn't ahead of this box's. Else the job waits a minute and is checked again, until the box has caught up (or while it can't read the hub).
5. The job's time is at most 5 minutes ahead of this box's clock; else the job waits a minute.
6. The job hasn't expired and lives at most 24h; else `refused: expired`. An expired job is acked, answered and logged only.
7. The op is in this box's `accept`; else `refused: accept`.
8. The job hasn't been seen before; else `refused: replay`.

A refused job never runs. A job delivered again after it ran gets its stored reply again, never a re-run. A job delivered again after a crash or sleep mid-run is resumed when its op is idempotent (`ping`, `level`, `prune`, `stop-thread`, `check-step`), else answered `interrupted`.

Any daemon with `[notify]` and `[hosts]` pushes roster, signature, `to` and replay failures once (title `<host> refused <sender>: <check>`, priority `high`), at most once per sender and check every 10 minutes (see [Pushes](#pushes)).

The caller checks a reply likewise: it must be signed by the box the job went to and name the job. [`bmesh hosts replies`](#bmesh-hosts-replies) lists the replies.

The `ping` op answers `<rev8>, roster v<N>`. [`bmesh hosts ping`](#bmesh-hosts-ping) sends it; with its default wait spelled out, that is `bmesh hosts ping [<host>] [--wait 10s]`.

#### Waking

A daemon detects a wake when two of its minute ticks are 3 minutes or more apart on the wall clock (monotonic time stops while a box sleeps). It logs the wake, re-reads the roster and self-levels (see [Levelling](#levelling)).

#### Levelling

Levelling keeps every box's checkout of a repo level with origin. On a secure box, code the operator hasn't seen waits for approval.

A level, the daemon's routine fetch, and the fast-forward of `main` in the main checkout by `bmesh land`, `build` or `choose-ideas` take one lock per repo (the repo's lock), so they never collide. With `[hosts]` off nothing takes it.

**The `level` op.** `level` is a built-in op: `repo` the registry name, `args` `[<sha>, <branch>, <until>]`. These are origin's head after the sender levelled, the sender's branch (`""` for a pinned row), and when the sender stops waiting. The receiving box judges it against the checkout of its own registry row, which must exist (else `refused: no checkout of <repo> here`):

1. `git fetch origin`, plain (for a row with a `pin`, also that tag, never forced).
2. The target is `origin/<branch>` (the job's branch, else the checkout's current one), or the pin tag's commit for a pinned row. A missing one is `refused: no origin/<branch>` or `refused: no tag <pin>`; a checkout with no origin, `refused: no origin`.
3. The job's sha must be in the target's history, else `refused: <sha8> not on origin/<branch>` (or `not on <pin>`).
4. The checkout check moves nothing and answers:
   - `refused: in progress`: a `MERGE_HEAD`, `CHERRY_PICK_HEAD`, `REVERT_HEAD`, a `rebase-merge` or `rebase-apply` directory, or an `index.lock`;
   - `refused: dirty`: uncommitted or untracked changes;
   - `refused: other branch`: on another branch than the target's, or detached, for a row that isn't pinned;
   - `refused: diverged`: both ahead of and behind the target.
5. Then, by where the checkout stands:
   - A pinned row moves to the tag, detached (`moved to <pin>`), unless it carries commits no remote branch or tag has (`ahead <N>, not pushed: pinned to <pin>`). A pinned row is never pushed.
   - Behind only: `merge --ff-only`, answered `fast-forwarded <N>`.
   - Level with the target: `up to date`.
   - Ahead only: `git push origin <branch> <tags…>` (the tags that point at the pushed commits, lightweight ones included), answered `pushed <N>`. It pushes only when the caller may push (a received job or `bmesh sync`, never a self-level), the row's mode is in `[hosts] push_modes` (default `["ship", "fork"]`) and the newest local commit is at least 10 minutes old. Else it answers `ahead <N>, not pushed: <why>` (`a self-level never pushes`, `mode <m> is not in push_modes`, `the newest commit is <age> old (10m0s before a push)`). If another box pushed first, the push fails as not a fast-forward: `refused: diverged`.
6. **Plugin refresh.** After the checkout moves, the box refreshes what it loads from it:
   - with `.claude-plugin/marketplace.json` in it: `claude plugin marketplace update <name>`, then `claude plugin update <id> --scope user` for each user-scope install from that marketplace whose version lags the checkout's;
   - with `codex` on `PATH`: `codex plugin add <id>` for each installed Codex plugin whose marketplace source is the checkout.

   Each refresh adds a part to the answer (`fast-forwarded 3; claude buildermesh@buildermesh 0.7.3 -> 0.7.4`). A failure is appended as `plugins: <error>`. A new session loads the change. bmesh runs no other command a job or the registry names.

**The hold.** On a secure box (its `[hosts] secure`, or its roster row's), a level that would move a repo sessions load waits for the operator. This applies to a job, a self-level and `bmesh sync` alike. Such a repo is a plugin marketplace (`.claude-plugin/marketplace.json`, `.agents/plugins/marketplace.json`, or the marketplace source of a Codex install) or the `[update] repo`. A push moves nothing here, so it isn't held.

- The hold keeps the job, the repo, its head and target, and `git log --stat HEAD..<target>`. The job is answered `held for approval (+<N> commits)`.
- A repo has one hold. A newer job's hold replaces it, and the older job's sender gets the reply `hold superseded by job <id>`. A self-level leaves an existing hold as it is.
- The box pushes the hold once (with `[notify]`; title `<host> holds: <repo> +<N> commits (<sender or self-level>)`, priority `default`).
- A hold expires with its job (24h, a self-level's too) and is dropped, logged only.

[`bmesh hosts held`](#bmesh-hosts-held) lists the holds, [`bmesh hosts approve`](#bmesh-hosts-approve) approves one and [`bmesh hosts deny`](#bmesh-hosts-deny) drops one.

**Self-level.** Each daemon with `[hosts] enabled` levels every registered repo it has a checkout of, on start and on each wake, as a fast-forward only, never a push. Refusals are logged, not pushed. Holds apply as above.

**Pushes.** A refusal or failure of a received `level` whose sender stopped waiting (its `until` has passed) is pushed once (title `<host>: <repo> <answer> (job from <sender>)`, priority `high`). So when two boxes are ahead at once, the second's push is refused as not a fast-forward and the phone gets `diverged`.

[`bmesh sync`](#bmesh-sync) levels a repo on demand.

#### The registry

With `[hosts]` on, `repos.toml` is the admin's signed document. A row's `verify` is shell every lead runs (`bmesh scaffold` writes it into `.bmesh/routines/verify.toml`; `bmesh land` refuses a branch that changes it there or in `AGENTS.md` by hand), and its `path` is what `nix` deletes. With `[hosts]` off it is a plain local file.

- The admin publishes its `repos.toml` as written, comments and all.
- A box takes a registry only when its pinned admin key signed it, its `version` is higher than the box's own, and the box's own loader reads the rows (with `repos.toml`'s refusals, an unknown key among them).
- The box writes the rows as its `repos.toml` (through a rename, the old file kept as `repos.toml.prev`). Its hosts layer (presence, levels, prune and nix) reads the new rows at once; the routine loop reads them at start.
- A box that can't read a registry leaves its file alone, and the newer registry stays on the hub for the boxes that can. The box pushes this once per version (with `[notify]`): `<host>: refused: registry v<N> needs a newer bmesh (<error>)`, priority `high`.
- Every daemon re-reads the hub's registry each minute (so at start and on wake). It puts its own back when the hub's copy is missing, unsigned or lower, as with the roster.
- A box re-pinned to a new admin starts again from no registry.

The admin changes the registry with [`bmesh repos publish`](#bmesh-repos-publish). Another box sends its changes with [`bmesh repos propose`](#bmesh-repos-propose). [`bmesh repos remove`](#bmesh-repos-remove) takes a row out.

#### Pruning

`prune [<repo>]` is a built-in op: `repo` a registry name, or `""` for every registered repo with a checkout on the box; `args` `["apply"]` to remove, else a dry run. Each box judges its own checkouts by its own nodes:

1. Each linked worktree of the checkout is a row (the main checkout and routine checkouts under `[runner] checkouts` aside).
   - A worktree under `[runner] worktrees` is bmesh's. It is removed (`git worktree remove`, never forced, then `git branch -d` of its branch, `-D` when the merge was proven by `git cherry` or the lead's branch) when its node (by worktree path) is `done`, it is clean, and its HEAD is merged into `main` (as `node forget --worktree` judges it, see [`bmesh node forget`](#bmesh-node-forget)) or into its lead's branch. Else it is kept, with the reason `no bmesh node`, `node <state>` (`idle` with no state), `uncommitted changes` or `unmerged`.
   - Any other worktree is kept, `not bmesh's (T3 cleans its own)`. T3 0.1.0 removes a thread's worktree once it has no commits beyond the default branch, keeping one with uncommitted changes.
2. Then every local `bmesh/*` branch no worktree holds is removed when `git branch -d` accepts it (merged into its upstream, else into the checkout's HEAD), never with `-D`. Any other is kept, `unmerged`.

With `apply`, the box holds the repo's lock while it removes.

The answer is a line per row:

- `<repo> <worktree path> <keep|remove|removed|failed> <reason>`;
- `<repo> branch <name> <keep|remove|removed> <reason>`;
- a failed removal adds `  (failed: <error>)`.

The last line is `<N> worktrees, <B> branches, <M> to remove, <K> kept` (applied: `, <R> removed`, and `, <F> failed`). A named repo the box has no checkout of answers `no checkout of <repo> here`.

[`bmesh prune`](#bmesh-prune) sends the op.

#### Other ops

**Another box's worktree.** `forget-worktree <node path>` runs the removal of `node forget --worktree` on the node's own box, with the same rules (see [`bmesh node forget`](#bmesh-node-forget)). On success it forgets the node there too: `removed worktree <path>; deleted branch <b>; forgot <path>`. `forget-worktree <node path> wipe`, sent by [`node forget --wipe`](#bmesh-node-forget), also removes the node's brief there and leaves the node for the wiping box to purge: `removed worktree <path>; deleted branch <b>; removed brief`. A box on an older bmesh refuses it (`takes a node path`). Beyond the removal's own refusals it answers:

- `refused: no node <path>`;
- `refused: <path> is on <host>, not <box>`;
- `refused: <path> is <state>; only a done or blocked node is forgotten`.

**Another box's thread.** `stop-thread <node path> <who> <why>` stops one of the box's own nodes as [`bmesh node stop`](#bmesh-node-stop) does there:

1. It marks the node `blocked` with `stopped by <who>[: <why>]`.
2. It publishes that `blocked`.
3. It interrupts the thread through the box's runner.
4. It answers `stopped <path>`, or `already <state>` for a `done` or `blocked` node (a stopped one is interrupted again).

It refuses `refused: no node <path>`, `refused: <path> is on <host>, not <box>` and `refused: <path>'s runner is "<kind>", not this box's <runner>`.

**A step's check.** `check-step` (`repo` a registry name, `args` `[<spec id>, <step number>]`) runs one step's `check:` for `bmesh spec steps --check` (see [`bmesh spec steps`](#bmesh-spec-steps)). The job names the step, never a command.

1. The box fetches its own checkout of the repo, under the repo's lock.
2. It reads the spec's `operator_steps` from its `origin/main` (`main` without an origin).
3. It runs that step's check through the platform shell in the checkout, for at most 2 minutes.
4. It answers `exit 0`, or `exit <code>: <the output's last line>`.

It refuses `refused: no checkout of <repo> here`, a spec or step it can't find, `refused: step <n> is for "<box>", not <host>` and `refused: step <n> has no check`. The job lives 10 minutes, since its sender stops listening after `--wait`. Accepting `check-step` lets a box run the `check:` commands of specs on `main`, as a box with routines runs main's kinds. The admin chooses per box (`bmesh hosts set <name> --accept ...`).

**Removing a repo everywhere.** `nix <repo>` is a built-in op signed by the admin key, from the admin box. It carries the repo name and the origin of the repo's row in the admin's registry (`args` `[<origin>]`, plus `ignored-ok`), never a path. The path comes from the receiving box's own registry row.

- A repo with no row there answers `absent: <repo> is not in this box's registry`.
- A row whose checkout doesn't exist answers `absent: no checkout at <path>` (its routine checkouts are still removed).

The box refuses, touching nothing, unless all of these hold:

- the directory (symlinks resolved) has the name of the row's path;
- it is a checkout's top level;
- it has an `origin` (`refused: <path> has no origin`) that is the job's (`refused: <path>'s origin is <url>, not the registry's <origin>`; a GitHub URL matches its `owner/name`, and a trailing `/` or `.git` doesn't count);
- the box's own row names no other remote.

It also refuses:

- a checkout with uncommitted or untracked changes;
- a linked worktree (other than a routine checkout) with uncommitted or untracked changes, or whose HEAD is not in `main` or `origin/main` (`refused: worktree <path> is on <branch>, not merged into main`);
- a commit on no remote (`git log --branches --tags --not --remotes`, so an unpushed tag too: `refused: <path> has commits on no remote (<commit>)`);
- a stash;
- ignored files, in the checkout or a linked worktree, unless the job says `ignored-ok` (`refused: <path> has ignored files (<paths>): nix with ignored-ok to remove them too`).

On success it removes the linked worktrees (`git worktree remove`), the routine checkouts `<[runner] checkouts>/<checkout name>` and the checkout, under the repo's lock: `removed <path> and <checkouts dir>[, with <N> linked worktrees]`. [`bmesh repos remove`](#bmesh-repos-remove) sends it, as `bmesh repos remove <name> --checkouts [--ignored-ok] [--apply] [--wait 1m]`.

#### Threat model

- Whoever holds the hub token can read every message, delete keys and streams, and flood subjects. Signatures don't stop that.
- Signatures stop such a party, or any box not in the roster, from getting a box to act.
- Revocation is best-effort against a token holder, who can delay a box's view of the newest roster but can't forge one.
- A box's `host.key` is readable by its own agents, so a compromised box can act as itself until it is revoked.
- The admin and approve keys are the secrets no agent can use.

Internals: [internals](internals.md#hosts)

### The plugin

The repo is a Claude Code plugin, `buildermesh`, with twelve skills. The binary reads none of them; a session loads them.

| Skill | Run by | What it covers |
|---|---|---|
| `ideate` | the operator, from a chat | a proposal round ([`bmesh ideate`](#bmesh-ideate)) |
| `choose-ideas` | the operator, from a chat | the verdicts on a pitch ([`bmesh choose-ideas`](#bmesh-choose-ideas)) |
| `build` | the operator, from a chat | dispatch, watch, holds and stops ([`bmesh build`](#bmesh-build)) |
| `scaffold` | the operator, from a chat | a ship repo's shared sections and its rule sets ([`bmesh scaffold`](#bmesh-scaffold)) |
| `mesh-status` | the operator, from a chat | what's building ([`bmesh mesh-status`](#bmesh-mesh-status)) |
| `sync` | the operator, from a chat | levelling repos across boxes (below) |
| `prune` | the operator, from a chat | removing finished worktrees across boxes (below) |
| `fix` | the fixer | the fixer's pass (see [The fixer](#the-fixer)) |
| `lead` | a lead | one spec, its executors, one `review` pass, landing, feedback |
| `executor` | an executor | its own worktree and branch only; it reports and asks through the lead and never merges or pushes, except its own branch to queue a kind of another OS |
| `ideate-engineer` | a round's engineer | a round's method, from reading the repo to the pitch the operator answers with `bmesh choose-ideas` |
| `ideate-ux` | a round's UI/UX engineer | the same, for how the app looks and feels |

Seven are the operator's chat entry points: `ideate`, `choose-ideas`, `build`, `scaffold` and `mesh-status` for the lifecycle; `sync` and `prune` across boxes. The nodes the lifecycle starts follow the other five.

#### From a chat

From a chat (a phone, or a session with the plugin and no command typed), the operator runs this lifecycle through the plugin's skills:

- **`buildermesh:ideate`** ("proposals for X", "run a round on X") runs `ideate` on the row's `host` (locally, else over ssh), with what was asked beyond the repo's name as `--context`; watches with `bmesh watch --nodes <repo>-round-<N>`, relaying an engineer's `ask`; and reports the round proposed (its ranked pitch) or blocked. For one idea of the operator's own (by what was asked, not the words) it starts no two-thread round: `ideate --idea` (one engineer thread, on the row's `host`) when a thread, a model or an effort is asked for, or the idea needs the repo read and measured at length; otherwise it writes the spec itself (one half-pager, or two or three in build order for an idea too big for one lead, `proposed_by: operator`) and lands it on `main` as `proposed`.
- **`buildermesh:choose-ideas`** (the reply to a pitch, in plain words) runs `choose-ideas`.
- **`buildermesh:build`** ("build X 3", "build what's accepted in X") runs `build` on the row's `host`; after a `held` refusal it drains its own family, waits with `bmesh watch` on the named nodes and runs `build` again; a hold by another coordinator's family, or a red `main`, it reports. It watches the leads with `bmesh watch --nodes <paths>`, relays each `ask` and answers it with `bmesh --node operator send`, reports a lead reaching its gate as one line, holds and releases with `hold` and `unhold` ("hold X", "hold land on X", "release X"; see [Holds](#holds)) and stops with `node stop` ("stop X"; see [`bmesh node stop`](#bmesh-node-stop)), never with free text. Once the family's watch is done it names the fixer started for the issues naming bmesh's own repo (see [The fixer](#the-fixer)), and turns the rest into specs and drains them (see [`bmesh feedback`](#bmesh-feedback)).
- **`buildermesh:scaffold`** ("scaffold X", "change X's rules") runs `scaffold --check` and shows the diff. When the repo's `AGENTS.md` has no `rules` block, or the operator asked to change its rules, it interviews the operator first: it shows each of the [rule sets](#rule-sets) and asks which to pick (several, or none), recommending `core`, plus `tests` when the repo has tests and `design` when it holds stateful or concurrent code, each with the evidence it saw; it writes the pick as an empty block (`<!-- bmesh:rules <sets> -->` and its closing marker) at the end of `AGENTS.md`, or rewrites the existing opening marker, and runs `scaffold` to fill it; on a `customized` block it asks whether to keep it (the default) or go back to bmesh's sets. Then, on the filled block, it clears conflicts out of the repo's own prose: it reads `AGENTS.md` outside bmesh's markers, `CLAUDE.md` and any file `AGENTS.md` says to read at the start for lines that restate a picked rule (deleted, since the block carries it), contradict one (the operator says which wins: the repo's line, with the rule cut or reworded in the block, which marks it customized, or the rule), or rest on a set not picked (asked the same), and changes only what the operator answers, running `scaffold` again after an edit to the block so its marker reads `customized`. Then it runs the row's verify, commits `Scaffold the shared definition of done, proposals and rules` with those files, and pushes.
- **`buildermesh:mesh-status`** ("what's building", "where is X") summarises `mesh-status` (and `spec list` for a named repo) as working, asking, silent, stopped and done since the last look.

`ideate` and `mesh-status` also run "stop X" as `node stop`, never as a message, which would wake the node. `ideate` and `build` add the Fable fallback the binary lacks: a Fable model the runner rejects runs once more with `--model claude-opus-5-5`, and the skill reports what the runner said.

Each skill ends with one line: what is started or landed and what waits on the operator, each thing named in plain words (a spec or issue by what it does, its number after it in brackets if at all). The done rule `scaffold` writes and every build brief's closing end the same way (`brief.PlainWords`).

**`buildermesh:sync`** ("sync my repos", "sync X") shows what `bmesh sync` would do (it has no dry run): this box's checkout against a fresh fetch of origin (up to date, behind, ahead, or what will refuse it) and, from `bmesh hosts --json`, the boxes that would get a `level`, their state and which are secure; then runs `bmesh sync --all` or `--repo R` and reports per box what moved, is held for approval, was refused or has yet to answer. See [`bmesh sync`](#bmesh-sync).

**`buildermesh:prune`** ("clean up worktrees") runs `bmesh prune` dry first, shows the keep and remove rows per box, runs it with `--apply` and reports per box; on a bmesh without `prune` it says so. See [`bmesh prune`](#bmesh-prune).

Both stop, saying so, on a box with `[hosts]` off.

**The optional `method` skill.** The lead skill runs `method:writing-plans` on an L or larger spec when it is in the session's skill list, and otherwise writes the plan itself; the binary doesn't read the `method` plugin. Engineering principles come from the repo's `AGENTS.md`, its [rule sets](#rule-sets) included, which every node works by.

**Install** it from the repo:

```
claude plugin marketplace add rickdiculousli/buildermesh && claude plugin install buildermesh@buildermesh
```

or from a checkout: `claude plugin marketplace add <path to the checkout> && claude plugin install buildermesh@buildermesh`.

**The hook.** `hooks/hooks.json` runs `bmesh hosts --here` and then `bmesh update --notice` at every session start when `bmesh` is on PATH, with a 10-second timeout, and always exits 0. The stderr of `bmesh update --notice` is dropped, so a binary older than the plugin prints nothing there. On a box with `[hosts]` on, a session opens with the line naming the box (see [Several boxes](#several-boxes) and [`bmesh hosts --here`](#bmesh-hosts---here)); on a box whose binary is behind `main`, with the notice (see [Updates](#updates)). The hook prints nothing else.

**Briefs and conventions.** A lead's brief says "follow the `buildermesh:lead` skill" and adds the operator's own conventions; the executor header names the executor skill (see [`bmesh executor new`](#bmesh-executor-new)); a round's briefs name the ideate skills (see [Briefs](#briefs)). The skills name no repo convention: `docs/agents-snippet.md`, the paragraph a repo's AGENTS.md gets, is embedded in the binary for `bmesh scaffold` and kept in the repo for reading.

### Names

- A node path matches `^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)*$`.
- Its first token is never `inbox`, `daemon`, `feedback`, `host` or `hostreply`, which start the subjects that are not a node's events. `routine` is the routine pool's (`routine.<kind>`, `routine.<kind>.<instance>`).
- No token contains `--`: the inbox consumer name maps `.` to `--`, so `a--b` and `a.b` would collide.

**Node token.** A registry name may hold capitals, `_` and `.` (see [repos.toml](#repostoml)); a path token can't. So every path, family and branch made from a repo's name starts with its node token. These include `<repo>-round-<N>` and its children's branches, `<repo>-<NNN>`, `<repo>-build-<N>`, and the branches `build` and `choose-ideas` record on `main` through.

The node token is the name in lower case, with `.` and `_` as `-`, runs of `-` collapsed, and a trailing `-` trimmed: `MapleMarketScanner` gives `maplemarketscanner`, `my_repo.js` gives `my-repo-js`. A node's `repo` and the worktree directories keep the registry name.

`bmesh init`, `repos publish` and `repos propose` refuse a registry where two names share a token (`Foo` and `foo`, `my.app` and `my_app`): exit 3, `bmesh: repos Foo and foo are both foo in node paths; rename one`.

Internals: [internals](internals.md#names)

## Config

Three files live in the config dir, `<os.UserConfigDir()>/bmesh` (`~/.config/bmesh` on Linux, or anywhere that directory already exists):

| File | What it holds |
|---|---|
| `config.toml` | This box's settings ([config.toml](#configtoml)). |
| `repos.toml` | The registry of repos, the same on every box ([repos.toml](#repostoml)). |
| `.setup-complete` | The setup flag `bmesh init` writes (see [Synopsis](#synopsis)). |

`BMESH_CONFIG` overrides the config file's path; the other two stay beside it.

Every command loads both files. [`bmesh config check`](#bmesh-config-check) runs that load alone; when it refuses (exit 3), it names the key; `bmesh serve` and `bmesh hub` run it at start and exit 78 on a refused config.

### config.toml

Keys and defaults. An unknown key is ignored. `[runner] t3_dispatch` and `t3_project`, which the t3 runner read before T3 0.1.0, each load with one stderr line: `bmesh: [runner] t3_dispatch is ignored (T3 0.1.0 needs no script); delete it from <path>`.

```toml
[mesh]
nats = ""                                   # "" = the server bmesh serve embeds on listen, which other commands dial; else "nats://localhost:4222"
domain = "hub"                              # JetStream domain
token_file = ""                             # "" = no token
store = "<cache dir>/bmesh/jetstream"       # embedded server's store (os.UserCacheDir: ~/.cache on Linux, ~/Library/Caches on macOS); an existing ~/.local/state/bmesh/jetstream is kept
listen = "127.0.0.1:4222"                   # embedded server's bind
[host]
name = ""                                   # "" = `hostname -s`; also the name of the server bmesh hub runs
[hub]                                       # the server bmesh hub runs here (see Several boxes)
role = ""                                   # "" = no hub here | "hub" | "leaf"
listen = ""                                 # hub: "<tailnet ip>:4222"; leaf: "127.0.0.1:4222"; never every interface
leaf_listen = ""                            # hub only: "<tailnet ip>:7422", the port leaves dial; "" = none
remote = ""                                 # leaf only: "nats://<hub tailnet ip>:7422", redialed forever
store = ""                                  # hub only; "" = <state dir>/hub/jetstream
max_file_store = "8GB"                      # hub's JetStream limits, as nats-server reads sizes (GB/MB powers of 1024, G/M of 1000)
max_memory_store = "256MB"
[serve]
listen = "127.0.0.1:3774"
[runner]
kind = "t3"                                 # claude | t3 | tmux (init writes claude when claude is on PATH)
t3_server = "http://127.0.0.1:3773"         # this box's T3 0.1.0; "" is a config error
t3_runtime_mode = "auto"                    # auto | full-access: the mode t3 threads start in
tmux_session = "bmesh"
worktrees = "~/.bmesh/worktrees"            # executors' worktrees, and the claude and tmux runners'
checkouts = "~/.bmesh/checkouts"            # routine instances' checkouts; "" = routines off
claude = "claude"                           # claude command for model routines and the claude runner, split on spaces
max_leads = 0                               # leads at once on this host; 0 = no cap
[notify]
ntfy = ""                                   # "" = no digest; else "http://k16:2586/bmesh"
command = ""                                # test hook instead of HTTP: run through the platform shell, POST body on stdin, title in $BMESH_TITLE (and $NTFY_TITLE)
digest_every = "1h"
push = ["ask", "blocked", "done"]           # lead (and round) events pushed at once; only these three (another is a config error); [] = none
map_url = ""                                # map URL for a push's click-through (<map_url>#/lead/<path>); "" = none
catch_up = "1h"                             # on start, push missed events no older than this
[liveness]
grace = "20m"
poll_every = "60s"
wake_every = "10m"                          # an idle lead (or engineer) with unread mail is woken at most this often
[spec]
glob = "docs/proposals/{id}-*.md"
[models]
lead = "claude-opus-5-5"                    # lead start's model when --model names none (build passes the spec's judge)
lead_effort = "high"                        # low, medium, high, xhigh or max; "" = the runner's default
proposer = "claude-opus-5-5"                # a proposal round's two threads
proposer_effort = "xhigh"
[status]
keep_done = "24h"                           # mesh-status hides nodes done longer than this; the daemon deletes stopped routine instances' nodes after it; "0" = never; days work: "30d"
[routines]
keep_results = "30d"                        # bm-results' age limit, applied by serve; "0" = none
[quota]
command = ""                                # the map's quota strip; "" = none. Run through the platform shell, at most every 5m (30s timeout); prints a JSON array of {"reading":{"provider","five_hour","week","five_reset","week_reset"}}
remind_topic = ""                           # ntfy topic for a used 5-hour window's reset reminder; "" = none; needs command and [notify] ntfy (see Quota reminders)
[console]
thread_url = ""                             # the map's thread link for leads here; {thread} = the runner thread, path-escaped (mcp:x is mcp%3Ax); "" = this host's [console.hosts] entry, else no link
[console.hosts]                             # host = template, for leads on other hosts (e.g. k16 = "http://k16:3773/…/{thread}")
[update]
repo = ""                                   # registry name the daemon rebuilds itself from, at what track follows; "" = off (see Updates)
check_every = "6h"                          # without repo: how often to check what track follows on the module's main and write the notice when behind; "0" = never
upstream = false                            # without repo: also go install a newer target, drain and re-exec; on Windows only under serve --restart
track = "release"                           # what every update follows on main: release = the newest vX.Y.Z tag (the code head until there is one); latest = main's code head (see Updates); bmesh update --latest goes there once
[hosts]                                     # written by bmesh init --hosts (see Hosts)
enabled = false                             # nothing of the hosts layer runs unless true
admin = false                               # this box is the roster's admin
secure = false                              # what others ask of this box waits for the operator
key_store = ""                              # keychain | file: where the admin or approve key lives; "" = keychain on macOS, file elsewhere
push_modes = ["ship", "fork"]               # registry modes whose rows a level pushes when this box is ahead (see Hosts)
[addons]                                    # optional parts compiled into bmesh (see Add-ons)
enable = []                                 # the add-ons serve runs here: t3-done; [] = none; another name is a config error
[addons.t3-done]
ntfy = ""                                   # the ntfy URL with its topic it posts to ("http://k16:2586/t3"); required when enabled; never [notify]'s
every = "5s"                                # how often it looks at T3's database
min_busy = "5m"                             # a done posts only when the thread was busy at least this long; "0s" = every done
```

#### State dir

What the daemon keeps across restarts (the T3 sign-in's push record, `roster.json`, the salt records, holds in `held/`, per-repo locks in `locks/`) lives in the state dir:

| Where | State dir |
|---|---|
| `XDG_STATE_HOME` set | `$XDG_STATE_HOME/bmesh` |
| macOS | `~/Library/Application Support/bmesh/state` |
| elsewhere (Linux, Windows) | `~/.local/state/bmesh` |

The state dir is never in the cache dir, because macOS purges the cache dir.

#### Wakes

At most once per `wake_every`, the daemon sends a wake to the thread of a lead or engineer on this host when that node is idle with unread mail, or silent whatever its mail. These nodes are exceptions:

- A `done` or `blocked` node is woken only for a message sent with `bmesh send`. So a question from you reaches a finished lead; a late report or reply doesn't wake it.
- A stopped node is woken only for a message from outside its own tree sent after the stop (see [`bmesh node stop`](#bmesh-node-stop)).
- A thread its runner reports stopped by the user is never woken.
- A lead with a ready request ([`bmesh ready`](#bmesh-ready)), `landing` or `held`, is left to its daemon's lander: only a message wakes it, once the lander has cancelled any running step and ended the request. The lander sends its own two wakes at once, without waiting out `wake_every`: the refusal when land refuses, and the final wake once the change has landed.

#### Silence

The daemon marks a `working` node `silent` (`quiet for <age>`) when nothing at or under its path was published within `[liveness] grace`. It never marks a `blocked` node silent, a stopped one included, nor a `held` or `landing` one.

#### Wake text

A wake starts `bmesh: ` (`bmesh: resumed by <sender>; ` for a stopped node), then names what waits: `` bmesh: 3 unread: done from app-012.alpha (branch t3code/x-alpha at 1a2b3c4d); reply to job <id> (review fail); message from operator. Run `bmesh inbox` now, then continue. ``

| Item | Reads |
|---|---|
| report | `done from <path> (branch <b> at <sha8>)` or `blocked from <path>` |
| reply | `reply to job <id> (<kind> ok\|fail)` |
| other | `<kind> from <from>` |

A wake lists up to five items, oldest first, then `and N more`. It prints the count alone (`bmesh: 3 unread. Run ...`) when there is nothing to list or the read fails. A landing lead's two wakes read otherwise: `bmesh: land refused and needs you: <refusal>`, land's last lines, then ``Fix it, commit, then run `bmesh ready` again.``; and `bmesh: landed <sha8>, spec NNN done; origin/main at <sha8> contains it. Write your closing line; nothing else to run.` (see [`bmesh ready`](#bmesh-ready)).

Internals: [internals](internals.md#configtoml)

### repos.toml

One `[repos.<name>]` table per repo. A name matches `^[A-Za-z0-9][A-Za-z0-9_.-]*$` (a directory name, so a checkout keeps it), quoted when it holds a dot (`[repos."my.app"]`). Keys and defaults:

```toml
[repos.party-buzzer]
path = "~/repos/party-buzzer"           # required; this box's checkout (~ expands; relative = from the config dir)
mode = "ship"                           # ship | companion | publish | fork | dormant
remote = ""                             # owner/repo or a URL, for people and the table
host = ""                               # box for proposal threads ("" = this one)
ux_host = ""                            # box for UI/UX threads
verify = []                             # shell commands a change must pass
surfaces = []                           # UI screens a UI/UX thread screenshots, with viewports
pin = ""                                # release tag every checkout stays on
routines = ["verify", "review"]         # routine kinds a build runs
code = []                               # git pathspecs of the code ("cmd/**", "*.go"); none = all but the spec files
setup = ""                              # shell command that installs what a fresh worktree lacks ("npm ci"); none = no setup
setup_inputs = []                       # git pathspecs whose files make an install stale (":(glob)**/package-lock.json")
setup_timeout = ""                      # a setup run's cap; "" = 10m
```

**Code.** `code` lists git pathspecs from the repo's root, read as `git log -- <pathspec>` reads them (`*` crosses `/`: `*.go` is every Go file, `cmd/**` everything under `cmd/`). A change is code when it touches a matching path. `.bmesh/` (the routine kinds, which define verify) is always code. When `code` is unset or empty, code is everything but the spec files `[spec] glob` matches (`docs/proposals/NNN-*.md`), so a `README.md` beside them is code. The code head (see [Updates](#updates)) is what `build` verifies, `release` tags and `[update]` follows, and `routine run`, `land`, `build` and `release` carry a `verify` across commits that change nothing under code (see [Carried verify](#bmesh-routine-run)). So list every file the build or tests read (`go:embed` sources, fixtures, documents a test checks); else a change to that file alone is never verified.

**Setup.** `setup` is the one shell command that installs what the repo gitignores and a fresh `git worktree add` lacks (its dependencies: `npm ci`, `pnpm install --frozen-lockfile`, `uv sync`). [`bmesh scaffold`](#bmesh-scaffold) writes it, with `setup_inputs` and `setup_timeout`, into the repo's `.bmesh/config.toml` as `[setup] command / inputs / timeout`, and bmesh runs the `[setup]` of the commit it works on, never the row, so a box with another registry version runs the commit's (see [Setup](#setup)). `setup_inputs` are git pathspecs, read as `code`'s are: `**/package-lock.json` matches only nested lockfiles (`*` crosses `/`, but the `/` must be there), and `:(glob)**/package-lock.json` matches the root's too. They are code beside `code` (a commit that only bumps a lockfile is verified, never carried). `setup_inputs` and `setup_timeout` need `setup`; `setup_timeout` is a Go duration. A row with no `setup` behaves as before it existed.

**Errors.** One loader serves every command, so an unknown key, a row without `path` or a mode outside the five fails them all. `[repos]` in `config.toml`, the registry's old place, is refused with a message naming `repos.toml`. `bmesh lead start --repo`, `routine queue` and the daemon resolve repo names here.

**With `[hosts]` on**, each daemon rewrites this file from the newest signed registry (the old file kept as `repos.toml.prev`), so a local edit lasts until the next registry. Change it with [`bmesh repos publish`](#bmesh-repos-publish) on the admin box or [`bmesh repos propose`](#bmesh-repos-propose) elsewhere (see [The registry](#the-registry)).

**Init.** `bmesh init` adds the current directory's checkout as a row (`path`, and its GitHub origin as `owner/repo`) unless it is there, rewriting the whole file, so comments are lost.

The commands that read and publish it: [`bmesh repos`](#bmesh-repos).

### Pushes

With `[notify] ntfy` or `command` set, a daemon also pushes a lead's events named in `push` to the same topic as they happen:

| Event | Title | Priority | Tag |
|---|---|---|---|
| ask | `<path> asks` | `high` | `question` |
| blocked | `<path> is blocked` | `high` | `no_entry` |
| done | `<path> is done` | `default` | `white_check_mark` |

- **Body:** the event's text, cut to 3 KB; done adds `commit <sha8>`.
- **Tap (`Click`):** the lead's thread when `[console]` resolves it, with an `Open map` action for its map page when `map_url` is set; else the map page. The phone must reach both, so on a tailnet `map_url` and the `[console]` templates use the full MagicDNS name (`http://<host>.<tailnet>.ts.net:3773/…`), not the short host name.
- **`command`** gets the body on stdin and the headers in `$NTFY_TITLE`, `$NTFY_PRIORITY`, `$NTFY_TAGS`, `$NTFY_CLICK` and `$NTFY_ACTIONS`.

**Other nodes.**

- A fixer (see [The fixer](#the-fixer)) pushes like a lead. Its `done` is titled `<repo> fixes: N fixed, M wait on you` (`waits` for one): `N` counts its report's `<seq>: fixed by` lines, `M` its `<seq>: proposed` ones. The body puts the proposed lines first, the rest after as written. A fix lead (a `<repo>-fix-<N>` family) never pushes; its fixer reports it.
- A round's `done` and `blocked` and an engineer's `ask` push as [`bmesh ideate`](#bmesh-ideate) says; the T3 sign-in's expiry pushes as [`bmesh t3 login`](#bmesh-t3-login) says; with `[hosts] enabled`, a box's refusals push as [Hosts](#hosts) says (even with `push = []`).
- Executors' and routines' events never push.

**Once only.** An event is pushed once, across restarts and daemons. On start a daemon pushes, in order, the listed events no older than `catch_up` that nobody pushed yet, then follows live. A push that still fails after retries at 5 s and 30 s is logged and never repeated. At shutdown the push being posted gets 10 s.

Internals: [internals](internals.md#pushes)

### Quota reminders

With `[quota] remind_topic` set (an ntfy topic name: 1 to 64 of `A-Z`, `a-z`, `0-9`, `_` and `-`, else the config is refused), the daemon reads quota on start and then hourly. It reads through the map strip's gate, so `[quota] command` still runs at most every 5 minutes, whichever reader triggers it.

A provider gets a reminder when it has usage in its 5-hour window (`five_hour` above 0) and its `five_reset`, rounded to the minute, is still ahead. The daemon then posts one message per window to the `[notify] ntfy` URL with its last path segment replaced by the topic (`http://127.0.0.1:2586/bmesh` and `quota-reset` give `http://127.0.0.1:2586/quota-reset`):

- title `Claude 5-hour quota reset` (the provider's name, capitalised);
- body `Claude's 5-hour window is fresh. Weekly was 41% used.`;
- tag `hourglass`;
- an `At` header of the rounded reset, so ntfy delivers it then; none when the reset is under 30 s away (bmesh's margin over ntfy's 10 s minimum delay), so it posts at once.

The daemon logs a failed post and retries it at the next reading.

Internals: [internals](internals.md#quota-reminders)

### Add-ons

Add-ons are optional parts compiled into `bmesh`. `bmesh serve` runs one only when `[addons] enable` lists it; the default `[]` runs none. An unknown name, or one listed twice, is a config error (exit 3 from `bmesh config check`, 78 from `serve`), and each add-on's settings live in `[addons.<name>]`. One add-on exists, `t3-done`.

#### t3-done

`t3-done` posts to its own ntfy address when a T3 thread a person started on this box is done, failed or asking. T3's desktop notifications reach only the box running the app; this reaches the phone.

```toml
[addons]
enable = ["t3-done"]
[addons.t3-done]
ntfy = "http://k16:2586/t3"                 # required: enabling t3-done without it is a config error
every = "5s"
min_busy = "5m"                             # the default; "0s" posts every done
```

Turning it on never starts the `[notify]` digest or pushes. It reads this box's T3 database, `~/.t3/userdata/statev2.sqlite`, read-only. Each tick (`every`) it compares the database file's and its `-wal` file's modification time and size with the last read, and reads only when either changed, so an idle T3 costs one file check per tick.

| What happens | Posts | Tag |
|---|---|---|
| A run ends `completed`, the thread has no background task pending, and it was busy at least `min_busy` | done | `white_check_mark` |
| A run ends `failed` | failed | `x` |
| A runtime request (a question to you) turns `pending` | asking for input | `raising_hand` |

- **Not posted:** a run that ends `interrupted`, `cancelled` or `rolled_back`, and a run that ends `completed` while a background task (a monitor, a background command) is still pending. That run is a wake-up, not the end: a coordinator waiting on `bmesh watch` posts nothing until its watch ends and it reports. A thread whose background task ends without a new run stays silent until its next turn.
- **Busy long enough:** a done posts only when the thread was busy at least `min_busy` (default `5m`), so a quick back-and-forth you're watching never pushes. Busy runs from your last message to the end of the run, so a turn a background task woke counts from the ask that armed the task, wait included. Time between turns counts too: a background task's wake-up after a done that already posted is measured from the same ask, so it posts a second done. With t3-done enabled, a negative `min_busy` is a config error. Failed and asking post however short.
- **Which threads:** top-level threads a person started. Threads with a parent (a delegated child) and threads an agent created (bmesh's leads and rounds, which push on their own) never post.
- **Title:** the thread's title.
- **Body:** `done in <project> on <box>: <the start of the last assistant reply>`, likewise `failed in …` and `asking for input in …`. The reply is put on one line and cut to 140 characters with `…`. There's no `in <project>` without a project and no `: …` without a reply.
- **Tap (`Click`):** the thread, through `[console] thread_url`, when that is set; else none.
- **Once each:** a mark per kind (the run's end, the request's creation) lives in `<state dir>/addons/t3-done/` (`runs`, `requests`). Every row read moves it, posted or not, so a wake-up skipped never posts later when its task ends. A fresh start begins at the current time. A post that fails isn't marked, so the next tick posts it again.
- **Idle on a mismatch:** before each read it checks that the tables and columns it reads exist, and that the newest rows carry the payload fields it reads. When one is missing (a T3 update changed it), it logs one line naming it, `t3-done: idle: T3's database has no column orchestration_v2_projection_runs.completed_at (a T3 update changed it); nothing is posted until it does`, posts nothing, and checks again when the database next changes. Without a database it logs `t3-done: no T3 database at <path>` once. When a read works again it logs `t3-done: reading again`.

Internals: [internals](internals.md#t3-done)

### Map

The map (`/`) draws one block per lead, a tree of the lead and its executors (a round and its engineers alike), then the orphans block, then the routines column (last on a phone, where blocks stack). Blocks run in need order: `blocked`, `asking`, `silent`, `working`, `landing`, `held`, `idle` (no state yet), `done`. While a live lead (any state but `done`) is on the map, done leads leave the tree for a shelf under the live blocks: chips (`buildermesh-001 ✓`, newest first) headed `done (N)`, each linking its lead page. With no live lead, done leads draw as trees.

**The routines column** draws one box per kind, with its queue and instance counts, linking to the routines page, which lists the instances per repo.

**The strip**, under the header, opens with a summary row, `2 working · 1 landing · 1 asking · 1 silent · 2 held · 3 done` (`all idle` when none), over leads, rounds and their children, not routines. Below it come the quota lines, then the newest digest as a fold (`▸ digest 50m ago: <title>`) that opens to its body.

**The recent-events drawer** shows each row's node as a chip in its lead tree's hue (routines and `daemon.*` grey); an inbox row shows the sender's chip, `→`, and a small recipient chip. Hovering a chip dims every row but that node's and those addressed to it. Clicking it (a tap on touch) pins that focus until you click it again or press Escape.

**The needs-you band** above the map has these rows:

1. One row per lead, round or engineer that is `asking`, `blocked` or `silent`, in need order: state badge, path (linking its lead page) and the state's words (the ask or block text; for a silent node `quiet for 20m` and how long ago). A row leaves on the node's next `started`, `progress` or `done`.
2. Then one row per landed spec with operator steps still open, from every configured repo: an `operator` badge, `<repo> NNN`, and `<title>: X of Y open · bmesh spec steps NNN --repo <repo>`. The row goes once every step is ticked.

On the map, an `asking` or `blocked` box's second line is the state and its words. A `held` box (a coordinator holds the lead at a gate) has a badge in its own slate colour and its words as the second line. A `held` lead counts in the strip but never gets a band row, because the coordinator put it there. A `landing` box (a lead that ran `ready`, its box's daemon landing its change) has a badge in its own calm teal and as its second line the daemon's latest landing words (`review job abc running on k16`); it counts in the strip (`1 landing`) and never gets a band row. The daemon's `land-refused` and `land-withdrawn` events put the lead back to `working`.

**The lead page** (`#/lead/<path>`, for any node) shows, in order:

1. a `tree` card, one row per child (state badge, last segment of its path, branch, `N unread`) linking its page;
2. the lead's `spec` card, or for a round a `specs` card with one `NNN title · status` line per spec its engineers proposed, opening the body in place (`no specs landed yet` before any lands);
3. its inbox and timeline.

The spec is read from the repo's local `origin/main` (`main` without an origin). While lead pages are viewed, the daemon fetches `origin main` in the background at most once a minute per repo, so a spec shows a minute or two after landing. A card with nothing to show (`no spec`, `no messages`, no events) is one muted line.

Internals: [internals](internals.md#map)

### Upgrade notes

In bmesh's own repo, `docs/upgrading/vX.Y.Z.toml` holds the steps an install at the release before needs. The coordinator distils them from the `## Upgrading` sections of the specs the release carries (the `build` skill, step 7) and commits the file to `main` before `release` (see [`bmesh release`](#bmesh-release)). A TOML key not listed here is an error:

- `summary`: one line, what the release asks of an install;
- `start_over`: `true` when the steps uninstall the box and set it up again;
- `loses`: what a start-over loses;
- `[[step]]`, in order, each with a `kind` and an optional `when`:
  - `kind = "ask"` sets a key: `file` is `config` (the default; `key` is `[section] key` in `config.toml`) or `registry` (`key` is a row key in `repos.toml`, set on every row); `changed`, one line on what changed; `question` (required); `choices`, a list; `default` (required; one of `choices` when given). No `command`, `os`, `confirm` or `expect`.
  - `kind = "run"`: `command` and `why` (both required), `os` (a list of `linux`, `darwin`, `windows`; none means every OS), `confirm = true` for anything destructive, which prompts before running.
  - `kind = "check"`: `command` (required), `expect` (what its output must show), `why`, `os`.
  - Run and check steps take no `key`, `question`, `choices`, `default` or `changed`.
- `when`: comparisons that must all hold, `[section] key == "value"` or `!=` (the value quoted or bare), joined by `&&`, read from this box's `config.toml` just before the step. An unset key reads as its default, so a step after `init` sees the new config.

```toml
summary = "Boxes follow releases; restart the hub."

[[step]]
kind = "ask"
key = "[update] track"
changed = "boxes now update to releases, not every landing."
question = "Follow releases or every landing?"
choices = ["release", "latest"]
default = "release"

[[step]]
kind = "run"
command = "systemctl --user restart bmesh-hub"
why = "the hub runs the old binary"
os = ["linux"]
when = '[hub] role != ""'
```

## Exit codes

| Code | Meaning |
|---|---|
| 0 | ok |
| 1 | error; a failed run (`routine run`, `routine wait`, an inline kind `land` runs); a lost lease; an interrupted `lock-lease run`; a malformed spec, or a `spec set` its `--from` refuses |
| 2 | usage; a `scaffold` of a repo without a row or whose row is not `ship` |
| 3 | refused: each command's section names its refusals (see [`bmesh config check`](#bmesh-config-check) and [`bmesh repos show`](#bmesh-repos-show)) |
| 5 | timeout (`watch`, `routine wait`, `t3 login`, a `land` waiting for its lease) |
| 78 | config: `serve` or `hub` refused this box's config at start, naming the key (see [Config](#config)); their units and `serve --restart` don't start it again |

`lock-lease run` exits with its command's code (see [`bmesh lock-lease`](#bmesh-lock-lease)).

## Build and test

```
GOBIN=~/.local/bin go install ./cmd/bmesh
TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test ./...
GOOS=windows go vet ./...    # type-checks the Windows-only files a Linux go vet skips
```

Tests never use `/tmp`: `meshtest.Start` fails when `TMPDIR` is `/tmp`.

For a spec that calls for repeated runs on a Mac (`go test -count=10`), use the repo's `soak-darwin` kind: push the branch, then run `bmesh routine queue soak-darwin --go` and `bmesh routine wait <job id>` (see [`bmesh routine queue`](#bmesh-routine-queue) and [`bmesh routine wait`](#bmesh-routine-wait)). Only a darwin box takes it; each queue is a fresh run.

### Testing

Runner tests drive shell fakes instead of tmux and claude, and a fake T3. No test reaches the network, the box's cache, the real Keychain dialog, or the box's own `claude` and `codex` (the test-only variables and fixtures are in the internals; leave them unset outside tests). Check the real Keychain dialog by hand on a Mac.

**`scripts/seed.sh [seeder args]`** starts a throwaway `bmesh serve` in the background (embedded NATS and HTTP on free loopback ports; routines and ntfy off) over a seeded mesh that holds everything the map draws: every node state, a done lead on the shelf, a round with its engineers, routines with done instances, a queued job, leases, a digest, quota lines and a landed spec with operator steps open (a band row). It prints the map's URL, its check command and a CLI config for poking the mesh, then returns; serve keeps running.

- State lives under `~/.cache/bmesh-scratch/seed/<checkout directory name>` (`BMESH_SEED_DIR` overrides it, `BMESH_SEED_HTTP` the listen address), wiped each run, so two checkouts seed side by side. `seed.pid` there holds serve's pid; a run first stops its checkout's previous serve.
- `scripts/seed.sh stop` stops it by that pidfile. Never `pkill -f` its command line: it matches the caller's own shell.
- The seeded serve reads the page from the checkout's `internal/web/` on every request, so edits to `index.html`, `map.js` or `map.css` show on reload; a Go change needs a re-run. `BMESH_WEB_DIR=<dir>` does the same for any `bmesh serve` (logged as `bmesh serve: map page from <dir>`); unset, the page is the one embedded at build.

**`node scripts/check.mjs <url> <width> <height> [png]`** (Node 22 or later) is the map's browser check:

1. It loads `url` (adding `?check=1`) in headless Chromium at that viewport, over the DevTools protocol.
2. It waits for the self-check title to show a loaded tree on a live connection (`trees=` at least 1, `conn=` ending `live`).
3. It prints the title and, with `png` given, writes a viewport screenshot there.

| Exit | When |
|---|---|
| 0 | the title showed |
| 1 | no such title showed within 20 s (the last title is printed anyway), or the title's `bandHeld=` is non-zero (stderr: `check.mjs: a held node is in the needs-you band`), or its `bandLanding=` is non-zero (`check.mjs: a landing node is in the needs-you band`), or its `landing=` is 0 or missing (`check.mjs: no landing box on the map`; the seed always has one landing lead) |
| 2 | a usage or browser error |

A hash picks the view (`/#/lead/<path>`, `/#/routines`). Chromium is `$CHROME`, else the newest headless shell or Chromium in the Playwright cache (`$PLAYWRIGHT_BROWSERS_PATH`, `~/.cache/ms-playwright`, `~/Library/Caches/ms-playwright`); stderr names it. `chromium --dump-dom` is no substitute: the page's `/events` stream never ends, so it never returns.

To check a UI change:

1. Run `scripts/seed.sh`.
2. Run the printed `node scripts/check.mjs ... 390 844 shot.png`, and the same at `1280 800`.
3. Run `scripts/seed.sh stop`.

Internals: [internals](internals.md#testing)
