// Package proc puts a command in its own process group and kills the group,
// so no descendant outlives a run: a new session (Setsid, so no controlling
// terminal) and kill(-pid) on Unix, a new process group and taskkill /T on
// Windows.
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

// QuitGroup asks cmd's process group to quit (SIGQUIT on Unix, so Go and
// the JVM print their stacks; on Windows the group is killed); nil when it
// is gone or never started.
func QuitGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return quitGroup(cmd)
}

// Run starts cmd, looks at its group with w (nil: not at all) while it
// runs, and waits for it.
func Run(cmd *exec.Cmd, w *Watch) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	w.Start(cmd.Process.Pid)
	defer w.Stop()
	return cmd.Wait()
}
