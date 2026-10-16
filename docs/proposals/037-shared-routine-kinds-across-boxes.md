---
id: 037
title: Two boxes running the same routine kind stop answering, throttling and naming over each other
kind: refinement
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
built_by: 01a8df36-d510-45d6-ae37-d652fb03b0c4
host: k16
family: buildermesh-build-3
---

## Opportunity

This follows 036 and touches the same code (`rescan`, `route`). 036 gives a Mac-only kind one consumer, the Mac. Kinds that both boxes run, `review` and `verify` in nearly every repo, still share one consumer per name (`inbox-routine--<kind>`), and three things go wrong between two boxes today:

1. **A box answers jobs it can't run.** A box with no checkout of a repo has no group for it (`rescan`, `internal/daemon/routines.go:234-241`). `handOff` then answers that repo's jobs `kind <k> is not on <repo>'s main` and acks them (`routines.go:653`). `computer-use` is checked out on the Mac and not on k16, so k16 can refuse its `review` jobs. 036's dropped `os` kinds add a second way to have no group.
2. **One box's settings throttle the other's.** `applyKind` writes `MaxAckPending` and `AckWait` from this box's groups alone (`consumerSettings`, `routines.go:361`) whenever its feed starts or its own values change. Whichever box wrote last sets the kind's concurrency for the whole mesh, and that is a `fresh` kind's only limit. A shorter `AckWait` from the other box gets a long run redelivered while it still runs, because each box heartbeats at its own `ackWait/3` (`routines_job.go:68`).
3. **Both boxes name their pooled instances alike** (`routine.<kind>.<model-short>-<n>`, `routines.go:417`). Two `routine.review.opus-5-5-1` instances write one node, one state and one runner sample.

## Proposal

The rule: a box runs what it can and leaves the rest for a box that can, and each box limits only itself.

1. **Defer only what this box can't judge.** `rescan` already knows, per repo, whether it read the repo's main here. It now hands each feed the set of repos it could not judge the kind for: repos with no checkout on this box, repos whose main failed to read and had no group from an earlier scan, and repos whose kind 036 left out for another `os`. The set changes together with the feed's groups under `f.mu`, and `handOff` checks it there, so no rescan leaves a repo briefly neither deferred nor grouped. A job for a repo in that set, or for a repo this box's registry doesn't name yet (`unknown repo`, `routines.go:632`: the routine loop reads `repos.toml` only at start), is NAKed with delay `min(2s × 2^(n-1), 1m)`, `n` the message's delivery count. There is no reply and no ack, and the box logs `routines: deferred job <id>: <repo> <why> here` once per job. A box that read the repo's main keeps answering what it can judge, today's not-on-main answer included, which now names the commit it read: `kind <k> is not on <repo>'s main (origin/main at <sha8>)`, since each box reads main at its own last fetch. A repo kept from an earlier scan names that scan's commit. So a lead that queues a kind only its branch has is still told at once by any box with the repo. A job only boxes without the repo see waits until `routine wait` times out, as a job for a kind no repo defines waits today.
2. **`queue` refuses what no daemon should have to.** An inline kind (exit 3, `bmesh: <kind> is inline: run it with bmesh routine run`), since it already holds the kind (`cmd/bmesh/cmd_routine.go:311`). A repo its own registry doesn't name (exit 3, `bmesh: <repo> is not in the registry`), checked in `cmdRoutineQueue` after `repoName` resolves, so a node's repo is checked too. Today `repoNameAt` falls back to the directory's base name (`cmd_routine.go:393-410`). It keeps that fallback for `bmesh land`'s lease name (`cmd_land.go:80`), which needs no registry. A daemon's `unknown repo` is then only ever a registry row that box hasn't loaded yet, which item 1 rightly defers, and never a job that would wait 30 days for nobody.
3. **Each box caps its own concurrency.** A daemon sets the kind's consumer to `MaxAckPending: -1`, which nats-server accepts as unlimited. Each feed fetches only while `feed.inflight` is below this box's capacity: the sum over its groups of `pool × models` (pooled) and `pool` (fresh), and at least 1, so a feed whose groups can only refuse still fetches and answers. Deferred jobs are NAKed at once and never fill the capacity.
4. **The consumer converges, and heartbeats outpace any `AckWait`.** On each rescan, `applyKind` reads the consumer's config (`Consumer.Info`) rather than trusting its cached `f.maxAck`, and writes `MaxAckPending: -1` back, with the `AckWait` it read, when it reads anything else. An old daemon that writes a count is undone within a minute. `AckWait` stays last-writer-wins, written only when this box's own values change, as today. Its bug was the heartbeat: each delivery beats at a third of this box's own `AckWait` (`routines_job.go:68`), and nats-server applies another box's shorter `AckWait` to every pending message at once, so a long run was redelivered while it still ran. A delivery now beats every `max(ackWaitExtra/3, 100ms)` (20 s; the floor keeps the tests' shortened tunables from panicking `time.NewTicker`). Every `AckWait` any daemon writes is a kind's timeout plus `ackWaitExtra`, so no setting from any box can outrun the beat.
5. **`go` reaches every box.** A go is one message, and on a shared consumer one box fetches it. Every box already peeks the stream for gos while it holds jobs (`peekGo`). The peek starts after `max(the box's oldest held job's sequence, the last go it acted on)`, not after the highest sequence it fetched. A go another box fetched then still starts this box's batch, and a go already acted on is never found twice. A gated feed peeks each `fetchWait` instead of fetching.
6. **Instance names carry the host.** Pooled instances become `routine.<kind>.<host>-<model-short>-<n>`, numbered from 1 per model per box (`routine.review.k16-opus-5-5-1`). The host part is the box's host name (`[host] name`, else `os.Hostname()` cut at the first `.` and lowercased, `config.go:443-449`) made a path token by the rule `ModelShort` applies after its `claude-` strip, factored out without the strip: every character outside `[a-z0-9]` made `-`, runs collapsed, ends trimmed. `ricks-macbook` stays `ricks-macbook`. When 032 has landed, this is its `mesh.RepoToken` (`.` and `_` written as `-`, lower case) reused for host names rather than a second helper. The name stays one path token, so no instance looks like a parent of another. Checkouts are local to the box, so their directories keep the host-less name (`<checkouts>/<repo>/routine.<kind>.<short>-<n>`, `checkout.go:48`): the instance passes that name to `ensureCheckout`, and a warm build tree stays warm across the rename. Fresh runs keep `routine.<kind>.f<id8>`, unique per job. The old host-less nodes need no code: the old daemon's own stop marks them `done` (`routines_instance.go:36`), and the sweep removes them after `[status] keep_done`.
7. **Docs and constants.** The README's Jobs section replaces the consumer paragraph's `MaxAckPending = the kind's instance count` with per-box capacity, the converging consumer and the defer rule, and the buckets table's consumer row (README:495) says the same. README:290's list of `ok=false` answers loses the unknown repo and gains the deferral and not-on-main's `(origin/main at <sha8>)`. README:288's "else the main repo directory's base name" becomes queue's refusal. README:288's heartbeat (`AckWait`/3) becomes 20 s, and so does the delivery comment at `routines_job.go:29`. Modes gives the new pooled names, numbered from 1 per model across this box's repos that define the kind, no longer shared between boxes. 036's one-inbox-per-name rule goes, since a shared name is now safe. `defaultKindMaxAck` (`internal/mesh/jobs.go:59`) becomes `-1`, and the comment over `dispatch` (`routines.go:531`) is rewritten for gating.

In: the defer rule, `queue`'s inline and unregistered-repo refusals, per-box capacity, the converging consumer and heartbeats, the go peek, host-named pooled instances with their checkouts kept in place, the README and constants, and tests for each.

Out: per-box slot leases (`bm-leases` keys stay `<slot>.<n>` mesh-wide, so two boxes' browser checks share one `browser` slot: slower, never wrong), and a per-OS inbox. Defer covers both "no checkout here" and "another OS on main" with one mechanism. 036's answer in `handle` for a commit whose `os` names another box stays an `ok=false` answer, not a deferral.

Judge: opus, because it changes how every daemon takes jobs from a consumer two boxes share, a delivery and concurrency change with no taste call in it.

## What you'll see

`routine status review` lists `k16-opus-5-5-1` and `ricks-macbook-opus-5-5-1` as two instances with their own states. A `review` job from a repo only the Mac has is run by the Mac and never answered `not on main` by k16. With `pool = 1` on each box, two `review` jobs run at once, one per box. A `verify` queued by mistake is refused at once by `queue`.

## Verification

The verify kind, with new tests in `internal/daemon/routines_test.go` running two daemons (different `Host`, one `meshtest.Start` server):
- A job for a repo only daemon A has is run by A. B defers it and never replies, and B's deferral delays grow and cap at 1m.
- A job for a kind on neither repo's main, from a repo both daemons have, is answered not-on-main at once, naming the main commit.
- With `pool = 1` on each, two jobs run concurrently and neither box exceeds 1 in flight.
- A consumer rewritten to `MaxAckPending: 1`, as an old daemon would, reads `-1` after the next rescan. A consumer whose `AckWait` another daemon shortened below a running job's time does not redeliver it, since the job beats every 20 s.
- A go fetched by A starts B's held batch.
- The two boxes' pooled instances are distinct nodes named with their hosts, and each still uses its old checkout directory.

A `cmd/bmesh` test covers queue's inline and unregistered-repo refusals. `kindRepo` (`cmd_routine_test.go:16`) registers the repo it makes with `writeRepos` (`cli_test.go:59`), since `setup` registers nothing, so the existing queue tests keep passing. `TestQueueFromLinkedWorktreeNamesConfiguredRepo`'s base-name and unregistered-node assertions rewrite `repos.toml` without the repo and assert the refusal. On the fleet, `routine status review` shows both hosts' instances.

## Risks and the alternative considered

- **Unlimited `MaxAckPending`.** A daemon that failed to gate would pull every waiting job. The gate is the counter the feed already keeps, and the tests pin it.
- **Old daemons.** Until both boxes update, an old one can still answer `not on main` for a repo it lacks, and can rewrite the consumer between rescans. Convergence undoes the consumer write within a minute. An old daemon doesn't gate either, so under `MaxAckPending: -1` it can pull every waiting job of a shared kind, up to 64 held per pool for a pooled kind, and every waiting job at once for a fresh kind, which has no pool channel (`routines.go:668-673`): party-buzzer's `surface` would get a worktree per job and NAK the busy ones every 30 s. Both end when the Mac runs `bmesh update`, which is why that comes first.
- **Bounces.** A deferred job is fetched by a box that can't run it at most about once a minute once the backoff caps. A NAK with a delay keeps the message pending until the delay ends, so a box that can run it and has just woken may wait up to that minute.
- **Double runs on a shared kind when a box sleeps mid-run.** If the Mac sleeps through a `review` past `AckWait`, the job is redelivered and k16 may run it too. This is accepted: the box that can run a job should, and a deterministic kind is answered from its record the second time.

Alternatives:
- **A consumer per box.** Each box would then see every job, and two boxes that can both run it would both run it. JetStream's competing consumer is the right primitive, and it only needs boxes to stop answering for each other.
- **Requester-chosen box.** Presence is never the source of a decision, and a box asleep at queue time would strand the job.

## Needs from the operator

After the merge, `bmesh update` on the Mac. k16 updates itself.
