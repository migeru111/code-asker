package web

import (
	"os/exec"
	"testing"
)

// TestLocateJS runs the frontend's location-parsing unit tests with Node (skipped when node is absent).
func TestLocateJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "--test", "tests/locate.test.js").CombinedOutput()
	if err != nil {
		t.Fatalf("node --test failed:\n%s", out)
	}
}
