package merge

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the fixtures' want files")

// diff3 is one region as git merge-file --diff3 writes it, between before
// and after.
func diff3(before, ours, base, theirs, after string) string {
	return before + "<<<<<<< ours\n" + ours + "||||||| base\n" + base + "=======\n" + theirs + ">>>>>>> theirs\n" + after
}

// TestMeasuredRegions resolves the three sides of conflict regions measured
// in real land refusals (testdata/<name>/{ours,base,theirs}), each as part
// of a file of its own kind, and checks the rule and the text.
func TestMeasuredRegions(t *testing.T) {
	for _, c := range []struct {
		name, path string
		rules      []Rule // nil: the region stays a conflict
	}{
		{"057-appended-tests", "cmd/bmesh/cmd_spec_test.go", []Rule{KeptBoth}},
		{"020-struct-fields", "internal/daemon/hosts.go", []Rule{KeptBoth}},
		{"054-rename-beside-new-lines", "internal/runner/claude_test.go", []Rule{AppliedBoth}},
		{"059-shared-deletion", "cmd/bmesh/cmd_update.go", []Rule{AppliedBoth}},
		{"037-comment", "internal/daemon/routines.go", []Rule{BySentence}},
		{"061-skill-paragraph", "skills/build/SKILL.md", []Rule{BySentence}},
		// Both sides added a sentence at one point of the line: rule 1 on
		// its sentences.
		{"032-modes-line", "README.md", []Rule{BySentence}},
		{"020-comment", "internal/daemon/hosts.go", nil},
		{"058-builds-line", "README.md", nil},
		{"056-statusrow-comment", "cmd/bmesh/cmd_status.go", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join("testdata", c.name)
			read := func(f string) string {
				b, err := os.ReadFile(filepath.Join(dir, f))
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
			ours, base, theirs := read("ours"), read("base"), read("theirs")
			before, after := "before\n\n", "\nafter\n"
			if strings.HasSuffix(c.path, ".md") {
				before = "# Title\n\n" // past any frontmatter
			}
			got, rules, ok := File(c.path, diff3(before, ours, base, theirs, after), MarkerSize(ours, base, theirs))
			if c.rules == nil {
				if ok {
					t.Fatalf("resolved (%v), want a conflict:\n%s", rules, got)
				}
				return
			}
			if !ok || !slices.Equal(rules, c.rules) {
				t.Fatalf("ok %v rules %v, want %v", ok, rules, c.rules)
			}
			if !strings.HasPrefix(got, before) || !strings.HasSuffix(got, after) {
				t.Fatalf("the text around the region changed:\n%s", got)
			}
			got = strings.TrimSuffix(strings.TrimPrefix(got, before), after)
			want := filepath.Join(dir, "want")
			if *update {
				if err := os.WriteFile(want, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if w := read("want"); got != w {
				t.Fatalf("got\n%s\nwant\n%s", got, w)
			}
		})
	}
}

func TestKeptBothPutsMainFirst(t *testing.T) {
	got, rules, ok := File("a_test.go", diff3("package a\n", "func TestA() {}\n", "", "func TestB() {}\n", ""), 7)
	if !ok || got != "package a\nfunc TestA() {}\nfunc TestB() {}\n" || !slices.Equal(rules, []Rule{KeptBoth}) {
		t.Fatalf("%v %v\n%s", ok, rules, got)
	}
}

func TestSameLineRewrittenStaysConflict(t *testing.T) {
	if got, _, ok := File("a.go", diff3("", "x := 2\n", "x := 1\n", "x := 3\n", ""), 7); ok {
		t.Fatalf("resolved:\n%s", got)
	}
}

func TestSameChangeOnBothSidesMadeOnce(t *testing.T) {
	got, _, ok := File("a.go", diff3("", "a\nB\nc\nd\ne\nF\ng\n", "a\nb\nc\nd\ne\nf\ng\n", "a\nB\nc\nD\ne\nf\ng\n", ""), 7)
	if !ok || got != "a\nB\nc\nD\ne\nF\ng\n" {
		t.Fatalf("%v\n%s", ok, got)
	}
}

func TestInsertInsideOtherSidesChangeStaysConflict(t *testing.T) {
	// Theirs rewrote b..c; ours put a line between them.
	if got, _, ok := File("a.go", diff3("", "a\nb\nX\nc\n", "a\nb\nc\n", "a\nB\nC\n", ""), 7); ok {
		t.Fatalf("resolved:\n%s", got)
	}
}

func TestBothAddASentenceToAParagraph(t *testing.T) {
	base := "The box runs jobs. It logs each.\n"
	ours := "The box runs jobs. It logs each. Main adds this.\n"
	theirs := "The box runs jobs. It logs each. The branch adds that.\n"
	got, rules, ok := File("README.md", diff3("# T\n\n", ours, base, theirs, ""), 7)
	want := "# T\n\nThe box runs jobs. It logs each. Main adds this. The branch adds that.\n"
	if !ok || got != want || !slices.Equal(rules, []Rule{BySentence}) {
		t.Fatalf("%v %v\n%s", ok, rules, got)
	}
}

func TestSentencesThatDontRoundTripAreLeftAlone(t *testing.T) {
	// The same edits resolve by sentence with one space after each
	// sentence; with two, a line doesn't split and join to itself.
	for _, c := range []struct {
		sp string
		ok bool
	}{{" ", true}, {"  ", false}} {
		base := "One." + c.sp + "Two." + c.sp + "Three.\n"
		ours := "One more." + c.sp + "Two." + c.sp + "Three.\n"
		theirs := "One." + c.sp + "Two." + c.sp + "Three more.\n"
		got, _, ok := File("doc.md", diff3("", ours, base, theirs, ""), 7)
		if ok != c.ok {
			t.Fatalf("space %q: ok %v, want %v\n%s", c.sp, ok, c.ok, got)
		}
		if ok && got != "One more. Two. Three more.\n" {
			t.Fatalf("got %q", got)
		}
	}
}

func TestFrontmatterStaysConflict(t *testing.T) {
	front := "---\nid: 001\ntitle: x\n"
	got, _, ok := File("docs/proposals/001-x.md", diff3(front, "merged: abc\n", "", "branch: b\n", "---\n\nBody.\n"), 7)
	if ok {
		t.Fatalf("a frontmatter key added on both sides resolved:\n%s", got)
	}
	// At the file's very start, too.
	if got, _, ok := File("x.md", diff3("", "---\nid: 1\n", "", "---\nid: 2\n", "---\n"), 7); ok {
		t.Fatalf("resolved:\n%s", got)
	}
	// Past the frontmatter, the same addition is kept.
	if _, _, ok := File("x.md", diff3(front+"---\n\n", "A.\n", "", "B.\n", ""), 7); !ok {
		t.Fatal("an addition past the frontmatter stayed a conflict")
	}
}

func TestProseRuleSkipsFencesAndTables(t *testing.T) {
	base := "One. Two.\n"
	ours := "One more. Two.\n"
	theirs := "One. Two more.\n"
	if _, _, ok := File("a.md", diff3("```\n", ours, base, theirs, "```\n"), 7); ok {
		t.Fatal("a region in a code fence merged by sentence")
	}
	if _, _, ok := File("a.txt", diff3("", ours, base, theirs, ""), 7); ok {
		t.Fatal("a text file merged by sentence")
	}
	if _, _, ok := File("a.md", diff3("", "| "+ours, "| "+base, "| "+theirs, ""), 7); ok {
		t.Fatal("a table row merged by sentence")
	}
	if _, _, ok := File("a.md", diff3("```\nx\n```\n", ours, base, theirs, ""), 7); !ok {
		t.Fatal("a region past a closed fence stayed a conflict")
	}
}

func TestCommentRule(t *testing.T) {
	base := "\t// The box runs jobs. It logs\n\t// each one.\n"
	ours := "\t// The box runs all jobs. It logs\n\t// each one.\n"
	theirs := "\t// The box runs jobs. It logs each one, and\n\t// says so.\n"
	got, rules, ok := File("a.go", diff3("", ours, base, theirs, ""), 7)
	// Wrapped at the widest line a side had.
	want := "\t// The box runs all jobs. It logs each one,\n\t// and says so.\n"
	if !ok || got != want || !slices.Equal(rules, []Rule{BySentence}) {
		t.Fatalf("%v %v\n%q", ok, rules, got)
	}
	// # comments in a shell script, with a paragraph break.
	c := "#\n# C is the last paragraph.\n"
	got, _, ok = File("x.sh", diff3("", "# A one. B.\n"+c, "# A. B.\n"+c, "# A. B two.\n"+c, ""), 7)
	if !ok || got != "# A one. B two.\n"+c {
		t.Fatalf("%v %q", ok, got)
	}
	// A list in a comment is structure, not prose.
	if _, _, ok := File("a.go", diff3("", "// - A one. B.\n", "// - A. B.\n", "// - A. B two.\n", ""), 7); ok {
		t.Fatal("a comment list merged by sentence")
	}
	// A directive is not prose.
	if _, _, ok := File("a.go", diff3("", "//go:build a. B\n", "//go:build x. B\n", "//go:build x. C\n", ""), 7); ok {
		t.Fatal("a directive merged by sentence")
	}
}

func TestEveryRegionMustResolve(t *testing.T) {
	text := diff3("", "a\n", "", "b\n", "mid\n") + diff3("", "x := 2\n", "x := 1\n", "x := 3\n", "")
	if _, _, ok := File("a.go", text, 7); ok {
		t.Fatal("a file with one region left resolved")
	}
}

func TestMarkersOfTheGivenSize(t *testing.T) {
	// A line of seven '<' in a side is content when the markers are longer.
	ours, base, theirs := "<<<<<<< not a marker\n", "", "b\n"
	n := MarkerSize(ours, base, theirs)
	if n != 8 {
		t.Fatalf("MarkerSize = %d, want 8", n)
	}
	m := func(c string) string { return strings.Repeat(c, n) }
	text := m("<") + " ours\n" + ours + m("|") + " base\n" + base + m("=") + "\n" + theirs + m(">") + " theirs\n"
	got, _, ok := File("a.go", text, n)
	if !ok || got != ours+theirs {
		t.Fatalf("%v %q", ok, got)
	}
	if _, _, ok := File("a.go", "<<<<<<< ours\na\n", 7); ok {
		t.Fatal("an unterminated region parsed")
	}
}

func TestNoFinalNewlineKept(t *testing.T) {
	got, _, ok := File("a.go", diff3("", "a\n", "", "b\n", "end"), 7)
	if !ok || got != "a\nb\nend" {
		t.Fatalf("%v %q", ok, got)
	}
}
