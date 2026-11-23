package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// ErrNoAntigravity is InstallAntigravity's and StatusAntigravity's error on
// a box without Antigravity (neither `agy` on PATH nor its config dir).
var ErrNoAntigravity = errors.New("agy is not on PATH")

// AntigravityCommand is the agy command, split on spaces:
// $BMESH_ANTIGRAVITY_COMMAND, else agy when it is on PATH, else nil.
func AntigravityCommand() []string {
	if c := strings.Fields(os.Getenv("BMESH_ANTIGRAVITY_COMMAND")); len(c) > 0 {
		return c
	}
	if p, err := exec.LookPath("agy"); err == nil {
		return []string{p}
	}
	return nil
}

// AntigravityConfigDir is Antigravity's global config dir:
// $BMESH_ANTIGRAVITY_CONFIG_DIR when set, else ~/.gemini/config.
func AntigravityConfigDir() string {
	if d := strings.TrimSpace(os.Getenv("BMESH_ANTIGRAVITY_CONFIG_DIR")); d != "" {
		return ExpandHome(d)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".gemini", "config")
}

// AntigravityAvailable reports whether Antigravity is present on this box:
// either `agy` is on PATH (or $BMESH_ANTIGRAVITY_COMMAND) or its config dir
// already exists.
func AntigravityAvailable() bool {
	if AntigravityCommand() != nil {
		return true
	}
	dir := AntigravityConfigDir()
	if dir == "" {
		return false
	}
	st, err := os.Stat(dir)
	return err == nil && st.IsDir()
}

// ExpandHome expands a leading "~" or "~/" against the current user's home dir.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// AntigravityEntry is one path entry in Antigravity's plugins.json or
// skills.json.
type AntigravityEntry struct {
	Path        string   `json:"path"`
	IncludeOnly []string `json:"include_only,omitempty"`
	Exclude     []string `json:"exclude,omitempty"`
}

// MatchesDir reports whether e loads the direct child dir (e.Path is dir's
// parent, and dir's base name passes include_only and exclude).
func (e AntigravityEntry) MatchesDir(dir string) bool {
	if e.Path == "" {
		return false
	}
	parent, base := filepath.Dir(dir), filepath.Base(dir)
	if !SamePath(ExpandHome(e.Path), parent) {
		return false
	}
	if len(e.IncludeOnly) > 0 && !slices.Contains(e.IncludeOnly, base) {
		return false
	}
	return !slices.Contains(e.Exclude, base)
}

// Reads reports whether e loads plugins or skills from the checkout at dir:
// either e.Path is dir or inside dir, or e.Path is dir's parent and includes
// dir's base name.
func (e AntigravityEntry) Reads(dir string) bool {
	if strings.TrimSpace(e.Path) == "" {
		return false
	}
	return Within(ExpandHome(e.Path), dir) || e.MatchesDir(dir)
}

// AntigravityConfigEntries returns the `entries` array from
// <AntigravityConfigDir>/<filename> (such as "plugins.json" or "skills.json").
// Missing or empty file returns nil, nil; malformed or unreadable file returns
// an error.
func AntigravityConfigEntries(filename string) ([]AntigravityEntry, error) {
	cfgDir := AntigravityConfigDir()
	if cfgDir == "" {
		return nil, nil
	}
	_, entries, err := readAntigravityPluginsJSON(filepath.Join(cfgDir, filename))
	return entries, err
}

// AntigravityEnabledSnapshots reads enabled snapshot-installed plugin names
// directly from <AntigravityConfigDir>/plugins/*/plugin.json and config.json,
// needing no `agy` binary on PATH. Missing dirs return nil, nil; malformed
// config.json returns an error.
func AntigravityEnabledSnapshots() (map[string]bool, error) {
	cfgDir := AntigravityConfigDir()
	if cfgDir == "" {
		return nil, nil
	}
	pluginsDir := filepath.Join(cfgDir, "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	overrides := map[string]*bool{}
	if b, err := os.ReadFile(filepath.Join(cfgDir, "config.json")); err == nil && strings.TrimSpace(string(b)) != "" {
		var cfg struct {
			Plugins map[string]struct {
				Enabled *bool `json:"enabled"`
			} `json:"plugins"`
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			return nil, fmt.Errorf("config.json: %w", err)
		}
		for k, v := range cfg.Plugins {
			overrides[k] = v.Enabled
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	enabled := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(pluginsDir, e.Name(), "plugin.json"))
		if err != nil {
			continue
		}
		var m struct {
			Name     string `json:"name"`
			Disabled bool   `json:"disabled"`
		}
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		name := m.Name
		if name == "" {
			name = e.Name()
		}
		isEnabled := !m.Disabled
		if ov, ok := overrides[e.Name()]; ok && ov != nil {
			isEnabled = *ov
		}
		if isEnabled {
			enabled[name] = true
		}
	}
	return enabled, nil
}

// AntigravityState is whether Antigravity reads bmesh's plugin dir.
type AntigravityState struct {
	ConfigPath string // <configDir>/plugins.json
	Reads      bool   // true when plugins.json includes dir and no shadow copy exists
	Shadowed   string // non-empty when <configDir>/plugins/buildermesh exists
}

// StatusAntigravity reads whether Antigravity loads the buildermesh plugin
// from dir.
func StatusAntigravity(dir string) (AntigravityState, error) {
	if !AntigravityAvailable() {
		return AntigravityState{}, ErrNoAntigravity
	}
	cfgDir := AntigravityConfigDir()
	if cfgDir == "" {
		return AntigravityState{}, errors.New("cannot locate Antigravity config dir")
	}
	s := AntigravityState{ConfigPath: filepath.Join(cfgDir, "plugins.json")}
	shadow := filepath.Join(cfgDir, "plugins", Name)
	if _, err := os.Lstat(shadow); err == nil {
		s.Shadowed = shadow
	}
	_, entries, err := readAntigravityPluginsJSON(s.ConfigPath)
	if err != nil {
		return s, err
	}
	for _, e := range entries {
		if e.MatchesDir(dir) {
			s.Reads = s.Shadowed == ""
			break
		}
	}
	return s, nil
}

// InstallAntigravity points Antigravity at dir via <configDir>/plugins.json
// (removing any stale snapshot copy under <configDir>/plugins/buildermesh so
// names never collide), preserving every other field and entry. Returns one
// line per change, none when already configured.
func InstallAntigravity(dir string) ([]string, error) {
	if !AntigravityAvailable() {
		return nil, ErrNoAntigravity
	}
	cfgDir := AntigravityConfigDir()
	if cfgDir == "" {
		return nil, errors.New("cannot locate Antigravity config dir")
	}
	cfgPath := filepath.Join(cfgDir, "plugins.json")
	var lines []string
	shadow := filepath.Join(cfgDir, "plugins", Name)
	if _, err := os.Lstat(shadow); err == nil {
		if err := os.RemoveAll(shadow); err != nil {
			return lines, fmt.Errorf("remove Antigravity plugin copy %s: %w", shadow, err)
		}
		lines = append(lines, fmt.Sprintf("removed Antigravity plugin copy (%s)", shadow))
	}
	doc, entries, err := readAntigravityPluginsJSON(cfgPath)
	if err != nil {
		return lines, err
	}
	parent, base := filepath.Dir(dir), filepath.Base(dir)
	found := false
	changed := false
	kept := make([]AntigravityEntry, 0, len(entries)+1)
	for _, e := range entries {
		if e.MatchesDir(dir) {
			found = true
			kept = append(kept, e)
			continue
		}
		// Drop a stale single-plugin entry pointing to a different bmesh plugin
		// dir (same plugin name in plugin.json) so two buildermesh entries don't
		// collide. Never touch multi-plugin dirs like a tools repo.
		if isStaleBuildermeshEntry(e) {
			lines = append(lines, fmt.Sprintf("removed Antigravity plugin entry (%s)", e.Path))
			changed = true
			continue
		}
		kept = append(kept, e)
	}
	if !found {
		kept = append(kept, AntigravityEntry{Path: parent, IncludeOnly: []string{base}})
		lines = append(lines, fmt.Sprintf("added Antigravity plugin entry (%s) to %s", dir, cfgPath))
		changed = true
	}
	if changed {
		if err := writeAntigravityPluginsJSON(cfgPath, doc, kept); err != nil {
			return lines, err
		}
	}
	s, err := StatusAntigravity(dir)
	if err != nil {
		return lines, err
	}
	if !s.Reads {
		return lines, fmt.Errorf("%s does not include %s", cfgPath, dir)
	}
	return lines, nil
}

// isStaleBuildermeshEntry reports whether e points specifically at a single
// child dir that carries a buildermesh plugin.json.
func isStaleBuildermeshEntry(e AntigravityEntry) bool {
	if len(e.IncludeOnly) != 1 || e.Path == "" {
		return false
	}
	target := filepath.Join(ExpandHome(e.Path), e.IncludeOnly[0])
	return pluginManifestName(target) == Name
}

// pluginManifestName reads the "name" from <dir>/plugin.json, or "" when absent.
func pluginManifestName(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return ""
	}
	var m struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	return m.Name
}

func readAntigravityPluginsJSON(path string) (map[string]json.RawMessage, []AntigravityEntry, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(string(b)) == "" {
		return map[string]json.RawMessage{}, nil, nil
	}
	doc := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	var entries []AntigravityEntry
	if raw, ok := doc["entries"]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, nil, fmt.Errorf("%s entries: %w", path, err)
		}
	}
	return doc, entries, nil
}

func writeAntigravityPluginsJSON(path string, doc map[string]json.RawMessage, entries []AntigravityEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	rawEntries, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	doc["entries"] = rawEntries
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".plugins.json-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
