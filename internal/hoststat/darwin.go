package hoststat

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// readDarwin reads the box with macOS's commands, no cgo: ps for the
// processes (-S sums exited children into their parent), vm_stat for
// memory used, sysctl for cores, memory, swap, load and pressure, and lsof
// for the working directories of uid's processes.
func readDarwin(ctx context.Context, run func(context.Context, string, ...string) ([]byte, error), uid int, now time.Time) (*raw, error) {
	r := &raw{at: now}
	out, err := run(ctx, "ps", "-axS", "-o", "pid,ppid,time,rss,comm")
	if err != nil {
		return nil, fmt.Errorf("hoststat: ps: %w", err)
	}
	r.procs = parsePS(out)
	if out, err = run(ctx, "vm_stat"); err != nil {
		return nil, fmt.Errorf("hoststat: vm_stat: %w", err)
	}
	if r.memUsed, err = parseVMStat(out); err != nil {
		return nil, err
	}
	// sysctl prints the keys it knows and fails on one it doesn't
	// (kern.memorystatus_vm_pressure_level on an old macOS).
	out, _ = run(ctx, "sysctl", "hw.ncpu", "hw.memsize", "vm.swapusage", "vm.loadavg", "kern.memorystatus_vm_pressure_level")
	sc := parseSysctl(out)
	r.cores, _ = strconv.Atoi(sc["hw.ncpu"])
	r.cores = max(r.cores, 1)
	r.memTotal, _ = strconv.ParseInt(sc["hw.memsize"], 10, 64)
	r.swapUsed = parseSwapUsage(sc["vm.swapusage"])
	if f := strings.Fields(strings.Trim(sc["vm.loadavg"], "{} ")); len(f) > 0 {
		r.load1, _ = strconv.ParseFloat(f[0], 64)
	}
	if v, err := strconv.Atoi(sc["kern.memorystatus_vm_pressure_level"]); err == nil {
		r.mac = &v
	}
	// lsof exits 1 when a pid went between its listing and its read.
	out, _ = run(ctx, "lsof", "-a", "-d", "cwd", "-Fpn", "-u", strconv.Itoa(uid))
	for pid, dir := range parseLsof(out) {
		if p := r.procs[pid]; p != nil {
			p.cwd = dir
		}
	}
	var nodes []string
	for pid, p := range r.procs {
		p.claude = IsClaude(p.comm, nil)
		if p.comm == "node" {
			nodes = append(nodes, strconv.Itoa(pid))
		}
	}
	if len(nodes) > 0 {
		out, _ = run(ctx, "ps", "-o", "pid=,args=", "-p", strings.Join(nodes, ","))
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			pid, err := strconv.Atoi(f[0])
			if p := r.procs[pid]; err == nil && p != nil {
				p.claude = IsClaude(p.comm, f[1:])
			}
		}
	}
	return r, nil
}

// parsePS reads `ps -axS -o pid,ppid,time,rss,comm`: comm is the rest of
// the line (it may hold spaces), its base name kept; time is cumulative
// CPU, rss in KB.
func parsePS(b []byte) map[int]*rproc {
	out := map[int]*rproc{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue // the header
		}
		ppid, _ := strconv.Atoi(f[1])
		t, err := psTime(f[2])
		if err != nil {
			continue
		}
		rss, _ := strconv.ParseInt(f[3], 10, 64)
		// comm starts after the fourth field.
		rest := line
		for i := 0; i < 4; i++ {
			rest = strings.TrimLeft(rest, " \t")
			rest = rest[strings.IndexAny(rest, " \t"):]
		}
		out[pid] = &rproc{ppid: ppid, comm: path.Base(strings.TrimSpace(rest)), t: t, rss: rss * 1024}
	}
	return out
}

// psTime reads ps's time: [[dd-]hh:]mm:ss[.cc] in seconds.
func psTime(s string) (float64, error) {
	var days float64
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.ParseFloat(d, 64)
		if err != nil {
			return 0, err
		}
		days, s = n, rest
	}
	var t float64
	for _, p := range strings.Split(s, ":") {
		n, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0, err
		}
		t = t*60 + n
	}
	return days*86400 + t, nil
}

var pageSizeRE = regexp.MustCompile(`page size of (\d+) bytes`)

// parseVMStat is memory used: (wired + anonymous − purgeable + occupied by
// compressor) pages times the page size vm_stat's header prints.
func parseVMStat(b []byte) (int64, error) {
	m := pageSizeRE.FindSubmatch(b)
	if m == nil {
		return 0, errors.New("hoststat: vm_stat: no page size")
	}
	page, _ := strconv.ParseInt(string(m[1]), 10, 64)
	pages := map[string]int64{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(v), "."), 10, 64)
		if err == nil {
			pages[strings.Trim(k, `" `)] = n
		}
	}
	used := pages["Pages wired down"] + pages["Anonymous pages"] - pages["Pages purgeable"] + pages["Pages occupied by compressor"]
	return max(used, 0) * page, nil
}

// parseSysctl reads `sysctl <keys>`: key to value.
func parseSysctl(b []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// parseSwapUsage reads vm.swapusage's `used = 1024.50M` in bytes.
func parseSwapUsage(s string) int64 {
	_, rest, ok := strings.Cut(s, "used = ")
	if !ok {
		return 0
	}
	f := strings.Fields(rest)
	if len(f) == 0 {
		return 0
	}
	v := f[0]
	mult := 1.0
	switch v[len(v)-1] {
	case 'K':
		mult = 1 << 10
	case 'M':
		mult = 1 << 20
	case 'G':
		mult = 1 << 30
	}
	n, _ := strconv.ParseFloat(strings.TrimRight(v, "KMG"), 64)
	return int64(n * mult)
}

// parseLsof reads `lsof -Fpn`: a p<pid> line, then its n<path>.
func parseLsof(b []byte) map[int]string {
	out := map[int]string{}
	pid := 0
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			if pid > 0 {
				out[pid] = line[1:]
			}
		}
	}
	return out
}
