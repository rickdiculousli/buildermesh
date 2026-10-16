package selfupdate

// Lock does nothing on Windows, so the daemon's update and a `bmesh update`
// run by hand there can race.
func Lock(dir string) (unlock func(), err error) { return func() {}, nil }
