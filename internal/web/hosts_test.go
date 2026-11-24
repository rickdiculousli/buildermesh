package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
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
// minute, a box that never answers (mac), one not recent (old) and one
// with a presence entry and no minutes (win, asked, no responder) read null;
// the agents table gains each agent's mean cpu and peak memory.
func TestHostsHistoryLiveAndAgentColumns(t *testing.T) {
	srv, ctx, c := usageMesh(t) // this box is k16, its clock usageNow
	pub := func(m *hoststat.Minute) {
		t.Helper()
		if err := hoststat.Publish(ctx, c, m); err != nil {
			t.Fatal(err)
		}
	}
	lead := "buildermesh-096"
	for i, cpu := range []float64{2, 4} { // 12:08 and 12:09, a third of its CPU work queued
		m := hostMinute("k16", "linux", usageNow.Add(time.Duration(i-2)*time.Minute), cpu,
			map[string]hoststat.Use{lead: {CPU: cpu / 2, RSS: int64(1+i) << 30}, hoststat.Other: {CPU: cpu / 2, RSS: 1 << 30}})
		m.Run, m.Wait = f64(30), f64(float64(1+14*i))
		if i == 1 {
			m.Agents[lead] = hoststat.Use{CPU: 2, RSS: 2 << 30, Run: f64(10), Wait: f64(5)}
		}
		pub(m)
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

	// win publishes no minutes (Windows, or a release before the sampler):
	// its presence entry alone gives it a card.
	kv, err := c.JS.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: hosts.BucketHosts})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kv.Put(ctx, "win", []byte(`{"host":"win","os":"windows"}`)); err != nil {
		t.Fatal(err)
	}

	var v HostsView
	raw := getJSON(t, srv, "/api/hosts?since=1h", 200, &v)
	if strings.Contains(raw, `"live"`) {
		t.Fatalf("no live=1: want no live answers: %s", raw)
	}
	if v.Step != 60 || v.Host != "k16" || len(v.Boxes) != 3 || v.Boxes[0].Host != "k16" || v.Boxes[1].Host != "mac" ||
		v.Boxes[2].Host != "win" || v.Boxes[2].OS != "windows" || v.Boxes[2].Last != nil || len(v.Boxes[2].Points) != 0 {
		t.Fatalf("%+v", v)
	}
	k := v.Boxes[0]
	if len(k.Points) != 2 || k.Points[1].CPU != 4 || k.Points[1].Agents[lead].CPU != 2 || *k.Points[1].Pressure.CPU != 4 ||
		k.Points[1].Pressure.IO != nil || k.Last.CPU != 4 || *k.Points[0].Wait != 1 || *k.Points[1].Wait != 15 {
		t.Fatalf("k16 %+v", k)
	}
	getJSON(t, srv, "/api/hosts?since=6h", 200, &v)
	if len(v.Boxes) != 4 || v.Step != 600 {
		t.Fatalf("6h: %+v", v)
	}
	// 6h buckets of 10 minutes: 12:08 and 12:09 share one, its means.
	// Run and wait are the means over the minutes that have them, an
	// agent's over its own.
	if p := v.Boxes[0].Points; len(p) != 1 || p[0].N != 2 || p[0].CPU != 3 || p[0].Agents[lead].RSS != 3<<29 ||
		*p[0].Run != 30 || *p[0].Wait != 8 || *p[0].Agents[lead].Run != 10 || *p[0].Agents[lead].Wait != 5 ||
		p[0].Agents[hoststat.Other].Run != nil {
		t.Fatalf("6h k16 %+v", v.Boxes[0].Points)
	}
	if p := v.Boxes[1].Points; len(p) != 1 || *p[0].MacPressure != 2 || p[0].Pressure != nil || p[0].Run != nil {
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
	if strings.Count(raw, `"live":`) != 4 || lv.Boxes[0].Live == nil || len(lv.Boxes[0].Live.Procs) != 1 ||
		lv.Boxes[0].Live.Procs[0].Agent != lead || lv.Boxes[1].Live != nil || lv.Boxes[2].Live != nil || lv.Boxes[3].Live != nil {
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
