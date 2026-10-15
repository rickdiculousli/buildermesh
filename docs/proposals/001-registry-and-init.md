---
id: 001
title: Registry, init and the claude runner
kind: feature
size: L
judge: fable
status: accepted
lens: eng
proposed_by: da6488fd-38bd-4c4c-994c-a7de257d9424
proposed_on: 2026-10-15
---

## Opportunity
Round two decisions 2, 9 and 10 in `~/repos/command-center/docs/superpowers/specs/2026-10-15-buildermesh-round2.md`. `bmesh` can't be started by anyone without command-center's registry, a NATS hub someone set up and T3 or tmux. The per-repo facts live in command-center's `projects.md`; `config.toml`'s `[repos]` is a second, partial copy.

## Proposal
In:
- `~/.config/bmesh/repos.toml` (OS config dir on other platforms): `[repos.<name>]` with `path` (required), `mode` (ship | companion | publish | fork | dormant; default ship), `remote`, `host`, `ux_host`, `verify` (list of shell commands), `surfaces` (list), `pin`, `routines` (default `["verify","review"]`). One loader in `internal/config` used by every command; `config.toml [repos]` is refused with a message naming repos.toml.
- `bmesh repos [--json]`: the table (name, mode, remote, verify, host, ux_host, surfaces, pin), rows as JSON with `--json`. `bmesh repos show <name> [--json]` one row.
- `bmesh init [--service]`: guided. Checks the setup flag (`<config dir>/.setup-complete`); asks single box or multi-box (multi: the hub URL and token file); writes `config.toml` with the embedded hub as the single-box default; detects runners on PATH (`claude`, `t3`, `tmux`) and picks `claude` when present; registers the current directory's repo in `repos.toml` when it is a git checkout; `--service` writes the systemd user unit (Linux) or launchd plist (macOS) for `bmesh serve` and says how to enable it; writes the flag; prints the next commands. Every other command, finding no flag, prints one line and runs init (non-interactive `-p` runs refuse instead). `init` on an existing config edits nothing without `--force`.
- The `claude` runner (`[runner] kind = "claude"`): starts a lead as `claude -p --output-format json` in a worktree it makes under `[runner] worktrees` from the repo's main, with the brief on stdin; the session id is the thread id; `send` resumes with `--resume <id>` and queues when a turn runs; state from the process; the same `BMESH_CLAUDE_COMMAND` fake drives its tests.
- Cross-platform: no `sh -c`, no tmux assumption, config and state dirs through `os.UserConfigDir`/`os.UserCacheDir`, and a `GOOS=windows go build ./...` step in CI.
- README: the first section becomes a quickstart (`go install`, `bmesh init`, `bmesh serve`, first command), the config section documents repos.toml and the setup flag, nothing of any particular fleet.
- command-center's `repo_sync.py` and `prune_worktrees.py` read the registry through `bmesh repos --json` instead of `projects.py` (projects.py stays until spec 005 deletes it); `fleet/bmesh/repos.toml` added with every repo from `projects.md`, installed on both boxes.

Out: `propose`, `build`, `align` (002, 003); Windows testing and release binaries (a later spec); a `codex` runner.

Judge: fable, because the init dialogue and the registry defaults are taste the spec can't pin.

## What you'll see
A fresh machine with Go and Claude Code runs four commands and has a working single-box mesh; `bmesh repos` prints the table `projects.md` used to hold; both boxes have `repos.toml` and `repo_sync` still syncs every repo.

## Verification
The repo's verify (routine `verify`); new tests for the loader, `repos`, `init` (driven non-interactively through flags or stdin), and the claude runner against a fake; `uv run -m unittest discover -s tests -t .` in command-center; `repo_sync.py --all --dry-run` from the Mac reports every repo as before.

## Risks and the alternative considered
Keeping `[repos]` in config.toml: rejected, the shared and the per-box parts change for different reasons. The claude runner's `send` can't steer a running turn: documented, queued.

## Needs from Rick
None.
