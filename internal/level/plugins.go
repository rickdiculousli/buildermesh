package level

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/plugin"
)

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
	if err != nil {
		return lines, err
	}
	l, err = refreshAntigravity(ctx, dir)
	lines = append(lines, l...)
	return lines, err
}

// LoadsPlugins reports whether sessions on this box load from the
// checkout at dir: a Claude Code marketplace reads it (or a dir inside it),
// a Codex install's marketplace source is it, or Antigravity loads from it
// (an entry in plugins.json or skills.json points into it, or a snapshot
// install matches a plugin inside it). What it can't ask it counts as read,
// so a secure box holds rather than move it: a claude or agy on PATH that
// fails to answer reads it, a malformed Antigravity config file reads it,
// and with no claude on PATH (a service's PATH can lack one sessions use) a
// checkout with .claude-plugin/marketplace.json does.
func LoadsPlugins(ctx context.Context, dir string) bool {
	if base := plugin.ClaudeCommand(); base == nil {
		if fileExists(claudeMarketplace(dir)) {
			return true
		}
	} else {
		mks, err := plugin.Marketplaces(ctx, base)
		if err != nil {
			return true
		}
		for _, m := range mks {
			if m.Reads(dir) {
				return true
			}
		}
	}
	installs, err := codexInstalls(ctx, dir)
	if err == nil && len(installs) > 0 {
		return true
	}
	return antigravityLoads(ctx, dir)
}

func claudeMarketplace(dir string) string {
	return filepath.Join(dir, ".claude-plugin", "marketplace.json")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
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

var run = plugin.Run

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
	base := plugin.ClaudeCommand()
	if base == nil {
		return nil, nil
	}
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
		if e.ReadFromFolder != "" && !plugin.Within(e.ReadFromFolder, dir) {
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
		if e.Installed && e.MarketplaceSource.Source != "" && plugin.SamePath(e.MarketplaceSource.Source, dir) {
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

type agyEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
}

type agyPluginSource struct {
	Name    string
	Version string
	Dir     string
}

// antigravityPluginSources finds native plugin manifests inside dir:
// <dir>/plugin.json and <dir>/plugins/*/plugin.json.
func antigravityPluginSources(dir string) map[string]agyPluginSource {
	candidates := []string{dir}
	if entries, err := os.ReadDir(filepath.Join(dir, "plugins")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				candidates = append(candidates, filepath.Join(dir, "plugins", e.Name()))
			}
		}
	}
	found := map[string]agyPluginSource{}
	for _, c := range candidates {
		b, err := os.ReadFile(filepath.Join(c, "plugin.json"))
		if err != nil {
			continue
		}
		var m struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if json.Unmarshal(b, &m) != nil || m.Name == "" {
			continue
		}
		found[m.Name] = agyPluginSource{Name: m.Name, Version: m.Version, Dir: c}
	}
	return found
}

// antigravityLoads reports whether Antigravity loads plugins or skills from
// dir (via plugins.json / skills.json live entries, or an enabled snapshot
// install on disk matching a plugin inside dir). Reads config directly from
// disk so a service PATH lacking `agy` still holds; malformed config fails
// closed.
func antigravityLoads(_ context.Context, dir string) bool {
	for _, name := range []string{"plugins.json", "skills.json"} {
		entries, err := plugin.AntigravityConfigEntries(name)
		if err != nil {
			return true
		}
		for _, e := range entries {
			if e.Reads(dir) {
				return true
			}
		}
	}
	sources := antigravityPluginSources(dir)
	if len(sources) == 0 {
		return false
	}
	snapshots, err := plugin.AntigravityEnabledSnapshots()
	if err != nil {
		return true
	}
	for name := range snapshots {
		if name == plugin.Name {
			continue
		}
		if _, ok := sources[name]; ok {
			return true
		}
	}
	return false
}

// refreshAntigravity re-installs any enabled snapshot-installed Antigravity
// plugin whose version lags the matching plugin inside dir. Live entries in
// plugins.json read the checkout directly and need no copy step, and
// buildermesh itself is never copied from a checkout (the binary carries it).
func refreshAntigravity(ctx context.Context, dir string) ([]string, error) {
	sources := antigravityPluginSources(dir)
	if len(sources) == 0 {
		return nil, nil
	}
	base := plugin.AntigravityCommand()
	if base == nil {
		return nil, nil
	}
	out, err := run(ctx, base, "plugin", "list", "--json")
	if err != nil {
		return nil, err
	}
	var all []agyEntry
	if err := json.Unmarshal(out, &all); err != nil {
		return nil, fmt.Errorf("agy plugin list --json: %v", err)
	}
	var lines []string
	for _, e := range all {
		if !e.Enabled || e.Name == plugin.Name {
			continue
		}
		src, ok := sources[e.Name]
		if !ok {
			continue
		}
		if src.Version != "" && src.Version == e.Version {
			continue
		}
		if _, err := run(ctx, base, "plugin", "install", src.Dir); err != nil {
			return lines, err
		}
		nv := src.Version
		if nv == "" {
			nv = e.Version
		}
		if e.Version == "" || nv == e.Version {
			lines = append(lines, fmt.Sprintf("antigravity %s %s", e.Name, nv))
		} else {
			lines = append(lines, fmt.Sprintf("antigravity %s %s -> %s", e.Name, e.Version, nv))
		}
	}
	return lines, nil
}
