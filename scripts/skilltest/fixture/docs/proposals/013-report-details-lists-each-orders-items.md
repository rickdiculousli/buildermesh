---
id: 013
title: report --details lists each order with its items
kind: feature
size: S
judge: sonnet
status: proposed
lens: ux
proposed_by: t3-0d7a3f18
proposed_on: 2026-10-23
---

## Opportunity
`shop report` prints three totals. To see what was sold in one order, you open `orders.json` and read the JSON.

## Proposal
A `--details` flag on `shop report` prints each order before the totals: its id, its date and its items with their quantities. In: `report`, `cmd/shop`, the README. Out: filters by date.

Judge: sonnet, because it is one flag and one loop.

## What you'll see
`shop report --details` starts with `order 1  2026-10-20  apple x2`, one line per order.

## Verification
`go test ./...`, with a test of the lines `--details` prints.

## Risks and the alternative considered
The report grows long with many orders. Considered: a separate `shop orders` command, which is a second place to keep in step with the report.

## Needs from the operator
