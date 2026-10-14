package search

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRun(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "sub"), 0o755)
	os.WriteFile(filepath.Join(d, "a.txt"), []byte("hello\nParse_Header here\n"), 0o644)
	os.WriteFile(filepath.Join(d, "sub", "b.txt"), []byte("x parse_header(y)\n"), 0o644)

	r, err := Run(context.Background(), d, "", "parse_header", Options{})
	if err != nil || len(r.Matches) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	r, _ = Run(context.Background(), d, "sub", "parse_header", Options{})
	if len(r.Matches) != 1 || r.Matches[0].Path != "sub/b.txt" || r.Matches[0].Line != 1 {
		t.Fatalf("sub: %+v", r)
	}
	// a leading dash must be treated as the pattern, not as a flag
	if r, err = Run(context.Background(), d, "", "--files", Options{}); err != nil || len(r.Matches) != 0 {
		t.Fatalf("dash: %+v %v", r, err)
	}
	// fixed-string by default: "(" is literal; as a regex it is invalid
	if _, err = Run(context.Background(), d, "", "parse_header(", Options{}); err != nil {
		t.Fatalf("literal paren: %v", err)
	}
	if _, err = Run(context.Background(), d, "", "(", Options{Regex: true}); !errors.Is(err, ErrBadQuery) {
		t.Fatalf("invalid regex should be ErrBadQuery, got %v", err)
	}
	if _, err = Run(context.Background(), d, "", "  ", Options{}); !errors.Is(err, ErrBadQuery) {
		t.Fatal("empty query")
	}
}
