// Package config reads bmesh's config.toml: schema, discovery and defaults.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type MeshConfig struct {
	NATS      string
	Domain    string
	TokenFile string `toml:"token_file"`
	Store     string
	Listen    string
}

type HostConfig struct{ Name string }

// HubConfig is the server `bmesh hub` runs on this box (040): Role "" for
// none (a single box's serve embeds the hub; a client dials another box's),
// "hub" for the mesh's hub with JetStream, "leaf" for a leaf of it. Listen
// is the client port, LeafListen the hub's leaf port, Remote the hub's leaf
// URL a leaf dials, Store the hub's store ("" = <state dir>/hub/jetstream).
// The sizes are as nats-server's config takes them ("8GB" is 8 GiB).
type HubConfig struct {
	Role           string
	Listen         string
	LeafListen     string `toml:"leaf_listen"`
	Remote         string
	Store          string
	MaxFileStore   string `toml:"max_file_store"`
	MaxMemoryStore string `toml:"max_memory_store"`
}

// HubRoles are the [hub] role values.
var HubRoles = []string{"", "hub", "leaf"}

// HubStore is [hub] store, expanded, else <state dir>/hub/jetstream.
func (c *Config) HubStore() string {
	if c.Hub.Store != "" {
		return c.Expand(c.Hub.Store)
	}
	return filepath.Join(StateDir(), "hub", "jetstream")
}

// ParseSize reads a byte size the way nats-server's config does: a whole
// number with an optional suffix, k/m/g/t powers of 1000 and kb/mb/gb/tb
// (or ki/kib...) powers of 1024, case-insensitive.
func ParseSize(s string) (int64, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	n, err := strconv.ParseInt(t[:i], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("size %q: want a whole number with an optional unit (256MB, 8GB)", s)
	}
	mult := map[string]int64{"": 1, "k": 1e3, "m": 1e6, "g": 1e9, "t": 1e12}
	for i, u := range []string{"k", "m", "g", "t"} {
		p := int64(1) << (10 * (i + 1))
		mult[u+"b"], mult[u+"i"], mult[u+"ib"] = p, p, p
	}
	m, ok := mult[strings.TrimSpace(t[i:])]
	if !ok {
		return 0, fmt.Errorf("size %q: unknown unit %q (want k, kb, m, mb, g, gb, t or tb)", s, t[i:])
	}
	return n * m, nil
}

type ServeConfig struct{ Listen string }

type RunnerConfig struct {
	Kind        string
	T3Server    string `toml:"t3_server"`       // this box's T3 0.1.0
	T3Runtime   string `toml:"t3_runtime_mode"` // auto | full-access
	TmuxSession string `toml:"tmux_session"`
	Worktrees   string
	Checkouts   string // routine instances' checkouts
	Claude      string // the claude command, split on spaces when run
	MaxLeads    int    `toml:"max_leads"` // leads at once on this host; 0 = no cap
}

// NotifyConfig is where the digest and the pushes go. Push lists the lead
// events pushed at once (empty = none); MapURL is the map's URL for a push's
// click-through ("" = none); CatchUp is how old a missed event may be and
// still be pushed when the daemon starts.
type NotifyConfig struct {
	Ntfy        string
	Command     string
	DigestEvery time.Duration
	Push        []string
	MapURL      string
	CatchUp     time.Duration
}

type LivenessConfig struct {
	Grace     time.Duration
	PollEvery time.Duration
	WakeEvery time.Duration
}

type SpecConfig struct{ Glob string }

type ModelsConfig struct {
	Lead           string // a lead's model when neither --model nor the spec's judge names one
	LeadEffort     string `toml:"lead_effort"` // a lead's effort
	Proposer       string // a proposal round's threads
	ProposerEffort string `toml:"proposer_effort"` // a proposal round's effort
}

// Efforts are the effort levels a thread can be started at.
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

// CheckEffort is nil for "" (the runner's default) and a level of Efforts.
func CheckEffort(e string) error {
	if e == "" || slices.Contains(Efforts, e) {
		return nil
	}
	return fmt.Errorf("effort %q: want one of %s", e, strings.Join(Efforts, ", "))
}

// StatusConfig: KeepDone is how long a done node shows in mesh-status (and
// how long a stopped routine instance's node stays); 0 = forever.
type StatusConfig struct{ KeepDone time.Duration }

// RoutinesConfig: KeepResults is bm-results' age limit; 0 = none.
type RoutinesConfig struct{ KeepResults time.Duration }

// QuotaConfig names the command whose JSON output the map's strip shows; ""
// hides the strip. RemindTopic is the ntfy topic the daemon schedules each
// used 5-hour window's reset reminder on ("" = no reminders).
type QuotaConfig struct {
	Command     string
	RemindTopic string `toml:"remind_topic"`
}

// topicRE is an ntfy topic name.
var topicRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ConsoleConfig holds the thread link templates of the map, {thread} being
// replaced by a lead's runner thread: ThreadURL for leads on this host, Hosts
// by host name for leads on other hosts.
type ConsoleConfig struct {
	ThreadURL string `toml:"thread_url"`
	Hosts     map[string]string
}

// UpdateConfig is the daemon's self-update: Repo names the registry row
// whose main it builds itself from ("" = off). Without it, the daemon reads
// the module's main every CheckEvery (0 = never) and writes the notice when
// this binary is behind; Upstream also installs it.
type UpdateConfig struct {
	Repo       string
	CheckEvery time.Duration
	Upstream   bool
}

// HostsConfig is the hosts layer (boxes joined by the admin's roster):
// nothing of it runs unless Enabled, which only `bmesh init --hosts` sets.
// Admin marks the admin box, Secure a box that holds for the operator what
// others ask of it, KeyStore where the admin or approval key lives
// (keychain or file; "" = keychain on macOS, file elsewhere), PushModes
// the registry modes whose rows a level pushes when this box is ahead (020;
// nil = ship and fork).
type HostsConfig struct {
	Enabled   bool
	Admin     bool
	Secure    bool
	KeyStore  string   `toml:"key_store"`
	PushModes []string `toml:"push_modes"`
}

// KeyStoreKind is [hosts] key_store, else $BMESH_KEY_STORE (tests set it,
// so no test reaches the real Keychain), else keychain on macOS and file
// elsewhere.
func (h HostsConfig) KeyStoreKind() string {
	if h.KeyStore != "" {
		return h.KeyStore
	}
	if k := os.Getenv("BMESH_KEY_STORE"); k == "keychain" || k == "file" {
		return k
	}
	if goos == "darwin" {
		return "keychain"
	}
	return "file"
}

// Config is the parsed config.toml plus the registry read from repos.toml
// beside it. Path-valued fields are stored as written; callers pass them
// through Expand.
type Config struct {
	Mesh     MeshConfig
	Host     HostConfig
	Hub      HubConfig
	Serve    ServeConfig
	Runner   RunnerConfig
	Notify   NotifyConfig
	Liveness LivenessConfig
	Repos    map[string]Repo // from repos.toml, by name
	Spec     SpecConfig
	Models   ModelsConfig
	Quota    QuotaConfig
	Console  ConsoleConfig
	Status   StatusConfig
	Routines RoutinesConfig
	Update   UpdateConfig
	Hosts    HostsConfig

	dir     string   // directory of the config file; "" when none was read
	path    string   // the config file's path as given to Load
	ignored []string // the lines Ignored returns
}

// duration decodes strings like "20m" or "30d" from TOML.
type duration time.Duration

func (d *duration) UnmarshalText(b []byte) error {
	v, err := ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = duration(v)
	return nil
}

// ParseDuration is time.ParseDuration plus whole days, "30d".
func ParseDuration(s string) (time.Duration, error) {
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil || days < 0 {
			return 0, fmt.Errorf("time: invalid duration %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// FormatDuration writes d the way config.toml takes it: "30d" for whole days
// past one, else time.Duration's form without zero minutes and seconds
// ("24h", "1h30m").
func FormatDuration(d time.Duration) string {
	day := 24 * time.Hour
	if d > day && d%day == 0 {
		return strconv.Itoa(int(d/day)) + "d"
	}
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// file mirrors the TOML layout; durations use the text-decoding type.
type file struct {
	Mesh   MeshConfig `toml:"mesh"`
	Host   HostConfig `toml:"host"`
	Hub    HubConfig  `toml:"hub"`
	Serve  ServeConfig
	Runner RunnerConfig
	Notify struct {
		Ntfy        string
		Command     string
		DigestEvery duration  `toml:"digest_every"`
		Push        *[]string // nil = not in the file, so the default
		MapURL      string    `toml:"map_url"`
		CatchUp     duration  `toml:"catch_up"`
	}
	Liveness struct {
		Grace     duration
		PollEvery duration `toml:"poll_every"`
		WakeEvery duration `toml:"wake_every"`
	}
	Repos   map[string]any // refused: the registry lives in repos.toml
	Spec    SpecConfig
	Models  ModelsConfig
	Quota   QuotaConfig
	Console ConsoleConfig
	Status  struct {
		KeepDone duration `toml:"keep_done"`
	}
	Routines struct {
		KeepResults duration `toml:"keep_results"`
	}
	Update struct {
		Repo       string
		CheckEvery duration `toml:"check_every"`
		Upstream   bool
	}
	Hosts HostsConfig
}

func defaults() *Config {
	return &Config{
		Mesh:  MeshConfig{Domain: "hub", Store: DefaultStore(), Listen: "127.0.0.1:4222"},
		Hub:   HubConfig{MaxFileStore: "8GB", MaxMemoryStore: "256MB"},
		Serve: ServeConfig{Listen: "127.0.0.1:3774"},
		Runner: RunnerConfig{Kind: "t3", T3Server: "http://127.0.0.1:3773", T3Runtime: "auto", TmuxSession: "bmesh",
			Worktrees: "~/.bmesh/worktrees", Checkouts: "~/.bmesh/checkouts", Claude: "claude"},
		Notify:   NotifyConfig{DigestEvery: time.Hour, Push: []string{"ask", "blocked", "done"}, CatchUp: time.Hour},
		Liveness: LivenessConfig{Grace: 20 * time.Minute, PollEvery: 60 * time.Second, WakeEvery: 10 * time.Minute},
		Repos:    map[string]Repo{},
		Spec:     SpecConfig{Glob: "docs/proposals/{id}-*.md"},
		Models: ModelsConfig{Lead: "claude-opus-5-5", LeadEffort: "high", Proposer: "claude-opus-5-5",
			ProposerEffort: "xhigh"},
		Console:  ConsoleConfig{Hosts: map[string]string{}},
		Status:   StatusConfig{KeepDone: 24 * time.Hour},
		Routines: RoutinesConfig{KeepResults: 30 * 24 * time.Hour},
		Update:   UpdateConfig{CheckEvery: 6 * time.Hour},
	}
}

// Dir is the config directory: ~/.config/bmesh when it exists (where an
// earlier bmesh kept it on every OS), else the user's config dir
// (os.UserConfigDir: ~/.config on Linux, ~/Library/Application Support on
// macOS, %AppData% on Windows) plus bmesh. The home directory stands in
// when the OS names none.
func Dir() string {
	if home, err := os.UserHomeDir(); err == nil {
		if old := filepath.Join(home, ".config", "bmesh"); dirExists(old) {
			return old
		}
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d, _ = os.UserHomeDir()
		d = filepath.Join(d, ".config")
	}
	return filepath.Join(d, "bmesh")
}

// DefaultStore is the embedded server's store: the store an earlier bmesh
// made at ~/.local/state/bmesh/jetstream when it exists (so an upgrade keeps
// its buckets), else under the user's cache dir (os.UserCacheDir: ~/.cache
// on Linux, ~/Library/Caches on macOS, %LocalAppData% on Windows).
func DefaultStore() string {
	if home, err := os.UserHomeDir(); err == nil {
		if old := filepath.Join(home, ".local", "state", "bmesh", "jetstream"); dirExists(old) {
			return old
		}
	}
	d, err := os.UserCacheDir()
	if err != nil {
		d, _ = os.UserHomeDir()
		d = filepath.Join(d, ".cache")
	}
	return filepath.Join(d, "bmesh", "jetstream")
}

// CacheDir is where bmesh keeps what it writes for reading later (briefs,
// prompts, a claude session's files): os.UserCacheDir, or
// $BMESH_CACHE_DIR when it is an absolute path. Tests set it to keep out of
// the box's own cache, which on macOS os.UserCacheDir always is (it ignores
// XDG_CACHE_HOME there, and a box's CLI and daemon rely on agreeing).
func CacheDir() (string, error) {
	if d := os.Getenv("BMESH_CACHE_DIR"); filepath.IsAbs(d) {
		return d, nil
	}
	return os.UserCacheDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Path is $BMESH_CONFIG, else <Dir>/config.toml.
func Path() string {
	if p := os.Getenv("BMESH_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(Dir(), "config.toml")
}

// SetupFlag is the file bmesh init writes last, beside the config file: its
// presence means the box has been set up.
func SetupFlag(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), ".setup-complete")
}

// Load reads path over the defaults and the registry from repos.toml beside
// it. A missing file yields the defaults; unknown keys are ignored, except
// [repos], which is refused: it moved to repos.toml. Keys bmesh dropped
// load too, each named by a line in Ignored.
func Load(path string) (*Config, error) {
	c := defaults()
	c.path = path
	if abs, err := filepath.Abs(path); err == nil {
		c.dir = filepath.Dir(abs)
	}
	repos, err := LoadRepos(ReposPath(path))
	if err != nil {
		return nil, err
	}
	c.Repos = repos
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return c, nil
	}
	f := file{
		Mesh: c.Mesh, Host: c.Host, Hub: c.Hub, Serve: c.Serve, Runner: c.Runner, Spec: c.Spec, Models: c.Models,
		Quota: c.Quota, Console: c.Console, Hosts: c.Hosts,
	}
	f.Update.Repo, f.Update.CheckEvery, f.Update.Upstream = c.Update.Repo, duration(c.Update.CheckEvery), c.Update.Upstream
	f.Notify.DigestEvery = duration(c.Notify.DigestEvery)
	f.Notify.CatchUp = duration(c.Notify.CatchUp)
	f.Liveness.Grace = duration(c.Liveness.Grace)
	f.Liveness.PollEvery = duration(c.Liveness.PollEvery)
	f.Liveness.WakeEvery = duration(c.Liveness.WakeEvery)
	f.Status.KeepDone = duration(c.Status.KeepDone)
	f.Routines.KeepResults = duration(c.Routines.KeepResults)
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	for _, key := range []string{"t3_dispatch", "t3_project"} {
		if md.IsDefined("runner", key) {
			c.ignored = append(c.ignored, fmt.Sprintf("bmesh: [runner] %s is ignored (T3 0.1.0 needs no script); delete it from %s", key, path))
		}
	}
	c.Mesh, c.Host, c.Hub, c.Serve, c.Runner, c.Spec, c.Models = f.Mesh, f.Host, f.Hub, f.Serve, f.Runner, f.Spec, f.Models
	if !slices.Contains(HubRoles, c.Hub.Role) {
		return nil, fmt.Errorf("config %s: [hub] role %q: want \"\" (no hub here), hub or leaf", path, c.Hub.Role)
	}
	for key, v := range map[string]string{"max_file_store": c.Hub.MaxFileStore, "max_memory_store": c.Hub.MaxMemoryStore} {
		if _, err := ParseSize(v); err != nil {
			return nil, fmt.Errorf("config %s: [hub] %s: %w", path, key, err)
		}
	}
	c.Quota, c.Console, c.Hosts = f.Quota, f.Console, f.Hosts
	c.Update = UpdateConfig{Repo: f.Update.Repo, CheckEvery: time.Duration(f.Update.CheckEvery), Upstream: f.Update.Upstream}
	if k := c.Hosts.KeyStore; k != "" && k != "keychain" && k != "file" {
		return nil, fmt.Errorf("config %s: [hosts] key_store %q: want keychain or file", path, k)
	}
	for _, m := range c.Hosts.PushModes {
		if !slices.Contains(Modes, m) {
			return nil, fmt.Errorf("config %s: [hosts] push_modes: %q is not one of %s", path, m, strings.Join(Modes, ", "))
		}
	}
	for key, e := range map[string]string{"lead_effort": c.Models.LeadEffort, "proposer_effort": c.Models.ProposerEffort} {
		if err := CheckEffort(e); err != nil {
			return nil, fmt.Errorf("config %s: [models] %s: %w", path, key, err)
		}
	}
	if c.Runner.T3Server == "" {
		return nil, fmt.Errorf("config %s: [runner] t3_server is empty: want this box's T3 0.1.0 (the default is http://127.0.0.1:3773)", path)
	}
	if m := c.Runner.T3Runtime; m != "auto" && m != "full-access" {
		return nil, fmt.Errorf("config %s: [runner] t3_runtime_mode %q: want auto or full-access (approval-required and auto-accept-edits stall an unattended lead)", path, m)
	}
	if c.Console.Hosts == nil {
		c.Console.Hosts = map[string]string{}
	}
	push := c.Notify.Push
	if f.Notify.Push != nil {
		push = append([]string{}, *f.Notify.Push...) // push = [] stays empty: no pushes
	}
	for _, e := range push {
		if e != "ask" && e != "blocked" && e != "done" {
			return nil, fmt.Errorf("config %s: [notify] push: unknown event %q", path, e)
		}
	}
	c.Notify = NotifyConfig{Ntfy: f.Notify.Ntfy, Command: f.Notify.Command, DigestEvery: time.Duration(f.Notify.DigestEvery),
		Push: push, MapURL: f.Notify.MapURL, CatchUp: time.Duration(f.Notify.CatchUp)}
	if t := c.Quota.RemindTopic; t != "" {
		switch {
		case !topicRE.MatchString(t):
			return nil, fmt.Errorf("config %s: [quota] remind_topic %q: an ntfy topic is 1 to 64 of A-Z, a-z, 0-9, _ and -", path, t)
		case c.Quota.Command == "":
			return nil, fmt.Errorf("config %s: [quota] remind_topic needs [quota] command, which the readings come from", path)
		case c.Notify.Ntfy == "":
			return nil, fmt.Errorf("config %s: [quota] remind_topic needs [notify] ntfy, whose URL the topic goes into", path)
		}
	}
	c.Liveness = LivenessConfig{Grace: time.Duration(f.Liveness.Grace), PollEvery: time.Duration(f.Liveness.PollEvery),
		WakeEvery: time.Duration(f.Liveness.WakeEvery)}
	c.Status = StatusConfig{KeepDone: time.Duration(f.Status.KeepDone)}
	c.Routines = RoutinesConfig{KeepResults: time.Duration(f.Routines.KeepResults)}
	for key, d := range map[string]time.Duration{"[status] keep_done": c.Status.KeepDone, "[routines] keep_results": c.Routines.KeepResults,
		"[update] check_every": c.Update.CheckEvery} {
		if d < 0 {
			return nil, fmt.Errorf("config %s: %s: negative duration", path, key)
		}
	}
	if f.Repos != nil {
		return nil, fmt.Errorf("config %s: [repos] has moved to %s: one [repos.<name>] table per repo with path, mode, remote, host, ux_host, verify, surfaces, pin and routines (see README, Config)",
			path, ReposPath(path))
	}
	return c, nil
}

// Ignored is one line per key the config file sets that bmesh no longer
// reads, saying to delete it.
func (c *Config) Ignored() []string { return c.ignored }

// Path is the config file's path as loaded.
func (c *Config) Path() string { return c.path }

// ConfigDir is the directory of the config file, else Dir: where host.key
// and the file store's keys live.
func (c *Config) ConfigDir() string {
	if c.dir != "" {
		return c.dir
	}
	return Dir()
}

// T3TokenPath is the T3 sign-in `bmesh t3 login` writes: t3-token.json
// beside the config file (in Dir when no file was read).
func (c *Config) T3TokenPath() string {
	d := c.dir
	if d == "" {
		d = Dir()
	}
	return filepath.Join(d, "t3-token.json")
}

// goos is runtime.GOOS; tests set it.
var goos = runtime.GOOS

// StateDir is where the daemon keeps what must survive a restart:
// $XDG_STATE_HOME/bmesh on any OS, else ~/Library/Application
// Support/bmesh/state on macOS and ~/.local/state/bmesh elsewhere. Never the
// cache dir, which macOS purges.
func StateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "bmesh")
	}
	home, _ := os.UserHomeDir()
	if goos == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "bmesh", "state")
	}
	return filepath.Join(home, ".local", "state", "bmesh")
}

// HostName is [host] name, else the short host name, lowercased.
func (c *Config) HostName() string {
	n := c.Host.Name
	if n == "" {
		n, _ = os.Hostname()
		n, _, _ = strings.Cut(n, ".")
	}
	return strings.ToLower(n)
}

// HubURL is the hub the CLI dials: [mesh] nats, else (embedded true) the
// hub bmesh serve embeds on this box, nats://<[mesh] listen>, with an
// unspecified host (0.0.0.0, ::, "") dialled as 127.0.0.1.
func (c *Config) HubURL() (url string, embedded bool) {
	if c.Mesh.NATS != "" {
		return c.Mesh.NATS, false
	}
	addr := c.Mesh.Listen
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
			addr = net.JoinHostPort("127.0.0.1", port)
		}
	}
	return "nats://" + addr, true
}

// Token reads [mesh] token_file; "" when none is configured.
func (c *Config) Token() (string, error) {
	if c.Mesh.TokenFile == "" {
		return "", nil
	}
	b, err := os.ReadFile(c.Expand(c.Mesh.TokenFile))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// Expand replaces a leading ~ with the home directory.
func (c *Config) Expand(p string) string { return ExpandHome(p) }

// ExpandHome replaces a leading ~ with the home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// RepoPath resolves a [repos] key, else an existing directory, to (name, abs path).
// A relative [repos] path resolves against the directory of the config file
// (the current directory when no file was read); a relative nameOrPath
// resolves against the current directory. A directory that is a row's path
// (symlinks resolved) gets that row's name, else its base name.
func (c *Config) RepoPath(nameOrPath string) (name, path string, err error) {
	if _, ok := c.Repos[nameOrPath]; ok {
		abs, err := c.rowPath(nameOrPath)
		return nameOrPath, abs, err
	}
	p := c.Expand(nameOrPath)
	if st, serr := os.Stat(p); serr == nil && st.IsDir() {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", "", err
		}
		want := resolved(abs)
		for _, n := range c.RepoNames() {
			if rp, err := c.rowPath(n); err == nil && resolved(rp) == want {
				return n, abs, nil
			}
		}
		return filepath.Base(abs), abs, nil
	}
	return "", "", fmt.Errorf("unknown repo %q: not a [repos] name or an existing directory", nameOrPath)
}

// rowPath is a row's path, expanded and absolute.
func (c *Config) rowPath(name string) (string, error) {
	p := c.Expand(c.Repos[name].Path)
	if !filepath.IsAbs(p) && c.dir != "" {
		p = filepath.Join(c.dir, p)
	}
	return filepath.Abs(p)
}

// resolved is p with symlinks resolved, or p when that fails.
func resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
