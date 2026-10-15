package selfupdate

// Lock does nothing on Windows, where the daemon's update is untested.
func Lock(dir string) (unlock func(), err error) { return func() {}, nil }
