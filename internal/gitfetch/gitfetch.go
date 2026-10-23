// Package gitfetch is the one shape of every fetch bmesh runs: one that
// writes no FETCH_HEAD, so a fetch bmesh runs in a checkout never changes
// what another process's `git pull` there merges (089).
package gitfetch

import (
	"fmt"
	"os/exec"
	"sync"
)

var (
	once  sync.Once
	flags []string
)

// Args is `fetch` with args, behind --no-write-fetch-head when this box's
// git has it (2.29 or newer); an older git, or one whose version can't be
// read, gets a plain fetch.
func Args(args ...string) []string {
	once.Do(func() {
		if out, err := exec.Command("git", "version").Output(); err == nil && HasFlag(string(out)) {
			flags = []string{"--no-write-fetch-head"}
		}
	})
	return append(append([]string{"fetch"}, flags...), args...)
}

// HasFlag reports whether `git version` output names a git that takes
// --no-write-fetch-head: 2.29 or newer.
func HasFlag(version string) bool {
	var major, minor int
	if _, err := fmt.Sscanf(version, "git version %d.%d", &major, &minor); err != nil {
		return false
	}
	return major > 2 || major == 2 && minor >= 29
}
