package daemon

import (
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// ensureRetry is how long an instance waits before retrying its checkout.
const ensureRetry = 10 * time.Second

// pooledInstance runs one pooled instance until its group stops: it holds
// jobs until batch_idle passes with nothing new, a go arrives or a job with
// go arrives, runs them in order on its warm instance, then runs any job
// queued meanwhile at once before batching again.
func (r *routineSet) pooledInstance(g *group, p *pool, path string, goCh chan struct{}) {
	release := r.claimPath(g.ctx, path)
	if release == nil {
		return
	}
	defer release()
	var dir string
	for {
		var err error
		if dir, err = r.ensureCheckout(g.repoPath, path); err == nil {
			break
		}
		r.d.Log.Warn("routines: checkout", "path", path, "err", err)
		if !sleepCtx(g.ctx, ensureRetry) {
			return
		}
	}
	w := &worker{r: r, g: g, path: path, model: p.model, dir: dir}
	w.begin()
	defer w.end()
	for {
		batch := collect(g, p, path, goCh)
		for i, dl := range batch {
			if g.ctx.Err() != nil {
				for _, rest := range batch[i:] {
					rest.nak()
				}
				return
			}
			w.handle(dl)
		}
		for drained := false; !drained; {
			t := time.NewTimer(drainWait)
			select {
			case dl := <-p.jobs:
				if g.ctx.Err() != nil { // stopped meanwhile: the job goes back
					dl.nak()
					t.Stop()
					return
				}
				w.handle(dl)
			case <-t.C:
				drained = true
			case <-g.ctx.Done():
				t.Stop()
				return
			}
			t.Stop()
		}
		if g.ctx.Err() != nil {
			return
		}
	}
}

// collect holds jobs from p until batch_idle passes after the last one, a
// go arrives (taking whatever already waits in p), or a job with go
// arrives. With nothing held it waits on. It returns nil when the group
// stops, returning what it held to the inbox. What it holds is noted as
// held by path.
func collect(g *group, p *pool, path string, goCh <-chan struct{}) []*delivery {
	select { // a go from before this batch is spent
	case <-goCh:
	default:
	}
	var held []*delivery
	var fire <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-g.ctx.Done():
			for _, dl := range held {
				dl.nak()
			}
			return nil
		case dl := <-p.jobs:
			dl.note(path, "held")
			held = append(held, dl)
			if dl.job.Go {
				return held
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(g.k.BatchIdle)
			fire = timer.C
		case <-goCh:
			for waiting := true; waiting; {
				select {
				case dl := <-p.jobs:
					dl.note(path, "held")
					held = append(held, dl)
				default:
					waiting = false
				}
			}
			if len(held) > 0 {
				return held
			}
		case <-fire:
			return held
		}
	}
}

// freshRun runs one job of a fresh kind as routine.<kind>.f<id8> in a
// checkout of its own, removed afterwards with the instance's node.
func (r *routineSet) freshRun(g *group, dl *delivery, model string) {
	path := "routine." + g.k.Name + ".f" + freshID(dl.job.ID)
	dl.note(path, "held")
	release := r.claimPath(r.ctx, path)
	if release == nil {
		dl.nak()
		return
	}
	defer release()
	dir, err := r.ensureCheckout(g.repoPath, path)
	if err != nil {
		if r.ctx.Err() != nil {
			dl.nak()
			return
		}
		r.reply(dl, mesh.Reply{Output: err.Error(), By: path})
		dl.ack()
		return
	}
	w := &worker{r: r, g: g, path: path, model: model, dir: dir}
	w.begin()
	w.handle(dl)
	w.end()
	if err := r.removeCheckout(g.repoPath, dir); err != nil {
		r.d.Log.Warn("routines: remove checkout", "path", path, "err", err)
		return // the node stays done; the rescan forgets it after keep_done
	}
	// Its events stay in the stream; the node, state and runner sample go.
	ctx, cancel := r.settleCtx()
	defer cancel()
	if err := mesh.ForgetNode(ctx, r.d.Client, path); err != nil {
		r.d.Log.Warn("routines: forget node", "path", path, "err", err)
	}
}

// freshID is the first 8 characters of a job id, lowercased, each one
// outside [a-z0-9] made an x so the path stays valid.
func freshID(id string) string {
	if len(id) > 8 {
		id = id[:8]
	}
	id = strings.Map(func(c rune) rune {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			return c
		}
		return 'x'
	}, strings.ToLower(id))
	if id == "" {
		return "x"
	}
	return id
}
