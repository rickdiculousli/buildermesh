package brief

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// skillFrontmatter parses a SKILL.md's frontmatter as strict YAML and
// returns its name and description.
func skillFrontmatter(b []byte) (name, description string, err error) {
	rest, ok := bytes.CutPrefix(b, []byte("---\n"))
	if !ok {
		return "", "", fmt.Errorf("no frontmatter")
	}
	front, _, ok := bytes.Cut(rest, []byte("\n---\n"))
	if !ok {
		return "", "", fmt.Errorf("frontmatter not closed")
	}
	var m map[string]any
	if err := yaml.Unmarshal(front, &m); err != nil {
		return "", "", err
	}
	name, _ = m["name"].(string)
	description, _ = m["description"].(string)
	if name == "" || description == "" {
		return "", "", fmt.Errorf("want a name and a description, got %v", m)
	}
	return name, description, nil
}

// A loader that parses frontmatter strictly skips a skill whose YAML is
// invalid, silently: every skill must parse.
func TestEverySkillFrontmatterParses(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "skills", "*", "SKILL.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no skills: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name, desc, err := skillFrontmatter(b)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if dir := filepath.Base(filepath.Dir(f)); name != dir {
			t.Errorf("%s: name %q, want %q", f, name, dir)
		}
		if strings.Contains(desc, "\n") {
			t.Errorf("%s: description spans lines: %q", f, desc)
		}
	}
}

func TestSkillFrontmatterRefusesAPlainColon(t *testing.T) {
	old := "---\nname: ideate\ndescription: Use when the operator brings one (\"ideate this idea: ...\", \"spec this\").\n---\n"
	if _, _, err := skillFrontmatter([]byte(old)); err == nil {
		t.Fatal("the old ideate line parsed")
	}
}
