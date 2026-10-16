package hosts

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	return log
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
	got, err := s.Get(KeyAdmin, "sign roster v2")
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("Get = %v", err)
	}
	if out.buf.String() != "Keychain dialog follows for: sign roster v2\n" {
		t.Fatalf("announcement %q", out.buf.String())
	}
	lines := readLog(t, log)
	seed := hex.EncodeToString(key.Seed())
	for _, l := range lines {
		if strings.Contains(l, seed) {
			t.Fatalf("seed in argv: %q", l)
		}
	}
	// The ACL check and the announcement come before the -w read.
	n := out.lines
	if n < 1 || lines[n-1] != "dump-keychain -a" {
		t.Fatalf("before announcement: %q", lines[:n])
	}
	if len(lines) != n+1 || lines[n] != "find-generic-password -s bmesh -a admin -w" {
		t.Fatalf("after announcement: %q", lines[n:])
	}
	if lines[1] != "-i" {
		t.Fatalf("Put argv %q", lines[1])
	}

	if _, err := s.Get(KeyApprove, "approve"); err == nil || err.Error() != "no approve key in the Keychain" {
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
		_, err := s.Get(KeyAdmin, "x")
		if err == nil || err.Error() != want {
			t.Errorf("%s: Get = %v", acl, err)
		}
		if out.Len() != 0 || strings.Contains(strings.Join(readLog(t, log), "\n"), "-w") {
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
	_, err := s.Get(KeyApprove, "approve job")
	if err == nil || !strings.Contains(err.Error(), "Keychain failed") || !strings.Contains(err.Error(), "Mac's own screen") {
		t.Fatalf("denied Get = %v", err)
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
	s, err := NewStore("file", dir, nil, pass)
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
	got, err := s.Get(KeyAdmin, "sign roster")
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("Get = %v", err)
	}
	if _, err := s.Get(KeyAdmin, "x"); err == nil || err.Error() != "wrong passphrase for "+path {
		t.Fatalf("wrong passphrase: %v", err)
	}
	want := []string{"New passphrase for " + path, "Passphrase for " + path + " (sign roster)", "Passphrase for " + path + " (x)"}
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
	got, err := s.Get(KeyApprove, "a")
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("Get n=1024 = %v", err)
	}

	for _, bad := range []sealedKey{{N: 1 << 21, R: 8, P: 1}, {N: 1000, R: 8, P: 1}, {N: 1024, R: 33, P: 1}, {N: 1024, R: 8, P: 17}, {N: 1024, R: 0, P: 1}} {
		d := doc
		d.N, d.R, d.P = bad.N, bad.R, bad.P
		b, _ = json.Marshal(d)
		os.WriteFile(path, b, 0o600)
		if _, err := s.Get(KeyApprove, "a"); err == nil || !strings.Contains(err.Error(), "out of bounds") {
			t.Errorf("n=%d r=%d p=%d: %v", bad.N, bad.R, bad.P, err)
		}
	}
}
