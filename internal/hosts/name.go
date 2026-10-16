package hosts

import "strings"

// ValidHostName: a host name is one subject token, ^[a-z0-9][a-z0-9-]*$, no
// "--", at most 63 characters.
func ValidHostName(name string) bool {
	if name == "" || len(name) > 63 || name[0] == '-' || strings.Contains(name, "--") {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}
