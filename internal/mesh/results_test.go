package mesh_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	. "github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestResultKeyWithAndWithoutArgs(t *testing.T) {
	if got := ResultKey("verify", "3f2a9c1e", nil, ""); got != "verify.3f2a9c1e" {
		t.Fatalf("no args: %q", got)
	}
	sum := sha256.Sum256([]byte("a\x00b c"))
	want := "verify.3f2a9c1e." + hex.EncodeToString(sum[:])[:8]
	if got := ResultKey("verify", "3f2a9c1e", []string{"a", "b c"}, ""); got != want {
		t.Fatalf("args: %q, want %q", got, want)
	}
	if ResultKey("verify", "t", []string{"a b"}, "") == ResultKey("verify", "t", []string{"a", "b"}, "") {
		t.Fatal("args joined ambiguously")
	}
	if got := ResultKey("review", "t", nil, "claude-opus-5-5"); got != "review.t.opus-5-5" {
		t.Fatalf("model: %q", got)
	}
	sum = sha256.Sum256([]byte("x"))
	if got, want := ResultKey("review", "t", []string{"x"}, "claude-opus-5-5"), "review.t."+hex.EncodeToString(sum[:])[:8]+".opus-5-5"; got != want {
		t.Fatalf("args and model: %q, want %q", got, want)
	}
	for in, want := range map[string]string{
		"claude-Sonnet_5.5[1m]": "sonnet-5-5-1m-",
		"Claude-Opus-5-5":       "opus-5-5",
		"claude-":               "",
	} {
		if got := ModelShort(in); got != want {
			t.Fatalf("ModelShort(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ResultKey("review", "t", nil, "claude-"); got != "review.t" {
		t.Fatalf("empty model short: %q", got)
	}

	c, ctx := connect(t)
	r := Result{Kind: "verify", Tree: "t1", Commit: "c1", OK: true, Output: "fine", By: "host"}
	if err := PutResult(ctx, c, r); err != nil {
		t.Fatal(err)
	}
	if err := PutResult(ctx, c, Result{Kind: "verify", Tree: "t2", Args: []string{"x"}, By: "host"}); err != nil {
		t.Fatal(err)
	}
	if err := PutResult(ctx, c, Result{Kind: "review", Tree: "t1", By: "host"}); err != nil {
		t.Fatal(err)
	}
	if err := PutResult(ctx, c, Result{Kind: "review", Tree: "t1", By: "host", Model: "claude-opus-5-5"}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := GetResult(ctx, c, "verify.t1")
	if err != nil || !ok || !got.OK || got.Output != "fine" || got.At == "" {
		t.Fatalf("get: %+v %v %v", got, ok, err)
	}
	kv, err := c.JS.KeyValue(ctx, BucketResults)
	if err != nil {
		t.Fatal(err)
	}
	if e, err := kv.Get(ctx, "verify.t1"); err != nil || !strings.Contains(string(e.Value()), `"args":[]`) {
		t.Fatalf("args not stored as []: %v", err)
	}
	if _, ok, err := GetResult(ctx, c, "verify.none"); ok || err != nil {
		t.Fatalf("absent: %v %v", ok, err)
	}
	list, err := ListResults(ctx, c, "verify")
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if list, err := ListResults(ctx, c, "review"); err != nil || len(list) != 2 ||
		list[0].Model != "" || list[1].Model != "claude-opus-5-5" {
		t.Fatalf("review list: %+v %v", list, err)
	}
	if list, err := ListResults(ctx, c, "lint"); err != nil || len(list) != 0 {
		t.Fatalf("empty list: %+v %v", list, err)
	}
}
