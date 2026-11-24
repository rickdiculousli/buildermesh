package daemon

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/hoststat"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// The sampler answers live requests on bmhosts.load.<host> from its first
// read and publishes each later minute to bmusage.host.<host>.
func TestHostStatAnswersAndPublishes(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("no reader on " + runtime.GOOS)
	}
	old := hostStatEvery
	hostStatEvery = 300 * time.Millisecond
	t.Cleanup(func() { hostStatEvery = old })
	f := setup(t)
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan struct{})
	go func() { hostStatRun(ctx, f.d); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	var l *hoststat.Live
	meshtest.Until(t, "a live answer", func() bool {
		msg, err := f.c.NC.Request(hoststat.LoadSubject(testHost), nil, 2*time.Second)
		if err != nil {
			return false
		}
		l = nil
		return json.Unmarshal(msg.Data, &l) == nil && l != nil
	})
	if l.Host != testHost || l.Cores < 1 || l.MemTotal <= 0 || len(l.Procs) == 0 || l.Totals[hoststat.Other].Procs == 0 {
		t.Fatalf("%+v", l)
	}
	meshtest.Until(t, "a published minute", func() bool {
		ms, err := hoststat.ReadHistory(f.ctx, f.c, time.Now().Add(-time.Hour))
		return err == nil && len(ms) > 0 && ms[0].Host == testHost && ms[0].Agents[hoststat.Other].RSS >= 0
	})
}
