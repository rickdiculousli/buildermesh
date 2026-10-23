package gitfetch

import "testing"

func TestHasFlag(t *testing.T) {
	for version, want := range map[string]bool{
		"git version 2.53.0\n":               true,
		"git version 2.29.0":                 true,
		"git version 2.39.5 (Apple Git-154)": true,
		"git version 2.45.1.windows.1":       true,
		"git version 3.0.0":                  true,
		"git version 2.28.1":                 false,
		"git version 1.9.5":                  false,
		"not git":                            false,
	} {
		if got := HasFlag(version); got != want {
			t.Errorf("HasFlag(%q) = %v, want %v", version, got, want)
		}
	}
}
