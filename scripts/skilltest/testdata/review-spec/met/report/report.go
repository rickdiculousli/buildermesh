// Package report sums the shop's orders up.
package report

import (
	"fmt"
	"io"
	"time"

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
	return write(w, orders, verbose, "orders")
}

// WriteSince prints the summary of the orders placed on day or later;
// its first line names the day, so the count is never read as the whole.
func WriteSince(w io.Writer, orders []store.Order, verbose bool, day time.Time) error {
	return write(w, Since(orders, day), verbose, "orders since "+day.Format("2006-01-02"))
}

// Since is the orders placed on day or later, day itself included.
func Since(orders []store.Order, day time.Time) []store.Order {
	var out []store.Order
	for _, o := range orders {
		if !o.At.Before(day) {
			out = append(out, o)
		}
	}
	return out
}

func write(w io.Writer, orders []store.Order, verbose bool, label string) error {
	if verbose {
		for _, o := range orders {
			if _, err := fmt.Fprintf(w, "order %d  %s  %d items  %s\n", o.ID, o.At.Format("2006-01-02"), items(o), cart.Money(o.Total)); err != nil {
				return err
			}
		}
	}
	s := Summarize(orders)
	_, err := fmt.Fprintf(w, "%s   %d\nitems    %d\nrevenue  %s\n", label, s.Orders, s.Items, cart.Money(s.Revenue))
	return err
}
