package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ledgerSpec is tests/test_proposals.py's SPEC.
const ledgerSpec = "---\nid: 012\ntitle: Faster tests\nkind: test-speed\nsize: S\njudge: sonnet\nstatus: proposed\nlens: eng\nproposed_by: t1\nproposed_on: 2026-10-12\n---\n\n## Opportunity\nslow\n"

func mustParse(t *testing.T, text, path string) *Spec {
	t.Helper()
	s, err := Parse(text, path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseIDStatusAndBody(t *testing.T) {
	s := mustParse(t, ledgerSpec, "docs/proposals/012-faster-tests.md")
	if s.ID != 12 || s.Slug != "faster-tests" || s.Status() != "proposed" || !strings.HasPrefix(s.Body, "## Opportunity") {
		t.Fatalf("%+v", s)
	}
	if s.Order[0] != "id" || s.Order[1] != "title" {
		t.Fatalf("order %v", s.Order)
	}
}

func TestParseRefusesMalformed(t *testing.T) {
	for name, tc := range map[string]struct{ text, path, want string }{
		"status":   {strings.Replace(ledgerSpec, "proposed", "maybe", 1), "x/012-a.md", "malformed"},
		"judge":    {strings.Replace(ledgerSpec, "judge: sonnet", "judge: gpt", 1), "x/012-a.md", "malformed"},
		"id":       {strings.Replace(ledgerSpec, "id: 012\n", "", 1), "x/012-a.md", "malformed"},
		"filename": {ledgerSpec, "x/013-a.md", "malformed: 013-a.md"},
		"fences":   {"id: 012\nstatus: proposed\n", "x/012-a.md", "malformed"},
		"line":     {strings.Replace(ledgerSpec, "lens: eng\n", "lens eng\n", 1), "x/012-a.md", "key: value"},
	} {
		_, err := Parse(tc.text, tc.path)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", name, err, tc.want)
		}
	}
}

func TestModelFor(t *testing.T) {
	m, err := ModelFor(mustParse(t, ledgerSpec, "x/012-a.md"))
	if err != nil || m != "claude-sonnet-5-5" {
		t.Fatal(m, err)
	}
	if _, err := ModelFor(mustParse(t, strings.Replace(ledgerSpec, "judge: sonnet\n", "", 1), "x/012-a.md")); err == nil || !strings.Contains(err.Error(), "judge") {
		t.Fatal(err)
	}
}

func TestUpdateSetsExistingAndAppendsNew(t *testing.T) {
	s := mustParse(t, ledgerSpec, "x/012-a.md")
	out, err := Update(s, Field{"status", "building"}, Field{"branch", "t3code/ab"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "status: building\n") || !strings.Contains(out, "branch: t3code/ab\n---") {
		t.Fatalf("%q", out)
	}
	if strings.Index(out, "title:") != strings.Index(ledgerSpec, "title:") {
		t.Fatalf("title moved: %q", out)
	}
	if same, _ := Update(s); same != ledgerSpec {
		t.Fatalf("update with no fields changed the text: %q", same)
	}
	if _, err := Update(s, Field{"reason", "a\nb"}); err == nil {
		t.Fatal("a value with a line break was accepted")
	}
}

func writeSpec(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSetStatus(t *testing.T) {
	dir := t.TempDir()
	p := writeSpec(t, dir, "012-a.md", ledgerSpec)
	s := mustParse(t, ledgerSpec, p)
	if _, err := SetStatus(s, "building", []string{"accepted"}); err == nil || !strings.Contains(err.Error(), "proposed") {
		t.Fatalf("wrong source accepted: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != ledgerSpec {
		t.Fatal("refused set changed the file")
	}
	n, err := SetStatus(s, "accepted", []string{"proposed"}, Field{"judge", "opus"})
	if err != nil || n.Status() != "accepted" || n.Fields["judge"] != "opus" {
		t.Fatal(n, err)
	}
	b, _ := os.ReadFile(p)
	if back := mustParse(t, string(b), p); back.Status() != "accepted" {
		t.Fatal(back.Status())
	}
	if _, err := SetStatus(n, "maybe", nil); err == nil {
		t.Fatal("unknown status accepted")
	}
	// A duplicated status key would read back as the last one: refused.
	dup := strings.Replace(ledgerSpec, "lens: eng\n", "lens: eng\nstatus: proposed\n", 1)
	p2 := writeSpec(t, dir, "013-b.md", strings.Replace(dup, "012", "013", 1))
	if _, err := SetStatus(mustParse(t, strings.Replace(dup, "012", "013", 1), p2), "accepted", []string{"proposed"}); err == nil || !strings.Contains(err.Error(), "read back") {
		t.Fatal(err)
	}
}

func TestScanNextIDAndCount(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "docs/proposals/README.md", "ignored")
	writeSpec(t, root, "docs/proposals/012-a.md", ledgerSpec)
	writeSpec(t, root, "docs/proposals/013-b.md", strings.ReplaceAll(strings.Replace(ledgerSpec, "012", "013", 1), "proposed", "rejected"))
	specs, err := Scan(root, "docs/proposals/{id}-*.md")
	if err != nil || len(specs) != 2 || NextID(specs) != 14 || Count(specs, "proposed") != 1 {
		t.Fatalf("%v %v", specs, err)
	}
	if empty, err := Scan(t.TempDir(), "docs/proposals/{id}-*.md"); err != nil || NextID(empty) != 1 {
		t.Fatal(empty, err)
	}
	writeSpec(t, root, "docs/proposals/014-c.md", strings.Replace(ledgerSpec, "012", "014", 1)+"")
	writeSpec(t, root, "docs/proposals/015-d.md", strings.Replace(strings.Replace(ledgerSpec, "012", "015", 1), "proposed", "maybe", 1))
	if _, err := Scan(root, "docs/proposals/{id}-*.md"); err == nil || !strings.Contains(err.Error(), "015-d.md") {
		t.Fatalf("malformed file counted: %v", err)
	}
}

func TestScanRefusesDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "docs/proposals/012-a.md", ledgerSpec)
	writeSpec(t, root, "docs/proposals/012-b.md", ledgerSpec)
	if _, err := Scan(root, "docs/proposals/{id}-*.md"); err == nil || !strings.Contains(err.Error(), "share id 012") {
		t.Fatal(err)
	}
}

func TestWriteBodyKeepsFrontmatter(t *testing.T) {
	p := writeSpec(t, t.TempDir(), "012-a.md", ledgerSpec)
	n, err := WriteBody(mustParse(t, ledgerSpec, p), "## Opportunity\nfast\n")
	if err != nil || n.Body != "## Opportunity\nfast\n" || n.Fields["title"] != "Faster tests" {
		t.Fatal(n, err)
	}
}
