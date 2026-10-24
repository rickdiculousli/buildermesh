package store

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

func TestAddsAtOnceKeepEveryOrder(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "orders.json"))
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Add(Order{At: time.Now().UTC(), Items: []cart.Item{{Name: "pear", Price: 120, Qty: 1}}, Total: 120})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	orders, err := s.Orders()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[int]bool{}
	for _, o := range orders {
		ids[o.ID] = true
	}
	if len(orders) != n || len(ids) != n {
		t.Fatalf("%d orders with %d ids after %d checkouts at once, want %d of each", len(orders), len(ids), n, n)
	}
	if _, err := os.Stat(s.Path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the lock is left behind: %v", err)
	}
}

func TestAddTakesOverACrashedLock(t *testing.T) {
	// A process that has exited stands for a checkout that crashed holding it.
	gone := exec.Command(os.Args[0], "-test.run=^$")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	s := Open(filepath.Join(t.TempDir(), "orders.json"))
	if err := os.WriteFile(s.Path+".lock", []byte(strconv.Itoa(gone.ProcessState.Pid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o, err := s.Add(Order{Total: 150}); err != nil || o.ID != 1 {
		t.Fatalf("order %+v, err %v; want order 1 saved", o, err)
	}
}

func TestAddWaitsForAHeldLock(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "orders.json"))
	if err := os.WriteFile(s.Path+".lock", []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := s.Add(Order{Total: 150}); err == nil || !strings.Contains(err.Error(), "orders.json.lock") {
		t.Fatalf("err %v, want one naming the lock", err)
	}
	if waited := time.Since(start); waited < lockWait {
		t.Fatalf("gave up after %s, want %s", waited, lockWait)
	}
	if orders, _ := s.Orders(); len(orders) != 0 {
		t.Fatalf("saved %+v without the lock", orders)
	}
}
