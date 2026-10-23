package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/gitfetch"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// UpstreamURL is where the module's main is read from: https://<module>, or
// $BMESH_UPSTREAM_URL (a test hook) when set.
func UpstreamURL(module string) string {
	if u := os.Getenv("BMESH_UPSTREAM_URL"); u != "" {
		return u
	}
	return "https://" + module
}

// UpstreamDir is the bare repo the module's main is fetched into, under the
// user cache dir.
func UpstreamDir(cache string) string { return filepath.Join(cache, "bmesh", "update", "upstream") }

// FetchUpstream fetches the module's main and its release tags into the
// bare repo dir (made when missing) and returns main's commit, with dir
// under Lock, which the caller releases once it is done reading or
// building from it.
func FetchUpstream(dir, module string) (head string, unlock func(), err error) {
	if module == "" {
		return "", nil, errors.New("this binary does not say its module")
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", nil, err
	}
	if unlock, err = Lock(dir); err != nil {
		return "", nil, err
	}
	if _, err = os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		if err = os.RemoveAll(dir); err == nil {
			_, err = git(filepath.Dir(dir), "init", "--quiet", "--bare", "--end-of-options", dir)
		}
	}
	if err == nil {
		_, err = git(dir, gitfetch.Args("--quiet", "--end-of-options", UpstreamURL(module), "+refs/heads/main:refs/heads/main", "+refs/tags/v*:refs/tags/v*")...)
	}
	if err == nil {
		head, err = git(dir, "rev-parse", "--verify", "refs/heads/main^{commit}")
	}
	if err != nil {
		unlock()
		return "", nil, err
	}
	return head, unlock, nil
}

// InstallModule builds <module>/cmd/bmesh at t with `go install` into a
// temporary directory beside exe (GOBIN), `@<tag>` for a release and
// `@<commit>` for a code head, stamped with t's commit and its commit time
// from dir (the fetched main) as Install's build is, so a build go
// resolves to a tag still names its commit; then it refuses, gates or swaps
// the build in as Install does.
func InstallModule(ctx context.Context, dir, module, exe string, t Target, gate Gate) error {
	ctx, cancel := context.WithTimeout(ctx, BuildTimeout)
	defer cancel()
	head := t.Commit
	ldflags, err := stampFlags(dir, head)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(exe), ".bmesh-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	argv := append(goCommand(), "install", "-ldflags", ldflags, module+"/cmd/bmesh@"+t.Ref())
	if Scoped() {
		argv = append([]string{"systemd-run", "--user", "--scope", "--quiet", "--collect", "--"}, argv...)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = tmp // outside any module
	cmd.Env = append(os.Environ(), "GOBIN="+tmp)
	cmd.WaitDelay = 5 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(argv, " "), err, tail(string(out)))
	}
	built := filepath.Join(tmp, "bmesh")
	if runtime.GOOS == "windows" {
		built += ".exe"
	}
	return swap(ctx, built, exe, head, gate)
}

// Installed is the revision the binary at exe reports (`<rev8>`), its
// release when that is all it reports (a tagged module install, for
// OwnCommit), or "" when it reports neither.
func Installed(ctx context.Context, exe string) string {
	if exe == "" {
		return ""
	}
	if r := reports(ctx, exe); hex8.MatchString(r) || IsRelease(r) {
		return r
	}
	return ""
}

var hex8 = regexp.MustCompile(`^[0-9a-f]{8}$`)

// doneAt matches a landing's `NNN: done at <sha8>` subject.
var doneAt = regexp.MustCompile(`^([0-9]{3,}): done at ([0-9a-f]{7,40})`)

// Notice is the line a binary at own (a commit in dir) reads when it is
// behind t, read on ref (main): for a release, `bmesh: v0.2.0 is out (you
// run v0.1.0: 046, 047); run bmesh update, then update the buildermesh
// plugin`; for a code head, `bmesh: N commits behind main (landed since:
// NNN <title>, …); run …`, N the commits in own..t that change code
// (pathspecs as CodePaths gives them) and each spec titled from its file on ref. The specs are
// those whose `NNN: done at <sha8>` landings in own..ref name a merged
// commit t has and own hasn't (or one dir doesn't know), oldest first. It
// is "" when own is not behind t (Stand): current, ahead, or not in its
// history.
func Notice(dir, own, ref string, t Target, code []string) (string, error) {
	if Stand(dir, own, t.Commit) != Behind {
		return "", nil
	}
	all, err := landings(dir, own+".."+ref)
	if err != nil {
		return "", err
	}
	var ids []string
	for _, l := range all {
		if _, err := git(dir, "rev-parse", "--verify", "-q", "--end-of-options", l.at+"^{commit}"); err != nil ||
			(Contains(dir, l.at, t.Commit) && !Contains(dir, l.at, own)) {
			ids = append(ids, l.id)
		}
	}
	tail := "; run bmesh update, then update the buildermesh plugin"
	if t.Tag != "" {
		line := fmt.Sprintf("bmesh: %s is out (you run %s", t.Tag, Label(dir, own))
		if len(ids) > 0 {
			line += ": " + strings.Join(ids, ", ")
		}
		return line + ")" + tail, nil
	}
	n, err := git(dir, append([]string{"rev-list", "--count", own + ".." + t.Commit, "--"}, code...)...)
	if err != nil {
		return "", err
	}
	count, err := strconv.Atoi(n)
	if err != nil || count == 0 {
		return "", err
	}
	var specs []string
	for _, id := range ids {
		specs = append(specs, specTitle(dir, ref, id))
	}
	line := fmt.Sprintf("bmesh: %d commits behind main", count)
	if count == 1 {
		line = "bmesh: 1 commit behind main"
	}
	if len(specs) > 0 {
		line += " (landed since: " + strings.Join(specs, ", ") + ")"
	}
	return line + tail, nil
}

// Landed is the ids of the specs whose `NNN: done at` subjects are in
// revs (a git revision range, or a commit and its history), oldest first,
// each once.
func Landed(dir, revs string) ([]string, error) {
	all, err := landings(dir, revs)
	var ids []string
	for _, l := range all {
		ids = append(ids, l.id)
	}
	return ids, err
}

// landing is a spec's `NNN: done at <sha8>` subject: its id and the merged
// commit it names.
type landing struct{ id, at string }

// landings is the `NNN: done at` subjects in revs, oldest first, each id
// once.
func landings(dir, revs string) ([]landing, error) {
	subjects, err := git(dir, "log", "--reverse", "--format=%s", "--end-of-options", revs)
	if err != nil {
		return nil, err
	}
	var out []landing
	seen := map[string]bool{}
	for _, s := range strings.Split(subjects, "\n") {
		m := doneAt.FindStringSubmatch(s)
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, landing{id: m[1], at: m[2]})
	}
	return out, nil
}

// specTitle is `NNN <title>` from docs/proposals/NNN-*.md on head, or the
// id alone when that is not there.
func specTitle(dir, head, id string) string {
	names, err := git(dir, "ls-tree", "--name-only", head, "docs/proposals/")
	if err != nil {
		return id
	}
	for _, p := range strings.Split(names, "\n") {
		if !strings.HasPrefix(filepath.Base(p), id+"-") || !strings.HasSuffix(p, ".md") {
			continue
		}
		text, err := git(dir, "show", head+":"+p)
		if err != nil {
			return id
		}
		if fm, err := spec.Frontmatter(text); err == nil && fm["title"] != "" {
			return id + " " + fm["title"]
		}
		return id
	}
	return id
}

// NoticePath is the notice's file: <state dir>/update.txt.
func NoticePath(stateDir string) string { return filepath.Join(stateDir, "update.txt") }

// WriteNotice writes line to NoticePath (through a temporary file and a
// rename), or removes it when line is "".
func WriteNotice(stateDir, line string) error {
	p := NoticePath(stateDir)
	if line == "" {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(stateDir, ".update-*")
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), p); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}

// ReadNotice is the notice's line, "" when there is none.
func ReadNotice(stateDir string) string {
	b, err := os.ReadFile(NoticePath(stateDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
