package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
)

const hostsUsage = "usage: bmesh hosts [--json] | hosts --here | hosts --notes | hosts invite <name> [--secure] [--accept a,b] [--replace] | " +
	"hosts set <name> [--secure=true|false] [--accept a,b] | hosts revoke <name> | hosts ping [<host>] [--wait D] | hosts replies [--json]"

// defaultAccept is what a box accepts when the admin names nothing: ping,
// which shows the mesh works and changes nothing.
var defaultAccept = []string{"ping"}

// pingTTL is how long a ping waits for a box that is asleep.
const pingTTL = 24 * time.Hour

// atScreen is the terminal check on admin and approve commands: a
// convenience, since the Keychain dialog and the passphrase are the
// control. Tests replace it.
var atScreen = interactive

// cmdHosts lists the roster joined with presence, or runs a subcommand.
func (a *app) cmdHosts(args []string) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "invite":
			return a.cmdHostsInvite(args[1:])
		case "set":
			return a.cmdHostsSet(args[1:])
		case "revoke":
			return a.cmdHostsRevoke(args[1:])
		case "ping":
			return a.cmdHostsPing(args[1:])
		case "replies":
			return a.cmdHostsReplies(args[1:])
		}
		return fail(exitUsage, hostsUsage)
	}
	fs := a.flags("hosts")
	here := fs.Bool("here", false, "print this box's session line (<state dir>/this-box.txt) and never dial the hub")
	notes := fs.Bool("notes", false, "print every box's verified notes")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || (*here && *notes) {
		return fail(exitUsage, hostsUsage)
	}
	if *here {
		return a.hostsHere()
	}
	box, err := a.hostsBox()
	if err != nil {
		return err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "hosts"); err != nil {
		return err
	}
	if err := box.sync(ctx, a); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: roster: %v (showing this box's copy)\n", err)
	}
	kv, err := hosts.Bucket(ctx, a.client.JS, hosts.BucketHosts)
	if err != nil {
		return err
	}
	r := box.local.Roster
	seen, err := hosts.ReadPresence(ctx, kv, r, time.Now())
	if err != nil {
		return err
	}
	admin, _ := r.Admin()
	if *notes {
		return a.printNotes(seen)
	}
	if a.json {
		return a.printJSON(map[string]any{"roster_version": r.Version, "admin": admin.Name,
			"fingerprint": hosts.Fingerprint(r.AdminKey), "hosts": seen})
	}
	fmt.Fprintf(a.stdout, "roster v%d, admin %s %s\n", r.Version, admin.Name, hosts.Fingerprint(r.AdminKey))
	for _, s := range seen {
		marks := []string{}
		if s.Host.Name == admin.Name {
			marks = append(marks, "admin")
		}
		if s.Host.Secure {
			marks = append(marks, "secure")
		}
		line := fmt.Sprintf("%-16s %-10s %-13s", s.Host.Name, s.State, strings.Join(marks, " "))
		if s.State != hosts.Absent && s.State != hosts.Unverified {
			p := s.Presence
			routines := "routines off"
			if p.Routines {
				routines = "routines on"
			}
			line += fmt.Sprintf(" %s %s %s, %d repos", p.OS, p.Rev, routines, len(p.Repos))
			if p.Notes != "" {
				line += ", notes"
			}
		}
		line += "  accept " + strings.Join(s.Host.Accept, ",")
		if s.At != "" {
			line += "  seen " + s.At
		}
		fmt.Fprintln(a.stdout, strings.TrimRight(line, " "))
	}
	return nil
}

// hostsHere prints the session line the daemon last wrote, reading only
// that file (marked with its age when the daemon hasn't rewritten it
// lately): nothing when [hosts] is off or the file is missing, and never
// the hub, so a session hook doesn't wait on it.
func (a *app) hostsHere() error {
	if err := a.loadConfig(); err != nil {
		return err
	}
	if !a.cfg.Hosts.Enabled {
		return nil
	}
	p := hosts.HerePath(config.StateDir())
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	st, err := os.Stat(p)
	if err != nil {
		return err
	}
	_, err = io.WriteString(a.stdout, hosts.HereText(string(b), st.ModTime(), time.Now()))
	return err
}

// printNotes prints each box's notes under a "## <host>" heading (--json:
// {host: notes}); an unverified box's are withheld, and a box with none is
// left out.
func (a *app) printNotes(seen []hosts.Seen) error {
	out := map[string]string{}
	for _, s := range seen {
		if s.State != hosts.Unverified && s.Presence.Notes != "" {
			out[s.Host.Name] = s.Presence.Notes
		}
	}
	if a.json {
		return a.printJSON(out)
	}
	if len(out) == 0 {
		fmt.Fprintln(a.stdout, "no notes")
		return nil
	}
	first := true
	for _, s := range seen {
		n, ok := out[s.Host.Name]
		if !ok {
			continue
		}
		if !first {
			fmt.Fprintln(a.stdout)
		}
		first = false
		fmt.Fprintf(a.stdout, "## %s (%s)\n\n%s\n", s.Host.Name, s.State, n)
	}
	for _, s := range seen {
		if s.State == hosts.Unverified {
			fmt.Fprintf(a.stderr, "bmesh: %s: unverified, notes withheld\n", s.Host.Name)
		}
	}
	return nil
}

// hostsBox is this box on the hosts layer, as the CLI sees it.
type hostsBox struct {
	cfg      *config.Config
	host     string
	stateDir string
	key      ed25519.PrivateKey
	local    hosts.Local
}

// hostsBox loads [hosts], the host key and roster.json; a box with hosts
// off or that hasn't joined is refused.
func (a *app) hostsBox() (*hostsBox, error) {
	if err := a.loadConfig(); err != nil {
		return nil, err
	}
	if !a.cfg.Hosts.Enabled {
		return nil, fail(exitRefused, "hosts are off on this box ([hosts] enabled = false): run `bmesh init --hosts`")
	}
	b := &hostsBox{cfg: a.cfg, host: a.cfg.HostName(), stateDir: config.StateDir()}
	key, err := hosts.LoadHostKey(hosts.HostKeyPath(a.cfg.ConfigDir()))
	if err != nil {
		return nil, fail(exitRefused, "no host key (%v): run `bmesh init --hosts`", err)
	}
	b.key = key
	local, ok, err := hosts.LoadLocal(b.stateDir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fail(exitRefused, "this box hasn't joined (no %s): run `bmesh init --hosts`", hosts.LocalPath(b.stateDir))
	}
	b.local = local
	return b, nil
}

// sync takes a newer roster from the hub (and puts this box's back when
// the hub's is missing or behind).
func (b *hostsBox) sync(ctx context.Context, a *app) error {
	kv, err := hosts.Bucket(ctx, a.client.JS, hosts.BucketRoster)
	if err != nil {
		return err
	}
	_, err = hosts.Sync(ctx, kv, b.stateDir, &b.local)
	return err
}

// keyStore is this box's protected key store ([hosts] key_store).
func (a *app) keyStore() (hosts.Store, error) {
	return hosts.NewStore(a.cfg.Hosts.KeyStoreKind(), a.cfg.ConfigDir(), a.stdout, a.passphrase)
}

// lines is the one buffered reader over stdin, shared by init's questions
// and the passphrase prompt.
func (a *app) lines() *bufio.Reader {
	if a.in == nil {
		a.in = bufio.NewReader(a.stdin)
	}
	return a.in
}

// passphrase asks on stderr: without echo at a terminal, else a line of
// stdin. confirm asks twice and refuses a mismatch.
func (a *app) passphrase(prompt string, confirm bool) ([]byte, error) {
	read := func(p string) ([]byte, error) {
		fmt.Fprintf(a.stderr, "%s: ", p)
		if f, ok := a.stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(a.stderr)
			return b, err
		}
		line, err := a.lines().ReadString('\n')
		if err != nil && !(errors.Is(err, io.EOF) && line != "") {
			return nil, fmt.Errorf("no passphrase: %w", err)
		}
		return []byte(strings.TrimRight(line, "\r\n")), nil
	}
	p, err := read(prompt)
	if err != nil || !confirm {
		return p, err
	}
	again, err := read("Again")
	if err != nil {
		return nil, err
	}
	if string(again) != string(p) {
		return nil, errors.New("the passphrases differ")
	}
	return p, nil
}

// adminKey reads the admin key for action, refusing a box that isn't the
// roster's admin and a key that isn't the roster's.
func (a *app) adminKey(b *hostsBox, action string) (ed25519.PrivateKey, error) {
	admin, ok := b.local.Roster.Admin()
	if !b.cfg.Hosts.Admin || !ok || admin.Name != b.host {
		return nil, fail(exitRefused, "only the admin box (%s) can %s", admin.Name, action)
	}
	if !atScreen(a.stdin) {
		return nil, fail(exitRefused, "%s needs the admin at this box's own screen (stdin is not a terminal)", action)
	}
	store, err := a.keyStore()
	if err != nil {
		return nil, err
	}
	key, err := store.Get(hosts.KeyAdmin, action)
	if err != nil {
		return nil, err
	}
	if hosts.KeyHex(key.Public().(ed25519.PublicKey)) != b.local.Roster.AdminKey {
		return nil, fail(exitRefused, "the admin key in the %s store isn't the one the roster names (%s)",
			store.Kind(), hosts.Fingerprint(b.local.Roster.AdminKey))
	}
	return key, nil
}

// acceptFlag is a comma-separated --accept.
func acceptList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return hosts.Names(out)
}

// withRow is the roster's hosts with row h in place of the row of the same
// name (or added).
func withRow(r hosts.Roster, h hosts.Host) []hosts.Host {
	out := []hosts.Host{}
	for _, x := range r.Hosts {
		if x.Name != h.Name {
			out = append(out, x)
		}
	}
	return append(out, h)
}

func (a *app) publish(ctx context.Context, b *hostsBox, key ed25519.PrivateKey, rows []hosts.Host) (hosts.Roster, error) {
	kv, err := hosts.Bucket(ctx, a.client.JS, hosts.BucketRoster)
	if err != nil {
		return hosts.Roster{}, err
	}
	next := b.local.Roster
	next.Hosts = rows
	return hosts.Publish(ctx, kv, b.stateDir, &b.local, next, key, time.Now())
}

// cmdHostsInvite prints a one-time code for name and waits (up to ten
// minutes) for the box to join with it; the join publishes the next
// roster with the box's row.
func (a *app) cmdHostsInvite(args []string) error {
	fs := a.flags("hosts invite")
	secure := fs.Bool("secure", false, "the box is secure: what others ask of it waits for the operator")
	accept := fs.String("accept", strings.Join(defaultAccept, ","), "the ops the box accepts, comma-separated")
	replace := fs.Bool("replace", false, "the name may already be in the roster: the joining box replaces its row")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, "usage: bmesh hosts invite <name> [--secure] [--accept a,b] [--replace]")
	}
	name := pos[0]
	if !hosts.ValidHostName(name) {
		return fail(exitUsage, "host name %q: want lowercase letters, digits and single hyphens (at most 63)", name)
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), hosts.InviteTTL+time.Minute)
	defer cancel()
	if err := a.connect(ctx, "hosts-invite"); err != nil {
		return err
	}
	if err := b.sync(ctx, a); err != nil {
		return fmt.Errorf("roster: %w", err)
	}
	if admin, _ := b.local.Roster.Admin(); name == admin.Name {
		return fail(exitRefused, "%s is the admin box: its row carries the admin key, so no invite replaces it", name)
	}
	if _, taken := b.local.Roster.Host(name); taken && !*replace {
		return fail(exitRefused, "%s is already in the roster (invite it again with --replace)", name)
	}
	key, err := a.adminKey(b, "invite "+name)
	if err != nil {
		return err
	}
	inv, err := hosts.NewInvite(name, time.Now())
	if err != nil {
		return err
	}
	inv.Secure, inv.Accept, inv.Replace = *secure, acceptList(*accept), *replace
	fmt.Fprintf(a.stdout, "invite for %s: %s\n", name, inv.Code())
	fmt.Fprintf(a.stdout, "good once, until %s: on %s run `bmesh init --hosts` and type it at its prompt; waiting\n",
		inv.Expires.Local().Format("15:04"), name)
	var published hosts.Roster
	_, err = hosts.Serve(ctx, a.client.NC, inv, func(req hosts.JoinRequest) (json.RawMessage, string, error) {
		if err := b.sync(ctx, a); err != nil {
			return nil, "", fmt.Errorf("roster: %w", err)
		}
		if _, taken := b.local.Roster.Host(req.Name); taken && !inv.Replace {
			return nil, "", fmt.Errorf("%s is already in the roster", req.Name)
		}
		if _, err := hosts.ParseKey(req.Key); err != nil {
			return nil, "", fmt.Errorf("host key: %w", err)
		}
		row := hosts.Host{Name: req.Name, Key: req.Key, Secure: inv.Secure, Accept: inv.Accept}
		if inv.Secure {
			if _, err := hosts.ParseKey(req.ApproveKey); err != nil {
				return nil, "", errors.New("a secure box joins with an approve key: answer secure at its `bmesh init --hosts`")
			}
			row.ApproveKey = req.ApproveKey
		}
		r, err := a.publish(ctx, b, key, withRow(b.local.Roster, row))
		if err != nil && r.Version == 0 {
			return nil, "", err
		}
		if err != nil {
			fmt.Fprintf(a.stderr, "bmesh: %v\n", err)
		}
		published = r
		raw, err := json.Marshal(r)
		return raw, r.AdminKey, err
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "joined: %s, roster v%d\n", name, published.Version)
	return nil
}

// cmdHostsSet changes a roster row: whether it is secure and what it
// accepts.
func (a *app) cmdHostsSet(args []string) error {
	fs := a.flags("hosts set")
	secure := fs.Bool("secure", false, "the box is secure")
	accept := fs.String("accept", "", "the ops the box accepts, comma-separated (\"\" accepts none)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if len(pos) != 1 || (!given["secure"] && !given["accept"]) {
		return fail(exitUsage, "usage: bmesh hosts set <name> [--secure=true|false] [--accept a,b]")
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "hosts-set"); err != nil {
		return err
	}
	if err := b.sync(ctx, a); err != nil {
		return fmt.Errorf("roster: %w", err)
	}
	row, ok := b.local.Roster.Host(pos[0])
	if !ok {
		return fail(exitRefused, "%s is not in the roster", pos[0])
	}
	if given["secure"] {
		if *secure && row.ApproveKey == "" {
			return fail(exitRefused, "%s has no approve key: invite it again with --secure --replace", row.Name)
		}
		row.Secure = *secure
	}
	if given["accept"] {
		row.Accept = acceptList(*accept)
	}
	key, err := a.adminKey(b, "set "+row.Name)
	if err != nil {
		return err
	}
	r, err := a.publish(ctx, b, key, withRow(b.local.Roster, row))
	if err != nil && r.Version == 0 {
		return err
	}
	state := "not secure"
	if row.Secure {
		state = "secure"
	}
	fmt.Fprintf(a.stdout, "roster v%d: %s %s, accept %s\n", r.Version, row.Name, state, strings.Join(row.Accept, ","))
	return err
}

// cmdHostsRevoke publishes the next roster without a box.
func (a *app) cmdHostsRevoke(args []string) error {
	fs := a.flags("hosts revoke")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, "usage: bmesh hosts revoke <name>")
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	name := pos[0]
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "hosts-revoke"); err != nil {
		return err
	}
	if err := b.sync(ctx, a); err != nil {
		return fmt.Errorf("roster: %w", err)
	}
	if _, ok := b.local.Roster.Host(name); !ok {
		return fail(exitRefused, "%s is not in the roster", name)
	}
	if name == b.host {
		return fail(exitRefused, "the admin can't revoke itself")
	}
	key, err := a.adminKey(b, "revoke "+name)
	if err != nil {
		return err
	}
	rows := slices.DeleteFunc(slices.Clone(b.local.Roster.Hosts), func(h hosts.Host) bool { return h.Name == name })
	r, err := a.publish(ctx, b, key, rows)
	if err != nil && r.Version == 0 {
		return err
	}
	fmt.Fprintf(a.stdout, "roster v%d: %s revoked\n", r.Version, name)
	return err
}

// cmdHostsPing sends ping to every roster box (or one) and prints each
// answer that arrives within --wait; a box that hasn't answered keeps the
// job until it expires.
func (a *app) cmdHostsPing(args []string) error {
	fs := a.flags("hosts ping")
	wait := fs.Duration("wait", 10*time.Second, "how long to wait for answers")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return fail(exitUsage, "usage: bmesh hosts ping [<host>] [--wait D]")
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *wait+time.Minute)
	defer cancel()
	if err := a.connect(ctx, "hosts-ping"); err != nil {
		return err
	}
	if err := b.sync(ctx, a); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: roster: %v (using this box's copy)\n", err)
	}
	r := b.local.Roster
	var targets []string
	for _, h := range r.Hosts {
		if len(pos) == 0 || h.Name == pos[0] {
			targets = append(targets, h.Name)
		}
	}
	if len(targets) == 0 {
		return fail(exitRefused, "%s is not in the roster", pos[0])
	}
	jobs := map[string]hosts.Job{}
	now := time.Now()
	for _, to := range targets {
		jobs[to] = hosts.NewJob(b.key, b.host, to, "ping", "", nil, r.Version, now, pingTTL)
	}
	want := map[string]hosts.Job{}
	for _, j := range jobs {
		want[j.ID] = j
	}
	// Follow the replies before sending, so a fast one isn't missed.
	wctx, wcancel := context.WithTimeout(ctx, *wait)
	defer wcancel()
	answered := map[string]bool{}
	done := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		done <- hosts.WaitReplies(wctx, a.client.JS, b.host, want, &r, ready, func(j hosts.Job, rep hosts.Reply) {
			answered[j.To] = true
			fmt.Fprintf(a.stdout, "%s: %s\n", j.To, rep.Text)
		})
	}()
	select {
	case <-ready:
	case err := <-done:
		return err
	}
	for _, to := range targets {
		if err := hosts.SendJob(ctx, a.client.JS, jobs[to]); err != nil {
			return err
		}
	}
	if err := <-done; err != nil {
		return err
	}
	var states map[string]string
	if kv, err := hosts.Bucket(ctx, a.client.JS, hosts.BucketHosts); err == nil {
		if seen, err := hosts.ReadPresence(ctx, kv, r, time.Now()); err == nil {
			states = map[string]string{}
			for _, s := range seen {
				states[s.Host.Name] = s.State
			}
		}
	}
	for _, to := range targets {
		if answered[to] {
			continue
		}
		exp, _ := hosts.ParseStamp(jobs[to].Expires)
		how := "asleep"
		if states[to] == hosts.Awake {
			how = "no answer in " + wait.String()
		}
		fmt.Fprintf(a.stdout, "%s: %s: queued until %s\n", to, how, exp.Local().Format("2006-01-02 15:04"))
	}
	return nil
}

// cmdHostsReplies prints the replies this box's jobs got, checked against
// the jobs in the stream and the roster.
func (a *app) cmdHostsReplies(args []string) error {
	fs := a.flags("hosts replies")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "usage: bmesh hosts replies [--json]")
	}
	b, err := a.hostsBox()
	if err != nil {
		return err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "hosts-replies"); err != nil {
		return err
	}
	if err := b.sync(ctx, a); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: roster: %v (using this box's copy)\n", err)
	}
	sent, err := hosts.SentJobs(ctx, a.client.JS, b.host)
	if err != nil {
		return err
	}
	reps, err := hosts.Replies(ctx, a.client.JS, b.host)
	if err != nil {
		return err
	}
	type row struct {
		TS       string `json:"ts"`
		By       string `json:"by"`
		JobID    string `json:"job_id"`
		Op       string `json:"op"`
		OK       bool   `json:"ok"`
		Text     string `json:"text"`
		Verified bool   `json:"verified"`
	}
	rows := []row{}
	for _, rep := range reps {
		j, ok := sent[rep.JobID]
		x := row{TS: rep.TS, By: rep.By, JobID: rep.JobID}
		if ok && hosts.CheckReply(&rep, &j, &b.local.Roster) == nil {
			x.Op, x.OK, x.Text, x.Verified = j.Op, rep.OK, rep.Text, true
		}
		rows = append(rows, x)
	}
	if a.json {
		return a.printJSON(rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(a.stdout, "replies: none")
	}
	for _, x := range rows {
		if !x.Verified {
			fmt.Fprintf(a.stdout, "%s %s job %s: unverified, withheld\n", x.TS, x.By, x.JobID)
			continue
		}
		fmt.Fprintf(a.stdout, "%s %s %s %s: %s\n", x.TS, x.By, x.Op, x.JobID, x.Text)
	}
	return nil
}
