package web

import (
	"context"
	"encoding/json"
	"github.com/rickdiculousli/buildermesh/internal/shell"
	"sync"
	"time"
)

const (
	quotaEvery   = 5 * time.Minute // never poll faster: the upstream page rate-limits
	quotaTimeout = 30 * time.Second
)

// Reading is one provider's quota as the strip shows it; ReadAt is when the
// command that produced it ran.
type Reading struct {
	Provider  string  `json:"provider"`
	FiveHour  float64 `json:"five_hour"`
	Week      float64 `json:"week"`
	FiveReset string  `json:"five_reset"`
	WeekReset string  `json:"week_reset"`
	ReadAt    string  `json:"read_at"`
}

// Quota runs Command (through the platform shell) at most once per five minutes and keeps
// the readings of its last good run; a failed run (non-zero exit, bad JSON,
// timeout) leaves them as they were.
type Quota struct {
	Command string

	mu    sync.Mutex
	tried time.Time // the last run, good or not
	good  []Reading
}

// Get returns the last good readings, running the command first when the
// last run is five minutes old; [] before any good run. The run is detached
// from ctx (a dropped request does not spend the slot) and made without the
// lock, so a caller arriving mid-run gets the last good readings at once.
func (q *Quota) Get(ctx context.Context, now time.Time) []Reading {
	q.mu.Lock()
	due := q.Command != "" && (q.tried.IsZero() || now.Sub(q.tried) >= quotaEvery)
	if due {
		q.tried = now
	}
	q.mu.Unlock()
	if due {
		if rs, ok := q.run(context.WithoutCancel(ctx), now); ok {
			q.mu.Lock()
			q.good = rs
			q.mu.Unlock()
		}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]Reading{}, q.good...)
}

func (q *Quota) run(ctx context.Context, now time.Time) ([]Reading, bool) {
	ctx, cancel := context.WithTimeout(ctx, quotaTimeout)
	defer cancel()
	cmd := shell.Command(ctx, q.Command)
	cmd.WaitDelay = 2 * time.Second // a child holding stdout can't block Wait past the timeout
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	var raw []struct {
		Reading struct {
			Provider  string  `json:"provider"`
			FiveHour  float64 `json:"five_hour"`
			Week      float64 `json:"week"`
			FiveReset string  `json:"five_reset"`
			WeekReset string  `json:"week_reset"`
		} `json:"reading"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, false
	}
	at := now.UTC().Format(time.RFC3339)
	rs := make([]Reading, 0, len(raw))
	for _, r := range raw {
		x := r.Reading
		rs = append(rs, Reading{Provider: x.Provider, FiveHour: x.FiveHour, Week: x.Week,
			FiveReset: utc(x.FiveReset), WeekReset: utc(x.WeekReset), ReadAt: at})
	}
	return rs, true
}

// utc renders an RFC3339 time in UTC; "" when it does not parse.
func utc(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
