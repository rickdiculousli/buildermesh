package main

// reexec exits 0 on Windows, which has no exec: bmesh serve --restart, the
// supervisor init --service sets up there, starts the new binary.
func reexec(exe string) error { return nil }
