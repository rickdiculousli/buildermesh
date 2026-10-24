package cart

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// LoadCatalog reads a catalog file, a JSON object of item names and their
// prices in cents, and makes it the catalog. A missing file leaves the
// catalog built in, so a shop with no file sells what it always did. A file
// that is not such an object, or that prices an item at less than one cent,
// is an error naming the file and its line; the catalog is left as it was.
func LoadCatalog(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		var syn *json.SyntaxError
		if errors.As(err, &syn) {
			return fmt.Errorf("%s:%d: %v", path, line(b, syn.Offset), err)
		}
		return fmt.Errorf("%s: %v", path, err)
	}
	if len(raw) == 0 {
		return fmt.Errorf("%s: no items", path)
	}
	prices := make(map[string]int64, len(raw))
	for name, v := range raw {
		at := line(b, int64(bytes.Index(b, []byte(fmt.Sprintf("%q", name)))))
		var price int64
		if err := json.Unmarshal(v, &price); err != nil {
			return fmt.Errorf("%s:%d: %s is not a price in cents", path, at, name)
		}
		if price < 1 {
			return fmt.Errorf("%s:%d: %s is priced %s; a price is at least 0.01", path, at, name, Money(price))
		}
		prices[name] = price
	}
	Catalog = prices
	return nil
}

// line is the line of b that offset falls on, counted from 1.
func line(b []byte, offset int64) int {
	if offset > int64(len(b)) {
		offset = int64(len(b))
	}
	if offset < 0 {
		offset = 0
	}
	return bytes.Count(b[:offset], []byte("\n")) + 1
}
