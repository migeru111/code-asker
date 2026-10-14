package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/migeru111/code-asker/internal/ai"
	"github.com/migeru111/code-asker/internal/auth"
	"github.com/migeru111/code-asker/internal/repo"
	"github.com/migeru111/code-asker/internal/safepath"
)

func newTestServer(t *testing.T) http.Handler { return newTestServerAI(t, nil) }

func newTestServerAI(t *testing.T, p ai.Provider) http.Handler {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "src")
	os.MkdirAll(filepath.Join(root, "proj", ".git"), 0o755)
	os.MkdirAll(filepath.Join(root, "proj", "sub"), 0o755)
	os.MkdirAll(filepath.Join(base, "out"), 0o755)
	os.WriteFile(filepath.Join(base, "out", "x.go"), []byte("package x\nfunc Secret() {}\n"), 0o644)
	os.Symlink(filepath.Join(base, "out"), filepath.Join(root, "proj", "escape"))
	os.WriteFile(filepath.Join(root, "proj", "a.go"), []byte("package a\n\nfunc ParseHeader() {}\n"), 0o644)
	os.WriteFile(filepath.Join(root, "proj", "sub", "b.go"), []byte("package b\nfunc ParseHeader2() {}\n"), 0o644)
	r, err := safepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return New(auth.New("test-token-123"), repo.NewStore(r), fstest.MapFS{"index.html": {Data: []byte("hi")}}, "test", p).Handler()
}

func get(h http.Handler, path string, authed bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if authed {
		req.Header.Set("Authorization", "Bearer test-token-123")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthRequired(t *testing.T) {
	h := newTestServer(t)
	for _, p := range []string{"/api/repos", "/api/tree?repo=proj", "/api/file?repo=proj&path=a.go", "/api/symbols?repo=proj&path=a.go", "/api/search?repo=proj&q=Parse"} {
		if c := get(h, p, false).Code; c != 401 {
			t.Errorf("%s without token: %d", p, c)
		}
	}
	if c := get(h, "/healthz", false).Code; c != 200 {
		t.Errorf("healthz: %d", c)
	}
}

func TestSymbols(t *testing.T) {
	h := newTestServer(t)
	rec := get(h, "/api/symbols?repo=proj&path=a.go", true)
	var syms []struct {
		Name string
		Line int
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &syms) != nil || len(syms) != 1 || syms[0].Name != "ParseHeader" || syms[0].Line != 3 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if c := get(h, "/api/symbols?repo=proj&path=escape/x.go", true).Code; c != 403 {
		t.Errorf("symbols escape: %d", c)
	}
}

func TestSearch(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	h := newTestServer(t)
	var res struct{ Matches []struct{ Path string } }
	rec := get(h, "/api/search?repo=proj&q=ParseHeader", true)
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &res) != nil || len(res.Matches) != 2 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// symlinks are not followed, so the file outside the root never shows up
	if rec = get(h, "/api/search?repo=proj&q=Secret", true); rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &res) != nil || len(res.Matches) != 0 {
		t.Fatalf("leak: %d %s", rec.Code, rec.Body)
	}
	for _, p := range []string{"/api/search?repo=proj&q=x&path=escape", "/api/search?repo=proj&q=x&path=..", "/api/search?repo=proj&q=x&path=.git"} {
		if c := get(h, p, true).Code; c != 403 {
			t.Errorf("%s: %d", p, c)
		}
	}
	if c := get(h, "/api/search?repo=proj&q=", true).Code; c != 400 {
		t.Errorf("empty q: %d", c)
	}
	if rec = get(h, "/api/search?repo=proj&q=%28&regex=1", true); rec.Code != 400 {
		t.Errorf("bad regex: %d %s", rec.Code, rec.Body)
	}
}

type fakeAI struct {
	mu   sync.Mutex
	got  ai.Request
	hold chan struct{} // if set, Ask blocks until closed
}

func (f *fakeAI) Name() string { return "fake" }
func (f *fakeAI) Ask(ctx context.Context, r ai.Request) (<-chan ai.Event, error) {
	f.mu.Lock()
	f.got = r
	f.mu.Unlock()
	ch := make(chan ai.Event, 4)
	go func() {
		defer close(ch)
		if f.hold != nil {
			<-f.hold
		}
		ch <- ai.Event{Type: ai.EventTool, Text: "Grep foo"}
		ch <- ai.Event{Type: ai.EventText, Text: "答え"}
		ch <- ai.Event{Type: ai.EventDone, Session: "sid"}
	}()
	return ch, nil
}

func post(h http.Handler, body string, authed bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/chat", strings.NewReader(body))
	if authed {
		req.Header.Set("Authorization", "Bearer test-token-123")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestChat(t *testing.T) {
	f := &fakeAI{}
	h := newTestServerAI(t, f)
	if c := post(h, `{}`, false).Code; c != 401 {
		t.Fatalf("no auth: %d", c)
	}
	rec := post(h, `{"repo":"proj","path":"a.go","start":3,"end":3,"question":"何？"}`, true)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{"event: start", `"start":3`, "event: tool", "event: text", "答え", "event: done", `"session":"sid"`} {
		if !strings.Contains(body, want) {
			t.Errorf("SSE lacks %q:\n%s", want, body)
		}
	}
	f.mu.Lock()
	got := f.got
	f.mu.Unlock()
	if !strings.HasSuffix(got.RepoDir, "proj") || got.Location.StartLine != 3 || got.Location.TotalLines != 3 ||
		!strings.Contains(got.Snippet, "ParseHeader") || got.Question != "何？" {
		t.Errorf("request to provider: %+v", got)
	}

	for body, want := range map[string]int{
		`{"repo":"proj","path":"a.go","question":""}`:         400,
		`{"repo":"proj","path":"escape/x.go","question":"q"}`: 403,
		`{"repo":"proj","path":".git/config","question":"q"}`: 403,
		`{"repo":"../x","path":"a.go","question":"q"}`:        400,
		`{"repo":"proj","path":"nope.go","question":"q"}`:     404,
		`not json`: 400,
	} {
		if c := post(h, body, true).Code; c != want {
			t.Errorf("%s: got %d want %d", body, c, want)
		}
	}
	if c := post(newTestServer(t), `{"repo":"proj","question":"q"}`, true).Code; c != 503 {
		t.Errorf("AI disabled: %d", c)
	}
}

func TestChatBusy(t *testing.T) {
	f := &fakeAI{hold: make(chan struct{})}
	h := newTestServerAI(t, f)
	done := make(chan int)
	go func() { done <- post(h, `{"repo":"proj","path":"a.go","question":"q"}`, true).Code }()
	for i := 0; i < 200; i++ { // wait until the first request holds the slot
		f.mu.Lock()
		started := f.got.Question != ""
		f.mu.Unlock()
		if started {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c := post(h, `{"repo":"proj","path":"a.go","question":"q2"}`, true).Code; c != 429 {
		t.Errorf("second concurrent question: %d", c)
	}
	close(f.hold)
	if c := <-done; c != 200 {
		t.Errorf("first: %d", c)
	}
}

func TestSymSearch(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	h := newTestServer(t)
	var hits []struct {
		Path, Name string
		Line, End  int
	}
	// "parse header" (spoken) must find ParseHeader and ParseHeader2; the exact name ranks first
	rec := get(h, "/api/symsearch?repo=proj&q=parse+header", true)
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &hits) != nil || len(hits) != 2 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if hits[0].Name != "ParseHeader" || hits[0].Path != "a.go" || hits[0].Line != 3 || hits[0].End < hits[0].Line || hits[1].Name != "ParseHeader2" {
		t.Errorf("ranking: %+v", hits)
	}
	if rec = get(h, "/api/symsearch?repo=proj&q=parse_header", true); rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &hits) != nil || len(hits) != 2 {
		t.Errorf("snake_case: %d %s", rec.Code, rec.Body)
	}
	if rec = get(h, "/api/symsearch?repo=proj&q=zzznothing", true); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("no match: %d %s", rec.Code, rec.Body)
	}
	for _, p := range []string{"/api/symsearch?repo=proj&q=", "/api/symsearch?repo=proj&q=a", "/api/symsearch?repo=..&q=abc"} {
		if c := get(h, p, true).Code; c != 400 {
			t.Errorf("%s: %d", p, c)
		}
	}
	if c := get(h, "/api/symsearch?repo=proj&q=abc", false).Code; c != 401 {
		t.Errorf("auth: %d", c)
	}
}

func TestChatSearchMode(t *testing.T) {
	f := &fakeAI{}
	h := newTestServerAI(t, f)
	rec := post(h, `{"repo":"proj","path":"a.go","mode":"search","question":"ログを書き出しているところは？"}`, true)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	f.mu.Lock()
	got := f.got
	f.mu.Unlock()
	if !got.Location.Search || got.Snippet != "" || got.Location.Path != "a.go" {
		t.Errorf("search mode request: %+v", got)
	}
	if !strings.Contains(ai.BuildPrompt(got), "Grep / Read で探し") {
		t.Errorf("prompt must tell the AI to search:\n%s", ai.BuildPrompt(got))
	}
	if c := post(h, `{"repo":"proj","path":"escape/x.go","mode":"search","question":"q"}`, true).Code; c != 403 {
		t.Errorf("hint path escape: %d", c)
	}
}
