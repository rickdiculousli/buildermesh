package hosts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Salt record states.
const (
	SaltStarted = "started"
	SaltDone    = "done"
)

// SaltRecord is what a box remembers of a job it took, by the job's salt,
// until the job expires: started when it began, done with the reply sent.
type SaltRecord struct {
	State   string `json:"state"`
	JobID   string `json:"job_id"`
	Op      string `json:"op"`
	Expires string `json:"expires"`
	Reply   *Reply `json:"reply,omitempty"`
}

// Salts is the store in <state dir>/salts: one file per salt, so it
// survives a restart.
type Salts struct {
	dir string
	mu  sync.Mutex
}

// OpenSalts is the store under stateDir.
func OpenSalts(stateDir string) *Salts { return &Salts{dir: filepath.Join(stateDir, "salts")} }

var saltRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidSalt: 16 bytes as lowercase hex.
func ValidSalt(s string) bool { return saltRE.MatchString(s) }

func (s *Salts) path(salt string) string { return filepath.Join(s.dir, salt+".json") }

// Get is the salt's record; ok is false when there is none.
func (s *Salts) Get(salt string) (SaltRecord, bool, error) {
	if !ValidSalt(salt) {
		return SaltRecord{}, false, fmt.Errorf("salt %q: want 16 bytes as hex", salt)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(salt)
}

func (s *Salts) get(salt string) (SaltRecord, bool, error) {
	b, err := os.ReadFile(s.path(salt))
	if errors.Is(err, os.ErrNotExist) {
		return SaltRecord{}, false, nil
	}
	if err != nil {
		return SaltRecord{}, false, err
	}
	var r SaltRecord
	if err := json.Unmarshal(b, &r); err != nil {
		return SaltRecord{}, false, fmt.Errorf("%s: %w", s.path(salt), err)
	}
	return r, true, nil
}

// Put writes the salt's record.
func (s *Salts) Put(salt string, r SaltRecord) error {
	if !ValidSalt(salt) {
		return fmt.Errorf("salt %q: want 16 bytes as hex", salt)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.put(salt, r)
}

func (s *Salts) put(salt string, r SaltRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return writeAtomic(s.path(salt), b)
}

// Prune deletes the records whose job has expired: such a job is refused
// by its expiry before its salt is looked at.
func (s *Salts) Prune(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ents, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range ents {
		salt, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !ValidSalt(salt) {
			continue
		}
		r, _, err := s.get(salt)
		if err != nil {
			continue
		}
		if exp, err := ParseStamp(r.Expires); err == nil && now.After(exp) {
			os.Remove(s.path(salt))
		}
	}
	return nil
}
