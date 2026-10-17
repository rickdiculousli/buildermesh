package selfupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ProcRecord is what a running bmesh hub or serve says of itself in
// <state dir>/<name>.json: its pid and the binary it started with.
type ProcRecord struct {
	Pid      int    `json:"pid"`
	Revision string `json:"revision,omitempty"`
	Release  string `json:"release,omitempty"`
	Time     string `json:"time,omitempty"`
}

// ProcPath is the record's file: <state dir>/<name>.json (hub, serve).
func ProcPath(stateDir, name string) string { return filepath.Join(stateDir, name+".json") }

// RecordOf is the record of a binary at v, with no pid: what bmesh update
// passes for the binary it just installed.
func RecordOf(v Version) ProcRecord {
	return ProcRecord{Revision: v.Revision, Release: v.Release, Time: v.Time}
}

// Label is how a record's binary reads: its release (v0.1.0, v0.1.0+3)
// when stamped, else its revision's first eight characters.
func (r ProcRecord) Label() string {
	if r.Release != "" {
		return r.Release
	}
	return Short(r.Revision)
}

// WriteProc records this process, at v, as name in stateDir; remove takes
// the record away again on a clean stop, unless another process has
// written its own since.
func WriteProc(stateDir, name string, v Version) (remove func(), err error) {
	rec := RecordOf(v)
	rec.Pid = os.Getpid()
	b, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, err
	}
	p := ProcPath(stateDir, name)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	return func() {
		if have, ok := readProc(p); ok && have.Pid == rec.Pid {
			os.Remove(p)
		}
	}, nil
}

// ReadProc is name's record in stateDir; false when there is none, it
// doesn't parse, or its pid isn't alive.
func ReadProc(stateDir, name string) (ProcRecord, bool) {
	r, ok := readProc(ProcPath(stateDir, name))
	if !ok || r.Pid <= 0 || !pidAlive(r.Pid) {
		return ProcRecord{}, false
	}
	return r, true
}

func readProc(path string) (ProcRecord, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ProcRecord{}, false
	}
	var r ProcRecord
	if json.Unmarshal(b, &r) != nil {
		return ProcRecord{}, false
	}
	return r, true
}

// HubLine names a bmesh hub on this box running an older binary than
// serve: `hub runs <hub>, serve <serve>: restart bmesh-hub`. It is "" when
// no live hub is recorded, the two are the same commit, or the hub is the
// newer by commit time; with no time on either side, any difference counts.
func HubLine(stateDir string, serve ProcRecord) string {
	hub, ok := ReadProc(stateDir, "hub")
	if !ok || sameBinary(hub, serve) {
		return ""
	}
	if hub.Revision == "" && hub.Release == "" && serve.Revision == "" && serve.Release == "" {
		return ""
	}
	ht, herr := time.Parse(time.RFC3339, hub.Time)
	st, serr := time.Parse(time.RFC3339, serve.Time)
	if herr == nil && serr == nil && !ht.Before(st) {
		return ""
	}
	return "hub runs " + hub.Label() + ", serve " + serve.Label() + ": restart bmesh-hub"
}

// sameBinary: one commit by revision when both are known, else one release.
func sameBinary(a, b ProcRecord) bool {
	if a.Revision != "" && b.Revision != "" {
		return Same(a.Revision, b.Revision)
	}
	return a.Release != "" && a.Release == b.Release
}
