package hosts

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Held is a level waiting for the operator on a secure box (020), in
// <state dir>/held/<id>.json: the job (a self-level's is the box's own,
// signed with its host key), the target and git log --stat HEAD..target.
// A registry proposal (023, Kind HeldRegistry) waits on the admin box the
// same way: its job's args carry the proposed rows, Base the registry
// version they were proposed against and Log the row diff.
type Held struct {
	ID      string `json:"id"` // the job's id
	Kind    string `json:"kind,omitempty"`
	Base    int64  `json:"base,omitempty"`
	Job     Job    `json:"job"`
	Self    bool   `json:"self"` // the box's own self-level
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Head    string `json:"head"`
	Target  string `json:"target"`
	Commits int    `json:"commits"`
	Log     string `json:"log"`
	Created string `json:"created"`
}

// HeldRegistry is a registry proposal's hold kind; a level's is "".
const HeldRegistry = "registry"

// ProposeArgs is a propose job's args: the registry version the rows were
// proposed against, then the rows (repos.toml's text).
func ProposeArgs(base int64, rows string) []string {
	return []string{strconv.FormatInt(base, 10), rows}
}

// ParseProposeArgs reads ProposeArgs.
func ParseProposeArgs(args []string) (base int64, rows string, err error) {
	if len(args) != 2 {
		return 0, "", fmt.Errorf("propose takes 2 args, got %d", len(args))
	}
	base, err = strconv.ParseInt(args[0], 10, 64)
	if err != nil || base < 0 {
		return 0, "", fmt.Errorf("propose: base %q is not a version", args[0])
	}
	return base, args[1], nil
}

// From is who asked: the job's sender, or self-level.
func (h Held) From() string {
	if h.Self {
		return "self-level"
	}
	return h.Job.By
}

// Expired: the hold's job has expired (a hold lives as long as its job).
func (h Held) Expired(now time.Time) bool {
	exp, err := ParseStamp(h.Job.Expires)
	return err != nil || !now.Before(exp)
}

// Presence is h as the box's presence carries it.
func (h Held) Presence() PresenceHold {
	p := PresenceHold{Kind: h.Kind, Commits: h.Commits, ID: h.ID, Expires: h.Job.Expires}
	if h.Kind == HeldRegistry {
		p.From = h.Job.By
	} else {
		p.Repo = h.Repo
	}
	return p
}

// PresenceHold is one hold as its box's presence carries it (093), so
// every box's sessions and mesh-status can say it waits on the operator:
// a level's repo and commits, or a registry proposal's box and row
// changes (in Commits), with the hold's id and when it expires.
type PresenceHold struct {
	Kind    string `json:"kind,omitempty"` // "" a level, HeldRegistry a registry proposal
	Repo    string `json:"repo,omitempty"` // a level's repo
	From    string `json:"from,omitempty"` // a proposal's box
	Commits int    `json:"commits"`
	ID      string `json:"id"`
	Expires string `json:"expires"`
}

// Expired: the hold's job has expired, or its expiry doesn't read.
func (p PresenceHold) Expired(now time.Time) bool {
	exp, err := ParseStamp(p.Expires)
	return err != nil || !now.Before(exp)
}

// What is the hold in a few words: `buildermesh +62 commits`, or `a
// registry proposal from k16 (2 row changes)`.
func (p PresenceHold) What() string {
	if p.Kind == HeldRegistry {
		return fmt.Sprintf("a registry proposal from %s (%s)", p.From, rowChanges(p.Commits))
	}
	return fmt.Sprintf("%s +%d %s", p.Repo, p.Commits, plural(p.Commits, "commit"))
}

// Waits is the hold as waiting on the operator, the title of its push:
// `<host>: buildermesh waits for your approval (+62 commits)`, or
// `<host>: a registry proposal from k16 waits for your approval (2 row
// changes)`.
func (p PresenceHold) Waits(host string) string {
	if p.Kind == HeldRegistry {
		return fmt.Sprintf("%s: a registry proposal from %s waits for your approval (%s)", host, p.From, rowChanges(p.Commits))
	}
	return fmt.Sprintf("%s: %s waits for your approval (+%d %s)", host, p.Repo, p.Commits, plural(p.Commits, "commit"))
}

// Step is how the operator answers the hold, and when it expires (in this
// box's time zone): `In a terminal on <host>: bmesh hosts approve <id> (it
// shows the commits first), or bmesh hosts deny <id> to leave it as it is.
// Expires Sat 23:47.`
func (p PresenceHold) Step(host string) string {
	shows := "the commits"
	if p.Kind == HeldRegistry {
		shows = "the diff"
	}
	return fmt.Sprintf("In a terminal on %s: bmesh hosts approve %s (it shows %s first), or bmesh hosts deny %s to leave it as it is. Expires %s.",
		host, p.ID, shows, p.ID, p.ExpiresAt())
}

// ExpiresAt is the expiry as `Sat 23:47`, in this box's time zone.
func (p PresenceHold) ExpiresAt() string {
	exp, err := ParseStamp(p.Expires)
	if err != nil {
		return p.Expires
	}
	return exp.Local().Format("Mon 15:04")
}

func rowChanges(n int) string { return fmt.Sprintf("%d row %s", n, plural(n, "change")) }

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// Decision is what an approval of h signs: approve and the target the
// operator saw.
func (h Held) Decision() string { return "approve " + h.Target }

// HeldDir is <state dir>/held.
func HeldDir(stateDir string) string { return filepath.Join(stateDir, "held") }

func heldPath(stateDir, id string) (string, error) {
	if !tokenOK(id) {
		return "", fmt.Errorf("hold %q: not a job id", id)
	}
	return filepath.Join(HeldDir(stateDir), id+".json"), nil
}

// SaveHeld writes h.
func SaveHeld(stateDir string, h Held) error {
	p, err := heldPath(stateDir, h.ID)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(p, b)
}

// LoadHeld reads hold id; ok is false when there is none.
func LoadHeld(stateDir, id string) (h Held, ok bool, err error) {
	p, err := heldPath(stateDir, id)
	if err != nil {
		return Held{}, false, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return Held{}, false, nil
	}
	if err != nil {
		return Held{}, false, err
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return Held{}, false, fmt.Errorf("%s: %w", p, err)
	}
	return h, true, nil
}

// ListHeld is every hold, oldest first; an unreadable file is skipped.
func ListHeld(stateDir string) ([]Held, error) {
	ents, err := os.ReadDir(HeldDir(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Held
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		if h, ok, err := LoadHeld(stateDir, id); err == nil && ok {
			out = append(out, h)
		}
	}
	slices.SortFunc(out, func(a, b Held) int { return strings.Compare(a.Created+a.ID, b.Created+b.ID) })
	return out, nil
}

// DropHeld removes hold id; one already gone is no error.
func DropHeld(stateDir, id string) error {
	p, err := heldPath(stateDir, id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ApproveSubject is where a box's daemon takes approvals of its holds over
// core NATS request/reply: bmhosts.approve.<host>, outside bm.>, so nothing
// of it is stored.
func ApproveSubject(host string) string { return "bmhosts.approve." + host }

// ApproveAnswer is the daemon's answer to an approval: what the level did,
// or, when origin moved since the operator saw the log, the hold with its
// new target and log, for the operator to approve again.
type ApproveAnswer struct {
	OK    bool   `json:"ok"`
	Text  string `json:"text"`
	Moved *Held  `json:"moved,omitempty"`
}

// NewApprove is the operator's approval of h, signed with the box's
// approve key.
func NewApprove(key ed25519.PrivateKey, h Held, by string, now time.Time) Approve {
	a := Approve{JobID: h.ID, JobSalt: h.Job.Salt, JobSHA256: h.Job.Digest(), By: by, Decision: h.Decision(),
		TS: Stamp(now), Salt: NewSalt()}
	a.Sign(key)
	return a
}
