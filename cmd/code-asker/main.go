// Command code-asker serves a read-only code viewer for iPad Safari.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/migeru111/code-asker/internal/auth"
	"github.com/migeru111/code-asker/internal/repo"
	"github.com/migeru111/code-asker/internal/safepath"
	"github.com/migeru111/code-asker/internal/server"
	"github.com/migeru111/code-asker/web"
)

var version = "dev"

func main() {
	rootDir := os.Getenv("CODE_ASKER_ROOT")
	token := os.Getenv("CODE_ASKER_TOKEN")
	listen := os.Getenv("CODE_ASKER_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:8080"
	}
	if rootDir == "" || token == "" {
		log.Fatal("CODE_ASKER_ROOT and CODE_ASKER_TOKEN must be set")
	}
	if len(token) < 8 {
		log.Fatal("CODE_ASKER_TOKEN must be at least 8 characters")
	}
	root, err := safepath.New(rootDir)
	if err != nil {
		log.Fatalf("root: %v", err)
	}
	srv := &http.Server{
		Addr:              listen,
		Handler:           server.New(auth.New(token), repo.NewStore(root), web.Static(), version).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()
	log.Printf("code-asker %s: root=%s listen=%s", version, root.Dir(), listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
