package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
)

// proposeTTL is how long a proposal waits for the admin box; nixTTL a
// nix for a box that is asleep.
const (
	proposeTTL = 24 * time.Hour
	nixTTL     = 24 * time.Hour
)

// reposText is this box's repos.toml as written ("" when there is none).
func (a *app) reposText() (string, error) {
	b, err := os.ReadFile(config.ReposPath(a.cfg.Path()))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// applyRegistry writes a registry's rows as this box's repos.toml
// (keeping repos.toml.prev) once this binary's loader reads them.
func (a *app) applyRegistry(r hosts.Registry) error {
	if _, err := config.ParseRepos(r.Rows); err != nil {
		return fmt.Errorf("registry v%d needs a newer bmesh (%v)", r.Version, err)
	}
	return config.WriteReposText(config.ReposPath(a.cfg.Path()), r.Rows)
}

// registry is this box's newest registry, after taking a newer one from
// the hub (written as repos.toml, the old file kept as repos.toml.prev),
// which the caller is refused with: its own repos.toml was just replaced.
// Version 0 is none.
func (a *app) registry(ctx context.Context, b *hostsBox) (hosts.Registry, error) {
	kv, err := hosts.Bucket(ctx, a.client.JS, hosts.BucketRoster)
	if err != nil {
		return hosts.Registry{}, err
	}
	res, err := hosts.SyncRegistry(ctx, kv, b.stateDir, b.local.Pinned, a.applyRegistry)
	if err != nil {
		return hosts.Registry{}, err
	}
	if res.Refused > 0 {
		return hosts.Registry{}, fail(exitRefused, "the hub has registry v%d, which this bmesh can't read: %v; run `bmesh update`",
			res.Refused, res.RefuseErr)
	}
	if res.Took > 0 {
		return hosts.Registry{}, fail(exitRefused, "took registry v%d from the hub as %s (your file is kept as %s.prev): bring your edits over and run this again",
			res.Took, config.ReposPath(a.cfg.Path()), config.ReposFile)
	}
	reg, _, err := hosts.LoadRegistry(b.stateDir, b.local.Pinned)
	return reg, err
}

// registryRows reads a registry's rows; none for version 0.
func registryRows(reg hosts.Registry) (map[string]config.Repo, error) {
	if reg.Version == 0 {
		return map[string]config.Repo{}, nil
	}
	return config.ParseRepos(reg.Rows)
}

// printDiff prints the row diff from reg to rows under a heading.
func (a *app) printDiff(head string, reg hosts.Registry, rows map[string]config.Repo) ([]string, error) {
	old, err := registryRows(reg)
	if err != nil {
		return nil, fmt.Errorf("registry v%d: %w", reg.Version, err)
	}
	diff := config.DiffRepos(old, rows)
	fmt.Fprintln(a.stdout, head)
	for _, l := range diff {
		fmt.Fprintln(a.stdout, "  "+l)
	}
	if len(diff) == 0 {
		fmt.Fprintln(a.stdout, "  (comments and layout only)")
	}
	return diff, nil
}

// cmdReposPublish (admin) publishes this box's repos.toml as the next
// registry version, after printing the row diff from the current one and
// reading the admin key.
func (a *app) cmdReposPublish(args []string) error {
	fs := a.flags("repos publish")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "usage: bmesh repos publish")
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	if admin, ok := b.local.Roster.Admin(); !b.cfg.Hosts.Admin || !ok || admin.Name != b.host {
		return fail(exitRefused, "only the admin box (%s) publishes the registry: run `bmesh repos propose` to send it this box's changes", admin.Name)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "repos-publish"); err != nil {
		return err
	}
	if err := b.sync(ctx, a); err != nil {
		return fmt.Errorf("roster: %w", err)
	}
	reg, err := a.registry(ctx, b)
	if err != nil {
		return err
	}
	text, err := a.reposText()
	if err != nil {
		return err
	}
	if text == reg.Rows {
		fmt.Fprintf(a.stdout, "registry v%d: no change\n", reg.Version)
		return nil
	}
	if _, err := a.printDiff(fmt.Sprintf("registry v%d -> v%d:", reg.Version, reg.Version+1), reg, a.cfg.Repos); err != nil {
		return err
	}
	key, err := a.adminKey(b, fmt.Sprintf("publish registry v%d", reg.Version+1))
	if err != nil {
		return err
	}
	kv, err := hosts.Bucket(ctx, a.client.JS, hosts.BucketRoster)
	if err != nil {
		return err
	}
	next, err := hosts.PublishRegistry(ctx, kv, b.stateDir, text, key, time.Now(), a.applyRegistry)
	if err != nil && next.Version == 0 {
		return err
	}
	fmt.Fprintf(a.stdout, "registry v%d published\n", next.Version)
	return err
}

// cmdReposPropose sends this box's repos.toml, as a diff from the registry
// it has, to the admin box, which holds it for the operator.
func (a *app) cmdReposPropose(args []string) error {
	fs := a.flags("repos propose")
	wait := fs.Duration("wait", 30*time.Second, "how long to wait for the admin box's answer")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "usage: bmesh repos propose [--wait D]")
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *wait+time.Minute)
	defer cancel()
	if err := a.connect(ctx, "repos-propose"); err != nil {
		return err
	}
	if err := b.sync(ctx, a); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: roster: %v (using this box's copy)\n", err)
	}
	admin, ok := b.local.Roster.Admin()
	if !ok {
		return fail(exitRefused, "roster v%d names no admin box", b.local.Roster.Version)
	}
	if admin.Name == b.host {
		return fail(exitRefused, "this is the admin box: publish with `bmesh repos publish`")
	}
	reg, err := a.registry(ctx, b)
	if err != nil {
		return err
	}
	text, err := a.reposText()
	if err != nil {
		return err
	}
	if text == reg.Rows {
		fmt.Fprintf(a.stdout, "no change against registry v%d\n", reg.Version)
		return nil
	}
	if _, err := a.printDiff(fmt.Sprintf("proposed against registry v%d:", reg.Version), reg, a.cfg.Repos); err != nil {
		return err
	}
	r := b.local.Roster
	j := hosts.NewJob(b.key, b.host, admin.Name, "propose", "", hosts.ProposeArgs(reg.Version, text), r.Version, time.Now(), proposeTTL)
	replies, err := a.askBoxes(ctx, b, []hosts.Job{j}, *wait, func(j hosts.Job, rep hosts.Reply) {
		fmt.Fprintf(a.stdout, "%s: %s\n", j.To, rep.Text)
	})
	if err != nil {
		return err
	}
	rep, ok := replies[j.ID]
	if !ok {
		fmt.Fprintf(a.stdout, "%s: %s\n", admin.Name, pending(j, a.presence(ctx, r), *wait))
		return nil
	}
	if !rep.OK {
		return fail(exitRefused, "not proposed")
	}
	return nil
}

// nixRun is a `repos remove --checkouts` in progress, in <state
// dir>/nix/<name>.json: the boxes asked and what each answered (""
// pending, else removed or absent), with the last job each was sent.
type nixRun struct {
	Repo   string               `json:"repo"`
	Origin string               `json:"origin"`
	Boxes  map[string]string    `json:"boxes"`
	Jobs   map[string]hosts.Job `json:"jobs"`
	Text   map[string]string    `json:"text"`
}

func nixRunPath(stateDir, repo string) string {
	return filepath.Join(stateDir, "nix", repo+".json")
}

func loadNixRun(stateDir, repo string) (nixRun, bool, error) {
	b, err := os.ReadFile(nixRunPath(stateDir, repo))
	if errors.Is(err, os.ErrNotExist) {
		return nixRun{}, false, nil
	}
	if err != nil {
		return nixRun{}, false, err
	}
	var n nixRun
	if err := json.Unmarshal(b, &n); err != nil {
		return nixRun{}, false, fmt.Errorf("%s: %w", nixRunPath(stateDir, repo), err)
	}
	return n, true, nil
}

func saveNixRun(stateDir string, n nixRun) error {
	b, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	p := nixRunPath(stateDir, n.Repo)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// nixDone: a nix reply that counts, removed or absent.
func nixDone(rep hosts.Reply) (string, bool) {
	for _, s := range []string{"removed", "absent"} {
		if rep.OK && strings.HasPrefix(rep.Text, s) {
			return s, true
		}
	}
	return "", false
}

// cmdReposRemove takes a row out of the registry. With hosts off it is
// this box's repos.toml; with hosts on, the admin publishes the next
// registry without it, and with --checkouts only after every box whose
// presence lists the repo has answered nix with removed or absent.
// Without --apply it says what it would do.
func (a *app) cmdReposRemove(args []string) error {
	fs := a.flags("repos remove")
	checkouts := fs.Bool("checkouts", false, "first remove the repo's checkout on every box that lists it (nix), then the row")
	ignoredOK := fs.Bool("ignored-ok", false, "a checkout's ignored files go with it, rather than refusing it")
	apply := fs.Bool("apply", false, "do it (default: say what would be done)")
	wait := fs.Duration("wait", time.Minute, "how long to wait for the boxes' answers")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, "usage: bmesh repos remove <name> [--checkouts [--ignored-ok]] [--apply] [--wait D]")
	}
	name := pos[0]
	if err := a.loadConfig(); err != nil {
		return err
	}
	if !a.cfg.Hosts.Enabled {
		if *checkouts {
			return fail(exitRefused, "--checkouts sends nix to every box: it needs [hosts] enabled (`bmesh init --hosts`)")
		}
		return a.removeLocalRow(name, *apply)
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	if admin, ok := b.local.Roster.Admin(); !b.cfg.Hosts.Admin || !ok || admin.Name != b.host {
		return fail(exitRefused, "only the admin box (%s) removes a row from the registry", admin.Name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *wait+2*time.Minute)
	defer cancel()
	if err := a.connect(ctx, "repos-remove"); err != nil {
		return err
	}
	if err := b.sync(ctx, a); err != nil {
		return fmt.Errorf("roster: %w", err)
	}
	reg, err := a.registry(ctx, b)
	if err != nil {
		return err
	}
	if text, err := a.reposText(); err != nil {
		return err
	} else if reg.Version > 0 && text != reg.Rows {
		return fail(exitRefused, "%s has changes registry v%d doesn't: publish them first (`bmesh repos publish`)",
			config.ReposPath(a.cfg.Path()), reg.Version)
	}
	rows, err := registryRows(reg)
	if err != nil {
		return err
	}
	row, ok := rows[name]
	if !ok {
		_ = os.Remove(nixRunPath(b.stateDir, name))
		return fail(exitRefused, "%s is not in registry v%d", name, reg.Version)
	}
	next, err := config.RemoveRepoText(reg.Rows, name)
	if err != nil {
		return err
	}
	// One read of the admin key signs the nix jobs and the registry.
	var key ed25519.PrivateKey
	adminKey := func(nix []string) (ed25519.PrivateKey, error) {
		if key != nil {
			return key, nil
		}
		action := fmt.Sprintf("publish registry v%d without %s", reg.Version+1, name)
		if len(nix) > 0 {
			action = fmt.Sprintf("nix %s on %s, then %s", name, strings.Join(nix, ", "), action)
		}
		k, err := a.adminKey(b, action)
		key = k
		return k, err
	}
	if *checkouts {
		done, err := a.nixEverywhere(ctx, b, row, *ignoredOK, *apply, *wait, adminKey)
		if err != nil || !done {
			return err
		}
	}
	if !*apply {
		fmt.Fprintf(a.stdout, "would publish registry v%d without %s (run again with --apply)\n", reg.Version+1, name)
		return nil
	}
	if _, err := adminKey(nil); err != nil {
		return err
	}
	kv, err := hosts.Bucket(ctx, a.client.JS, hosts.BucketRoster)
	if err != nil {
		return err
	}
	pub, err := hosts.PublishRegistry(ctx, kv, b.stateDir, next, key, time.Now(), a.applyRegistry)
	if err != nil && pub.Version == 0 {
		return err
	}
	_ = os.Remove(nixRunPath(b.stateDir, name))
	fmt.Fprintf(a.stdout, "registry v%d: %s removed\n", pub.Version, name)
	return err
}

// nixEverywhere asks every box whose signed presence lists row's repo to
// nix it, re-asking on a later run only the boxes that haven't answered
// removed or absent (an answer that came in since counts). done is true
// once every box has; without apply it prints what it would ask.
func (a *app) nixEverywhere(ctx context.Context, b *hostsBox, row config.Repo, ignoredOK, apply bool, wait time.Duration,
	adminKey func(nix []string) (ed25519.PrivateKey, error)) (done bool, err error) {
	if row.Remote == "" {
		return false, fail(exitRefused, "%s has no remote in the registry: nix matches each checkout's origin against it", row.Name)
	}
	r := b.local.Roster
	run, ok, err := loadNixRun(b.stateDir, row.Name)
	if err != nil {
		return false, err
	}
	if !ok || run.Origin != row.Remote {
		seen := a.presence(ctx, r)
		if seen == nil {
			return false, fmt.Errorf("can't read the boxes' presence: no telling which have %s", row.Name)
		}
		run = nixRun{Repo: row.Name, Origin: row.Remote, Boxes: map[string]string{}, Jobs: map[string]hosts.Job{}, Text: map[string]string{}}
		for _, s := range seen {
			if listsRepo(s, row.Name) {
				run.Boxes[s.Host.Name] = ""
			}
		}
	}
	// Answers that arrived since the last run.
	if len(run.Jobs) > 0 {
		reps, err := hosts.Replies(ctx, a.client.JS, b.host)
		if err != nil {
			return false, err
		}
		for _, rep := range reps {
			for box, j := range run.Jobs {
				if run.Boxes[box] == "" && rep.JobID == j.ID && hosts.CheckReply(&rep, &j, &r) == nil {
					run.Text[box] = rep.Text
					if s, ok := nixDone(rep); ok {
						run.Boxes[box] = s
					}
				}
			}
		}
	}
	var left []string
	for box, s := range run.Boxes {
		if s == "" {
			left = append(left, box)
		}
	}
	slices.Sort(left)
	if !apply {
		boxes := make([]string, 0, len(run.Boxes))
		for box := range run.Boxes {
			boxes = append(boxes, box)
		}
		slices.Sort(boxes)
		for _, box := range boxes {
			state := run.Boxes[box]
			if state == "" {
				state = "would be asked to nix " + row.Name + " (origin " + row.Remote + ")"
				if t := run.Text[box]; t != "" {
					state += "; last answer: " + t
				}
			}
			fmt.Fprintf(a.stdout, "%s: %s\n", box, state)
		}
		return true, nil
	}
	if len(left) > 0 {
		key, err := adminKey(left)
		if err != nil {
			return false, err
		}
		jobArgs := []string{row.Remote}
		if ignoredOK {
			jobArgs = append(jobArgs, "ignored-ok")
		}
		var jobs []hosts.Job
		now := time.Now()
		for _, box := range left {
			j := hosts.NewJob(key, b.host, box, "nix", row.Name, jobArgs, r.Version, now, nixTTL)
			run.Jobs[box] = j
			jobs = append(jobs, j)
		}
		if err := saveNixRun(b.stateDir, run); err != nil {
			return false, err
		}
		replies, err := a.askBoxes(ctx, b, jobs, wait, func(j hosts.Job, rep hosts.Reply) {
			fmt.Fprintf(a.stdout, "%s: %s\n", j.To, rep.Text)
		})
		if err != nil {
			return false, err
		}
		seen := a.presence(ctx, r)
		left = left[:0]
		for _, j := range jobs {
			rep, ok := replies[j.ID]
			if ok {
				run.Text[j.To] = rep.Text
				if s, done := nixDone(rep); done {
					run.Boxes[j.To] = s
					continue
				}
			} else {
				fmt.Fprintf(a.stdout, "%s: %s\n", j.To, pending(j, seen, wait))
			}
			left = append(left, j.To)
		}
	}
	if err := saveNixRun(b.stateDir, run); err != nil {
		return false, err
	}
	if len(left) > 0 {
		fmt.Fprintf(a.stdout, "pending: %s; the registry keeps %s until each answers removed or absent (run this again to re-ask them)\n",
			strings.Join(left, ", "), row.Name)
		return false, nil
	}
	return true, nil
}

// removeLocalRow takes a row out of this box's repos.toml, with [hosts]
// off: the file is plain local config.
func (a *app) removeLocalRow(name string, apply bool) error {
	path := config.ReposPath(a.cfg.Path())
	if _, ok := a.cfg.Repos[name]; !ok {
		return fail(exitRefused, "no repo %q in %s", name, path)
	}
	if !apply {
		fmt.Fprintf(a.stdout, "would remove %s from %s (run again with --apply)\n", name, path)
		return nil
	}
	text, err := a.reposText()
	if err != nil {
		return err
	}
	next, err := config.RemoveRepoText(text, name)
	if err != nil {
		return err
	}
	if err := config.WriteReposText(path, next); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "removed %s from %s\n", name, path)
	return nil
}
