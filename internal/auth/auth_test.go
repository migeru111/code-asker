package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFlow(t *testing.T) {
	a := New("secret-token")
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 401 {
		t.Fatalf("no token: %d", rec.Code)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("bearer: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	if !a.Login("secret-token", rec, httptest.NewRequest("POST", "/", nil)) {
		t.Fatal("login failed")
	}
	req = httptest.NewRequest("GET", "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("cookie: %d", rec.Code)
	}
}

func TestThrottle(t *testing.T) {
	a := New("secret-token")
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		a.Login("bad", rec, httptest.NewRequest("POST", "/", nil))
		if rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	if a.Login("secret-token", rec, httptest.NewRequest("POST", "/", nil)) || rec.Code != 429 {
		t.Fatalf("expected throttle, got %d", rec.Code)
	}
}
