// Package usage puts every agent's token use on the mesh, per model call
// (105): each box's daemon reads the Claude Code transcripts on its disk,
// decides once per file which agent it is, and publishes the new calls to
// the bm-usage stream; Read sums them per agent for `bmesh usage` and the
// map. Unknown transcript lines count as nothing: a missing number beats a
// wrong one.
package usage

import (
	"regexp"
	"strings"
)

// Call is one model call (one message id), from the first transcript line
// that carries it.
type Call struct {
	At    string `json:"at"`
	ID    string `json:"id"`
	Model string `json:"model,omitempty"`
	In    int64  `json:"in"`              // input tokens
	CR    int64  `json:"cr"`              // cache read
	CW    int64  `json:"cw"`              // cache write
	Out   int64  `json:"out"`             // output tokens
	Ctx   int64  `json:"ctx"`             // in + cr + cw: the context the call read
	Sub   string `json:"sub,omitempty"`   // the subagent's id, "" on the main thread
	Phase string `json:"phase,omitempty"` // design or exec for a coordinator, "" for a node
	Cause string `json:"cause,omitempty"` // what fed its growth: the previous message's tool uses, message, wake or compact
}

// Mark is a compaction: kind "compact".
type Mark struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
	Sub  string `json:"sub,omitempty"`
}

// Message is what one transcript file's pass publishes: its head and the
// file's new calls and marks, at most maxCalls calls.
type Message struct {
	Agent   string `json:"agent"` // a node's path, or session.<host>.<session id>
	Role    string `json:"role"`
	Host    string `json:"host"`
	Repo    string `json:"repo,omitempty"`
	Session string `json:"session"`
	File    string `json:"file"`           // the transcript, under <claude config dir>/projects
	Sub     string `json:"sub,omitempty"`  // a subagent file's id
	Desc    string `json:"desc,omitempty"` // and the Agent call's description
	Calls   []Call `json:"calls,omitempty"`
	Marks   []Mark `json:"marks,omitempty"`
}

// maxCalls is the most calls one message carries; a var for the tests.
var maxCalls = 500

// Coordinator is the role of a session no node owns.
const Coordinator = "coordinator"

// token is what a subject token may be.
var token = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// SessionAgent names a coordinator session: session.<host>.<id>.
func SessionAgent(host, id string) string { return "session." + host + "." + id }

// subject is where m's agent publishes: bmusage.node.<path>, or
// bmusage.session.<host>.<id>; "" when its names can't make one.
func subject(agent string) string {
	if rest, ok := strings.CutPrefix(agent, "session."); ok {
		host, id, _ := strings.Cut(rest, ".")
		if !token.MatchString(host) || !token.MatchString(id) {
			return ""
		}
		return "bmusage.session." + host + "." + id
	}
	return "bmusage.node." + agent
}

// Short is how an agent prints: a session as session.<host>.<first 8>.
func Short(agent string) string {
	if rest, ok := strings.CutPrefix(agent, "session."); ok {
		host, id, _ := strings.Cut(rest, ".")
		return "session." + host + "." + id[:min(8, len(id))]
	}
	return agent
}

// QuotaReading is one provider's quota as the strip shows it and the stream
// keeps it; ReadAt is when the command that produced it ran.
type QuotaReading struct {
	Provider  string  `json:"provider"`
	FiveHour  float64 `json:"five_hour"`
	Week      float64 `json:"week"`
	FiveReset string  `json:"five_reset"`
	WeekReset string  `json:"week_reset"`
	ReadAt    string  `json:"read_at"`
}
