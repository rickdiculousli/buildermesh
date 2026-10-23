package checkout

import (
	"context"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// rebaseRE matches a reflog entry a finished rebase wrote on the branch
// (rebase, rebase -i, pull --rebase; "rebase finished" from older git).
var rebaseRE = regexp.MustCompile(`^(rebase|pull)\b[^:]*\(finish\): |^rebase finished: `)

// Rebased is one rebase on a branch: Head the commit it rewrote, Rebase the
// commit it made of it.
type Rebased struct{ Head, Rebase string }

// RebasedFrom reads the branch's reflog in dir, newest first, for a rebase
// that finished at head and returns the head it rewrote (the entry just
// before it), and so on back while that head was itself made by a rebase:
// the heads land would have rebased itself, nearest first. A head with
// commits on top of the rebase, or a branch with no reflog, returns none.
func RebasedFrom(ctx context.Context, dir, branch, head string) []Rebased {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "reflog", "show", "--format=%H%x00%gs", "refs/heads/"+branch, "--").Output()
	if err != nil {
		return nil
	}
	type entry struct{ sha, subject string }
	var log []entry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if sha, subject, ok := strings.Cut(line, "\x00"); ok {
			log = append(log, entry{sha, subject})
		}
	}
	var heads []Rebased
	for i, cur := 0, head; ; {
		k := slices.IndexFunc(log[i:], func(e entry) bool { return e.sha == cur && rebaseRE.MatchString(e.subject) })
		if k < 0 || i+k+1 >= len(log) || log[i+k+1].sha == cur {
			return heads
		}
		i += k + 1
		heads = append(heads, Rebased{Head: log[i].sha, Rebase: cur})
		cur = log[i].sha
	}
}
