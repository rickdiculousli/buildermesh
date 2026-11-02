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

func TestSinceCountsTheDayItNames(t *testing.T) {
	got := Since(orders, time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("since 2026-10-21: %+v", got)
	}
	if got := Since(orders, time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)); len(got) != 0 {
		t.Fatalf("since 2026-10-22: %+v", got)
	}
}

func TestWriteSinceNamesTheWindow(t *testing.T) {
	var b strings.Builder
	if err := WriteSince(&b, orders, true, time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if want := "order 2  2026-10-21  2 items  4.70\norders since 2026-10-21   1\nitems    2\nrevenue  4.70\n"; b.String() != want {
		t.Fatalf("report:\n%s\nwant:\n%s", b.String(), want)
	}
}
