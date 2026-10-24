package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/proc"
)

// models are the --model shorthands; anything else is passed as given.
var models = map[string]string{"opus": "claude-opus-5-5", "sonnet": "claude-sonnet-5-5", "haiku": "claude-haiku-4-5-20251001", "fable": "claude-fable-5-1"}

func modelID(m string) string {
	if id, ok := models[m]; ok {
		return id
	}
	return m
}

// runner runs a cell's turns as headless claude sessions.
type runner struct {
	claude   string        // the claude binary
	model    string        // a full model id
	timeout  time.Duration // one turn's limit; its whole process group is killed past it
	projects string        // claude's projects dir, where a session's transcript is written
	env      []string      // this process's environment, scrubbed per cell
	host     Host
}

// turnArgs is the claude command line for one turn: the operator's words,
// the session before (none on the first turn), and the isolation every
// session runs under: no user settings, plugins or hooks, no MCP server, only
// the arm's plugin. The output is the JSON stream, not one JSON object: only
// a stream's results state their own usage on a resumed session (a json
// result's cost and model usage run on from the session's saved totals), and
// its init event names the plugins, skills and MCP servers the session has.
func (r *runner) turnArgs(words, resume, plugin string) []string {
	args := []string{"-p", words}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	return append(args, "--model", r.model, "--output-format", "stream-json", "--verbose", "--permission-mode", "bypassPermissions",
		"--setting-sources", "project,local", "--strict-mcp-config", "--plugin-dir", plugin)
}

// keptEnv are the variables of the claude family a session keeps: where its
// config is and how it signs in. Every other CLAUDE* variable couples a
// child to the session that started this harness.
var keptEnv = map[string]bool{"CLAUDE_CONFIG_DIR": true, "CLAUDE_CODE_OAUTH_TOKEN": true,
	"CLAUDE_CODE_USE_BEDROCK": true, "CLAUDE_CODE_USE_VERTEX": true, "CLAUDE_CODE_USE_FOUNDRY": true}

// Host is what of this box a session may use: the home it must not see, the
// toolchain directories that live under it, and Go's caches, shared so a
// cell's builds start warm.
type Host struct {
	Home       string
	Tools      []string // directories put on PATH right after the cell's bin
	GoCache    string
	GoModCache string
}

// findHost reads this box's home and its go and git.
func findHost() Host {
	h := Host{}
	h.Home, _ = os.UserHomeDir()
	if out, err := exec.Command("go", "env", "GOROOT", "GOCACHE", "GOMODCACHE").Output(); err == nil {
		if f := strings.Split(strings.TrimSpace(string(out)), "\n"); len(f) == 3 {
			h.Tools = append(h.Tools, filepath.Join(f[0], "bin"))
			h.GoCache, h.GoModCache = f[1], f[2]
		}
	}
	if p, err := exec.LookPath("git"); err == nil {
		if p, err = filepath.EvalSymlinks(p); err == nil {
			h.Tools = append(h.Tools, filepath.Dir(p))
		}
	}
	return h
}

func under(p, dir string) bool {
	return dir != "" && (p == dir || strings.HasPrefix(p, dir+string(filepath.Separator)))
}

// sessionEnv is the environment a cell's session runs in: base (this
// process's) less what would tie it to this session, a T3 thread, a mesh,
// another box or this box's git config. HOME is the cell's own, which has
// claude's config and no shell profile: the shell claude runs commands in
// sources the profile, and this box's puts ~/.local/bin, the real bmesh's
// home, first on PATH. So PATH is the cell's bin (the fake and the stubs),
// the toolchain, and base's directories outside this box's home; with the
// cell's TMPDIR, git config and inert bmesh config, no auto-memory and no
// module downloads.
func sessionEnv(base []string, dir string, h Host) []string {
	path := append([]string{filepath.Join(dir, "bin")}, h.Tools...)
	var env []string
	for _, kv := range base {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case k == "PATH":
			for _, p := range filepath.SplitList(v) {
				if p != "" && !under(p, h.Home) && !slices.Contains(path, p) {
					path = append(path, p)
				}
			}
			continue
		case keptEnv[k]:
		case strings.HasPrefix(k, "CLAUDE"), strings.HasPrefix(k, "T3"), strings.HasPrefix(k, "BMESH_"),
			strings.HasPrefix(k, "SKILLTEST_"), strings.HasPrefix(k, "GIT_"), strings.HasPrefix(k, "GO"),
			k == "XDG_CONFIG_HOME", k == "XDG_DATA_HOME", k == "XDG_CACHE_HOME", k == "XDG_STATE_HOME",
			k == "AI_AGENT", k == "HOME", k == "SSH_AUTH_SOCK", k == "SSH_AGENT_PID", k == "TMPDIR", k == "BASH_ENV", k == "ENV":
			continue
		}
		env = append(env, kv)
	}
	env = append(env, gitEnv(dir)...)
	env = append(env,
		"HOME="+filepath.Join(dir, "home"),
		"PATH="+strings.Join(path, string(os.PathListSeparator)),
		"CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
		"DISABLE_AUTOUPDATER=1",
		"TMPDIR="+filepath.Join(dir, "tmp"),
		"BMESH_CONFIG="+filepath.Join(dir, "bmesh-config", "config.toml"),
		"SKILLTEST_LOG="+filepath.Join(dir, logFile),
		"SKILLTEST_CELL="+dir,
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
		"GOWORK=off",
		"GOPATH="+filepath.Join(dir, "home", "go"),
	)
	if h.GoCache != "" {
		env = append(env, "GOCACHE="+h.GoCache, "GOMODCACHE="+h.GoModCache)
	}
	return env
}

// claudeLinks are what of this box's home a cell's home links to: claude's
// own config, sign-in and session store, nothing else.
var claudeLinks = []string{".claude", ".claude.json"}

// makeHome makes the cell's home, linking claude's config from this box's.
func makeHome(dir string, h Host) error {
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	for _, name := range claudeLinks {
		if h.Home == "" {
			break
		}
		if _, err := os.Lstat(filepath.Join(h.Home, name)); err != nil {
			continue
		}
		if err := os.Symlink(filepath.Join(h.Home, name), filepath.Join(home, name)); err != nil {
			return err
		}
	}
	return nil
}

// unlinkHome removes the links makeHome made, so nothing that archives or
// removes the cell's home reaches this box's; and whatever claude left in
// their place (a config it renamed over its link is a copy of this box's),
// which is no work of the session's.
func unlinkHome(dir string) {
	for _, name := range claudeLinks {
		os.RemoveAll(filepath.Join(dir, "home", name))
	}
}

// turn runs one operator turn of the cell in dir and records it.
func (r *runner) turn(ctx context.Context, dir string, c *Cell, words string, steer bool) error {
	n := len(c.Turns) + 1
	t := Turn{Words: words, Steer: steer, File: fmt.Sprintf("turns/%02d.json", n)}
	stdout, err := os.Create(filepath.Join(dir, t.File))
	if err != nil {
		return err
	}
	stderr, err := os.Create(filepath.Join(dir, fmt.Sprintf("turns/%02d.stderr", n)))
	if err != nil {
		stdout.Close()
		return err
	}
	cmd := exec.Command(r.claude, r.turnArgs(words, c.lastSession(), c.Plugin)...)
	cmd.Dir = filepath.Join(dir, sandboxDir)
	cmd.Env = sessionEnv(r.env, dir, r.host)
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = stdout, stderr // files, never pipes: a child holding a pipe open would outlive the timeout
	proc.Group(cmd)
	start := time.Now()
	runErr := cmd.Start()
	if runErr == nil {
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		timer := time.NewTimer(r.timeout)
		select {
		case runErr = <-done:
		case <-timer.C:
			_ = proc.KillGroup(cmd)
			<-done
			runErr = fmt.Errorf("killed after %s", r.timeout)
		case <-ctx.Done():
			_ = proc.KillGroup(cmd)
			<-done
			runErr = fmt.Errorf("stopped: %w", ctx.Err())
		}
		timer.Stop()
	}
	t.WallMS = time.Since(start).Milliseconds()
	stdout.Close()
	if runErr != nil {
		fmt.Fprintf(stderr, "\n[skilltest: %v]\n", runErr)
	}
	stderr.Close()
	res, perr := readResult(filepath.Join(dir, t.File))
	t.Session = res.SessionID
	switch {
	case runErr != nil:
		t.Error = runErr.Error()
	case perr != nil:
		t.Error = perr.Error()
	case res.IsError:
		t.Error = strings.TrimSpace("claude: " + res.Subtype + " " + res.Result)
	}
	t.After = snapshot(dir, c.Base)
	c.Turns = append(c.Turns, t)
	if t.Error != "" {
		return errors.New(t.Error)
	}
	return nil
}

// Result is the part of claude's result event a cell is scored on.
type Result struct {
	Type       string  `json:"type"`
	Subtype    string  `json:"subtype"`
	IsError    bool    `json:"is_error"`
	Result     string  `json:"result"`
	SessionID  string  `json:"session_id"`
	NumTurns   int     `json:"num_turns"`
	DurationMS int64   `json:"duration_ms"`
	CostUSD    float64 `json:"total_cost_usd"`
	Usage      struct {
		Input       int64 `json:"input_tokens"`
		Output      int64 `json:"output_tokens"`
		CacheRead   int64 `json:"cache_read_input_tokens"`
		CacheCreate int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	ModelUsage map[string]Spend `json:"modelUsage"`
	SinceLast  map[string]Spend `json:"model_usage_since_last_result"`
}

// Spend is tokens and cost: one model's in a result, or a turn's.
type Spend struct {
	Input       int64   `json:"inputTokens"`
	Output      int64   `json:"outputTokens"`
	CacheRead   int64   `json:"cacheReadInputTokens"`
	CacheCreate int64   `json:"cacheCreationInputTokens"`
	CostUSD     float64 `json:"costUSD"`
}

func (s Spend) plus(o Spend) Spend {
	return Spend{s.Input + o.Input, s.Output + o.Output, s.CacheRead + o.CacheRead, s.CacheCreate + o.CacheCreate, s.CostUSD + o.CostUSD}
}

func (s Spend) Tokens() int64 { return s.Input + s.Output + s.CacheRead + s.CacheCreate }

func sumSpend(m map[string]Spend) Spend {
	var t Spend
	for _, s := range m {
		t = t.plus(s)
	}
	return t
}

// totals are the session's running totals as a result states them: every
// model's (subagents included) when it breaks them down, else its own.
func (r Result) totals() Spend {
	t := sumSpend(r.ModelUsage)
	if len(r.ModelUsage) == 0 {
		t = Spend{r.Usage.Input, r.Usage.Output, r.Usage.CacheRead, r.Usage.CacheCreate, 0}
	}
	t.CostUSD = r.CostUSD
	return t
}

// turnSpend is what one turn cost. A result in a stream states it
// (model_usage_since_last_result); otherwise its totals run on from the
// session's saved ones on a resume, so prev's, the session's result before
// it, are taken off when every total is at least prev's.
func turnSpend(r Result, prev *Result) Spend {
	if r.SinceLast != nil {
		return sumSpend(r.SinceLast)
	}
	t := r.totals()
	if prev == nil {
		return t
	}
	p := prev.totals()
	if t.Input >= p.Input && t.Output >= p.Output && t.CacheRead >= p.CacheRead && t.CacheCreate >= p.CacheCreate && t.CostUSD >= p.CostUSD {
		return Spend{t.Input - p.Input, t.Output - p.Output, t.CacheRead - p.CacheRead, t.CacheCreate - p.CacheCreate, t.CostUSD - p.CostUSD}
	}
	return t
}

// Init is the stream's first event: what the session has to work with.
type Init struct {
	Type       string            `json:"type"`
	Subtype    string            `json:"subtype"`
	Plugins    []json.RawMessage `json:"plugins"`
	Skills     []json.RawMessage `json:"skills"`
	MCPServers []json.RawMessage `json:"mcp_servers"`
}

// names reads a list of names, or of objects with a name.
func names(raws []json.RawMessage) []string {
	out := []string{}
	for _, raw := range raws {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			out = append(out, s)
			continue
		}
		var o struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &o) == nil && o.Name != "" {
			out = append(out, o.Name)
		}
	}
	return out
}

// readTurn reads a turn's output: the last result event, and the init
// event when the output is a stream. Lines that are no JSON are skipped;
// an array of events (an older json output) is read through.
func readTurn(file string) (Result, *Init, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return Result{}, nil, err
	}
	var last Result
	var init *Init
	found := false
	take := func(raw json.RawMessage) {
		var head struct{ Type, Subtype string }
		if json.Unmarshal(raw, &head) != nil {
			return
		}
		switch {
		case head.Type == "result":
			var r Result
			if json.Unmarshal(raw, &r) == nil {
				last, found = r, true
			}
		case head.Type == "system" && head.Subtype == "init" && init == nil:
			var i Init
			if json.Unmarshal(raw, &i) == nil {
				init = &i
			}
		}
	}
	for _, line := range bytes.Split(b, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) > 0 && line[0] == '[' {
			var arr []json.RawMessage
			if json.Unmarshal(line, &arr) == nil {
				for _, e := range arr {
					take(e)
				}
			}
			continue
		}
		if len(line) > 0 {
			take(line)
		}
	}
	if !found {
		return last, init, fmt.Errorf("%s: no result in claude's output", filepath.Base(file))
	}
	return last, init, nil
}

func readResult(file string) (Result, error) {
	r, _, err := readTurn(file)
	return r, err
}

// claudeProjects is where claude writes its sessions' transcripts.
func claudeProjects() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "projects")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// projectName is claude's directory name for a session started in dir.
func projectName(dir string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, dir)
}

// keepTranscripts moves the transcripts of the cell's sessions from
// claude's projects dir into the cell's claude-project/: the whole project
// directory when it is the sandbox's alone, else each session's files.
func (r *runner) keepTranscripts(dir string, c *Cell) {
	if r.projects == "" {
		return
	}
	want := projectName(filepath.Join(dir, sandboxDir))
	seen := map[string]bool{}
	for _, t := range c.Turns {
		if t.Session == "" || seen[t.Session] {
			continue
		}
		seen[t.Session] = true
		matches, _ := filepath.Glob(filepath.Join(r.projects, "*", t.Session+".jsonl"))
		for _, m := range matches {
			proj := filepath.Dir(m)
			if filepath.Base(proj) == want {
				_ = move(proj, filepath.Join(dir, "claude-project"))
				return
			}
			_ = move(m, filepath.Join(dir, "claude-project", filepath.Base(m)))
			if sub := strings.TrimSuffix(m, ".jsonl"); isDir(sub) {
				_ = move(sub, filepath.Join(dir, "claude-project", filepath.Base(sub)))
			}
		}
	}
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func claudeVersion(claude string) string {
	out, err := exec.Command(claude, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
