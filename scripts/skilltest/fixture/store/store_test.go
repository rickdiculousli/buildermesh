package store

import (
	"path/filepath"
	"testing"
	"time"

	"example.com/shop/cart"
)

func TestMissingFileIsNoOrders(t *testing.T) {
	orders, err := Open(filepath.Join(t.TempDir(), "orders.json")).Orders()
	if err != nil || len(orders) != 0 {
		t.Fatalf("orders %v, err %v; want none", orders, err)
	}
}

func TestAddKeepsEveryOrder(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "orders.json"))
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	for i, total := range []int64{300, 450} {
		o, err := s.Add(Order{At: at, Items: []cart.Item{{Name: "apple", Price: 150, Qty: 2}}, Total: total})
		if err != nil {
			t.Fatal(err)
		}
		if o.ID != i+1 {
			t.Fatalf("order %d got id %d", i+1, o.ID)
		}
	}
	orders, err := s.Orders()
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 2 || orders[1].Total != 450 || !orders[0].At.Equal(at) {
		t.Fatalf("read back %+v", orders)
	}
}
