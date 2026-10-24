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
		if strings.Contains(strings.TrimSuffix(desc, "\n"), "\n") {
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

// The skills name no pstack moment (none was ever listed), restate no
// engineering rule (the repo's AGENTS.md carries those), and the scaffold
// skill asks which rule sets a repo gets. Ponytail's copies are its own text.
func TestSkillsLeaveRulesToAgentsMD(t *testing.T) {
	copies := ponytailCopies(t)
	var files []string
	for _, dir := range []string{filepath.Join("..", "..", "skills"), "templates"} {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && !copies[filepath.ToSlash(p)] {
				files = append(files, p)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(files) < 10 {
		t.Fatalf("found only %v", files)
	}
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		if strings.Contains(text, "pstack") && strings.Contains(filepath.ToSlash(p), "skills/") {
			t.Errorf("%s names pstack", p)
		}
		for _, rule := range []string{"test that fails", "small enough to verify"} {
			if strings.Contains(text, rule) {
				t.Errorf("%s restates a rule: %q", p, rule)
			}
		}
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "skills", "scaffold", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"The rules interview", "Clear conflicts out of the repo's own prose", "Rule sets section of bmesh's manual"} {
		if !strings.Contains(string(b), w) {
			t.Errorf("the scaffold skill lacks %q", w)
		}
	}
}

// ponytailCopies is the files third_party/ponytail/PIN names as copied
// verbatim (its `copy <upstream> <ours>` lines), as paths from this package.
func ponytailCopies(t *testing.T) map[string]bool {
	b, err := os.ReadFile(filepath.Join("..", "..", "third_party", "ponytail", "PIN"))
	if err != nil {
		t.Fatal(err)
	}
	copies := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) == 3 && f[0] == "copy" {
			copies["../../"+f[2]] = true
		}
	}
	if len(copies) == 0 {
		t.Fatal("PIN names no copy")
	}
	return copies
}
