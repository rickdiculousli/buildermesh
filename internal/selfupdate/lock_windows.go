package selfupdate

// Lock does nothing on Windows, so the daemon's update and a `bmesh update`
// run by hand there can race.
func Lock(dir string) (unlock func(), err error) { return func() {}, nil }

// LockUpdate does nothing on Windows, so a daemon there may restart onto
// a binary while `bmesh update` still walks its notes.
func LockUpdate(stateDir string) (unlock func(), err error) { return func() {}, nil }

// TryLockUpdate does nothing on Windows and always takes it.
func TryLockUpdate(stateDir string) (unlock func(), ok bool, err error) {
	return func() {}, true, nil
}
