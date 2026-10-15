// Package meshtest starts an embedded NATS server for tests.
package meshtest

import (
	"os"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// Start runs an embedded server with a store under t.TempDir() and returns its URL.
func Start(t *testing.T) string {
	t.Helper()
	if d := os.TempDir(); d == "/tmp" || strings.HasPrefix(d, "/tmp/") {
		t.Fatal("meshtest: TMPDIR is /tmp: set TMPDIR=$HOME/.cache/bmesh-scratch/tmp")
	}
	e, err := mesh.StartEmbedded(t.TempDir(), "127.0.0.1:-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Shutdown)
	return e.URL()
}
