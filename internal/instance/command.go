package instance

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/proc"
	"github.com/rickdiculousli/buildermesh/internal/shell"
)

type command struct{ k kind.Kind }

// NewCommand returns an instance that runs k.Command through the platform
// shell with the job's args appended.
func NewCommand(k kind.Kind) Instance {
	k.Command = strings.TrimRight(k.Command, " \t\r\n;")
	return command{k}
}

func (c command) Run(ctx context.Context, j Job) Reply {
	var out bytes.Buffer
	ws := []io.Writer{&out}
	if j.Out != nil {
		ws = append(ws, j.Out)
	}
	var watch *proc.Watch
	if j.Checkup != nil {
		watch = proc.NewWatch(c.k.Timeout, j.Checkup)
		ws = append(ws, watch)
	}
	w := io.MultiWriter(ws...) // one writer for all, so exec copies the output in one goroutine
	note, err := run(ctx, c.k.Timeout, j, func(ctx context.Context) *exec.Cmd { return shell.Command(ctx, c.k.Command, j.Args...) }, nil, w, w, watch)
	if note != "" {
		return Reply{Judged: true, Output: out.String() + newline(out.String()) + note}
	}
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) {
		out.WriteString(newline(out.String()) + "[" + err.Error() + "]\n") // sh did not start
	}
	return Reply{OK: err == nil, Judged: true, Output: out.String()}
}
