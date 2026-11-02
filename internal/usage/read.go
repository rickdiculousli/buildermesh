package usage

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// Kinds are the four token counts.
type Kinds struct {
	In  int64 `json:"in"`
	CR  int64 `json:"cr"`
	CW  int64 `json:"cw"`
	Out int64 `json:"out"`
}

// Read is what the calls read: in + cr + cw.
func (k Kinds) Read() int64 { return k.In + k.CR + k.CW }

func (k *Kinds) add(c Call) { k.In += c.In; k.CR += c.CR; k.CW += c.CW; k.Out += c.Out }

// Sub is one subagent's use.
type Sub struct {
	ID    string `json:"id"`
	Desc  string `json:"desc,omitempty"`
	Calls int    `json:"calls"`
	Kinds
}

// Agent is one agent's use in a window.
type Agent struct {
	Agent       string   `json:"agent"`
	Role        string   `json:"role"`
	Host        string   `json:"host"`
	Repo        string   `json:"repo,omitempty"`
	Models      []string `json:"models"`
	First       string   `json:"first"`
	Last        string   `json:"last"`
	Calls       int      `json:"calls"`
	Main        Kinds    `json:"main"`
	Sub         Kinds    `json:"sub"`
	Design      *Kinds   `json:"design,omitempty"` // a coordinator's, main and subagents
	Exec        *Kinds   `json:"exec,omitempty"`
	Peak        int64    `json:"peak"`     // the main thread's largest ctx
	LastCtx     int64    `json:"last_ctx"` // and its newest
	Compactions int      `json:"compactions"`
	Subs        []Sub    `json:"subs,omitempty"`
}

// Read is the agent's read, main and subagents.
func (a Agent) Read() int64 { return a.Main.Read() + a.Sub.Read() }

// Filter narrows a Read: Agent is a node's path or a session agent
// (session.<host>.<id>, or session.<id>, either id whole or its first 8);
// Repo keeps one repo's agents.
type Filter struct {
	Agent string
	Repo  string
}

// Report is a Read's answer: per agent, most read first, and with an agent
// named, its calls and marks in order.
type Report struct {
	Since  time.Time `json:"since"`
	Agents []Agent   `json:"agents"`
	Calls  []Call    `json:"calls,omitempty"`
	Marks  []Mark    `json:"marks,omitempty"`
	// ByAgent is every agent's calls in order, main and subagents, for the map.
	ByAgent map[string][]Call `json:"-"`
}

// ErrNoStream is a hub with no bm-usage: no daemon of 105 or later serves it.
var ErrNoStream = errors.New("the hub has no " + mesh.StreamUsage + " stream: its daemon predates bmesh usage")

// Read sums the calls at or after since, each (agent, id) once, per agent.
// A call is published after it happened, so reading the stream from since
// misses nothing. Node roles come from bm-nodes.
func Read(ctx context.Context, c *mesh.Client, since time.Time, f Filter) (Report, error) {
	rep := Report{Since: since, Agents: []Agent{}, ByAgent: map[string][]Call{}}
	filters := []string{"bmusage.node.>", "bmusage.session.>"}
	switch {
	case strings.HasPrefix(f.Agent, "session."):
		filters = filters[1:]
	case f.Agent != "":
		filters = []string{subject(f.Agent)}
	}
	var msgs []Message
	err := fetch(ctx, c, since, filters, func(data []byte) {
		var m Message
		if json.Unmarshal(data, &m) == nil { // a message it can't read counts as nothing
			msgs = append(msgs, m)
		}
	})
	if err != nil {
		return rep, err
	}
	roles := map[string]string{}
	if nodes, err := mesh.ListNodes(ctx, c); err == nil {
		for _, n := range nodes {
			roles[n.Path] = n.Role
		}
	}
	type acc struct {
		a     *Agent
		subs  map[string]*Sub
		calls []Call
		marks []Mark
	}
	by := map[string]*acc{}
	seen := map[[2]string]bool{}
	cutoff := since.UTC()
	for _, m := range msgs {
		if f.Repo != "" && m.Repo != f.Repo || f.Agent != "" && !named(f.Agent, m.Agent) {
			continue
		}
		x := by[m.Agent]
		if x == nil {
			x = &acc{a: &Agent{Agent: m.Agent, Role: m.Role, Host: m.Host, Repo: m.Repo, Models: []string{}}, subs: map[string]*Sub{}}
			if r := roles[m.Agent]; r != "" {
				x.a.Role = r
			}
			by[m.Agent] = x
		}
		for _, k := range m.Marks {
			if at, err := time.Parse(time.RFC3339Nano, k.At); err != nil || at.Before(cutoff) || seen[[2]string{m.Agent, "mark " + k.Sub + k.At}] {
				continue
			}
			seen[[2]string{m.Agent, "mark " + k.Sub + k.At}] = true
			if k.Sub == "" {
				x.a.Compactions++
			}
			x.marks = append(x.marks, k)
		}
		for _, cl := range m.Calls {
			at, err := time.Parse(time.RFC3339Nano, cl.At)
			if err != nil || at.Before(cutoff) || seen[[2]string{m.Agent, cl.ID}] {
				continue
			}
			seen[[2]string{m.Agent, cl.ID}] = true
			cl.Sub = m.Sub
			x.calls = append(x.calls, cl)
			if m.Sub != "" {
				s := x.subs[m.Sub]
				if s == nil {
					s = &Sub{ID: m.Sub, Desc: m.Desc}
					x.subs[m.Sub] = s
				}
				s.Calls++
				s.add(cl)
			}
		}
	}
	for _, x := range by {
		a := x.a
		if len(x.calls) == 0 && len(x.marks) == 0 {
			continue
		}
		sortCalls(x.calls)
		for _, cl := range x.calls {
			a.Calls++
			if a.First == "" {
				a.First = cl.At
			}
			a.Last = cl.At
			if cl.Model != "" && !slices.Contains(a.Models, cl.Model) {
				a.Models = append(a.Models, cl.Model)
			}
			if cl.Sub == "" {
				a.Main.add(cl)
				a.Peak, a.LastCtx = max(a.Peak, cl.Ctx), cl.Ctx
			} else {
				a.Sub.add(cl)
			}
			switch cl.Phase {
			case Design:
				a.Design = cmpKinds(a.Design)
				a.Design.add(cl)
			case Exec:
				a.Exec = cmpKinds(a.Exec)
				a.Exec.add(cl)
			}
		}
		if a.Role == Coordinator {
			a.Design, a.Exec = cmpKinds(a.Design), cmpKinds(a.Exec)
		}
		sort.Strings(a.Models)
		for _, s := range x.subs {
			a.Subs = append(a.Subs, *s)
		}
		sort.Slice(a.Subs, func(i, j int) bool { return a.Subs[i].Read() > a.Subs[j].Read() })
		if a.Calls > 0 {
			rep.Agents = append(rep.Agents, *a)
			rep.ByAgent[a.Agent] = x.calls
		}
		if f.Agent != "" {
			rep.Calls = append(rep.Calls, x.calls...)
			rep.Marks = append(rep.Marks, x.marks...)
		}
	}
	sort.Slice(rep.Agents, func(i, j int) bool {
		if ri, rj := rep.Agents[i].Read(), rep.Agents[j].Read(); ri != rj {
			return ri > rj
		}
		return rep.Agents[i].Agent < rep.Agents[j].Agent
	})
	sortCalls(rep.Calls)
	sort.SliceStable(rep.Marks, func(i, j int) bool { return rep.Marks[i].At < rep.Marks[j].At })
	return rep, nil
}

func cmpKinds(k *Kinds) *Kinds {
	if k == nil {
		return &Kinds{}
	}
	return k
}

func sortCalls(cs []Call) {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].At < cs[j].At })
}

// named reports whether filter names agent: a node's path exactly, or a
// session by its whole id or first 8, with or without its host.
func named(filter, agent string) bool {
	if filter == agent {
		return true
	}
	want, ok := strings.CutPrefix(filter, "session.")
	rest, isSession := strings.CutPrefix(agent, "session.")
	if !ok || !isSession {
		return false
	}
	host, id, _ := strings.Cut(rest, ".")
	if h, i, found := strings.Cut(want, "."); found {
		if h != host {
			return false
		}
		want = i
	}
	return len(want) >= 8 && strings.HasPrefix(id, want)
}

// fetch hands each of the stream's messages on filters from since to each,
// oldest first, up to what the stream held when the read began.
func fetch(ctx context.Context, c *mesh.Client, since time.Time, filters []string, each func([]byte)) error {
	st, err := c.JS.Stream(ctx, mesh.StreamUsage)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		return ErrNoStream
	}
	if err != nil {
		return err
	}
	info, err := st.Info(ctx)
	if err != nil {
		return err
	}
	if info.State.Msgs == 0 || info.State.LastTime.Before(since) {
		return nil
	}
	last := info.State.LastSeq
	cons, err := c.JS.OrderedConsumer(ctx, mesh.StreamUsage, jetstream.OrderedConsumerConfig{
		FilterSubjects: filters, DeliverPolicy: jetstream.DeliverByStartTimePolicy, OptStartTime: &since,
	})
	if err != nil {
		return err
	}
	for {
		m, err := cons.Next(jetstream.FetchMaxWait(2 * time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, jetstream.ErrNoMessages) || errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}
		meta, err := m.Metadata()
		if err != nil {
			return err
		}
		each(m.Data())
		if meta.NumPending == 0 || meta.Sequence.Stream >= last {
			return nil
		}
	}
}

// quotaSlot is the span ReadQuota keeps one reading per provider in: the
// strip's poll period, so two boxes polling count once.
const quotaSlot = 5 * 60

// ReadQuota reads the quota readings from since, keeping each provider's
// newest per 5-minute slot across boxes, ordered by read_at.
func ReadQuota(ctx context.Context, c *mesh.Client, since time.Time) ([]QuotaReading, error) {
	type key struct {
		provider string
		slot     int64
	}
	newest := map[key]QuotaReading{}
	at := map[key]time.Time{}
	err := fetch(ctx, c, since, []string{"bmusage.quota.>"}, func(data []byte) {
		var r QuotaReading
		if json.Unmarshal(data, &r) != nil {
			return
		}
		t, err := time.Parse(time.RFC3339, r.ReadAt)
		if err != nil || t.Before(since) {
			return
		}
		k := key{r.Provider, t.Unix() / quotaSlot}
		if old, ok := at[k]; !ok || !t.Before(old) {
			newest[k], at[k] = r, t
		}
	})
	out := make([]QuotaReading, 0, len(newest))
	for _, r := range newest {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ReadAt != out[j].ReadAt {
			return out[i].ReadAt < out[j].ReadAt
		}
		return out[i].Provider < out[j].Provider
	})
	return out, err
}

// PublishQuota puts rs on bmusage.quota.<host>, each once per provider and
// read_at; a host that is no subject token publishes nothing.
func PublishQuota(ctx context.Context, c *mesh.Client, host string, rs []QuotaReading) error {
	if !token.MatchString(host) {
		return nil
	}
	for _, r := range rs {
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := c.JS.Publish(ctx, "bmusage.quota."+host, data,
			jetstream.WithMsgID(host+"/quota/"+r.Provider+"/"+r.ReadAt), jetstream.WithExpectStream(mesh.StreamUsage)); err != nil {
			return err
		}
	}
	return nil
}
