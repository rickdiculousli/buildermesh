package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

func TestMeshStatusEndsWithOpenOperatorSteps(t *testing.T) {
	_, _, repo := stepsRepo(t)
	line := "operator: demo 015 Steps here: 4 of 4 open (bmesh spec steps 015 --repo demo)"
	out, errs, code := runCLI(t, nil, "mesh-status")
	if code != 0 || !strings.HasSuffix(out, "\n"+line+"\n") {
		t.Fatalf("mesh-status: %d %q %q", code, out, errs)
	}
	// Ticked from another clone (another box): the fetch drops the line.
	other := filepath.Join(t.TempDir(), "other")
	meshtest.Git(t, filepath.Dir(other), "clone", "-q", "-b", "main", gitOut(t, "-C", repo, "remote", "get-url", "origin"), other)
	p := filepath.Join(other, "docs", "proposals", "015-steps.md")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.ReplaceAll(string(b), `"done":""`, `"done":"2026-10-17T10:00:00Z otherbox"`)), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, other, "commit", "-qam", "Steps: 015 1, 2, 3, 4 done")
	meshtest.Git(t, other, "push", "-q", "origin", "HEAD:main")
	out, errs, code = runCLI(t, nil, "mesh-status")
	if code != 0 || strings.Contains(out, "operator:") {
		t.Fatalf("after the ticks: %d %q %q", code, out, errs)
	}
}
