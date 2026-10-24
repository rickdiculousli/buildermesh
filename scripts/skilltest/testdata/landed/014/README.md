# shop

A small shop on the command line: price a cart, check it out, and report on the orders.

```
go run ./cmd/shop checkout --code SAVE5 apple=2 pear=1
go run ./cmd/shop report --verbose
```

- `cart` prices a cart: the catalog (built in, or read from `catalog.json`), the subtotal, the discount codes and the receipt.
- `store` keeps the orders in one JSON file, `orders.json` (`SHOP_STORE` names another).
- `report` sums the orders up.
- `cmd/shop` is the command.

`shop checkout [--code CODE] ITEM=QTY...` prints the receipt and saves the order. The items and their prices are read from `catalog.json` (`SHOP_CATALOG` names another), a JSON object of item names and prices in cents such as `{"apple": 150, "pear": 120}`; with no such file the items are `apple`, `bread`, `cheese` and `pear`, as built in. A malformed file, or a price under 0.01, stops checkout with an error naming the file and the line. The one code is `SAVE5`, which takes 5.00 off.

`shop report [--verbose]` prints the number of orders, the items sold and the revenue; `--verbose` lists each order first.

`orders.json` is also read by the bookkeeping import, so its format is an interface others depend on.

Run the tests with `go test ./...`.
