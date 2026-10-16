package hosts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

func connect(t *testing.T, url string) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}

type served struct {
	req JoinRequest
	err error
}

// serve runs Serve in the background; calls counts accept's calls.
func serve(t *testing.T, nc *nats.Conn, inv Invite, acceptErr error) (<-chan served, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	accept := func(r JoinRequest) (json.RawMessage, string, error) {
		calls.Add(1)
		if acceptErr != nil {
			return nil, "", acceptErr
		}
		return json.RawMessage(`{
  "type": "bmesh/roster/v1", "note": "<a&b>",
  "name": "` + r.Name + `"}`), "adminkey", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	out := make(chan served, 1)
	go func() {
		req, err := Serve(ctx, nc, inv, accept)
		out <- served{req, err}
	}()
	// Let the subscription reach the server before anyone joins.
	time.Sleep(50 * time.Millisecond)
	return out, &calls
}

func wait(t *testing.T, ch <-chan served) served {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
		return served{}
	}
}

func join(t *testing.T, nc *nats.Conn, code, name string, d time.Duration) (JoinReply, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return Join(ctx, nc, code, JoinRequest{Name: name, Key: "hostkey", ApproveKey: "approvekey"})
}

func newInvite(t *testing.T, name string) Invite {
	t.Helper()
	inv, err := NewInvite(name, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

// otherCode is inv's id with another secret.
func otherCode(t *testing.T, inv Invite) string {
	t.Helper()
	other := newInvite(t, inv.Name)
	return inv.ID + "-" + other.Secret
}

func TestJoinGood(t *testing.T) {
	url := meshtest.Start(t)
	admin, box := connect(t, url), connect(t, url)
	inv := newInvite(t, "mac")
	ch, calls := serve(t, admin, inv, nil)
	rep, err := join(t, box, inv.Code(), "mac", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var roster struct{ Type, Note, Name string }
	if err := json.Unmarshal(rep.Roster, &roster); err != nil {
		t.Fatal(err)
	}
	if roster.Name != "mac" || roster.Note != "<a&b>" || rep.AdminKey != "adminkey" {
		t.Fatalf("reply %+v", rep)
	}
	s := wait(t, ch)
	if s.err != nil || s.req.Name != "mac" || s.req.Key != "hostkey" || s.req.ApproveKey != "approvekey" {
		t.Fatalf("Serve: %+v %v", s.req, s.err)
	}
	if _, err := ParseStamp(s.req.TS); err != nil || len(s.req.Salt) != 32 {
		t.Fatalf("ts %q salt %q", s.req.TS, s.req.Salt)
	}
	if calls.Load() != 1 {
		t.Fatalf("accept called %d times", calls.Load())
	}
	// The code is used: no answer.
	if _, err := join(t, box, inv.Code(), "mac", time.Second); err == nil || !strings.Contains(err.Error(), "wrong, used or expired") {
		t.Fatalf("reused code: %v", err)
	}
}

func TestJoinExpired(t *testing.T) {
	url := meshtest.Start(t)
	admin, box := connect(t, url), connect(t, url)
	inv := newInvite(t, "mac")
	inv.Expires = time.Now().Add(300 * time.Millisecond)
	ch, calls := serve(t, admin, inv, nil)
	time.Sleep(400 * time.Millisecond)
	if _, err := join(t, box, inv.Code(), "mac", time.Second); err == nil {
		t.Fatal("joined an expired invite")
	}
	if s := wait(t, ch); s.err == nil || s.err.Error() != "invite "+inv.ID+" expired" {
		t.Fatalf("Serve: %v", s.err)
	}
	if calls.Load() != 0 {
		t.Fatal("accept called")
	}
}

func TestJoinWrongCodeDoesNotBurn(t *testing.T) {
	url := meshtest.Start(t)
	admin, box := connect(t, url), connect(t, url)
	inv := newInvite(t, "mac")
	ch, _ := serve(t, admin, inv, nil)
	if _, err := join(t, box, otherCode(t, inv), "mac", time.Second); err == nil {
		t.Fatal("wrong code joined")
	}
	if _, err := join(t, box, inv.Code(), "mac", 3*time.Second); err != nil {
		t.Fatalf("right code after a wrong one: %v", err)
	}
	if s := wait(t, ch); s.err != nil {
		t.Fatal(s.err)
	}
}

func TestJoinThreeBadBurn(t *testing.T) {
	url := meshtest.Start(t)
	admin, box := connect(t, url), connect(t, url)
	inv := newInvite(t, "mac")
	ch, calls := serve(t, admin, inv, nil)
	// One junk body and two wrong secrets.
	if err := box.Publish(JoinSubject(inv.ID), []byte("junk")); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := join(t, box, otherCode(t, inv), "mac", 300*time.Millisecond); err == nil {
			t.Fatal("wrong code joined")
		}
	}
	if s := wait(t, ch); s.err == nil || s.err.Error() != "invite "+inv.ID+" burned after 3 bad requests" {
		t.Fatalf("Serve: %v", s.err)
	}
	if _, err := join(t, box, inv.Code(), "mac", time.Second); err == nil {
		t.Fatal("burned invite joined")
	}
	if calls.Load() != 0 {
		t.Fatal("accept called")
	}
}

func TestJoinIgnoresJunkReplies(t *testing.T) {
	url := meshtest.Start(t)
	admin, box, rogue := connect(t, url), connect(t, url), connect(t, url)
	inv := newInvite(t, "mac")
	_, err := rogue.Subscribe(JoinSubject(inv.ID), func(m *nats.Msg) {
		_ = rogue.Publish(m.Reply, []byte("junk"))
		forged, _ := json.Marshal(JoinReply{Roster: json.RawMessage(`{"forged":true}`), AdminKey: "evil", MAC: strings.Repeat("ab", 32)})
		_ = rogue.Publish(m.Reply, forged)
		refusal, _ := json.Marshal(JoinReply{Error: "go away", MAC: "zz"})
		_ = rogue.Publish(m.Reply, refusal)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rogue.Flush(); err != nil {
		t.Fatal(err)
	}
	ch, _ := serve(t, admin, inv, nil)
	rep, err := join(t, box, inv.Code(), "mac", 3*time.Second)
	if err != nil || rep.AdminKey != "adminkey" {
		t.Fatalf("Join: %+v %v", rep, err)
	}
	if s := wait(t, ch); s.err != nil {
		t.Fatal(s.err)
	}
}

func TestJoinWrongName(t *testing.T) {
	url := meshtest.Start(t)
	admin, box := connect(t, url), connect(t, url)
	inv := newInvite(t, "mac")
	ch, calls := serve(t, admin, inv, nil)
	_, err := join(t, box, inv.Code(), "other", 3*time.Second)
	if err == nil || !strings.HasPrefix(err.Error(), "refused: ") || !strings.Contains(err.Error(), "not other") {
		t.Fatalf("Join: %v", err)
	}
	if s := wait(t, ch); s.err == nil || s.req.Name != "other" {
		t.Fatalf("Serve: %+v %v", s.req, s.err)
	}
	if calls.Load() != 0 {
		t.Fatal("accept called")
	}
	// Burned on the verified request.
	if _, err := join(t, box, inv.Code(), "mac", time.Second); err == nil {
		t.Fatal("joined after a refusal")
	}
}

func TestJoinInvalidName(t *testing.T) {
	url := meshtest.Start(t)
	admin, box := connect(t, url), connect(t, url)
	inv := newInvite(t, "Bad.Name")
	ch, calls := serve(t, admin, inv, nil)
	if _, err := join(t, box, inv.Code(), "Bad.Name", 3*time.Second); err == nil || !strings.Contains(err.Error(), "not a valid host name") {
		t.Fatalf("Join: %v", err)
	}
	wait(t, ch)
	if calls.Load() != 0 {
		t.Fatal("accept called")
	}
}

func TestJoinAcceptRefuses(t *testing.T) {
	url := meshtest.Start(t)
	admin, box := connect(t, url), connect(t, url)
	inv := newInvite(t, "mac")
	ch, calls := serve(t, admin, inv, errors.New("mac is already in the roster"))
	if _, err := join(t, box, inv.Code(), "mac", 3*time.Second); err == nil || err.Error() != "refused: mac is already in the roster" {
		t.Fatalf("Join: %v", err)
	}
	if s := wait(t, ch); s.err == nil {
		t.Fatal("Serve: no error")
	}
	if calls.Load() != 1 {
		t.Fatalf("accept called %d times", calls.Load())
	}
}

func TestParseCode(t *testing.T) {
	inv := newInvite(t, "mac")
	if len(inv.ID) != 8 || len(inv.Secret) != 26 || inv.Secret != strings.ToUpper(inv.Secret) {
		t.Fatalf("invite %+v", inv)
	}
	if d := time.Until(inv.Expires); d < InviteTTL-time.Minute || d > InviteTTL {
		t.Fatalf("expires in %v", d)
	}
	id, secret, err := ParseCode("  " + inv.Code() + "\n")
	if err != nil || id != inv.ID || secret != inv.Secret {
		t.Fatalf("good: %q %q %v", id, secret, err)
	}
	id, secret, err = ParseCode(inv.ID + "-" + strings.ToLower(inv.Secret))
	if err != nil || id != inv.ID || secret != inv.Secret {
		t.Fatalf("lower case: %q %q %v", id, secret, err)
	}
	for _, bad := range []string{
		"",
		inv.Secret,
		"ABCDEF12-" + inv.Secret,       // upper-case id
		"abcdef1-" + inv.Secret,        // short id
		"abcdefgh-" + inv.Secret,       // not hex
		inv.ID + "-" + inv.Secret[:20], // short secret
		inv.ID + "-" + inv.Secret + "AA",
		inv.ID + "-" + strings.Repeat("1", 26), // not base32
	} {
		if _, _, err := ParseCode(bad); err == nil {
			t.Errorf("ParseCode(%q) accepted", bad)
		}
	}
	if JoinSubject("abcd1234") != "bmhosts.join.abcd1234" {
		t.Fatal(JoinSubject("abcd1234"))
	}
}

func TestValidHostName(t *testing.T) {
	for _, ok := range []string{"k16", "mac", "a", "my-box-2", strings.Repeat("a", 63)} {
		if !ValidHostName(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "-a", "a--b", "Mac", "a.b", "a b", "a*", "a>", "é", strings.Repeat("a", 64)} {
		if ValidHostName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
