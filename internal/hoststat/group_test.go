package hoststat

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// groupStat is pidStat with the process group set.
func groupStat(pid, ppid, pgrp int, ticks [4]int) string {
	return strconv.Itoa(pid) + " (x) S " + strconv.Itoa(ppid) + " " + strconv.Itoa(pgrp) + " 1 0 -1 4194560 100 0 0 0 " +
		strconv.Itoa(ticks[0]) + " " + strconv.Itoa(ticks[1]) + " " + strconv.Itoa(ticks[2]) + " " + strconv.Itoa(ticks[3]) +
		" 20 0 1 0 " + strconv.Itoa(pid) + " 1000000 10 18446744073709551615 0 0 0 0 0\n"
}

func writeProc(t *testing.T, root string, stats map[int]string) {
	t.Helper()
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		os.RemoveAll(filepath.Join(root, e.Name()))
	}
	for pid, s := range stats {
		d := filepath.Join(root, strconv.Itoa(pid))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "stat"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A group's CPU is its own processes' only, and a child reaped by a
// parent in the group counts once: its time since the last read, not
// again when it moves into the parent's.
func TestGroupCPU(t *testing.T) {
	root := t.TempDir()
	writeProc(t, root, map[int]string{
		10: groupStat(10, 1, 10, [4]int{100, 0, 0, 0}),
		11: groupStat(11, 10, 10, [4]int{300, 0, 0, 0}),
		20: groupStat(20, 1, 20, [4]int{9000, 0, 0, 0}), // another group's
	})
	prev, err := readGroupLinux(root, 10)
	if err != nil || len(prev.procs) != 2 {
		t.Fatalf("read %v %v", prev, err)
	}
	if got := prev.CPUSince(nil); got != 4 {
		t.Fatalf("all time: %v, want 4", got)
	}
	// 11 ran 1 s more and was reaped by 10, whose cutime now holds its 4 s;
	// 10 itself ran 0.5 s more; a new 12 has run 2 s.
	writeProc(t, root, map[int]string{
		10: groupStat(10, 1, 10, [4]int{150, 0, 400, 0}),
		12: groupStat(12, 10, 10, [4]int{200, 0, 0, 0}),
		20: groupStat(20, 1, 20, [4]int{9900, 0, 0, 0}),
	})
	cur, err := readGroupLinux(root, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := cur.CPUSince(prev); got != 3.5 {
		t.Fatalf("since: %v, want 3.5 (1 reaped + 0.5 own + 2 new)", got)
	}
	if got := cur.CPUSince(cur); got != 0 {
		t.Fatalf("since itself: %v", got)
	}
}

func TestParseGroupPS(t *testing.T) {
	out := "PGID   PID  PPID      TIME    RSS COMM\n" +
		"  700   700     1   0:01.50   1000 /bin/sh\n" +
		"  700   701   700   1:02.00   2000 /usr/local/go/bin/go test\n" +
		"  800   800     1  10:00.00   3000 /usr/bin/other\n"
	g := parseGroupPS([]byte(out), 700)
	if len(g) != 2 || g[701].t != 62 || g[701].comm != "go test" || g[700].ppid != 1 {
		t.Fatalf("%+v", g)
	}
}
