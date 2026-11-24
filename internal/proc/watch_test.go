package proc

import (
	"strings"
	"testing"
	"time"
)

func TestWatchCountsOutput(t *testing.T) {
	w := NewWatch(time.Minute, nil)
	w.Write([]byte("one\ntw"))
	if w.Last() != "tw" {
		t.Fatalf("partial: %q", w.Last())
	}
	w.Write([]byte("o\n  \n\r50%\r"))
	if w.lines != 3 || w.Last() != "50%" {
		t.Fatalf("lines %d last %q", w.lines, w.Last())
	}
	w.Write([]byte(strings.Repeat("é", 1000)))
	if l := []rune(w.Last()); len(l) != lineMax || l[lineMax-1] != '…' {
		t.Fatalf("cut: %d runes", len(l))
	}
	if Every(59*time.Minute) != Cadence || Every(time.Hour) != LongCadence {
		t.Fatal("cadence")
	}
	c := Checkup{Elapsed: 4*time.Minute + time.Second, Lines: 37, CPU: 61 * time.Second, Last: "ok", QuietN: 2, Quiet: 4 * time.Minute}
	if c.String() != "4m: 37 lines, cpu 61s: ok; quiet 4m" {
		t.Fatalf("%q", c)
	}
	c.NoCPU, c.QuietN, c.Last = true, 0, ""
	if c.String() != "4m: 37 lines" || c.Stalled() {
		t.Fatalf("%q", c)
	}
}
