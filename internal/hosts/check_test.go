package hosts

import (
	"crypto/ed25519"
	"crypto/sha256"
	"testing"
	"time"
)

func keyFor(name string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("bmesh test " + name))
	return ed25519.NewKeyFromSeed(seed[:])
}

func pubFor(name string) string { return KeyHex(keyFor(name).Public().(ed25519.PublicKey)) }

// testRoster: mac is the admin (its approve key is the admin key), k16
// accepts ping.
func testRoster(version int64) Roster {
	r := Roster{Version: version, Hosts: []Host{
		{Name: "mac", Key: pubFor("mac"), Secure: true, ApproveKey: pubFor("admin"), Accept: []string{"ping"}},
		{Name: "k16", Key: pubFor("k16"), Accept: []string{"ping", "level"}},
	}}
	r.AdminKey = pubFor("admin")
	r.IssuedAt = "2026-10-16T08:00:00Z"
	r.Sign(keyFor("admin"))
	return r
}

var t0 = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

func pingJob(by, to string) Job {
	return NewJob(keyFor(by), by, to, "ping", "", nil, 2, t0, time.Hour)
}

func TestCheckEachRefusal(t *testing.T) {
	r := testRoster(2)
	resign := func(j Job, key string) Job { j.Sign(keyFor(key)); return j }
	cases := []struct {
		name   string
		job    func() Job
		now    time.Time
		action Action
		check  string
	}{
		{"passes", func() Job { return pingJob("mac", "k16") }, t0, Run, ""},
		{"1 sender not in the roster", func() Job { return pingJob("rogue", "k16") }, t0, Refuse, CheckRoster},
		{"2 changed in flight", func() Job { j := pingJob("mac", "k16"); j.Args = []string{"x"}; return j }, t0, Refuse, CheckSignature},
		{"2 signed by another box's key", func() Job { return resign(pingJob("mac", "k16"), "k16") }, t0, Refuse, CheckSignature},
		{"3 sent to another box", func() Job { return pingJob("mac", "mac") }, t0, Refuse, CheckTo},
		{"4 roster ahead", func() Job { j := pingJob("mac", "k16"); j.RosterVersion = 3; return resign(j, "mac") }, t0, Nak, CheckRosterVersion},
		{"5 clock ahead", func() Job { return pingJob("mac", "k16") }, t0.Add(-6 * time.Minute), Nak, CheckClock},
		{"5 a little ahead is fine", func() Job { return pingJob("mac", "k16") }, t0.Add(-4 * time.Minute), Run, ""},
		{"6 expired", func() Job { return pingJob("mac", "k16") }, t0.Add(2 * time.Hour), Expired, CheckExpired},
		{"6 lives over a day", func() Job {
			j := pingJob("mac", "k16")
			j.Expires = Stamp(t0.Add(25 * time.Hour))
			return resign(j, "mac")
		}, t0, Expired, CheckExpired},
		{"7 op not accepted", func() Job {
			j := pingJob("mac", "k16")
			j.Op = "prune"
			return resign(j, "mac")
		}, t0, Refuse, CheckAccept},
	}
	for _, c := range cases {
		j := c.job()
		v := Check(&j, &r, "k16", c.now)
		if v.Action != c.action || v.Check != c.check {
			t.Errorf("%s: got %v %q, want %v %q", c.name, v.Action, v.Check, c.action, c.check)
		}
	}
}

func TestCheckRevokedSender(t *testing.T) {
	r := testRoster(3)
	r.Hosts = []Host{r.Hosts[1]} // k16 revoked (Sign sorted the rows: k16, mac)
	r.Sign(keyFor("admin"))
	j := NewJob(keyFor("k16"), "k16", "mac", "ping", "", nil, 2, t0, time.Hour)
	if v := Check(&j, &r, "mac", t0); v.Action != Refuse || v.Check != CheckRoster {
		t.Fatalf("a revoked box's job: %v %q", v.Action, v.Check)
	}
}

func TestCheckAdminOp(t *testing.T) {
	AdminOps["test-admin"] = true
	defer delete(AdminOps, "test-admin")
	r := testRoster(2)
	r.Hosts[0].Accept = append(r.Hosts[0].Accept, "test-admin") // k16
	r.Sign(keyFor("admin"))
	byHostKey := NewJob(keyFor("mac"), "mac", "k16", "test-admin", "", nil, 2, t0, time.Hour)
	if v := Check(&byHostKey, &r, "k16", t0); v.Check != CheckSignature {
		t.Errorf("an admin op signed by the admin box's host key: %v %q", v.Action, v.Check)
	}
	byAdmin := NewJob(keyFor("admin"), "mac", "k16", "test-admin", "", nil, 2, t0, time.Hour)
	if v := Check(&byAdmin, &r, "k16", t0); v.Action != Run {
		t.Errorf("an admin op signed by the admin key: %v %q", v.Action, v.Check)
	}
	fromK16 := NewJob(keyFor("admin"), "k16", "k16", "test-admin", "", nil, 2, t0, time.Hour)
	if v := Check(&fromK16, &r, "k16", t0); v.Check != CheckRoster {
		t.Errorf("an admin op from a box that isn't the admin: %v %q", v.Action, v.Check)
	}
}

func TestSaltsThreeStatesAcrossARestart(t *testing.T) {
	dir := t.TempDir()
	j := pingJob("mac", "k16")
	v, err := OpenSalts(dir).Admit(&j)
	if err != nil || v.Action != Run {
		t.Fatalf("new salt: %v %v", v.Action, err)
	}
	// The daemon restarts mid-job: the record says started.
	if v, _ := OpenSalts(dir).Admit(&j); v.Action != Resume {
		t.Fatalf("started idempotent op after a restart: %v", v.Action)
	}
	other := NewJob(keyFor("mac"), "mac", "k16", "nix", "", nil, 2, t0, time.Hour)
	if v, _ := OpenSalts(dir).Admit(&other); v.Action != Run {
		t.Fatal(v.Action)
	}
	if v, _ := OpenSalts(dir).Admit(&other); v.Action != Interrupted {
		t.Fatalf("started non-idempotent op after a restart: %v", v.Action)
	}
	reply := NewReply(keyFor("k16"), &j, "k16", true, "abc roster v2", t0)
	if err := OpenSalts(dir).Put(j.Salt, SaltRecord{State: SaltDone, JobID: j.ID, Op: j.Op, Expires: j.Expires, Reply: &reply}); err != nil {
		t.Fatal(err)
	}
	v, _ = OpenSalts(dir).Admit(&j)
	if v.Action != Replay || v.Record.Reply == nil || v.Record.Reply.Text != "abc roster v2" {
		t.Fatalf("done: %+v", v)
	}
	thief := j
	thief.ID = "other"
	thief.Sign(keyFor("mac"))
	if v, _ := OpenSalts(dir).Admit(&thief); v.Action != Refuse || v.Check != CheckReplay {
		t.Fatalf("another job reusing the salt: %+v", v)
	}
	if err := OpenSalts(dir).Prune(t0.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := OpenSalts(dir).Get(j.Salt); ok {
		t.Fatal("an expired job's record was kept")
	}
}

func TestSaltRefusesAMalformedSalt(t *testing.T) {
	j := pingJob("mac", "k16")
	j.Salt = "../../etc"
	if v, _ := OpenSalts(t.TempDir()).Admit(&j); v.Action != Refuse {
		t.Fatalf("malformed salt: %v", v.Action)
	}
}

func TestCheckReply(t *testing.T) {
	r := testRoster(2)
	j := pingJob("mac", "k16")
	good := NewReply(keyFor("k16"), &j, "k16", true, "ok", t0)
	if err := CheckReply(&good, &j, &r); err != nil {
		t.Fatal(err)
	}
	forged := NewReply(keyFor("mac"), &j, "k16", true, "ok", t0)
	if CheckReply(&forged, &j, &r) == nil {
		t.Error("a reply not signed by the job's to checked")
	}
	other := pingJob("mac", "k16")
	if CheckReply(&good, &other, &r) == nil {
		t.Error("a reply naming another job checked")
	}
}
