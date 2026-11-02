package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// The flag levels (108).
const (
	Overworking = "overworking"
	Runaway     = "runaway"
)

// Flag is one rule an agent trips: its level, the rule's name and the
// words that say it with its numbers.
type Flag struct {
	Level string `json:"level"`
	Rule  string `json:"rule"`
	Words string `json:"words"`
}

// The rules' thresholds, fixed in code from the fleet's 10-15 to 10-24
// (spec 108's calibration): never config.
const (
	sizeOver    = 2.5         // read past this many times its size's median: overworking
	sizeRunaway = 5.0         // and past this many: runaway
	sizeFloor   = 5_000_000   // a read at or under this is never heavy, whatever its median (XS medians are tiny)
	refusedOver = 3           // land refused this many times or more: overworking
	burnOver    = 50_000_000  // read past this in the trailing 60 minutes: overworking
	burnRunaway = 100_000_000 // and past this: runaway
	minRuns     = 5           // the done runs a median needs
)

// Sizes are the spec sizes the size rule compares within.
var Sizes = []string{"XS", "S", "M", "L"}

// fixer is the baseline key a fixer is judged by in place of a size.
const fixer = "fixer"

// Subject is what the rules judge an agent by.
type Subject struct {
	Agent   string
	Role    string
	Repo    string
	Size    string   // a lead's spec size
	Read    int64    // its read over its life
	Ctx     int64    // its main thread's newest context
	Hour    int64    // its read in the trailing 60 minutes
	Refused []string // its land refusals' texts, oldest first
}

// Run is one done lead or fixer, for the baselines.
type Run struct {
	Agent string `json:"agent"`
	Role  string `json:"role"`
	Repo  string `json:"repo"`
	Size  string `json:"size"`
	Read  int64  `json:"read"`
	Wall  int64  `json:"wall"` // seconds from started to done
}

// Median is the middle of a baseline's runs.
type Median struct {
	Read int64 `json:"read"`
	Wall int64 `json:"wall"`
	N    int   `json:"n"`
}

// Baselines are the medians the size rule compares with, keyed
// <repo>/<size> and, for the fleet, /<size>; a fixer's size is "fixer".
type Baselines map[string]Median

// NewBaselines takes the median read and wall time of runs per repo and
// size, and per size across the fleet.
func NewBaselines(runs []Run) Baselines {
	groups := map[string][]Run{}
	for _, r := range runs {
		k := baseSize(r.Role, r.Size)
		if k == "" {
			continue
		}
		groups[r.Repo+"/"+k] = append(groups[r.Repo+"/"+k], r)
		groups["/"+k] = append(groups["/"+k], r)
	}
	b := Baselines{}
	for k, rs := range groups {
		reads, walls := make([]int64, len(rs)), make([]int64, len(rs))
		for i, r := range rs {
			reads[i], walls[i] = r.Read, r.Wall
		}
		b[k] = Median{Read: median(reads), Wall: median(walls), N: len(rs)}
	}
	return b
}

func median(xs []int64) int64 {
	slices.Sort(xs)
	n := len(xs)
	if n%2 == 1 {
		return xs[n/2]
	}
	return (xs[n/2-1] + xs[n/2]) / 2
}

// baseSize is the baseline key a run of role and size falls in: its size
// for a lead, "fixer" for a fixer, "" for none.
func baseSize(role, size string) string {
	switch {
	case role == "fixer":
		return fixer
	case role == "lead" && slices.Contains(Sizes, size):
		return size
	}
	return ""
}

// For is the median an agent of role, repo and size is judged by and whose
// it is (the repo, or "fleet"): its repo's with 5 runs or more, else the
// fleet's with 5 or more; ok is false when neither has 5.
func (b Baselines) For(role, repo, size string) (m Median, scope string, ok bool) {
	k := baseSize(role, size)
	if k == "" {
		return Median{}, "", false
	}
	if m := b[repo+"/"+k]; m.N >= minRuns && repo != "" {
		return m, repo, true
	}
	if m := b["/"+k]; m.N >= minRuns {
		return m, "fleet", true
	}
	return Median{}, "", false
}

// Flags are the rules s trips, the worst first (runaway before
// overworking, then in the table's order); compactSoon is the box's
// [context] compact_soon (0: the context rule is off).
func Flags(s Subject, b Baselines, compactSoon int) []Flag {
	var out []Flag
	if m, scope, ok := b.For(s.Role, s.Repo, s.Size); ok && m.Read > 0 && s.Read > sizeFloor {
		level := ""
		switch {
		case float64(s.Read) > sizeRunaway*float64(m.Read):
			level = Runaway
		case float64(s.Read) > sizeOver*float64(m.Read):
			level = Overworking
		}
		if level != "" {
			name := s.Size
			if s.Role == "fixer" {
				name = fixer
			}
			out = append(out, Flag{level, "size", fmt.Sprintf("read %s, %.1f× the %s %s median (%s)",
				Tokens(s.Read), float64(s.Read)/float64(m.Read), scope, name, Tokens(m.Read))})
		}
	}
	if s.Role == "lead" && len(s.Refused) >= refusedOver {
		out = append(out, Flag{Overworking, "refused", refusedWords(s.Refused)})
	}
	if compactSoon > 0 && s.Ctx > int64(compactSoon) {
		out = append(out, Flag{Runaway, "context", fmt.Sprintf("context %s, past compact_soon (%s)",
			Tokens(s.Ctx), Tokens(int64(compactSoon)))})
	}
	switch {
	case s.Hour > burnRunaway:
		out = append(out, Flag{Runaway, "burn", "read " + Tokens(s.Hour) + " in the last hour"})
	case s.Hour > burnOver:
		out = append(out, Flag{Overworking, "burn", "read " + Tokens(s.Hour) + " in the last hour"})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level == Runaway && out[j].Level != Runaway })
	return out
}

// Worst is the worst level among fs: runaway, overworking or "".
func Worst(fs []Flag) string {
	if len(fs) == 0 {
		return ""
	}
	return fs[0].Level
}

// Words joins fs' words, the worst first: what the band and the event say.
func Words(fs []Flag) string {
	ws := make([]string, len(fs))
	for i, f := range fs {
		ws[i] = f.Words
	}
	return strings.Join(ws, "; ")
}

// RuleOf is the rule a flag's words come from ("" for none): what reads
// a runaway event's text back.
func RuleOf(words string) string {
	switch {
	case strings.HasPrefix(words, "land refused "):
		return "refused"
	case strings.HasPrefix(words, "context "):
		return "context"
	case strings.HasPrefix(words, "read ") && strings.HasSuffix(words, " in the last hour"):
		return "burn"
	case strings.HasPrefix(words, "read ") && strings.Contains(words, " median ("):
		return "size"
	}
	return ""
}

// refusalKinds name a land refusal by its text, in the order the words
// list them.
var refusalKinds = []struct{ match, one, many string }{
	{"rebase onto origin/main conflicts", "rebase conflict", "rebase conflicts"},
	{"review failed", "review failed", "reviews failed"},
	{"verify failed", "verify failed", "verifies failed"},
}

// refusedWords says refusals: land refused 5× (3 rebase conflicts, 2 reviews failed).
func refusedWords(refused []string) string {
	counts := make([]int, len(refusalKinds)+1)
	for _, t := range refused {
		i := len(refusalKinds)
		for k, rk := range refusalKinds {
			if strings.Contains(t, rk.match) {
				i = k
				break
			}
		}
		counts[i]++
	}
	var parts []string
	for k, rk := range refusalKinds {
		switch counts[k] {
		case 0:
		case 1:
			parts = append(parts, "1 "+rk.one)
		default:
			parts = append(parts, fmt.Sprintf("%d %s", counts[k], rk.many))
		}
	}
	if n := counts[len(refusalKinds)]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d other", n))
	}
	return fmt.Sprintf("land refused %d× (%s)", len(refused), strings.Join(parts, ", "))
}

// HourRead is what calls read in the 60 minutes up to now.
func HourRead(calls []Call, now time.Time) int64 {
	var n int64
	from := now.Add(-time.Hour)
	for _, c := range calls {
		if at, err := time.Parse(time.RFC3339Nano, c.At); err == nil && !at.Before(from) && !at.After(now) {
			n += c.In + c.CR + c.CW
		}
	}
	return n
}

// SubjectOf is agent a's subject from its calls (the main thread's and its
// subagents', in order), its spec size and land refusals, at now.
func SubjectOf(a Agent, calls []Call, size string, refused []string, now time.Time) Subject {
	return Subject{Agent: a.Agent, Role: a.Role, Repo: a.Repo, Size: size, Read: a.Read(),
		Ctx: a.LastCtx, Hour: HourRead(calls, now), Refused: refused}
}

// Refusals are the land refusals in hist (bm-mesh messages) by the node
// they were addressed to, each one's text, oldest first.
func Refusals(hist []mesh.Stored) map[string][]string {
	out := map[string][]string{}
	for _, m := range hist {
		if !strings.HasPrefix(m.Subject, "bm.daemon.") || !strings.HasSuffix(m.Subject, ".land-refused") {
			continue
		}
		var ev mesh.Event
		if json.Unmarshal(m.Data, &ev) == nil && ev.To != "" {
			out[ev.To] = append(out[ev.To], ev.Text)
		}
	}
	return out
}

// Runs are the done leads and fixers with use in rep: each one's read in
// rep, its spec size (sizeOf) and its wall time from started_at to the
// done state's at (its first call when it has no started_at).
func Runs(rep Report, nodes []mesh.Node, states map[string]mesh.State, sizeOf func(mesh.Node) string) []Run {
	byPath := map[string]Agent{}
	for _, a := range rep.Agents {
		byPath[a.Agent] = a
	}
	var out []Run
	for _, n := range nodes {
		a, used := byPath[n.Path]
		st := states[n.Path]
		if !used || st.State != "done" || n.Role != "lead" && n.Role != "fixer" {
			continue
		}
		size := ""
		if n.Role == "lead" {
			if size = sizeOf(n); !slices.Contains(Sizes, size) {
				continue
			}
		}
		out = append(out, Run{Agent: n.Path, Role: n.Role, Repo: n.Repo, Size: size, Read: a.Read(), Wall: Wall(n, a, st)})
	}
	return out
}

// Wall is a node's wall time in seconds: from its started_at (else its
// first call) to its done state's at, or to its last call while not done.
func Wall(n mesh.Node, a Agent, st mesh.State) int64 {
	from, err := time.Parse(time.RFC3339, n.StartedAt)
	if err != nil {
		if from, err = time.Parse(time.RFC3339Nano, a.First); err != nil {
			return 0
		}
	}
	to, err := time.Parse(time.RFC3339, st.At)
	if st.State != "done" || err != nil {
		if to, err = time.Parse(time.RFC3339Nano, a.Last); err != nil {
			return 0
		}
	}
	return max(0, int64(to.Sub(from).Seconds()))
}

// BaselineRead is what LoadBaselines read: the baselines, the stream's
// last Keep (every agent's use), the nodes and their states.
type BaselineRead struct {
	Baselines Baselines
	Runs      []Run
	Report    Report
	Nodes     []mesh.Node
	States    map[string]mesh.State
}

// LoadBaselines reads the stream's last Keep and the nodes, and builds the
// baselines from the done leads and fixers; sizeOf is a node's spec size.
func LoadBaselines(ctx context.Context, c *mesh.Client, now time.Time, sizeOf func(mesh.Node) string) (BaselineRead, error) {
	rep, err := Read(ctx, c, now.Add(-Keep), Filter{})
	if err != nil {
		return BaselineRead{}, err
	}
	nodes, err := mesh.ListNodes(ctx, c)
	if err != nil {
		return BaselineRead{}, err
	}
	states, err := mesh.ReadAllStates(ctx, c)
	if err != nil {
		return BaselineRead{}, err
	}
	runs := Runs(rep, nodes, states, sizeOf)
	return BaselineRead{Baselines: NewBaselines(runs), Runs: runs, Report: rep, Nodes: nodes, States: states}, nil
}
