# The review-spec cases' promises

The `review-spec-miss` and `review-spec-met` cases review one commit on the `shop` sandbox: `base/` laid over the fixture is the base (origin's `main`), adding spec 015 sized M and `proposed`, whose `What you'll see` makes three promises: `report --since` counts from the day it names, the day included; `--verbose --since` lists only those orders; and the summary's first line names the window (`orders since 2026-10-21   1`). The change is `miss/` or `met/` laid over the base, committed as `report --since counts the orders from a date`. Both pass the fixture's tests, plant no bug, and set 015 `done` with a `## Shown` section.

- `miss/` keeps the first two promises and drops the third: the summary still opens `orders   1` under `--since`, and the test of that line its Verification asks for is missing. Nothing but the spec says that is wrong, and its `Shown` is silent on it. A finding about either names the dropped promise.
- `met/` keeps all three, and its `Shown` names a line and a test for each.

Real change: `Since`, `TestSinceCountsTheDayItNames`, `TestWriteSinceListsOnlyThoseOrders`, `TestReportSince`, `reportCmd`. The day filter, the verbose list and the bad-date refusal are right; a finding about them, short of a nice-to-have, that names no dropped promise fails `review-spec-miss`.

A dropped promise counts as named as a planted defect does in the `review` case (`testdata/review/defects.md`): one finding names one of its files, one of its names as a word or a line in its range, and one of its cues.

| defect | class | files | names | lines | cues | what breaks |
|---|---|---|---|---|---|---|
| window-label | a dropped promise of the spec | report/report.go; cmd/shop/main.go; report/report_test.go; cmd/shop/main_test.go; docs/proposals/015-report-since-counts-from-a-date.md | WriteSince; --since; orders since | report/report.go:52-55 | orders since; first line; names the window; window; label; partial; whole | 015 promises the summary's first line names the window (`orders since 2026-10-21   1`), but `WriteSince` calls `Write`, which prints `orders   1`: the partial count reads as the shop's whole. |
