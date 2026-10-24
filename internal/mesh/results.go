package mesh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Result is the record of one job of a kind run against a tree. PatchID is
// the `git patch-id --verbatim` of the diff a model was asked to read (the job
// text's `base: <sha>` to the commit), so a review carries to another tree
// with the same diff.
type Result struct {
	Kind    string   `json:"kind"`
	Tree    string   `json:"tree"`
	Commit  string   `json:"commit"`
	Args    []string `json:"args"`
	OK      bool     `json:"ok"`
	Output  string   `json:"output"`
	At      string   `json:"at"`
	By      string   `json:"by"`
	Model   string   `json:"model,omitempty"`
	PatchID string   `json:"patch_id,omitempty"`
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

// CarriedPrefix leads the output of a result stored for a tree from
// another tree's run (`carried from t:<old8>: ...`), never run there.
const CarriedPrefix = "carried from t:"

// Carried reports whether r was carried to its tree rather than run there.
func (r Result) Carried() bool { return strings.HasPrefix(r.Output, CarriedPrefix) }

// CreateResult stores r under its key, as PutResult does, only when no
// result is stored there; false when one is.
func CreateResult(ctx context.Context, c *Client, r Result) (bool, error) {
	if r.Args == nil {
		r.Args = []string{}
	}
	if r.At == "" {
		r.At = time.Now().UTC().Format(time.RFC3339)
	}
	kv, err := bucket(ctx, c, BucketResults)
	if err != nil {
		return false, err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return false, err
	}
	if _, err := kv.Create(ctx, ResultKey(r.Kind, r.Tree, r.Args, r.Model), data); err != nil {
		if isConflict(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// NearestResult is the result of kind under args and model at the first of
// trees that has one run there: a carried result (Result.Carried) is
// passed over for the trees after it, where its source is. False when none
// has one.
func NearestResult(ctx context.Context, c *Client, kind string, trees, args []string, model string) (Result, bool, error) {
	for _, tree := range trees {
		r, ok, err := GetResult(ctx, c, ResultKey(kind, tree, args, model))
		if err != nil || (ok && !r.Carried()) {
			return r, ok, err
		}
	}
	return Result{}, false, nil
}

// GetResult returns false when no result is stored under key.
func GetResult(ctx context.Context, c *Client, key string) (Result, bool, error) {
	var r Result
	ok, err := get(ctx, c, BucketResults, key, &r)
	return r, ok, err
}

// ListResults returns every result of kind, sorted by key.
func ListResults(ctx context.Context, c *Client, kind string) ([]Result, error) {
	return listResults(ctx, c, kind+".")
}

// ListAllResults returns every result of every kind, sorted by key.
func ListAllResults(ctx context.Context, c *Client) ([]Result, error) {
	return listResults(ctx, c, "")
}

func listResults(ctx context.Context, c *Client, prefix string) ([]Result, error) {
	kv, err := bucket(ctx, c, BucketResults)
	if err != nil {
		return nil, err
	}
	keys, err := listKeys(ctx, c, kv, ">")
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	var out []Result
	for _, k := range keys {
		if !strings.HasPrefix(k, prefix) {
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
