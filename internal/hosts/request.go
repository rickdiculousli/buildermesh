package hosts

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// MaxDetails is how many lines of a request's details are shown.
const MaxDetails = 12

// ErrCancelled is the bmesh dialog answered Cancel, or not at all.
var ErrCancelled = errors.New("cancelled at the dialog: nothing signed")

// Request is what a protected key is read for, shown before the read: a
// title, the details of what changes, and the exact bytes the key signs,
// composed before the read and signed as they are (Sign) after it.
type Request struct {
	Title   string   // "Sign roster v6"
	Details []string // what changes, one line each
	Signs   [][]byte // the bytes the key signs; none when it signs nothing yet
}

// Sum is the first 8 hex of the SHA-256 of the bytes the request signs,
// one item after another; "" when it signs none.
func (r Request) Sum() string {
	if len(r.Signs) == 0 {
		return ""
	}
	h := sha256.New()
	for _, b := range r.Signs {
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

// Lines is what the dialog and the terminal show under the title: the
// details, at most MaxDetails lines, then "signs <sum>".
func (r Request) Lines() []string {
	d := r.Details
	if len(d) > MaxDetails {
		d = append(d[:MaxDetails-1:MaxDetails-1], fmt.Sprintf("... and %d more", len(d)-MaxDetails+1))
	}
	if s := r.Sum(); s != "" {
		d = append(d[:len(d):len(d)], "signs "+s)
	}
	return d
}

// Print writes the request as the terminal shows it.
func (r Request) Print(w io.Writer) {
	fmt.Fprintf(w, "bmesh: %s\n", r.Title)
	for _, l := range r.Lines() {
		fmt.Fprintf(w, "  %s\n", l)
	}
}

// Sign is the signature over each of the request's items, in order: the
// bytes shown, never composed again.
func (r Request) Sign(key ed25519.PrivateKey) []string {
	sigs := make([]string, len(r.Signs))
	for i, b := range r.Signs {
		sigs[i] = sign(key, b)
	}
	return sigs
}
