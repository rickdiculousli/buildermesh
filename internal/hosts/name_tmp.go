package hosts

import "regexp"

var hostNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidHostName stands in until the invite piece's name.go is merged.
func ValidHostName(name string) bool {
	return len(name) <= 63 && hostNameRE.MatchString(name) && !regexpContains(name)
}

func regexpContains(s string) bool { return len(s) > 1 && (contains2(s)) }

func contains2(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '-' && s[i+1] == '-' {
			return true
		}
	}
	return false
}
