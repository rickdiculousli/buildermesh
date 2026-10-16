---
id: 032
title: The Quickstart works as written (GitHub issues 1, 2 and 3)
kind: refinement
size: M
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
built_by: e1bab2af-74ec-4b51-b73b-0ffc430a981f
host: k16
family: buildermesh-build-3
branch: bmesh/e1bab2af-74ec-4b51-b73b-0ffc430a981f
merged: d842de47eac0eee9a749d050d5554caedd4a3b72
verified: verify ok t:bff0a401
---

## Opportunity
The first person outside the operator's fleet to set up buildermesh (gobucha, Windows 11, single box, `claude` runner, `go install …@latest` and the marketplace plugin) filed three issues on 2026-10-16. Each one stops the Quickstart:

- **#1.1, the single box can't reach its own hub.** `bmesh init` (single) writes `[mesh] nats = ""`. `serve` embeds a hub only when it is empty, and `connect` (`cmd/bmesh/main.go:375`) refuses when it is empty: `bmesh: [mesh] nats is empty; the CLI needs a hub URL`. With one config file, either `serve` works or the CLI does, never both. This isn't Windows-specific. Our boxes never hit it because both are multi-box.
- **#1.2, `main` is assumed.** 58 non-test lines in `cmd/` and `internal/` name `main` or `origin/main`. `init` registers a repo whose default branch is `master` without a word, then the daemon logs `routines: read kinds … "origin/main": not a commit` once a minute, indefinitely.
- **#2, three skills don't load.** The `description:` lines of `ideate`, `ideate-engineer` and `ideate-ux` are plain YAML scalars containing `: ` (`"ideate this idea: ..."`, `"Engineer: proposal round"`, `"UI/UX engineer: proposal round"`). That is invalid YAML, so a strict loader skips the skill silently. The operator-facing round is then missing from the plugin.
- **#3, a registry name with a capital can't ideate or build.** `config.NamePattern` allows `A-Z`, `_` and `.` on purpose, but `roundPath`, the round branch, `nextRound`, `latestRanks` and `leadPath` build node paths from `row.Name`, and `mesh.ParsePath` accepts only `[a-z0-9-]`. `nextFamily` handles `.` and nothing else. The issue carries a patch with a `mesh.RepoToken` helper and a test.

## Proposal
In:
- **Single-box dial.** When `[mesh] nats` is empty, `connect` dials `nats://<[mesh] listen>` (default `127.0.0.1:4222`), the hub `serve` embeds, with the same token handling. When nothing is listening there, it fails with `no hub at <addr>: is bmesh serve running?`. README Quickstart and Config say so.
- **The default branch, checked.** `init` looks for `main` on the repo's `origin`, or locally when there's no origin. A repo with no `main` is refused (exit 3): `<repo> has no main branch; bmesh builds on main (rename the branch, or see #1)`. The daemon logs a kind-read failure once per repo and commit, not once a minute. A `branch` key per repo is out: it touches all 58 sites and the briefs, and it is its own spec if anyone needs it.
- **Skills parse.** The three descriptions become `>-` block scalars. A new test parses every `skills/*/SKILL.md` frontmatter with a strict YAML decoder and checks for `name` and `description`, so verify catches the next one. Plugin version bump.
- **Repo tokens.** Take #3's `mesh.RepoToken` (lower case, `.` and `_` written as `-`) and use it at every site the issue lists and any the lead finds (`grep -n 'row.Name\|repo string' cmd/bmesh` for paths, subjects and branches). `node.Repo` and worktree directories keep the registry name. Two registry names that map to one token (`Foo` and `foo`) are refused at registration.
- **Init says what it set.** Before `Next:`, `init` prints `What this box is set to:` followed by one line per setting in effect, read back from the loaded config, so defaults it didn't write show too. Each line gives the value, a plain-words explanation for someone new to buildermesh, and the key that changes it. The settings are the setup (single box or several), the hub, the runner, the registered repo and its path, where worktrees go, the map's address, the models for proposers, leads and executors, the update notice (033), the service, and whether this box runs routines (`routines: on. This box runs any pushed commit's routine commands for every box on the mesh ([runner] checkouts)`, as 036 has the README say). Examples:
  - `hub: embedded on 127.0.0.1:4222. bmesh serve runs the message hub itself; nothing to install ([mesh] nats, listen)`
  - `runner: claude. Each lead, executor and round is a claude session started for you ([runner] kind)`
  - `map: http://127.0.0.1:3774/. A page showing every running node; open it while bmesh serve runs ([serve] listen)`

  On an existing config (`init` without `--force`), it prints the same block for what is already there. `config.toml` carries the same explanation as a comment beside each key it writes. README Quickstart shows the block.
- The landing commit message carries `Fixes #2, Fixes #3` and `Refs #1`. When it lands, the lead comments on #1 with what landed, and says the Windows service is not part of this spec.

Out: a Windows service in `init --service` (Task Scheduler at logon) and a Windows restart after self-update. Round two's decision 10 keeps tested Windows support for its own spec. 033 makes self-update leave Windows boxes alone.

Judge: opus, because the dial change sits under every command and the token must be applied at every site where a name becomes a path, not just the ones the issue lists.

## What you'll see
A fresh single box runs the four Quickstart commands, `init` ends by listing every setting it chose and what each one means, and `bmesh mesh-status` answers. `bmesh init` in a `master` repo says why it refuses it. All nine skills show in Claude Code. `bmesh ideate MapleMarketScanner` opens `maplemarketscanner-round-1`. Issues #2 and #3 close on the push.

## Verification
The repo's verify. New tests:
- the CLI with `nats = ""` dials an embedded hub started on `[mesh] listen` (meshtest), and reports the "no hub" line when none is running;
- `init` refuses a repo with only `master`;
- every SKILL.md frontmatter parses strictly (and the test fails on the old `ideate` line);
- `RepoToken` and an `ideate`/`build` dry path for a capitalised name (round, branch, lead and family paths all parse);
- a token collision is refused;
- `init`'s settings block, from a fresh run and from an existing config, names every listed setting with its key, and its values match the loaded config.

## Risks and the alternative considered
Defaulting the dial to `listen` points a multi-box config with an empty `nats` at localhost. That config is already broken today, and the "no hub" line names the cause. The alternative for #1.1 was having `init` write `nats = "nats://127.0.0.1:4222"` and having `serve` embed when the URL is local. That changes what `nats` means and breaks configs already written. Dialing `listen` is the smaller change.

## Needs from the operator
Agreed on 2026-10-16: the lead pushes `Fixes #2, Fixes #3` and comments on #1.
