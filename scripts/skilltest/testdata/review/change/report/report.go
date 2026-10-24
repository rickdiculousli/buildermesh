// Package report sums the shop's orders up.
package report

import (
	"fmt"
	"io"
	"slices"

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

// From is the orders from the one with the id on, oldest first.
func From(orders []store.Order, id int) []store.Order {
	if id >= len(orders) {
		return nil
	}
	return orders[id:]
}

// Day is one day's orders.
type Day struct {
	Date    string // 2006-01-02
	Orders  int
	Revenue int64 // cents
}

// Days sums the orders up by the day they were made, oldest first.
func Days(orders []store.Order) []Day {
	var days []Day
	for _, o := range orders {
		date := o.At.Format("2006-01-02")
		if slices.ContainsFunc(days, func(d Day) bool { return d.Date == date }) {
			continue
		}
		d := Day{Date: date}
		for _, p := range orders {
			if p.At.Format("2006-01-02") == date {
				d.Orders++
				d.Revenue += p.Total
			}
		}
		days = append(days, d)
	}
	return days
}

// WriteDays prints a line per day: its date, orders and revenue.
func WriteDays(w io.Writer, orders []store.Order) error {
	for _, d := range Days(orders) {
		if _, err := fmt.Fprintf(w, "%s  %d orders  %s\n", d.Date, d.Orders, cart.Money(d.Revenue)); err != nil {
			return err
		}
	}
	return nil
}
