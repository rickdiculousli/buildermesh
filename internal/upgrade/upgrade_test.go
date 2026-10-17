package upgrade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRefusesWhatAWalkCanNotDo(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"[[step]]\nkind = \"note\"\n", `kind "note"`},
		{"[[step]]\nkind = \"ask\"\nkey = \"track\"\nquestion = \"q\"\ndefault = \"a\"\n", `want [section] key`},
		{"[[step]]\nkind = \"ask\"\nkey = \"[update] track\"\nquestion = \"q\"\n", "no default"},
		{"[[step]]\nkind = \"ask\"\nkey = \"[update] track\"\nquestion = \"q\"\ndefault = \"x\"\nchoices = [\"a\"]\n", "not one of the choices"},
		{"[[step]]\nkind = \"run\"\ncommand = \"true\"\n", "no why"},
		{"[[step]]\nkind = \"run\"\ncommand = \"true\"\nwhy = \"w\"\nos = [\"plan9\"]\n", `os "plan9"`},
		{"[[step]]\nkind = \"check\"\ncommand = \"true\"\nwhen = \"track is release\"\n", "want [section] key =="},
		{"[[step]]\nkind = \"check\"\ncommand = \"true\"\nwhatever = 1\n", "unknown key"},
	} {
		if _, err := Parse("v0.2.0", tc.text); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.text, err, tc.want)
		}
	}
}

func TestNotesKeysWhenAndText(t *testing.T) {
	n, err := Parse("v0.2.0", `summary = "two things"
[[step]]
kind = "ask"
key = "[update] track"
changed = "boxes follow releases."
question = "Follow releases?"
choices = ["release", "latest"]
default = "release"
[[step]]
kind = "ask"
file = "registry"
key = "pin"
question = "Pin?"
default = "v1"
[[step]]
kind = "run"
why = "restart the hub"
command = "systemctl --user restart bmesh-hub"
os = ["linux"]
when = '[hub] role != "" && [runner] nope == "x"'
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(n.Unknown(), ","); got != "[runner] nope" {
		t.Fatalf("unknown %q", got)
	}
	if !n.Steps[2].Applies("linux") || n.Steps[2].Applies("darwin") || !n.Steps[0].Applies("windows") {
		t.Fatal("applies")
	}
	w, _ := ParseWhen(`[hub] role != "" && [update] track == release`)
	get := func(vals map[string]string) func(string, string) (string, bool) {
		return func(s, k string) (string, bool) { v, ok := vals[s+"."+k]; return v, ok }
	}
	if !w.Holds(get(map[string]string{"hub.role": "hub", "update.track": "release"})) || w.Holds(get(map[string]string{"update.track": "release"})) {
		t.Fatal("holds")
	}
	want := "Upgrading to v0.2.0 (docs/upgrading/v0.2.0.toml):\ntwo things\n" +
		"1. ask [update] track: boxes follow releases. Follow releases? (release/latest, default release)\n" +
		"2. ask repos.toml pin: Pin? (v1)\n" +
		"3. restart the hub: `systemctl --user restart bmesh-hub` [linux] when [hub] role != \"\" && [runner] nope == \"x\""
	if got := n.Text(); got != want {
		t.Fatalf("text:\n%s\nwant:\n%s", got, want)
	}
}

func TestPendingSinceTheRecord(t *testing.T) {
	sd := t.TempDir()
	for own, want := range map[string]string{
		"":         "",
		"v0.1.0":   "",
		"v0.1.0+4": "",
		"v0.2.0":   "upgrade notes pending since v0.1.0: run bmesh update",
		"v0.2.0+1": "upgrade notes pending since v0.1.0: run bmesh update",
	} {
		if got := Pending(sd, own); got != want {
			t.Errorf("no record, %q: %q, want %q", own, got, want)
		}
	}
	if err := WriteInstalled(sd, Record{Version: "v0.2.0", Revision: "abc", At: "2026-10-17T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if got := Pending(sd, "v0.2.0+3"); got != "" {
		t.Fatalf("at the record: %q", got)
	}
	if got := Pending(sd, "v0.3.0"); got != "upgrade notes pending since v0.2.0: run bmesh update" {
		t.Fatalf("past it: %q", got)
	}
	if r, ok := ReadInstalled(sd); !ok || r.Version != "v0.2.0" || r.Revision != "abc" || r.At != "2026-10-17T00:00:00Z" {
		t.Fatalf("record %+v", r)
	}
	if err := WriteInstalled(sd, Record{Version: "v0.2.0+3 (5d76ed88)", Revision: "5d76ed88aa", At: "t"}); err != nil {
		t.Fatal(err)
	}
	if r, ok := ReadInstalled(sd); !ok || r.Version != "v0.2.0+3" || r.Revision != "5d76ed88aa" || r.At != "t" {
		t.Fatalf("a label's record %+v", r)
	}
	// A start-over removes the state dir mid-walk: the next save makes it.
	if err := os.RemoveAll(sd); err != nil {
		t.Fatal(err)
	}
	if err := (&State{To: "x"}).Save(sd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sd, "upgrade.json")); err != nil {
		t.Fatal(err)
	}
}

// The repo's own notes parse and name only keys this binary has.
func TestRepoNotesParse(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", Dir, "*.toml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no notes: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		tag := strings.TrimSuffix(filepath.Base(f), ".toml")
		n, err := Parse(tag, string(b))
		if err != nil {
			t.Fatal(err)
		}
		if u := n.Unknown(); len(u) > 0 {
			t.Fatalf("%s names unknown keys %v", f, u)
		}
		if tag == First && !n.StartOver {
			t.Fatalf("%s: the first release's notes start over", f)
		}
	}
}
