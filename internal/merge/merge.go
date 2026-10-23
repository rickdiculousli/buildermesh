// Package merge resolves the conflict regions of a three-way text merge by
// rule, and leaves a region no rule resolves a conflict. It reads a file as
// git merge-file --diff3 writes it, one region at a time:
//
//  1. additions at one point (the base side is empty): both are kept,
//     main's ("ours") lines first;
//  2. changes to different lines, a conflict only because they touch, with
//     lines both sides deleted counted as agreed: both are applied;
//  3. prose a sentence apart (markdown outside code fences and tables, or a
//     region of comment lines): each side is split one sentence per line,
//     rules 1 and 2 are applied to the sentences, and the sentences are
//     joined back into their lines (a comment is wrapped again at its old
//     width).
//
// A region in a markdown file's frontmatter always stays a conflict. The
// rules only keep both sides' lines or sentences; they never write one.
package merge

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rule names how a region was resolved.
type Rule string

const (
	KeptBoth    Rule = "kept both"
	AppliedBoth Rule = "applied both"
	BySentence  Rule = "by sentence"
)

// MinMarker is git's conflict marker size.
const MinMarker = 7

// MarkerSize is a conflict marker size no line of texts can be mistaken
// for: one longer than the longest run of a marker character starting any
// line, and at least MinMarker.
func MarkerSize(texts ...string) int {
	n := MinMarker
	for _, t := range texts {
		for _, l := range strings.Split(t, "\n") {
			if l == "" || !strings.ContainsRune("<|=>", rune(l[0])) {
				continue
			}
			run := len(l) - len(strings.TrimLeft(l, l[:1]))
			n = max(n, run+1)
		}
	}
	return n
}

// File resolves every conflict region of text, the file at path (its name
// says whether it is markdown or which comments it has) merged in diff3
// style with markers of size marker. It returns the resolved text and the
// rules that resolved its regions, each once in the order first used; ok is
// false when any region is left, or the markers don't parse.
func File(path, text string, marker int) (out string, rules []Rule, ok bool) {
	open, mid, sep, end := strings.Repeat("<", marker), strings.Repeat("|", marker), strings.Repeat("=", marker), strings.Repeat(">", marker)
	isMarker := func(l, m string) bool {
		return l == m || strings.HasPrefix(l, m+" ")
	}
	final := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	f := fileKind(path)
	var res []string
	var st scan
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSuffix(lines[i], "\r")
		if !isMarker(l, open) {
			if isMarker(l, mid) || l == sep || isMarker(l, end) {
				return "", nil, false
			}
			res = append(res, lines[i])
			st.feed(f, lines[i])
			continue
		}
		var sides [3][]string // ours, base, theirs
		cur := -1
		for i++; ; i++ {
			if i >= len(lines) {
				return "", nil, false
			}
			l := strings.TrimSuffix(lines[i], "\r")
			switch {
			case cur == -1 && isMarker(l, mid):
				cur = 1
			case cur == 1 && l == sep:
				cur = 2
			case cur == 2 && isMarker(l, end):
				cur = 3
			case isMarker(l, open) || isMarker(l, mid) || l == sep || isMarker(l, end):
				return "", nil, false
			default:
				sides[max(cur, 0)] = append(sides[max(cur, 0)], lines[i])
			}
			if cur == 3 {
				break
			}
		}
		got, used, ok := region(f, &st, sides[1], sides[0], sides[2])
		if !ok {
			return "", nil, false
		}
		for _, r := range used {
			if !slices.Contains(rules, r) {
				rules = append(rules, r)
			}
		}
		res = append(res, got...)
		for _, g := range got {
			st.feed(f, g)
		}
	}
	out = strings.Join(res, "\n")
	if final {
		out += "\n"
	}
	return out, rules, true
}

// kind is what a file's name says about its text.
type kind struct {
	markdown bool
	comment  string // the line comment leader, "" for none
}

// slashExt and hashExt are the extensions whose line comments start with
// // and with #; hashNames are the files named without one that use #.
var (
	slashExt  = []string{".go", ".c", ".h", ".cc", ".cpp", ".hpp", ".java", ".js", ".jsx", ".ts", ".tsx", ".rs", ".swift", ".kt", ".scala", ".cs", ".proto", ".dart"}
	hashExt   = []string{".sh", ".bash", ".zsh", ".py", ".rb", ".pl", ".toml", ".yaml", ".yml", ".conf", ".cfg", ".mk"}
	hashNames = []string{"Makefile", "Dockerfile", ".gitignore", ".gitattributes"}
)

func fileKind(path string) kind {
	base := path[strings.LastIndexAny(path, `/\`)+1:]
	ext := ""
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		ext = strings.ToLower(base[i:])
	}
	switch {
	case ext == ".md" || ext == ".markdown":
		return kind{markdown: true}
	case slices.Contains(slashExt, ext):
		return kind{comment: "//"}
	case slices.Contains(hashExt, ext) || slices.Contains(hashNames, base):
		return kind{comment: "#"}
	}
	return kind{}
}

// scan is where the lines so far leave a markdown file: in its
// frontmatter, in a code fence.
type scan struct {
	n     int // lines seen
	front bool
	fence bool
}

func (s *scan) feed(f kind, l string) {
	defer func() { s.n++ }()
	if !f.markdown {
		return
	}
	l = strings.TrimSuffix(l, "\r")
	switch {
	case s.n == 0 && l == "---":
		s.front = true
	case s.front:
		if l == "---" {
			s.front = false
		}
	case isFence(l):
		s.fence = !s.fence
	}
}

func isFence(l string) bool {
	t := strings.TrimLeft(l, " ")
	return len(l)-len(t) <= 3 && (strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"))
}

// region resolves one conflict region from its three sides, rule by rule,
// after the lines st has seen.
func region(f kind, st *scan, base, ours, theirs []string) ([]string, []Rule, bool) {
	if f.markdown && (st.front || st.n == 0 && slices.ContainsFunc([][]string{base, ours, theirs}, func(s []string) bool {
		return len(s) > 0 && strings.TrimSuffix(s[0], "\r") == "---"
	})) {
		return nil, nil, false
	}
	if out, kept, ok := merge3(base, ours, theirs); ok {
		if len(base) == 0 {
			return out, []Rule{KeptBoth}, true
		}
		if kept {
			return out, []Rule{KeptBoth, AppliedBoth}, true
		}
		return out, []Rule{AppliedBoth}, true
	}
	switch {
	case f.markdown && !st.fence:
		if out, ok := mdMerge(base, ours, theirs); ok {
			return out, []Rule{BySentence}, true
		}
	case f.comment != "":
		if out, ok := commentMerge(f.comment, base, ours, theirs); ok {
			return out, []Rule{BySentence}, true
		}
	}
	return nil, nil, false
}

// hunk is one side's change to base: base[s:e] replaced by lines (s == e an
// insertion before base[s]).
type hunk struct {
	s, e  int
	lines []string
}

func (h hunk) insert() bool { return h.s == h.e }
func (h hunk) delete() bool { return h.s < h.e && len(h.lines) == 0 }

// maxCells bounds a diff's work: a longer region stays a conflict.
const maxCells = 1 << 22

// diff is side's hunks against base: the longest run of lines both share is
// matched first, earliest on ties, then the same on each side of it
// (difflib's matching blocks), so a line repeated nearby (a brace, a blank)
// matches beside its neighbours rather than wherever a subsequence fits.
func diff(base, side []string) ([]hunk, bool) {
	if (len(base)+1)*(len(side)+1) > maxCells {
		return nil, false
	}
	type block struct{ i, j, k int }
	var blocks []block
	var match func(alo, ahi, blo, bhi int)
	match = func(alo, ahi, blo, bhi int) {
		best := block{alo, blo, 0}
		run := make([]int, bhi-blo+1) // run[j-blo+1]: the match ending at base[i], side[j]
		for i := alo; i < ahi; i++ {
			next := make([]int, len(run))
			for j := blo; j < bhi; j++ {
				if base[i] == side[j] {
					k := run[j-blo] + 1
					next[j-blo+1] = k
					if k > best.k {
						best = block{i - k + 1, j - k + 1, k}
					}
				}
			}
			run = next
		}
		if best.k == 0 {
			return
		}
		match(alo, best.i, blo, best.j)
		blocks = append(blocks, best)
		match(best.i+best.k, ahi, best.j+best.k, bhi)
	}
	match(0, len(base), 0, len(side))
	blocks = append(blocks, block{len(base), len(side), 0})
	var hs []hunk
	i, j := 0, 0
	for _, b := range blocks {
		if i < b.i || j < b.j {
			hs = append(hs, hunk{i, b.i, side[j:b.j]})
		}
		i, j = b.i+b.k, b.j+b.k
	}
	return hs, true
}

// merge3 applies both sides' changes to base when no two of them touch the
// same base line, except lines both delete, and neither inserts inside a
// run of lines the other changes. Insertions of both at one point are both
// kept, ours first (kept reports one), and a change both made is made once.
func merge3(base, ours, theirs []string) (out []string, kept, ok bool) {
	ho, ok := diff(base, ours)
	if !ok {
		return nil, false, false
	}
	ht, ok := diff(base, theirs)
	if !ok {
		return nil, false, false
	}
	ht = slices.DeleteFunc(ht, func(k hunk) bool {
		return slices.ContainsFunc(ho, func(h hunk) bool { return h.s == k.s && h.e == k.e && slices.Equal(h.lines, k.lines) })
	})
	for _, h := range ho {
		for _, k := range ht {
			switch {
			case h.insert() && k.insert():
				kept = kept || h.s == k.s
			case h.insert():
				if k.s < h.s && h.s < k.e {
					return nil, false, false
				}
			case k.insert():
				if h.s < k.s && k.s < h.e {
					return nil, false, false
				}
			case max(h.s, k.s) < min(h.e, k.e) && !(h.delete() && k.delete()):
				return nil, false, false
			}
		}
	}
	gone := make([]bool, len(base))
	at := make([][]string, len(base)+1) // what goes before base[i]: insertions, then replacements
	for _, hs := range [][]hunk{ho, ht} {
		for _, h := range hs {
			if h.insert() {
				at[h.s] = append(at[h.s], h.lines...)
			}
		}
	}
	for _, hs := range [][]hunk{ho, ht} {
		for _, h := range hs {
			if !h.insert() {
				at[h.s] = append(at[h.s], h.lines...)
				for x := h.s; x < h.e; x++ {
					gone[x] = true
				}
			}
		}
	}
	out = []string{}
	for i := 0; i <= len(base); i++ {
		out = append(out, at[i]...)
		if i < len(base) && !gone[i] {
			out = append(out, base[i])
		}
	}
	return out, kept, true
}

// eol ends a line's sentences, and para a comment's paragraph; neither can
// be part of a line.
const (
	eol  = "\n"
	para = "\n\n"
)

// sentences splits s at each run of white space that follows a sentence's
// or a clause's end (. ! ? ; :, then any closing marks) and comes before
// what can start one (a capital, a quote, a backtick, an opening bracket or
// emphasis).
func sentences(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 1; i < len(s); i++ {
		if !isSpace(s[i]) || isSpace(s[i-1]) {
			continue
		}
		e := i
		for e < len(s) && isSpace(s[e]) {
			e++
		}
		if e == len(s) {
			break
		}
		j := i - 1
		for j > start && strings.IndexByte(")]\"'*_`", s[j]) >= 0 {
			j--
		}
		r, _ := utf8.DecodeRuneInString(s[e:])
		if strings.IndexByte(".!?;:", s[j]) < 0 || !(unicode.IsUpper(r) || strings.ContainsRune("`([*\"'_", r)) {
			continue
		}
		out = append(out, s[start:i])
		start = e
	}
	return append(out, s[start:])
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

// mdMerge is rule 3 for markdown lines: none a fence, a table row or
// indented code, and each splitting into sentences that join back to it.
func mdMerge(base, ours, theirs []string) ([]string, bool) {
	var toks [3][]string
	for n, side := range [][]string{base, ours, theirs} {
		for _, l := range side {
			t := strings.TrimLeft(l, " ")
			if isFence(l) || strings.HasPrefix(t, "|") || strings.HasPrefix(l, "\t") || len(l)-len(t) >= 4 {
				return nil, false
			}
			s := sentences(l)
			if strings.Join(s, " ") != l {
				return nil, false
			}
			toks[n] = append(append(toks[n], s...), eol)
		}
	}
	m, _, ok := merge3(toks[0], toks[1], toks[2])
	if !ok {
		return nil, false
	}
	var out, cur []string
	for _, t := range m {
		if t == eol {
			out = append(out, strings.Join(cur, " "))
			cur = nil
			continue
		}
		cur = append(cur, t)
	}
	if cur != nil {
		out = append(out, strings.Join(cur, " "))
	}
	return out, true
}

// structured reports comment text that starts a list item or a table row:
// structure, not prose.
func structured(t string) bool {
	for _, p := range []string{"- ", "* ", "+ ", "|"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	d := strings.TrimLeft(t, "0123456789")
	return len(d) < len(t) && (strings.HasPrefix(d, ". ") || strings.HasPrefix(d, ") "))
}

// commentMerge is rule 3 for a region whose every line, on all three sides,
// is a comment with one indent and leader: the comment's paragraphs are
// split into sentences and merged, and the result is wrapped again at the
// widest line any side had.
func commentMerge(leader string, base, ours, theirs []string) ([]string, bool) {
	prefix := ""
	width := 0
	var toks [3][]string
	for n, side := range [][]string{base, ours, theirs} {
		var text []string
		end := func() bool {
			p := strings.Join(text, " ")
			text = nil
			if p == "" {
				return true
			}
			s := sentences(p)
			if strings.Join(s, " ") != p || strings.Contains(p, "  ") || strings.Contains(p, "\t") {
				return false
			}
			toks[n] = append(toks[n], s...)
			return true
		}
		for _, l := range side {
			k := strings.Index(l, leader)
			if k < 0 || strings.TrimLeft(l[:k], " \t") != "" {
				return nil, false
			}
			p, rest := l[:k+len(leader)], l[k+len(leader):]
			if prefix == "" {
				prefix = p
			}
			if p != prefix {
				return nil, false
			}
			width = max(width, utf8.RuneCountInString(l))
			if rest == "" {
				if !end() {
					return nil, false
				}
				toks[n] = append(toks[n], para)
				continue
			}
			t, ok := strings.CutPrefix(rest, " ")
			if !ok || t == "" || t != strings.TrimSpace(t) || structured(t) {
				return nil, false
			}
			text = append(text, t)
		}
		if !end() {
			return nil, false
		}
	}
	m, _, ok := merge3(toks[0], toks[1], toks[2])
	if !ok {
		return nil, false
	}
	var out, cur []string
	flush := func() {
		if cur != nil {
			out = append(out, wrap(prefix+" ", strings.Join(cur, " "), width)...)
			cur = nil
		}
	}
	for _, t := range m {
		if t == para {
			flush()
			out = append(out, prefix)
			continue
		}
		cur = append(cur, t)
	}
	flush()
	return out, true
}

// wrap fills text's words into lines that start with prefix and are at
// most width long, a longer word alone on its line.
func wrap(prefix, text string, width int) []string {
	var out []string
	line := ""
	for _, w := range strings.Split(text, " ") {
		switch {
		case line == "":
			line = prefix + w
		case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) <= width:
			line += " " + w
		default:
			out = append(out, line)
			line = prefix + w
		}
	}
	return append(out, line)
}
