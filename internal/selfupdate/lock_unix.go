//go:build !windows

package selfupdate

import (
	"errors"
	"os"
	"syscall"
)

// Lock takes the advisory lock <dir>.lock, waiting for another holder, so
// one process at a time moves a main checkout and builds from it (the
// daemon and a `bmesh update` share the daemon's checkout).
func Lock(dir string) (unlock func(), err error) {
	unlock, _, err = flock(dir+".lock", true)
	return unlock, err
}

// LockUpdate takes <state dir>/update.lock (UpdateLockPath), waiting for
// another holder: `bmesh update` holds it from before its swap to the end
// of its walk, so the daemon never restarts onto a binary mid-walk.
func LockUpdate(stateDir string) (unlock func(), err error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, err
	}
	unlock, _, err = flock(UpdateLockPath(stateDir), true)
	return unlock, err
}

// TryLockUpdate takes <state dir>/update.lock without waiting; ok is false
// while another process holds it.
func TryLockUpdate(stateDir string) (unlock func(), ok bool, err error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, false, err
	}
	return flock(UpdateLockPath(stateDir), false)
}

// flock takes the exclusive advisory lock on path, waiting when wait is
// set; ok is false when it doesn't wait and another holds it.
func flock(path string, wait bool) (unlock func(), ok bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { f.Close() }, true, nil // closing releases the lock
}
