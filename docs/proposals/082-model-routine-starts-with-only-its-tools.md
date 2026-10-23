---
id: 082
title: A model routine starts claude with only the tools its kind allows, and no skills or MCP servers
kind: token-cost
size: S
judge: opus
status: done
lens: eng
proposed_by: a234110b-dade-4827-b449-632e5de4650b
proposed_on: 2026-10-18
built_by: ebc8e6f2-ef9f-48eb-8a65-d513339357c3
host: k16
family: buildermesh-build-13
branch: bmesh/ebc8e6f2-ef9f-48eb-8a65-d513339357c3
merged: 6064dfb3a7848f8aec4d830a3c5f0fc559eec056
verified: verify ok t:beb11a61
---

## Opportunity
A model routine, such as the `review` kind, runs `claude -p ... --allowedTools Read,Grep,Glob,LS,<allow>` (`internal/instance/model.go:89`). `--allowedTools` only grants permission. Every other tool, every skill and every MCP server the box's user settings enable is still sent to the model.

Claude Code records each request it sends in the session's transcript, as `api-request-shape` and `api-request-blob` entries. Over 74 review sessions on k16 (2026-10-15 to 2026-10-18), the first request averages 77 KB, about 29k tokens:
- **Built-in tool schemas: 27.0 KB.** The review uses Bash, Read, Grep and Glob (9.6 KB). The rest are Agent, ScheduleWakeup, ReportFindings, ListAgents, Skill, ToolSearch, Edit and Write.
- **The skill listing: 21.0 KB.**
- **The rest:** instructions 7.8 KB, system prompt 7.8, MCP tool names and connection notes 3.6, the agent listing 2.1, a plugin's SessionStart hook 1.8, and the job 2.8.

Across those 74 sessions the model called Bash 486 times, Read 99, Grep 74, Write 6 (refused) and Glob 5. It never called a skill, an agent or an MCP tool.

A review averages 8.9 calls, and each one re-reads that start, so 56% of a review's context reads are the start read again. Prices relative to fresh input are 0.1x for a cache read, 1.25x for a cache write and 5x for output. At those prices, cache writes are 46% of a review's cost, and most of that is the first call's ~17k-token write.

## Proposal
In:
- **Every model-kind run** (the first job and every `--resume`) adds three flags:
  - `--tools <names>`: the built-in tools named in `baseTools` and `allow`, each entry cut at `(` (`Bash(git diff:*)` gives `Bash`). MCP entries (`mcp__…`) aren't built-in tools and stay out of this list.
  - `--strict-mcp-config`, with no `--mcp-config`, so no MCP servers load, unless `allow` names an `mcp__` tool.
  - `--disable-slash-commands`, so no skills load, unless `allow` names `Skill`.
- **`--allowedTools` stays as it is.** It sets permission; `--tools` sets which tools exist.
- **`LS` is no longer a Claude Code tool.** `--tools` gets only names claude accepts: check what an unknown name does, and drop `LS` from `baseTools` if claude refuses it. As built: claude 2.1.309 ignores an unknown name in `--tools` without a word (the request held Bash, Glob, Grep and Read for `--tools Read,Grep,Glob,LS,Bash`), so `LS` is a dead name and goes from `baseTools`.
- **Docs.** The manual's model-kind argv line and its `allow` row, which now also decides what the session holds; internals where it gives the argv.

Out:
- Command kinds.
- Agent threads (083).
- Hooks and CLAUDE.md, which stay as the operator's settings make them.

Judge: opus, because deriving tool names from permission rules must not drop a tool a kind relies on, and the result is checked against the request claude actually sends.

## What you'll see
A review's first request drops from ~77 KB to ~33 KB: its tool list is Bash, Read, Grep and Glob, with no skill listing and no MCP tool names. That is ~16k fewer tokens on each of its ~9 calls, about a third of its context reads. Its first call also writes ~10k fewer tokens at 1.25x. Together that is about a fifth of a review's cost. Findings don't change, because the review never used what goes.

## Verification
The repo's verify, plus `internal/instance` tests with the fake claude, which logs its argv:
- The review kind's argv carries `--tools` with the base set plus `Bash`, `--strict-mcp-config` and `--disable-slash-commands`, on the first job and on a `--resume`.
- `allow = ["Skill"]` drops `--disable-slash-commands`.
- `allow = ["mcp__x__y"]` drops `--strict-mcp-config` and keeps `mcp__x__y` out of `--tools`.
- `--allowedTools` is unchanged.

Then one real review job on the built commit, with `bmesh routine queue review` and `routine wait` as for the lead's own review. The job's transcript (`~/.claude/projects/<checkout dir>/<session>.jsonl`) must show a first `api-request-shape` whose tools are those four, and a first `api-request-blob` with no skill listing and no deferred-tool block. The done message gives the before and after sizes.

## Risks and the alternative considered
- **A kind that used a skill or an MCP tool loses it.** If a fleet's model kind has a prompt that invokes one, the kind loses it until `allow` names it. The Upgrading step says so.
- **One cache rewrite.** `--tools` changes the cached prefix, so the first review after the update writes it once.
- **Alternative: `claude --bare`.** It also drops hooks and CLAUDE.md, but it signs in only with `ANTHROPIC_API_KEY` or an `apiKeyHelper`, never the OAuth sign-in a subscription uses, so reviews would stop on such boxes.
- **Alternative: `--setting-sources project,local`.** It drops the user's plugins, but also the user's permissions and any sign-in setting.
- **Alternative: a per-kind `context = "full"|"bare"` key.** That is one more key, when `allow` already says what the kind uses.

## Upgrading
A model routine now sees only the tools its kind's `allow` names, plus Read, Grep and Glob, and no skills or MCP servers. A kind whose prompt uses a skill adds `"Skill"` to its `allow`. One that uses an MCP tool adds that tool (`"mcp__<server>__<tool>"`). Kinds that use neither need nothing.

## Needs from the operator
