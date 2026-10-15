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
type Job struct {
	ID, Kind, Commit, Tree, From, Reply string
	Args                                []string
	Text, Dir                           string
}

// Reply is a job's outcome. Context is the model's context tokens (0 for commands).
type Reply struct {
	OK      bool
	Output  string
	Context int
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

// run runs argv in j.Dir with the job's environment, in its own process
// group, killing the group at timeout (0: none) or when ctx ends, and again
// after the command exits so no descendant outlives the job. It returns a
// note for the output when the run was killed, and the run's error; a command
// that exited while a leftover child held its output is not an error.
func run(ctx context.Context, timeout time.Duration, j Job, argv []string, stdin io.Reader, stdout, stderr io.Writer) (string, error) {
	runCtx, cancel := ctx, context.CancelFunc(func() {})
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
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
