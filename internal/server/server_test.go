package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/migeru111/code-asker/internal/auth"
	"github.com/migeru111/code-asker/internal/repo"
	"github.com/migeru111/code-asker/internal/safepath"
)

func newTestServer(t *testing.T) http.Handler {
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
	return New(auth.New("test-token-123"), repo.NewStore(r), fstest.MapFS{"index.html": {Data: []byte("hi")}}, "test").Handler()
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
