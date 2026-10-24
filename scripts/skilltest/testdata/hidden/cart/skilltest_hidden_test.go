// The hidden checks skilltest copies into a checkout of the sandbox origin's
// main at scoring time; the session under test never sees this file.
package cart_test

import (
	"strings"
	"testing"

	"example.com/shop/cart"
)

// The bug case: an empty cart with a discount code totals 0.00.
func TestSkilltestHiddenEmptyCartTotal(t *testing.T) {
	if got := (cart.Cart{Code: "SAVE5"}).Total(); got != 0 {
		t.Fatalf("an empty cart with SAVE5 totals %s, want 0.00", cart.Money(got))
	}
}

// The qa case: the bug the operator didn't pick is still there.
func TestSkilltestHiddenEmptyCartStillNegative(t *testing.T) {
	if got := (cart.Cart{Code: "SAVE5"}).Total(); got >= 0 {
		t.Fatalf("an empty cart with SAVE5 totals %s: the -5.00 bug was fixed, though only the typo was picked", cart.Money(got))
	}
}

// The qa case: the receipt says Total, never Totl.
func TestSkilltestHiddenReceiptSaysTotal(t *testing.T) {
	r := cart.Receipt(cart.Cart{Items: []cart.Item{{Name: "apple", Price: 150, Qty: 2}}})
	if !strings.Contains(r, "Total") || strings.Contains(r, "Totl") {
		t.Fatalf("receipt:\n%s", r)
	}
}
