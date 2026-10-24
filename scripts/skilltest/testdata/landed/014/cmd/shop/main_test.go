package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shop(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out, errs strings.Builder
	code := run(args, &out, &errs)
	return code, out.String() + errs.String()
}

func TestCheckoutThenReport(t *testing.T) {
	t.Setenv("SHOP_STORE", filepath.Join(t.TempDir(), "orders.json"))
	if code, out := shop(t, "checkout", "--code", "SAVE5", "apple=2", "cheese=1"); code != 0 || !strings.Contains(out, "order 1 saved") {
		t.Fatalf("checkout exit %d:\n%s", code, out)
	}
	if code, out := shop(t, "report", "--verbose"); code != 0 || !strings.Contains(out, "order 1 ") || !strings.Contains(out, "revenue  4.75") {
		t.Fatalf("report --verbose exit %d:\n%s", code, out)
	}
}

func TestCheckoutRefusesABadItem(t *testing.T) {
	t.Setenv("SHOP_STORE", filepath.Join(t.TempDir(), "orders.json"))
	if code, _ := shop(t, "checkout", "apple"); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if code, _ := shop(t, "checkout", "caviar=1"); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}

func TestCheckoutReadsTheCatalogFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHOP_STORE", filepath.Join(dir, "orders.json"))
	t.Setenv("SHOP_CATALOG", filepath.Join(dir, "catalog.json"))
	if err := os.WriteFile(filepath.Join(dir, "catalog.json"), []byte(`{"apple": 200, "pear": 120, "bread": 350, "cheese": 675}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := shop(t, "checkout", "apple=2"); code != 0 || !strings.Contains(out, "4.00") {
		t.Fatalf("checkout exit %d:\n%s", code, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalog.json"), []byte(`{"apple": 0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := shop(t, "checkout", "apple=2"); code != 1 || !strings.Contains(out, "catalog.json:1: apple is priced 0.00") {
		t.Fatalf("checkout with a price of 0: exit %d:\n%s", code, out)
	}
}
