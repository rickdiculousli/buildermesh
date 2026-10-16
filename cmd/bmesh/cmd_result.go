package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const (
	resultFindUsage   = "usage: bmesh result find <kind> [--commit C] [--model M] [--json] [-- args...]"
	resultRecordUsage = "usage: bmesh result record <kind> --ok|--fail [--commit C] [--model M] [--output FILE|-] [-- args...]"
	resultUsage       = resultFindUsage + " | " + resultRecordUsage
)

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

// checkModel refuses a --model value that is not a model id (it would
// break a verified line's m:<model>).
func checkModel(model string) error {
	if model != "" && !kind.ModelRE.MatchString(model) {
		return fail(exitUsage, "model %q: want no spaces or \";\"", model)
	}
	return nil
}

// checkEffort refuses an effort level the runner cannot start a thread at.
func checkEffort(effort string) error {
	if err := config.CheckEffort(effort); err != nil {
		return fail(exitUsage, "%v", err)
	}
	return nil
}

// kindArg checks the kind positional; the rest are the job args. usage is
// the subcommand's.
func kindArg(pos []string, usage string) (string, []string, error) {
	if len(pos) == 0 || !kind.NameRE.MatchString(pos[0]) {
		return "", nil, fail(exitUsage, "%s", usage)
	}
	return pos[0], pos[1:], nil
}

// cmdResultFind prints the result of a kind at the commit's tree; without
// --model, every model's result there (a command kind's has none).
func (a *app) cmdResultFind(args []string) error {
	fs := a.flags("result find")
	commit := fs.String("commit", "HEAD", "commit whose tree to look up")
	model := fs.String("model", "", "the model of a model kind's result")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	name, jobArgs, err := kindArg(pos, resultFindUsage)
	if err != nil {
		return err
	}
	tree, err := gitRev("--verify", "--end-of-options", *commit+"^{tree}")
	if err != nil {
		return err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "result"); err != nil {
		return err
	}
	var found []mesh.Result
	if *model != "" {
		r, ok, err := mesh.GetResult(ctx, a.client, mesh.ResultKey(name, tree, jobArgs, *model))
		if err != nil {
			return err
		}
		if ok {
			found = append(found, r)
		}
	} else {
		rs, err := mesh.ListResults(ctx, a.client, name)
		if err != nil {
			return err
		}
		for _, r := range rs {
			if r.Tree == tree && mesh.ResultKey(name, tree, r.Args, r.Model) == mesh.ResultKey(name, tree, jobArgs, r.Model) {
				found = append(found, r)
			}
		}
	}
	if len(found) == 0 {
		return fail(exitRefused, "no result for %s", mesh.ResultKey(name, tree, jobArgs, *model))
	}
	for _, r := range found {
		if a.json {
			if err := a.printJSON(r); err != nil {
				return err
			}
			continue
		}
		line := fmt.Sprintf("%s %s %s %s", okWord(r.OK), r.Tree, r.At, r.By)
		if r.Model != "" {
			line += " m:" + r.Model
		}
		fmt.Fprintln(a.stdout, line)
	}
	return nil
}

// cmdResultRecord stores a result of a kind at the commit's tree. The kind
// file need not exist (R8): any name may be recorded. A result for HEAD
// needs a clean worktree: the run checked the files, which must be HEAD's.
func (a *app) cmdResultRecord(args []string) error {
	fs := a.flags("result record")
	ok := fs.Bool("ok", false, "the run passed")
	failed := fs.Bool("fail", false, "the run failed")
	commit := fs.String("commit", "HEAD", "commit the run checked")
	model := fs.String("model", "", "the model that ran, for a model kind")
	outPath := fs.String("output", "", "file holding the run's output (- reads stdin)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	name, jobArgs, err := kindArg(pos, resultRecordUsage)
	if err != nil {
		return err
	}
	if *ok == *failed {
		return fail(exitUsage, "result record: exactly one of --ok and --fail; "+resultRecordUsage)
	}
	if err := checkModel(*model); err != nil {
		return err
	}
	sha, err := gitRev("--verify", "--end-of-options", *commit+"^{commit}")
	if err != nil {
		return err
	}
	tree, err := gitRev(sha + "^{tree}")
	if err != nil {
		return err
	}
	if head, err := gitRev("--verify", "--end-of-options", "HEAD^{commit}"); err == nil && head == sha {
		if err := refuseDirty(); err != nil {
			return err
		}
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
	r := mesh.Result{Kind: name, Tree: tree, Commit: sha, Args: jobArgs, OK: *ok, Output: output, By: by, Model: *model}
	if err := mesh.PutResult(ctx, a.client, r); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "recorded %s\n", mesh.ResultKey(name, tree, jobArgs, *model))
	return nil
}

// readOutput reads the --output source ("" is none), cut by mesh.Truncate.
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
	return mesh.Truncate(string(b)), nil
}
