package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestChildArgs(t *testing.T) {
	for _, c := range []struct {
		config string
		rest   []string
		want   []string
	}{
		{"", []string{"--restart"}, []string{"serve"}},
		{"/c/config.toml", []string{"--restart", "--detach", "--listen", ":1"}, []string{"serve", "--config", "/c/config.toml", "--listen", ":1"}},
		{"/c/config.toml", []string{"-restart=true", "--config", "/c/config.toml"}, []string{"serve", "--config", "/c/config.toml"}},
		{"", []string{"--listen=:2", "--restart", "--detach=true"}, []string{"serve", "--listen=:2"}},
	} {
		if got := childArgs(c.config, c.rest); !slices.Equal(got, c.want) {
			t.Errorf("childArgs(%q, %q) = %q, want %q", c.config, c.rest, got, c.want)
		}
	}
}

func TestServeDetachNeedsRestart(t *testing.T) {
	var out, errb strings.Builder
	if code := run([]string{"serve", "--detach"}, strings.NewReader(""), &out, &errb); code != exitUsage {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
}

func TestRotLogKeepsOnePrevious(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.log")
	l, err := openRotLog(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"aaaa\n", "bbbb\n", "cccc\n", "dddd\n", "eeeeeeeeeeeeeee\n"} {
		if _, err := l.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()
	cur, _ := os.ReadFile(path)
	prev, _ := os.ReadFile(filepath.Join(filepath.Dir(path), "serve.prev.log"))
	if string(cur) != "eeeeeeeeeeeeeee\n" || string(prev) != "cccc\ndddd\n" {
		t.Fatalf("serve.log %q, serve.prev.log %q", cur, prev)
	}
	// Reopened, it counts what the file already holds.
	l, err = openRotLog(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	l.Write([]byte("ffff\n"))
	l.Close()
	if cur, _ := os.ReadFile(path); string(cur) != "ffff\n" {
		t.Fatalf("serve.log %q", cur)
	}
}

func TestSchtasksLines(t *testing.T) {
	lines, err := schtasksLines(`C:\Program Files\bmesh\bmesh.exe`, `C:\Users\Ann Lee\AppData\Roaming\bmesh\config.toml`, `C:\Users\Ann Lee\.local\state\bmesh\serve.log`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`schtasks /Create /TN buildermesh /SC ONLOGON /RL LIMITED /F /TR "\"C:\Program Files\bmesh\bmesh.exe\" serve --restart --detach --config \"C:\Users\Ann Lee\AppData\Roaming\bmesh\config.toml\""`,
		`schtasks /Run /TN buildermesh`,
		`rem in cmd: bmesh serve then starts at every logon with no window; its log is C:\Users\Ann Lee\.local\state\bmesh\serve.log`,
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	for _, c := range [][2]string{
		{`C:\100%\bmesh.exe`, `C:\c\config.toml`},
		{`C:\b\bmesh.exe`, `C:\%USERPROFILE%\config.toml`},
		{`C:\b"\bmesh.exe`, `C:\c\config.toml`},
		{`C:\b\bmesh.exe`, `C:\` + strings.Repeat("x", 250) + `\config.toml`},
	} {
		if lines, err := schtasksLines(c[0], c[1], `C:\l\serve.log`); err == nil {
			t.Errorf("%q, %q accepted: %q", c[0], c[1], lines)
		}
	}
}
