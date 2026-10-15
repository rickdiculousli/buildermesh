---
id: 007
title: A routine reply is never lost, and an inbox read returns at once
kind: refinement
size: S
judge: opus
status: proposed
lens: eng
proposed_by: fd21a373-851c-4e68-beb6-831cb4be0fa9
proposed_on: 2026-10-15
---

## Opportunity
Two gaps in one code path, `mesh.Fetch` and `mesh.FetchMatch` (`internal/mesh/inbox.go:66`, `jobs.go:185`).

A stall the README documents instead of fixing (Jobs): "`bmesh inbox` acks everything it reads, replies included ... a `wait` for a job whose reply was read that way waits until its timeout". The lead skill warns about it twice (§4, §6). A lead that reads its inbox at a checkpoint, as the skill tells it to, while a review is in flight, then runs `routine wait` and sits for the default 20 minutes with the reply already gone. The reply is also printed whole into the lead's context: up to 64 KiB (the output cap), ~16k tokens, where one line would do.

A fixed wait on every read: `Fetch(100, FetchMaxWait(2*time.Second))` returns after 100 messages or two seconds, so every `bmesh inbox`, every `executor remove` (it drains the inbox) and every `wait` poll blocks two seconds (one for `FetchMatch`) even on an empty inbox. Measured on k16, 2026-10-15: verify is 57.1 s wall; `go test ./...` runs packages in parallel, so that is the longest package, `cmd/bmesh` at 54.1 s for 67 tests, each starting its own embedded server (`meshtest` starts in 0.04 s, so the server is not the cost). The slowest tests are the ones that read: `TestExecutorRemoveRefusesUnmerged` 8.3 s, `TestSendInboxRoundTrip` 8.1 s, `TestFetchAckRemovesUnread` 4.1 s (`internal/mesh`), `TestGlobalFlagsAfterSubcommand` 2.1 s for one `send` and two `inbox` calls. A lead pays the same two seconds at every checkpoint.

## Proposal
In:
- `Fetch` and `FetchMatch` use `FetchNoWait(100)`: what the inbox holds now, at once; a "no messages" status is an empty batch, not an error. The README's two-second mentions go.
- `routine wait` reads the reply from the stream, not the consumer: the inbox subject `bm.inbox.<path>` keeps every message for the stream's 30 days whether acked or not (the map's inbox panel already reads it that way, `internal/web/api.go:222`). `wait` scans that history for `kind: reply` with the job id, then follows the subject live until it arrives or `--timeout` passes; it never fetches from the consumer, so it reads and acks nothing else. A reply that `inbox` already printed is found the same way.
- `inbox` prints a reply as one line: `<at> <from>: reply to job <id>: <kind> ok|fail, <n> lines; bmesh routine wait <id> prints it`. `--json` keeps the whole message as now.
- `wait`'s default `--timeout` becomes the kind's `timeout` plus the daemon's `ackWaitExtra` (1 m) when the kind is known in the cwd repo, else 20 m: today both default to 20 m, so a review that runs to its own limit times out the lead's wait first.
- README (Jobs, Executors): the trap paragraph becomes the new rule; the lead and executor skills drop the "wait right after queueing" warnings.
Out: the inbox consumer's shape, the job and reply formats, anything in the daemon.

Judge: opus, because the behaviour is pinned here and the work is JetStream API detail.

## What you'll see
`bmesh inbox` on an empty inbox returns in well under a second. A lead reads its inbox, then `bmesh routine wait <id>` prints the review that was already delivered. `cmd/bmesh` tests drop by the two seconds per read they paid; the verify wall time should fall to the next-longest package (`internal/daemon`, 27.4 s today), to be confirmed by timing it in the build.

## Verification
The repo's verify (routine `verify`). New tests: `wait` after an `inbox` read of the same reply exits by the verdict within a second; `inbox` on an empty inbox returns under 500 ms; `inbox` prints a reply in one line and `--json` whole. The build thread reports `go test -count=1 ./cmd/bmesh` before and after.

## Risks and the alternative considered
`FetchNoWait` returns a 404/408 status message when nothing waits: handled as empty, with a test. Scanning history for a chatty inbox: the subject is one node's, bounded by the stream's age. Alternative: leave replies unacked in `inbox` so `wait` still finds them in the consumer; rejected, the consumer redelivers them on every read (AckWait 2 m) and the lead reads them again and again. Alternative: a shorter `FetchMaxWait` (200 ms); rejected, it is still a sleep on every read for messages that are already in the stream.

## Needs from the operator
None.
