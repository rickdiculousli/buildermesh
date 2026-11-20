package mesh_test

import (
	"fmt"
	"testing"
	"time"

	. "github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestListNodesInOneReadSkipsForgotten(t *testing.T) {
	c, ctx := connect(t)
	for _, p := range []string{"c", "a", "b"} {
		if err := PutNode(ctx, c, Node{Path: p, Role: "lead", Host: "h1"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ForgetNode(ctx, c, "b"); err != nil {
		t.Fatal(err)
	}
	if _, ok := LastValues(ctx, c, BucketNodes); !ok {
		t.Fatal("the one-request read was not answered whole")
	}
	l, err := ListNodes(ctx, c)
	if err != nil || len(l) != 2 || l[0].Path != "a" || l[1].Path != "c" {
		t.Fatal(l, err)
	}
}

func TestListNodesPastOneBatch(t *testing.T) {
	c, ctx := connect(t)
	const n = 1030 // past the server's 1024 answers to one multi_last read
	for i := range n {
		if err := PutNode(ctx, c, Node{Path: fmt.Sprintf("n%04d", i), Role: "lead"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := LastValues(ctx, c, BucketNodes); ok {
		t.Fatal("a read past the server's limit claimed to be whole")
	}
	l, err := ListNodes(ctx, c)
	if err != nil || len(l) != n || l[0].Path != "n0000" || l[n-1].Path != fmt.Sprintf("n%04d", n-1) {
		t.Fatal(len(l), err)
	}
}

func TestReadAllStatesInOneRead(t *testing.T) {
	c, ctx := connect(t)
	if s, err := ReadAllStates(ctx, c); err != nil || len(s) != 0 {
		t.Fatal(s, err)
	}
	at := time.Now().UTC().Format(time.RFC3339)
	for _, p := range []string{"a", "b"} {
		if _, err := WriteState(ctx, c, p, State{State: "working", At: at}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := ReadAllStates(ctx, c)
	if err != nil || len(s) != 2 || s["a"].State != "working" || s["b"].State != "working" {
		t.Fatal(s, err)
	}
}
