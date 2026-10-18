---
id: 083
title: Agent threads start with only the plugins and settings the box names for them, in a worktree bmesh makes before the thread starts
kind: token-cost
size: M
judge: opus
status: rejected
lens: eng
proposed_by: a234110b-dade-4827-b449-632e5de4650b
proposed_on: 2026-10-18
reason: config hell: a per-box, per-repo thread profile plus moving T3 lead worktrees outweighs ~5% of a lead's cost; trim the box's own Claude Code settings instead
---

## Opportunity
This builds on 082, which trims the model routines. This spec does the same for agent threads.

Claude Code records each request a session sends in its transcript (`api-request-shape` and `api-request-blob`). Across 49 lead transcripts on k16 (specs 017 to 071), the first request averages 101 KB, which is 36.6k tokens at the median. Every one of a lead's ~64 calls re-reads it, which makes it 24% of a lead's context reads. Round threads start at 90 KB, and that start is 36% of their reads.

The first request breaks down as:
- **Built-in tool schemas: 26.9 KB.**
- **The skill listing: 22.2 KB.**
- **The system prompt: 15.9 KB.** This is the transcript's `prompt_snapshot`, recorded once. It is not an extra copy.
- **The brief: 14.3 KB.**
- **Instructions (CLAUDE.md, AGENTS.md, MEMORY.md): 9.1 KB.**
- **MCP tool names and connection notes: 4.4 KB.**
- **pstack's SessionStart hook: 2.1 KB.**
- **The agent listing: 2.1 KB.**

Those 49 leads invoked `buildermesh:*` skills 55 times and `method:writing-plans` 5 times, and nothing else. They never used:
- **The claude.ai-synced `anthropic-skills`** (docx, pptx, browsers, innerwork and the rest): 10.7 KB of the listing.
- **Claude Code's bundled skills** (dataviz, claude-api, schedule and others): 6.4 KB.
- **pstack.** The lead skill's pstack steps never ran: `poteto-mode`, `architect` and `blast-radius` appear in none of the 49 listings. pstack's hook still adds 2.1 KB.
- **ghidra's ~80 MCP tools.**
- **Three built-in tools:** ScheduleWakeup (2 calls), ReportFindings (0) and ListAgents (0), 8.3 KB of schemas.

Nothing bmesh controls narrows this:
- **T3's launch takes no plugin options.** `t3_thread_launch` takes a project, model, runtime mode, workspace and message, and T3 runs `claude` with no `--setting-sources` and no `--strict-mcp-config`. So a thread loads everything the box's user settings enable.
- **Every runner's claude does read the worktree's `.claude/settings.local.json`.** That is local scope, which wins over user scope, and it is the file `claude plugin disable --scope local` writes.
- **T3 leads have no worktree before launch.** T3 makes a lead's worktree during the launch (`internal/runner/t3v2.go:375`), and the first turn follows straight after, so bmesh has nowhere to put that file in time. Rounds already run in a worktree bmesh makes (`existing_worktree`), and so do claude- and tmux-runner leads.

## Proposal
In:
- **`[threads]` in the box's `config.toml`.** It is unset by default, which keeps today's behaviour.
  ```toml
  [threads]          # what agent threads bmesh starts here load: leads, fix leads, round threads (an executor shares its lead's session)
  plugins = []       # plugins they keep, as `claude plugin list` names them; [] = every enabled plugin, as before
  settings = ""      # a Claude Code settings file laid over that (skill overrides, deny rules); "" = none
  ```
  The plugin that carries bmesh's own skills (named `buildermesh`) is always kept.
- **The profile.** When a thread starts, bmesh runs `<[runner] claude> plugin list --json`, sets `enabledPlugins: {"<id>": false}` for every enabled plugin it doesn't keep, lays the `settings` file over that, and writes the result to `<worktree>/.claude/settings.local.json`.
  - **Kept out of git.** bmesh adds `/.claude/settings.local.json` to the repo's `info/exclude` when git doesn't already ignore it, so `git status` stays clean and a lead's `git add -A` never commits it.
  - **Never in the way.** A worktree that tracks that file gets no profile, with a log line. If the plugin list fails, only the settings file's part is written, with a log line. A profile never refuses a start.
- **Every agent thread's worktree exists before its runner starts.** The T3 runner makes a lead's worktree itself, as the claude and tmux runners do (`worktreeFor`), at `<[runner] worktrees>/<repo>/<thread>` on the same branch as today (`bmesh/<thread>`). It launches with `existing_worktree`, as rounds do, and removes the worktree and branch if the launch fails. Each runner writes the profile after the worktree exists and before the agent's first turn, for leads, fix leads and rounds alike.
- **`bmesh config check`** prints `threads: keeps <k> of <n> plugins (off: <ids>)[, settings <path>]`, and refuses a settings file that isn't a JSON object.
- **Docs.** The manual covers the `[threads]` keys, with a fleet-neutral example profile; the runners' worktree lines (a T3 lead's worktree moves from T3's home to `[runner] worktrees`); and `config check`. Internals says where and when the profile is written.

Out:
- Profiles per role. One profile covers every agent thread.
- Model routines (082).
- The brief and the skills. The brief and the lead skill restate landing and review in different words, but only 9% verbatim, which is worth under 1% of a lead's reads.
- Running `bmesh setup` before the agent starts. That is now possible for T3 leads, but spec 070's step stays in the brief.

Judge: opus, because the T3 worktree move changes the start path every lead takes, and a profile written too late, or committed by a lead, fails quietly.

## What you'll see
- **With the k16 values below,** a lead's first request drops from ~101 KB to ~69 KB, ~11.5k tokens less. Over ~64 calls that is ~740k fewer tokens re-read, ~7% of a lead's context reads and ~5% of its cost. A round thread saves ~12% of its reads.
- **With plugins and bundled skills alone,** without the deny rules and skill overrides, the drop is ~10 KB.
- **The listing itself** shows only the kept plugins' skills, with no pstack hook and no ghidra tools.
- **`bmesh config check`** names the plugins threads turn off.
- **T3 leads' worktrees** sit under `[runner] worktrees` beside the rounds' and executors', and T3 shows them as before.
- **With `[threads]` unset,** threads load what they load today, and only a T3 lead's worktree location changes.

## Verification
The repo's verify, plus tests:
- **`internal/runner`.**
  - The T3 fake receives `existing_worktree` for a worktree that exists and already holds the profile when `t3_thread_launch` arrives.
  - The claude and tmux fakes find the file before their first turn.
  - With no profile, nothing is written.
  - A failed launch removes the worktree and branch the runner made.
- **`cmd/bmesh`.** `testdata/fake-claude.sh` learns `plugin list --json`.
  - With three plugins and `plugins = ["a@m"]`, the file holds `{"enabledPlugins":{"b@m":false,"c@m":false}}` merged with the settings file.
  - `buildermesh@x` is never turned off.
  - `git status --porcelain` is empty in the worktree.
  - A tracked `.claude/settings.local.json` gets no profile.
- **`config check`.** Its line and its refusal.

## Risks and the alternative considered
- **Which settings keys reach what belongs to Claude Code, and varies by version.**
  - `enabledPlugins` at local scope is documented.
  - `skillOverrides` (`"off"`, or `"name-only"` to list a skill without its description) and `disableBundledSkills` are in claude 2.1.300's settings schema.
  - Whether a bare `permissions.deny` entry also takes a tool or an MCP server out of the list is unverified. This session's auto mode refused to start a nested `claude` to test it.

  That is why bmesh generates only `enabledPlugins` and passes the rest through the operator's file. The first thread after it is set shows the effect in its recorded first request.
- **A T3 project's worktree-create script no longer runs for leads.** T3 doesn't run it for an existing worktree, the same as for rounds today. A repo that installs dependencies that way should name the command as its `setup` (070).
- **Alternative: launch the T3 thread idle, wait for T3's worktree, write the profile, then send the brief.** This keeps T3's worktree, but it relies on T3 not starting the session at launch, and it adds a step that can fail halfway.
- **Alternative: a committed `.claude/settings.json` per repo, written by scaffold.** It reaches every session in the repo, the operator's own included, and puts one fleet's plugin names into each repo.
- **Alternative: `--bare`, `--setting-sources` or a separate `CLAUDE_CONFIG_DIR`.** T3 takes no flags and starts every thread in its own environment. `--bare` also drops the OAuth sign-in.

## Upgrading
- Agent threads can start with only the plugins this box names. Which plugins should they keep? Choices: a list of `claude plugin list` ids in `[threads] plugins`, or unset to keep every enabled plugin. Default: unset.
- Should agent threads get extra Claude Code settings, such as skill overrides or deny rules? Choices: a settings file path in `[threads] settings`, or none. Default: none.
- A T3 lead's worktree is now made by bmesh under `[runner] worktrees`, so T3 no longer runs the project's worktree-create script for it. If that script installs dependencies, name the command as the repo's `setup`.

## Needs from the operator
- [k16] In `~/.config/bmesh/config.toml`, set `[threads] plugins = ["buildermesh@buildermesh", "method@rickdiculousli-tools"]`, and set `settings` to a file holding `"disableBundledSkills": true`, `skillOverrides` `"off"` for each claude.ai-synced `anthropic-skills` skill, and `permissions.deny` for `ScheduleWakeup`, `ReportFindings`, `ListAgents`, `mcp__ghidra` and the claude.ai connectors. Add `pstack@pstack-claude` to `plugins` instead if leads should run pstack's steps. check: bmesh config check | grep -q '^threads: keeps'
