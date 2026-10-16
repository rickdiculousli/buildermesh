package mesh

import "testing"

func TestParsePath(t *testing.T) {
	for _, s := range []string{"party-buzzer-012", "party-buzzer-012.alpha"} {
		p, err := ParsePath(s)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		if p.String() != s {
			t.Fatalf("round trip %q -> %q", s, p.String())
		}
	}
	for _, s := range []string{"inbox.x", "routine", "daemon.h", "feedback", "feedback.x", "host", "host.x", "hostreply", "hostreply.x", "A.b", "a..b", "", "a.", ".a", "-a", "a b", "a--b", "a.b--c"} {
		if _, err := ParsePath(s); err == nil {
			t.Fatalf("%q: want error", s)
		}
	}
}

func TestParsePathRoutine(t *testing.T) {
	for _, s := range []string{"routine.verify", "routine.verify.sonnet-1"} {
		p, err := ParsePath(s)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		if k, ok := KindFromPath(p); !ok || k != "verify" {
			t.Fatalf("%q: kind %q %v", s, k, ok)
		}
	}
	for _, s := range []string{"routine", "inbox.x.y", "daemon.x.y"} {
		if _, err := ParsePath(s); err == nil {
			t.Fatalf("%q: want error", s)
		}
	}
	if got := RoutinePath("verify").String(); got != "routine.verify" {
		t.Fatal(got)
	}
	if _, ok := KindFromPath(Path{"app", "verify"}); ok {
		t.Fatal("non-routine path has a kind")
	}
}

func TestPathLeadParent(t *testing.T) {
	p, _ := ParsePath("a.b.c")
	if p.Lead() != "a" {
		t.Fatal(p.Lead())
	}
	par, ok := p.Parent()
	if !ok || par.String() != "a.b" {
		t.Fatal(par, ok)
	}
	one, _ := ParsePath("a")
	if _, ok := one.Parent(); ok {
		t.Fatal("single token has no parent")
	}
}

func TestSubjects(t *testing.T) {
	p, _ := ParsePath("party-buzzer-012.alpha")
	if got := EventSubject(p, "done"); got != "bm.party-buzzer-012.alpha.done" {
		t.Fatal(got)
	}
	if got := InboxSubject(p); got != "bm.inbox.party-buzzer-012.alpha" {
		t.Fatal(got)
	}
	if got := InboxConsumer(p); got != "inbox-party-buzzer-012--alpha" {
		t.Fatal(got)
	}
	if got := TreeFilter("x"); got != "bm.x.>" {
		t.Fatal(got)
	}
	if got := DaemonSubject("k16", "wake"); got != "bm.daemon.k16.wake" {
		t.Fatal(got)
	}
}

func TestIsMeshInternal(t *testing.T) {
	for _, s := range []string{"bm.inbox.app", "bm.inbox.host.k16", "bm.daemon.k16.started", "bm.feedback.app", "bm.host.k16", "bm.hostreply.k16.abc"} {
		if !IsMeshInternal(s) {
			t.Errorf("%q: want internal", s)
		}
	}
	for _, s := range []string{"bm.app.started", "bm.app.x.progress", "bm.routine.verify.done", "bm.hosts.done", "bm.inboxes.x", "other.inbox.x", "bm.inbox"} {
		if IsMeshInternal(s) != (s == "bm.inbox") {
			t.Errorf("%q: IsMeshInternal = %v", s, IsMeshInternal(s))
		}
	}
}

func TestIsHostTraffic(t *testing.T) {
	for _, s := range []string{"bm.inbox.host.k16", "bm.hostreply.k16.abc"} {
		if !IsHostTraffic(s) {
			t.Errorf("%q: want host traffic", s)
		}
	}
	for _, s := range []string{"bm.inbox.app", "bm.inbox.hosts-1", "bm.daemon.k16.started", "bm.app.started", "bm.feedback.app"} {
		if IsHostTraffic(s) {
			t.Errorf("%q: not host traffic", s)
		}
	}
}
