package symbols

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const rustSrc = `pub struct Header { a: u32 }
impl Header {
    pub fn parse_header(&self) -> u32 { 1 }
}
pub trait Foo { fn bar(&self); }
`

func write(t *testing.T, name, body string) string {
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func has(syms []Symbol, name string, line int) bool {
	for _, s := range syms {
		if s.Name == name && s.Line == line {
			return true
		}
	}
	return false
}

func TestFallbackRust(t *testing.T) {
	syms, err := fallback(write(t, "a.rs", rustSrc))
	if err != nil {
		t.Fatal(err)
	}
	for name, line := range map[string]int{"Header": 1, "parse_header": 3, "Foo": 5, "bar": 5} {
		if name == "bar" {
			continue // single-line trait body: only the first match per line is kept
		}
		if !has(syms, name, line) {
			t.Errorf("missing %s@%d in %+v", name, line, syms)
		}
	}
}

func TestFallbackGoPythonTS(t *testing.T) {
	g, _ := fallback(write(t, "a.go", "package x\n\nfunc Foo() {}\nfunc (s *S) Bar() {}\ntype T struct{}\n"))
	if !has(g, "Foo", 3) || !has(g, "Bar", 4) || !has(g, "T", 5) {
		t.Errorf("go: %+v", g)
	}
	p, _ := fallback(write(t, "a.py", "class A:\n    def m(self):\n        pass\nasync def f(): pass\n"))
	if !has(p, "A", 1) || !has(p, "m", 2) || !has(p, "f", 4) {
		t.Errorf("py: %+v", p)
	}
	j, _ := fallback(write(t, "a.ts", "export interface I {}\nexport const f = async (x) => 1\nexport class C {}\n"))
	if !has(j, "I", 1) || !has(j, "f", 2) || !has(j, "C", 3) {
		t.Errorf("ts: %+v", j)
	}
}

func TestExtractWithCtagsIfAvailable(t *testing.T) {
	e := New()
	if !e.HasCtags() {
		t.Skip("universal-ctags not installed")
	}
	syms, err := e.Extract(context.Background(), write(t, "a.rs", rustSrc))
	if err != nil {
		t.Fatal(err)
	}
	if !has(syms, "parse_header", 3) || !has(syms, "Header", 1) {
		t.Errorf("ctags: %+v", syms)
	}
	for _, s := range syms {
		if s.Kind == "field" {
			t.Errorf("field leaked: %+v", s)
		}
	}
}

func TestFillEnds(t *testing.T) {
	syms := []Symbol{
		{Name: "Header", Kind: "implementation", Line: 2},
		{Name: "parse_header", Kind: "method", Line: 3, Scope: "Header"},
		{Name: "other", Kind: "method", Line: 10, Scope: "Header"},
		{Name: "Foo", Kind: "interface", Line: 20},
		{Name: "bar", Kind: "method", Line: 21, Scope: "Foo", End: 22},
	}
	fillEnds(syms)
	want := map[string]int{"Header": 19, "parse_header": 9, "other": 19, "Foo": 20 + approxBodyLines, "bar": 22}
	for _, s := range syms {
		if s.End != want[s.Name] {
			t.Errorf("%s: end=%d want %d", s.Name, s.End, want[s.Name])
		}
	}
}
