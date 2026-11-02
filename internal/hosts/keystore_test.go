package hosts

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// fakeSecurity points BMESH_SECURITY_COMMAND at testdata/fake-security.sh
// and returns its argv log path.
func fakeSecurity(t *testing.T) string {
	t.Helper()
	script, err := filepath.Abs("../../testdata/fake-security.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	t.Setenv("BMESH_SECURITY_COMMAND", "sh "+script)
	t.Setenv("FAKE_SECURITY_DIR", filepath.Join(dir, "items"))
	t.Setenv("FAKE_SECURITY_LOG", log)
	t.Setenv("FAKE_SECURITY_ACL", "")
	t.Setenv("FAKE_SECURITY_DENY", "")
	osa, err := filepath.Abs("../../testdata/fake-osascript.sh")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_OSASCRIPT_COMMAND", "sh "+osa)
	t.Setenv("FAKE_OSASCRIPT_LOG", log)
	t.Setenv("FAKE_OSASCRIPT_BODY", filepath.Join(dir, "body"))
	t.Setenv("FAKE_OSASCRIPT_ANSWER", "")
	return log
}

// rosterRequest is a request to sign roster v2, the bytes it shows.
func rosterRequest() Request {
	r := Roster{Version: 2, AdminKey: KeyHex(keyFor("admin").Public().(ed25519.PublicKey)), IssuedAt: "2026-11-02T10:00:00Z",
		Hosts: []Host{{Name: "k16", Key: KeyHex(keyFor("k16").Public().(ed25519.PublicKey)), Accept: []string{"stop-thread", "nix", "ping"}}}}
	r.Prepare()
	return Request{Title: "Sign roster v2", Details: []string{"k16: accept nix,ping -> nix,ping,stop-thread"}, Signs: [][]byte{r.Bytes()}}
}

func readLog(t *testing.T, path string) []string {
	b, _ := os.ReadFile(path)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// announceWriter records how many log lines existed when the announcement
// was written.
type announceWriter struct {
	log   string
	t     *testing.T
	buf   bytes.Buffer
	lines int
}

func (w *announceWriter) Write(p []byte) (int, error) {
	w.lines = len(readLog(w.t, w.log))
	return w.buf.Write(p)
}

func TestDefaultKeyStore(t *testing.T) {
	if DefaultKeyStore("darwin") != "keychain" || DefaultKeyStore("linux") != "file" {
		t.Fatal("DefaultKeyStore")
	}
	if _, err := NewStore("vault", t.TempDir(), nil, nil); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestKeychainStore(t *testing.T) {
	log := fakeSecurity(t)
	out := &announceWriter{log: log, t: t}
	s, err := NewStore("keychain", t.TempDir(), out, nil)
	if err != nil || s.Kind() != "keychain" {
		t.Fatal(s, err)
	}
	if ok, err := s.Exists(KeyAdmin); err != nil || ok {
		t.Fatalf("Exists before Put = %v, %v", ok, err)
	}
	key := newKey(t)
	if err := s.Put(KeyAdmin, key); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Exists(KeyAdmin); err != nil || !ok {
		t.Fatalf("Exists after Put = %v, %v", ok, err)
	}
	req := rosterRequest()
	got, err := s.Get(KeyAdmin, req)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("Get = %v", err)
	}
	want := "bmesh: Sign roster v2\n  k16: accept nix,ping -> nix,ping,stop-thread\n  signs " + req.Sum() + "\n"
	if out.buf.String() != want {
		t.Fatalf("terminal %q, want %q", out.buf.String(), want)
	}
	body, _ := os.ReadFile(os.Getenv("FAKE_OSASCRIPT_BODY"))
	if string(body) != "k16: accept nix,ping -> nix,ping,stop-thread\nsigns "+req.Sum()+"\n" {
		t.Fatalf("dialog body %q", body)
	}
	lines := readLog(t, log)
	seed := hex.EncodeToString(key.Seed())
	for _, l := range lines {
		if strings.Contains(l, seed) {
			t.Fatalf("seed in argv: %q", l)
		}
	}
	// The terminal, then the dialog, then security: the ACL check and the
	// -w read.
	n := out.lines
	after := []string{"osascript bmesh: Sign roster v2", "dump-keychain -a", "find-generic-password -s bmesh -a admin -w"}
	if strings.Join(lines[n:], "|") != strings.Join(after, "|") {
		t.Fatalf("after the terminal's request: %q", lines[n:])
	}
	if lines[1] != "-i" {
		t.Fatalf("Put argv %q", lines[1])
	}

	if _, err := s.Get(KeyApprove, req); err == nil || err.Error() != "no approve key in the Keychain" {
		t.Fatalf("Get absent = %v", err)
	}
}

func TestKeychainACLRefused(t *testing.T) {
	for acl, want := range map[string]string{
		"app":  "the admin key's Keychain item trusts /usr/bin/security; remove it in Keychain Access",
		"null": "the admin key's Keychain item trusts every app; remove it in Keychain Access",
	} {
		log := fakeSecurity(t)
		var out bytes.Buffer
		s, _ := NewStore("keychain", t.TempDir(), &out, nil)
		if err := s.Put(KeyAdmin, newKey(t)); err != nil {
			t.Fatal(err)
		}
		t.Setenv("FAKE_SECURITY_ACL", acl)
		_, err := s.Get(KeyAdmin, rosterRequest())
		if err == nil || err.Error() != want {
			t.Errorf("%s: Get = %v", acl, err)
		}
		if strings.Contains(strings.Join(readLog(t, log), "\n"), "-w") {
			t.Errorf("%s: read attempted after refusal", acl)
		}
	}
}

func TestKeychainDenied(t *testing.T) {
	fakeSecurity(t)
	s, _ := NewStore("keychain", t.TempDir(), nil, nil)
	if err := s.Put(KeyApprove, newKey(t)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_SECURITY_DENY", "1")
	_, err := s.Get(KeyApprove, rosterRequest())
	if err == nil || !strings.Contains(err.Error(), "Keychain failed") || !strings.Contains(err.Error(), "Mac's own screen") {
		t.Fatalf("denied Get = %v", err)
	}
}

// Cancel, no answer within the dialog's wait, or no dialog at all never
// reaches security, so nothing is read or signed.
func TestKeychainDialogNotContinued(t *testing.T) {
	for answer, want := range map[string]string{
		"cancel": "cancelled at the dialog: nothing signed",
		"giveup": "cancelled at the dialog: nothing signed (no answer in 2m0s)",
		"fail":   "the bmesh dialog couldn't be shown",
	} {
		log := fakeSecurity(t)
		s, _ := NewStore("keychain", t.TempDir(), nil, nil)
		if err := s.Put(KeyAdmin, newKey(t)); err != nil {
			t.Fatal(err)
		}
		before := len(readLog(t, log))
		t.Setenv("FAKE_OSASCRIPT_ANSWER", answer)
		_, err := s.Get(KeyAdmin, rosterRequest())
		if err == nil || !strings.HasPrefix(err.Error(), want) || errors.Is(err, ErrCancelled) != (answer != "fail") {
			t.Errorf("%s: Get = %v", answer, err)
		}
		if got := readLog(t, log)[before:]; len(got) != 1 || got[0] != "osascript bmesh: Sign roster v2" {
			t.Errorf("%s: after the dialog %q", answer, got)
		}
	}
}

func TestRequestSumIsTheSignedBytes(t *testing.T) {
	req := rosterRequest()
	sum := sha256.Sum256(req.Signs[0])
	if req.Sum() != hex.EncodeToString(sum[:])[:8] {
		t.Fatalf("Sum %s", req.Sum())
	}
	// The signature is over exactly the bytes shown.
	r := Roster{Version: 2, AdminKey: KeyHex(keyFor("admin").Public().(ed25519.PublicKey)), IssuedAt: "2026-11-02T10:00:00Z",
		Hosts: []Host{{Name: "k16", Key: KeyHex(keyFor("k16").Public().(ed25519.PublicKey)), Accept: []string{"nix", "ping", "stop-thread"}}}}
	r.Prepare()
	r.Sig = req.Sign(keyFor("admin"))[0]
	if err := r.Verify(r.AdminKey); err != nil {
		t.Fatal(err)
	}
	// Several items: one sum over them in order, one signature each.
	two := Request{Signs: [][]byte{[]byte("a"), []byte("b")}}
	ab := sha256.Sum256([]byte("ab"))
	if two.Sum() != hex.EncodeToString(ab[:])[:8] || len(two.Sign(keyFor("admin"))) != 2 {
		t.Fatalf("two items: %s", two.Sum())
	}
	if (Request{}).Sum() != "" || strings.Contains(strings.Join(Request{Details: []string{"x"}}.Lines(), "\n"), "signs") {
		t.Fatal("a request signing nothing shows a sum")
	}
}

func TestRequestDetailsCapped(t *testing.T) {
	req := Request{Signs: [][]byte{[]byte("x")}}
	for i := range 20 {
		req.Details = append(req.Details, fmt.Sprintf("line %d", i))
	}
	got := req.Lines()
	if len(got) != MaxDetails+1 || got[MaxDetails-2] != "line 10" || got[MaxDetails-1] != "... and 9 more" ||
		got[MaxDetails] != "signs "+req.Sum() || len(req.Details) != 20 || req.Details[11] != "line 11" {
		t.Fatalf("Lines %q", got)
	}
	req.Details = req.Details[:MaxDetails]
	if got := req.Lines(); len(got) != MaxDetails+1 || got[MaxDetails-1] != "line 11" {
		t.Fatalf("12 lines: %q", got)
	}
}

func TestCheckKeychainACLOtherItems(t *testing.T) {
	// A trusted app on another item doesn't refuse ours; ours must be found
	// among several.
	dump := `keychain: "/k"
class: "genp"
attributes:
    "acct"<blob>="admin"
    "svce"<blob>="other"
access: 1 entries
    entry 0:
        authorizations (1): decrypt
        applications: <null>
keychain: "/k"
class: "genp"
attributes:
    "acct"<blob>="admin"
    "svce"<blob>="bmesh"
access: 1 entries
    entry 0:
        authorizations (1): decrypt
        applications (0):
`
	if err := checkKeychainACL([]byte(dump), KeyAdmin); err != nil {
		t.Fatal(err)
	}
}

func passFn(answers ...string) (Passphrase, *[]string) {
	var prompts []string
	return func(prompt string, confirm bool) ([]byte, error) {
		prompts = append(prompts, prompt)
		if len(answers) == 0 {
			return nil, errors.New("no more answers")
		}
		a := answers[0]
		answers = answers[1:]
		return []byte(a), nil
	}, &prompts
}

func TestFileStore(t *testing.T) {
	dir := t.TempDir()
	pass, prompts := passFn("hunter2", "hunter2", "wrong")
	var out bytes.Buffer
	s, err := NewStore("file", dir, &out, pass)
	if err != nil || s.Kind() != "file" {
		t.Fatal(s, err)
	}
	if ok, err := s.Exists(KeyAdmin); err != nil || ok {
		t.Fatalf("Exists = %v, %v", ok, err)
	}
	key := newKey(t)
	if err := s.Put(KeyAdmin, key); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "admin.key")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi.Mode(), err)
	}
	b, _ := os.ReadFile(path)
	if bytes.Contains(b, []byte(hex.EncodeToString(key.Seed()))) {
		t.Fatal("seed in the file")
	}
	var doc sealedKey
	if err := json.Unmarshal(b, &doc); err != nil || doc.KDF != "scrypt" || doc.N != 32768 || doc.R != 8 || doc.P != 1 || len(doc.Salt) != 32 || len(doc.Nonce) != 48 {
		t.Fatalf("doc %s: %v", b, err)
	}
	if ok, _ := s.Exists(KeyAdmin); !ok {
		t.Fatal("Exists after Put")
	}
	req := rosterRequest()
	got, err := s.Get(KeyAdmin, req)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("Get = %v", err)
	}
	if want := "bmesh: Sign roster v2\n  k16: accept nix,ping -> nix,ping,stop-thread\n  signs " + req.Sum() + "\n"; out.String() != want {
		t.Fatalf("terminal %q", out.String())
	}
	if _, err := s.Get(KeyAdmin, req); err == nil || err.Error() != "wrong passphrase for "+path {
		t.Fatalf("wrong passphrase: %v", err)
	}
	want := []string{"New passphrase for " + path, "Passphrase for " + path, "Passphrase for " + path}
	if strings.Join(*prompts, "|") != strings.Join(want, "|") {
		t.Fatalf("prompts %q", *prompts)
	}
	if err := s.Put(KeyAdmin, newKey(t)); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("overwrite: %v", err)
	}
}

func TestFileStoreParams(t *testing.T) {
	dir := t.TempDir()
	pass, _ := passFn("pw", "pw", "pw")
	s, _ := NewStore("file", dir, nil, pass)
	key := newKey(t)
	if err := s.Put(KeyApprove, key); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "approve.key")
	b, _ := os.ReadFile(path)
	var doc sealedKey
	json.Unmarshal(b, &doc)
	salt, _ := hex.DecodeString(doc.Salt)
	nonce, _ := hex.DecodeString(doc.Nonce)

	// A file with a small n opens: the parameters come from the file.
	aead, err := fileAEAD([]byte("pw"), salt, 1024, 8, 1)
	if err != nil {
		t.Fatal(err)
	}
	doc.N = 1024
	doc.Sealed = hex.EncodeToString(aead.Seal(nil, nonce, key.Seed(), keyAD(KeyApprove)))
	b, _ = json.Marshal(doc)
	os.WriteFile(path, b, 0o600)
	got, err := s.Get(KeyApprove, Request{Title: "a"})
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("Get n=1024 = %v", err)
	}

	for _, bad := range []sealedKey{{N: 1 << 21, R: 8, P: 1}, {N: 1000, R: 8, P: 1}, {N: 1024, R: 33, P: 1}, {N: 1024, R: 8, P: 17}, {N: 1024, R: 0, P: 1}} {
		d := doc
		d.N, d.R, d.P = bad.N, bad.R, bad.P
		b, _ = json.Marshal(d)
		os.WriteFile(path, b, 0o600)
		if _, err := s.Get(KeyApprove, Request{Title: "a"}); err == nil || !strings.Contains(err.Error(), "out of bounds") {
			t.Errorf("n=%d r=%d p=%d: %v", bad.N, bad.R, bad.P, err)
		}
	}
}
