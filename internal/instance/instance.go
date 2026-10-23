// Package instance runs routine jobs: a shell command, or a claude -p session
// that persists across jobs.
package instance

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/rickdiculousli/buildermesh/internal/proc"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Job is one routine job as an instance sees it. From is the path the job
// runs as; Reply is the requester's path. Dir is the checkout to run in.
// Effort is a model kind's effort for this job ("": the model's default).
type Job struct {
	ID, Kind, Commit, Tree, From, Reply string
	Args                                []string
	Text, Dir, Effort                   string
}

// Reply is a job's outcome. Judged is whether the run judged the job: a
// command's reply always does, its exit code being the verdict; a model's
// only when claude returned a result that isn't an error. An unjudged reply
// is never recorded. Context is the model's context tokens (0 for commands).
type Reply struct {
	OK, Judged bool
	Output     string
	Context    int
}

// UnjudgedPrefix starts the answer for a reply that judged nothing.
const UnjudgedPrefix = "model run failed: "

// Unjudged is the answer for a reply that judged nothing:
// "model run failed: <the output's first non-empty line>; nothing recorded".
func (r Reply) Unjudged() string {
	why := ""
	for _, line := range strings.Split(r.Output, "\n") {
		if why = strings.TrimSpace(line); why != "" {
			break
		}
	}
	return UnjudgedPrefix + why + "; nothing recorded"
}

// Instance runs jobs.
type Instance interface {
	Run(ctx context.Context, j Job) Reply
}

func (j Job) env() []string {
	return append(os.Environ(),
		"BMESH_JOB="+j.ID, "BMESH_KIND="+j.Kind, "BMESH_COMMIT="+j.Commit,
		"BMESH_TREE="+j.Tree, "BMESH_FROM="+j.From, "BMESH_REPLY="+j.Reply)
}

// run runs the command mk makes (with the run's context) in j.Dir with the
// job's environment, in its own process
// group, killing the group at timeout (0: none) or when ctx ends, and again
// after the command exits so no descendant outlives the job. It returns a
// note for the output when the run was killed, and the run's error; a command
// that exited while a leftover child held its output is not an error.
func run(ctx context.Context, timeout time.Duration, j Job, mk func(context.Context) *exec.Cmd, stdin io.Reader, stdout, stderr io.Writer) (string, error) {
	runCtx, cancel := ctx, context.CancelFunc(func() {})
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	cmd := mk(runCtx)
	cmd.Dir, cmd.Env = j.Dir, j.env()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	proc.Group(cmd)
	var note string // written by Cancel, which runs before Wait returns
	cmd.Cancel = func() error {
		note = fmt.Sprintf("[timeout after %s]\n", timeout)
		if ctx.Err() != nil {
			note = "[cancelled]\n"
		}
		return proc.KillGroup(cmd)
	}
	cmd.WaitDelay = time.Second // a leftover child may hold the output open
	err := cmd.Run()
	proc.KillGroup(cmd) // nothing left when the group is already gone
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState.Success() {
		err = nil
	}
	return note, err
}

// newline returns "\n" when s is non-empty and does not end in one.
func newline(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		return "\n"
	}
	return ""
}

// uuid4 returns a random RFC 4122 version 4 UUID.
func uuid4() string {
	var b [16]byte
	rand.Read(b[:]) // never fails since Go 1.24
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
