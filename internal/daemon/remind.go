package daemon

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/web"
)

// remindEvery is how often the daemon reads quota for the reset reminders;
// the reading goes through the map's 5-minute gate, so the upstream page is
// never asked more often than the map alone would ask it.
const remindEvery = time.Hour

// providerRE is a provider name that can stand in a bm-claims key.
var providerRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Reminder is one used 5-hour window's reset post: At is the reset rounded
// to the minute (Claude's reset jitters by fractions of a second between
// readings), which also names the window in its claim.
type Reminder struct {
	Provider    string
	At          time.Time
	Title, Body string
}

// Key is the window's bm-claims key: quota.<provider>.<At unix>.
func (r Reminder) Key() string {
	return "quota." + r.Provider + "." + strconv.FormatInt(r.At.Unix(), 10)
}

// Reminders is one reminder per reading whose 5-hour window has usage and
// whose rounded reset is still ahead of now; a reading without a parseable
// reset, or whose provider can't name a key, is skipped.
func Reminders(rs []web.Reading, now time.Time) []Reminder {
	var out []Reminder
	for _, r := range rs {
		if r.FiveHour <= 0 || !providerRE.MatchString(r.Provider) {
			continue
		}
		reset, err := time.Parse(time.RFC3339, r.FiveReset)
		if err != nil {
			continue
		}
		at := reset.Round(time.Minute)
		if !at.After(now) {
			continue
		}
		name := strings.ToUpper(r.Provider[:1]) + r.Provider[1:]
		out = append(out, Reminder{Provider: r.Provider, At: at,
			Title: name + " 5-hour quota reset",
			Body:  fmt.Sprintf("%s's 5-hour window is fresh. Weekly was %.0f%% used.", name, r.Week)})
	}
	return out
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

// remindOnce reads quota through q and, for each used window no daemon has
// claimed, claims it in bm-claims and posts its reminder to [quota]
// remind_topic, scheduled for the reset with ntfy's At header. A post that
// fails gives the claim back, so the next reading tries again.
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
		d.Log.Info("remind: scheduled", "provider", r.Provider, "at", r.At.UTC().Format(time.RFC3339))
	}
}

func postReminder(ctx context.Context, target string, r Reminder) error {
	ctx, cancel := context.WithTimeout(ctx, deliverTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader([]byte(r.Body)))
	if err != nil {
		return err
	}
	req.Header.Set("Title", r.Title)
	req.Header.Set("At", strconv.FormatInt(r.At.Unix(), 10))
	req.Header.Set("Tags", "hourglass")
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
