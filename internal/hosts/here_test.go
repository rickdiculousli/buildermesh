package hosts

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestHereLine(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	r := Roster{AdminKey: "ak", Hosts: []Host{
		{Name: "ricks-macbook", Key: "m", Secure: true, ApproveKey: "ak"},
		{Name: "k16", Key: "k"},
		{Name: "pi", Key: "p"},
	}}
	seen := []Seen{
		{Host: r.Hosts[0], State: Asleep, At: Stamp(now.Add(-2*time.Hour - 5*time.Minute))},
		{Host: r.Hosts[1], State: Awake, At: Stamp(now)},
		{Host: r.Hosts[2], State: Unverified, Presence: Presence{Notes: "forged"}},
	}
	if got, want := HereLine("k16", r, seen, now), "this box is k16; secure: no; on the mesh: ricks-macbook (asleep 2h, secure, admin), pi (unverified)"; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	seen[1].Presence.Notes = "mine"
	if got, want := HereLine("ricks-macbook", r, seen, now), "this box is ricks-macbook (admin); secure: yes; on the mesh: k16 (awake), pi (unverified); notes: `bmesh hosts --notes`"; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if got := HereLine("k16", Roster{Hosts: []Host{{Name: "k16"}}}, nil, now); !strings.HasSuffix(got, "on the mesh: no other box") {
		t.Fatal(got)
	}
}

func TestReadNotesCutsAt16K(t *testing.T) {
	dir := t.TempDir()
	if n, cut, err := ReadNotes(dir); n != "" || cut || err != nil {
		t.Fatalf("missing notes: %q %v %v", n, cut, err)
	}
	long := strings.Repeat("é", NotesMax) // two bytes each: the cut lands mid-character
	if err := os.WriteFile(NotesPath(dir), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	n, cut, err := ReadNotes(dir)
	if err != nil || !cut || len(n) != NotesMax || !strings.HasSuffix(n, "é") {
		t.Fatalf("cut %v len %d err %v", cut, len(n), err)
	}
}

func TestWriteHereReplacesTheFile(t *testing.T) {
	dir := t.TempDir()
	for _, line := range []string{"one", "two"} {
		if err := WriteHere(dir, line); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(HerePath(dir))
	if err != nil || string(b) != "two\n" {
		t.Fatalf("%q %v", b, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("a temporary file was left: %v", entries)
	}
}

func TestHereTextMarksAStaleFile(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	line := "this box is k16; secure: no; on the mesh: mac (awake)\n"
	if got := HereText(line, now.Add(-2*time.Minute), now); got != line {
		t.Fatalf("fresh: %q", got)
	}
	if got, want := HereText(line, now.Add(-2*time.Hour), now), "this box is k16; secure: no; on the mesh: mac (awake); as of 2h ago, not updated since\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if HereText("", now.Add(-time.Hour), now) != "" {
		t.Fatal("an empty file prints nothing")
	}
}
