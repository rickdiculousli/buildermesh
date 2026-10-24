package mesh_test

import (
	"reflect"
	"testing"

	. "github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestPutGetListNodes(t *testing.T) {
	c, ctx := connect(t)
	b := Node{Path: "b", Role: "lead", Worktree: "/w/b", Host: "h1", Runner: NodeRunner{Kind: "t3", Thread: "x"}}
	a := Node{Path: "a", Role: "lead", Worktree: "/w/a", Host: "h1"}
	for _, n := range []Node{b, a} {
		if err := PutNode(ctx, c, n); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := GetNode(ctx, c, "b")
	if err != nil || !ok || !reflect.DeepEqual(got, b) {
		t.Fatal(got, ok, err)
	}
	if _, ok, _ := GetNode(ctx, c, "zz"); ok {
		t.Fatal("zz found")
	}
	l, err := ListNodes(ctx, c)
	if err != nil || len(l) != 2 || l[0].Path != "a" || l[1].Path != "b" {
		t.Fatal(l, err)
	}
	if n, ok, _ := NodeForWorktree(ctx, c, "/w/a", "h1"); !ok || n.Path != "a" {
		t.Fatal(n, ok)
	}
	if _, ok, _ := NodeForWorktree(ctx, c, "/w/a", "h2"); ok {
		t.Fatal("other host matched")
	}
}

func TestRunnerSamplesAndWatch(t *testing.T) {
	c, ctx := connect(t)
	if _, ok, _ := GetRunner(ctx, c, "a"); ok {
		t.Fatal("unexpected sample")
	}
	if err := PutRunner(ctx, c, "a", RunnerSample{Running: true, Known: true, At: "t", LastReply: "hi"}); err != nil {
		t.Fatal(err)
	}
	s, ok, err := GetRunner(ctx, c, "a")
	if err != nil || !ok || !s.Running || s.LastReply != "hi" {
		t.Fatal(s, ok, err)
	}
	ch, stop := WatchRunner(ctx, c)
	u := <-ch
	if u.Path != "a" || !u.Sample.Running {
		t.Fatal(u)
	}
	if err := PutRunner(ctx, c, "b", RunnerSample{Known: true}); err != nil {
		t.Fatal(err)
	}
	if u := <-ch; u.Path != "b" {
		t.Fatal(u)
	}
	stop()
	for range ch {
	}
}
