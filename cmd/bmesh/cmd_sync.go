package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/level"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const syncUsage = "usage: bmesh sync [--repo R | --all] [--wait D]"

// levelTTL is how long a level job waits for a box that is asleep.
const levelTTL = 24 * time.Hour

// syncLevelTimeout bounds this box's own levels (a fetch and a push each).
const syncLevelTimeout = 10 * time.Minute

// cmdSync levels this box's checkout of each repo (pushing when it is
// ahead and allowed to), then sends level with origin's new head to every
// roster box that accepts level and whose signed presence lists the repo,
// and prints each answer that arrives within --wait. Each box judges its
// own checkout against origin, so it works the same from any box.
func (a *app) cmdSync(args []string) error {
	fs := a.flags("sync")
	repo := fs.String("repo", "", "the registry repo to level (default: the repo of the current directory)")
	all := fs.Bool("all", false, "every registered repo this box has a checkout of")
	wait := fs.Duration("wait", 30*time.Second, "how long to wait for the other boxes' answers")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || (*all && *repo != "") {
		return fail(exitUsage, syncUsage)
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	var names []string
	switch {
	case *all:
		for _, n := range a.cfg.RepoNames() {
			if _, ok := b.checkout(n); ok {
				names = append(names, n)
			}
		}
	case *repo != "":
		names = []string{*repo}
	default:
		cur, ok := cwdRepo()
		if !ok {
			return fail(exitUsage, "not in a registered repo's checkout: name one with --repo, or --all")
		}
		n, _, err := a.cfg.RepoPath(cur.Path)
		if _, reg := a.cfg.Repos[n]; err != nil || !reg {
			return fail(exitUsage, "%s is not in the registry: name a repo with --repo, or --all", cur.Path)
		}
		names = []string{n}
	}
	for _, n := range names {
		if _, ok := a.cfg.Repos[n]; !ok {
			return fail(exitRefused, "%s is not in the registry", n)
		}
		if _, ok := b.checkout(n); !ok {
			return fail(exitRefused, "no checkout of %s on this box", n)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), syncLevelTimeout+*wait+time.Minute)
	defer cancel()
	if err := a.connect(ctx, "sync"); err != nil {
		return err
	}
	if err := b.sync(ctx, a); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: roster: %v (using this box's copy)\n", err)
	}
	r := b.local.Roster
	var seen []hosts.Seen
	if kv, err := hosts.Bucket(ctx, a.client.JS, hosts.BucketHosts); err == nil {
		seen, _ = hosts.ReadPresence(ctx, kv, r, time.Now())
	}
	prefix := func(name string) string {
		if len(names) > 1 {
			return name + ": "
		}
		return ""
	}
	type sent struct {
		repo string
		job  hosts.Job
	}
	heads := map[string]level.Outcome{}
	for _, name := range names {
		path, _ := b.checkout(name)
		o := a.levelHere(ctx, b, name, path)
		fmt.Fprintf(a.stdout, "%s%s: %s\n", prefix(name), b.host, o.Text())
		heads[name] = o
	}
	// The wait starts once this box's own levels are done: until tells a
	// receiving box whether anyone still waits for its answer.
	var jobs []sent
	until := hosts.Stamp(time.Now().Add(*wait))
	for _, name := range names {
		o := heads[name]
		if o.Target == "" {
			continue // origin's head is unknown here: nothing to send
		}
		for _, s := range seen {
			if s.Host.Name == b.host || !slices.Contains(s.Host.Accept, "level") || !listsRepo(s, name) {
				continue
			}
			j := hosts.NewJob(b.key, b.host, s.Host.Name, "level", name, level.JobArgs(o.Target, o.Branch, until),
				r.Version, time.Now(), levelTTL)
			jobs = append(jobs, sent{repo: name, job: j})
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	want := map[string]hosts.Job{}
	for _, s := range jobs {
		want[s.job.ID] = s.job
	}
	repoOf := map[string]string{}
	for _, s := range jobs {
		repoOf[s.job.ID] = s.repo
	}
	wctx, wcancel := context.WithTimeout(ctx, *wait)
	defer wcancel()
	answered := map[string]bool{}
	done := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		done <- hosts.WaitReplies(wctx, a.client.JS, b.host, want, &r, ready, func(j hosts.Job, rep hosts.Reply) {
			answered[j.ID] = true
			fmt.Fprintf(a.stdout, "%s%s: %s\n", prefix(repoOf[j.ID]), j.To, rep.Text)
		})
	}()
	select {
	case <-ready:
	case err := <-done:
		return err
	}
	for _, s := range jobs {
		if err := hosts.SendJob(ctx, a.client.JS, s.job); err != nil {
			return err
		}
	}
	if err := <-done; err != nil {
		return err
	}
	states := map[string]string{}
	for _, s := range seen {
		states[s.Host.Name] = s.State
	}
	for _, s := range jobs {
		if answered[s.job.ID] {
			continue
		}
		exp, _ := hosts.ParseStamp(s.job.Expires)
		how := "asleep"
		if states[s.job.To] == hosts.Awake {
			how = "no answer in " + wait.String()
		}
		fmt.Fprintf(a.stdout, "%s%s: %s, queued until %s\n", prefix(s.repo), s.job.To, how, exp.Local().Format("Mon 15:04"))
	}
	return nil
}

// listsRepo: s is verified presence (awake or asleep) that lists repo.
func listsRepo(s hosts.Seen, repo string) bool {
	if s.State != hosts.Awake && s.State != hosts.Asleep {
		return false
	}
	return slices.ContainsFunc(s.Presence.Repos, func(p hosts.PresenceRepo) bool { return p.Name == repo })
}

// levelHere levels this box's checkout of repo as a received job would
// (it may push), holding a move of a repo sessions load on a secure box:
// the hold is this box's own, kept for `bmesh hosts approve` as a
// self-level's is.
func (a *app) levelHere(ctx context.Context, b *hostsBox, repo, path string) level.Outcome {
	row := a.cfg.Repos[repo]
	req := level.Request{Repo: repo, Path: path, Mode: row.Mode, Pin: row.Pin, StateDir: b.stateDir, Push: true,
		PushModes: a.cfg.Hosts.PushModes}
	req.Hold = b.secure() && (repo == a.cfg.Update.Repo || level.LoadsPlugins(ctx, path))
	o := level.Level(ctx, req)
	if o.Result != level.Held {
		return o
	}
	all, _ := hosts.ListHeld(b.stateDir)
	if slices.ContainsFunc(all, func(x hosts.Held) bool { return x.Repo == repo }) {
		return o // the hold there stands for it
	}
	now := time.Now()
	j := hosts.NewJob(b.key, b.host, b.host, "level", repo, level.JobArgs(o.Target, o.Branch, ""),
		b.local.Roster.Version, now, hosts.MaxLifetime)
	x := hosts.Held{ID: j.ID, Job: j, Self: true, Repo: repo, Path: path, Branch: o.Branch, Head: o.Head,
		Target: o.Target, Commits: o.N, Log: o.Log, Created: hosts.Stamp(now)}
	if err := hosts.SaveHeld(b.stateDir, x); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: hold %s: %v\n", repo, err)
		return o
	}
	text := fmt.Sprintf("%s holds: %s +%d %s (%s)", b.host, repo, o.N, plural(o.N, "commit"), x.From())
	ev := mesh.Event{From: "daemon." + b.host, Event: "held", Text: text, Body: x.ID, At: now.UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, a.client, ev); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: publish held: %v\n", err)
	}
	return o
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
