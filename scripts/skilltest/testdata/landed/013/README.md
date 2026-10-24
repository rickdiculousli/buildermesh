# shop

A small shop on the command line: price a cart, check it out, and report on the orders.

```
go run ./cmd/shop checkout --code SAVE5 apple=2 pear=1
go run ./cmd/shop report --verbose
```

- `cart` prices a cart: the catalog, the subtotal, the discount codes and the receipt.
- `store` keeps the orders in one JSON file, `orders.json` (`SHOP_STORE` names another).
- `report` sums the orders up.
- `cmd/shop` is the command.

`shop checkout [--code CODE] ITEM=QTY...` prints the receipt and saves the order. The items are `apple`, `bread`, `cheese` and `pear`; the one code is `SAVE5`, which takes 5.00 off.

`shop report [--verbose] [--details]` prints the number of orders, the items sold and the revenue; `--verbose` lists each order first with its item count and total, and `--details` lists each order first with its items, one line each: `order 1  2026-10-20  apple x2, pear x1`.

`orders.json` is also read by the bookkeeping import, so its format is an interface others depend on.

Run the tests with `go test ./...`.
