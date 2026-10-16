package mesh

import "testing"

func TestRepoTokenIsAPathToken(t *testing.T) {
	for name, want := range map[string]string{
		"app":                "app",
		"MapleMarketScanner": "maplemarketscanner",
		"my_repo.js":         "my-repo-js",
		"a..b_":              "a-b",
		"Foo--Bar":           "foo-bar",
	} {
		got := RepoToken(name)
		if got != want {
			t.Errorf("RepoToken(%q) = %q, want %q", name, got, want)
		}
		if _, err := ParsePath(got + "-round-1.eng"); err != nil {
			t.Errorf("RepoToken(%q) = %q is not a path token: %v", name, got, err)
		}
	}
}
