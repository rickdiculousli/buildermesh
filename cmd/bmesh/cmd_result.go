package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// nameRE is the grammar of kind and slot names.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// maxOutput bounds a recorded output; longer output is cut and marked.
const maxOutput = 64 << 10

const resultUsage = "usage: bmesh result find <kind> [--commit C] [--json] [-- args...] | " +
	"bmesh result record <kind> --ok|--fail [--commit C] [--output FILE|-] [-- args...]"

func (a *app) cmdResult(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "find":
			return a.cmdResultFind(args[1:])
		case "record":
			return a.cmdResultRecord(args[1:])
		}
	}
	return fail(exitUsage, resultUsage)
}

// gitRev runs git rev-parse in the cwd and returns its one line of output.
func gitRev(args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"rev-parse"}, args...)...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git rev-parse %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// kindArg checks the kind positional; the rest are the job args.
func kindArg(pos []string) (string, []string, error) {
	if len(pos) == 0 || !nameRE.MatchString(pos[0]) {
		return "", nil, fail(exitUsage, resultUsage)
	}
	return pos[0], pos[1:], nil
}

// cmdResultFind prints the result of a kind at the commit's tree.
func (a *app) cmdResultFind(args []string) error {
	fs := a.flags("result find")
	commit := fs.String("commit", "HEAD", "commit whose tree to look up")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	kind, jobArgs, err := kindArg(pos)
	if err != nil {
		return err
	}
	tree, err := gitRev(*commit + "^{tree}")
	if err != nil {
		return err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "result"); err != nil {
		return err
	}
	key := mesh.ResultKey(kind, tree, jobArgs)
	r, ok, err := mesh.GetResult(ctx, a.client, key)
	if err != nil {
		return err
	}
	if !ok {
		return fail(exitRefused, "no result for %s", key)
	}
	if a.json {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "%s\n", b)
		return nil
	}
	verdict := "fail"
	if r.OK {
		verdict = "ok"
	}
	fmt.Fprintf(a.stdout, "%s %s %s %s\n", verdict, r.Tree, r.At, r.By)
	return nil
}

// cmdResultRecord stores a result of a kind at the commit's tree. The kind
// file need not exist (R8): any name may be recorded.
func (a *app) cmdResultRecord(args []string) error {
	fs := a.flags("result record")
	ok := fs.Bool("ok", false, "the run passed")
	failed := fs.Bool("fail", false, "the run failed")
	commit := fs.String("commit", "HEAD", "commit the run checked")
	outPath := fs.String("output", "", "file holding the run's output (- reads stdin)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	kind, jobArgs, err := kindArg(pos)
	if err != nil {
		return err
	}
	if *ok == *failed {
		return fail(exitUsage, "result record: exactly one of --ok and --fail")
	}
	sha, err := gitRev("--verify", "--end-of-options", *commit+"^{commit}")
	if err != nil {
		return err
	}
	tree, err := gitRev(sha + "^{tree}")
	if err != nil {
		return err
	}
	output, err := a.readOutput(*outPath)
	if err != nil {
		return err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	by, err := a.identityOrHost(ctx, "result")
	if err != nil {
		return err
	}
	r := mesh.Result{Kind: kind, Tree: tree, Commit: sha, Args: jobArgs, OK: *ok, Output: output, By: by}
	if err := mesh.PutResult(ctx, a.client, r); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "recorded %s\n", mesh.ResultKey(kind, tree, jobArgs))
	return nil
}

// readOutput reads the --output source ("" is none), cut to maxOutput.
func (a *app) readOutput(path string) (string, error) {
	var b []byte
	var err error
	switch path {
	case "":
		return "", nil
	case "-":
		b, err = io.ReadAll(a.stdin)
	default:
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("output: %w", err)
	}
	return truncateOutput(string(b)), nil
}

func truncateOutput(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + "\n[truncated]"
}
