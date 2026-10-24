package cart

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// catalogFile writes text as a catalog file and puts the built-in catalog
// back when the test ends.
func catalogFile(t *testing.T, text string) string {
	t.Helper()
	built := maps.Clone(Catalog)
	t.Cleanup(func() { Catalog = built })
	p := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadCatalogPrices(t *testing.T) {
	p := catalogFile(t, "{\n  \"apple\": 160,\n  \"plum\": 90\n}\n")
	if err := LoadCatalog(p); err != nil {
		t.Fatal(err)
	}
	var c Cart
	if err := c.Add("plum", 2); err != nil {
		t.Fatal(err)
	}
	if err := c.Add("bread", 1); err == nil {
		t.Fatal("bread is not in the file's catalog, but was added")
	}
	if got := c.Total(); got != 180 {
		t.Fatalf("total %s, want 1.80", Money(got))
	}
}

func TestLoadCatalogMissingFileKeepsTheBuiltIn(t *testing.T) {
	catalogFile(t, "{}")
	if err := LoadCatalog(filepath.Join(t.TempDir(), "catalog.json")); err != nil {
		t.Fatal(err)
	}
	if Catalog["bread"] != 350 {
		t.Fatalf("catalog %v, want the built-in one", Catalog)
	}
}

func TestLoadCatalogRefusesABadFile(t *testing.T) {
	for _, tc := range []struct{ name, text, want string }{
		{"malformed", "{\n  \"apple\": 150,\n  \"pear\" 120\n}\n", "catalog.json:3: "},
		{"a price that is not cents", "{\n  \"apple\": \"1.50\"\n}\n", "catalog.json:2: apple is not a price in cents"},
		{"a price of 0", "{\n  \"apple\": 150,\n  \"pear\": 0\n}\n", "catalog.json:3: pear is priced 0.00"},
		{"no items", "{}\n", "catalog.json: no items"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := catalogFile(t, tc.text)
			err := LoadCatalog(p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v, want one with %q", err, tc.want)
			}
			if Catalog["bread"] != 350 {
				t.Fatalf("a refused file changed the catalog: %v", Catalog)
			}
		})
	}
}
