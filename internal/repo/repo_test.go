package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/migeru111/code-asker/internal/safepath"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "src")
	os.MkdirAll(filepath.Join(root, "proj", ".git"), 0o755)
	os.MkdirAll(filepath.Join(root, "proj", "sub"), 0o755)
	os.MkdirAll(filepath.Join(root, "notrepo"), 0o755)
	os.MkdirAll(filepath.Join(base, "out"), 0o755)
	os.WriteFile(filepath.Join(base, "out", "secret"), []byte("s"), 0o644)
	os.Symlink(filepath.Join(base, "out"), filepath.Join(root, "proj", "escape"))
	os.WriteFile(filepath.Join(root, "proj", "a.txt"), []byte("l1\nl2\r\nl3\nl4"), 0o644)
	os.WriteFile(filepath.Join(root, "proj", "bin"), []byte("ab\x00cd"), 0o644)
	os.WriteFile(filepath.Join(root, "proj", "long"), []byte(strings.Repeat("x", 100000)+"\nnext\n"), 0o644)
	r, err := safepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return NewStore(r)
}

func TestReposAndList(t *testing.T) {
	s := newStore(t)
	rs, err := s.Repos()
	if err != nil || len(rs) != 1 || rs[0].Name != "proj" {
		t.Fatalf("repos: %v %v", rs, err)
	}
	es, err := s.List("proj", "")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name)
	}
	got := strings.Join(names, ",")
	if got != "sub,a.txt,bin,long" { // escape symlink and .git hidden
		t.Fatalf("entries: %s", got)
	}
	if _, err := s.List("proj", "escape"); err == nil {
		t.Fatal("escape should fail")
	}
	if _, err := s.List("../x", ""); err == nil {
		t.Fatal("bad repo name should fail")
	}
}

func TestRead(t *testing.T) {
	s := newStore(t)
	f, err := s.Read("proj", "a.txt", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if f.TotalLines != 4 || f.Start != 2 || f.End != 3 || strings.Join(f.Lines, "|") != "l2|l3" {
		t.Fatalf("%+v", f)
	}
	f, _ = s.Read("proj", "bin", 1, 10)
	if !f.Binary {
		t.Fatal("expected binary")
	}
	f, _ = s.Read("proj", "long", 1, 10)
	if f.TotalLines != 2 || len(f.Lines[0]) > MaxLineBytes+40 || f.Lines[1] != "next" {
		t.Fatalf("long: total=%d len=%d", f.TotalLines, len(f.Lines[0]))
	}
	if _, err := s.Read("proj", "escape/secret", 1, 10); err == nil {
		t.Fatal("escape read should fail")
	}
	if _, err := s.Read("proj", ".git/config", 1, 10); err == nil {
		t.Fatal(".git read should fail")
	}
}
