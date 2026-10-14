// Package auth implements fixed-token authentication with a derived session cookie.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const CookieName = "ca_session"

// Auth validates the shared token.
type Auth struct {
	token   []byte
	cookie  string
	mu      sync.Mutex
	fails   map[string][]time.Time
	maxFail int
	window  time.Duration
}

// New creates an Auth for the given token.
func New(token string) *Auth {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("code-asker-session-v1"))
	return &Auth{
		token:   []byte(token),
		cookie:  hex.EncodeToString(mac.Sum(nil)),
		fails:   map[string][]time.Time{},
		maxFail: 5,
		window:  time.Minute,
	}
}

func eq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// Authorized reports whether the request carries a valid cookie or bearer token.
func (a *Auth) Authorized(r *http.Request) bool {
	if c, err := r.Cookie(CookieName); err == nil && eq(c.Value, a.cookie) {
		return true
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return eq(strings.TrimPrefix(h, "Bearer "), string(a.token))
	}
	return false
}

// Middleware rejects unauthenticated requests with 401.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Authorized(r) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *Auth) throttled(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	cut := time.Now().Add(-a.window)
	keep := a.fails[ip][:0]
	for _, t := range a.fails[ip] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	a.fails[ip] = keep
	return len(keep) >= a.maxFail
}

func (a *Auth) recordFail(ip string) {
	a.mu.Lock()
	a.fails[ip] = append(a.fails[ip], time.Now())
	a.mu.Unlock()
}

// Login checks the posted token and sets the session cookie.
func (a *Auth) Login(token string, w http.ResponseWriter, r *http.Request) bool {
	ip := clientIP(r)
	if a.throttled(ip) {
		http.Error(w, `{"error":"too many attempts"}`, http.StatusTooManyRequests)
		return false
	}
	if !eq(token, string(a.token)) {
		a.recordFail(ip)
		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    a.cookie,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   180 * 24 * 3600,
	})
	return true
}
