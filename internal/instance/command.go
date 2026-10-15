package instance

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/kind"
)

type command struct{ k kind.Kind }

// NewCommand returns an instance that runs k.Command with `sh -c`, the job's
// args as "$@".
func NewCommand(k kind.Kind) Instance {
	k.Command = strings.TrimRight(k.Command, " \t\r\n;")
	return command{k}
}

func (c command) Run(ctx context.Context, j Job) Reply {
	argv := append([]string{"sh", "-c", c.k.Command + ` "$@"`, "sh"}, j.Args...)
	var out bytes.Buffer
	note, err := run(ctx, c.k.Timeout, j, argv, nil, &out, &out)
	if note != "" {
		return Reply{Output: out.String() + newline(out.String()) + note}
	}
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) {
		out.WriteString(newline(out.String()) + "[" + err.Error() + "]\n") // sh did not start
	}
	return Reply{OK: err == nil, Output: out.String()}
}
