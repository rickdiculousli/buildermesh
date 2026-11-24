package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// A manual quote: a line the manual says bmesh prints, as a pattern, and
// its fixed start, the text before its first placeholder.
type quote struct {
	text, start string
	re          *regexp.Regexp
}

var (
	manualOnce     sync.Once
	manualQuotes   []quote
	manualCommands string // commands.txt for an arm of this tree's manual
	manualErr      error
)

// quotes are the inline code spans of this tree's docs/manual.md that read
// as lines (quoteOf), read one line at a time, fenced blocks skipped.
func quotes(t *testing.T) []quote {
	t.Helper()
	manualOnce.Do(func() {
		var b []byte
		if b, manualErr = os.ReadFile(filepath.Join(repoRoot(t), "docs", "manual.md")); manualErr != nil {
			return
		}
		manualCommands = strings.Join(commandsFromManual(string(b)), "\n") + "\n"
		fenced := false
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "```") {
				fenced = !fenced
				continue
			}
			if fenced {
				continue
			}
			for _, s := range codeSpans(l) {
				if q, ok := quoteOf(s); ok {
					manualQuotes = append(manualQuotes, q)
				}
			}
		}
	})
	if manualErr != nil {
		t.Fatal(manualErr)
	}
	return manualQuotes
}

// codeSpans are a markdown line's inline code: a run of n backticks up to
// the next run of exactly n, one space trimmed from each end of a span
// that has a space at both.
func codeSpans(l string) []string {
	var out []string
	run := func(i int) int {
		n := 0
		for i+n < len(l) && l[i+n] == '`' {
			n++
		}
		return n
	}
	for i := 0; i < len(l); {
		n := run(i)
		if n == 0 {
			i++
			continue
		}
		end := -1
		for j := i + n; j < len(l); {
			m := run(j)
			if m == 0 {
				j++
				continue
			}
			if m == n {
				end = j
				break
			}
			j += m
		}
		if end < 0 {
			i += n
			continue
		}
		s := l[i+n : end]
		if len(s) > 1 && s[0] == ' ' && s[len(s)-1] == ' ' && strings.Trim(s, " ") != "" {
			s = s[1 : len(s)-1]
		}
		out = append(out, s)
		i = end + n
	}
	return out
}

// placeholder is the manual's grammar for what varies in a quoted line:
// <x> any text, NNN or N digits (or itself, as `--spec NNN` prints),
// X.Y.Z a version, a|b either word, [...] optional, ... or … any text.
var placeholder = regexp.MustCompile(`<[^<>]+>|\bN{1,3}\b|X\.Y\.Z\b|\w[\w-]*(?:\|\w[\w-]*)+|\[[^\[\]]+\]|\.\.\.|…`)

// quoteOf reads a code span as a quote; ok is false when its fixed start
// is less than a word and a space: a name (`verify`, `started`), not a
// line, which every line beginning that word would otherwise be held to.
func quoteOf(s string) (quote, bool) {
	s = strings.ReplaceAll(s, `\|`, `|`)
	start := s
	if loc := placeholder.FindStringIndex(s); loc != nil {
		start = s[:loc[0]]
	}
	if !strings.Contains(strings.TrimLeft(start, " "), " ") {
		return quote{}, false
	}
	return quote{text: s, start: start, re: regexp.MustCompile("^" + pattern(s) + "$")}, true
}

func pattern(s string) string {
	var b strings.Builder
	last := 0
	for _, loc := range placeholder.FindAllStringIndex(s, -1) {
		b.WriteString(regexp.QuoteMeta(s[last:loc[0]]))
		p := s[loc[0]:loc[1]]
		switch {
		case p[0] == '<', p == "...", p == "…":
			b.WriteString(".*")
		case p[0] != '[' && strings.Contains(p, "|"):
			words := strings.Split(p, "|")
			for i, w := range words {
				words[i] = regexp.QuoteMeta(w)
			}
			b.WriteString("(?:" + strings.Join(words, "|") + ")")
		case p[0] == 'N':
			b.WriteString(`(?:\d+|` + p + `)`)
		case p == "X.Y.Z":
			b.WriteString(`\d+\.\d+\.\d+`)
		default:
			b.WriteString("(?:" + pattern(p[1:len(p)-1]) + ")?")
		}
		last = loc[1]
	}
	b.WriteString(regexp.QuoteMeta(s[last:]))
	return b.String()
}

// mismatch is the manual's quotes a printed line begins but matches none
// of, nil when it matches one or begins none.
func mismatch(qs []quote, line string) []string {
	line = strings.TrimPrefix(line, "bmesh: ")
	var begun []string
	for _, q := range qs {
		if !strings.HasPrefix(line, q.start) {
			continue
		}
		if q.re.MatchString(line) {
			return nil
		}
		begun = append(begun, q.text)
	}
	return begun
}

// heldToManual fails t for each line the fake printed in cell dir that
// the manual quotes differently: a line the binary and the manual changed
// is changed in the fake too, or this names it. A cell of an older arm,
// whose commands aren't this manual's, prints its own manual's lines, so
// it is not held to this one.
func heldToManual(t *testing.T, dir string, args []string, outs ...string) {
	t.Helper()
	qs := quotes(t)
	if b, err := os.ReadFile(filepath.Join(dir, "commands.txt")); err != nil || string(b) != manualCommands {
		return
	}
	for _, out := range outs {
		for _, l := range strings.Split(out, "\n") {
			if l == "" {
				continue
			}
			if begun := mismatch(qs, l); begun != nil {
				t.Errorf("bmesh %s printed %q, which the manual quotes as %q", strings.Join(args, " "), l, begun)
			}
		}
	}
}

// The matcher reads the manual's grammar, and holds only a line that
// begins a quote's fixed start.
func TestManualQuotes(t *testing.T) {
	if got := codeSpans("prints `a b`, or ``c `d` e`` and `` `f` ``"); !slices.Equal(got, []string{"a b", "c `d` e", "`f`"}) {
		t.Errorf("codeSpans: %q", got)
	}
	var qs []quote
	for _, s := range []string{"verify", "next:", "<kind> ok t:<tree8>", "ready: <host>'s daemon lands <sha8>",
		"drained N from <family>", "would release <repo> vX.Y.Z at <sha8> (...)", "this box is <host>[ (admin)]; secure: yes\\|no",
		"land needs --spec NNN"} {
		if q, ok := quoteOf(s); ok {
			qs = append(qs, q)
		}
	}
	if len(qs) != 5 {
		t.Fatalf("%d quotes read as lines, want 5 (not a name, a bare prefix or one opening with a placeholder): %v", len(qs), qs)
	}
	for line, held := range map[string]bool{
		"ready: k16's daemon lands 0123abcd":               false,
		"bmesh: ready: k16's daemon land 0123abcd":         true,
		"drained 2 from shop-build-1":                      false,
		"drained two from shop-build-1":                    true,
		"would release shop v0.1.0 at 0123abcd (no specs)": false,
		"would release shop vX at 0123abcd (no specs)":     true,
		"this box is k16; secure: no":                      false,
		"this box is k16 (admin); secure: yes":             false,
		"this box is k16; secure: maybe":                   true,
		"land needs --spec NNN":                            false,
		"land needs --spec 012":                            false,
		"verify ok t:0123abcd":                             false,
		"started shop-012":                                 false,
	} {
		if got := mismatch(qs, line) != nil; got != held {
			t.Errorf("%q fails the check %v, want %v", line, got, held)
		}
	}
}
