---
id: 029
title: The t3 runner speaks T3 Code 0.1.0 (Orchestrator V2) through its MCP tools
kind: maintenance
size: L
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
---

## Opportunity
T3 Code 0.1.0 makes Orchestrator V2 stable and removes the V1 API that the `t3` runner depends on. `t3_dispatch.py` posts `thread.create` and `thread.turn.start` to `/api/orchestration/dispatch`, and V2 has neither the route nor the command set. Upgrading T3 before bmesh can talk to V2 stops `bmesh build` from starting leads. Staying on 0.0.45 isn't an option either: V1 gets no more fixes, and the 2.0 phone app refuses a V1 server.

Both boxes run 0.0.45 today and must move one at a time (k16 first, then the Mac), so for a while one mesh has a V1 server and a V2 server. The plan is command-center's `docs/superpowers/plans/2026-10-15-t3-v2-migration.md`. Its Phase 1 measured a scratch 0.1.0 server on k16 (2026-10-16), and every V2 fact below comes from that run:
- **Protocol.** `GET /.well-known/t3/environment` reports `serverVersion` and `orchestrationProtocolVersion` (`1` on 0.0.45, `2` on 0.1.0).
- **Credential.** The MCP endpoint `/mcp` (streamable HTTP, JSON-RPC, `mcp-session-id`) accepts only MCP client sessions.
  - A token minted on the box with `t3 auth session issue` is capped at `approval-required`. With it, a `full-access` launch fails `runtime_mode_escalation_denied`, and `t3_project_create` fails `capability_denied`.
  - Only the OAuth sign-in carries a full-access ceiling. The server advertises dynamic registration, PKCE S256 and public clients at `/.well-known/oauth-authorization-server`, with grant `authorization_code` only.
  - The token lasts 30 days and has no refresh token. The ceiling is set only by the code exchange (`issueMcpClientSession`, `MCP_CLIENT_SESSION_TTL = days(30)`).
- **Launch.** `t3_thread_launch {projectId, title, modelSelection: {provider: "claudeAgent", model, options: {effort}}, runtimeMode: "full-access", workspaceStrategy, message}`.
  - `workspaceStrategy` is `{type: "worktree", baseRef, branch}` or `{type: "existing_worktree", worktreePath, branch}`.
  - The given `branch` is used as is, and the worktree lands at `<T3 home>/worktrees/<repo>/<branch>`.
  - The server assigns thread ids (`mcp:<uuid>`). Launch has no retry key.
  - `existing_worktree` refuses a path that isn't one of the project's own git worktrees ("worktreePath must be one of the project's git worktrees").
- **Messages.** `t3_thread_send {threadId, message, mode, clientRequestId}`:
  - `queue` waits behind a running turn (`{status: "queued"}`), then runs as the next run;
  - `auto` steers into a fully active turn;
  - `clientRequestId` makes a retry idempotent.
- **State.** `t3_thread_read` gives `thread.status`, `activeRunId`, `pendingRequestCount`, `lastErrorClass`, `usageLimitResetAt` and `recentRuns[].status`.
  - Run states are `preparing`, `queued`, `starting`, `running`, `waiting`, `completed`, `interrupted`, `failed`, `cancelled` and `rolled_back`.
  - `waiting` means a pending approval or question.
  - A usage limit shows as `failed` with `lastErrorClass: "usage_limit"`.
  - `t3_thread_interrupt` on an idle thread returns `no_active_run`.
- **Links.** The web route is still `/<environmentId>/<threadId>`. A V2 thread id has a colon, which the link must encode.

## Proposal
The `t3` runner keeps one interface and gets two paths. In, all in buildermesh:

- **Picking the path.**
  - `[runner] t3_server` (default `http://127.0.0.1:3773`) names the box's own T3.
  - Each call reads `/.well-known/t3/environment`, cached for a minute, and uses V1 (`t3_dispatch.py`, unchanged) for protocol 1 and V2 for protocol 2. Anything else is an error that names the version.
  - An existing node's thread stays on whichever server path it was started on. A V1 thread id given to a V2 server is `ErrNoThread`.
- **The V2 client** is Go, with no `uv`: a small MCP-over-HTTP client in `internal/runner/t3mcp` (initialize, `notifications/initialized`, `tools/call`, the session header, re-initializing once on an expired session). A tool result with `isError` becomes an error carrying its `code` and `message`.
- **Start.**
  - It finds the project whose `workspaceRoot` is the repo (`t3_project_list`), or creates it (`t3_project_create`).
  - It then launches with `runtimeMode: "full-access"`, provider `claudeAgent`, `[models]` model and effort as `options.effort`, the title, the brief as `message`, and `workspaceStrategy {type: "worktree", baseRef: <StartSpec.Branch or "main">, branch: "bmesh/<ThreadID>"}`.
  - With `StartSpec.Worktree` set, it launches `existing_worktree` with that path and branch.
  - Because launch has no retry key, a timeout or lost reply is reconciled with `t3_thread_list` on the project, matching the `bmesh/<ThreadID>` branch, before any error is returned. `Started.Thread` is T3's id.
  - On V2, a round's thread is filed under the target repo's own project and `[runner] t3_project` is ignored: V2 can't bind another repo's worktree. The daemon logs once that `t3_project` has no effect on a V2 server.
- **Status.**
  - `Running` is true when `activeRunId` is set or the newest run is `preparing`, `queued`, `starting`, `running` or `waiting`.
  - `LastReply` is the newest `assistant_message` text.
  - `waiting` with `pendingRequestCount > 0` is logged as `t3: <path> waits on a question or approval` (a full-access lead shouldn't get here).
  - A `failed` newest run with `usage_limit` logs the reset time, and the silence check treats it as not running.
- **Send** keeps today's contract. It reads Status first and returns `ErrRunning` for a running thread, so the daemon's wake doesn't pile up. Otherwise it calls `t3_thread_send` with `mode: "queue"`, never `auto`, so a turn that starts between the check and the send gets a queued message rather than a steered one. `clientRequestId` is `bmesh-<sha256(thread, text, minute)>`, so a retried wake isn't sent twice.
- **Interrupt** is `t3_thread_interrupt`, and `no_active_run` counts as success.
- **The credential.**
  - `bmesh t3 login [--server URL] [--callback-port 52299]` registers bmesh as a public client and prints the authorize URL. It accepts the redirect on a local listener (default port 52299, so the operator's tunnel command is the same every month) or as a pasted URL.
  - It exchanges the code with PKCE and writes `<config dir>/t3-token.json` (mode 0600: server, token, `expiresAt`, scopes). `--status` prints the server, scopes and days left.
  - The login prints the steps for approving from another machine, so the operator can sign in from the phone or the Mac:
    - `ssh -N -L <port>:localhost:<port> <box>` for the callback;
    - choose **Full access**;
    - a pairing code from `t3 auth pairing create` if the browser isn't paired.
  - The V2 path refuses to start a lead without the token, or with it expired, with `bmesh: T3 sign-in missing or expired; run bmesh t3 login` (exit 3). It refuses a token whose ceiling turns out to be less than full access with the same exit, naming the cap.
  - The daemon pushes `T3 sign-in on <box> expires in N days: bmesh t3 login` at 7, 3 and 1 days before `expiresAt` (once each; only that box's daemon reads its token, so the pushes sent are kept in `<state dir>/t3-login-pushes.json`, which survives a restart), and `bmesh mesh-status` shows the expiry when it is under 7 days.
  - The token path and the login are behind one `credential` interface, so if T3 later lets the box's own CLI mint a full-access session, a self-renewing source replaces the login without touching the runner.
- **Console links.** `ConsoleURL` path-escapes the thread id before putting it into `{thread}`, so `mcp:…` becomes `mcp%3A…` and a V1 id is unchanged.
- **Tests.** An MCP fake in `testdata/` (an `httptest` server) serves the environment document, the OAuth endpoints and the tools above with the shapes recorded in Phase 1. It covers:
  - protocol 1 versus 2 path selection;
  - start, including a lost reply reconciled by branch;
  - an `existing_worktree` start;
  - each run status mapped to `Running`;
  - send refusing a running thread, and queueing otherwise;
  - the `clientRequestId` dedupe;
  - interrupt on an idle thread;
  - the login with a pasted redirect, and an expired token refused;
  - each expiry push sent once across a daemon restart;
  - an escaped console link.
- **README's Runners section** describes both paths, the sign-in and its monthly re-approval, the `t3_project` caveat on V2, and `[runner] t3_server`.

Out:
- Removing the V1 path and `t3_dispatch.py` (024, once both boxes are on 0.1.0).
- Codex threads through the runner.
- T3's own push, webhooks, schedules and Connect. ntfy stays the push path.
- `t3_thread_wait` inside the daemon. The daemon keeps polling Status each minute, so one slow server can't hold a goroutine.

Judge: opus, because the facts are measured but the reconcile-by-branch, the send race and the login flow need judgment against a live server.

## What you'll see
On a box whose T3 is 0.1.0, `bmesh build buildermesh 0NN` starts a lead that shows in T3's sidebar under the repo's project, on branch `bmesh/<thread id>`. From another box, `bmesh --node operator send <path> ping` reaches it within `wake_every`, and the map's thread link opens it. The same binary on a 0.0.45 box still starts leads through `t3_dispatch.py`. `bmesh t3 login --status` prints `k16: full-access, 29 days left`, and a week before expiry the phone gets a push saying to sign in again.

## Verification
- The repo's verify, with the new fake-backed tests above.
- A live check by the lead on k16:
  - against the Phase 1 scratch V2 server (`systemctl --user status t3-v2-scratch`, port 3780; its scratch project's repo is `~/.cache/t3-v2-scratch/repo`), start a demo lead with a two-line brief, send it a message, read its status, interrupt it and `node forget` it;
  - do the same against the live 0.0.45 server on 3773 through the V1 path.
- The lead runs `bmesh t3 login --server http://127.0.0.1:3780` itself and asks the operator through `bmesh ask` to approve it (Full access). The `ask` text carries the URL, a fresh pairing code and the tunnel command.
- Record both live checks in the closing commit.

## Risks and the alternative considered
The sign-in needs a person every 30 days per box. Missing it stops new leads on that box but not running ones, and the pushes and `mesh-status` warn a week ahead.

The alternative is the apps' WebSocket RPC (protocol 2), with a box-minted session bmesh renews itself, as `t3_dispatch.py` does on V1. It needs no person, but it ties bmesh to T3's undocumented internal API again, which is what broke this time. The MCP tools are T3's documented surface for outside agents.

Filing round threads under the target repo's project instead of buildermesh's changes where they show in the sidebar. V2 leaves no other choice short of `scratch` threads, which would run outside the repo.

## Needs from the operator
- **Accept that, on V2, a proposal round's threads show under the target repo's T3 project**, not under buildermesh's (round two's decision 4 can't hold on V2).
- On each box after its T3 is on 0.1.0: run `bmesh t3 login` and approve it with Full access. Repeat monthly when the push says so.
- `[runner] t3_server` only if a box's T3 isn't on `127.0.0.1:3773`.
