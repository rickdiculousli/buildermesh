---
id: 025
title: A daemon that stops hands its held jobs back
kind: refinement
size: S
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-16
from_feedback: retrospective of 007-016 and 018 (buildermesh-007, -011)
built_by: 8b924fe0-b2da-4104-9e95-6c93ebcb790f
host: k16
family: buildermesh-build-2
branch: bmesh/8b924fe0-b2da-4104-9e95-6c93ebcb790f
---

## Opportunity
A pooled instance holds the jobs it has fetched until its batch runs. When the daemon stops, those jobs stay ack-pending on a dead consumer until AckWait (21m, `internal/mesh/jobs.go`) passes, and `routine wait` shows nothing. k16 now restarts its daemon on every landing (011), so any lead with a review in flight loses about 21 minutes. 007 lost 30.

## Proposal
In: on SIGTERM or SIGINT, and before an `[update]` re-exec, the daemon Naks every job its instances hold but haven't finished, and kills a running job's process group, so the next daemon redelivers it at once. A job that was mid-run is Nak'd too; deterministic kinds answer a repeat from their record anyway. `routine status <kind>` shows waiting and held jobs by id and holder. Out: changing AckWait.

Judge: opus, because shutdown ordering against in-flight runs is easy to get subtly wrong.

## What you'll see
A lead's review queued just before a landing comes back within a minute or two of the restart instead of after 21.

## Verification
Verify, plus a daemon test: queue a job to a pooled kind, hold it, stop the daemon, start another, and the job runs without waiting out AckWait.

## Risks and the alternative considered
A model job killed mid-run is paid for twice. The alternative, a short AckWait with progress acks, already exists (`AckWait/3` in-progress acks) and doesn't help when the holder is dead.

## Needs from the operator
None.
