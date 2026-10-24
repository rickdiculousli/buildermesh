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
	return lines, err
}

// LoadsPlugins reports whether sessions on this box load from the
// checkout at dir: a Claude Code marketplace reads it (or a dir inside it),
// or a Codex install's marketplace source is it. A claude on PATH that
// fails to answer counts as reading it, so a secure box holds what it
// can't tell rather than move it.
func LoadsPlugins(ctx context.Context, dir string) bool {
	if base := plugin.ClaudeCommand(); base != nil {
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
	return err == nil && len(installs) > 0
}

func claudeMarketplace(dir string) string {
	return filepath.Join(dir, ".claude-plugin", "marketplace.json")
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
