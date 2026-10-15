//go:build windows

package shell

import "strings"

func argv(line string, args []string) []string {
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"") {
			a = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		}
		line += " " + a
	}
	return []string{"cmd", "/C", line}
}
