---
id: 075
title: The daemon watches the roster and registry keys, so a publish reaches every box at once, not within a minute
kind: time-cost
size: S
judge: opus
status: proposed
lens: eng
proposed_by: 9079c88e-3b4d-4243-afcb-44802f8c604a
proposed_on: 2026-10-17
---

## Opportunity
The roster and the registry are two keys, `roster` and `registry`, in the hub's roster bucket. A daemon reads them only from its minute tick (`hostsNode.tick`, `internal/daemon/hosts.go:113`, every `defaultHostsEvery`). So an admin's publish or approval reaches each box anywhere from zero to sixty seconds later.

On 2026-10-17 the operator published opengym-rick's new `setup` from the Mac and asked whether k16 had it. It didn't yet. Each wait like that is time spent re-checking, and 074's rescaffold would inherit the same lag.

JetStream key-value buckets push changes: `kv.Watch` delivers each put and delete as it happens. bmesh already uses that for runner samples (`WatchRunner`, `internal/mesh/nodes.go:206`).

## Proposal
In:
- **A watch on both keys.** `hostsNode` starts one more goroutine that watches `roster` and `registry` in the roster bucket. Each update (a put, delete or purge) calls `syncRoster` or `syncRegistry`, the same functions the tick calls. They already check the signature, compare versions and re-put this box's copy, and `syncRegistry` already holds `regMu`, so a watch event and a tick never apply the same registry twice.
- **The tick stays as the resync.** Its other work is time-driven: the wake check, presence, salts, holds and `this-box.txt`. It also keeps the minute's roster and registry reads, which cover what a watch misses: the gap while the watch re-subscribes after the hub restarts or the box wakes from sleep.
- **A watch that ends is restarted.** It restarts after the tick interval, with a log line, and never takes the daemon down.
- **Docs.** The manual's "Every daemon re-reads the hub's roster/registry each minute" lines say that a box takes a change when it is published, and re-checks each minute.

Out: presence, rounds, branches and self-update, which stay on the tick. Changing the tick interval.

Judge: opus, because the watch and the tick reach the same apply path from two goroutines, and a race there writes `repos.toml` twice or re-puts a stale copy over a fresh one.

## What you'll see
`bmesh repos show <repo>` on every awake box shows a just-published row within a second or two, instead of up to a minute later. An approved registry proposal and a revoked host take effect just as fast.

## Verification
The repo's verify, plus `internal/daemon` tests on the in-process hub (`meshtest`):
- A registry put with a higher signed version is applied without advancing the tick.
- A roster put is the same.
- Deleting the hub's registry key is healed (this box's copy re-put) without a tick.
- A watch event and a tick at the same moment write `repos.toml` once.
- A watch closed by the hub is restarted, and a put after the restart is applied.

## Risks and the alternative considered
- **Two paths to one apply.** That is why the existing locks and version check stay the only gate, and the test above pins it.
- **A hub that flaps** restarts the watch at most once per tick interval.
- **Alternative: a shorter tick** (say 5 s). Simpler, but every box then reads the hub twelve times as often for a change that comes a few times a week, and it is still a lag.

## Upgrading
nothing

## Needs from the operator
