package hosts

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Held is a level waiting for the operator on a secure box (020), in
// <state dir>/held/<id>.json: the job (a self-level's is the box's own,
// signed with its host key), the target and git log --stat HEAD..target.
type Held struct {
	ID      string `json:"id"` // the job's id
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
