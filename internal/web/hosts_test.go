package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/rickdiculousli/buildermesh/internal/hoststat"
)

func f64(v float64) *float64 { return &v }

// hostMinute is a box's minute at t, with agents' cpu and rss.
func hostMinute(host, os string, t time.Time, cpu float64, agents map[string]hoststat.Use) *hoststat.Minute {
	m := &hoststat.Minute{Host: host, OS: os, At: t.UTC().Format(time.RFC3339), Cores: 16, CPU: cpu, Load1: cpu,
		MemUsed: 10 << 30, MemTotal: 32 << 30, Agents: agents}
	if os == "linux" {
		m.Pressure = &hoststat.Pressure{CPU: f64(cpu), Mem: f64(0), IO: nil}
	} else {
		lvl := 2
		m.MacPressure = &lvl
	}
	return m
}

// /api/hosts draws each box's minutes in buckets of the window's step,
// this box first; with live=1 it asks this box and the boxes with a recent
// minute, a box that never answers (mac) and one not recent (old) read
// null; the agents table gains each agent's mean cpu and peak memory.
func TestHostsHistoryLiveAndAgentColumns(t *testing.T) {
	srv, ctx, c := usageMesh(t) // this box is k16, its clock usageNow
	pub := func(m *hoststat.Minute) {
		t.Helper()
		if err := hoststat.Publish(ctx, c, m); err != nil {
			t.Fatal(err)
		}
	}
	lead := "buildermesh-096"
	for i, cpu := range []float64{2, 4} { // 12:08 and 12:09
		pub(hostMinute("k16", "linux", usageNow.Add(time.Duration(i-2)*time.Minute), cpu,
			map[string]hoststat.Use{lead: {CPU: cpu / 2, RSS: int64(1+i) << 30}, hoststat.Other: {CPU: cpu / 2, RSS: 1 << 30}}))
	}
	pub(hostMinute("mac", "darwin", usageNow.Add(-time.Minute), 1, map[string]hoststat.Use{lead: {CPU: 0.5, RSS: 1 << 30}}))
	pub(hostMinute("old", "linux", usageNow.Add(-2*time.Hour), 1, map[string]hoststat.Use{}))
	sub, err := c.NC.Subscribe(hoststat.LoadSubject("k16"), func(m *nats.Msg) {
		l := hoststat.Live{Minute: *hostMinute("k16", "linux", usageNow, 3, map[string]hoststat.Use{}),
			Procs: []hoststat.Proc{{PID: 10, Comm: "java", CPU: 2.5, RSS: 1 << 30, Agent: lead}}}
		b, _ := json.Marshal(l)
		_ = m.Respond(b)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()

	var v HostsView
	raw := getJSON(t, srv, "/api/hosts?since=1h", 200, &v)
	if strings.Contains(raw, `"live"`) {
		t.Fatalf("no live=1: want no live answers: %s", raw)
	}
	if v.Step != 60 || v.Host != "k16" || len(v.Boxes) != 2 || v.Boxes[0].Host != "k16" || v.Boxes[1].Host != "mac" {
		t.Fatalf("%+v", v)
	}
	k := v.Boxes[0]
	if len(k.Points) != 2 || k.Points[1].CPU != 4 || k.Points[1].Agents[lead].CPU != 2 || *k.Points[1].Pressure.CPU != 4 ||
		k.Points[1].Pressure.IO != nil || k.Last.CPU != 4 {
		t.Fatalf("k16 %+v", k)
	}
	getJSON(t, srv, "/api/hosts?since=6h", 200, &v)
	if len(v.Boxes) != 3 || v.Step != 600 {
		t.Fatalf("6h: %+v", v)
	}
	// 6h buckets of 10 minutes: 12:08 and 12:09 share one, its means.
	if p := v.Boxes[0].Points; len(p) != 1 || p[0].N != 2 || p[0].CPU != 3 || p[0].Agents[lead].RSS != 3<<29 {
		t.Fatalf("6h k16 %+v", v.Boxes[0].Points)
	}
	if p := v.Boxes[1].Points; len(p) != 1 || *p[0].MacPressure != 2 || p[0].Pressure != nil {
		t.Fatalf("6h mac %+v", p)
	}

	start := time.Now()
	var lv struct {
		Boxes []struct {
			Host string         `json:"host"`
			Live *hoststat.Live `json:"live"`
		} `json:"boxes"`
	}
	raw = getJSON(t, srv, "/api/hosts?since=6h&live=1", 200, &lv)
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("live=1 took %v", took)
	}
	if strings.Count(raw, `"live":`) != 3 || lv.Boxes[0].Live == nil || len(lv.Boxes[0].Live.Procs) != 1 ||
		lv.Boxes[0].Live.Procs[0].Agent != lead || lv.Boxes[1].Live != nil || lv.Boxes[2].Live != nil {
		t.Fatalf("live: %s", raw)
	}

	var u UsageView
	getJSON(t, srv, "/api/usage?since=1h", 200, &u)
	for _, a := range u.Agents {
		switch a.Agent {
		case lead:
			// 12:08 1 core, 12:09 2 on k16 and 0.5 on mac: a mean of 1.75;
			// memory peaks at 12:09, 2 GB and 1 GB.
			if a.CPU == nil || *a.CPU != 1.75 || a.PeakMem == nil || *a.PeakMem != 3<<30 {
				t.Fatalf("%s: cpu %v peak %v", a.Agent, a.CPU, a.PeakMem)
			}
		default:
			if a.CPU != nil || a.PeakMem != nil {
				t.Fatalf("%s: cpu %v peak %v, want none", a.Agent, *a.CPU, *a.PeakMem)
			}
		}
	}
}
