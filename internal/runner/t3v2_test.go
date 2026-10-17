package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp/t3fake"
)

// v2Setup is a fake T3 0.1.0, a sign-in for it at access and a runner on it
// in mode; it returns the runner, the fake and the token file.
func v2Setup(t *testing.T, access, mode string) (*T3, *t3fake.Server, string) {
	t.Helper()
	fake := t3fake.New()
	t.Cleanup(fake.Close)
	path := filepath.Join(t.TempDir(), "t3-token.json")
	exp := time.Now().Add(29 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := t3mcp.WriteToken(path, t3mcp.Token{Server: fake.URL, Token: fake.MintToken(access, exp), ExpiresAt: exp,
		Scopes: []string{"orchestration:read", "orchestration:operate"}, Access: access}); err != nil {
		t.Fatal(err)
	}
	r := &T3{Server: fake.URL, RuntimeMode: mode, Cred: t3mcp.FileCredential{Path: path}, Timeout: 5 * time.Second}
	return r, fake, path
}

func writeBriefFile(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// lastCall is the arguments of the newest call of tool.
func lastCall(fake *t3fake.Server, tool string) map[string]any {
	var args map[string]any
	fake.Do(func(s *t3fake.Server) {
		for _, c := range s.Calls {
			if c.Tool == tool {
				args = c.Args
			}
		}
	})
	return args
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestT3V2StartLaunchesUnderTheRepoProject(t *testing.T) {
	r, fake, _ := v2Setup(t, "auto", "auto")
	repo := t.TempDir()
	brief := writeBriefFile(t, "build spec 012")
	spec := StartSpec{RepoPath: repo, Model: "m1", Effort: "high", Title: "[build] demo 012", BriefPath: brief,
		ThreadID: "uuid-1"}
	// An auto sign-in can't register a project.
	if _, err := r.Start(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "add the repo as a project in T3") {
		t.Fatalf("unregistered repo: %v", err)
	}
	fake.Do(func(s *t3fake.Server) {
		s.Projects = append(s.Projects, t3fake.Project{ID: "mcp:p1", Title: "demo", WorkspaceRoot: repo})
	})
	got, err := r.Start(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Thread, "mcp:") || got.Branch != "bmesh/uuid-1" ||
		got.Worktree != filepath.Join("/fake-t3/worktrees", filepath.Base(repo), "bmesh/uuid-1") {
		t.Fatalf("started %+v", got)
	}
	want := `{"message":"build spec 012","modelSelection":{"model":"m1","options":{"effort":"high"},"provider":"claudeAgent"},` +
		`"projectId":"mcp:p1","runtimeMode":"auto","title":"[build] demo 012",` +
		`"workspaceStrategy":{"baseRef":"main","branch":"bmesh/uuid-1","type":"worktree"}}`
	if a := jsonOf(lastCall(fake, "t3_thread_launch")); a != want {
		t.Fatalf("launch %s\nwant   %s", a, want)
	}
	// StartSpec.Branch is the base.
	spec.ThreadID, spec.Branch = "uuid-2", "release"
	if _, err := r.Start(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if ws := lastCall(fake, "t3_thread_launch")["workspaceStrategy"]; jsonOf(ws) != `{"baseRef":"release","branch":"bmesh/uuid-2","type":"worktree"}` {
		t.Fatalf("workspace %s", jsonOf(ws))
	}
}

func TestT3V2StartRegistersTheProjectWithFullAccess(t *testing.T) {
	r, fake, _ := v2Setup(t, "full-access", "full-access")
	repo := t.TempDir()
	got, err := r.Start(context.Background(), StartSpec{RepoPath: repo, Model: "m1", Title: "t", BriefPath: writeBriefFile(t, "x"), ThreadID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	var root string
	fake.Do(func(s *t3fake.Server) { root = s.Projects[0].WorkspaceRoot })
	if root != repo || got.Thread == "" {
		t.Fatalf("project %q started %+v", root, got)
	}
	if a := lastCall(fake, "t3_thread_launch"); a["runtimeMode"] != "full-access" || a["modelSelection"].(map[string]any)["options"] != nil {
		t.Fatalf("launch %s", jsonOf(a))
	}
}

func TestT3V2StartReconcilesALostReplyByBranch(t *testing.T) {
	r, fake, _ := v2Setup(t, "auto", "auto")
	repo := t.TempDir()
	fake.Do(func(s *t3fake.Server) {
		s.Projects = append(s.Projects, t3fake.Project{ID: "mcp:p1", WorkspaceRoot: repo})
		s.HangLaunch = true
	})
	// Another thread with the same title, on another branch, is not it.
	fake.AddThread(&t3fake.Thread{ID: "mcp:other", ProjectID: "mcp:p1", Title: "[build] demo 012", Branch: "bmesh/old"})
	r.Timeout = 300 * time.Millisecond
	got, err := r.Start(context.Background(), StartSpec{RepoPath: repo, Model: "m1", Title: "[build] demo 012",
		BriefPath: writeBriefFile(t, "b"), ThreadID: "uuid-9"})
	if err != nil {
		t.Fatal(err)
	}
	var made string
	fake.Do(func(s *t3fake.Server) {
		for id, th := range s.Threads {
			if th.Branch == "bmesh/uuid-9" {
				made = id
			}
		}
	})
	if got.Thread != made || made == "" || got.Branch != "bmesh/uuid-9" {
		t.Fatalf("started %+v, made %q", got, made)
	}
	// With nothing to find, the timeout is the error.
	fake.Do(func(s *t3fake.Server) { s.HangLaunch, s.LoseLaunch = false, true })
	_, err = r.Start(context.Background(), StartSpec{RepoPath: repo, Model: "m1", Title: "[build] demo 012",
		BriefPath: writeBriefFile(t, "b"), ThreadID: "uuid-10"})
	var to *StartTimeout
	if !errors.Is(err, ErrTimeout) || !errors.As(err, &to) || to.Branch != "bmesh/uuid-10" {
		t.Fatalf("lost launch: %v", err)
	}
}

func TestT3V2StartInAnExistingWorktree(t *testing.T) {
	r, fake, _ := v2Setup(t, "auto", "auto")
	repo := t.TempDir()
	fake.Do(func(s *t3fake.Server) {
		s.Projects = append(s.Projects, t3fake.Project{ID: "mcp:p1", WorkspaceRoot: repo})
	})
	got, err := r.Start(context.Background(), StartSpec{RepoPath: repo, Model: "m1", Title: "[ideate] demo round 1: engineer",
		BriefPath: writeBriefFile(t, "propose"), ThreadID: "uuid-2", Branch: "demo-round-1-eng",
		Worktree: "/bm/wt/demo/demo-round-1-eng", Effort: "xhigh"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch != "demo-round-1-eng" || got.Worktree != "/bm/wt/demo/demo-round-1-eng" {
		t.Fatalf("started %+v", got)
	}
	a := lastCall(fake, "t3_thread_launch")
	if jsonOf(a["workspaceStrategy"]) != `{"branch":"demo-round-1-eng","type":"existing_worktree","worktreePath":"/bm/wt/demo/demo-round-1-eng"}` ||
		a["projectId"] != "mcp:p1" {
		t.Fatalf("launch %s", jsonOf(a))
	}
}

func TestT3V2StatusMapsRunStates(t *testing.T) {
	r, fake, _ := v2Setup(t, "auto", "auto")
	for _, c := range []struct {
		run     string
		running bool
	}{
		{"preparing", true}, {"queued", true}, {"starting", true}, {"running", true}, {"waiting", true},
		{"completed", false}, {"interrupted", false}, {"failed", false}, {"cancelled", false}, {"rolled_back", false},
	} {
		id := "mcp:" + c.run
		fake.AddThread(&t3fake.Thread{ID: id, Runs: []t3fake.Run{{ID: "r1", Status: "completed"}, {ID: "r2", Status: c.run}}})
		st, err := r.Status(context.Background(), id)
		if err != nil || st.Running != c.running || !st.Known {
			t.Errorf("%s: %+v %v", c.run, st, err)
		}
		// Only an interrupt is a stop: a restart's cancel and a crash's
		// failure are not.
		if stopped := c.run == "interrupted"; st.Stopped != stopped || stopped && st.StoppedRun != "r2" || !stopped && st.StoppedRun != "" {
			t.Errorf("%s: stopped %+v", c.run, st)
		}
	}
	fake.AddThread(&t3fake.Thread{ID: "mcp:active", ActiveRunID: "r1", Runs: []t3fake.Run{{ID: "r1", Status: "completed"}}})
	if st, _ := r.Status(context.Background(), "mcp:active"); !st.Running {
		t.Errorf("activeRunId: %+v", st)
	}
	fake.AddThread(&t3fake.Thread{ID: "mcp:asks", Pending: 1, Runs: []t3fake.Run{{ID: "r1", Status: "waiting"}}})
	if st, _ := r.Status(context.Background(), "mcp:asks"); !st.Waiting || !st.Running {
		t.Errorf("waiting: %+v", st)
	}
	fake.AddThread(&t3fake.Thread{ID: "mcp:limited", LastErrorClass: "usage_limit", UsageLimitResetAt: "2026-10-16T09:00:00Z",
		Runs: []t3fake.Run{{ID: "r1", Status: "failed"}}})
	if st, _ := r.Status(context.Background(), "mcp:limited"); st.Running || !st.UsageLimit || st.LimitResetAt != "2026-10-16T09:00:00Z" {
		t.Errorf("usage limit: %+v", st)
	}
	fake.AddThread(&t3fake.Thread{ID: "mcp:new"})
	if st, _ := r.Status(context.Background(), "mcp:new"); st.Running {
		t.Errorf("no runs: %+v", st)
	}
	if _, err := r.Status(context.Background(), "mcp:gone"); !errors.Is(err, ErrNoThread) {
		t.Errorf("missing: %v", err)
	}
}

func TestT3V2StatusFollowsTheNewestReply(t *testing.T) {
	r, fake, _ := v2Setup(t, "auto", "auto")
	th := fake.AddThread(&t3fake.Thread{ID: "mcp:t", ActiveRunID: "r1", Runs: []t3fake.Run{{ID: "r1", Status: "running"}},
		Items: []t3fake.Item{{Type: "user_message", Status: "completed", Text: "go"}, {Type: "assistant_message", Status: "running", Text: "work"}}})
	if st, _ := r.Status(context.Background(), "mcp:t"); st.LastReply != "work" {
		t.Fatalf("streaming: %+v", st)
	}
	// The streamed reply grows and completes; the cursor waited for it.
	fake.Do(func(*t3fake.Server) {
		th.Items[1] = t3fake.Item{Type: "assistant_message", Status: "completed", Text: "work done"}
		th.ActiveRunID, th.Runs[0].Status = "", "completed"
	})
	if st, _ := r.Status(context.Background(), "mcp:t"); st.LastReply != "work done" || st.Running {
		t.Fatalf("completed: %+v", st)
	}
	// A long timeline is paged through.
	fake.Do(func(*t3fake.Server) {
		for i := 0; i < 250; i++ {
			th.Items = append(th.Items, t3fake.Item{Type: "assistant_message", Status: "completed", Text: "reply " + string(rune('a'+i%26))})
		}
		th.Items = append(th.Items, t3fake.Item{Type: "user_message", Status: "completed", Text: "next"})
	})
	if st, _ := r.Status(context.Background(), "mcp:t"); st.LastReply != "reply p" { // item 249: 249%26 = 15
		t.Fatalf("paged: %+v", st)
	}
	// Later reads start where the last one stopped.
	before := len(fake.ToolCalls())
	if st, _ := r.Status(context.Background(), "mcp:t"); st.LastReply != "reply p" || len(fake.ToolCalls()) != before+1 {
		t.Fatalf("cursor: %+v, %d calls", st, len(fake.ToolCalls())-before)
	}
	if a := lastCall(fake, "t3_thread_read"); a["afterPosition"] != float64(2*252) {
		t.Fatalf("read args %s", jsonOf(a))
	}
}

func TestT3V2SendRefusesARunningThreadAndQueues(t *testing.T) {
	r, fake, _ := v2Setup(t, "auto", "auto")
	now := time.Date(2026, 10, 16, 3, 0, 10, 0, time.UTC)
	r.Now = func() time.Time { return now }
	th := fake.AddThread(&t3fake.Thread{ID: "mcp:t", ActiveRunID: "r1", Runs: []t3fake.Run{{ID: "r1", Status: "running"}}})
	if err := r.Send(context.Background(), "mcp:t", "hello"); !errors.Is(err, ErrRunning) {
		t.Fatalf("running: %v", err)
	}
	idle := func() {
		fake.Do(func(*t3fake.Server) {
			th.ActiveRunID = ""
			for i := range th.Runs {
				th.Runs[i].Status = "completed"
			}
		})
	}
	idle()
	if err := r.Send(context.Background(), "mcp:t", "hello"); err != nil {
		t.Fatal(err)
	}
	a := lastCall(fake, "t3_thread_send")
	if a["mode"] != "queue" || a["message"] != "hello" || a["clientRequestId"] != sendRequestID("mcp:t", "hello", now) ||
		!strings.HasPrefix(a["clientRequestId"].(string), "bmesh-") || len(a["clientRequestId"].(string)) != 6+64 {
		t.Fatalf("send %s", jsonOf(a))
	}
	// A retry within the minute carries the same id, so T3 sends it once.
	idle()
	now = now.Add(40 * time.Second)
	if err := r.Send(context.Background(), "mcp:t", "hello"); err != nil {
		t.Fatal(err)
	}
	var sends []t3fake.Send
	fake.Do(func(s *t3fake.Server) { sends = append(sends, s.Sends...) })
	if len(sends) != 1 {
		t.Fatalf("sends %+v", sends)
	}
	// The next minute is a new wake.
	idle()
	now = now.Add(time.Minute)
	if err := r.Send(context.Background(), "mcp:t", "hello"); err != nil {
		t.Fatal(err)
	}
	fake.Do(func(s *t3fake.Server) { sends = append([]t3fake.Send{}, s.Sends...) })
	if len(sends) != 2 || sends[1].ClientRequestID == sends[0].ClientRequestID {
		t.Fatalf("sends %+v", sends)
	}
}

func TestT3V2InterruptAnIdleThread(t *testing.T) {
	r, fake, _ := v2Setup(t, "auto", "auto")
	fake.AddThread(&t3fake.Thread{ID: "mcp:t", Runs: []t3fake.Run{{ID: "r1", Status: "completed"}}})
	if err := r.Interrupt(context.Background(), "mcp:t"); err != nil {
		t.Fatalf("idle: %v", err)
	}
	fake.AddThread(&t3fake.Thread{ID: "mcp:busy", ActiveRunID: "r1", Runs: []t3fake.Run{{ID: "r1", Status: "running"}}})
	if err := r.Interrupt(context.Background(), "mcp:busy"); err != nil {
		t.Fatal(err)
	}
	if st, _ := r.Status(context.Background(), "mcp:busy"); st.Running || !st.Stopped || st.StoppedRun != "r1" {
		t.Fatalf("not stopped: %+v", st)
	}
}

func TestT3V2SignIn(t *testing.T) {
	r, fake, path := v2Setup(t, "auto", "full-access")
	repo := t.TempDir()
	fake.Do(func(s *t3fake.Server) {
		s.Projects = append(s.Projects, t3fake.Project{ID: "mcp:p1", WorkspaceRoot: repo})
	})
	spec := StartSpec{RepoPath: repo, Model: "m", Title: "t", BriefPath: writeBriefFile(t, "b"), ThreadID: "u"}
	var se *SignInError
	_, err := r.Start(context.Background(), spec)
	if !errors.As(err, &se) || se.Msg != "T3 sign-in allows auto, below [runner] t3_runtime_mode full-access; run bmesh t3 login and approve full-access" {
		t.Fatalf("ceiling: %v", err)
	}
	r.RuntimeMode = "auto"
	tok, _ := t3mcp.ReadToken(path)
	tok.ExpiresAt = time.Now().Add(-time.Minute)
	if err := t3mcp.WriteToken(path, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start(context.Background(), spec); !errors.As(err, &se) || se.Msg != "T3 sign-in missing or expired; run bmesh t3 login" {
		t.Fatalf("expired: %v", err)
	}
	os.Remove(path)
	if _, err := r.Status(context.Background(), "mcp:t"); !errors.As(err, &se) || se.Msg != signInHint {
		t.Fatalf("missing: %v", err)
	}
	tok.ExpiresAt, tok.Server = time.Now().Add(time.Hour), "http://127.0.0.1:1"
	if err := t3mcp.WriteToken(path, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start(context.Background(), spec); !errors.As(err, &se) || !strings.Contains(se.Msg, "not [runner] t3_server") {
		t.Fatalf("other server: %v", err)
	}
	// A token T3 refuses.
	tok.Server, tok.Token = fake.URL, "revoked"
	if err := t3mcp.WriteToken(path, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Status(context.Background(), "mcp:t"); !errors.As(err, &se) {
		t.Fatalf("refused: %v", err)
	}
}

func TestT3V2ReinitializesAnExpiredSession(t *testing.T) {
	r, fake, _ := v2Setup(t, "auto", "auto")
	fake.AddThread(&t3fake.Thread{ID: "mcp:t"})
	if _, err := r.Status(context.Background(), "mcp:t"); err != nil {
		t.Fatal(err)
	}
	fake.ExpireSessions()
	if _, err := r.Status(context.Background(), "mcp:t"); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
}

// The recorded T3 0.1.0 read parses into the status the fake's does.
func TestT3V2RecordedReadShape(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "t3v2", "t3_thread_read.json"))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		StructuredContent v2Read `json:"structuredContent"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent
	st := statusFrom(v)
	if st.Running || st.Waiting || len(v.Items) == 0 || v.Thread.Branch != "probe-q5" || len(v.RecentRuns) == 0 {
		t.Fatalf("status %+v read %+v", st, v)
	}
}
