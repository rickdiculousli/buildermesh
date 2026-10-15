package mesh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Result is the record of one job of a kind run against a tree.
type Result struct {
	Kind   string   `json:"kind"`
	Tree   string   `json:"tree"`
	Commit string   `json:"commit"`
	Args   []string `json:"args"`
	OK     bool     `json:"ok"`
	Output string   `json:"output"`
	At     string   `json:"at"`
	By     string   `json:"by"`
	Model  string   `json:"model,omitempty"`
}

// ResultKey is <kind>.<tree>, with "." and the first 8 hex digits of the
// sha256 of the args joined by NUL appended when there are args, then "."
// and the model's ModelShort when that is not empty.
func ResultKey(kind, tree string, args []string, model string) string {
	key := kind + "." + tree
	if len(args) > 0 {
		sum := sha256.Sum256([]byte(strings.Join(args, "\x00")))
		key += "." + hex.EncodeToString(sum[:])[:8]
	}
	if short := ModelShort(model); short != "" {
		key += "." + short
	}
	return key
}

// PutResult stores r under its key, filling At when empty; nil Args are
// stored as [].
func PutResult(ctx context.Context, c *Client, r Result) error {
	if r.Args == nil {
		r.Args = []string{}
	}
	if r.At == "" {
		r.At = time.Now().UTC().Format(time.RFC3339)
	}
	return put(ctx, c, BucketResults, ResultKey(r.Kind, r.Tree, r.Args, r.Model), r)
}

// GetResult returns false when no result is stored under key.
func GetResult(ctx context.Context, c *Client, key string) (Result, bool, error) {
	var r Result
	ok, err := get(ctx, c, BucketResults, key, &r)
	return r, ok, err
}

// ListResults returns every result of kind, sorted by key.
func ListResults(ctx context.Context, c *Client, kind string) ([]Result, error) {
	kv, err := bucket(ctx, c, BucketResults)
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
	var out []Result
	for _, k := range keys {
		if !strings.HasPrefix(k, kind+".") {
			continue
		}
		r, ok, err := GetResult(ctx, c, k)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, r)
		}
	}
	return out, nil
}
