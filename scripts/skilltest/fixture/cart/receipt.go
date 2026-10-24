package cart

import (
	"fmt"
	"strings"
)

// Receipt is the cart as checkout prints it: a line per item, then the
// subtotal, the discount when the code takes something off, and the total.
func Receipt(c Cart) string {
	var b strings.Builder
	for _, it := range c.Items {
		fmt.Fprintf(&b, "%-10s %3d x %6s %9s\n", it.Name, it.Qty, Money(it.Price), Money(it.Price*int64(it.Qty)))
	}
	fmt.Fprintf(&b, "%-21s %9s\n", "Subtotal", Money(c.Subtotal()))
	if d := c.Discount(); d > 0 {
		fmt.Fprintf(&b, "%-21s %9s\n", "Discount "+c.Code, Money(-d))
	}
	fmt.Fprintf(&b, "%-21s %9s\n", "Totl", Money(c.Total()))
	return b.String()
}
