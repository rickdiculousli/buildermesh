package hosts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// AsleepAfter is how old a presence entry's server timestamp may be before
// its box shows asleep.
const AsleepAfter = 3 * time.Minute

// PutPresence writes p (signed) under its host's key.
func PutPresence(ctx context.Context, kv jetstream.KeyValue, p Presence) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = kv.Put(ctx, p.Host, b)
	return err
}

// Presence states.
const (
	Awake      = "awake"
	Asleep     = "asleep"
	Unverified = "unverified"
	Absent     = "absent"
)

// Seen is one roster box joined with its presence: State is awake,
// asleep (the entry is over AsleepAfter old by its server timestamp),
// unverified (its signature fails against the roster: notes withheld) or
// absent (no entry).
type Seen struct {
	Host     Host     `json:"host"`
	State    string   `json:"state"`
	At       string   `json:"at,omitempty"` // the entry's server timestamp
	Presence Presence `json:"presence"`
}

// ReadPresence joins each roster box with its presence entry.
func ReadPresence(ctx context.Context, kv jetstream.KeyValue, r Roster, now time.Time) ([]Seen, error) {
	var out []Seen
	for _, h := range r.Hosts {
		s := Seen{Host: h, State: Absent}
		e, err := kv.Get(ctx, h.Name)
		if err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, err
		}
		if err == nil {
			s.At = Stamp(e.Created())
			var p Presence
			switch {
			case json.Unmarshal(e.Value(), &p) != nil || p.Host != h.Name || p.Verify(h.Key) != nil:
				p.Notes = ""
				s.State = Unverified
			case now.Sub(e.Created()) > AsleepAfter:
				s.State = Asleep
			default:
				s.State = Awake
			}
			s.Presence = p
		}
		out = append(out, s)
	}
	return out, nil
}
