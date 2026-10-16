package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
)

func TestStatusT3SignInLine(t *testing.T) {
	setup(t)
	cfg, err := config.Load(os.Getenv("BMESH_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	path := cfg.T3TokenPath()
	status := func() string {
		t.Helper()
		out, errs, code := runCLI(t, nil, "mesh-status")
		if code != 0 {
			t.Fatal(code, errs)
		}
		return out
	}
	if out := status(); strings.Contains(out, "t3 sign-in") {
		t.Fatalf("no token, got %q", out)
	}
	write := func(left time.Duration) {
		t.Helper()
		if err := t3mcp.WriteToken(path, t3mcp.Token{Server: "s", Token: "x", ExpiresAt: time.Now().Add(left)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		left time.Duration
		want string
	}{
		{20 * 24 * time.Hour, ""},
		{3*24*time.Hour - time.Hour, "t3 sign-in on " + testHost + ": expires in 3 days (bmesh t3 login)\n"},
		{time.Hour, "t3 sign-in on " + testHost + ": expires in 1 day (bmesh t3 login)\n"},
		{-time.Hour, "t3 sign-in on " + testHost + ": expired (bmesh t3 login)\n"},
	} {
		write(c.left)
		out := status()
		if c.want == "" {
			if strings.Contains(out, "t3 sign-in") {
				t.Errorf("left %v: %q", c.left, out)
			}
		} else if !strings.HasSuffix(out, c.want) {
			t.Errorf("left %v: %q, want suffix %q", c.left, out, c.want)
		}
	}
}
