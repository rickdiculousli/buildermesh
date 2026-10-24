// Command shop prices a cart, checks it out and reports on the orders.
//
//	shop checkout [--code CODE] ITEM=QTY...
//	shop report [--verbose] [--daily] [--from ID]
//
// The orders are kept in orders.json, or in the file SHOP_STORE names.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"example.com/shop/cart"
	"example.com/shop/report"
	"example.com/shop/store"
)

const usage = "usage: shop checkout [--code CODE] ITEM=QTY... | shop report [--verbose] [--daily] [--from ID]"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	st := store.Open(storePath())
	switch args[0] {
	case "checkout":
		return checkout(st, args[1:], stdout, stderr)
	case "report":
		return reportCmd(st, args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "shop: unknown command %q\n%s\n", args[0], usage)
	return 2
}

func storePath() string {
	if p := os.Getenv("SHOP_STORE"); p != "" {
		return p
	}
	return "orders.json"
}

func checkout(st *store.Store, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("checkout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	code := fs.String("code", "", "a discount code")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	c := cart.Cart{Code: *code}
	for _, a := range fs.Args() {
		name, q, ok := strings.Cut(a, "=")
		qty, err := strconv.Atoi(q)
		if !ok || err != nil {
			fmt.Fprintf(stderr, "shop: %q is not ITEM=QTY (items: %s)\n", a, strings.Join(cart.Names(), ", "))
			return 2
		}
		if err := c.Add(name, qty); err != nil {
			fmt.Fprintln(stderr, "shop:", err)
			return 1
		}
	}
	// The receipt is printed once the order is saved, so a failed save
	// prints no receipt for an order the shop doesn't have.
	o, err := st.Add(store.Order{At: time.Now().UTC(), Items: c.Items, Code: c.Code, Total: c.Total()})
	if err != nil {
		fmt.Fprintln(stderr, "shop:", err)
		return 1
	}
	fmt.Fprint(stdout, cart.Receipt(c))
	fmt.Fprintf(stdout, "order %d saved\n", o.ID)
	return 0
}

func reportCmd(st *store.Store, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	verbose := fs.Bool("verbose", false, "list each order before the summary")
	daily := fs.Bool("daily", false, "print the orders and revenue of each day instead of the summary")
	from := fs.Int("from", 0, "report only the orders from the one with this id on")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	orders, err := st.Orders()
	if err != nil {
		fmt.Fprintln(stderr, "shop:", err)
		return 1
	}
	if *from > 0 {
		orders = report.From(orders, *from)
	}
	write := func() error { return report.Write(stdout, orders, *verbose) }
	if *daily {
		write = func() error { return report.WriteDays(stdout, orders) }
	}
	if err := write(); err != nil {
		fmt.Fprintln(stderr, "shop:", err)
		return 1
	}
	return 0
}
