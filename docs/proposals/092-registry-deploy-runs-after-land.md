---
id: 092
title: A repo's deploy kind runs on its host after every code change, and a lead is done only once it is live
kind: feature
size: L
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-23
---

## Opportunity
Some ship repos run where they are built. opengym-rick is a PWA served from its main checkout on k16:
`deploy/deploy.sh` builds `frontend/dist`, restarts a systemd user unit and mounts `tailscale serve`.
A change isn't in the operator's hands until that script has run. On 2026-10-23 the operator made
deploying part of done in opengym-rick's `AGENTS.md`, and the next lead (opengym-rick-016) landed
and finished without deploying: the served build stayed the 19:02 one until the coordinator ran the
script by hand. Prose can't win here. The landing daemon runs `land` and `done` for the lead, then
sends it `LandedText` (`internal/daemon/land.go:44`): `... Write your closing line; nothing else to
run.` A lead that lands in its own turn reads `done` succeeding as the end in the same way. Before
the rule, UI specs carried the deploy as an operator step (opengym-rick 010 and 012), which the
operator then ran and ticked by hand. Nothing in bmesh knows a repo has a deploy, so neither leads
nor `done` can hold to one.

## Proposal
Settled with the operator in a grilling on 2026-10-23.

In:
- **The switch is a reserved kind.** A repo deploys when it has `.bmesh/routines/deploy.toml`.
  There's no registry field. Like verify, the command is committed and reviewed with the code it
  ships, so the manual's levelling promise ("bmesh runs no other command a job or the registry
  names") still holds. The kind may set only `command` and `timeout` (default `15m`). Any other key
  (`mode`, `os`, `model`, `pool`, `slots`, ...) is refused when the file is read, with a message
  naming the key. Committing the kind turns deploys on; deleting it turns them off.
- **Where and when.** Only the daemon of the row's `host` box deploys, always in its main checkout
  (what serves), under the repo's lock (so never at once with a land, a level or a rescaffold). It
  deploys when `origin/main` has moved past the last commit it deployed, whoever moved it (a
  landing, a lead's own `bmesh land`, a hand push), but only when something under the row's code
  paths changed since then (the same test as `bmesh release`'s `nothing under code changed`).
  Several landings in a row deploy once, at the newest commit. When the kind is first committed,
  the current `main` deploys at once.
- **Moving the checkout.** It fast-forwards the checkout as a level would, with the same refusals
  (dirty, other branch, diverged, in progress). On a secure host it moves only as far as levelling
  may and waits for the same approval. Then it runs the command in the checkout with `BMESH_COMMIT`
  and `BMESH_TREE` set, its process group killed at `timeout`. The run is recorded as a `deploy`
  result for that tree, and the daemon keeps the last deployed commit per repo.
- **Done waits for a live deploy.** `bmesh done` for a spec of a repo with a deploy kind refuses
  until a `deploy ok` result exists for a commit that contains the spec's `merged` commit. A merged
  commit whose change since the last deploy touches no code is covered by that deploy. In the
  landing daemon's step 1, a landed but undeployed spec keeps the lead `landing`, with text
  `waiting for deploy on <host>`. There's no timeout: a host daemon that is down already shows on
  the map. `LandedText` is sent only after the deploy is ok, and both it and the done line name the
  deployed commit (`deployed <sha8> on <host>`). The spec's `verified` line is unchanged.
- **A failed command** wakes only the newest waiting lead, as a land refusal does:
  `bmesh: deploy failed on <host> and needs you`, the command's last 40 lines, then
  ``Fix it, commit, then run `bmesh ready` again.`` Other waiting leads stay `landing`. With no lead
  waiting (a hand push), the operator is notified through `[notify]` at high priority. A failed
  command isn't rerun until `main` moves again or someone runs `bmesh deploy <repo>`.
- **A refusal** (dirty, other branch, diverged, in progress, approval pending) fails no lead, since
  a lead can't clean the operator's checkout. Waiting leads stay `landing` with the refusal as their
  text, the operator is notified at high priority once per refusal, and every pass tries again, so
  cleaning the checkout is enough.
- **`bmesh deploy <repo>`** asks the host to deploy now (a retry after fixing the box) and prints
  the result. `bmesh mesh-status` and the map show each repo's last deploy (commit, ok, failed or
  refused, age).
- **Scaffold.** When the kind exists, `AGENTS.md`'s done block says the host's daemon deploys after
  landing and a lead is done once it is live, and that a lead never lists the deploy as an operator
  step. `done_rule` in `bmesh repos show --json` says the same.
- **Tests.** In the daemon: one deploy per newest code commit; none for a docs-only move; none on a
  box other than the host; a refusal fails no lead and retries; a failure wakes only the newest
  waiting lead; no rerun until `main` moves. In `done`: it refuses without a covering result and
  passes with one. In the kind loader: it refuses a key other than `command` and `timeout`.

Out: deploying anywhere but the host box; rollbacks; `fork` and pinned rows; running the deploy
inside `bmesh land` (that runs in the lead's worktree, while the deploy belongs in the checkout that
serves); a deploy as a model kind.

Judge: opus, because it changes when `done` holds, across the daemon's landing, `done`, the kind
loader, scaffold and mesh-status.

## What you'll see
Once opengym-rick has its kind (`command = "sh deploy/deploy.sh"`), a lead's watch reads `landing
waiting for deploy on k16`, then `done ...; deployed <sha8> on k16`. The served app is the landed
commit without anyone running anything, and specs stop carrying "rebuild and restart" steps for the
operator. A broken script puts the lead back to work with the error. A dirty checkout gets the
operator one notification, and the deploy goes through once the checkout is clean.

## Verification
`go test ./...` and the repo's verify, with the tests listed above (fake repos whose deploy command
writes a file, exits 1, or sleeps past its timeout). By hand on k16, once it has shipped: commit
opengym-rick's kind, see `main` deploy at once, then land an XS spec and see the unit restart
(`systemctl --user show opengym.service -p ActiveEnterTimestamp`) and the lead's done line name the
deployed commit. A docs-only push restarts nothing.

## Risks and the alternative considered
- **A hanging deploy** holds the repo's lock until `timeout`, and that counts as a failure.
- **Racing landings:** the newer commit deploys second, and `done` reads "contains", so both leads
  finish on the second deploy.
- **The newest lead may not have broken it.** It is still the one woken: it has the freshest
  context, and the others stay waiting rather than all fixing the same thing.
- **Alternatives considered.** A `deploy` command in the registry row (the first draft) breaks the
  levelling promise and isn't reviewed with the code. A per-repo systemd timer that pulls and
  deploys works today, but never holds `done`, and each repo would grow its own. Splitting this into
  "deploy runner" and "done waits" was rejected, since the runner alone doesn't make done mean live.

## Upgrading
A new reserved routine kind, `deploy`. Repos without the kind behave as before, and nothing has to
be set. Ask: does any ship repo run from its host's main checkout? Then commit
`.bmesh/routines/deploy.toml` with its deploy command. Default: none.

## Needs from the operator
