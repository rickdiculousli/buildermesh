package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp/t3fake"
)

// urlWatcher is a stdout that hands the authorize URL, once printed, to
// onURL.
type urlWatcher struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	fired bool
	onURL func(string)
}

func (w *urlWatcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if !w.fired {
		for _, line := range strings.Split(w.buf.String(), "\n") {
			if strings.Contains(line, "/oauth/mcp/authorize?") {
				w.fired = true
				go w.onURL(strings.TrimSpace(line))
				break
			}
		}
	}
	return len(p), nil
}

func (w *urlWatcher) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// t3Setup is setup with [runner] t3_server on a fake T3 0.1.0.
func t3Setup(t *testing.T) (*mesh.Client, *t3fake.Server) {
	t.Helper()
	c, _ := setup(t)
	fake := t3fake.New()
	t.Cleanup(fake.Close)
	f, err := os.OpenFile(os.Getenv("BMESH_CONFIG"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("[runner]\nkind = \"t3\"\nt3_server = \"" + fake.URL + "\"\n")
	f.Close()
	return c, fake
}

func tokenPath() string {
	return filepath.Join(filepath.Dir(os.Getenv("BMESH_CONFIG")), "t3-token.json")
}

// login runs `bmesh t3 login` and answers the authorize URL with answer.
func login(t *testing.T, answer func(authorizeURL string, stdin io.Writer)) (string, string, int) {
	t.Helper()
	t.Setenv("BMESH_NODE", "")
	pr, pw := io.Pipe()
	defer pw.Close()
	out := &urlWatcher{onURL: func(u string) { answer(u, pw) }}
	var errb bytes.Buffer
	code := run([]string{"t3", "login", "--callback-port", "0", "--timeout", "20s"}, pr, out, &errb)
	return out.String(), errb.String(), code
}

func TestT3LoginWithAPastedRedirect(t *testing.T) {
	_, fake := t3Setup(t)
	out, errs, code := login(t, func(u string, stdin io.Writer) {
		redirect, err := fake.AuthorizeRedirect(u)
		if err != nil {
			t.Error(err)
			return
		}
		io.WriteString(stdin, "not a redirect\n"+redirect+"\n")
	})
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	port := strings.TrimPrefix(fake.URL, "http://127.0.0.1:")
	for _, want := range []string{"ssh -N -L " + port + ":127.0.0.1:" + port + " -L ", " " + testHost + "\n",
		"t3 auth pairing create", "Choose Auto and approve", "signed in: " + testHost + ": auto, 30 days left"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	st, err := os.Stat(tokenPath())
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("token file %v %v", st, err)
	}
	tok, err := t3mcp.ReadToken(tokenPath())
	if err != nil || tok.Server != fake.URL || tok.Access != "auto" || tok.DaysLeft(time.Now()) != 30 ||
		strings.Join(tok.Scopes, " ") != "orchestration:read orchestration:operate" {
		t.Fatalf("token %+v %v", tok, err)
	}
	out, errs, code = runCLI(t, nil, "t3", "login", "--status")
	if code != 0 || !strings.HasPrefix(out, testHost+": auto, 30 days left\nserver "+fake.URL+", scopes orchestration:read orchestration:operate, expires ") {
		t.Fatalf("status %d %q %q", code, out, errs)
	}
}

// The browser reached T3 as localhost while bmesh asks 127.0.0.1: the code
// still exchanges, and the redirect lands on the listener.
func TestT3LoginOnTheListenerThroughAnotherOrigin(t *testing.T) {
	_, fake := t3Setup(t)
	fake.Do(func(s *t3fake.Server) { s.Access = "full-access" })
	out, errs, code := login(t, func(u string, _ io.Writer) {
		redirect, err := fake.AuthorizeRedirect(strings.Replace(u, "127.0.0.1", "localhost", 1))
		if err != nil {
			t.Error(err)
			return
		}
		resp, err := http.Get(redirect)
		if err != nil {
			t.Error(err)
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(b), "signed in to T3") {
			t.Errorf("callback %d %q", resp.StatusCode, b)
		}
	})
	if code != 0 || !strings.Contains(out, "signed in: "+testHost+": full-access, 30 days left") {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
}

func TestT3LoginStatusWithoutASignIn(t *testing.T) {
	t3Setup(t)
	if _, errs, code := runCLI(t, nil, "t3", "login", "--status"); code != 3 || errs != "bmesh: no T3 sign-in on "+testHost+"; run bmesh t3 login\n" {
		t.Fatalf("code %d err %q", code, errs)
	}
}

func TestLeadStartRefusesAnExpiredSignIn(t *testing.T) {
	c, fake := t3Setup(t)
	writeRepos(t, "[repos.demo]\npath = \""+t.TempDir()+"\"\n")
	if err := t3mcp.WriteToken(tokenPath(), t3mcp.Token{Server: fake.URL, Token: fake.MintToken("auto", time.Now().Add(-time.Hour)),
		ExpiresAt: time.Now().Add(-time.Hour), Access: "auto"}); err != nil {
		t.Fatal(err)
	}
	_, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null")
	if code != 3 || errs != "bmesh: T3 sign-in missing or expired; run bmesh t3 login\n" {
		t.Fatalf("code %d err %q", code, errs)
	}
	if _, ok, _ := mesh.GetNode(t.Context(), c, "demo-012"); ok || len(fake.ToolCalls()) != 0 {
		t.Fatalf("registered %v, calls %v", ok, fake.ToolCalls())
	}
}

func TestLeadStartOnAV2Server(t *testing.T) {
	c, fake := t3Setup(t)
	repo := t.TempDir()
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\n")
	fake.Do(func(s *t3fake.Server) {
		s.Projects = append(s.Projects, t3fake.Project{ID: "mcp:p1", WorkspaceRoot: repo})
	})
	exp := time.Now().Add(20 * 24 * time.Hour)
	if err := t3mcp.WriteToken(tokenPath(), t3mcp.Token{Server: fake.URL, Token: fake.MintToken("auto", exp), ExpiresAt: exp, Access: "auto"}); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	n, ok, err := mesh.GetNode(t.Context(), c, "demo-012")
	if err != nil || !ok || !strings.HasPrefix(n.Runner.Thread, "mcp:") || !uuidRE.MatchString(n.Runner.Ref) || n.Branch != "bmesh/"+n.Runner.Ref ||
		n.Worktree != filepath.Join("/fake-t3/worktrees", filepath.Base(repo), n.Branch) {
		t.Fatalf("node %+v %v %v", n, ok, err)
	}
}
