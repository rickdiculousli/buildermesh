// Package proc puts a command in its own process group and kills the group,
// so no descendant outlives a run: Setpgid and kill(-pid) on Unix, a new
// process group and taskkill /T on Windows.
package proc

import "os/exec"

// Group makes cmd start in a process group of its own.
func Group(cmd *exec.Cmd) { group(cmd) }

// KillGroup kills cmd's process group; nil when it is already gone.
func KillGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return killGroup(cmd)
}
