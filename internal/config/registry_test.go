package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const twoRows = `# the registry
[repos.app]   # the app
path = "~/repos/app"
remote = "me/app"
verify = ["npm test"]

# tools: a plugin marketplace
[repos.tools]
path = "~/repos/tools"
mode = "companion"
`

func TestParseReposRefusesAnUnknownKey(t *testing.T) {
	rows, err := ParseRepos(twoRows)
	if err != nil || len(rows) != 2 || rows["tools"].Mode != "companion" || rows["app"].Mode != "ship" {
		t.Fatalf("%v %+v", err, rows)
	}
	if _, err := ParseRepos(twoRows + "agent = \"x\"\n"); err == nil || !strings.Contains(err.Error(), "unknown key repos.tools.agent") {
		t.Fatalf("unknown key: %v", err)
	}
}

func TestWriteReposTextKeepsThePreviousFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), ReposFile)
	if err := WriteReposText(p, "one\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + ".prev"); !os.IsNotExist(err) {
		t.Fatal("a first write made a .prev")
	}
	if err := WriteReposText(p, "two\n"); err != nil {
		t.Fatal(err)
	}
	if err := WriteReposText(p, "two\n"); err != nil { // unchanged: .prev stays one
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	prev, _ := os.ReadFile(p + ".prev")
	if string(got) != "two\n" || string(prev) != "one\n" {
		t.Fatalf("file %q prev %q", got, prev)
	}
}

func TestDiffRepos(t *testing.T) {
	old, _ := ParseRepos(twoRows)
	next, _ := ParseRepos(strings.Replace(twoRows, `["npm test"]`, `["npm test", "npm run lint"]`, 1) +
		"\n[repos.new]\npath = \"~/repos/new\"\n")
	delete(next, "tools")
	got := strings.Join(DiffRepos(old, next), "\n")
	want := `~ app: verify: ["npm test"] -> ["npm test", "npm run lint"]
+ new: path ~/repos/new, mode ship, routines ["verify", "review"]
- tools`
	if got != want {
		t.Fatalf("diff:\n%s\nwant:\n%s", got, want)
	}
	if d := DiffRepos(old, old); len(d) != 0 {
		t.Fatalf("no change: %v", d)
	}
}

func TestRemoveRepoTextKeepsTheOtherRowsAndComments(t *testing.T) {
	got, err := RemoveRepoText(twoRows, "app")
	if err != nil {
		t.Fatal(err)
	}
	want := `# the registry
# tools: a plugin marketplace
[repos.tools]
path = "~/repos/tools"
mode = "companion"
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	got, err = RemoveRepoText(twoRows, "tools")
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := ParseRepos(got); err != nil || len(rows) != 1 || rows["app"].Remote != "me/app" || !strings.Contains(got, "# the app") {
		t.Fatalf("without tools: %v\n%s", err, got)
	}
	if strings.Contains(got, "plugin marketplace") {
		t.Fatalf("tools' own comment stayed:\n%s", got)
	}
	if _, err := RemoveRepoText(twoRows, "nope"); err == nil {
		t.Fatal("removed a row that isn't there")
	}
	// A table the text cut can't find is written afresh from the rows.
	dotted := "[repos]\napp = { path = \"~/repos/app\" }\ntools = { path = \"~/repos/tools\" }\n"
	got, err = RemoveRepoText(dotted, "app")
	if rows, perr := ParseRepos(got); err != nil || perr != nil || len(rows) != 1 || rows["tools"].Path != "~/repos/tools" {
		t.Fatalf("inline tables: %v %v\n%s", err, perr, got)
	}
}

func TestSameRemote(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"me/app", "git@github.com:me/app.git", true},
		{"me/app", "https://github.com/me/app", true},
		{"ssh://git@github.com/me/app.git", "https://github.com/Me/App/", true},
		{"me/app", "me/other", false},
		{"ssh://rick@k16/home/rick/git/x.git", "ssh://rick@k16/home/rick/git/x", true},
		{"", "", false},
	} {
		if got := SameRemote(c.a, c.b); got != c.same {
			t.Errorf("SameRemote(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
