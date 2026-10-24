package daemon

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/notify"
	"github.com/rickdiculousli/buildermesh/internal/web"
)

// remindEvery is how often the daemon reads quota for the weekly alerts;
// the reading goes through the map's 5-minute gate, so the upstream page is
// never asked more often than the map alone would ask it.
const remindEvery = time.Hour

// providerRE is a provider name that can stand in a bm-claims key.
var providerRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// weekLevels are the weekly usage marks an alert is posted at, highest
// first: the share used, and what the title says is left.
var weekLevels = []struct {
	used        float64
	emoji, left string
}{{90, "🪫", "10% left"}, {75, "🪫", "25% left"}, {50, "🔋", "50% left"}}

// Reminder is one weekly quota alert: a provider's week, named by its reset
// rounded to the minute (Claude's reset jitters by fractions of a second
// between readings), and the mark its usage reached.
type Reminder struct {
	Provider    string
	Reset       time.Time
	Used        int
	Title, Body string
}

// Key is the alert's bm-claims key: quota.<provider>.week.<reset unix>.<used>.
func (r Reminder) Key() string {
	return fmt.Sprintf("quota.%s.week.%d.%d", r.Provider, r.Reset.Unix(), r.Used)
}

// Reminders is one alert per reading whose weekly usage reached a mark of
// weekLevels, the highest it reached, while its rounded reset is still
// ahead of now: `🔋 50% left • Claude weekly`, body `resets in 2d 4h`. A
// reading without a parseable weekly reset, or whose provider can't name a
// key, is skipped.
func Reminders(rs []web.Reading, now time.Time) []Reminder {
	var out []Reminder
	for _, r := range rs {
		if !providerRE.MatchString(r.Provider) {
			continue
		}
		reset, err := time.Parse(time.RFC3339, r.WeekReset)
		if err != nil {
			continue
		}
		at := reset.Round(time.Minute)
		if !at.After(now) {
			continue
		}
		name := strings.ToUpper(r.Provider[:1]) + r.Provider[1:]
		for _, l := range weekLevels {
			if r.Week >= l.used {
				out = append(out, Reminder{Provider: r.Provider, Reset: at, Used: int(l.used),
					Title: notify.Title(l.emoji, l.left, name+" weekly"), Body: "resets in " + resetsIn(at.Sub(now))})
				break
			}
		}
	}
	return out
}

// resetsIn renders d as 2d 4h from a day up, else 3h 10m, else 25m.
func resetsIn(d time.Duration) string {
	d = d.Round(time.Minute)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}

// TopicURL is the ntfy URL with its last path segment replaced by topic
// (http://h:2586/bmesh and topic quota-reset give http://h:2586/quota-reset;
// a URL with no path gets /<topic>).
func TopicURL(ntfy, topic string) (string, error) {
	u, err := url.Parse(ntfy)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("ntfy URL %q: want scheme://host/topic", ntfy)
	}
	p := strings.TrimSuffix(u.Path, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[:i]
	}
	u.Path, u.RawPath = p+"/"+topic, ""
	return u.String(), nil
}

// remindOnce reads quota through q and, for each weekly alert no daemon has
// claimed, claims it in bm-claims and posts it to [quota] remind_topic. A
// post that fails gives the claim back, so the next reading tries again.
func remindOnce(ctx context.Context, d Deps, q *web.Quota) {
	now := d.Now()
	target, err := TopicURL(d.Cfg.Notify.Ntfy, d.Cfg.Quota.RemindTopic)
	if err != nil {
		d.Log.Warn("remind: topic", "err", err)
		return
	}
	for _, r := range Reminders(q.Get(ctx, now), now) {
		won, err := mesh.Claim(ctx, d.Client, r.Key(), d.from())
		if err != nil {
			d.Log.Warn("remind: claim", "key", r.Key(), "err", err)
			continue
		}
		if !won {
			continue
		}
		if err := postReminder(ctx, target, r); err != nil {
			d.Log.Warn("remind: post", "key", r.Key(), "err", err)
			uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliverTimeout)
			err := mesh.Unclaim(uctx, d.Client, r.Key())
			cancel()
			if err != nil {
				d.Log.Warn("remind: unclaim", "key", r.Key(), "err", err)
			}
			continue
		}
		d.Log.Info("remind: posted", "provider", r.Provider, "used", r.Used)
	}
}

// deliverTimeout is a reminder post's cap.
const deliverTimeout = 10 * time.Second

// postReminder posts r.
func postReminder(ctx context.Context, target string, r Reminder) error {
	ctx, cancel := context.WithTimeout(ctx, deliverTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader([]byte(r.Body)))
	if err != nil {
		return err
	}
	req.Header.Set("Title", r.Title)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("ntfy: %s", resp.Status)
	}
	return nil
}
