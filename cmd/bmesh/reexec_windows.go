package main

// reexec exits 0 on Windows, which has no exec: the service manager starts
// the new binary.
func reexec(exe string) error { return nil }
