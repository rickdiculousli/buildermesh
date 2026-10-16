package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

const initUsage = "usage: bmesh init [--service] [--force] [--hub URL] [--token-file F] [--runner claude|t3|tmux] [--no-repo] | bmesh init --hosts [--admin]"

// runnerKinds are the runners init looks for on PATH, in order of
// preference: claude needs nothing else.
var runnerKinds = []string{"claude", "t3", "tmux"}

// cmdInit sets a box up: config.toml, repos.toml with the current repo, the
// service unit with --service, and the setup flag last. Answers come from
// flags, else from questions on stdin (an empty line, or no stdin, takes
// the default), so a script can drive it.
func (a *app) cmdInit(args []string) error {
	fs := a.flags("init")
	service := fs.Bool("service", false, "write the systemd user unit (Linux) or launchd plist (macOS) for bmesh serve, or print the schtasks commands (Windows)")
	force := fs.Bool("force", false, "rewrite an existing config.toml and this repo's registry row")
	hub := fs.String("hub", "", "multi-box: the NATS hub URL (nats://host:4222)")
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
		multi := *hub != ""
		if !multi {
			multi = strings.HasPrefix(ask.choice("Single box (bmesh serve runs the hub) or multi-box (a NATS hub elsewhere)? [single/multi]", "single"), "m")
		}
		if multi && *hub == "" {
			*hub = ask.ask("Hub URL", "nats://localhost:4222")
		}
		if multi && *tokenFile == "" {
			*tokenFile = ask.ask("Token file (empty for none)", "")
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
		if err := os.WriteFile(cfgPath, []byte(configBody(*hub, *tokenFile, kind)), 0o600); err != nil {
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

	var enable []string
	if *service {
		path, how, err := writeService(dir)
		if err != nil {
			return err
		}
		if path != "" {
			wrote = append(wrote, path)
		}
		enable = how
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
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
	} else {
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

// configBody is the config.toml init writes: the hub (embedded when hub is
// empty) and the runner, each with what it means as a comment beside it;
// everything else keeps its default.
func configBody(hub, tokenFile, kind string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# bmesh config, written by bmesh init on %s. Keys not set here keep the defaults (README, Config).\n", time.Now().UTC().Format("2006-01-02"))
	b.WriteString("[mesh]\n")
	if hub == "" {
		fmt.Fprintf(&b, "nats = \"\"   # \"\" = the hub embedded on [mesh] listen: %s\n", whyEmbedded)
	} else {
		fmt.Fprintf(&b, "nats = %q   # %s\n", hub, whyHub)
		if tokenFile != "" {
			fmt.Fprintf(&b, "token_file = %q   # %s\n", tokenFile, whyToken)
		}
	}
	fmt.Fprintf(&b, "[runner]\nkind = %q   # %s\n", kind, whyRunner(kind))
	return b.String()
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

// writeService writes the unit that keeps bmesh serve running: a systemd user
// unit on Linux, a launchd agent on macOS. It returns the file and the
// commands that enable it; on Windows, with no file, the schtasks commands
// that set up a logon task; on another OS, only the advice.
func writeService(configDir string) (string, []string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
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
	file, err := serviceFile()
	if err != nil {
		return "", nil, err
	}
	switch runtime.GOOS {
	case "linux":
		unit := file
		body := "[Unit]\nDescription=buildermesh daemon (bmesh serve)\nAfter=network-online.target\n\n[Service]\n" +
			"ExecStart=" + exe + " serve\nRestart=always\nRestartSec=5\n" +
			"KillMode=process\n" + // a restart must not take the claude turns the daemon started with it
			"Environment=" + strings.ReplaceAll(env, "\n", "\nEnvironment=") + "\n\n" +
			"[Install]\nWantedBy=default.target\n"
		if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
			return "", nil, err
		}
		if err := os.WriteFile(unit, []byte(body), 0o644); err != nil {
			return "", nil, err
		}
		return unit, []string{"systemctl --user daemon-reload && systemctl --user enable --now bmesh   # the daemon; journalctl --user -u bmesh -f for its log",
			"loginctl enable-linger $USER   # so it survives logout"}, nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil, err
		}
		plist := file
		logDir := filepath.Join(home, "Library", "Logs")
		body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>com.buildermesh.bmesh</string>
	<key>ProgramArguments</key><array><string>` + exe + `</string><string>serve</string></array>
	<key>EnvironmentVariables</key><dict>
` + plistEnv(env) + `	</dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>` + filepath.Join(logDir, "bmesh.log") + `</string>
	<key>StandardErrorPath</key><string>` + filepath.Join(logDir, "bmesh.log") + `</string>
</dict>
</plist>
`
		if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
			return "", nil, err
		}
		if err := os.WriteFile(plist, []byte(body), 0o644); err != nil {
			return "", nil, err
		}
		return plist, []string{"launchctl bootstrap gui/$(id -u) " + plist + "   # the daemon; log in ~/Library/Logs/bmesh.log"}, nil
	case "windows":
		cfgPath, err := filepath.Abs(filepath.Join(configDir, "config.toml"))
		if err != nil {
			return "", nil, err
		}
		lines, err := schtasksLines(exe, cfgPath, filepath.Join(config.StateDir(), "serve.log"))
		return "", lines, err
	}
	return "", []string{"bmesh serve        # no service file for " + runtime.GOOS + "; keep this running"}, nil
}

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

// serviceFile is where init --service writes the unit on this OS: a
// systemd user unit on Linux, a launchd agent on macOS; "" elsewhere.
func serviceFile() (string, error) {
	switch runtime.GOOS {
	case "linux":
		ucd, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(ucd, "systemd", "user", "bmesh.service"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "LaunchAgents", "com.buildermesh.bmesh.plist"), nil
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
