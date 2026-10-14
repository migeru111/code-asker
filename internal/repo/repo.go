// Package repo provides read-only access to git repositories under a root.
package repo

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/migeru111/code-asker/internal/safepath"
)

const (
	MaxFileBytes  = 20 << 20 // files larger than this are not displayed
	MaxLineBytes  = 4000     // longer lines are truncated for display
	DefaultLimit  = 500
	MaxLimit      = 2000
	binarySniffSz = 8000
)

var (
	ErrNotFound = errors.New("not found")
	ErrBadName  = errors.New("invalid repository name")
)

// Store reads repositories below a safepath.Root.
type Store struct {
	root *safepath.Root
}

func NewStore(root *safepath.Root) *Store { return &Store{root: root} }

type Repo struct {
	Name string `json:"name"`
}

type Entry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "dir" or "file"
	Size int64  `json:"size,omitempty"`
}

type File struct {
	Repo       string   `json:"repo"`
	Path       string   `json:"path"`
	Size       int64    `json:"size"`
	Binary     bool     `json:"binary,omitempty"`
	TooLarge   bool     `json:"too_large,omitempty"`
	TotalLines int      `json:"total_lines"`
	Start      int      `json:"start"`
	End        int      `json:"end"`
	Lines      []string `json:"lines"`
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00") && !strings.HasPrefix(name, ".")
}

// Repos lists the directories directly under the root that contain a .git entry.
func (s *Store) Repos() ([]Repo, error) {
	ents, err := os.ReadDir(s.root.Dir())
	if err != nil {
		return nil, err
	}
	out := []Repo{}
	for _, e := range ents {
		if !validName(e.Name()) {
			continue
		}
		dir, err := s.root.Resolve(e.Name())
		if err != nil {
			continue
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			continue
		}
		out = append(out, Repo{Name: e.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (s *Store) resolve(repo, rel string) (string, error) {
	if !validName(repo) {
		return "", ErrBadName
	}
	p := repo
	if rel != "" {
		p += "/" + strings.Trim(rel, "/")
	}
	full, err := s.root.Resolve(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	return full, nil
}

// Path resolves a file or directory inside a repository to its canonical absolute path.
func (s *Store) Path(repo, rel string) (string, error) { return s.resolve(repo, rel) }

// List returns the entries of a directory inside a repository.
func (s *Store) List(repo, rel string) ([]Entry, error) {
	dir, err := s.resolve(repo, rel)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	for _, e := range ents {
		if e.Name() == ".git" {
			continue
		}
		// Resolve through symlinks; skip anything pointing outside the root.
		real, err := s.root.Resolve(joinRel(repo, rel, e.Name()))
		if err != nil {
			continue
		}
		st, err := os.Stat(real)
		if err != nil {
			continue
		}
		if st.IsDir() {
			out = append(out, Entry{Name: e.Name(), Type: "dir"})
		} else if st.Mode().IsRegular() {
			out = append(out, Entry{Name: e.Name(), Type: "file", Size: st.Size()})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type == "dir"
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func joinRel(repo, rel, name string) string {
	p := repo
	if r := strings.Trim(rel, "/"); r != "" {
		p += "/" + r
	}
	return p + "/" + name
}

// Read returns lines [start, start+limit) of a file (1-based start) and the total line count.
func (s *Store) Read(repo, rel string, start, limit int) (*File, error) {
	full, err := s.resolve(repo, rel)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, ErrNotFound
	}
	if start < 1 {
		start = 1
	}
	if limit < 1 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	f := &File{Repo: repo, Path: strings.Trim(rel, "/"), Size: st.Size(), Start: start, Lines: []string{}}
	if st.Size() > MaxFileBytes {
		f.TooLarge = true
		return f, nil
	}
	fh, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	br := bufio.NewReaderSize(fh, 64<<10)
	if head, _ := br.Peek(binarySniffSz); containsNUL(head) {
		f.Binary = true
		return f, nil
	}
	n := 0
	for {
		line, err := readLine(br)
		if err != nil && line == nil {
			break
		}
		n++
		if n >= start && n < start+limit {
			f.Lines = append(f.Lines, string(line))
		}
		if err != nil {
			break
		}
	}
	f.TotalLines = n
	f.End = start + len(f.Lines) - 1
	if len(f.Lines) == 0 {
		f.End = start - 1
	}
	return f, nil
}

func containsNUL(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

// readLine reads one line without its terminator, truncating very long lines.
// It returns a nil slice with io.EOF when no more data is available.
func readLine(br *bufio.Reader) ([]byte, error) {
	var buf []byte
	truncated := false
	got := false
	for {
		chunk, err := br.ReadSlice('\n')
		if len(chunk) > 0 {
			got = true
			if len(buf) < MaxLineBytes {
				buf = append(buf, chunk...)
			} else {
				truncated = true
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			if !got {
				return nil, io.EOF
			}
			return finishLine(buf, truncated), io.EOF
		}
		return finishLine(buf, truncated), nil
	}
}

func finishLine(buf []byte, truncated bool) []byte {
	buf = []byte(strings.TrimRight(string(buf), "\r\n"))
	if len(buf) > MaxLineBytes {
		buf = buf[:MaxLineBytes]
		truncated = true
	}
	if !utf8.Valid(buf) {
		buf = []byte(strings.ToValidUTF8(string(buf), "�"))
	}
	if truncated {
		buf = append(buf, []byte(" …[truncated]")...)
	}
	if buf == nil {
		buf = []byte{}
	}
	return buf
}
