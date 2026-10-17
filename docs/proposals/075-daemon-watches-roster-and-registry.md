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
- **A watch on both keys.** `hostsNode` starts one more goroutine that watches `roster` and `registry` in the roster bucket. Each update (a put, delete or purge) calls `syncRoster` or `syncRegistry`, the same functions the tick calls. They already check the signature, compare versions and re-put this box's copy. Each also holds a lock for its whole read-compare-write: `syncRegistry` holds `regMu`, and `syncRoster` holds `h.mu` across `hosts.Sync`. So a watch event and a tick are serialized, and never apply the same version twice or put a stale copy over one just taken.
- **Updates only.** The watch is opened with `jetstream.UpdatesOnly()`. The tick already does the start-up read, so the initial values a default watch delivers would only run one extra sync.
- **The tick stays as the resync.** Its other work is time-driven: the wake check, presence, salts, holds and `this-box.txt`. It also keeps the minute's roster and registry reads, which cover what a watch misses: the gap while the watch re-subscribes after the hub restarts or the box wakes from sleep.
- **A watch that ends is restarted.** When its updates channel closes, or opening it fails, it is opened again after the tick interval, with a log line, and never takes the daemon down. The jetstream watcher usually rebuilds its consumer itself after a hub restart, so a watch can also go quiet without closing. That case is the tick's to cover, and no liveness probe is added for it.
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
- Two daemons, one holding registry v5 and one v6, both heal a deleted key at once. Whatever order their puts land in, the hub settles on v6 with no tick, because the v6 box's watch sees v5 and puts v6 again. Neither box takes v5.
- A watch event and a tick at the same moment write `repos.toml` once.
- A watch whose channel closes (the test stops it) is reopened after the tick interval, and a put after that is applied.
- After the in-process hub restarts, a put is applied: by the rebuilt watch, or at the latest by the next tick.

## Risks and the alternative considered
- **Two paths to one apply.** That is why the existing locks and version check stay the only gate, and the test above pins it.
- **Every box heals at once.** Today each box's tick fires at its own moment, so the box that re-puts a deleted key is effectively whichever ticks first. With a watch, every awake box re-puts its own copy at the same moment. Puts are plain and signed, and a lower version is always overwritten by the next watch event of a box holding a higher one, so it converges within a round of events (the two-daemon test).
- **A hub that flaps** restarts the watch at most once per tick interval.
- **Alternative: a shorter tick** (say 5 s). Simpler, but every box then reads the hub twelve times as often for a change that comes a few times a week, and it is still a lag.

## Upgrading
nothing

## Needs from the operator
