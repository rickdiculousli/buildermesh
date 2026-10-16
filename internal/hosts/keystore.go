package hosts

import (
	"bufio"
	"bytes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/scrypt"
)

// The protected keys' names.
const (
	KeyAdmin   = "admin"
	KeyApprove = "approve"
)

// Passphrase asks for a passphrase; confirm asks twice and refuses a
// mismatch (or an empty one).
type Passphrase func(prompt string, confirm bool) ([]byte, error)

// Store holds the protected key (admin or approve) agents can't use.
type Store interface {
	Kind() string // "keychain" or "file"
	Exists(name string) (bool, error)
	Put(name string, key ed25519.PrivateKey) error
	Get(name, action string) (ed25519.PrivateKey, error) // action names what the key is read for
}

// DefaultKeyStore is [hosts] key_store's default: "keychain" on darwin,
// else "file".
func DefaultKeyStore(goos string) string {
	if goos == "darwin" {
		return "keychain"
	}
	return "file"
}

// NewStore is the store of kind ("keychain" | "file"); out receives the
// dialog announcement; pass is used by the file store.
func NewStore(kind, configDir string, out io.Writer, pass Passphrase) (Store, error) {
	switch kind {
	case "keychain":
		if out == nil {
			out = io.Discard
		}
		return &keychainStore{out: out}, nil
	case "file":
		if pass == nil {
			return nil, errors.New("file key store: no way to ask for a passphrase")
		}
		return &fileStore{dir: configDir, pass: pass}, nil
	}
	return nil, fmt.Errorf("key_store %q: want \"keychain\" or \"file\"", kind)
}

var keyNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func checkKeyName(name string) error {
	if !keyNameRe.MatchString(name) {
		return fmt.Errorf("key name %q: want lowercase letters, digits and dashes", name)
	}
	return nil
}

// ---- Keychain ----

const keychainService = "bmesh"

type keychainStore struct{ out io.Writer }

func (*keychainStore) Kind() string { return "keychain" }

// security runs the security command ($BMESH_SECURITY_COMMAND split on
// spaces, else "security") with args and stdin.
func (*keychainStore) security(stdin string, args ...string) (stdout []byte, code int, err error) {
	argv := strings.Fields(os.Getenv("BMESH_SECURITY_COMMAND"))
	if len(argv) == 0 {
		argv = []string{"security"}
	}
	cmd := exec.Command(argv[0], append(argv[1:], args...)...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = ee.Error()
		}
		return out.Bytes(), ee.ExitCode(), fmt.Errorf("security %s: %s", args[0], msg)
	}
	if err != nil {
		return nil, -1, err
	}
	return out.Bytes(), 0, nil
}

func (k *keychainStore) Exists(name string) (bool, error) {
	if err := checkKeyName(name); err != nil {
		return false, err
	}
	_, code, err := k.security("", "find-generic-password", "-s", keychainService, "-a", name)
	switch {
	case err == nil:
		return true, nil
	case code == 44: // errSecItemNotFound
		return false, nil
	}
	return false, err
}

// Put writes the item through `security -i` so the seed is on stdin, never
// in argv; -T "" leaves the trusted-app list empty, so every read raises
// the dialog.
func (k *keychainStore) Put(name string, key ed25519.PrivateKey) error {
	if err := checkKeyName(name); err != nil {
		return err
	}
	if len(key) != ed25519.PrivateKeySize {
		return errors.New("not an Ed25519 private key")
	}
	line := fmt.Sprintf("add-generic-password -U -s %s -a %s -l \"bmesh %s key\" -T \"\" -w %s\n",
		keychainService, name, name, hex.EncodeToString(key.Seed()))
	out, _, err := k.security(line, "-i")
	if err != nil {
		return err
	}
	ok, err := k.Exists(name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("storing the %s key in the Keychain failed: %s", name, strings.TrimSpace(string(out)))
	}
	return nil
}

func (k *keychainStore) Get(name, action string) (ed25519.PrivateKey, error) {
	if err := checkKeyName(name); err != nil {
		return nil, err
	}
	dump, _, err := k.security("", "dump-keychain", "-a")
	if err != nil {
		return nil, err
	}
	if err := checkKeychainACL(dump, name); err != nil {
		return nil, err
	}
	fmt.Fprintf(k.out, "Keychain dialog follows for: %s\n", action)
	out, _, err := k.security("", "find-generic-password", "-s", keychainService, "-a", name, "-w")
	if err != nil {
		return nil, fmt.Errorf("reading the %s key from the Keychain failed (%v); admin and approve commands run at the Mac's own screen, where its dialog can be answered", name, err)
	}
	seed, err := parseSeed(strings.TrimSpace(string(out)))
	if err != nil {
		return nil, fmt.Errorf("the %s key's Keychain item: %w", name, err)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

var (
	appsCountRe = regexp.MustCompile(`^applications \((\d+)\):$`)
	appLineRe   = regexp.MustCompile(`^\d+: (.*?)(?: \([^()]*\))?$`)
)

// checkKeychainACL finds the bmesh <name> item in `security dump-keychain
// -a` output and refuses when any access entry that allows decrypt trusts an
// app (applications: <null> trusts every app).
func checkKeychainACL(dump []byte, name string) error {
	var items [][]string
	var cur []string
	sc := bufio.NewScanner(bytes.NewReader(dump))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "keychain: ") && cur != nil {
			items = append(items, cur)
			cur = nil
		}
		cur = append(cur, line)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if cur != nil {
		items = append(items, cur)
	}
	svce := `"svce"<blob>="` + keychainService + `"`
	acct := `"acct"<blob>="` + name + `"`
	for _, it := range items {
		var genp, hasSvce, hasAcct bool
		for _, l := range it {
			switch strings.TrimSpace(l) {
			case `class: "genp"`:
				genp = true
			case svce:
				hasSvce = true
			case acct:
				hasAcct = true
			}
		}
		if genp && hasSvce && hasAcct {
			if app := trustedApp(it); app != "" {
				return fmt.Errorf("the %s key's Keychain item trusts %s; remove it in Keychain Access", name, app)
			}
			return nil
		}
	}
	return fmt.Errorf("no %s key in the Keychain", name)
}

// trustedApp is the first app a decrypt-allowing access entry trusts:
// "every app" for applications: <null>; "" when none does.
func trustedApp(lines []string) string {
	decrypt := false
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(l, "entry ") && strings.HasSuffix(l, ":"):
			decrypt = false
		case strings.HasPrefix(l, "authorizations"):
			_, auths, _ := strings.Cut(l, ":")
			decrypt = false
			for _, a := range strings.Fields(auths) {
				if a == "decrypt" || a == "any" {
					decrypt = true
				}
			}
		case !decrypt:
		case l == "applications: <null>":
			return "every app"
		case appsCountRe.MatchString(l):
			n, _ := strconv.Atoi(appsCountRe.FindStringSubmatch(l)[1])
			if n == 0 {
				continue
			}
			for j := i + 1; j < len(lines); j++ {
				if m := appLineRe.FindStringSubmatch(strings.TrimSpace(lines[j])); m != nil {
					return m[1]
				}
			}
			return "an app"
		}
	}
	return ""
}

// ---- File ----

const (
	scryptN = 32768
	scryptR = 8
	scryptP = 1
)

type fileStore struct {
	dir  string
	pass Passphrase
}

type sealedKey struct {
	KDF    string `json:"kdf"`
	N      int    `json:"n"`
	R      int    `json:"r"`
	P      int    `json:"p"`
	Salt   string `json:"salt"`
	Nonce  string `json:"nonce"`
	Sealed string `json:"sealed"`
}

func (*fileStore) Kind() string { return "file" }

func (f *fileStore) path(name string) string { return filepath.Join(f.dir, name+".key") }

func keyAD(name string) []byte { return []byte("bmesh " + name + " key v1") }

func (f *fileStore) Exists(name string) (bool, error) {
	if err := checkKeyName(name); err != nil {
		return false, err
	}
	_, err := os.Stat(f.path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (f *fileStore) Put(name string, key ed25519.PrivateKey) error {
	if err := checkKeyName(name); err != nil {
		return err
	}
	if len(key) != ed25519.PrivateKeySize {
		return errors.New("not an Ed25519 private key")
	}
	path := f.path(name)
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	pass, err := f.pass("New passphrase for "+path, true)
	if err != nil {
		return err
	}
	if len(pass) == 0 {
		return errors.New("empty passphrase")
	}
	salt := make([]byte, 16)
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	aead, err := fileAEAD(pass, salt, scryptN, scryptR, scryptP)
	if err != nil {
		return err
	}
	doc := sealedKey{KDF: "scrypt", N: scryptN, R: scryptR, P: scryptP,
		Salt: hex.EncodeToString(salt), Nonce: hex.EncodeToString(nonce),
		Sealed: hex.EncodeToString(aead.Seal(nil, nonce, key.Seed(), keyAD(name)))}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := writeNew(path, append(b, '\n')); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s already exists", path)
		}
		return err
	}
	return nil
}

func (f *fileStore) Get(name, action string) (ed25519.PrivateKey, error) {
	if err := checkKeyName(name); err != nil {
		return nil, err
	}
	path := f.path(name)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no %s key: %s doesn't exist", name, path)
		}
		return nil, err
	}
	var doc sealedKey
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc.KDF != "scrypt" {
		return nil, fmt.Errorf("%s: kdf %q: want scrypt", path, doc.KDF)
	}
	if doc.N < 2 || doc.N > 1<<20 || doc.N&(doc.N-1) != 0 || doc.R < 1 || doc.R > 32 || doc.P < 1 || doc.P > 16 {
		return nil, fmt.Errorf("%s: scrypt parameters n=%d r=%d p=%d out of bounds", path, doc.N, doc.R, doc.P)
	}
	salt, err1 := hex.DecodeString(doc.Salt)
	nonce, err2 := hex.DecodeString(doc.Nonce)
	sealed, err3 := hex.DecodeString(doc.Sealed)
	if err := errors.Join(err1, err2, err3); err != nil || len(nonce) != chacha20poly1305.NonceSizeX || len(salt) == 0 {
		return nil, fmt.Errorf("%s: malformed salt, nonce or sealed key", path)
	}
	pass, err := f.pass(fmt.Sprintf("Passphrase for %s (%s)", path, action), false)
	if err != nil {
		return nil, err
	}
	aead, err := fileAEAD(pass, salt, doc.N, doc.R, doc.P)
	if err != nil {
		return nil, err
	}
	seed, err := aead.Open(nil, nonce, sealed, keyAD(name))
	if err != nil {
		return nil, fmt.Errorf("wrong passphrase for %s", path)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s: sealed key is %d bytes, want %d", path, len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func fileAEAD(pass, salt []byte, n, r, p int) (cipher.AEAD, error) {
	k, err := scrypt.Key(pass, salt, n, r, p, chacha20poly1305.KeySize)
	if err != nil {
		return nil, err
	}
	return chacha20poly1305.NewX(k)
}
