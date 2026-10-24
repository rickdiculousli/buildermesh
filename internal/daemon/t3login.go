package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/notify"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
)

// t3Thresholds are the days-left marks that each push once, largest first.
var t3Thresholds = []int{7, 3, 1}

const t3PushesFile = "t3-login-pushes.json"

// t3Pushes is the record of which warnings went out for one token.
type t3Pushes struct {
	ExpiresAt time.Time `json:"expiresAt"`
	Sent      []int     `json:"sent"`
}

// t3LoginOnce warns, once per threshold across restarts, that the T3
// sign-in is close to expiring.
func t3LoginOnce(ctx context.Context, d Deps) {
	tok, err := t3mcp.ReadToken(d.T3Token)
	if errors.Is(err, t3mcp.ErrNoToken) {
		return
	}
	if err != nil {
		d.Log.Warn("t3 login: read token", "err", err)
		return
	}
	now := d.Now()
	left := tok.Left(now)
	if left <= 0 {
		return
	}
	due := 0
	for _, th := range t3Thresholds {
		if left <= time.Duration(th)*24*time.Hour {
			due = th
		}
	}
	if due == 0 {
		return
	}
	path := filepath.Join(d.StateDir, t3PushesFile)
	rec := t3Pushes{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &rec)
	}
	if !rec.ExpiresAt.Equal(tok.ExpiresAt) {
		rec = t3Pushes{ExpiresAt: tok.ExpiresAt}
	}
	if slices.Contains(rec.Sent, due) {
		return
	}
	n := tok.DaysLeft(now)
	unit := "days"
	if n == 1 {
		unit = "day"
	}
	title := notify.Title("🔑", "Expiring", fmt.Sprintf("T3 sign-in on %s in %d %s", d.Host, n, unit))
	body := fmt.Sprintf("Run bmesh t3 login on %s. The sign-in for %s expires at %s.",
		d.Host, tok.Server, tok.ExpiresAt.UTC().Format(time.RFC3339))
	if err := deliver(ctx, d, notify.Headers{Title: title}, body); err != nil {
		d.Log.Warn("t3 login: push failed", "err", err)
		return
	}
	for _, th := range t3Thresholds {
		if th >= due && !slices.Contains(rec.Sent, th) {
			rec.Sent = append(rec.Sent, th)
		}
	}
	if err := writeT3Pushes(path, rec); err != nil {
		d.Log.Warn("t3 login: record push", "err", err)
	}
	d.Log.Info("t3 login: pushed " + title)
}

func writeT3Pushes(path string, rec t3Pushes) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, t3PushesFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
