// Package hosts is the mesh's membership and proof: the admin's signed
// roster, signed jobs and replies between boxes, presence, the one-time
// invite and the keys behind them. Nothing here runs unless [hosts]
// enabled = true.
package hosts

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The type tags: each signed document's first field.
const (
	TagJob      = "bmesh/job/v1"
	TagReply    = "bmesh/reply/v1"
	TagApprove  = "bmesh/approve/v1"
	TagRoster   = "bmesh/roster/v1"
	TagRegistry = "bmesh/registry/v1"
	TagPresence = "bmesh/presence/v1"
)

// enc builds the signed bytes: a scalar is a 4-byte big-endian length then
// its bytes (a string as UTF-8, an integer as decimal ASCII, a boolean as 0
// or 1); a list is a 4-byte big-endian count then each item; a struct is its
// fields in their listed order.
type enc struct{ b []byte }

func (e *enc) str(s string) {
	e.b = binary.BigEndian.AppendUint32(e.b, uint32(len(s)))
	e.b = append(e.b, s...)
}

func (e *enc) int(n int64) { e.str(strconv.FormatInt(n, 10)) }

func (e *enc) bool(v bool) {
	if v {
		e.str("1")
	} else {
		e.str("0")
	}
}

func (e *enc) count(n int) { e.b = binary.BigEndian.AppendUint32(e.b, uint32(n)) }

// strs is a list of strings in the order given (a job's args).
func (e *enc) strs(xs []string) {
	e.count(len(xs))
	for _, x := range xs {
		e.str(x)
	}
}

// names is a list of names, sorted and deduplicated first.
func (e *enc) names(xs []string) { e.strs(Names(xs)) }

// Names is xs sorted and deduplicated, never nil: how a list of names is
// encoded and stored.
func Names(xs []string) []string {
	out := append([]string{}, xs...)
	slices.Sort(out)
	return slices.Compact(out)
}

// TimeFormat is how ts, expires and issued_at are written: RFC 3339 UTC to
// the second.
const TimeFormat = "2006-01-02T15:04:05Z"

// Stamp is t in TimeFormat.
func Stamp(t time.Time) string { return t.UTC().Format(TimeFormat) }

// ParseStamp reads a TimeFormat time; any other form is an error.
func ParseStamp(s string) (time.Time, error) {
	t, err := time.Parse(TimeFormat, s)
	if err != nil || Stamp(t) != s {
		return time.Time{}, fmt.Errorf("time %q: want RFC 3339 UTC to the second", s)
	}
	return t, nil
}

// sign is the hex Ed25519 signature over SHA-256 of b. Ed25519 is
// deterministic, so one message gives one signature everywhere.
func sign(key ed25519.PrivateKey, b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(ed25519.Sign(key, sum[:]))
}

var errBadSig = errors.New("bad signature")

// verify checks a hex signature over SHA-256 of b against a hex public key.
func verify(pubHex, sigHex string, b []byte) error {
	pub, err := ParseKey(pubHex)
	if err != nil {
		return err
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errBadSig
	}
	sum := sha256.Sum256(b)
	if !ed25519.Verify(pub, sum[:], sig) {
		return errBadSig
	}
	return nil
}

// KeyHex is a public key as the documents carry it: lowercase hex.
func KeyHex(pub ed25519.PublicKey) string { return hex.EncodeToString(pub) }

// ParseKey reads a hex public key.
func ParseKey(s string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize || hex.EncodeToString(b) != s {
		return nil, fmt.Errorf("key %q: want %d bytes in lowercase hex", s, ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

// Fingerprint is the first 16 hex characters of the SHA-256 of a hex
// public key's bytes; "?" for a key that doesn't parse.
func Fingerprint(pubHex string) string {
	pub, err := ParseKey(pubHex)
	if err != nil {
		return "?"
	}
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])[:16]
}

// Job asks another box to run an op. Expires - TS is at most 24h.
type Job struct {
	Type          string   `json:"type"`
	ID            string   `json:"id"`
	By            string   `json:"by"`
	To            string   `json:"to"`
	Op            string   `json:"op"`
	Repo          string   `json:"repo"`
	Args          []string `json:"args"`
	RosterVersion int64    `json:"roster_version"`
	TS            string   `json:"ts"`
	Expires       string   `json:"expires"`
	Salt          string   `json:"salt"`
	Sig           string   `json:"sig"`
}

// Bytes is the job's signed bytes.
func (j *Job) Bytes() []byte {
	var e enc
	e.str(j.Type)
	e.str(j.ID)
	e.str(j.By)
	e.str(j.To)
	e.str(j.Op)
	e.str(j.Repo)
	e.strs(j.Args)
	e.int(j.RosterVersion)
	e.str(j.TS)
	e.str(j.Expires)
	e.str(j.Salt)
	return e.b
}

// Sign sets Type and Sig.
func (j *Job) Sign(key ed25519.PrivateKey) { j.Type = TagJob; j.Sig = sign(key, j.Bytes()) }

// Verify checks Type and Sig against pubHex.
func (j *Job) Verify(pubHex string) error {
	if j.Type != TagJob {
		return fmt.Errorf("type %q: want %s", j.Type, TagJob)
	}
	return verify(pubHex, j.Sig, j.Bytes())
}

// Digest is the hex SHA-256 of the job's signed bytes: what an approval
// names.
func (j *Job) Digest() string {
	sum := sha256.Sum256(j.Bytes())
	return hex.EncodeToString(sum[:])
}

// Reply answers a job, signed by the job's To; it names the job's id and
// salt.
type Reply struct {
	Type    string `json:"type"`
	JobID   string `json:"job_id"`
	JobSalt string `json:"job_salt"`
	By      string `json:"by"`
	To      string `json:"to"`
	OK      bool   `json:"ok"`
	Text    string `json:"text"`
	TS      string `json:"ts"`
	Salt    string `json:"salt"`
	Sig     string `json:"sig"`
}

func (r *Reply) Bytes() []byte {
	var e enc
	e.str(r.Type)
	e.str(r.JobID)
	e.str(r.JobSalt)
	e.str(r.By)
	e.str(r.To)
	e.bool(r.OK)
	e.str(r.Text)
	e.str(r.TS)
	e.str(r.Salt)
	return e.b
}

func (r *Reply) Sign(key ed25519.PrivateKey) { r.Type = TagReply; r.Sig = sign(key, r.Bytes()) }

func (r *Reply) Verify(pubHex string) error {
	if r.Type != TagReply {
		return fmt.Errorf("type %q: want %s", r.Type, TagReply)
	}
	return verify(pubHex, r.Sig, r.Bytes())
}

// Approve is the operator's decision on a held job, signed by the
// approving box's protected key (020).
type Approve struct {
	Type      string `json:"type"`
	JobID     string `json:"job_id"`
	JobSalt   string `json:"job_salt"`
	JobSHA256 string `json:"job_sha256"`
	By        string `json:"by"`
	Decision  string `json:"decision"`
	TS        string `json:"ts"`
	Salt      string `json:"salt"`
	Sig       string `json:"sig"`
}

func (a *Approve) Bytes() []byte {
	var e enc
	e.str(a.Type)
	e.str(a.JobID)
	e.str(a.JobSalt)
	e.str(a.JobSHA256)
	e.str(a.By)
	e.str(a.Decision)
	e.str(a.TS)
	e.str(a.Salt)
	return e.b
}

func (a *Approve) Sign(key ed25519.PrivateKey) { a.Type = TagApprove; a.Sig = sign(key, a.Bytes()) }

func (a *Approve) Verify(pubHex string) error {
	if a.Type != TagApprove {
		return fmt.Errorf("type %q: want %s", a.Type, TagApprove)
	}
	return verify(pubHex, a.Sig, a.Bytes())
}

// Host is one roster row.
type Host struct {
	Name       string   `json:"name"`
	Key        string   `json:"key"`
	Secure     bool     `json:"secure"`
	ApproveKey string   `json:"approve_key"`
	Accept     []string `json:"accept"`
}

// Roster is the canonical list of who belongs, signed by the admin key.
// The admin's own row carries the admin key as its approve key.
type Roster struct {
	Type     string `json:"type"`
	Version  int64  `json:"version"`
	AdminKey string `json:"admin_key"`
	Hosts    []Host `json:"hosts"`
	IssuedAt string `json:"issued_at"`
	Sig      string `json:"sig"`
}

// Bytes encodes the hosts sorted by name, each one's accept as names.
func (r *Roster) Bytes() []byte {
	var e enc
	e.str(r.Type)
	e.int(r.Version)
	e.str(r.AdminKey)
	hosts := append([]Host{}, r.Hosts...)
	slices.SortFunc(hosts, func(a, b Host) int { return strings.Compare(a.Name, b.Name) })
	e.count(len(hosts))
	for _, h := range hosts {
		e.str(h.Name)
		e.str(h.Key)
		e.bool(h.Secure)
		e.str(h.ApproveKey)
		e.names(h.Accept)
	}
	e.str(r.IssuedAt)
	return e.b
}

func (r *Roster) Sign(admin ed25519.PrivateKey) {
	r.Type = TagRoster
	for i := range r.Hosts {
		r.Hosts[i].Accept = Names(r.Hosts[i].Accept)
	}
	slices.SortFunc(r.Hosts, func(a, b Host) int { return strings.Compare(a.Name, b.Name) })
	r.Sig = sign(admin, r.Bytes())
}

// Verify checks the type, that no name repeats, that the roster names
// pinned as its admin key, and the signature.
func (r *Roster) Verify(pinned string) error {
	if r.Type != TagRoster {
		return fmt.Errorf("type %q: want %s", r.Type, TagRoster)
	}
	if r.AdminKey != pinned {
		return fmt.Errorf("roster v%d is signed by %s, not the pinned admin %s", r.Version,
			Fingerprint(r.AdminKey), Fingerprint(pinned))
	}
	seen := map[string]bool{}
	for _, h := range r.Hosts {
		if seen[h.Name] {
			return fmt.Errorf("roster v%d names %s twice", r.Version, h.Name)
		}
		seen[h.Name] = true
	}
	return verify(pinned, r.Sig, r.Bytes())
}

// Host is the named row.
func (r *Roster) Host(name string) (Host, bool) {
	for _, h := range r.Hosts {
		if h.Name == name {
			return h, true
		}
	}
	return Host{}, false
}

// Admin is the admin box's row: the one whose approve key is the admin key.
func (r *Roster) Admin() (Host, bool) {
	for _, h := range r.Hosts {
		if h.ApproveKey != "" && h.ApproveKey == r.AdminKey {
			return h, true
		}
	}
	return Host{}, false
}

// PresenceRepo is one registered repo as a box reports it.
type PresenceRepo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Origin string `json:"origin"`
}

// Presence is what each daemon says about its box each minute, signed with
// its host key. It is never the source of a path or a decision.
type Presence struct {
	Type     string         `json:"type"`
	Host     string         `json:"host"`
	Rev      string         `json:"rev"`
	OS       string         `json:"os"`
	Routines bool           `json:"routines"`
	Repos    []PresenceRepo `json:"repos"`
	Notes    string         `json:"notes"`
	TS       string         `json:"ts"`
	Sig      string         `json:"sig"`
	// Held is the box's holds (093), signed apart in HeldSig so a box on
	// an older release, which knows only Sig, still verifies the rest.
	Held    []PresenceHold `json:"held,omitempty"`
	HeldSig string         `json:"held_sig,omitempty"`
}

// Bytes encodes the repos sorted by name.
func (p *Presence) Bytes() []byte {
	var e enc
	e.str(p.Type)
	e.str(p.Host)
	e.str(p.Rev)
	e.str(p.OS)
	e.bool(p.Routines)
	repos := append([]PresenceRepo{}, p.Repos...)
	slices.SortFunc(repos, func(a, b PresenceRepo) int { return strings.Compare(a.Name, b.Name) })
	repos = slices.CompactFunc(repos, func(a, b PresenceRepo) bool { return a.Name == b.Name })
	e.count(len(repos))
	for _, r := range repos {
		e.str(r.Name)
		e.str(r.Path)
		e.str(r.Origin)
	}
	e.str(p.Notes)
	e.str(p.TS)
	return e.b
}

// HeldBytes is Bytes followed by the holds, in their order: so HeldSig
// binds them to this entry's host and ts.
func (p *Presence) HeldBytes() []byte {
	e := enc{b: p.Bytes()}
	e.count(len(p.Held))
	for _, h := range p.Held {
		e.str(h.Kind)
		e.str(h.Repo)
		e.str(h.From)
		e.int(int64(h.Commits))
		e.str(h.ID)
		e.str(h.Expires)
	}
	return e.b
}

func (p *Presence) Sign(key ed25519.PrivateKey) {
	p.Type = TagPresence
	slices.SortFunc(p.Repos, func(a, b PresenceRepo) int { return strings.Compare(a.Name, b.Name) })
	p.Sig = sign(key, p.Bytes())
	p.HeldSig = ""
	if len(p.Held) > 0 {
		p.HeldSig = sign(key, p.HeldBytes())
	}
}

func (p *Presence) Verify(pubHex string) error {
	if p.Type != TagPresence {
		return fmt.Errorf("type %q: want %s", p.Type, TagPresence)
	}
	if err := verify(pubHex, p.Sig, p.Bytes()); err != nil {
		return err
	}
	if len(p.Held) == 0 && p.HeldSig == "" {
		return nil
	}
	return verify(pubHex, p.HeldSig, p.HeldBytes())
}
