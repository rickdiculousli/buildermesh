package main

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
)

// askBoxes follows this box's reply subject, sends jobs, and calls got for
// each reply that checks as it arrives within wait; it returns the replies
// by job id. A job with no reply stays queued in its box's inbox until it
// expires (pending says how to print it).
func (a *app) askBoxes(ctx context.Context, b *hostsBox, jobs []hosts.Job, wait time.Duration,
	got func(hosts.Job, hosts.Reply)) (map[string]hosts.Reply, error) {
	r := b.local.Roster
	want := map[string]hosts.Job{}
	for _, j := range jobs {
		want[j.ID] = j
	}
	var mu sync.Mutex
	replies := map[string]hosts.Reply{}
	wctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	done := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		done <- hosts.WaitReplies(wctx, a.client.JS, b.host, want, &r, ready, func(j hosts.Job, rep hosts.Reply) {
			mu.Lock()
			replies[j.ID] = rep
			mu.Unlock()
			if got != nil {
				got(j, rep)
			}
		})
	}()
	// Follow the replies before sending, so a fast one isn't missed.
	select {
	case <-ready:
	case err := <-done:
		return nil, err
	}
	for _, j := range jobs {
		if err := hosts.SendJob(ctx, a.client.JS, j); err != nil {
			cancel()
			<-done
			return replies, err
		}
	}
	err := <-done
	mu.Lock()
	defer mu.Unlock()
	return replies, err
}

// presence is every roster box's presence, by name; nil when the hub's
// can't be read.
func (a *app) presence(ctx context.Context, r hosts.Roster) map[string]hosts.Seen {
	kv, err := hosts.Bucket(ctx, a.client.JS, hosts.BucketHosts)
	if err != nil {
		return nil
	}
	seen, err := hosts.ReadPresence(ctx, kv, r, time.Now())
	if err != nil {
		return nil
	}
	out := map[string]hosts.Seen{}
	for _, s := range seen {
		out[s.Host.Name] = s
	}
	return out
}

// accepting is the roster boxes whose accept has op, by name.
func accepting(r hosts.Roster, op string) []string {
	var out []string
	for _, h := range r.Hosts {
		if slices.Contains(h.Accept, op) {
			out = append(out, h.Name)
		}
	}
	return out
}

// pending is how a job without a reply prints: asleep (or no answer in
// wait, for an awake box), queued until its expiry.
func pending(j hosts.Job, seen map[string]hosts.Seen, wait time.Duration) string {
	exp, _ := hosts.ParseStamp(j.Expires)
	how := "asleep"
	if seen[j.To].State == hosts.Awake {
		how = "no answer in " + wait.String()
	}
	return how + ", queued until " + exp.Local().Format("Mon 15:04")
}
