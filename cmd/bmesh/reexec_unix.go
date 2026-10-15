//go:build !windows

package main

import (
	"os"
	"syscall"
)

// reexec replaces this process with exe, same arguments and environment,
// so the service manager sees the same process carry on.
func reexec(exe string) error {
	return syscall.Exec(exe, os.Args, os.Environ())
}
