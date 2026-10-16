package t3mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrNoToken means the box has no T3 sign-in.
var ErrNoToken = errors.New("t3mcp: no T3 sign-in")

// Token is an MCP client session: the server it is for, the bearer token,
// when it expires, its scopes and its runtime-mode ceiling (Access:
// approval-required, auto-accept-edits, auto or full-access; read-only
// without orchestration:operate).
type Token struct {
	Server    string    `json:"server"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	Scopes    []string  `json:"scopes"`
	Access    string    `json:"access"`
}

// Left is how long t has before it expires at now.
func (t Token) Left(now time.Time) time.Duration { return t.ExpiresAt.Sub(now) }

// DaysLeft is Left in whole days, rounded up (0 once expired).
func (t Token) DaysLeft(now time.Time) int {
	left := t.Left(now)
	if left <= 0 {
		return 0
	}
	return int((left + 24*time.Hour - 1) / (24 * time.Hour))
}

// Credential is where the runner gets its token. The file a login writes
// is one source; a source that renews itself can replace it without
// touching the runner.
type Credential interface {
	Token(ctx context.Context) (Token, error)
}

// FileCredential reads the token `bmesh t3 login` wrote at Path.
type FileCredential struct{ Path string }

func (f FileCredential) Token(context.Context) (Token, error) { return ReadToken(f.Path) }

// ReadToken reads a token file; ErrNoToken when there is none.
func ReadToken(path string) (Token, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Token{}, ErrNoToken
	}
	if err != nil {
		return Token{}, err
	}
	var t Token
	if err := json.Unmarshal(b, &t); err != nil {
		return Token{}, fmt.Errorf("%s: %w", path, err)
	}
	if t.Token == "" {
		return Token{}, ErrNoToken
	}
	return t, nil
}

// WriteToken writes t to path, owner-only, through a temporary file so a
// reader never sees half of it.
func WriteToken(path string, t Token) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".t3-token-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// RuntimeRank orders T3's runtime modes as T3 does (approval-required 0 ..
// full-access 3); anything else, read-only included, is -1.
func RuntimeRank(mode string) int {
	switch mode {
	case "approval-required":
		return 0
	case "auto-accept-edits":
		return 1
	case "auto":
		return 2
	case "full-access":
		return 3
	}
	return -1
}

// SameServer reports whether two server URLs name one server: scheme, port
// and host, with localhost, 127.0.0.1 and [::1] counted as one host.
func SameServer(a, b string) bool {
	ua, err1 := url.Parse(strings.TrimRight(a, "/"))
	ub, err2 := url.Parse(strings.TrimRight(b, "/"))
	if err1 != nil || err2 != nil {
		return false
	}
	host := func(u *url.URL) string {
		h := u.Hostname()
		if h == "localhost" || h == "::1" {
			h = "127.0.0.1"
		}
		return h
	}
	return ua.Scheme == ub.Scheme && ua.Port() == ub.Port() && host(ua) == host(ub)
}
