package hoststat

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// userHZ is /proc's clock tick, fixed at 100 on Linux.
const userHZ = 100

// readLinux reads the box from root (/proc): stat, meminfo, loadavg,
// schedstat, pressure/{cpu,memory,io} and each <pid>/stat and cwd (a
// node's cmdline too, to tell Claude Code's apart), and the schedstat and
// stat of each thread of a process whose cwd reads (this user's: an
// agent's processes are).
func readLinux(root string, pageSize int64, now time.Time) (*raw, error) {
	r := &raw{at: now, procs: map[int]*rproc{}, threads: map[int]rthread{}}
	b, err := os.ReadFile(filepath.Join(root, "stat"))
	if err != nil {
		return nil, err
	}
	if r.busy, r.total, r.cores, err = parseProcStat(b); err != nil {
		return nil, err
	}
	if b, err = os.ReadFile(filepath.Join(root, "meminfo")); err != nil {
		return nil, err
	}
	r.memUsed, r.memTotal, r.swapUsed = parseMeminfo(b)
	if b, err = os.ReadFile(filepath.Join(root, "loadavg")); err != nil {
		return nil, err
	}
	r.load1 = parseLoadavg(b)
	if b, err = os.ReadFile(filepath.Join(root, "schedstat")); err == nil {
		r.run, r.wait, r.sched = parseSchedstat(b)
	}
	r.p10, r.p60 = &Pressure{}, &Pressure{}
	any := false
	for _, f := range []struct {
		name     string
		ten, six **float64
	}{{"cpu", &r.p10.CPU, &r.p60.CPU}, {"memory", &r.p10.Mem, &r.p60.Mem}, {"io", &r.p10.IO, &r.p60.IO}} {
		b, err := os.ReadFile(filepath.Join(root, "pressure", f.name))
		if err != nil {
			continue
		}
		if a10, a60, ok := parsePSI(b); ok {
			*f.ten, *f.six, any = &a10, &a60, true
		}
		if f.name == "memory" {
			r.memFull, r.memFullOK = parseFullTotal(b)
		}
	}
	if !any {
		r.p10, r.p60 = nil, nil
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		dir := filepath.Join(root, e.Name())
		b, err := os.ReadFile(filepath.Join(dir, "stat"))
		if err != nil {
			continue // gone since the listing
		}
		p, err := parsePidStat(b, pageSize)
		if err != nil {
			continue
		}
		p.cwd, _ = os.Readlink(filepath.Join(dir, "cwd")) // another user's: none
		if r.sched && p.cwd != "" {
			readThreads(dir, pid, r.threads)
		}
		if p.comm == "node" {
			args, _ := os.ReadFile(filepath.Join(dir, "cmdline"))
			p.claude = IsClaude(p.comm, strings.Split(string(args), "\x00"))
		} else {
			p.claude = IsClaude(p.comm, nil)
		}
		r.procs[pid] = p
	}
	return r, nil
}

// readThreads adds the threads of the process in dir to out; a thread gone
// mid-read is skipped.
func readThreads(dir string, pid int, out map[int]rthread) {
	ents, err := os.ReadDir(filepath.Join(dir, "task"))
	if err != nil {
		return
	}
	for _, e := range ents {
		tid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "task", e.Name(), "schedstat"))
		if err != nil {
			continue
		}
		f := strings.Fields(string(b))
		if len(f) < 2 {
			continue
		}
		run, err1 := strconv.ParseFloat(f[0], 64)
		wait, err2 := strconv.ParseFloat(f[1], 64)
		st, err3 := os.ReadFile(filepath.Join(dir, "task", e.Name(), "stat"))
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		p, err := parsePidStat(st, 0)
		if err != nil {
			continue
		}
		out[tid] = rthread{pid: pid, start: p.start, run: run / 1e9, wait: wait / 1e9}
	}
}

// parseSchedstat sums /proc/schedstat's cpuN lines' fields 8 and 9 (after
// the name, the 7th and 8th): the nanoseconds tasks ran and waited
// runnable on each CPU, in seconds. ok is false with no cpu line, or when
// every one reads 0 (a kernel without CONFIG_SCHED_INFO).
func parseSchedstat(b []byte) (run, wait float64, ok bool) {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 9 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		r, err1 := strconv.ParseFloat(f[7], 64)
		w, err2 := strconv.ParseFloat(f[8], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		run, wait = run+r/1e9, wait+w/1e9
	}
	return run, wait, run+wait > 0
}

// parseFullTotal is a pressure file's `full` total= (µs).
func parseFullTotal(b []byte) (float64, bool) {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != "full" {
			continue
		}
		for _, kv := range f[1:] {
			if v, ok := strings.CutPrefix(kv, "total="); ok {
				x, err := strconv.ParseFloat(v, 64)
				return x, err == nil
			}
		}
	}
	return 0, false
}

// parseProcStat reads /proc/stat's cpu line: busy ticks (all but idle and
// iowait) and all ticks (user to steal), and counts the cpuN lines.
func parseProcStat(b []byte) (busy, total float64, cores int, err error) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	found := false
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		if f[0] != "cpu" {
			cores++
			continue
		}
		if len(f) < 5 {
			return 0, 0, 0, errors.New("hoststat: /proc/stat: short cpu line")
		}
		var v [8]float64
		for i := 0; i < 8 && i+1 < len(f); i++ {
			v[i], _ = strconv.ParseFloat(f[i+1], 64)
		}
		for _, x := range v {
			total += x
		}
		busy = total - v[3] - v[4]
		found = true
	}
	if !found {
		return 0, 0, 0, errors.New("hoststat: /proc/stat: no cpu line")
	}
	return busy, total, max(cores, 1), nil
}

// parseMeminfo is used (MemTotal less MemAvailable), total and swap used
// (SwapTotal less SwapFree), in bytes.
func parseMeminfo(b []byte) (used, total, swap int64) {
	kv := map[string]int64{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, _ := strconv.ParseInt(f[0], 10, 64)
		if len(f) > 1 && f[1] == "kB" {
			n *= 1024
		}
		kv[k] = n
	}
	return max(kv["MemTotal"]-kv["MemAvailable"], 0), kv["MemTotal"], max(kv["SwapTotal"]-kv["SwapFree"], 0)
}

func parseLoadavg(b []byte) float64 {
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

// parsePSI is a pressure file's `some` avg10 and avg60.
func parsePSI(b []byte) (avg10, avg60 float64, ok bool) {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != "some" {
			continue
		}
		var n int
		for _, kv := range f[1:] {
			k, v, _ := strings.Cut(kv, "=")
			x, err := strconv.ParseFloat(v, 64)
			if err != nil {
				continue
			}
			switch k {
			case "avg10":
				avg10, n = x, n+1
			case "avg60":
				avg60, n = x, n+1
			}
		}
		return avg10, avg60, n == 2
	}
	return 0, 0, false
}

// parsePidStat reads /proc/<pid>/stat: comm is up to the last ")", as it
// may hold spaces and parens; pgrp is its process group; t is utime+stime+cutime+cstime, so a reaped
// child's time is its parent's; rss is in pages.
func parsePidStat(b []byte, pageSize int64) (*rproc, error) {
	s := string(b)
	i, j := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if i < 0 || j < i {
		return nil, errors.New("hoststat: stat: no comm")
	}
	f := strings.Fields(s[j+1:])
	// f[0] is field 3 (state); field n is f[n-3].
	if len(f) < 22 {
		return nil, fmt.Errorf("hoststat: stat: %d fields", len(f)+2)
	}
	num := func(n int) float64 {
		v, _ := strconv.ParseFloat(f[n-3], 64)
		return v
	}
	ppid, _ := strconv.Atoi(f[4-3])
	pgrp, _ := strconv.Atoi(f[5-3])
	start, _ := strconv.ParseUint(f[22-3], 10, 64)
	return &rproc{
		ppid:  ppid,
		pgrp:  pgrp,
		comm:  s[i+1 : j],
		start: start,
		t:     (num(14) + num(15) + num(16) + num(17)) / userHZ,
		rss:   int64(num(24)) * pageSize,
	}, nil
}
