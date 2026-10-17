package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

const initUsage = "usage: bmesh init [--service] [--force] [--role hub|leaf|client] [--hub URL] [--token-file F] [--runner claude|t3|tmux] [--no-repo] | bmesh init --hosts [--admin]"

// runnerKinds are the runners init looks for on PATH, in order of
// preference: claude needs nothing else.
var runnerKinds = []string{"claude", "t3", "tmux"}

// cmdInit sets a box up: config.toml, repos.toml with the current repo, the
// service unit with --service, and the setup flag last. Answers come from
// flags, else from questions on stdin (an empty line, or no stdin, takes
// the default), so a script can drive it.
func (a *app) cmdInit(args []string) error {
	fs := a.flags("init")
	service := fs.Bool("service", false, "write the systemd user unit (Linux) or launchd plist (macOS) for bmesh serve, and for bmesh hub when [hub] role is set, or print the schtasks commands (Windows)")
	force := fs.Bool("force", false, "rewrite an existing config.toml and this repo's registry row, and with --service replace a unit that differs from what init writes")
	role := fs.String("role", "", "multi-box: this box's part, hub (runs the mesh's hub), leaf (runs a leaf of it) or client (dials it)")
	hub := fs.String("hub", "", "multi-box client: the NATS hub URL (nats://host:4222)")
	tokenFile := fs.String("token-file", "", "multi-box: the hub's token file")
	runnerFlag := fs.String("runner", "", "runner kind (default: claude when on PATH, else t3, else tmux)")
	noRepo := fs.Bool("no-repo", false, "do not register the current directory's repo")
	hostsFlag := fs.Bool("hosts", false, "join this box to the mesh's roster, or (--admin) make it the admin")
	admin := fs.Bool("admin", false, "with --hosts: make this box the admin")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || (*admin && !*hostsFlag) {
		return fail(exitUsage, initUsage)
	}
	cfgPath := a.configPath
	if cfgPath == "" {
		cfgPath = config.Path()
	}
	if abs, err := filepath.Abs(cfgPath); err == nil {
		cfgPath = abs
	}
	dir := filepath.Dir(cfgPath)
	ask := &asker{in: a.lines(), out: a.stdout}
	if *hostsFlag {
		return a.initHosts(cfgPath, *admin, ask)
	}
	wrote := []string{}

	// The repo to register is checked before anything is written: a repo
	// without main, or a name whose node token another row has, is refused.
	reposPath := config.ReposPath(cfgPath)
	var register *config.Repo
	if r, ok := cwdRepo(); ok && !*noRepo {
		if !config.ValidName(r.Name) {
			fmt.Fprintf(a.stdout, "not registering %s: a registry name matches %s (rename the directory, or add a row to %s by hand)\n",
				r.Path, config.NamePattern, reposPath)
		} else {
			repos, err := config.LoadRepos(reposPath)
			if err != nil {
				return err
			}
			if _, have := repos[r.Name]; have && !*force {
				fmt.Fprintf(a.stdout, "%s is already in %s\n", r.Name, reposPath)
			} else {
				if !hasMain(r.Path) {
					return errNoMain(r.Path)
				}
				repos[r.Name] = r
				if err := tokenClash(repos); err != nil {
					return err
				}
				register = &r
			}
		}
	}

	_, exists := statOK(cfgPath)
	if exists && !*force {
		fmt.Fprintf(a.stdout, "%s exists; leaving it as it is (--force rewrites it)\n", cfgPath)
	} else {
		m, err := ask.meshAnswer(*role, *hub, *tokenFile, dir)
		if err != nil {
			return err
		}
		if m.role == "hub" {
			made, err := makeToken(config.ExpandHome(m.tokenFile))
			if err != nil {
				return err
			}
			if made {
				wrote = append(wrote, config.ExpandHome(m.tokenFile))
			}
		}
		if m.role == "leaf" {
			if _, ok := statOK(config.ExpandHome(m.tokenFile)); !ok {
				fmt.Fprintf(a.stdout, "no token at %s yet: copy the hub's there (mode 0600) before bmesh hub starts\n", m.tokenFile)
			}
		}
		kind := *runnerFlag
		found := foundRunners()
		if len(found) == 0 {
			fmt.Fprintln(a.stdout, "no runner on PATH (claude, t3, tmux); install Claude Code and bmesh will use it")
		} else {
			fmt.Fprintf(a.stdout, "runners on PATH: %s\n", strings.Join(found, ", "))
		}
		if kind == "" {
			kind = runnerKinds[0]
			if len(found) > 0 {
				kind = found[0]
			}
		}
		if !contains(runnerKinds, kind) {
			return fail(exitUsage, "runner %q is not one of %s", kind, strings.Join(runnerKinds, ", "))
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(cfgPath, []byte(configBody(m, kind)), 0o600); err != nil {
			return err
		}
		wrote = append(wrote, cfgPath)
	}

	if r := register; r != nil {
		if strings.HasPrefix(ask.choice(fmt.Sprintf("Register %s (%s) in %s? [Y/n]", r.Name, r.Path, reposPath), "y"), "y") {
			if err := config.WriteRepo(reposPath, *r); err != nil {
				return err
			}
			wrote = append(wrote, reposPath)
		}
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	var enable []string
	if *service {
		units := []unit{serveUnit}
		if cfg.Hub.Role != "" {
			units = append(units, hubUnit)
		}
		for _, u := range units {
			path, state, how, err := writeService(dir, u, *force)
			if err != nil {
				return err
			}
			switch state {
			case unitWrote:
				wrote = append(wrote, path)
			case unitUnchanged:
				fmt.Fprintf(a.stdout, "unchanged %s\n", path)
			case unitKept:
				// Nobody enables a unit they haven't looked at.
				fmt.Fprintf(a.stdout, "kept %s (it differs from what init writes); the new one is %s.new: diff them, and move it over to take it\n", path, path)
				continue
			}
			enable = append(enable, how...)
		}
	}

	flag := config.SetupFlag(cfgPath)
	if err := os.WriteFile(flag, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		return err
	}
	for _, w := range wrote {
		fmt.Fprintf(a.stdout, "wrote %s\n", w)
	}
	printSettings(a.stdout, cfg, installedService())
	fmt.Fprintf(a.stdout, "set up (%s). Next:\n", flag)
	if len(enable) > 0 {
		for _, l := range enable {
			fmt.Fprintf(a.stdout, "  %s\n", l)
		}
	} else if !*service {
		if cfg.Hub.Role != "" {
			fmt.Fprintf(a.stdout, "  bmesh hub          # this box's %s; keep it running, apart from serve\n", cfg.Hub.Role)
		}
		fmt.Fprintln(a.stdout, "  bmesh serve        # the daemon and the map (and the hub on a single box); keep it running")
	}
	fmt.Fprintln(a.stdout, "  bmesh repos        # the registry")
	fmt.Fprintln(a.stdout, "  bmesh mesh-status  # every node on the mesh")
	return nil
}

// asker prints a question with its default and reads one line; EOF or an
// empty line is the default.
type asker struct {
	in  *bufio.Reader
	out io.Writer
}

func (q *asker) ask(question, def string) string {
	if def != "" {
		fmt.Fprintf(q.out, "%s (%s): ", question, def)
	} else {
		fmt.Fprintf(q.out, "%s: ", question)
	}
	line, err := q.in.ReadString('\n')
	line = strings.TrimSpace(line)
	if err != nil && !errors.Is(err, io.EOF) {
		return def
	}
	if line == "" {
		fmt.Fprintln(q.out, def)
		return def
	}
	return line
}

// choice is ask for a yes/no or either/or answer: lowercased.
func (q *asker) choice(question, def string) string { return strings.ToLower(q.ask(question, def)) }

// foundRunners is the runner kinds whose program is on PATH, in preference
// order.
func foundRunners() []string {
	var out []string
	for _, k := range runnerKinds {
		if _, err := exec.LookPath(k); err == nil {
			out = append(out, k)
		}
	}
	return out
}

func statOK(p string) (os.FileInfo, bool) {
	st, err := os.Stat(p)
	return st, err == nil
}

// configBody is the config.toml init writes: the hub (embedded when m
// names none), this box's [hub] server when it runs one, and the runner,
// each with what it means as a comment beside it; everything else keeps
// its default.
func configBody(m meshAnswer, kind string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# bmesh config, written by bmesh init on %s. Keys not set here keep the defaults (README, Config).\n", time.Now().UTC().Format("2006-01-02"))
	b.WriteString("[mesh]\n")
	if m.nats == "" {
		fmt.Fprintf(&b, "nats = \"\"   # \"\" = the hub embedded on [mesh] listen: %s\n", whyEmbedded)
	} else {
		fmt.Fprintf(&b, "nats = %q   # %s\n", m.nats, whyHub)
		if m.tokenFile != "" {
			fmt.Fprintf(&b, "token_file = %q   # %s\n", m.tokenFile, whyToken)
		}
	}
	switch m.role {
	case "hub":
		fmt.Fprintf(&b, "[hub]\nrole = \"hub\"   # %s\nlisten = %q   # the hub's client port, on the tailnet only\n", whyHubRole, m.listen)
		if m.leafListen != "" {
			fmt.Fprintf(&b, "leaf_listen = %q   # the port other boxes' leaves dial\n", m.leafListen)
		}
	case "leaf":
		fmt.Fprintf(&b, "[hub]\nrole = \"leaf\"   # %s\nlisten = %q   # this box's clients dial the leaf here\nremote = %q   # the hub's leaf port, redialed whenever the link drops\n",
			whyLeafRole, m.listen, m.remote)
	}
	fmt.Fprintf(&b, "[runner]\nkind = %q   # %s\n", kind, whyRunner(kind))
	return b.String()
}

// meshAnswer is init's answer to how this box reaches the mesh: nats ""
// for a single box; else the hub's URL and token file, and for a hub or a
// leaf the [hub] server bmesh hub runs here.
type meshAnswer struct {
	nats, tokenFile                  string
	role, listen, leafListen, remote string // role "" for a single box or a client
}

// meshAnswer asks single or multi, and for multi this box's part (hub, leaf
// or client), filling what role, hub and tokenFile (the flags) don't say.
// A --hub alone is a client. A hub listens on its tailnet address (the
// default when this box has one) and makes <config dir>/nats.token; a leaf
// listens on 127.0.0.1:4222, where this box's bmesh dials it.
func (q *asker) meshAnswer(role, hub, tokenFile, configDir string) (meshAnswer, error) {
	if role == "" && hub == "" &&
		!strings.HasPrefix(q.choice("Single box (bmesh serve runs the hub) or multi-box (one hub for several boxes)? [single/multi]", "single"), "m") {
		return meshAnswer{}, nil
	}
	if role == "" && hub == "" {
		role = q.choice("This box's part: hub (runs the mesh's hub; an always-on box), leaf (runs a leaf of another box's hub; a box that sleeps or roams) or client (dials another box's hub)? [hub/leaf/client]", "client")
	}
	switch {
	case role == "" || strings.HasPrefix(role, "c"):
		role = "client"
	case strings.HasPrefix(role, "h"):
		role = "hub"
	case strings.HasPrefix(role, "l"):
		role = "leaf"
	default:
		return meshAnswer{}, fail(exitUsage, "role %q is not one of hub, leaf, client", role)
	}
	defToken := filepath.Join(configDir, "nats.token")
	m := meshAnswer{tokenFile: tokenFile}
	switch role {
	case "client":
		m.nats = hub
		if m.nats == "" {
			m.nats = q.ask("Hub URL", "nats://localhost:4222")
		}
		if m.tokenFile == "" {
			m.tokenFile = q.ask("Token file (empty for none)", "")
		}
		return m, nil
	case "hub":
		def := ""
		if ip := tailnetIP(); ip != "" {
			def = net.JoinHostPort(ip, "4222")
		}
		m.role, m.listen = "hub", q.ask("The hub's address (this box's tailnet ip:4222)", def)
		if err := boundAddr("listen", m.listen); err != nil {
			return m, err
		}
		host, _, _ := net.SplitHostPort(m.listen)
		m.leafListen = q.ask("Its leaf port, which other boxes' leaves dial", net.JoinHostPort(host, "7422"))
		if err := boundAddr("leaf_listen", m.leafListen); err != nil {
			return m, err
		}
		m.nats = "nats://" + m.listen
		if m.tokenFile == "" {
			m.tokenFile = q.ask("Token file (made when missing)", defToken)
		}
	case "leaf":
		m.role, m.listen = "leaf", "127.0.0.1:4222"
		m.remote = q.ask("The hub's leaf URL (nats://<hub tailnet ip>:7422)", "")
		if m.remote == "" {
			return m, fail(exitUsage, "a leaf needs the hub's leaf URL, nats://<hub tailnet ip>:7422")
		}
		m.nats = "nats://" + m.listen
		if m.tokenFile == "" {
			m.tokenFile = q.ask("Token file (the hub's token, copied here)", defToken)
		}
	}
	if m.tokenFile == "" {
		return m, fail(exitUsage, "a %s needs a token file: the hub and every box share one token", role)
	}
	return m, nil
}

// tailnetIP is this box's first address in 100.64.0.0/10, the range
// Tailscale hands out, or "".
func tailnetIP() string {
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && cgnat.Contains(n.IP) {
			return n.IP.String()
		}
	}
	return ""
}

// makeToken writes a fresh token (24 random bytes, hex) to path, owner-only,
// unless a file is there; it reports whether it wrote one.
func makeToken(path string) (bool, error) {
	if _, ok := statOK(path); ok {
		return false, nil
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(hex.EncodeToString(b) + "\n"); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}

// cwdRepo is the registry row for the git checkout the current directory is
// in: its main repo's directory name and path, and its origin shortened to
// owner/repo for GitHub.
func cwdRepo() (config.Repo, bool) {
	out, err := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return config.Repo{}, false
	}
	common := strings.TrimSpace(string(out))
	root := filepath.Dir(common)
	if filepath.Base(common) != ".git" { // a bare repo: not a checkout
		return config.Repo{}, false
	}
	r := config.Repo{Name: filepath.Base(root), Path: root}
	if u, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output(); err == nil {
		r.Remote = shortRemote(strings.TrimSpace(string(u)))
	}
	return r, true
}

// shortRemote is owner/repo for a GitHub URL, else the URL itself.
func shortRemote(u string) string {
	s := strings.TrimSuffix(u, ".git")
	for _, p := range []string{"git@github.com:", "ssh://git@github.com/", "https://github.com/", "http://github.com/"} {
		if strings.HasPrefix(s, p) {
			return strings.TrimPrefix(s, p)
		}
	}
	return u
}

// unit is a service init --service writes: the daemon (bmesh serve), or
// this box's hub or leaf (bmesh hub) when [hub] role is set.
type unit struct {
	name, label, cmd, desc string
	killProcess            bool // KillMode=process: a restart leaves the claude turns serve started
}

var (
	serveUnit = unit{name: "bmesh", label: "com.buildermesh.bmesh", cmd: "serve", desc: "buildermesh daemon (bmesh serve)", killProcess: true}
	hubUnit   = unit{name: "bmesh-hub", label: "com.buildermesh.bmesh-hub", cmd: "hub", desc: "buildermesh NATS hub or leaf (bmesh hub)"}
)

// What placeUnit did with a unit's file.
const (
	unitNone      = ""          // no file on this OS
	unitWrote     = "wrote"     // missing, or replaced under --force
	unitUnchanged = "unchanged" // already the same bytes
	unitKept      = "kept"      // differs: left as it is, the new body beside it as <file>.new
)

// writeService writes the unit that keeps u's command running: a systemd
// user unit on Linux, a launchd agent on macOS, each restarting always
// (the tailnet address a hub binds may not exist yet at boot), placed as
// placeUnit says. It returns the file, what was done with it and the
// commands that enable it; on Windows, with no file, the schtasks commands
// that set up a logon task for serve; on another OS (and for the hub on
// Windows), only the advice.
func writeService(configDir string, u unit, force bool) (string, string, []string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", nil, err
	}
	path := os.Getenv("PATH")
	if dir := filepath.Dir(exe); !strings.Contains(path, dir) {
		path = dir + string(os.PathListSeparator) + path
	}
	// The daemon must see the same config, cache (the claude runner's state)
	// and store as the shell that runs the CLI.
	env := "PATH=" + path + "\nBMESH_CONFIG=" + filepath.Join(configDir, "config.toml")
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		if v := os.Getenv(k); v != "" {
			env += "\n" + k + "=" + v
		}
	}
	file, err := serviceFile(u)
	if err != nil {
		return "", "", nil, err
	}
	switch runtime.GOOS {
	case "linux":
		kill := ""
		if u.killProcess {
			kill = "KillMode=process\n" // a restart must not take the claude turns the daemon started with it
		}
		body := "[Unit]\nDescription=" + u.desc + "\nAfter=network-online.target\n\n[Service]\n" +
			"ExecStart=" + exe + " " + u.cmd + "\nRestart=always\nRestartSec=5\n" + kill +
			"Environment=" + strings.ReplaceAll(env, "\n", "\nEnvironment=") + "\n\n" +
			"[Install]\nWantedBy=default.target\n"
		state, err := placeUnit(file, body, force)
		if err != nil {
			return "", "", nil, err
		}
		how := []string{"systemctl --user daemon-reload && systemctl --user enable --now " + u.name + "   # " + u.cmd + "; journalctl --user -u " + u.name + " -f for its log"}
		if u == serveUnit {
			how = append(how, "loginctl enable-linger $USER   # so it survives logout")
		}
		return file, state, how, nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", nil, err
		}
		log := filepath.Join(home, "Library", "Logs", u.name+".log")
		body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + u.label + `</string>
	<key>ProgramArguments</key><array><string>` + exe + `</string><string>` + u.cmd + `</string></array>
	<key>EnvironmentVariables</key><dict>
` + plistEnv(env) + `	</dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>` + log + `</string>
	<key>StandardErrorPath</key><string>` + log + `</string>
</dict>
</plist>
`
		state, err := placeUnit(file, body, force)
		if err != nil {
			return "", "", nil, err
		}
		return file, state, []string{"launchctl bootstrap gui/$(id -u) " + file + "   # " + u.cmd + "; log in ~/Library/Logs/" + u.name + ".log"}, nil
	case "windows":
		if u != serveUnit {
			break
		}
		cfgPath, err := filepath.Abs(filepath.Join(configDir, "config.toml"))
		if err != nil {
			return "", "", nil, err
		}
		lines, err := schtasksLines(exe, cfgPath, filepath.Join(config.StateDir(), "serve.log"))
		return "", unitNone, lines, err
	}
	return "", unitNone, []string{"bmesh " + u.cmd + spaces(u.cmd) + "# no service file for " + runtime.GOOS + "; keep this running"}, nil
}

// placeUnit writes body to file when none is there, or under force. A file
// with the same bytes is left unchanged; one that differs (tuned by hand,
// or written by an older bmesh) is kept, and body is written beside it as
// file.new for the operator to diff and move over.
func placeUnit(file, body string, force bool) (string, error) {
	have, err := os.ReadFile(file)
	switch {
	case err == nil && string(have) == body:
		return unitUnchanged, nil
	case err == nil && !force:
		return unitKept, os.WriteFile(file+".new", []byte(body), 0o644)
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	return unitWrote, os.WriteFile(file, []byte(body), 0o644)
}

// spaces pads "bmesh <cmd>" to the column the Next: lines' comments start at.
func spaces(cmd string) string { return strings.Repeat(" ", max(1, 13-len(cmd))) }

// schtasksMax is the longest command schtasks /TR takes.
const schtasksMax = 261

// schtasksLines are the commands that keep bmesh serve running on Windows,
// to run in cmd: a task at logon running `serve --restart --detach` (the
// config path as --config, since a task sets no environment), the task
// started now, and a rem line naming the log. Each path is quoted for
// schtasks, so one with spaces passes; cmd has no escape for the \" that
// quoting takes, so to cmd each path stands outside quotes, and one holding
// a character cmd reads there (a quote, %, &, ^, |, < or >) is refused, as
// is a command longer than /TR takes.
func schtasksLines(exe, cfgPath, logPath string) ([]string, error) {
	for _, p := range []string{exe, cfgPath} {
		if strings.ContainsAny(p, "\"%&^|<>\r\n") {
			return nil, fmt.Errorf("init --service: %q cannot be passed through cmd (it holds one of \" %% & ^ | < >); move it and run init --service again", p)
		}
	}
	tr := `"` + exe + `" serve --restart --detach --config "` + cfgPath + `"`
	if len(tr) > schtasksMax {
		return nil, fmt.Errorf("init --service: the task's command is %d characters, more than the %d schtasks takes: %s", len(tr), schtasksMax, tr)
	}
	return []string{
		`schtasks /Create /TN buildermesh /SC ONLOGON /RL LIMITED /F /TR "` + strings.ReplaceAll(tr, `"`, `\"`) + `"`,
		"schtasks /Run /TN buildermesh",
		"rem in cmd: bmesh serve then starts at every logon with no window; its log is " + logPath,
	}, nil
}

// serviceFile is where init --service writes u's unit on this OS: a
// systemd user unit on Linux, a launchd agent on macOS; "" elsewhere.
func serviceFile(u unit) (string, error) {
	switch runtime.GOOS {
	case "linux":
		ucd, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(ucd, "systemd", "user", u.name+".service"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "LaunchAgents", u.label+".plist"), nil
	}
	return "", nil
}

// plistEnv renders KEY=value lines as launchd environment entries.
func plistEnv(env string) string {
	var b strings.Builder
	for _, kv := range strings.Split(env, "\n") {
		k, v, _ := strings.Cut(kv, "=")
		fmt.Fprintf(&b, "\t\t<key>%s</key><string>%s</string>\n", k, v)
	}
	return b.String()
}

// ensureSetup runs before every command but init and help: with no setup
// flag and no config file, an interactive run says so and runs init first;
// a non-interactive one is refused.
func (a *app) ensureSetup(args []string) error {
	cfgPath := a.configPath
	if cfgPath == "" {
		cfgPath = configArg(args)
	}
	if cfgPath == "" {
		cfgPath = config.Path()
	}
	if _, ok := statOK(config.SetupFlag(cfgPath)); ok {
		return nil
	}
	if _, ok := statOK(cfgPath); ok {
		return nil // installed by hand: set up
	}
	if !interactive(a.stdin) {
		return fail(exitRefused, "not set up (no %s): run `bmesh init`", cfgPath)
	}
	fmt.Fprintf(a.stderr, "bmesh: not set up yet (no %s); running bmesh init first\n", cfgPath)
	if err := a.cmdInit([]string{"--config", cfgPath}); err != nil {
		return err
	}
	a.cfg = nil // init wrote the config; the command reads it fresh
	return nil
}

// configArg finds a --config given anywhere in args, before the command's
// own flags are parsed.
func configArg(args []string) string {
	for i, s := range args {
		if v, ok := strings.CutPrefix(s, "--config="); ok {
			return v
		}
		if (s == "--config" || s == "-config") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// interactive reports whether stdin is a terminal.
func interactive(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
