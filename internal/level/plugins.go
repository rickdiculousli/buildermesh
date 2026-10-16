package level

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const cmdTimeout = 2 * time.Minute

// Refresh refreshes what this box loads from the checkout at dir and
// returns one line per install it updated or re-copied.
func Refresh(ctx context.Context, dir string) ([]string, error) {
	var lines []string
	l, err := refreshClaude(ctx, dir)
	lines = append(lines, l...)
	if err != nil {
		return lines, err
	}
	l, err = refreshCodex(ctx, dir)
	lines = append(lines, l...)
	return lines, err
}

// LoadsPlugins reports whether sessions on this box load from the
// checkout at dir: it carries .claude-plugin/marketplace.json or
// .agents/plugins/marketplace.json, or a Codex install's marketplace
// source is dir.
func LoadsPlugins(ctx context.Context, dir string) bool {
	if fileExists(claudeMarketplace(dir)) || fileExists(filepath.Join(dir, ".agents", "plugins", "marketplace.json")) {
		return true
	}
	installs, err := codexInstalls(ctx, dir)
	return err == nil && len(installs) > 0
}

func claudeMarketplace(dir string) string {
	return filepath.Join(dir, ".claude-plugin", "marketplace.json")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// resolve returns p with symlinks resolved, or cleaned when it can't be.
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	p, dir = resolve(p), resolve(dir)
	if p == dir {
		return true
	}
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func claudeCommand() []string {
	if c := strings.Fields(os.Getenv("BMESH_CLAUDE_COMMAND")); len(c) > 0 {
		return c
	}
	return []string{"claude"}
}

// codexCommand is nil when Codex isn't on this box.
func codexCommand() []string {
	if c := strings.Fields(os.Getenv("BMESH_CODEX_COMMAND")); len(c) > 0 {
		return c
	}
	if p, err := exec.LookPath("codex"); err == nil {
		return []string{p}
	}
	return nil
}

// run runs base plus args with a timeout and returns stdout; a failure
// names the command and its trimmed stderr.
func run(ctx context.Context, base []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, base[0], append(append([]string{}, base[1:]...), args...)...)
	cmd.WaitDelay = 5 * time.Second
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if len(msg) > 200 {
			msg = msg[:200]
		}
		name := strings.Join(append(append([]string{}, base...), args...), " ")
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s: %s", name, msg)
	}
	return out.Bytes(), nil
}

type claudeEntry struct {
	ID             string `json:"id"`
	Version        string `json:"version"`
	Scope          string `json:"scope"`
	ReadFromFolder string `json:"readFromFolder"`
	FolderVersion  string `json:"folderVersion"`
}

func refreshClaude(ctx context.Context, dir string) ([]string, error) {
	raw, err := os.ReadFile(claudeMarketplace(dir))
	if err != nil {
		return nil, nil
	}
	var mk struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &mk) != nil || mk.Name == "" {
		return nil, nil
	}
	base := claudeCommand()
	out, err := run(ctx, base, "plugin", "list", "--json")
	if err != nil {
		return nil, err
	}
	var all []claudeEntry
	if err := json.Unmarshal(out, &all); err != nil {
		return nil, fmt.Errorf("claude plugin list --json: %v", err)
	}
	var match []claudeEntry
	for _, e := range all {
		if !strings.HasSuffix(e.ID, "@"+mk.Name) || e.Scope != "user" {
			continue
		}
		if e.ReadFromFolder != "" && !within(e.ReadFromFolder, dir) {
			continue
		}
		match = append(match, e)
	}
	if len(match) == 0 {
		return nil, nil
	}
	if _, err := run(ctx, base, "plugin", "marketplace", "update", mk.Name); err != nil {
		return nil, err
	}
	var lines []string
	for _, e := range match {
		fv := e.FolderVersion
		if fv == "" || fv == e.Version {
			continue
		}
		if _, err := run(ctx, base, "plugin", "update", e.ID, "--scope", "user"); err != nil {
			return lines, err
		}
		lines = append(lines, fmt.Sprintf("claude %s %s -> %s", e.ID, e.Version, fv))
	}
	return lines, nil
}

type codexEntry struct {
	PluginID          string `json:"pluginId"`
	Version           string `json:"version"`
	Installed         bool   `json:"installed"`
	MarketplaceSource struct {
		Source string `json:"source"`
	} `json:"marketplaceSource"`
}

// codexInstalls lists the installed Codex plugins whose marketplace is dir.
func codexInstalls(ctx context.Context, dir string) ([]codexEntry, error) {
	base := codexCommand()
	if base == nil {
		return nil, nil
	}
	out, err := run(ctx, base, "plugin", "list", "--json")
	if err != nil {
		return nil, err
	}
	var res struct {
		Installed []codexEntry `json:"installed"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("codex plugin list --json: %v", err)
	}
	var match []codexEntry
	for _, e := range res.Installed {
		if e.Installed && e.MarketplaceSource.Source != "" && resolve(e.MarketplaceSource.Source) == resolve(dir) {
			match = append(match, e)
		}
	}
	return match, nil
}

func refreshCodex(ctx context.Context, dir string) ([]string, error) {
	installs, err := codexInstalls(ctx, dir)
	if err != nil {
		return nil, err
	}
	base := codexCommand()
	var lines []string
	for _, e := range installs {
		out, err := run(ctx, base, "plugin", "add", e.PluginID, "--json")
		if err != nil {
			return lines, err
		}
		var add struct {
			Version string `json:"version"`
		}
		_ = json.Unmarshal(out, &add)
		nv := add.Version
		if nv == "" {
			nv = e.Version
		}
		if nv == e.Version {
			lines = append(lines, fmt.Sprintf("codex %s %s", e.PluginID, nv))
		} else {
			lines = append(lines, fmt.Sprintf("codex %s %s -> %s", e.PluginID, e.Version, nv))
		}
	}
	return lines, nil
}
