package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The ledger: a repo's proposal specs, read and edited by these rules. A
// spec's frontmatter is flat
// `key: value` lines between `---` fences (lines starting with `#` are
// ignored); there is no other record, so a file that can't be read is
// reported as malformed, never skipped or guessed at.

// Statuses a spec may carry, in lifecycle order.
var Statuses = []string{"proposed", "accepted", "adjusting", "later", "rejected", "building", "blocked", "done"}

// Judges maps a spec's judge to the model that builds it.
var Judges = map[string]string{"fable": "claude-fable-5-1", "opus": "claude-opus-5-5", "sonnet": "claude-sonnet-5-5"}

// JudgeNames is Judges' keys, sorted.
func JudgeNames() []string {
	out := make([]string, 0, len(Judges))
	for j := range Judges {
		out = append(out, j)
	}
	sort.Strings(out)
	return out
}

// ValidStatus reports whether s is one of Statuses.
func ValidStatus(s string) bool {
	for _, x := range Statuses {
		if x == s {
			return true
		}
	}
	return false
}

// LedgerError is a malformed spec or a refused edit. Shared names the two
// files of a duplicate id, and ID that id, when that is what is wrong.
type LedgerError struct {
	Msg    string
	Shared [2]string
	ID     int
}

func (e *LedgerError) Error() string { return e.Msg }

func ledgerErr(format string, a ...any) error { return &LedgerError{Msg: fmt.Sprintf(format, a...)} }

var (
	keyRE  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	fileRE = regexp.MustCompile(`^(\d+)-(.+)$`)
)

// Spec is one spec file as read.
type Spec struct {
	Path   string            // the file
	ID     int               // frontmatter id
	Slug   string            // from the file name
	Fields map[string]string // frontmatter, in Order
	Order  []string          // frontmatter keys as they appear
	Body   string            // everything after the closing fence, leading blank lines dropped
	Text   string            // the file as read, so edits keep everything else
}

// IDString is the id as three digits.
func (s *Spec) IDString() string { return fmt.Sprintf("%03d", s.ID) }

// Status is the status field.
func (s *Spec) Status() string { return s.Fields["status"] }

// Title is the title field, else the slug.
func (s *Spec) Title() string {
	if t := s.Fields["title"]; t != "" {
		return t
	}
	return s.Slug
}

// Row is the one-line listing: id, status, size, kind, lens, title.
func (s *Spec) Row() string {
	f := s.Fields
	get := func(k, def string) string {
		if v, ok := f[k]; ok && v != "" {
			return v
		}
		return def
	}
	return fmt.Sprintf("%03d  %-9s  %-2s  %-11s  %-3s  %s", s.ID, f["status"], get("size", "?"), get("kind", "?"),
		get("lens", "?"), s.Title())
}

// JSON is the frontmatter plus the path, as proposals.py list --json printed it.
func (s *Spec) JSON() map[string]string {
	out := make(map[string]string, len(s.Fields)+1)
	for k, v := range s.Fields {
		out[k] = v
	}
	out["path"] = s.Path
	return out
}

// split returns the frontmatter lines (without their newlines) and the rest
// of the text after the closing fence.
func split(text, name string) (front []string, rest string, err error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\n") != "---" {
		return nil, "", ledgerErr("malformed: %s: no opening `---` fence", name)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\n") == "---" {
			for _, l := range lines[1:i] {
				front = append(front, strings.TrimRight(l, "\n"))
			}
			return front, strings.Join(lines[i+1:], ""), nil
		}
	}
	return nil, "", ledgerErr("malformed: %s: no closing `---` fence", name)
}

// Parse reads one spec's text; path names it (its base name carries the id
// and slug). It refuses what proposals.py refused: a missing or non-numeric
// id, a status outside Statuses, a judge outside Judges, an id that does not
// match the file name's.
func Parse(text, path string) (*Spec, error) {
	name := filepath.Base(path)
	front, rest, err := split(text, name)
	if err != nil {
		return nil, err
	}
	s := &Spec{Path: path, Fields: map[string]string{}, Text: text}
	for _, line := range front {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		k = strings.TrimSpace(k)
		if !ok || !keyRE.MatchString(k) {
			return nil, ledgerErr("malformed: %s: not a `key: value` line: %q", name, line)
		}
		if _, seen := s.Fields[k]; !seen {
			s.Order = append(s.Order, k)
		}
		s.Fields[k] = strings.TrimSpace(v)
	}
	id, err := strconv.Atoi(s.Fields["id"])
	if err != nil || !regexp.MustCompile(`^\d+$`).MatchString(s.Fields["id"]) {
		return nil, ledgerErr("malformed: %s: missing or non-numeric id", name)
	}
	s.ID = id
	if st := s.Fields["status"]; !ValidStatus(st) {
		return nil, ledgerErr("malformed: %s: status %q isn't one of %s", name, st, strings.Join(Statuses, ", "))
	}
	if j, ok := s.Fields["judge"]; ok {
		if _, known := Judges[j]; !known {
			return nil, ledgerErr("malformed: %s: judge %q isn't one of %s", name, j, strings.Join(JudgeNames(), ", "))
		}
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if m := fileRE.FindStringSubmatch(stem); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n != id {
			return nil, ledgerErr("malformed: %s: id %s doesn't match the file name's %s", name, s.Fields["id"], m[1])
		}
		s.Slug = m[2]
	} else {
		s.Slug = stem
	}
	s.Body = strings.TrimLeft(rest, "\r\n")
	return s, nil
}

// ModelFor is the model id the spec's judge builds on.
func ModelFor(s *Spec) (string, error) {
	m, ok := Judges[s.Fields["judge"]]
	if !ok {
		return "", ledgerErr("spec %03d names no judge (one of %s)", s.ID, strings.Join(JudgeNames(), ", "))
	}
	return m, nil
}

// Dir is the proposals directory a spec glob names (docs/proposals for
// docs/proposals/{id}-*.md).
func Dir(glob string) string { return filepath.FromSlash(filepath.Dir(filepath.ToSlash(glob))) }

// Scan reads every spec under repo's dir (the glob's directory), sorted by
// id; README.md is not a spec. The first malformed file or duplicate id is
// an error. A missing directory is an empty ledger.
func Scan(repo, glob string) ([]*Spec, error) {
	dir := filepath.Join(repo, Dir(glob))
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var specs []*Spec
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" || e.Name() == "README.md" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		s, err := Parse(string(b), p)
		if err != nil {
			return nil, err
		}
		specs = append(specs, s)
	}
	return checkIDs(specs)
}

func checkIDs(specs []*Spec) ([]*Spec, error) {
	sort.SliceStable(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	for i := 1; i < len(specs); i++ {
		if specs[i-1].ID == specs[i].ID {
			a, b := filepath.Base(specs[i-1].Path), filepath.Base(specs[i].Path)
			return nil, &LedgerError{Msg: fmt.Sprintf("malformed: %s and %s share id %03d", a, b, specs[i].ID),
				Shared: [2]string{a, b}, ID: specs[i].ID}
		}
	}
	return specs, nil
}

// ScanRef reads the ledger as it stands on a git ref of repo (origin/main,
// say) rather than in a working tree; the specs' paths are repo-relative.
func ScanRef(repo, glob, ref string) ([]*Spec, error) {
	dir := filepath.ToSlash(Dir(glob))
	out, err := git(repo, "ls-tree", "--name-only", ref, "--", dir+"/")
	if err != nil {
		return nil, err
	}
	var specs []*Spec
	for _, p := range strings.Split(out, "\n") {
		if p == "" || !strings.HasSuffix(p, ".md") || strings.HasSuffix(p, "/README.md") {
			continue
		}
		text, err := git(repo, "show", ref+":"+p)
		if err != nil {
			return nil, err
		}
		s, err := Parse(text, p)
		if err != nil {
			return nil, err
		}
		specs = append(specs, s)
	}
	return checkIDs(specs)
}

// Find returns the spec with this id among specs.
func Find(specs []*Spec, id int) (*Spec, bool) {
	for _, s := range specs {
		if s.ID == id {
			return s, true
		}
	}
	return nil, false
}

// NextID is one past the highest id (1 for an empty ledger).
func NextID(specs []*Spec) int {
	n := 0
	for _, s := range specs {
		n = max(n, s.ID)
	}
	return n + 1
}

// Count is how many specs have this status.
func Count(specs []*Spec, status string) int {
	n := 0
	for _, s := range specs {
		if s.Status() == status {
			n++
		}
	}
	return n
}

// Field is one frontmatter key and value to set.
type Field struct{ Key, Value string }

// ParseField reads KEY=VALUE.
func ParseField(kv string) (Field, error) {
	k, v, ok := strings.Cut(kv, "=")
	if !ok {
		return Field{}, ledgerErr("--field wants KEY=VALUE, got %q", kv)
	}
	return Field{strings.TrimSpace(k), strings.TrimSpace(v)}, nil
}

// Update is the spec's text with these frontmatter keys set: an existing
// key keeps its place, a new one goes last; everything else stays byte for
// byte. A key that is not a word, or a value with a line break, is refused.
func Update(s *Spec, fields ...Field) (string, error) {
	front, rest, err := split(s.Text, filepath.Base(s.Path))
	if err != nil {
		return "", err
	}
	todo := map[string]string{}
	var order []string
	for _, f := range fields {
		if !keyRE.MatchString(f.Key) || strings.ContainsAny(f.Value, "\r\n") {
			return "", ledgerErr("can't set %q to %q: keys are words and values are one line", f.Key, f.Value)
		}
		if _, ok := todo[f.Key]; !ok {
			order = append(order, f.Key)
		}
		todo[f.Key] = f.Value
	}
	var b strings.Builder
	b.WriteString("---\n")
	for _, line := range front {
		k, _, _ := strings.Cut(line, ":")
		k = strings.TrimSpace(k)
		if v, ok := todo[k]; ok && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
			delete(todo, k)
			continue
		}
		b.WriteString(line + "\n")
	}
	for _, k := range order {
		if v, ok := todo[k]; ok {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	b.WriteString("---\n")
	b.WriteString(rest)
	return b.String(), nil
}

// CheckFrom refuses a spec whose status is not one of allowedFrom (a
// LedgerError naming its status); nil allows any.
func CheckFrom(s *Spec, allowedFrom []string) error {
	if allowedFrom == nil {
		return nil
	}
	cur := s.Status()
	for _, a := range allowedFrom {
		if a == cur {
			return nil
		}
	}
	sorted := append([]string{}, allowedFrom...)
	sort.Strings(sorted)
	return ledgerErr("spec %03d is %s, not %s; left unchanged", s.ID, cur, strings.Join(sorted, " or "))
}

// SetStatus writes the new status (and fields) to the spec's file and
// returns it re-read. With allowedFrom set, a spec whose status is not one
// of them is left unchanged (a LedgerError naming its status). A write the
// ledger would read back otherwise (a duplicated `status:` key, say) is
// refused too.
func SetStatus(s *Spec, status string, allowedFrom []string, fields ...Field) (*Spec, error) {
	if !ValidStatus(status) {
		return nil, ledgerErr("unknown status %q; use one of %s", status, strings.Join(Statuses, ", "))
	}
	if err := CheckFrom(s, allowedFrom); err != nil {
		return nil, err
	}
	text, err := Update(s, append([]Field{{"status", status}}, fields...)...)
	if err != nil {
		return nil, err
	}
	n, err := Parse(text, s.Path) // never write a file the ledger couldn't read back
	if err != nil {
		return nil, err
	}
	if n.Status() != status {
		return nil, ledgerErr("spec %03d: writing status %s would read back as %s; left unchanged", s.ID, status, n.Status())
	}
	if err := os.WriteFile(s.Path, []byte(text), 0o644); err != nil {
		return nil, err
	}
	return n, nil
}

// WriteBody writes the spec with its body replaced; the frontmatter stays.
func WriteBody(s *Spec, body string) (*Spec, error) {
	front, _, err := split(s.Text, filepath.Base(s.Path))
	if err != nil {
		return nil, err
	}
	text := "---\n" + strings.Join(front, "\n") + "\n---\n\n" + strings.TrimLeft(body, "\r\n")
	if len(front) == 0 {
		text = "---\n---\n\n" + strings.TrimLeft(body, "\r\n")
	}
	n, err := Parse(text, s.Path)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.Path, []byte(text), 0o644); err != nil {
		return nil, err
	}
	return n, nil
}

// MarshalJSON lists specs the way `spec list --json` prints them.
func MarshalJSON(specs []*Spec) ([]byte, error) {
	rows := make([]map[string]string, 0, len(specs))
	for _, s := range specs {
		rows = append(rows, s.JSON())
	}
	return json.MarshalIndent(rows, "", "  ")
}
