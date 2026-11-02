---
id: 015
title: report --since counts the orders from a date
kind: feature
size: M
judge: opus
status: done
lens: eng
proposed_by: operator
proposed_on: 2026-10-24
---

## Opportunity
`shop report` sums every order ever saved. The bookkeeper closes each week by hand: they read `orders.json` and add up the orders since Monday.

## Proposal
A `--since YYYY-MM-DD` flag on `shop report` counts only the orders placed on that day or later. In: `report`, `cmd/shop`, the README. Out: an end date, time zones other than UTC.

Judge: opus, because the summary must never pass a partial count off as the whole.

## What you'll see
- `shop report --since 2026-10-21` counts only the orders placed on or after 2026-10-21, the day itself included.
- `shop report --verbose --since 2026-10-21` lists only those orders.
- The summary's first line names the window, `orders since 2026-10-21   1`, so a partial count is never read as the shop's whole.

## Verification
`go test ./...`, with a test that `--since` counts the day it names and a test of the summary's first line under `--since`.

## Shown
- counts from the day, the day itself included: `TestSinceCountsTheDayItNames` in report/report_test.go
- `--verbose --since` lists only those orders: `TestWriteSinceNamesTheWindow` in report/report_test.go
- the summary's first line names the window: `go run ./cmd/shop report --since 2026-10-21` -> `orders since 2026-10-21   1`, and `TestWriteSinceNamesTheWindow`
- Easy to miss: a bad date is refused, never read as no filter: `go run ./cmd/shop report --since 10/21` -> `shop: --since "10/21" is not a date (YYYY-MM-DD)`

## Risks and the alternative considered
A bad date is refused (exit 2), never read as no filter. Considered: a separate `shop week` command, a second report to keep in step.

## Needs from the operator
