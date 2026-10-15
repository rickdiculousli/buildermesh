// Package shell runs a command line through the platform's shell: sh on
// Unix, cmd on Windows. Nothing else in bmesh names a shell.
package shell

// Argv is the argument vector that runs line through the shell with args
// appended as positional arguments (`"$@"` under sh; after the line under
// cmd /C).
func Argv(line string, args ...string) []string { return argv(line, args) }
