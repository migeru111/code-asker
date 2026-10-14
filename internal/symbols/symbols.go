// Package symbols extracts function/type definitions from source files.
// It uses universal-ctags when available and falls back to simple regexes.
package symbols

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxFileBytes = 2 << 20

type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Line      int    `json:"line"`
	End       int    `json:"end,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// Extractor extracts symbols, caching by file identity.
type Extractor struct {
	ctags string // path of the ctags binary, "" if unavailable
	mu    sync.Mutex
	cache map[string][]Symbol
}

// New looks for universal-ctags on PATH.
func New() *Extractor {
	e := &Extractor{cache: map[string][]Symbol{}}
	if p, err := exec.LookPath("ctags"); err == nil {
		// Exuberant ctags has no JSON output; require the universal flavour.
		out, _ := exec.Command(p, "--version").Output()
		if bytes.Contains(out, []byte("Universal Ctags")) {
			e.ctags = p
		}
	}
	return e
}

// HasCtags reports whether ctags-based extraction is active.
func (e *Extractor) HasCtags() bool { return e.ctags != "" }

// Extract returns symbols for the file at the absolute path abs.
func (e *Extractor) Extract(ctx context.Context, abs string) ([]Symbol, error) {
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxFileBytes {
		return []Symbol{}, nil
	}
	key := abs + "|" + st.ModTime().String() + "|" + strconv.FormatInt(st.Size(), 10)
	e.mu.Lock()
	if v, ok := e.cache[key]; ok {
		e.mu.Unlock()
		return v, nil
	}
	e.mu.Unlock()

	var syms []Symbol
	if e.ctags != "" {
		syms, err = e.runCtags(ctx, abs)
	}
	if e.ctags == "" || err != nil {
		syms, err = fallback(abs)
		if err != nil {
			return nil, err
		}
	}
	if syms == nil {
		syms = []Symbol{}
	}
	fillEnds(syms)
	e.mu.Lock()
	if len(e.cache) > 256 {
		e.cache = map[string][]Symbol{}
	}
	e.cache[key] = syms
	e.mu.Unlock()
	return syms, nil
}

// kinds that are too noisy for an outline
var skipKinds = map[string]bool{
	"field": true, "local": true, "parameter": true, "enumerator": true,
	"member": true, "variable": true, "package": true, "label": true, "heading3": true, "heading4": true,
}

func (e *Extractor) runCtags(ctx context.Context, abs string) ([]Symbol, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.ctags, "--output-format=json", "--fields=+neKS", "--sort=no", "-f", "-", abs)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var syms []Symbol
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var t struct {
			Type      string `json:"_type"`
			Name      string `json:"name"`
			Kind      string `json:"kind"`
			Line      int    `json:"line"`
			End       int    `json:"end"`
			Scope     string `json:"scope"`
			Signature string `json:"signature"`
		}
		if json.Unmarshal(sc.Bytes(), &t) != nil || t.Type != "tag" || skipKinds[t.Kind] || t.Line < 1 {
			continue
		}
		syms = append(syms, Symbol{Name: t.Name, Kind: t.Kind, Line: t.Line, End: t.End, Scope: t.Scope, Signature: t.Signature})
	}
	return syms, nil
}

type pattern struct {
	kind string
	re   *regexp.Regexp
}

func pats(kv ...string) []pattern {
	var out []pattern
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, pattern{kv[i], regexp.MustCompile(kv[i+1])})
	}
	return out
}

var fallbackByExt = map[string][]pattern{
	".go": pats(
		"function", `^func\s+([A-Za-z_]\w*)\s*[(\[]`,
		"method", `^func\s+\([^)]*\)\s*([A-Za-z_]\w*)`,
		"type", `^type\s+([A-Za-z_]\w*)`),
	".rs": pats(
		"function", `^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+|const\s+|unsafe\s+|extern\s+"[^"]*"\s+)*fn\s+([A-Za-z_]\w*)`,
		"struct", `^\s*(?:pub(?:\([^)]*\))?\s+)?struct\s+([A-Za-z_]\w*)`,
		"enum", `^\s*(?:pub(?:\([^)]*\))?\s+)?enum\s+([A-Za-z_]\w*)`,
		"trait", `^\s*(?:pub(?:\([^)]*\))?\s+)?(?:unsafe\s+)?trait\s+([A-Za-z_]\w*)`,
		"impl", `^\s*(?:unsafe\s+)?impl(?:<[^>]*>)?\s+(?:[\w:<>, ]+\s+for\s+)?([A-Za-z_]\w*)`,
		"module", `^\s*(?:pub(?:\([^)]*\))?\s+)?mod\s+([A-Za-z_]\w*)`),
	".py": pats(
		"function", `^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)`,
		"class", `^\s*class\s+([A-Za-z_]\w*)`),
	".js": jsPats(), ".ts": jsPats(), ".tsx": jsPats(), ".jsx": jsPats(), ".mjs": jsPats(),
	".java": pats(
		"class", `^\s*(?:public\s+|private\s+|protected\s+|abstract\s+|final\s+|static\s+)*(?:class|interface|enum|record)\s+([A-Za-z_]\w*)`,
		"method", `^\s+(?:public|private|protected)\s+(?:static\s+|final\s+|abstract\s+|synchronized\s+)*[\w<>\[\], ?]+\s+([A-Za-z_]\w*)\s*\(`),
	".c": cPats(), ".h": cPats(), ".cc": cPats(), ".cpp": cPats(), ".hpp": cPats(),
}

func jsPats() []pattern {
	return pats(
		"function", `^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\*?\s+([A-Za-z_$][\w$]*)`,
		"class", `^\s*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)`,
		"interface", `^\s*(?:export\s+)?(?:interface|type|enum)\s+([A-Za-z_$][\w$]*)`,
		"function", `^\s*(?:export\s+)?(?:const|let)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[\w$]+)\s*=>`)
}

func cPats() []pattern {
	return pats(
		"struct", `^\s*(?:typedef\s+)?(?:struct|union|enum|class)\s+([A-Za-z_]\w*)`,
		"macro", `^\s*#\s*define\s+([A-Za-z_]\w*)`,
		"function", `^[A-Za-z_][\w \t\*&:<>,]*[\s\*&]([A-Za-z_]\w*)\s*\([^;]*$`)
}

func fallback(abs string) ([]Symbol, error) {
	pp := fallbackByExt[strings.ToLower(filepath.Ext(abs))]
	if pp == nil {
		return []Symbol{}, nil
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Symbol
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		for _, p := range pp {
			if m := p.re.FindStringSubmatch(line); m != nil {
				out = append(out, Symbol{Name: m[1], Kind: p.kind, Line: n})
				break
			}
		}
	}
	return out, nil
}

// approxBodyLines is the guessed length of the last symbol in a file when no end is known.
const approxBodyLines = 80

func depth(s Symbol) int {
	if s.Scope == "" {
		return 0
	}
	return 1 + strings.Count(s.Scope, ".") + strings.Count(s.Scope, "::") + strings.Count(s.Scope, "/")
}

// fillEnds sets End for symbols the extractor gave no end for (older ctags parsers, regex fallback):
// a symbol ends just before the next symbol that is not nested deeper than itself.
func fillEnds(syms []Symbol) {
	for i := range syms {
		if syms[i].End >= syms[i].Line && syms[i].End != 0 {
			continue
		}
		syms[i].End = syms[i].Line + approxBodyLines
		d := depth(syms[i])
		for j := i + 1; j < len(syms); j++ {
			if syms[j].Line > syms[i].Line && depth(syms[j]) <= d {
				syms[i].End = syms[j].Line - 1
				break
			}
		}
		if syms[i].End < syms[i].Line {
			syms[i].End = syms[i].Line
		}
	}
}
