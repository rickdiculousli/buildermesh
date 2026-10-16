package hosts

import (
	"slices"
	"time"
)

// Action is what a receiving box does with a job.
type Action int

const (
	// Run: every check passed and the salt is new (now recorded started).
	Run Action = iota
	// Resume: a redelivery of an idempotent op that started and never
	// finished; run it again.
	Resume
	// Refuse: ack, answer `refused: <check>`, never run.
	Refuse
	// Expired: as Refuse, but logged only, never pushed.
	Expired
	// Nak: hand it back for a minute (the box's roster is behind, or the
	// sender's clock is ahead).
	Nak
	// Replay: the job ran already; ack and send the stored reply again.
	Replay
	// Interrupted: a non-idempotent op that started and never finished;
	// answer `interrupted`, never re-run.
	Interrupted
)

// The checks, by the name a refusal carries.
const (
	CheckRoster        = "roster"
	CheckSignature     = "signature"
	CheckTo            = "to"
	CheckRosterVersion = "roster_version"
	CheckClock         = "clock"
	CheckExpired       = "expired"
	CheckAccept        = "accept"
	CheckReplay        = "replay"
)

// Pushed reports the refusals published as bm.daemon.<host>.refused for the
// phone: roster, signature, to and replay.
func Pushed(check string) bool {
	return check == CheckRoster || check == CheckSignature || check == CheckTo || check == CheckReplay
}

// Verdict is a check's outcome: the action and, unless Run, the check that
// decided it; Record is the stored record for Replay.
type Verdict struct {
	Action Action
	Check  string
	Record SaltRecord
}

const (
	// MaxClockAhead is how far a job's ts may be ahead of this box's clock.
	MaxClockAhead = 5 * time.Minute
	// MaxLifetime bounds expires - ts.
	MaxLifetime = 24 * time.Hour
)

// Idempotent ops resume after a crash or sleep; any other is answered
// interrupted.
var Idempotent = map[string]bool{"ping": true, "level": true, "prune": true}

// AdminOps need the admin's signature: their job is signed by the admin key.
var AdminOps = map[string]bool{}

// Check runs checks 1 to 7 in order and stops at the first failure:
//  1. by is in the roster (an admin op: verified against the admin key);
//  2. the signature verifies;
//  3. to is self;
//  4. roster_version isn't ahead of this box's roster (else nak);
//  5. ts is at most MaxClockAhead ahead of now (else nak);
//  6. expires hasn't passed and expires - ts is at most MaxLifetime;
//  7. the op is in self's accept in the roster.
//
// Check 8, the salt, is Salts.Admit's. A Run verdict means all seven passed.
func Check(j *Job, r *Roster, self string, now time.Time) Verdict {
	refuse := func(check string) Verdict { return Verdict{Action: Refuse, Check: check} }
	sender, ok := r.Host(j.By)
	if !ok {
		return refuse(CheckRoster)
	}
	key := sender.Key
	if AdminOps[j.Op] {
		if a, ok := r.Admin(); !ok || a.Name != j.By {
			return refuse(CheckRoster)
		}
		key = r.AdminKey
	}
	if j.Verify(key) != nil {
		return refuse(CheckSignature)
	}
	if j.To != self {
		return refuse(CheckTo)
	}
	if j.RosterVersion > r.Version {
		return Verdict{Action: Nak, Check: CheckRosterVersion}
	}
	ts, err := ParseStamp(j.TS)
	if err != nil {
		return refuse(CheckClock)
	}
	if ts.Sub(now) > MaxClockAhead {
		return Verdict{Action: Nak, Check: CheckClock}
	}
	exp, err := ParseStamp(j.Expires)
	if err != nil || !now.Before(exp) || exp.Sub(ts) > MaxLifetime {
		return Verdict{Action: Expired, Check: CheckExpired}
	}
	me, ok := r.Host(self)
	if !ok || !slices.Contains(me.Accept, j.Op) {
		return refuse(CheckAccept)
	}
	return Verdict{Action: Run}
}

// Admit is check 8, for a job that passed the other seven: no record
// writes started and runs; done replays the stored reply; started (a
// redelivery after a crash or sleep) resumes an idempotent op and answers
// any other interrupted. A malformed salt, or one another job used, is
// refused as a replay.
func (s *Salts) Admit(j *Job) (Verdict, error) {
	if !ValidSalt(j.Salt) {
		return Verdict{Action: Refuse, Check: CheckReplay}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok, err := s.get(j.Salt)
	if err != nil {
		return Verdict{}, err
	}
	switch {
	case !ok:
		rec = SaltRecord{State: SaltStarted, JobID: j.ID, Op: j.Op, Expires: j.Expires}
		if err := s.put(j.Salt, rec); err != nil {
			return Verdict{}, err
		}
		return Verdict{Action: Run}, nil
	case rec.JobID != j.ID:
		return Verdict{Action: Refuse, Check: CheckReplay}, nil // another job reusing a salt
	case rec.State == SaltDone:
		return Verdict{Action: Replay, Check: CheckReplay, Record: rec}, nil
	case Idempotent[j.Op]:
		return Verdict{Action: Resume, Record: rec}, nil
	}
	return Verdict{Action: Interrupted, Record: rec}, nil
}
