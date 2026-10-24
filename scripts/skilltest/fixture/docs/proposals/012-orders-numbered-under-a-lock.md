---
id: 012
title: Orders are numbered under a lock, so two checkouts at once never share an id
kind: refinement
size: S
judge: opus
status: proposed
lens: eng
proposed_by: t3-5b2c9e41
proposed_on: 2026-10-23
---

## Opportunity
`store.Add` numbers a new order `len(orders) + 1` after reading the file. Two `shop checkout` runs at the same moment both read the same orders, both save order N+1, and the second rename drops the first order: `report` then counts one order where two were paid for.

## Proposal
`store.Add` takes `orders.json.lock` (created with `O_EXCL`, removed when the rename is done) around its read, append and rename, and a second Add waits up to two seconds for it. In: `store`. Out: any other storage.

Judge: opus, because a lock that outlives a crash has to be told apart from one that is held.

## What you'll see
Two checkouts started together save orders 4 and 5, and `report` counts both.

## Verification
`go test ./...`, with a new test that runs two `Add` calls at once and finds both orders with different ids.

## Risks and the alternative considered
A crash leaves the lock behind: the lock file holds the pid, and a lock whose process is gone is taken over. Considered: ids from the clock, which still lose an order to the second rename.

## Needs from the operator
