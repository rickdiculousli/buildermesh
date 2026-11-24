package hoststat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Group is one read of a process group's processes: each one's CPU
// seconds, its own and its reaped children's.
type Group struct{ procs map[int]*rproc }

// ReadGroup reads the processes of process group pgid: /proc/<pid>/stat on
// Linux, `ps -axS` on macOS; ErrUnsupported elsewhere (Windows).
func ReadGroup(ctx context.Context, pgid int) (*Group, error) {
	switch runtime.GOOS {
	case "linux":
		return readGroupLinux("/proc", pgid)
	case "darwin":
		out, err := run(ctx, "ps", "-axS", "-o", "pgid,pid,ppid,time,rss,comm")
		if err != nil {
			return nil, fmt.Errorf("hoststat: ps: %w", err)
		}
		return &Group{procs: parseGroupPS(out, pgid)}, nil
	}
	return nil, ErrUnsupported
}

// readGroupLinux reads each <pid>/stat under root whose process group is
// pgid.
func readGroupLinux(root string, pgid int) (*Group, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	g := &Group{procs: map[int]*rproc{}}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "stat"))
		if err != nil {
			continue // gone since the listing
		}
		if p, err := parsePidStat(b, 1); err == nil && p.pgrp == pgid {
			g.procs[pid] = p
		}
	}
	return g, nil
}

// parseGroupPS keeps the lines of `ps -axS -o pgid,pid,ppid,time,rss,comm`
// in process group pgid, read as parsePS reads the rest of each line.
func parseGroupPS(b []byte, pgid int) map[int]*rproc {
	var keep strings.Builder
	want := strconv.Itoa(pgid)
	for _, line := range strings.Split(string(b), "\n") {
		first, rest, ok := strings.Cut(strings.TrimLeft(line, " \t"), " ")
		if ok && first == want {
			keep.WriteString(rest + "\n")
		}
	}
	return parsePS([]byte(keep.String()))
}

// CPUSince is the CPU seconds the group used since prev (nil: all its
// processes' time): per pid, a reaped child's last read time taken out of
// its parent's, which now holds it.
func (g *Group) CPUSince(prev *Group) float64 {
	var p map[int]*rproc
	if prev != nil {
		p = prev.procs
	}
	var sum float64
	for _, c := range procCPU(&raw{procs: p}, &raw{procs: g.procs}, 1) {
		sum += c
	}
	return sum
}
