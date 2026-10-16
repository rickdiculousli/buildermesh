package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
)

// joinWait bounds a join: the admin's command is waiting, so the answer is
// quick or the code is wrong, used or expired. Tests shorten it.
var joinWait = 30 * time.Second

// initHosts joins this box to the mesh, or makes it the admin: it asks
// join or admin (admin when --admin), whether the box is secure and
// whether its daemon runs routines, with the current answers as defaults;
// makes the keys; joins with a one-time code typed at the prompt, or (the
// admin) publishes the roster with its own row; and last writes [hosts]
// and, with routines off, [runner] checkouts = "" (keeping config.toml.prev).
func (a *app) initHosts(cfgPath string, admin bool, ask *asker) error {
	if _, ok := statOK(cfgPath); !ok {
		return fail(exitRefused, "no %s: run `bmesh init` first, then `bmesh init --hosts`", cfgPath)
	}
	a.configPath = cfgPath
	if err := a.loadConfig(); err != nil {
		return err
	}
	cfg := a.cfg
	host := cfg.HostName()
	if !hosts.ValidHostName(host) {
		return fail(exitRefused, "host name %q isn't one subject token: set [host] name in %s", host, cfgPath)
	}
	stateDir, cfgDir := config.StateDir(), cfg.ConfigDir()
	local, joined, err := hosts.LoadLocal(stateDir)
	if err != nil {
		fmt.Fprintf(a.stdout, "ignoring %s: %v\n", hosts.LocalPath(stateDir), err)
		joined = false
	}

	if !admin {
		def := "join"
		if cfg.Hosts.Admin {
			def = "admin"
		}
		admin = strings.HasPrefix(ask.choice("Join the mesh with an invite, or be its admin? [join/admin]", def), "a")
	}
	defSecure := admin
	if cfg.Hosts.Enabled {
		defSecure = cfg.Hosts.Secure
	}
	secure := yes(ask.choice(fmt.Sprintf("Is this box secure (what others ask of it waits for your approval)? %s", yn(defSecure)), word(defSecure)))
	defRoutines := !secure
	if cfg.Hosts.Enabled {
		defRoutines = cfg.Runner.Checkouts != ""
	}
	routines := yes(ask.choice(fmt.Sprintf("Does this box's daemon run routines? %s", yn(defRoutines)), word(defRoutines)))

	hostKey, _, err := hosts.EnsureHostKey(hosts.HostKeyPath(cfgDir))
	if err != nil {
		return err
	}
	hostPub := hosts.KeyHex(hostKey.Public().(ed25519.PublicKey))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if admin {
		err = a.becomeAdmin(ctx, host, hostPub, secure, stateDir, local, joined)
	} else {
		err = a.join(ctx, ask, host, hostPub, secure, stateDir, local, joined)
	}
	if err != nil {
		return err
	}
	prev, err := writeHostsConfig(cfgPath, admin, secure, routines)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s (the old one is %s)\n", cfgPath, prev)
	fmt.Fprintln(a.stdout, "restart the daemon so it takes the hosts layer up: systemctl --user restart bmesh, or launchctl kickstart -k gui/$(id -u)/com.buildermesh.bmesh")
	return nil
}

func yes(s string) bool { return strings.HasPrefix(s, "y") }

func yn(def bool) string {
	if def {
		return "[Y/n]"
	}
	return "[y/N]"
}

func word(def bool) string {
	if def {
		return "y"
	}
	return "n"
}

// protectedKey is the public half of this box's protected key name (admin
// or approve), and the key itself when it was just made (else nil): a key
// that exists is only read, behind its dialog or passphrase, when its
// public half isn't in <config dir>/<name>.pub.
func (a *app) protectedKey(name string) (pub string, made ed25519.PrivateKey, err error) {
	store, err := a.keyStore()
	if err != nil {
		return "", nil, err
	}
	pubPath := filepath.Join(a.cfg.ConfigDir(), name+".pub")
	have, err := store.Exists(name)
	if err != nil {
		return "", nil, err
	}
	if have {
		if b, err := os.ReadFile(pubPath); err == nil {
			if _, err := hosts.ParseKey(strings.TrimSpace(string(b))); err == nil {
				return strings.TrimSpace(string(b)), nil, nil
			}
		}
		key, err := store.Get(name, "read the "+name+" key's public half")
		if err != nil {
			return "", nil, err
		}
		pub = hosts.KeyHex(key.Public().(ed25519.PublicKey))
		return pub, nil, os.WriteFile(pubPath, []byte(pub+"\n"), 0o644)
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return "", nil, err
	}
	key := ed25519.NewKeyFromSeed(seed)
	if err := store.Put(name, key); err != nil {
		return "", nil, err
	}
	pub = hosts.KeyHex(key.Public().(ed25519.PublicKey))
	fmt.Fprintf(a.stdout, "made the %s key (%s store), fingerprint %s\n", name, store.Kind(), hosts.Fingerprint(pub))
	return pub, key, os.WriteFile(pubPath, []byte(pub+"\n"), 0o644)
}

// becomeAdmin makes the admin key (when missing) and publishes a roster
// with this box's row, unless the roster already has that row as it
// should be. It starts from this box's roster when it pinned its own key,
// else from the hub's when the same key signed it, else from nothing.
func (a *app) becomeAdmin(ctx context.Context, host, hostPub string, secure bool, stateDir string, local hosts.Local, joined bool) error {
	adminPub, adminKey, err := a.protectedKey(hosts.KeyAdmin)
	if err != nil {
		return err
	}
	if err := a.connect(ctx, "init-hosts"); err != nil {
		return err
	}
	kv, err := hosts.Bucket(ctx, a.client.JS, hosts.BucketRoster)
	if err != nil {
		return err
	}
	hub, hubOK, err := hosts.HubRoster(ctx, kv)
	if err != nil {
		return err
	}
	if hubOK && hub.AdminKey != adminPub {
		fmt.Fprintf(a.stdout, "a roster signed by %s exists; becoming admin replaces it on every box you invite again\n",
			hosts.Fingerprint(hub.AdminKey))
	}
	if !joined || local.Pinned != adminPub {
		local = hosts.Local{Pinned: adminPub}
	}
	if hubOK {
		local.Offer(hub) // the same key's newer roster: keep its rows and version
	}
	row := hosts.Host{Name: host, Key: hostPub, Secure: secure, ApproveKey: adminPub, Accept: defaultAccept}
	if cur, ok := local.Roster.Host(host); ok {
		row.Accept = cur.Accept
		if cur.Key == row.Key && cur.Secure == row.Secure && cur.ApproveKey == row.ApproveKey {
			if err := hosts.SaveLocal(stateDir, local); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "admin: %s %s, roster v%d\n", host, hosts.Fingerprint(adminPub), local.Roster.Version)
			return nil
		}
	}
	if adminKey == nil {
		if !atScreen(a.stdin) {
			return fail(exitRefused, "signing the roster needs the admin at this box's own screen (stdin is not a terminal)")
		}
		store, err := a.keyStore()
		if err != nil {
			return err
		}
		if adminKey, err = store.Get(hosts.KeyAdmin, fmt.Sprintf("sign roster v%d", local.Roster.Version+1)); err != nil {
			return err
		}
		if hosts.KeyHex(adminKey.Public().(ed25519.PublicKey)) != adminPub {
			return fail(exitRefused, "the admin key doesn't match %s", filepath.Join(a.cfg.ConfigDir(), "admin.pub"))
		}
	}
	next := local.Roster
	next.Hosts = withRow(local.Roster, row)
	r, err := hosts.Publish(ctx, kv, stateDir, &local, next, adminKey, time.Now())
	if err != nil && r.Version == 0 {
		return err
	}
	if err != nil {
		fmt.Fprintf(a.stderr, "bmesh: %v\n", err)
	}
	fmt.Fprintf(a.stdout, "admin: %s %s, roster v%d\n", host, hosts.Fingerprint(adminPub), r.Version)
	return nil
}

// join asks for the one-time code (at the prompt, never in argv) and joins
// with it: the admin's reply pins its key and gives the roster. A joined
// box may leave the code empty to keep its roster.
func (a *app) join(ctx context.Context, ask *asker, host, hostPub string, secure bool, stateDir string, local hosts.Local, joined bool) error {
	approvePub := ""
	if secure {
		var err error
		if approvePub, _, err = a.protectedKey(hosts.KeyApprove); err != nil {
			return err
		}
	}
	question := "Invite code (from `bmesh hosts invite " + host + "` on the admin box)"
	if joined {
		question += fmt.Sprintf("; empty keeps roster v%d", local.Roster.Version)
	}
	code := strings.TrimSpace(ask.ask(question, ""))
	if code == "" {
		if !joined {
			return fail(exitUsage, "joining needs an invite code: run `bmesh hosts invite %s` on the admin box", host)
		}
		if row, ok := local.Roster.Host(host); ok && row.Secure != secure {
			fmt.Fprintf(a.stdout, "the roster has %s secure=%v; the admin changes it (`bmesh hosts set` or a new invite)\n", host, row.Secure)
		}
		return nil
	}
	if joined {
		fmt.Fprintf(a.stdout, "this box pinned admin %s; the new invite re-pins it\n", hosts.Fingerprint(local.Pinned))
	}
	if err := a.connect(ctx, "init-hosts"); err != nil {
		return err
	}
	jctx, cancel := context.WithTimeout(ctx, joinWait)
	defer cancel()
	rep, err := hosts.Join(jctx, a.client.NC, code, hosts.JoinRequest{Name: host, Key: hostPub, ApproveKey: approvePub})
	if err != nil {
		return fail(exitRefused, "join: %v", err)
	}
	var r hosts.Roster
	if err := json.Unmarshal(rep.Roster, &r); err != nil {
		return fmt.Errorf("join: the roster: %w", err)
	}
	if err := r.Verify(rep.AdminKey); err != nil {
		return fmt.Errorf("join: %w", err)
	}
	row, ok := r.Host(host)
	if !ok || row.Key != hostPub {
		return fmt.Errorf("join: roster v%d doesn't carry this box's key", r.Version)
	}
	if err := hosts.SaveLocal(stateDir, hosts.Local{Pinned: rep.AdminKey, Roster: r}); err != nil {
		return err
	}
	adminRow, _ := r.Admin()
	fmt.Fprintf(a.stdout, "joined: roster v%d, admin %s %s\n", r.Version, adminRow.Name, hosts.Fingerprint(rep.AdminKey))
	if row.Secure != secure {
		fmt.Fprintf(a.stdout, "the invite has %s secure=%v; the admin changes it with a new invite\n", host, row.Secure)
	}
	return nil
}

var (
	tableRE = regexp.MustCompile(`^\s*\[`)
	keyRE   = func(k string) *regexp.Regexp { return regexp.MustCompile(`^\s*` + k + `\s*=`) }
)

// writeHostsConfig sets [hosts] enabled, admin and secure in cfgPath and,
// with routines off, [runner] checkouts = "" (with routines on, a
// checkouts = "" line goes, back to the default). Every other line stays
// as written; the old file is kept as config.toml.prev. The result must
// load, else the old file is put back.
func writeHostsConfig(cfgPath string, admin, secure, routines bool) (prev string, err error) {
	old, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(string(old), "\n"), "\n")
	hostsLines := []string{"enabled = true", fmt.Sprintf("admin = %v", admin), fmt.Sprintf("secure = %v", secure)}
	lines = setTable(lines, "hosts", func(body []string) []string {
		body = slices.DeleteFunc(body, func(l string) bool {
			return keyRE("enabled").MatchString(l) || keyRE("admin").MatchString(l) || keyRE("secure").MatchString(l)
		})
		return append(append([]string{}, hostsLines...), body...)
	}, true)
	lines = setTable(lines, "runner", func(body []string) []string {
		out := []string{}
		had := false
		for _, l := range body {
			if keyRE("checkouts").MatchString(l) {
				had = true
				if !routines {
					out = append(out, `checkouts = ""   # routines off on this box (bmesh init --hosts)`)
					continue
				}
				if v := strings.TrimSpace(strings.SplitN(strings.SplitN(l, "=", 2)[1], "#", 2)[0]); v == `""` {
					continue // routines back on: the default checkout root
				}
			}
			out = append(out, l)
		}
		if !routines && !had {
			out = append([]string{`checkouts = ""   # routines off on this box (bmesh init --hosts)`}, out...)
		}
		return out
	}, !routines)
	prev = cfgPath + ".prev"
	if err := os.WriteFile(prev, old, 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(cfgPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return "", err
	}
	if _, err := config.Load(cfgPath); err != nil {
		os.WriteFile(cfgPath, old, 0o600)
		return "", fmt.Errorf("the edited config doesn't load, so it is as it was: %w", err)
	}
	return prev, nil
}

// setTable rewrites the body of table [name] (the lines up to the next
// table header) with edit; a missing table is appended when add.
func setTable(lines []string, name string, edit func([]string) []string, add bool) []string {
	header := regexp.MustCompile(`^\s*\[\s*` + regexp.QuoteMeta(name) + `\s*\]\s*(#.*)?$`)
	start := slices.IndexFunc(lines, header.MatchString)
	if start < 0 {
		if !add {
			return lines
		}
		return append(append(lines, "", "["+name+"]"), edit(nil)...)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if tableRE.MatchString(lines[i]) {
			end = i
			break
		}
	}
	// Blank lines and comments that end the body belong to what follows.
	tail := end
	for tail > start+1 && (strings.TrimSpace(lines[tail-1]) == "" || strings.HasPrefix(strings.TrimSpace(lines[tail-1]), "#")) {
		tail--
	}
	out := append([]string{}, lines[:start+1]...)
	out = append(out, edit(append([]string{}, lines[start+1:tail]...))...)
	return append(out, lines[tail:]...)
}
