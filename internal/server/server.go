// Package server wires the HTTP API and the static frontend together.
package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"strconv"

	"github.com/migeru111/code-asker/internal/auth"
	"github.com/migeru111/code-asker/internal/repo"
	"github.com/migeru111/code-asker/internal/safepath"
)

type Server struct {
	auth    *auth.Auth
	store   *repo.Store
	static  fs.FS
	version string
}

func New(a *auth.Auth, store *repo.Store, static fs.FS, version string) *Server {
	return &Server{auth: a, store: store, static: static, version: version}
}

// Handler returns the root handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "version": s.version})
	})
	mux.HandleFunc("POST /api/login", s.login)

	api := http.NewServeMux()
	api.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	api.HandleFunc("GET /api/repos", s.repos)
	api.HandleFunc("GET /api/tree", s.tree)
	api.HandleFunc("GET /api/file", s.file)
	mux.Handle("/api/", s.auth.Middleware(api))

	// The static shell contains no repository data; every data endpoint is authenticated.
	mux.Handle("/", noCache(http.FileServer(http.FS(s.static))))
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	if s.auth.Login(body.Token, w, r) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}
}

func (s *Server) repos(w http.ResponseWriter, r *http.Request) {
	rs, err := s.store.Repos()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, rs)
}

func (s *Server) tree(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	es, err := s.store.List(q.Get("repo"), q.Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, es)
}

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	start, _ := strconv.Atoi(q.Get("start"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	f, err := s.store.Read(q.Get("repo"), q.Get("path"), start, limit)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, f)
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrBadName):
		writeErr(w, 400, "invalid repository name")
	case errors.Is(err, safepath.ErrOutside), errors.Is(err, safepath.ErrInvalid):
		writeErr(w, 403, "forbidden path")
	case errors.Is(err, repo.ErrNotFound), errors.Is(err, os.ErrNotExist):
		writeErr(w, 404, "not found")
	default:
		writeErr(w, 500, "internal error")
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
