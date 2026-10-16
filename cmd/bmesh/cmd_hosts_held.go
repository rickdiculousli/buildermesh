package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
)

// approveWait is how long an approval waits for the daemon's level.
const approveWait = 6 * time.Minute

// checkout is repo's checkout on this box, when it is registered and its
// path exists.
func (b *hostsBox) checkout(repo string) (string, bool) {
	if _, ok := b.cfg.Repos[repo]; !ok {
		return "", false
	}
	_, path, err := b.cfg.RepoPath(repo)
	if err != nil {
		return "", false
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return "", false
	}
	return path, true
}

// secure: this box holds for the operator, by its [hosts] secure or its
// roster row.
func (b *hostsBox) secure() bool {
	row, ok := b.local.Roster.Host(b.host)
	return b.cfg.Hosts.Secure || ok && row.Secure
}

// cmdHostsHeld lists this box's holds.
func (a *app) cmdHostsHeld(args []string) error {
	fs := a.flags("hosts held")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "usage: bmesh hosts held [--json]")
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	all, err := hosts.ListHeld(b.stateDir)
	if err != nil {
		return err
	}
	if a.json {
		if all == nil {
			all = []hosts.Held{}
		}
		return a.printJSON(all)
	}
	if len(all) == 0 {
		fmt.Fprintln(a.stdout, "held: none")
	}
	for _, x := range all {
		exp, _ := hosts.ParseStamp(x.Job.Expires)
		if x.Kind == hosts.HeldRegistry {
			fmt.Fprintf(a.stdout, "%s registry proposal, %d row %s against v%d, from %s, expires %s\n", x.ID, x.Commits,
				plural(x.Commits, "change"), x.Base, x.From(), exp.Local().Format("Mon 15:04"))
			continue
		}
		fmt.Fprintf(a.stdout, "%s %s +%d %s to %s, from %s, expires %s\n", x.ID, x.Repo, x.Commits, plural(x.Commits, "commit"),
			short8(x.Target), x.From(), exp.Local().Format("Mon 15:04"))
	}
	return nil
}

// cmdHostsApprove shows a hold's log, reads the approve key (the announced
// Keychain dialog, or the passphrase) and signs approve/v1 over the job
// and the target shown; the daemon re-checks it and levels. When origin
// moved since, it shows the new log and asks again.
func (a *app) cmdHostsApprove(args []string) error {
	fs := a.flags("hosts approve")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, "usage: bmesh hosts approve <id>")
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	x, ok, err := hosts.LoadHeld(b.stateDir, pos[0])
	if err != nil {
		return err
	}
	if !ok {
		return fail(exitRefused, "no hold %s on this box (`bmesh hosts held` lists them)", pos[0])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*approveWait)
	defer cancel()
	if err := a.connect(ctx, "hosts-approve"); err != nil {
		return err
	}
	if x.Kind == hosts.HeldRegistry {
		return a.approveProposal(ctx, b, x)
	}
	if err := b.sync(ctx, a); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: roster: %v (using this box's copy)\n", err)
	}
	me, ok := b.local.Roster.Host(b.host)
	if !ok || me.ApproveKey == "" {
		return fail(exitRefused, "%s has no approve key in roster v%d: ask the admin to invite it again with --secure --replace",
			b.host, b.local.Roster.Version)
	}
	showHold(a, x)
	if !atScreen(a.stdin) {
		return fail(exitRefused, "approving needs the operator at this box's own screen (stdin is not a terminal)")
	}
	name := hosts.KeyApprove
	if me.ApproveKey == b.local.Roster.AdminKey {
		name = hosts.KeyAdmin // the admin box approves with the admin key
	}
	store, err := a.keyStore()
	if err != nil {
		return err
	}
	key, err := store.Get(name, fmt.Sprintf("approve %s +%d %s", x.Repo, x.Commits, plural(x.Commits, "commit")))
	if err != nil {
		return err
	}
	if hosts.KeyHex(key.Public().(ed25519.PublicKey)) != me.ApproveKey {
		return fail(exitRefused, "the %s key in the %s store isn't the approve key the roster names (%s)",
			name, store.Kind(), hosts.Fingerprint(me.ApproveKey))
	}
	for {
		ans, err := a.sendApproval(ctx, b, hosts.NewApprove(key, x, b.host, time.Now()))
		if err != nil {
			return err
		}
		if ans.Moved == nil {
			fmt.Fprintf(a.stdout, "%s: %s\n", x.Repo, ans.Text)
			if !ans.OK {
				return fail(exitRefused, "not levelled")
			}
			return nil
		}
		x = *ans.Moved
		fmt.Fprintf(a.stdout, "origin moved since: %s is now at %s\n", x.Repo, short8(x.Target))
		showHold(a, x)
		fmt.Fprintf(a.stdout, "Approve %s at %s? [y/N] ", x.Repo, short8(x.Target))
		line, err := a.lines().ReadString('\n')
		if err != nil && line == "" {
			return fail(exitRefused, "left held: no answer")
		}
		if !yes(strings.ToLower(strings.TrimSpace(line))) {
			fmt.Fprintf(a.stdout, "left held: %s\n", x.ID)
			return nil
		}
	}
}

// sendApproval asks this box's daemon to level the hold an approval names.
func (a *app) sendApproval(ctx context.Context, b *hostsBox, ap hosts.Approve) (hosts.ApproveAnswer, error) {
	data, err := json.Marshal(ap)
	if err != nil {
		return hosts.ApproveAnswer{}, err
	}
	m, err := a.client.NC.Request(hosts.ApproveSubject(b.host), data, approveWait)
	if errors.Is(err, nats.ErrNoResponders) {
		return hosts.ApproveAnswer{}, fail(exitRefused, "no daemon on %s answered: is `bmesh serve` running with [hosts] enabled?", b.host)
	}
	if err != nil {
		return hosts.ApproveAnswer{}, fmt.Errorf("approval: %w", err)
	}
	var ans hosts.ApproveAnswer
	if err := json.Unmarshal(m.Data, &ans); err != nil {
		return hosts.ApproveAnswer{}, fmt.Errorf("approval answer: %w", err)
	}
	return ans, nil
}

// approveProposal publishes a held registry proposal as the next version,
// signed with the admin key (the announced Keychain dialog), and answers
// its sender. A proposal made against an older version than the
// registry's now is dropped: its rows would undo what came since.
func (a *app) approveProposal(ctx context.Context, b *hostsBox, x hosts.Held) error {
	if err := b.sync(ctx, a); err != nil {
		return fmt.Errorf("roster: %w", err)
	}
	drop := func(text string) error {
		if err := hosts.DropHeld(b.stateDir, x.ID); err != nil {
			return err
		}
		if err := hosts.SendReply(ctx, a.client.JS, hosts.NewReply(b.key, &x.Job, b.host, false, text, time.Now())); err != nil {
			fmt.Fprintf(a.stderr, "bmesh: reply: %v\n", err)
		}
		return fail(exitRefused, "%s: hold %s dropped", text, x.ID)
	}
	if x.Expired(time.Now()) {
		return drop("refused: the proposal expired")
	}
	r := b.local.Roster
	if sender, ok := r.Host(x.Job.By); !ok || x.Job.Verify(sender.Key) != nil || x.Job.To != b.host {
		return drop(fmt.Sprintf("refused: the proposal's job from %s doesn't verify against roster v%d", x.Job.By, r.Version))
	}
	base, text, err := hosts.ParseProposeArgs(x.Job.Args)
	if err != nil {
		return drop("refused: " + err.Error())
	}
	rows, err := config.ParseRepos(text)
	if err != nil {
		return drop(fmt.Sprintf("refused: the proposed rows don't load here: %v", err))
	}
	reg, err := a.registry(ctx, b)
	if err != nil {
		return err
	}
	if reg.Version != base {
		return drop(fmt.Sprintf("refused: proposed against registry v%d; it is v%d now: propose again", base, reg.Version))
	}
	if own, err := a.reposText(); err != nil {
		return err
	} else if reg.Version > 0 && own != reg.Rows {
		return fail(exitRefused, "%s has changes registry v%d doesn't, which approving would overwrite: undo them, or publish them and have %s propose again; the proposal stays held",
			config.ReposPath(a.cfg.Path()), reg.Version, x.From())
	}
	diff, err := a.printDiff(fmt.Sprintf("%s: registry proposal from %s, v%d -> v%d:", x.ID, x.From(), reg.Version, reg.Version+1), reg, rows)
	if err != nil {
		return err
	}
	key, err := a.adminKey(b, fmt.Sprintf("publish registry v%d (proposal from %s)", reg.Version+1, x.From()))
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
	if derr := hosts.DropHeld(b.stateDir, x.ID); derr != nil {
		fmt.Fprintf(a.stderr, "bmesh: drop hold: %v\n", derr)
	}
	ans := fmt.Sprintf("approved: registry v%d (%d row %s)", next.Version, len(diff), plural(len(diff), "change"))
	if rerr := hosts.SendReply(ctx, a.client.JS, hosts.NewReply(b.key, &x.Job, b.host, true, ans, time.Now())); rerr != nil {
		fmt.Fprintf(a.stderr, "bmesh: reply: %v\n", rerr)
	}
	fmt.Fprintf(a.stdout, "registry v%d published: proposal from %s approved\n", next.Version, x.From())
	return err
}

// showHold prints what an approval would let in.
func showHold(a *app, x hosts.Held) {
	fmt.Fprintf(a.stdout, "%s: %s +%d %s, %s -> %s (from %s)\n\n%s\n\n", x.ID, x.Repo, x.Commits, plural(x.Commits, "commit"),
		short8(x.Head), short8(x.Target), x.From(), x.Log)
}

// cmdHostsDeny drops a hold, answering its sender.
func (a *app) cmdHostsDeny(args []string) error {
	fs := a.flags("hosts deny")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, "usage: bmesh hosts deny <id>")
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	x, ok, err := hosts.LoadHeld(b.stateDir, pos[0])
	if err != nil {
		return err
	}
	if !ok {
		return fail(exitRefused, "no hold %s on this box (`bmesh hosts held` lists them)", pos[0])
	}
	if err := hosts.DropHeld(b.stateDir, x.ID); err != nil {
		return err
	}
	if x.Kind == hosts.HeldRegistry {
		fmt.Fprintf(a.stdout, "denied %s: the registry stays at v%d without %s's proposal\n", x.ID, x.Base, x.From())
	} else {
		fmt.Fprintf(a.stdout, "denied %s: %s stays at %s\n", x.ID, x.Repo, short8(x.Head))
	}
	if x.Self {
		return nil
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "hosts-deny"); err != nil {
		return err
	}
	return hosts.SendReply(ctx, a.client.JS, hosts.NewReply(b.key, &x.Job, b.host, false, "denied by the operator", time.Now()))
}
