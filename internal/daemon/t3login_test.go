package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
)

func TestT3LoginPushesOncePerThreshold(t *testing.T) {
	f := setup(t)
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-ntfy.sh"))
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "ntfy.log")
	t.Setenv("FAKE_NTFY_LOG", log)
	f.d.Cfg.Notify.Command = "sh " + fake
	dir := t.TempDir()
	f.d.T3Token = filepath.Join(dir, "t3-token.json")
	f.d.StateDir = filepath.Join(dir, "state")
	records := func() int {
		b, _ := os.ReadFile(log)
		return strings.Count(string(b), "---\n")
	}
	last := func() string {
		b, _ := os.ReadFile(log)
		return string(b)
	}
	day := 24 * time.Hour
	login := func(expires time.Time) {
		t.Helper()
		if err := t3mcp.WriteToken(f.d.T3Token, t3mcp.Token{Server: "http://t3", Token: "x", ExpiresAt: expires}); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Unix(2_000_000_000, 0)
	exp := base.Add(30 * day)
	login(exp)
	at := func(remaining time.Duration) { f.now = exp.Add(-remaining) }

	// no token elsewhere: nothing
	dirTok := f.d.T3Token
	f.d.T3Token = filepath.Join(dir, "none.json")
	at(2 * day)
	t3LoginOnce(f.ctx, f.d)
	if records() != 0 {
		t.Fatal("pushed without a token")
	}
	f.d.T3Token = dirTok

	at(10 * day)
	t3LoginOnce(f.ctx, f.d)
	if records() != 0 {
		t.Fatal("pushed at 10 days")
	}
	at(6*day + 12*time.Hour)
	t3LoginOnce(f.ctx, f.d)
	if records() != 1 || !strings.HasPrefix(last(), "🔑 Expiring • T3 sign-in on "+testHost+" in 7 days\nRun bmesh t3 login on "+testHost+". ") {
		t.Fatalf("6.5 days: %d %q", records(), last())
	}
	if !strings.Contains(last(), "http://t3") {
		t.Errorf("log %q", last())
	}
	t3LoginOnce(f.ctx, f.d)
	d2 := f.d // a restart: fresh Deps, same StateDir
	t3LoginOnce(f.ctx, d2)
	if records() != 1 {
		t.Fatalf("repeat pushed: %d", records())
	}
	at(2*day + 12*time.Hour)
	t3LoginOnce(f.ctx, f.d)
	if records() != 2 || !strings.Contains(last(), "T3 sign-in on "+testHost+" in 3 days\n") {
		t.Fatalf("2.5 days: %d", records())
	}
	at(12 * time.Hour)
	t3LoginOnce(f.ctx, f.d)
	if records() != 3 || !strings.Contains(last(), "T3 sign-in on "+testHost+" in 1 day\n") {
		t.Fatalf("0.5 days: %d %q", records(), last())
	}
	at(-time.Hour)
	t3LoginOnce(f.ctx, f.d)
	if records() != 3 {
		t.Fatal("pushed after expiry")
	}
	if fi, err := os.Stat(filepath.Join(f.d.StateDir, t3PushesFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("record: %v %v", fi, err)
	}

	// a jump from 8 days to 2 days: only the 3-day push, never a 7-day one after
	f.d.StateDir = filepath.Join(dir, "state2")
	at(8 * day)
	t3LoginOnce(f.ctx, f.d)
	at(2 * day)
	t3LoginOnce(f.ctx, f.d)
	if records() != 4 {
		t.Fatalf("jump: %d", records())
	}
	at(2*day - time.Minute)
	t3LoginOnce(f.ctx, f.d)
	if records() != 4 {
		t.Fatalf("after jump: %d", records())
	}

	// a new login resets the record
	exp = base.Add(36 * day)
	login(exp)
	at(6 * day)
	t3LoginOnce(f.ctx, f.d)
	if records() != 5 {
		t.Fatalf("new token: %d", records())
	}
}
