package hosts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// NotesMax is how much of notes.md travels in presence; the rest is cut.
const NotesMax = 16 << 10

// NotesPath is the box's notes, <config dir>/notes.md.
func NotesPath(configDir string) string { return filepath.Join(configDir, "notes.md") }

// ReadNotes reads notes.md, cut to NotesMax on a character boundary (cut
// says so); "" when there is none.
func ReadNotes(configDir string) (notes string, cut bool, err error) {
	b, err := os.ReadFile(NotesPath(configDir))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	s := strings.ToValidUTF8(string(b), "")
	if len(s) > NotesMax {
		n := NotesMax
		for n > 0 && !utf8.RuneStart(s[n]) {
			n--
		}
		s, cut = s[:n], true
	}
	return strings.TrimSpace(s), cut, nil
}

// HerePath is the session line the daemon writes each minute and
// `bmesh hosts --here` prints: <state dir>/this-box.txt.
func HerePath(stateDir string) string { return filepath.Join(stateDir, "this-box.txt") }

// HereLine says which box this is and who else is on the mesh, for a
// session's first line: "this box is k16; secure: no; on the mesh:
// ricks-macbook (asleep 2h, secure, admin)". seen is the roster joined with
// presence (this box among them); a box with verified notes adds a pointer
// to `bmesh hosts --notes`.
func HereLine(self string, r Roster, seen []Seen, now time.Time) string {
	admin, _ := r.Admin()
	row, _ := r.Host(self)
	head := "this box is " + self
	if admin.Name == self {
		head += " (admin)"
	}
	secure := "no"
	if row.Secure {
		secure = "yes"
	}
	var others []string
	notes := false
	for _, s := range seen {
		if s.State != Unverified && s.Presence.Notes != "" {
			notes = true
		}
		if s.Host.Name == self {
			continue
		}
		words := []string{s.State}
		if s.State == Asleep {
			if at, err := time.Parse(time.RFC3339, s.At); err == nil {
				words[0] += " " + ago(now.Sub(at))
			}
		}
		if s.Host.Secure {
			words = append(words, "secure")
		}
		if s.Host.Name == admin.Name {
			words = append(words, "admin")
		}
		others = append(others, fmt.Sprintf("%s (%s)", s.Host.Name, strings.Join(words, ", ")))
	}
	mesh := "no other box"
	if len(others) > 0 {
		mesh = strings.Join(others, ", ")
	}
	line := fmt.Sprintf("%s; secure: %s; on the mesh: %s", head, secure, mesh)
	if notes {
		line += "; notes: `bmesh hosts --notes`"
	}
	return line
}

// ago is d in its largest whole unit: 5m, 2h, 3d.
func ago(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}

// WriteHere writes line to this-box.txt through a temporary file, so a
// session never reads half of it.
func WriteHere(stateDir, line string) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(stateDir, ".this-box-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(line + "\n"); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), HerePath(stateDir))
}
