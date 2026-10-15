//go:build !windows

package shell

func argv(line string, args []string) []string {
	if len(args) == 0 {
		return []string{"sh", "-c", line}
	}
	return append([]string{"sh", "-c", line + ` "$@"`, "sh"}, args...)
}
