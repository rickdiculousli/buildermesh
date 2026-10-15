package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
)

// NodeRunner names the runner and thread behind a node.
type NodeRunner struct {
	Kind   string `json:"kind"`
	Thread string `json:"thread"`
}

// Node is one registered participant.
type Node struct {
	Path      string     `json:"path"`
	Role      string     `json:"role"`
	Repo      string     `json:"repo"`
	RepoPath  string     `json:"repo_path"`
	Spec      string     `json:"spec"`
	Branch    string     `json:"branch"`
	Worktree  string     `json:"worktree"`
	Host      string     `json:"host"`
	Runner    NodeRunner `json:"runner"`
	Parent    string     `json:"parent"`
	Model     string     `json:"model"`
	Effort    string     `json:"effort,omitempty"`
	StartedAt string     `json:"started_at"`
}

// RunnerSample is the daemon's last reading of a node's runner.
type RunnerSample struct {
	Running   bool   `json:"running"`
	Known     bool   `json:"known"`
	At        string `json:"at"`
	LastReply string `json:"last_reply"`
}

// RunnerUpdate is one change seen by WatchRunner.
type RunnerUpdate struct {
	Path   string
	Sample RunnerSample
}

func bucket(ctx context.Context, c *Client, name string) (jetstream.KeyValue, error) {
	return c.JS.KeyValue(ctx, name)
}

func put(ctx context.Context, c *Client, name, key string, v any) error {
	kv, err := bucket(ctx, c, name)
	if err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = kv.Put(ctx, key, data)
	return err
}

// get returns false when the key is absent.
func get(ctx context.Context, c *Client, name, key string, v any) (bool, error) {
	kv, err := bucket(ctx, c, name)
	if err != nil {
		return false, err
	}
	e, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(e.Value(), v); err != nil {
		return false, fmt.Errorf("%s %s: %w", name, key, err)
	}
	return true, nil
}

func PutNode(ctx context.Context, c *Client, n Node) error {
	return put(ctx, c, BucketNodes, n.Path, n)
}

func GetNode(ctx context.Context, c *Client, path string) (Node, bool, error) {
	var n Node
	ok, err := get(ctx, c, BucketNodes, path, &n)
	return n, ok, err
}

// ListNodes returns every node sorted by path.
func ListNodes(ctx context.Context, c *Client) ([]Node, error) {
	kv, err := bucket(ctx, c, BucketNodes)
	if err != nil {
		return nil, err
	}
	keys, err := kv.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	out := make([]Node, 0, len(keys))
	for _, k := range keys {
		n, ok, err := GetNode(ctx, c, k)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, n)
		}
	}
	return out, nil
}

// NodeForWorktree finds the node with this worktree on this host. Both
// sides are compared with symlinks resolved, so a node registered through a
// symlinked path matches the real checkout and vice versa.
func NodeForWorktree(ctx context.Context, c *Client, worktree, host string) (Node, bool, error) {
	nodes, err := ListNodes(ctx, c)
	if err != nil {
		return Node{}, false, err
	}
	want := resolve(worktree)
	for _, n := range nodes {
		if n.Host == host && n.Worktree != "" && resolve(n.Worktree) == want {
			return n, true, nil
		}
	}
	return Node{}, false, nil
}

// resolve returns p with symlinks resolved, or p itself when that fails
// (a worktree on another box, or one since removed).
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func PutRunner(ctx context.Context, c *Client, path string, s RunnerSample) error {
	return put(ctx, c, BucketRunner, path, s)
}

func GetRunner(ctx context.Context, c *Client, path string) (RunnerSample, bool, error) {
	var s RunnerSample
	ok, err := get(ctx, c, BucketRunner, path, &s)
	return s, ok, err
}

// WatchRunner streams runner sample changes until stop is called or ctx ends.
func WatchRunner(ctx context.Context, c *Client) (<-chan RunnerUpdate, func()) {
	out := make(chan RunnerUpdate)
	kv, err := bucket(ctx, c, BucketRunner)
	if err != nil {
		close(out)
		return out, func() {}
	}
	w, err := kv.Watch(ctx, ">")
	if err != nil {
		close(out)
		return out, func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case e, ok := <-w.Updates():
				if !ok {
					return
				}
				if e == nil || e.Operation() != jetstream.KeyValuePut {
					continue
				}
				var s RunnerSample
				if json.Unmarshal(e.Value(), &s) != nil {
					continue
				}
				select {
				case out <- RunnerUpdate{Path: e.Key(), Sample: s}:
				case <-done:
					return
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	var once sync.Once
	return out, func() { once.Do(func() { close(done); w.Stop() }) }
}
