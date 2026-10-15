//go:build !windows

package selfupdate

import (
	"os"
	"syscall"
)

// Lock takes the advisory lock <dir>.lock, waiting for another holder, so
// one process at a time moves a main checkout and builds from it (the
// daemon and a `bmesh update` share the daemon's checkout).
func Lock(dir string) (unlock func(), err error) {
	f, err := os.OpenFile(dir+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil // closing releases the lock
}
