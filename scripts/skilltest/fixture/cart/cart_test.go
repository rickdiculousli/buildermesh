package cart

import (
	"strings"
	"testing"
)

func basket(t *testing.T) Cart {
	t.Helper()
	var c Cart
	if err := c.Add("apple", 2); err != nil {
		t.Fatal(err)
	}
	if err := c.Add("bread", 1); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSubtotal(t *testing.T) {
	if got := basket(t).Subtotal(); got != 650 {
		t.Fatalf("subtotal %d, want 650", got)
	}
}

func TestTotalWithCode(t *testing.T) {
	c := basket(t)
	c.Code = "SAVE5"
	if got := c.Total(); got != 150 {
		t.Fatalf("total %d, want 150", got)
	}
}

func TestUnknownCodeTakesNothingOff(t *testing.T) {
	c := basket(t)
	c.Code = "NOPE"
	if got := c.Total(); got != 650 {
		t.Fatalf("total %d, want 650", got)
	}
}

func TestAddRefuses(t *testing.T) {
	var c Cart
	if err := c.Add("caviar", 1); err == nil {
		t.Fatal("an item outside the catalog was added")
	}
	if err := c.Add("apple", 0); err == nil {
		t.Fatal("a quantity of 0 was added")
	}
}

func TestMoney(t *testing.T) {
	for cents, want := range map[int64]string{0: "0.00", 5: "0.05", 1234: "12.34", -500: "-5.00"} {
		if got := Money(cents); got != want {
			t.Errorf("Money(%d) = %q, want %q", cents, got, want)
		}
	}
}

func TestReceiptListsItems(t *testing.T) {
	r := Receipt(basket(t))
	for _, want := range []string{"apple", "bread", "Subtotal", "6.50"} {
		if !strings.Contains(r, want) {
			t.Errorf("receipt has no %q:\n%s", want, r)
		}
	}
}
