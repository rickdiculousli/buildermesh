package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps every test off the box's real tools. On macOS the key
// store defaults to the Keychain, whose security command opens a dialog:
// the default here is the file store, as on Linux, which the hosts tests
// are written for, and a test that names keychain gets
// testdata/fake-security.sh. A level's plugin refresh runs claude, and
// codex whenever it is on PATH: both are the plugin fakes, which list no
// plugins. A test that sets its own fake overrides these.
func TestMain(m *testing.M) {
	os.Exit(func() int {
		testdata, err := filepath.Abs(filepath.Join("..", "..", "testdata"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		dir, err := os.MkdirTemp("", "bmesh-fake-security-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(dir)
		for k, v := range map[string]string{
			"BMESH_KEY_STORE":        "file",
			"BMESH_SECURITY_COMMAND": "sh " + filepath.Join(testdata, "fake-security.sh"),
			"BMESH_CLAUDE_COMMAND":   "sh " + filepath.Join(testdata, "fake-claude-plugin.sh"),
			"BMESH_CODEX_COMMAND":    "sh " + filepath.Join(testdata, "fake-codex.sh"),
			"FAKE_SECURITY_DIR":      filepath.Join(dir, "items"),
			"FAKE_SECURITY_LOG":      filepath.Join(dir, "log"),
			"FAKE_SECURITY_ACL":      "",
			"FAKE_SECURITY_DENY":     "",
		} {
			os.Setenv(k, v)
		}
		return m.Run()
	}())
}
