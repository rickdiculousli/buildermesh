package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Ref is a reference end state of a case, played offline: a good one
// (Fails empty) must pass every expectation, a bad one must fail the
// expectation Fails names. They are checked before any session runs.
type Ref struct {
	Name  string
	Fails string
	Play  func(r *Rehearsal)
}

// Rehearsal is an offline cell a Ref plays into: the edits, commits, pushes
// and bmesh calls a session might make, and its replies, each turn recorded
// as a live one is. Its first error stops the play.
type Rehearsal struct {
	Dir  string
	Case *Case
	cell *Cell
	err  error
}

func newRehearsal(dir string, k *Case, fixture string) (*Rehearsal, error) {
	arm := Arm{Name: "ref", Commands: []string{"help"}}
	now := time.Now()
	base, err := setupCell(dir, k, arm, fixture, thisBox(), now)
	if err != nil {
		return nil, err
	}
	c := &Cell{Case: k.Name, Arm: "ref", Run: 1, Host: thisBox(), Base: base, Started: now}
	return &Rehearsal{Dir: dir, Case: k, cell: c}, nil
}

func (r *Rehearsal) sandbox() string { return filepath.Join(r.Dir, sandboxDir) }

func (r *Rehearsal) fail(err error) {
	if r.err == nil && err != nil {
		r.err = err
	}
}

// Edit replaces every old in the sandbox's file with new; old must be there.
func (r *Rehearsal) Edit(rel, old, new string) {
	if r.err != nil {
		return
	}
	p := filepath.Join(r.sandbox(), rel)
	b, err := os.ReadFile(p)
	if err != nil {
		r.fail(err)
		return
	}
	if !strings.Contains(string(b), old) {
		r.fail(fmt.Errorf("%s has no %q", rel, old))
		return
	}
	r.fail(os.WriteFile(p, []byte(strings.ReplaceAll(string(b), old, new)), 0o644))
}

// Write writes a file in the sandbox.
func (r *Rehearsal) Write(rel, text string) {
	if r.err == nil {
		r.fail(writeFile(filepath.Join(r.sandbox(), rel), text))
	}
}

// Commit commits everything in the sandbox.
func (r *Rehearsal) Commit(msg string) {
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", msg}} {
		if r.err != nil {
			return
		}
		_, err := git(r.sandbox(), gitEnv(r.Dir), args...)
		r.fail(err)
	}
}

// Push pushes the sandbox's HEAD to origin's main.
func (r *Rehearsal) Push() {
	if r.err == nil {
		_, err := git(r.sandbox(), gitEnv(r.Dir), "push", "-q", "origin", "HEAD:main")
		r.fail(err)
	}
}

// Call logs a bmesh call from the sandbox, as the fake would.
func (r *Rehearsal) Call(args ...string) {
	if r.err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(r.Dir, logFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		r.fail(err)
		return
	}
	_, err = f.WriteString(logLine(r.sandbox(), args...))
	r.fail(err)
	r.fail(f.Close())
}

// Reply ends the turn with the session's reply: the next scripted words,
// or the case's steer once they are all sent.
func (r *Rehearsal) Reply(text string) {
	if r.err != nil {
		return
	}
	c := r.cell
	n := len(c.Turns)
	t := Turn{File: fmt.Sprintf("turns/%02d.json", n+1), Session: "rehearsal"}
	if n < len(r.Case.Turns) {
		t.Words = r.Case.Turns[n]
	} else {
		t.Words, t.Steer = r.Case.Steer, true
		c.Steers++
	}
	b, err := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false,
		"result": text, "session_id": "rehearsal", "num_turns": 1, "total_cost_usd": 0, "duration_ms": 0})
	if err != nil {
		r.fail(err)
		return
	}
	r.fail(os.WriteFile(filepath.Join(r.Dir, t.File), append(b, '\n'), 0o644))
	t.After = snapshot(r.Dir, c.Base)
	c.Turns = append(c.Turns, t)
}

// done saves the cell, so it scores as a live one.
func (r *Rehearsal) done() error {
	if r.err != nil {
		return r.err
	}
	r.cell.Ended = time.Now()
	return r.cell.save(r.Dir)
}

// checkRef plays ref into a fresh cell under root and scores it; nil when
// it scores as it should.
func checkRef(root string, k *Case, ref Ref, fixture string) error {
	dir, err := os.MkdirTemp(root, k.Name+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	r, err := newRehearsal(dir, k, fixture)
	if err != nil {
		return err
	}
	ref.Play(r)
	if err := r.done(); err != nil {
		return fmt.Errorf("playing it: %w", err)
	}
	row, err := scoreCell(dir, k)
	if err != nil {
		return err
	}
	var failed []string
	for _, p := range row.Predicates {
		if !p.OK {
			failed = append(failed, p.Name+" ("+p.Why+")")
		}
	}
	switch {
	case ref.Fails == "" && !row.Pass:
		return fmt.Errorf("a good reference fails %s", strings.Join(failed, "; "))
	case ref.Fails != "" && row.Pass:
		return fmt.Errorf("a bad reference passes; it should fail %s", ref.Fails)
	case ref.Fails != "":
		for _, p := range row.Predicates {
			if p.Name == ref.Fails {
				if p.OK {
					return fmt.Errorf("a bad reference passes %s (%s); it fails %s instead", p.Name, p.Why, strings.Join(failed, "; "))
				}
				return nil
			}
		}
		return fmt.Errorf("the case has no expectation %s", ref.Fails)
	}
	return nil
}

// checkRefs checks every reference of the cases, a few at a time, and
// returns every one that scored wrong.
func checkRefs(cases []*Case, fixture string) error {
	root, err := os.MkdirTemp("", "skilltest-refs-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	type job struct {
		k   *Case
		ref Ref
	}
	var jobs []job
	for _, k := range cases {
		for _, ref := range k.Refs {
			jobs = append(jobs, job{k, ref})
		}
	}
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := checkRef(root, j.k, j.ref, fixture); err != nil {
				errs[i] = fmt.Errorf("%s / %s: %w", j.k.Name, j.ref.Name, err)
			}
		}()
	}
	wg.Wait()
	var msgs []string
	for _, err := range errs {
		if err != nil {
			msgs = append(msgs, err.Error())
		}
	}
	if len(msgs) > 0 {
		return fmt.Errorf("%s", strings.Join(msgs, "\n"))
	}
	return nil
}
