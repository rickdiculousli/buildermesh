// Package report sums the shop's orders up.
package report

import (
	"fmt"
	"io"

	"example.com/shop/cart"
	"example.com/shop/store"
)

// Summary is the orders at a glance.
type Summary struct {
	Orders  int
	Items   int
	Revenue int64 // cents
}

// Summarize sums the orders up.
func Summarize(orders []store.Order) Summary {
	s := Summary{Orders: len(orders)}
	for _, o := range orders {
		s.Items += items(o)
		s.Revenue += o.Total
	}
	return s
}

func items(o store.Order) int {
	n := 0
	for _, it := range o.Items {
		n += it.Qty
	}
	return n
}

// Write prints the summary; verbose lists each order first.
func Write(w io.Writer, orders []store.Order, verbose bool) error {
	if verbose {
		for _, o := range orders {
			if _, err := fmt.Fprintf(w, "order %d  %s  %d items  %s\n", o.ID, o.At.Format("2006-01-02"), items(o), cart.Money(o.Total)); err != nil {
				return err
			}
		}
	}
	s := Summarize(orders)
	_, err := fmt.Fprintf(w, "orders   %d\nitems    %d\nrevenue  %s\n", s.Orders, s.Items, cart.Money(s.Revenue))
	return err
}
