package hosts

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// testKey is a fixed key: the seed is SHA-256("bmesh golden key").
func testKey() ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("bmesh golden key"))
	return ed25519.NewKeyFromSeed(seed[:])
}

func testPub() string { return KeyHex(testKey().Public().(ed25519.PublicKey)) }

// golden pins a document's signed bytes (as SHA-256, the full bytes for the
// job) and its signature, so a change to the encoding fails here.
type golden struct {
	name      string
	bytes     func() []byte
	sig       func() string
	wantBytes string // hex SHA-256 of the signed bytes
	wantSig   string
}

func goldenDocs() []golden {
	k := testKey()
	job := &Job{ID: "j1", By: "k16", To: "ricks-macbook", Op: "ping", Repo: "", Args: []string{"b", "a"},
		RosterVersion: 2, TS: "2026-10-16T09:00:00Z", Expires: "2026-10-17T09:00:00Z", Salt: "00112233445566778899aabbccddeeff"}
	job.Sign(k)
	reply := &Reply{JobID: "j1", JobSalt: job.Salt, By: "ricks-macbook", To: "k16", OK: true, Text: "1a2b3c4d roster v2",
		TS: "2026-10-16T09:00:05Z", Salt: "ffeeddccbbaa99887766554433221100"}
	reply.Sign(k)
	approve := &Approve{JobID: "j1", JobSalt: job.Salt, JobSHA256: job.Digest(), By: "ricks-macbook", Decision: "approve",
		TS: "2026-10-16T09:01:00Z", Salt: "0123456789abcdef0123456789abcdef"}
	approve.Sign(k)
	roster := &Roster{Version: 2, AdminKey: testPub(), IssuedAt: "2026-10-16T08:00:00Z", Hosts: []Host{
		{Name: "ricks-macbook", Key: testPub(), Secure: true, ApproveKey: testPub(), Accept: []string{"ping"}},
		{Name: "k16", Key: testPub(), Accept: []string{"prune", "ping", "level", "ping"}},
	}}
	roster.Sign(k)
	presence := &Presence{Host: "k16", Rev: "1a2b3c4d", OS: "linux", Routines: true, Notes: "",
		Repos: []PresenceRepo{{Name: "pstack", Path: "/home/rick/repos/pstack", Origin: "git@github.com:r/pstack.git"},
			{Name: "buildermesh", Path: "/home/rick/repos/buildermesh", Origin: "git@github.com:r/buildermesh.git"}},
		TS: "2026-10-16T09:00:00Z"}
	presence.Sign(k)
	registry := &Registry{Version: 7, AdminKey: testPub(), IssuedAt: "2026-10-16T10:00:00Z",
		Rows: "[repos.app]\npath = \"~/repos/app\"\nverify = [\"go test ./...\"]\n"}
	registry.Sign(k)
	return []golden{
		{"job", job.Bytes, func() string { return job.Sig }, "a1cd8ace1d86fc48363d40784e49bdb648fc3494ebdf77968639e609b4949548", "4f0267d3db28bdef9adc1a957ae2a6b853966e01088ed24e751b69448e58ee1cec68058280613759c2ab5845239a0317f2ef4c7761cdfe95b50373ec6494ef07"},
		{"reply", reply.Bytes, func() string { return reply.Sig }, "0b41e8d05b8ba20b07e096312f333caabd5b9ec06664f496fe7bee95ec179822", "e5b9c3e914f318ef728a07310d314682dfca079fb6207d289e22f24ff6ead4b75be43c254d54c7a583b2d105fd94722aad1de63472e3cb19e6026f7f92265201"},
		{"approve", approve.Bytes, func() string { return approve.Sig }, "a87858c941e359a71b23e9daa043680e4c9c41a8146bfb6231e1dc5cf9fec25a", "d8504f9e1e839209ebac9d635b76869779981225b3a4ac0a345bc6d92ee8ff940e8ba038745d532ae64f892e3b40b81af48c9699c0662f62397dfc95960e5603"},
		{"roster", roster.Bytes, func() string { return roster.Sig }, "650c40140438c038b0f4265f6fdd6363198baaed7951731185a9f4403ee881e6", "33f413b1090fd1f0d8ecca7712483d2423defb9b2188e2d66723957d6b67bd472ea3501fe2559e053feee0b645e5c547c006371ebcb1ae3dbfab6a2f566df503"},
		{"registry", registry.Bytes, func() string { return registry.Sig }, "00ff4eb44f9d06d8a2631162cacad3caff41aa0dc9fb29cf600d17b49befa66c", "e819fd74108500e9ff4d3b1424e6b9b165d7834608c7f9bff23996bf670d1fd2f6e20cdd3e70af40598168b1f26470c913933107323c9a0470cf641bac8e1d0c"},
		{"presence", presence.Bytes, func() string { return presence.Sig }, "17e56cc250c5663f744bebe9557748ffeca6d08765cf4a5ad7f2411e20e303fa", "325e9d77e2cf0e7504fca85adf425046b0fd8efb358a7f10a11beccb8cf24a34c5631c5c264b861d76aed25059c94da0ba982d7300f59d37c9f8dcfdc6e9310f"},
	}
}

func TestGoldenVectors(t *testing.T) {
	for _, g := range goldenDocs() {
		sum := sha256.Sum256(g.bytes())
		if got := hex.EncodeToString(sum[:]); got != g.wantBytes {
			t.Errorf("%s bytes sha256 = %s, want %s", g.name, got, g.wantBytes)
		}
		if got := g.sig(); got != g.wantSig {
			t.Errorf("%s sig = %s, want %s", g.name, got, g.wantSig)
		}
	}
}

// TestJobBytes pins the encoding itself on the job: the tag first, each
// scalar length-prefixed, args a counted list in their given order.
func TestJobBytes(t *testing.T) {
	j := &Job{Type: TagJob, ID: "j", By: "a", To: "b", Op: "ping", Args: []string{"y", "x"}, RosterVersion: 12,
		TS: "t", Expires: "e", Salt: "s"}
	want := "0000000c" + hex.EncodeToString([]byte(TagJob)) +
		"00000001" + "6a" + // id
		"00000001" + "61" + // by
		"00000001" + "62" + // to
		"00000004" + hex.EncodeToString([]byte("ping")) +
		"00000000" + // repo
		"00000002" + "00000001" + "79" + "00000001" + "78" + // args, as given
		"00000002" + "3132" + // roster_version, decimal ASCII
		"00000001" + "74" + "00000001" + "65" + "00000001" + "73"
	if got := hex.EncodeToString(j.Bytes()); got != want {
		t.Fatalf("job bytes\n got %s\nwant %s", got, want)
	}
}

func TestNamesSortedAndDeduplicated(t *testing.T) {
	r1 := &Roster{Version: 1, AdminKey: testPub(), Hosts: []Host{{Name: "b", Accept: []string{"ping", "level", "ping"}}, {Name: "a"}}}
	r2 := &Roster{Version: 1, AdminKey: testPub(), Hosts: []Host{{Name: "a", Accept: nil}, {Name: "b", Accept: []string{"level", "ping"}}}}
	if hex.EncodeToString(r1.Bytes()) != hex.EncodeToString(r2.Bytes()) {
		t.Fatal("host order or accept order/duplicates changed the roster's bytes")
	}
}

func TestVerifyRefusesAChangedMessage(t *testing.T) {
	k := testKey()
	j := &Job{ID: "j1", By: "k16", To: "mac", Op: "ping", TS: "2026-10-16T09:00:00Z", Expires: "2026-10-16T10:00:00Z", Salt: "aa"}
	j.Sign(k)
	if err := j.Verify(testPub()); err != nil {
		t.Fatal(err)
	}
	j.To = "other"
	if err := j.Verify(testPub()); err == nil {
		t.Fatal("a changed job verified")
	}
}

func TestRosterVerify(t *testing.T) {
	k := testKey()
	r := &Roster{Version: 3, AdminKey: testPub(), Hosts: []Host{{Name: "a", Key: testPub()}}}
	r.Sign(k)
	if err := r.Verify(testPub()); err != nil {
		t.Fatal(err)
	}
	other := ed25519.NewKeyFromSeed(make([]byte, 32))
	if err := r.Verify(KeyHex(other.Public().(ed25519.PublicKey))); err == nil {
		t.Fatal("a roster verified against another pinned key")
	}
	r.Hosts = append(r.Hosts, Host{Name: "a"})
	r.Sig = sign(k, r.Bytes())
	if err := r.Verify(testPub()); err == nil {
		t.Fatal("a roster naming a host twice verified")
	}
}

func TestFingerprint(t *testing.T) {
	if got := Fingerprint(testPub()); len(got) != 16 {
		t.Fatalf("fingerprint %q: want 16 hex characters", got)
	}
	if Fingerprint("nothex") != "?" {
		t.Fatal("a bad key has a fingerprint")
	}
}

func TestParseStamp(t *testing.T) {
	if _, err := ParseStamp("2026-10-16T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"2026-10-16T09:00:00.5Z", "2026-10-16T10:00:00+01:00", "yesterday"} {
		if _, err := ParseStamp(s); err == nil {
			t.Errorf("%q parsed", s)
		}
	}
}
