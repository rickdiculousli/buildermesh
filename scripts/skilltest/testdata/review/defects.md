# The review case's planted defects

The `review` case reviews one commit on the `shop` sandbox: `base/` laid over the fixture is the base (origin's `main`), and `change/` laid over that is the change, committed as `report --daily and --from; checkout prints the receipt once the order is saved; a discount is a negative amount`. Both pass the fixture's tests, as a change does once verify has run.

The change holds four planted defects, one per class the 2026-10-15 review run used that `shop` can carry. Each is found only by reading past the diff: the code that makes it wrong, or the caller it breaks, is in a file or function the diff doesn't show.

Real change: `WriteDays`, `reportCmd`, `--daily`, `TestWriteDays`, `TestReportDaily`. The day report, its flag and their tests, and the receipt printed after the save, are right; a finding about them that names no defect is a false alarm.

A defect counts as named when one finding of the reply (a section under a heading, else a numbered item, else the whole reply; the `Not checked:` and `VERDICT:` lines left out) names one of its files (the base name will do), one of its names as a word or a line in one of its ranges (`report.go:57`, or `line 57` in a finding that names the file), and one of its cues (any case).

| defect | class | files | names | lines | cues | what breaks |
|---|---|---|---|---|---|---|
| off-by-one | a logic off-by-one | report/report.go; cmd/shop/main.go | From; --from | report/report.go:52-58 | off-by-one; off by one; ids start; start at 1; starts at 1; 1-based; one-based; id-1; id - 1; skips; drops; excludes; leaves out; never included; newest; latest; last order; index | Order ids start at 1 (`store.Add`, outside the diff), so `From(orders, id)` returns `orders[id:]`, which leaves out the order with that id, and `--from` with the newest order's id reports nothing. |
| regression | a regression whose caller is outside the diff | cart/receipt.go; cart/cart.go | Receipt; receipt | cart/receipt.go:10-20 | discount | `Discount` now returns a negative amount and `Total` adds it, but `Receipt` (`cart/receipt.go`, outside the diff) still prints the discount line only when `d > 0`: a receipt never shows the discount again. |
| removed-guard | a removed guard | cmd/shop/main.go | checkout; c.Total() < 0 | cmd/shop/main.go:50-80 | guard; negative; below zero; -5.00; empty cart; refus; < 0 | Moving the receipt after the save dropped `checkout`'s refusal of a negative total; `Total` (outside the diff) is negative for an empty cart with `SAVE5`, so that order is saved at -5.00. |
| quadratic | a quadratic loop | report/report.go | Days | report/report.go:66-86 | quadratic; O(n²); O(n^2); O(n*n); n²; n^2; nested; every order; each order; all orders; rescan; per order | `Days` scans every order for each order (and every day so far): quadratic in the orders, which the store keeps forever (`orders.json`, never pruned), and `report --daily` runs it on every call. |
