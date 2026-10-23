---
id: 092
title: A registry row can name a deploy, which its host runs after every landing before the lead is done
kind: feature
size: L
judge: opus
status: proposed
lens: eng
proposed_by: operator
proposed_on: 2026-10-23
---

## Opportunity
Some ship repos run where they're built. opengym-rick is a PWA served from its main checkout on k16
(`deploy/deploy.sh` builds `frontend/dist`, restarts a systemd user unit and mounts `tailscale serve`).
A change isn't in the operator's hands until that script has run. On 2026-10-23 the operator made
deploying part of done in opengym-rick's `AGENTS.md`, and the next lead (opengym-rick-016) landed and
finished without deploying: the served build was still the 19:02 one until the coordinator ran the
script by hand. Prose can't win here. The landing daemon runs `land` and `done` for the lead, then
sends it `LandedText` (`internal/daemon/land.go:44`): `... Write your closing line; nothing else to
run.` A lead that lands in its own turn reads `done` succeeding as the end in the same way. Before
the rule, each UI spec carried the deploy as an operator step (`opengym-rick` 010 and 012), which the
operator then had to run and tick. Nothing in bmesh knows a repo has a deploy, so neither leads nor
`done` can hold to one.

## Proposal
In:
- **Row fields.** A registry row gets `deploy` (one shell command, run in the row's host's main
  checkout) and `deploy_timeout` (default `15m`). Both go through `bmesh repos propose` like
  `verify`. Only a `ship` row may have them.
- **Who runs it.** The daemon of the row's `host` box, and only that one. It runs the deploy each
  time it sees `origin/main` move past the last commit it deployed: after a landing it ran, after a
  lead's own `bmesh land`, or after a push from anywhere. It holds the repo's lock (as levelling
  does), fast-forwards the main checkout to `origin/main` as a level would (same refusals: dirty,
  other branch, diverged, in progress), then runs the command with `BMESH_COMMIT` and `BMESH_TREE`
  set. It records a result of kind `deploy` for that tree, as routine results are recorded, and
  remembers the last deployed commit. Several landings in a row deploy once, at the newest commit.
- **Done waits for it.** `bmesh done` for a spec on a row with `deploy` refuses until a `deploy ok`
  result exists for a commit that contains the spec's `merged` commit. In the landing daemon's
  step 1, a landed but undeployed spec keeps the lead `landing` (text `waiting for deploy on
  <host>`). A failed deploy wakes the lead as a refusal does: `bmesh: deploy failed on <host> and
  needs you`, the command's last 40 lines, then `Fix it, commit, then run bmesh ready again`. The
  fix lands as a new commit and deploys in turn. A refused fast-forward (dirty checkout, say) is
  also a failure, and is also pushed to the operator with `[notify]` at high priority, because a
  lead can't clean the operator's checkout. `LandedText` is only sent once the deploy is ok, and it
  names the deployed commit.
- **The done block.** `bmesh scaffold` names the deploy in `AGENTS.md`'s done block when the row
  has one ("... merged, pushed, and deployed on `<host>` by its daemon (`<command>`); a lead never
  lists the deploy as an operator step"). The `done_rule` in `bmesh repos show --json` says the same.
- **Seeing it.** `bmesh mesh-status` and the map show a repo's last deploy (commit, ok or failed,
  age). `bmesh deploy <repo>` runs it now on the host, for a retry after the operator fixed the box.
- **Tests** for the daemon: it deploys once per newest commit, not again for the same commit, and
  not on a non-host box. `done` refuses without a deploy result and passes with one. A failing
  deploy wakes the lead with its lines. A dirty checkout refuses without running the command.

Out: deploying anywhere but the host box; rollbacks; deploys for `fork` or `pinned` rows;
running the deploy inside `bmesh land` (it runs in the lead's worktree, and the deploy belongs in the
checkout that serves).

Judge: opus, because it changes when `done` holds, across the daemon, `land`, `done`, the registry
and scaffold.

## What you'll see
Once opengym-rick's row has `deploy = "sh deploy/deploy.sh"`, a lead's watch reads `landing ...
waiting for deploy on k16`, then `done ... deployed <sha8>`. The served app is the landed commit
without anyone running anything, and specs stop carrying "rebuild and restart" operator steps. A
broken deploy shows up as the lead back at work with the script's error.

## Verification
`go test ./...` and the repo's verify. One new daemon test per behaviour listed in the proposal,
using a fake row whose deploy writes a file and a second that exits 1. By hand on k16: publish
opengym-rick's `deploy`, land an XS spec there, and see the unit restart (`systemctl --user show
opengym.service -p ActiveEnterTimestamp`) and the lead's done line name the deployed commit.

## Risks and the alternative considered
- **A new principle.** Levelling says "bmesh runs no other command a job or the registry names";
  this adds one, from the registry, run on the host only, and only for a row its operator gave a
  deploy. The manual's sentence changes with it.
- **A deploy that hangs** holds the repo's lock: `deploy_timeout` kills its process group, and
  that counts as a failure.
- **Two landings racing a deploy** deploy the newer one second; `done` reads "contains", so both
  leads finish.
- **Alternatives considered.** A per-repo systemd timer that pulls and deploys (in opengym-rick)
  works today but never holds `done`, and every repo would grow its own. A post-merge git hook in
  the main checkout misses `land`'s own fast-forward when no checkout moves.

## Upgrading
A new optional registry field, `deploy` (with `deploy_timeout`). Rows without it behave as
before. Ask: does any ship repo run from its host's main checkout? Then propose its row's `deploy`
with `bmesh repos propose <repo> --deploy "<command>"`. Default: none.

## Needs from the operator
- propose opengym-rick's deploy once this lands: `bmesh repos propose opengym-rick --deploy "sh deploy/deploy.sh"` (check: bmesh repos show opengym-rick --json | grep -q '"deploy"')
