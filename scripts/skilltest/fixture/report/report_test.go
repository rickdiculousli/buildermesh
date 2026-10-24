package report

import (
	"strings"
	"testing"
	"time"

	"example.com/shop/cart"
	"example.com/shop/store"
)

var orders = []store.Order{
	{ID: 1, At: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), Items: []cart.Item{{Name: "apple", Price: 150, Qty: 2}}, Total: 300},
	{ID: 2, At: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC), Items: []cart.Item{{Name: "bread", Price: 350, Qty: 1}, {Name: "pear", Price: 120, Qty: 1}}, Total: 470},
}

func TestSummarize(t *testing.T) {
	if got := Summarize(orders); got != (Summary{Orders: 2, Items: 4, Revenue: 770}) {
		t.Fatalf("summary %+v", got)
	}
}

func TestWrite(t *testing.T) {
	var b strings.Builder
	if err := Write(&b, orders, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "revenue  7.70") || strings.Contains(b.String(), "order 1") {
		t.Fatalf("report:\n%s", b.String())
	}
}

func TestWriteVerboseListsOrders(t *testing.T) {
	var b strings.Builder
	if err := Write(&b, orders, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "order 2  2026-10-21  2 items  4.70") {
		t.Fatalf("report:\n%s", b.String())
	}
}
