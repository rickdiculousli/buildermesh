package main

import (
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
