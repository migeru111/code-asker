package safepath

import (
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) (*Root, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{filepath.Join(root, "repo", "src"), filepath.Join(root, "repo", ".git"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(root, "repo", "src", "a.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o644)
	os.Symlink(outside, filepath.Join(root, "repo", "escape"))
	os.Symlink(filepath.Join(root, "repo", "src"), filepath.Join(root, "repo", "inner"))
	r, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	return r, base
}

func TestResolve(t *testing.T) {
	r, _ := setup(t)
	ok := []string{"", "repo", "repo/src/a.go", "repo/inner/a.go"}
	for _, p := range ok {
		if _, err := r.Resolve(p); err != nil {
			t.Errorf("Resolve(%q) unexpected error: %v", p, err)
		}
	}
	bad := []string{"..", "repo/../../outside", "/etc/passwd", "repo/escape/secret", "repo/escape", "repo/.git", "repo/.git/config", "repo\\x", "repo/a\x00b"}
	for _, p := range bad {
		if _, err := r.Resolve(p); err == nil {
			t.Errorf("Resolve(%q) should fail", p)
		}
	}
}
