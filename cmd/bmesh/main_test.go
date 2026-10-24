package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

// TestMain keeps every test off the box's real tools and cache. Briefs,
// prompts and runner state go under a temp BMESH_CACHE_DIR. On macOS the key
// store defaults to the Keychain, whose security command opens a dialog:
// the default here is the file store, as on Linux, which the hosts tests
// are written for, and a test that names keychain gets
// testdata/fake-security.sh. A level's plugin refresh runs claude, and
// codex whenever it is on PATH: both are the plugin fakes, which list no
// plugins. The state dir is a temp one, and the module's main is read from
// a path that does not exist, so no test reads or removes the box's notice
// or reaches the network. No test runs in a Claude Code session. A test
// that sets its own fake overrides these.
func TestMain(m *testing.M) {
	if os.Getenv("BMESH_TEST_AS_BMESH") == "1" {
		os.Exit(asBmesh())
	}
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
		// A test run inside a Claude Code session would read its context
		// and end commands with the session's context line (094).
		os.Unsetenv("CLAUDE_CODE_SESSION_ID")
		for k, v := range map[string]string{
			"BMESH_KEY_STORE":        "file",
			"BMESH_CACHE_DIR":        filepath.Join(dir, "cache"),
			"XDG_STATE_HOME":         filepath.Join(dir, "state"),
			"BMESH_UPSTREAM_URL":     filepath.Join(dir, "no-upstream"),
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

// asBmesh runs this test binary as bmesh itself, for testdata/fake-go.sh's
// built binary to exec with $FAKE_GO_BMESH: built from $BMESH_TEST_REV of
// module example.com/bm, and with $BMESH_TEST_REJECT set rejecting every
// config with it, as a newer binary may reject what an older one took.
func asBmesh() int {
	rev := os.Getenv("BMESH_TEST_REV")
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Path: "example.com/bm"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: rev}, {Key: "vcs.time", Value: "2026-10-17T00:00:00Z"}}}, true
	}
	if msg := os.Getenv("BMESH_TEST_REJECT"); msg != "" {
		configRule = func(*config.Config) error { return errors.New(msg) }
	}
	return run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
