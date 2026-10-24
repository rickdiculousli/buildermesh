// Package cart prices a cart: the catalog, the subtotal, the discount a code
// takes off and the total.
package cart

import (
	"fmt"
	"sort"
)

// Item is one line of a cart: a catalog item, its price in cents and how
// many.
type Item struct {
	Name  string `json:"name"`
	Price int64  `json:"price"`
	Qty   int    `json:"qty"`
}

// Cart is what a customer picked, and the discount code they entered.
type Cart struct {
	Items []Item
	Code  string
}

// Catalog is every item the shop sells, with its price in cents.
var Catalog = map[string]int64{
	"apple":  150,
	"pear":   120,
	"bread":  350,
	"cheese": 675,
}

// Codes are the discount codes, each with the cents it takes off.
var Codes = map[string]int64{
	"SAVE5": 500,
}

// Names lists the catalog's items in order.
func Names() []string {
	names := make([]string, 0, len(Catalog))
	for n := range Catalog {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Add puts qty of the named catalog item in the cart.
func (c *Cart) Add(name string, qty int) error {
	price, ok := Catalog[name]
	if !ok {
		return fmt.Errorf("no item %q in the catalog", name)
	}
	if qty < 1 {
		return fmt.Errorf("%s: quantity %d is not a positive number", name, qty)
	}
	c.Items = append(c.Items, Item{Name: name, Price: price, Qty: qty})
	return nil
}

// Subtotal is the sum of the items, in cents.
func (c Cart) Subtotal() int64 {
	var sum int64
	for _, it := range c.Items {
		sum += it.Price * int64(it.Qty)
	}
	return sum
}

// Discount is what the cart's code adds to the subtotal, in cents: a
// negative amount, as the report and the receipt print it; an unknown code
// adds nothing.
func (c Cart) Discount() int64 {
	return -Codes[c.Code]
}

// Total is what the customer pays, in cents.
func (c Cart) Total() int64 {
	return c.Subtotal() + c.Discount()
}

// Money formats cents as 12.34.
func Money(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}
