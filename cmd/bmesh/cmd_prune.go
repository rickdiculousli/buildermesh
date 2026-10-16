package main

// cmdPrune sends prune to every roster box that accepts it (023).
func (a *app) cmdPrune(args []string) error {
	return fail(exitUsage, "usage: bmesh prune [--repo R | --all] [--apply] [--wait D]")
}
