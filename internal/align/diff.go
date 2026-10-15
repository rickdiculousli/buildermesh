package align

import (
	"fmt"
	"strings"
)

// Unified is the unified diff of a and b (file texts) with three lines of
// context, headed --- from / +++ to; empty when they are equal. Lines are
// compared whole; a missing final newline is marked as diff does.
func Unified(from, to, a, b string) string {
	if a == b {
		return ""
	}
	al, bl := lines(a), lines(b)
	ops := edits(al, bl)
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", from, to)
	const ctx = 3
	i := 0
	for i < len(ops) {
		// Skip equal runs until a change.
		for i < len(ops) && ops[i].kind == ' ' {
			i++
		}
		if i >= len(ops) {
			break
		}
		start := max(0, i-ctx)
		end := i
		// Extend over changes separated by at most 2*ctx equal lines.
		for {
			for end < len(ops) && ops[end].kind != ' ' {
				end++
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' && run-end < 2*ctx {
				run++
			}
			if run < len(ops) && ops[run].kind != ' ' {
				end = run
				continue
			}
			end = min(len(ops), end+ctx)
			break
		}
		hunk := ops[start:end]
		aStart, bStart := hunk[0].ai, hunk[0].bi
		var aN, bN int
		for _, op := range hunk {
			if op.kind != '+' {
				aN++
			}
			if op.kind != '-' {
				bN++
			}
		}
		fmt.Fprintf(&out, "@@ -%s +%s @@\n", span(aStart, aN), span(bStart, bN))
		for _, op := range hunk {
			out.WriteByte(op.kind)
			out.WriteString(op.text)
			if !strings.HasSuffix(op.text, "\n") {
				out.WriteString("\n\\ No newline at end of file\n")
			}
		}
		i = end
	}
	return out.String()
}

func span(start, n int) string {
	if n == 1 {
		return fmt.Sprint(start + 1)
	}
	if n == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	return fmt.Sprintf("%d,%d", start+1, n)
}

// lines splits text keeping each line's newline.
func lines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.SplitAfter(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// edit is one diff line: ' ' kept, '-' removed from a, '+' added from b,
// with the 0-based indexes into a and b where it sits.
type edit struct {
	kind   byte
	text   string
	ai, bi int
}

// edits is the longest-common-subsequence diff of a and b.
func edits(a, b []string) []edit {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []edit
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			out = append(out, edit{' ', a[i], i, j})
			i++
			j++
		case i < n && (j >= m || lcs[i+1][j] >= lcs[i][j+1]):
			out = append(out, edit{'-', a[i], i, j})
			i++
		default:
			out = append(out, edit{'+', b[j], i, j})
			j++
		}
	}
	return out
}
