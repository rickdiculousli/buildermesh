package selfupdate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakeProc starts a process that stays up for the test and returns its pid.
func fakeProc(t *testing.T) int {
	t.Helper()
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep command")
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd.Process.Pid
}

func writeRec(t *testing.T, dir, name string, r ProcRecord) {
	t.Helper()
	b, _ := json.Marshal(r)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ProcPath(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWriteReadRemoveProc(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	remove, err := WriteProc(dir, "serve", Version{Revision: "0123456789abcdef", Time: "2026-10-01T00:00:00Z", Release: "v0.1.0+2"})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := ReadProc(dir, "serve")
	if !ok || r.Pid != os.Getpid() || r.Revision != "0123456789abcdef" || r.Release != "v0.1.0+2" || r.Label() != "v0.1.0+2" {
		t.Fatalf("read %+v %v", r, ok)
	}
	remove()
	if _, err := os.Stat(ProcPath(dir, "serve")); !os.IsNotExist(err) {
		t.Fatalf("record left: %v", err)
	}
	if (ProcRecord{Revision: "0123456789abcdef"}).Label() != "01234567" {
		t.Fatal("label without a release")
	}
}

func TestHubLine(t *testing.T) {
	dir := t.TempDir()
	serve := ProcRecord{Pid: os.Getpid(), Revision: "bbbbbbbbbbbb", Release: "v0.2.0", Time: "2026-10-10T00:00:00Z"}
	if l := HubLine(dir, serve); l != "" {
		t.Fatalf("no hub: %q", l)
	}
	pid := fakeProc(t)
	hub := ProcRecord{Pid: pid, Revision: "aaaaaaaaaaaa", Release: "v0.1.0", Time: "2026-10-01T00:00:00Z"}
	writeRec(t, dir, "hub", hub)
	if l := HubLine(dir, serve); l != "hub runs v0.1.0, serve v0.2.0: restart bmesh-hub" {
		t.Fatalf("older hub: %q", l)
	}
	// The hub newer than serve, or the same commit: nothing.
	if l := HubLine(dir, ProcRecord{Revision: "cccccccc", Time: "2026-09-01T00:00:00Z"}); l != "" {
		t.Fatalf("newer hub: %q", l)
	}
	if l := HubLine(dir, ProcRecord{Revision: "aaaaaaaa"}); l != "" {
		t.Fatalf("same commit: %q", l)
	}
	// No times: different is enough.
	if l := HubLine(dir, ProcRecord{Revision: "dddddddddddd"}); l != "hub runs v0.1.0, serve dddddddd: restart bmesh-hub" {
		t.Fatalf("no time: %q", l)
	}
	// A dead hub pid: nothing.
	writeRec(t, dir, "hub", ProcRecord{Pid: deadPid(t), Revision: "aaaaaaaaaaaa", Time: "2026-10-01T00:00:00Z"})
	if l := HubLine(dir, serve); l != "" {
		t.Fatalf("dead hub: %q", l)
	}
}

// deadPid is the pid of a process that has exited.
func deadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}
