package spec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/shell"
)

// CheckTimeout bounds one step's check command.
const CheckTimeout = 2 * time.Minute

// RunCheck runs a step's check command through the platform shell in dir,
// for at most CheckTimeout: ok on exit 0, with `exit 0`, else `exit <n>`
// (or the error) and the output's last line.
func RunCheck(ctx context.Context, dir, command string) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	cmd := shell.Command(ctx, command)
	cmd.Dir = dir
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, "exit 0"
	}
	what := err.Error()
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) && ee.ExitCode() >= 0 {
		what = "exit " + strconv.Itoa(ee.ExitCode())
	}
	if ctx.Err() != nil {
		what = "timed out after " + CheckTimeout.String()
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		what += ": " + last
	}
	return false, what
}

// CheckStep is a received check-step job's work on the box it names: spec
// id's step n, read from the repo's local origin/main (main without an
// origin; the caller fetches), must be this box's (host) and have a check,
// which runs in repoPath; ok on exit 0.
func CheckStep(ctx context.Context, repoPath, glob, host, id string, n int) (bool, string) {
	p, text, err := FindOnMainLocal(ctx, repoPath, glob, id)
	if err != nil {
		return false, "refused: " + err.Error()
	}
	s, err := Parse(text, p)
	if err != nil {
		return false, "refused: " + err.Error()
	}
	steps, err := s.Steps()
	if err != nil {
		return false, "refused: " + err.Error()
	}
	if n < 1 || n > len(steps) {
		return false, fmt.Sprintf("refused: spec %s has no step %d", id, n)
	}
	st := steps[n-1]
	switch {
	case st.Box != host:
		return false, fmt.Sprintf("refused: step %d is for %q, not %s", n, st.Box, host)
	case st.Check == "":
		return false, fmt.Sprintf("refused: step %d has no check", n)
	}
	if _, err := os.Stat(repoPath); err != nil {
		return false, "refused: " + err.Error()
	}
	return RunCheck(ctx, repoPath, st.Check)
}
