# buildermesh (bmesh)

A NATS-based mesh for coordinating coding agents: lead and executor threads report events, exchange inbox messages and are woken when they go quiet; routines run a repo's checks (shell commands or headless `claude -p` sessions) against commits and record results by tree, and a lead's `done` is checked against them. A repo's work starts as proposal rounds (`bmesh ideate`): two engineer threads write ranked half-page specs into its ledger, the daemon pushes the pitch, the operator's reply (`bmesh choose-ideas`) lands the verdicts on main, and `bmesh build` dispatches an accepted spec to a lead; `bmesh scaffold` gives a repo the sections and routine kinds a build expects.

## Quickstart

A machine with Go and Claude Code (`claude` on PATH) gets a working single-box mesh in four commands:

```
go install github.com/rickdiculousli/buildermesh/cmd/bmesh@latest   # or, from a checkout, GOBIN=~/.local/bin go install ./cmd/bmesh
cd ~/repos/your-repo && bmesh init     # asks single or multi-box, writes config.toml, registers this repo, picks the claude runner
bmesh serve                            # the hub (embedded on a single box), the daemon and the map at http://127.0.0.1:3774/; keep it running
bmesh repos                            # the registry; then bmesh scaffold your-repo, bmesh ideate your-repo, bmesh choose-ideas, bmesh build your-repo 001, bmesh mesh-status
```

Recommended, optional: the [pstack](https://github.com/rickdiculousli/pstack-claude) and `method` Claude Code plugins. The lead and executor skills say, at fixed moments, "if the pstack plugin is installed, use `pstack:<skill>`; otherwise <the written default>" (poteto-mode as the working style, `architect` and `attack-the-premise` when the lead decides the shape, `tdd` as the executor's default, `figure-it-out` before any `bmesh ask`, `blast-radius` before landing, `exhaust-the-design-space` for a round's engineers) and name `method:writing-plans` for an L spec; without them a node works from the defaults, and nothing in the binary looks for either.

`bmesh init` asks for what its flags don't say (`--hub`, `--token-file`, `--runner`, `--no-repo`), reading answers from stdin (an empty line, or no stdin, takes the default), so a script can drive it; `--service` also writes the unit that keeps `bmesh serve` running (a systemd user unit on Linux, a launchd agent on macOS) and says how to enable it. It writes the setup flag last (`<config dir>/.setup-complete`); every other command but `help`, finding neither the flag nor a `config.toml`, prints one line and runs `init` first when run from a terminal, and is refused (exit 3, ``run `bmesh init` ``) when not. On an existing `config.toml` it edits nothing without `--force` (a registry row it already has is left too), but still writes the flag and, with `--service`, the unit. Pick the multi-box answer on a second box and give it the first box's hub URL and token file.

The config lives in the OS config dir (`os.UserConfigDir`: `~/.config/bmesh` on Linux, `~/Library/Application Support/bmesh` on macOS, `%AppData%\bmesh` on Windows; an existing `~/.config/bmesh` is kept on any OS), the embedded hub's store in the OS cache dir (`os.UserCacheDir`); see Config. A configured command line runs through the platform's shell (`sh` on Unix; `cmd /C` on Windows, where every argument is quoted and one holding a quote or a `%` is refused), nothing assumes tmux, and the build is checked on Linux, macOS and Windows (`GOOS=windows go build ./...` in CI); only Linux and macOS are run.

## Build and test

```
GOBIN=~/.local/bin go install ./cmd/bmesh
TMPDIR=$HOME/.cache/bmesh-scratch/tmp go test ./...
```

Tests never use `/tmp`: `meshtest.Start` fails when `TMPDIR` is `/tmp`. CI (`.github/workflows/ci.yml`) runs the tests, `go vet`, `gofmt -l` and the Windows and macOS builds.

### Testing

Runner tests drive shell fakes instead of T3 and tmux. `BMESH_T3_COMMAND`, when set, is split on spaces and replaces `uv run <t3_dispatch>` (tests set it to `sh <repo>/testdata/fake-t3.sh`); `BMESH_TMUX_COMMAND` replaces the tmux binary the same way (`sh <repo>/testdata/fake-tmux.sh`). `BMESH_T3_TIMEOUT` (a Go duration) replaces the t3 runner's 120 s per-call timeout. `BMESH_CLAUDE_COMMAND` replaces `[runner] claude` the same way (`sh <repo>/testdata/fake-claude.sh`), for routines and for the claude runner's tests. `BMESH_SSH_COMMAND` replaces the `ssh` that `bmesh ideate` reaches another host with (`sh <repo>/testdata/fake-ssh.sh`). Leave all five unset outside tests.

## Runners

`bmesh lead start --repo R --spec NNN --brief FILE [--model M] [--effort E] [--title T] [--path P]` starts a lead through `[runner] kind` on `--model` (default `[models] lead`) at `--effort` (`low`, `medium`, `high`, `xhigh` or `max`; default `[models] lead_effort`, `""` the runner's own) and registers it as node `<repo>-<NNN>` (state `idle`, the model and effort recorded on the node), printing `{"path","thread","branch","worktree"}`. A path whose node is not `done` or `blocked` is refused (exit 3); a `done` or `blocked` path is reused and its state reset to `idle`. The runner's start runs under the runner's own timeout, not the CLI's one-minute deadline; if it times out, or registration fails after it, the error names the thread id (and branch and worktree when known) so the session can be found.

- `t3`: `uv run <t3_dispatch> start|status|send|stop`, 120 s per call. `send` runs `status` first and never sends to a running thread; no `--force`, `--queue` or `wait-idle`. A session started in a checkout bmesh made (a round's thread) passes `--worktree <path> --branch <branch>`, and `--repo` names the T3 project it is filed under (`[runner] t3_project`, else the repo). An effort is passed as `--effort <e>` (T3's `effort` model option).
- `tmux`: `git worktree add -b <thread> <worktrees>/<repo>/<thread>`, then `claude [--model M] [--effort E]` in window `<thread>` of session `<tmux_session>` (created when missing; the window gets `BMESH_CONFIG` when the CLI has it, so the lead reports to the same mesh). It watches the window until claude's prompt is up, answering the dialogs a fresh worktree raises (the folder-trust question with yes, the first-run screens with Enter, an offer to try something with Escape), then types the brief in (one second before the Enter, since claude takes a burst of text as a paste); after 90 s it types anyway. It can't tell whether claude is mid-turn.
- `claude`: `git worktree add -b <thread> <worktrees>/<repo>/<thread> <branch or main>` (or the checkout bmesh made, for a round's thread), then each turn is a detached headless `<claude> -p --output-format json --dangerously-skip-permissions` in the worktree, the first with `--session-id <thread> [--model M] [--effort E]` and the brief on stdin, later ones with `--resume <thread>` and the same `--model` and `--effort` (a resume restores neither); no T3, no tmux. The session id is the thread id, so it must be a UUID (`lead start` passes one). Turns inherit the CLI's environment (so `BMESH_CONFIG` reaches the lead). State lives in `<user cache dir>/bmesh/claude/<thread>/`: `worktree`, `model`, `effort`, `pid`, `turn`, and `turn-<n>.in`, `.json`, `.err` per turn. A turn is running while its pid is alive and its `.json` is not yet claude's result; `status` replies with the newest result. `send` mid-turn queues the text under `queue/` instead of refusing, and `status` starts the oldest queued send once the turn ends (the daemon polls status every minute, so queued sends drain). An interrupt kills the running turn's process group.

`lead start` refuses with exit 3 `bmesh: N leads already working on this host` when `[runner] max_leads` is set and that many leads registered on this host are not `done` or `blocked`. It counts working leads, not resident sessions: a finished lead's session (a tmux window, a T3 thread) keeps its memory until the operator closes it, and a lead is counted only once its runner has started it. `scripts/memcap.sh N` measures what a box can carry: it runs N leads on the tmux runner against a mesh of its own, each spawning three executors, and prints the peak memory.

A runner may rename a lead's branch while it works (T3 does). Once a minute the daemon reads the current branch of the worktree of every local lead and engineer that is not `done` (`git rev-parse --abbrev-ref HEAD` in the node's `worktree`, when it exists on this host) and, when it is a branch that differs from the node's `branch`, rewrites the node and logs `branch: <path> <old> -> <new>`; a detached or missing worktree leaves the node as it is. `bmesh mesh-status`, the map and `bm-nodes` follow within the minute. The lead lands the branch its worktree is on (`git branch --show-current`), never the name in its brief.

## Proposal rounds

```
bmesh ideate <repo> [--model M] [--effort E] [--context <text|->]
```

A round is two threads on one ship repo, an engineer and a UI/UX engineer, each writing three to five ranked half-page specs into `docs/proposals/` and landing them on `main` as `proposed`. The round is a node `<repo>-round-<N>` (role `round`, `N` one past the highest round registered for the repo in `bm-nodes`) with children `<repo>-round-<N>.eng` and `.ux` (role `engineer`, `parent` the round), each with an inbox, a thread and a bmesh worktree; `bmesh mesh-status` lists the round with its children and the map draws it like a lead with executors.

`ideate` takes the repo's registry row (a path without a row is refused: the row carries `host`, `ux_host`, `verify` and `surfaces`) and refuses (exit 3) a row whose `mode` is not `ship`, a repo with five or more `proposed` specs on `main` (naming them: they wait on review), a host it cannot reach, and a `main` it cannot level with origin (`pull --ff-only origin main` when the main checkout is on `main`, else `fetch origin main:main`; never a reset or a stash). Then, from `main`, it makes `<[runner] worktrees>/<repo>/<repo>-round-<N>-eng` and `-ux` on branches of those names (a branch or a path that already exists is refused, untouched), renders the two briefs (the next free spec ID for the engineer, five past it for the UI/UX engineer, so the two never collide; the thread's UUID as `proposed_by`), registers the round (its `started` event opens it as `working`) and starts both threads through the runner on the row's `host` (the engineer) and `ux_host` (the UI/UX engineer; `""` = `host`, `""` = this box), with `--model` (default `[models] proposer`) at `--effort` (default `[models] proposer_effort`) and the title `[ideate] <repo> round <N>: engineer|UI/UX`. `--context` carries what the operator asked of the round (`-` reads it from stdin): both briefs get a section `## The operator's focus` with the words as given, trimmed, before their closing, and a repo's own template without `{{operator_context}}` gets the section appended; no words, no section. It prints `{"path","round","eng":{"path","thread","branch","worktree","host"},"ux":{...}}`. Briefs are kept under `<user cache dir>/bmesh/briefs/<round>-<child>.md`.

A host other than this box is reached as `ssh -o BatchMode=yes <host> bash -lc 'bmesh ideate <repo> --child eng|ux --round N --next-id N --thread-id UUID --model M [--effort E] [--context <text>]'`: the child form, which makes that one worktree, brief and node on the box it runs on (the registry is the same on every box) and prints its child's line. Both hosts are checked first (`bmesh help` over ssh), so an unreachable one is refused before anything starts. The child form refuses a child whose node is live or whose branch exists. Both children are started whatever happens to the other; one that does not start is marked `blocked` (`did not start: <error>`), the round reads `blocked` naming it (the started thread keeps running, and its pitch reaches the operator when the daemon resettles the round) and the command exits 1.

With the `t3` runner a round's thread is filed under the T3 project of `[runner] t3_project` (a registry name; `""` = the target repo) and runs in the bmesh worktree, so the target repo's T3 project gains no thread with no diff.

Each thread's node reports as any node does (`started`, `progress`, `ask`, `blocked`, `done`); the daemon polls and wakes an engineer like a lead, and an engineer's `done` needs only `--commit` (the commit on `main`) and carries its ranked list as `--text`. Once a minute the daemon settles every open round: both children `done` moves the round to `done` with the merged pitch as its text (the engineer's list, then the UI/UX engineer's, each as reported) and publishes `done` from the round; one child `blocked` moves it to `blocked` (`<child> blocked: <text>`, then the other's pitch when it is done). A blocked round is looked at again each minute: when the other child's pitch arrives the round says `blocked` again with it, and when a blocked child is unblocked and both finish `done`, the round is `done` after all. A settlement's event carries a `Nats-Msg-Id` made of the round, the state and its text, so two daemons settling the same round store and push it once. The round's `done` is pushed as `<repo> round N proposed` (priority default, tag `memo`, the pitch as body) and its `blocked` as `<repo> round N blocked` (high, `no_entry`), each opening the engineer's thread on a tap when `[console]` resolves it; a round's push, like a lead's, needs the event in `[notify] push`. An engineer's `ask` pushes as a lead's does (`<path> asks`, with its own thread's link); its `done` and `blocked` reach the operator through the round.

## Builds

```
bmesh build <repo> <NNN> [--model M] [--effort E] [--path P]
bmesh build --accepted <repo> [--model M] [--effort E]
bmesh scaffold <repo> [--check]
```

`build` dispatches one accepted spec. It takes the repo's registry row (a path without a row is refused, exit 2; a non-ship row exit 3), levels `main` as `ideate` does and reads the ledger there: the spec must read `accepted` (another status is refused, exit 3, naming it), and its `judge` names the model (`fable`, `opus`, `sonnet` as `claude-fable-5-1`, `claude-opus-5-5`, `claude-sonnet-5-5`; no judge is refused unless `--model` says), at `--effort` (default `[models] lead_effort`). The node is `<repo>-<NNN>`, or `--path`; a path whose node is not `done` or `blocked` is refused (exit 3, `use --path <repo>-<NNN>-2`), as is a host at `[runner] max_leads`, before anything is recorded. Then, in this order: `building` lands on `main` (`built_by` the thread id chosen for the lead, `host`, and `branch` when the runner names branches ahead, as `claude` and `tmux` do), committed as `Dispatch NNN: building on <host>` in a bmesh worktree, fast-forwarded and pushed as `choose-ideas` lands; the build brief is rendered (the spec file whole, the row's done rule and verify commands, the node path, the standard closing) to `<user cache dir>/bmesh/briefs/<path>.md`; the lead starts through the runner with that thread id, titled `[build] <repo> <NNN>: <title>`, exactly as `lead start` would. A runner that names the branch itself (`t3`) gets it recorded in a second commit, `Dispatch NNN: branch <b>`; a start that fails moves the spec back to `accepted` (`Dispatch NNN: did not start`) and exits 1. It prints `{"path","thread","branch","worktree","model","spec","building"}`, `building` the commit that recorded it. A model the runner rejects is the operator's to retry with `--model`; the binary has no fallback.

`--accepted` dispatches every accepted spec, lowest id first, through the same steps, and prints one line per spec: `started NNN <title>: <path> on <model> (thread, branch, building)`, `waits NNN <title>: on 004 (building)` for a spec whose body names a lower id (`004`, or a run `001 to 004`) whose spec is not yet `done`, `rejected` or `later` (an id above its own is a pointer, not a dependency; a rejected or later spec never reaches done, so it is not waited on), `waits NNN <title>: 3 leads working on <repo>` once three leads of the repo (on any host) are not `done` or `blocked`, counting the ones just started, or `not started NNN <title>: <the refusal>`; one spec's refusal does not stop the next. `--json` prints the lines as objects.

From a chat (a phone, a session with the plugin and no command typed), the operator runs this lifecycle through the plugin's operator skills, the chat entry points: `buildermesh:ideate` ("proposals for X", "run a round on X") runs `ideate` on the row's `host` (locally, else over ssh), with whatever the operator asked beyond the repo's name as `--context`; `buildermesh:choose-ideas` (the reply to a pitch in plain words) runs `choose-ideas`; `buildermesh:build` ("build X 3", "build what's accepted in X") runs `build` on the row's `host`, then watches the leads with `bmesh watch --nodes <paths>` and relays each `ask` to the operator, answering it with `bmesh --node operator send`; `buildermesh:scaffold` ("scaffold X") runs `scaffold --check`, shows the diff, runs `scaffold`, the row's verify, commits `Scaffold the shared definition of done and proposals` and pushes; `buildermesh:mesh-status` ("what's building", "where is X") summarises `mesh-status` (and `spec list` for a named repo) as working, asking, silent and done since the last look. `propose` and `build` carry the Fable fallback the binary has not: a Fable model the runner rejects is run once more with `--model claude-opus-5-5`, telling the operator what the runner said. Each skill ends with one line: what is started or landed, and what waits on the operator.

`scaffold` gives a ship repo what a build expects of it, from its row and the binary, and changes nothing else: the three sections of `AGENTS.md` between `<!-- command-center:done -->`, `<!-- command-center:proposals -->` and `<!-- command-center:mesh -->` markers, in that order (the definition of done from the row's verify commands and remote; the proposals convention; the Buildermesh paragraph of `docs/agents-snippet.md`, embedded, its verify placeholder filled from the row), only the text between the markers rewritten and a missing block appended at the end (a missing `AGENTS.md` is created with the blocks); `docs/proposals/README.md` (this repo's, embedded) and `docs/proposals/assets/.gitkeep` when missing; and a kind file for each of the row's `routines` the repo lacks, seeded from `examples/routines/` (`verify` inline and deterministic with its `command` the row's verify commands joined with ` && `; a row with no verify commands gets no verify kind and a line on stderr says so; a routine with no example is an error naming the file to write by hand). An existing kind is never touched. It prints `wrote <file>` per file, or `<repo> is scaffolded`. `--check` prints the unified diff of what would change and exits 1 when there is any, writing nothing. A row that is not `ship`, or a path without a row, exits 2. A second run changes nothing, so run it again after the row or the snippet changes; commit what it wrote.

### Briefs

The engineer, UI/UX engineer, build and half-pager templates are embedded in the binary (`internal/brief/templates/`); a repo overrides any of them with `.bmesh/briefs/<name>.md` (`engineer`, `ux-engineer`, `build`, `half-pager`). A template is filled in one pass: `{{name}}` placeholders take values and braces inside a value stay as written; a placeholder with no value is an error naming the template and the placeholder, never left in a brief. Every brief gets `repo`, `repo_path`, `verify` (the row's commands in backticks, or that none are listed), `surfaces` (the row's, or what to do without them), `standard_closing` (the closing paragraph with the verify commands inlined) and `done_rule` (the definition of done the row implies: verify passes, a model that did not write it reviewed it when it carries risk verify cannot catch (a rote change is done on verify alone), rebased, fast-forwarded to `main`, pushed to the row's `remote`); a round's brief also `node`, `round`, `round_n`, `worktree`, `branch`, `thread_id`, `next_id`, `date` and `operator_context` (the operator's focus section, or empty); a build brief `spec`, `spec_path`, `spec_id`, `node` and `date`. The proposal briefs carry the repo's facts and point at the `ideate-engineer` and `ideate-ux` skills for the method; the build brief carries the spec and points at the `lead` skill, and says that a review is only for risk verify can't catch (a rote change lands on verify alone), that its findings are fixed and then landed, the second pass only after a rebase changed the code, and a taste finding is noted in the final message, not re-reviewed. Briefs are kept under `<user cache dir>/bmesh/briefs/<node>.md`.

## The ledger

A repo's specs live in `docs/proposals/NNN-slug.md` (`[spec] glob`), each a flat `key: value` frontmatter between `---` fences (lines starting with `#` are ignored) over a half-page body. The frontmatter is the whole record, so a file that cannot be read is an error for every command, never skipped or counted around: a missing or non-numeric `id`, an `id` that does not match the file name's, a `status` outside `proposed`, `accepted`, `adjusting`, `later`, `rejected`, `building`, `blocked`, `done`, a `judge` outside `fable`, `opus`, `sonnet`, missing fences or a line that is not `key: value`; two files sharing an id. `README.md` is not a spec.

```
bmesh spec list [--repo R] [--status S] [--json]
bmesh spec next-id [--repo R]
bmesh spec set --id NNN --status S [--repo R] [--from A,B] [--field K=V]...
bmesh spec template [--repo R]
bmesh choose-ideas <repo> "<reply>"
```

A spec's `branch` is set by dispatch and corrected by the build thread when the runner renamed it: the `spec set` that writes `done` and `merged` adds `--field branch=<the branch landed>` when it differs, so the ledger names the branch that was merged.

`--repo` is a registry name or a path (default the current directory; a path that is a row's path counts as that row, as it does for every command taking a repo). `list` prints one row per spec, `NNN  status  size  kind  lens  title` (`?` for a missing field), sorted by id; `--json` prints each spec's frontmatter plus `path` as an array. `next-id` is one past the highest id (`001` for an empty ledger). `set` rewrites the spec's status and the given fields in place (an existing key keeps its line, a new one is added last, every other byte stays) and prints its row; `--from` refuses, changing nothing, a spec whose status is not one of the listed ones (exit 1, naming its status), as does a write the ledger would read back otherwise (a duplicated `status:` key) and a value with a line break. `template` prints the half-pager (the repo's `.bmesh/briefs/half-pager.md` when it has one, else the embedded one).

`review` is the operator's reply to a round, typed in plain words: `"1 and 3 yes, 2 smaller, 4 no, 5 later, 3 on opus"`. Clauses are split at commas, semicolons and line breaks, and, once a clause's phrase has begun, at a number that a spoken verdict follows (`3 yes and 4 no` and `11 smaller and 12 no` are two clauses each; `4 no because 5 covers it` is one). A clause is its leading numbers (joined by `and` or the like), which name specs, and the phrase after them. A number of three or more digits (`012`, as ids are written) names that spec; one of one or two digits is a position in the pitch when there is one (`1 and 3 yes` answering a pitch of 012 to 016 means its first and third items; `45 yes` to a five-item pitch is reported as no position, so the spec is `045`; `6` to a pitch of 001 to 003 and 006 to 010, both a pitched id and the sixth item, is reported as ambiguous), and a spec id when there is none. The pitch is the one the operator saw last, the text of the repo's newest settled round (`done` or `blocked`) on the mesh, read whole, line by line (a line is the id it starts with; one holding two or more items of the shape `NNN title (kind, size, judge):` after sentence ends, the title free of sentence ends (a `.;!?` before a space; `v1.2` is fine), is a list typed on one line and gives them all, so each child's pitch reads the same whether typed one per line or in one go, while an id cited inside a why is not an item), so a position means the same spec in every reply to it, whatever has moved since (a line whose spec has moved on is reported as left unchanged); an open round, even with a finished child, is not a pitch yet, and a settled round whose text names no spec (one blocked before a pitch) is passed over for an older one. With no mesh or no settled round with a pitch only ids count, and the output's first line says so. The phrase's first word decides: `yes`/`accept`/`ok` to `accepted`; `no`/`reject` to `rejected`, the rest of the phrase as `reason`; `later` to `later`; `on fable|opus|sonnet` alone sets `judge` and moves nothing; any other first word makes the whole phrase an instruction, so the spec becomes `adjusting` with it recorded as `Adjust (<date>): <phrase>` under its Proposal heading, for the thread that reworks the body. Reasons and instructions are kept as written, so a verdict word or a number inside one is just a word (`2 make it work with no network` adjusts 002). `on <judge>` right after `yes` or `later` sets the judge as well, and words after that (`yes but smaller`) make it an adjustment. A spec named in several clauses gets the last move and every judge. Only `proposed`, `later` and `adjusting` specs move; another status is reported and left alone. A clause with no number, no phrase, or a number that matches nothing is printed as `unmatched: ...`, never guessed at. The command levels `main` (as `ideate` does), makes a worktree `<[runner] worktrees>/<repo>/<repo>-review-<hex8>` from it, applies the verdicts there, commits `Review: 012 accepted, 013 later`, fast-forwards `main` (`merge --ff-only` in the main checkout when it is on `main`, else `fetch . <branch>:main`), pushes to origin, removes the worktree and its branch, and prints one line per spec (`NNN title: old -> new [on judge] [(note)]`), the unmatched items and `landed <sha8> on main: Review: ...` (or `nothing to land`). A `main` that will not fast-forward is refused (exit 3) with the review kept on its branch.

## Executors

A lead fans out by making executors: subagents in worktrees of their own, on branches under the lead's, each a node under the lead's path.

```
bmesh executor new --name N [--base REF] [--model M] [--json]
bmesh executor list [--json]
bmesh executor remove <name> [--force]
```

`new` runs as the lead (or as an executor fanning out again; the caller is the parent): it makes `<[runner] worktrees>/<repo>/<parent path>.<name>` with `git worktree add -b <parent's branch>-<name>` from the parent's HEAD (`--base` picks another commit; the branch is the worktree's current one, so a renamed branch is followed), registers node `<parent path>.<name>` (`role: executor`, `parent`, branch, worktree, repo, host, model) with an inbox and state `idle`, and prints `{"path","branch","worktree"}` followed, unless `--json`, by the executor header: the paragraph to paste at the top of the subagent's prompt, which names the path, worktree and branch, says to start every shell command with `cd <worktree> &&` and to carry `--node <path>` on every `bmesh` command (the harness keeps neither working directory nor environment between tool calls), to run `bmesh started` first and to invoke the `buildermesh:executor` skill. A name matches `^[a-z0-9][a-z0-9-]*$`; a second `new` with a name whose node is not `done` or `blocked`, or whose branch still exists, is refused (exit 3). When `git worktree add` fails (a hook, an unwritable root), nothing is registered and the worktree and branch are removed.

`list` prints the lead's tree (every node under the lead): path, state, branch, head (when the worktree is on this host) and unread count. `remove` refuses (exit 3) while the branch is not merged into the caller's HEAD unless `--force`; it then removes the worktree (`--force` also drops uncommitted changes) and the branch (`-d`, or `-D` with `--force`), forgets the worktree in the node, drains its inbox and marks it `done`. The node and its branch name stay in `bm-nodes` until `bmesh node forget` (see Finished nodes); the name can be used again.

An executor's `ask` is sent to its parent's inbox as a message `ask from <path>: <text>` as well as published as its own `ask` event, since only leads reach the operator; the digest lists an executor's ask, silence, block or completion nested under its lead's line and counts only leads under "asking". An executor's `done` needs `--commit` and never reads the spec.

### The skills

The repo is a Claude Code plugin, `buildermesh`, with nine skills. Five are the operator's chat entry points to the lifecycle, `ideate`, `choose-ideas`, `build`, `scaffold` and `mesh-status` (see Builds). Four are followed by the nodes the lifecycle starts: `lead` (own one spec, decide the shape, executors, checkpoints, merge, verify with one review pass, land, ask), `executor` (your worktree and branch only, checkpoints, routines, report, ask goes to the lead, never merge or push), `ideate-engineer` and `ideate-ux` (a round's method: what to read, the lenses and measurements, screenshots of the row's surfaces for the UI/UX engineer, three to five ranked half-pagers, landing on `main`, `done` with the ranked list, and the operator's reply through `bmesh choose-ideas`). Each names the optional pstack and method skills at fixed moments with a written default for a box without them (see Quickstart): the lead opens in `poteto-mode`, plans an L spec with `method:writing-plans`, decides the shape with `architect` and `attack-the-premise`, runs `figure-it-out` before an ask and `blast-radius` before landing; the executor builds with `tdd` and runs `figure-it-out` before an ask; the round's engineers run `exhaust-the-design-space` and `attack-the-premise` before ranking. Install it from the repo:

```
claude plugin marketplace add rickdiculousli/buildermesh && claude plugin install buildermesh@buildermesh
```

or from a checkout: `claude plugin marketplace add <path to the checkout> && claude plugin install buildermesh@buildermesh`. A lead's brief says "follow the `buildermesh:lead` skill" and adds the operator's own conventions; the executor header names the executor skill; a round's briefs name the ideate skills. The skills name no repo convention; `docs/agents-snippet.md` is the paragraph a repo's AGENTS.md gets, embedded in the binary for `bmesh scaffold` and kept in the repo for reading.

## Finished nodes

```
bmesh mesh-status [--all] [--json]
bmesh node forget <path> [--worktree]
```

`mesh-status` prints the nodes sorted by path: path, role, state, since, runner and unread count. A node `done` for longer than `[status] keep_done` (default `24h`; `0` shows them all) is left out, and a closing line counts what was: `… N finished nodes older than 24h (--all shows them)`. A `blocked` node is never left out: it waits on the operator. A round and its children are one unit, left out only when every one of them is. `--all` lists every node; `--json` carries every node with no flag, as the map's `/api/nodes` does, as an array of `{"node","state","runner","unread"}`.

A routine instance's node leaves the mesh by itself. A fresh run's node, state and runner sample are deleted once its run ends and its checkout is removed; its `started`, `progress` and `done` events stay in the stream for the map, and its reply's `by` still names it. A pooled instance that stops (its pool restarted or shrunk, its kind gone from its repo's main, its repo gone from the registry) is marked `done`; the daemon's kind rescan, once a minute, deletes the node, state and runner sample of every routine instance on its host that has been `done` for `[status] keep_done` (`0`: never) and is not running, a fresh run's left behind included.

Every other node stays until it is forgotten. `node forget` deletes a `done` or `blocked` node: its `bm-nodes` entry, state, runner sample and inbox consumer, printing `forgot <path>`; its events stay in the stream, and the nodes under it are not forgotten with it. Any other state is refused (exit 3, `refused: <path> is <state>; only a done or blocked node is forgotten`), as is a node with a node under it (a lead's executors, a round's children) that is not `done` or `blocked` (`refused: <child> under <path> is <state>`). Without `--worktree` nothing on disk is touched, and a node with a worktree prints `worktree <path> kept`. `--worktree` first removes the node's worktree (`git worktree remove` in its repo; one already gone from disk is pruned) and deletes its branch, printing `removed worktree <path>` and `deleted branch <b>`; it is refused (exit 3, naming it, nothing touched) when the worktree is on another host (`run it there`), is dirty (`git status --porcelain` prints anything) or the branch is not merged into `main` (an executor's: into its parent's branch). The merged branch is deleted with `-D`, since `-d` would also want it merged into its upstream, which a rebase before landing leaves behind.

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
| `command` | `""` | run through the platform shell in the checkout at the commit; a job's args are appended as positional arguments (`"$@"` under sh) |
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
- `review.toml`: `mode = "pooled"`, `models = ["claude-opus-5-5"]` (every change is reviewed on Opus, in a session that never saw it), `allow = ["Bash(git diff:*)", "Bash(git log:*)"]` and a prompt that reviews `git diff <base>..HEAD` (the job text carries `base: <sha>`, e.g. `--text "base: $(git merge-base origin/main HEAD)"`) and asks for findings as `### <severity>: <what breaks>` sections ending in `VERDICT: ok` or `VERDICT: fail`.
- `surface.toml`: `mode = "fresh"`, `deterministic = true`, `slots = ["browser"]`, `command = "npx playwright screenshot"`; the job args `<url> <file>` are appended as `"$@"`.

### Modes

| Mode | Runs | Where |
|---|---|---|
| `inline` | `bmesh routine run` in the requester's own process; no daemon | the requester's worktree, at HEAD |
| `fresh` | the daemon starts a new instance per job, `routine.<kind>.f<id8>`, and removes its checkout and node afterwards | a checkout of its own |
| `pooled` | the daemon keeps `pool` instances per model, `routine.<kind>.<model-short>-<n>` (`cmd-<n>` for a command), that drain the kind's inbox in batches | one checkout per instance, kept |

`id8` is the job id's first 8 characters, lowercased. `model-short` is the model id lowercased with a leading `claude-` stripped, every character outside `[a-z0-9]` made `-`, runs of `-` collapsed to one and leading and trailing `-` trimmed (`claude-opus-5-5` is `opus-5-5`, `claude-opus-5-5[1m]` is `opus-5-5-1m`). Pooled instances are numbered from 1 per model across every repo that defines the kind.

A pooled instance holds the jobs it takes until `batch_idle` passes with nothing new, or a go arrives (`bmesh routine go <kind>`, or a job queued with `--go`), then runs them in order and runs any job queued meanwhile at once. A model instance keeps one session across jobs (`--session-id` on its first job, `--resume` after) and starts a new one when `recycle_uses`, `recycle_context` or `recycle_idle` is reached, or when the kind's definition at a job's commit differs from the one its session started with. A run whose output does not parse as claude's JSON result (including a non-zero exit with no JSON), first or resumed, drops its session whatever the recycle settings: the next job starts a new one with the prompt.

Instances are nodes (`role: routine`, with repo, host and model). The daemon writes their state (`working` during a job, `idle` between, `done` when it stops: a fresh run after its job, a pooled one when its pool is stopped or restarted; see Finished nodes for when their nodes leave) and publishes `started`, `progress` (`job <id> started` before a run and `job <id> ok|fail <ms>` after it, `job <id> ok|fail <ms> ctx <tokens>` for a model instance (its context tokens), both with `to` = the requester's path; or `recycled`) and `done` for them on `bm.<instance path>.<event>`. A node event with a `to` counts as activity of that path too, so a lead waiting on a routine is not marked silent.

The daemon reads each registered repo's (repos.toml) kinds every minute from its main (`origin/main` after a fetch when the repo has an `origin`, else `main`) and starts, restarts or stops pools to match. Routines are off when `[runner] checkouts` is empty.

### Jobs

```
bmesh routine list [--json]
bmesh routine status [<kind>] [--json]
bmesh routine queue <kind> [--commit C] [--model M] [--go] [--rerun] [--again] [--text T] [-- args...]
bmesh routine go <kind>
bmesh routine wait <job id> [--timeout D]
bmesh routine run <kind> [--commit HEAD] [--model M] [--here] [--rerun] [--text T] [-- args...]
```

`list` prints the cwd repo's kinds with their instances and queue depth (waiting plus in-flight jobs); `status` prints a kind's (or every kind's) instances with state and model.

A job is an inbox message (`kind: "job"`) to `routine.<kind>`, with the job id as its `Nats-Msg-Id`, so a job queued twice is stored once (`queue` prints `duplicate <id>`). It names a commit (`--commit`, default `HEAD`, resolved in the cwd repo to a full SHA with its tree) and a repo: the requester node's `repo` when it is a registered node naming one; else the registry name whose path shares the cwd's git common dir (so a linked worktree, where leads live, names its main repo); else the main repo directory's base name. The daemon resolves it through its own registry. The kind's inbox consumer is `inbox-routine--<kind>`, with `MaxAckPending` = the kind's instance count (`pool` per model per repo for pooled, `pool` for fresh) and `AckWait` = the kind's `timeout` + 1m. The daemon keeps a running job in progress, so a job is redelivered only when its daemon dies.

The daemon runs a job with the kind as defined at the job's commit (`git show <commit>:.bmesh/routines/<kind>.toml`), falling back to main's definition only when the file is absent at that commit. A deterministic kind's job is answered from its recorded result, ok or failed, without a run unless `--rerun`; `queue` checks the record first and prints `answered from result <key> (job <id>)`. A job whose commit is not in the daemon's clone (queued from another box) gets one `git fetch origin` before it is answered `not a commit`, so push the branch before queueing to a daemon elsewhere. A job whose slot is busy goes back to the inbox for 30 s. A review is one pass per branch: `queue review` for a worktree whose branch already has a `review` result (one recorded for a commit that is an ancestor of HEAD but not of `main`, `origin/main` when the repo has an origin, so a landed spec's review in the history does not count) is refused (exit 3) naming that result, its verdict, commit and time, and queues only with `--again`, which is for code a rebase changed. An unknown repo, a kind that a configured repo defines but not the job's repo's main, an inline kind, an unknown model and a lost lease are answered `ok=false` with that reason. A kind that no configured repo defines on its main has no feed: its jobs wait unanswered until `routine wait` times out.

A reply is an inbox message (`kind: "reply"`) to the requester carrying `{"job","kind","tree","ok","output","result_key","by"}`; the output is cut at 64 KiB with a trailing `[truncated]` line. `wait` reads only that reply from this node's inbox, leaving other messages unread, prints its output and exits 0 when ok, 1 when failed, 5 on `--timeout` (default 20m). `bmesh inbox` acks everything it reads, replies included, and prints a reply whole (`reply to job <id>: <kind> ok|fail` and its output); a `wait` for a job whose reply was read that way waits until its timeout, so wait right after queueing.

`queue`, `go` and `wait` need a node identity (`--node`, `BMESH_NODE` or the registered worktree): the reply is addressed to it. `run`, `lock-lease` and `result record` use the node when one resolves and otherwise act as the host name; `list`, `status` and `result find` need none.

### Checkouts

Fresh and pooled runs never touch a requester's worktree. Each instance has a detached worktree of the registered repo's path at `<[runner] checkouts>/<repo directory name>/<instance path>`, moved before each job with `git checkout --detach <commit> && git clean -fd`; the daemon reads the repo's main in a sibling checkout named `main`.

`routine run` is the inline run: it runs HEAD of the cwd worktree (`--commit` must name HEAD) and refuses a fresh or pooled kind unless `--here`. Every inline run records a result for HEAD's tree, so it refuses a dirty tree: exit 3 `bmesh: worktree is dirty; commit first`.

### Leases

```
bmesh lock-lease acquire <slot> [--ttl D]
bmesh lock-lease renew <key> [--ttl D]
bmesh lock-lease release <key>
bmesh lock-lease run <slot> [--ttl D] -- <cmd...>
```

A slot is a resource only so many runs may use at once. Its capacity is in the repo's `.bmesh/config.toml` (default 1):

```toml
[slots]
browser = 1
[routines]
dir = ".bmesh/routines"
```

Leases live in KV `bm-leases`, key `<slot>.<n>` (`n` below the capacity), value `{"holder","expires_at"}`; an expired entry is free. `--ttl` defaults to 10m and is at least 3 s. `acquire` prints the key (exit 3 when the slot is busy); `renew` and `release` refuse a lease held by someone else (exit 3). A holder renews every ttl/3; when a renew fails the lease is lost: the run is killed and the job answered `ok=false` with `lease lost` (nothing is recorded; `lock-lease run` exits 1). `lock-lease run` runs the command in its own process group, kills the group and releases on SIGINT or SIGTERM, and exits with the command's code. Kinds take leases on their `slots` for `timeout` + 1m (inline runs and the daemon alike).

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

A result older than `[routines] keep_results` (default `30d`, the stream's age; `0` keeps them forever) is gone: `bmesh serve` provisions `bm-results` with that age. A deterministic kind then runs again, and `bmesh done` for a spec landed longer ago than that needs its routines run again on the merged tree (it names the missing result); the spec's `verified` line is the record that stays.

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
| KV | `bm-results` | `<kind>.<tree>[.<args8>][.<model-short>]` to Result (`[routines] keep_results`, default 30 days) |
| KV | `bm-leases` | `<slot>.<n>` to `{"holder","expires_at"}` |

JetStream is addressed through the domain from `[mesh] domain` (default `hub`).

## Config

Three files in the config dir (`<os.UserConfigDir()>/bmesh`: `~/.config/bmesh` on Linux, and anywhere that directory already exists; `BMESH_CONFIG` overrides the config file's path, and the other two live beside it): `config.toml`, this box's settings; `repos.toml`, the registry of repos, the same on every box; and the setup flag `.setup-complete`, written by `bmesh init` (see Quickstart).

### repos.toml

One `[repos.<name>]` table per repo (a name matches `^[A-Za-z0-9][A-Za-z0-9_.-]*$`: a directory name, so a checkout keeps its name); every key with its default:

```toml
[repos.party-buzzer]
path = "~/repos/party-buzzer"           # required; the checkout on this box (~ expands; a relative path is from the config dir)
mode = "ship"                           # ship | companion | publish | fork | dormant
remote = ""                             # owner/repo, or a URL; for people and the table
host = ""                               # the box proposal threads run on ("" = this one)
ux_host = ""                            # the box UI/UX threads run on
verify = []                             # the shell commands that must pass before a change is done
surfaces = []                           # the UI screens a UI/UX thread screenshots, with their viewports
pin = ""                                # a release tag every checkout stays on, instead of following a branch
routines = ["verify", "review"]         # the routine kinds a build runs
```

An unknown key, a row without `path` or a mode outside the five is an error for every command, since one loader reads it for all of them. `[repos]` in `config.toml`, where the registry used to be, is refused with a message naming `repos.toml`. `bmesh lead start --repo`, `routine queue` and the daemon resolve repo names through it.

```
bmesh repos [--json]               # the table: name, mode, remote, verify, host, ux_host, surfaces, pin (sorted by name)
bmesh repos show <name> [--json]   # one row, key: value lines
```

`--json` prints the rows as an array (`show`: one object) of `{"name","path","mode","remote","host","ux_host","verify":[],"surfaces":[],"pin","routines":[]}`, defaults filled in, for other tools to read. `show` of an unknown name exits 3. `bmesh init` adds the current directory's checkout as a row (`path` and its GitHub origin as `owner/repo`) unless it is there already, and rewrites the file whole, so a comment in it is lost.

### config.toml

Keys and defaults:

```toml
[mesh]
nats = ""                                   # "" = embedded server; else e.g. "nats://localhost:4222"
domain = "hub"
token_file = ""                             # "" = no token
store = "<cache dir>/bmesh/jetstream"       # embedded server's store (os.UserCacheDir: ~/.cache on Linux, ~/Library/Caches on macOS); an existing ~/.local/state/bmesh/jetstream is kept
listen = "127.0.0.1:4222"                   # embedded server's bind
[host]
name = ""                                   # "" = `hostname -s`
[serve]
listen = "127.0.0.1:3774"
[runner]
kind = "t3"                                 # claude | t3 | tmux (init writes claude when claude is on PATH)
t3_dispatch = "t3_dispatch.py"              # path; run as `uv run <path>`
t3_project = ""                             # registry name of the repo whose T3 project a round's threads open in; "" = the target repo
tmux_session = "bmesh"
worktrees = "~/.bmesh/worktrees"            # executors' worktrees, and the claude and tmux runners'
checkouts = "~/.bmesh/checkouts"            # routine instances' checkouts; "" = routines off
claude = "claude"                           # claude command for model routines and the claude runner, split on spaces
max_leads = 0                               # leads at once on this host (lead start refuses past it); 0 = no cap
[notify]
ntfy = ""                                   # "" = no digest; else "http://k16:2586/bmesh"
command = ""                                # test hook: receives the POST body on stdin instead of HTTP
digest_every = "1h"                         # command runs through the platform shell, body on stdin, title in $BMESH_TITLE (and $NTFY_TITLE)
push = ["ask", "blocked", "done"]           # a lead's (and a round's) events pushed at once (only these three; another entry is a config error); [] = no pushes
map_url = ""                                # the map's URL for a push's click-through (<map_url>#/lead/<path>); "" = none
catch_up = "1h"                             # on start, push missed events no older than this
[liveness]
grace = "20m"
poll_every = "60s"
wake_every = "10m"
[spec]
glob = "docs/proposals/{id}-*.md"
[models]
lead = "claude-opus-5-5"                    # lead start's model when --model names none (build passes the spec's judge)
lead_effort = "high"                        # a lead's effort: low, medium, high, xhigh or max; "" = the runner's default
proposer = "claude-opus-5-5"                # a proposal round's two threads
proposer_effort = "xhigh"                   # their effort
[status]
keep_done = "24h"                           # mesh-status leaves out nodes done longer than this, and the daemon deletes stopped routine instances' nodes after it; "0" = never. Durations take whole days too: "30d"
[routines]
keep_results = "30d"                        # bm-results' age limit, applied by serve; an older result is gone; "0" = none
[quota]
command = ""                                # the map's quota strip; "" = no strip. Run through the platform shell at most every 5m (30s timeout); prints a JSON array of {"reading":{"provider","five_hour","week","five_reset","week_reset"}}
[console]
thread_url = ""                             # the map's thread link for leads on this host, {thread} = the lead's runner thread; "" = this host's [console.hosts] entry, else no link
[console.hosts]                             # host = template, for leads on other hosts (e.g. k16 = "http://k16:3773/…/{thread}")
```

A digest is also published as a `digest` event on `bm.daemon.<host>.digest`, with the title as `text` and the body as `body`.

### Pushes

A daemon with `[notify] ntfy` or `command` set also pushes a lead's `ask`, `blocked` and `done` (the `push` list) to the same topic at once: title `<path> asks`, `<path> is blocked` or `<path> is done`; priority `high` for an ask or a block and `default` for done; tags `question`, `no_entry`, `white_check_mark`; the body is the event's text cut to 3 KB, plus `commit <sha8>` for done; a tap (`Click`) opens the lead's thread when `[console]` resolves it, with an `Open map` action for its map page when `map_url` is set; with no thread the tap opens the map page. The phone must reach both links, so on a tailnet `map_url` and the `[console]` templates use the full MagicDNS name (`http://<host>.<tailnet>.ts.net:3773/…`), not the short host name. A round's `done` and `blocked`, and an engineer's `ask`, push as Proposal rounds says. Executors' and routines' events never push. With `command` set, the body goes on stdin and the headers in `$NTFY_TITLE`, `$NTFY_PRIORITY`, `$NTFY_TAGS`, `$NTFY_CLICK` and `$NTFY_ACTIONS`.

Each event is pushed once across restarts and daemons: before posting, a daemon claims it by publishing `bm.daemon.<host>.push` (`to` the lead, `text` the title, `seq` the event's stream sequence) with `Nats-Msg-Id` `push-<seq>`, and a duplicate reply means another daemon has it; on start a daemon pushes the listed events no older than `catch_up` (by the event's `at`, else its stored time) that have no `push` claim yet, in order, then follows live. A live event stored over a minute ago (the backlog a reconnect to the hub delivers) gets the same rules: it is dropped when older than `catch_up` or when any daemon's `push` claim names its seq. A push that still fails after retries at 5 s and 30 s is logged and recorded as `push-failed`; the claim stands, so it is never repeated. At shutdown the push being posted gets 10 s to finish; any claimed push still queued or waiting to retry is logged and recorded as `push-failed` with body `shutdown`.

The map's lead page reads the lead's spec from its registered repo's local `origin/main` (`main` without an origin) and never fetches in the request; the daemon fetches `origin main` in the background at most once a minute per repo while lead pages are being viewed, so a spec shows up to a minute or two after it lands.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | ok |
| 1 | error; a failed run (`routine run`, `routine wait`); a lost lease; an interrupted `lock-lease run`; a malformed spec, or a `spec set` its `--from` refuses |
| 2 | usage; a `scaffold` of a repo without a row or whose row is not `ship` |
| 3 | refused (a command other than `init` and `help` on a box not set up, run outside a terminal; a `repos show` of an unknown name; a `done` the spec or its `verified` line doesn't support, a reserved or taken path, a send to a running thread, an executor name or branch in use, an `executor remove` of an unmerged branch, a dirty worktree for a deterministic inline run, an unknown kind, a fresh or pooled kind run inline without `--here`, an inline run of a commit other than HEAD, a busy slot, a lease held by someone else, no result for `result find` or `spec verified`; an `ideate` on a non-ship row, with five proposed specs, an unreachable host, a live child or an existing branch; a `main` that `ideate`, `build` or `choose-ideas` cannot level with origin or fast-forward; a `build` of a non-ship row, of a spec that is not `accepted`, with no judge and no `--model`, of a taken path or on a host at its lead cap; a second `review` job for a reviewed branch without `--again`; a `node forget` of an unknown node, of one not `done` or `blocked`, of one with a node under it that is neither, or with `--worktree` of a worktree on another host, dirty or with its branch unmerged) |
| 5 | timeout (`watch`, `routine wait`) |

`lock-lease run` exits with its command's code.

## License

MIT.
