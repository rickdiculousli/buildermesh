package meshtest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nuid"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// Git runs git in dir with a fixed identity, no signing, no hooks and neither
// global nor system config; it fails t on error and returns the combined
// output trimmed of surrounding whitespace.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t",
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// CommitFiles writes files (slash-separated path relative to repo -> content),
// commits them and returns the new commit's full sha.
func CommitFiles(t testing.TB, repo string, files map[string]string) string {
	t.Helper()
	names := make([]string, 0, len(files))
	for name, content := range files {
		p := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	Git(t, repo, append([]string{"add", "--"}, names...)...)
	Git(t, repo, "commit", "-q", "--allow-empty", "-m", "commit")
	return Git(t, repo, "rev-parse", "HEAD")
}

// SyncBuffer is a bytes.Buffer behind a mutex, safe to write from a daemon's
// goroutines while the test reads it. The zero value is ready to use.
type SyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *SyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *SyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func helperCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// ForceState puts s straight into the state bucket at path, bypassing the
// transition table; At defaults to now.
func ForceState(t testing.TB, c *mesh.Client, path string, s mesh.State) {
	t.Helper()
	ctx, cancel := helperCtx()
	defer cancel()
	if s.At == "" {
		s.At = time.Now().UTC().Format(time.RFC3339)
	}
	kv, err := c.JS.KeyValue(ctx, mesh.BucketState)
	if err != nil {
		t.Fatalf("state bucket: %v", err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kv.Put(ctx, path, data); err != nil {
		t.Fatalf("force state %s: %v", path, err)
	}
}

// QueueJob sends j to kind's inbox from from, in the shape mesh.Queue sends,
// without resolving a commit. The kind's consumer is created first so the job
// counts toward mesh.Depth. It returns j with Kind, ID and Args filled.
func QueueJob(t testing.TB, c *mesh.Client, kind, from string, j mesh.Job) mesh.Job {
	t.Helper()
	ctx, cancel := helperCtx()
	defer cancel()
	j.Kind = kind
	if j.ID == "" {
		j.ID = nuid.Next()
	}
	if j.Args == nil {
		j.Args = []string{}
	}
	if err := mesh.EnsureKindInbox(ctx, c, kind); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.Send(ctx, c, mesh.Message{ID: j.ID, Kind: "job", From: from,
		To: mesh.RoutinePath(kind).String(), Text: j.Text, Job: &j}); err != nil {
		t.Fatalf("queue %s job: %v", kind, err)
	}
	return j
}
