// Package search runs ripgrep over a repository.
package search

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

const (
	MaxMatches    = 200
	MaxQueryLen   = 500
	perFileMax    = "30"
	maxFilesize   = "2M"
	maxColumns    = "300"
	searchTimeout = 10 * time.Second
)

var ErrBadQuery = errors.New("bad query")

type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type Result struct {
	Matches   []Match `json:"matches"`
	Truncated bool    `json:"truncated,omitempty"`
}

type Options struct {
	Regex bool
}

// Run searches repoDir (absolute, already validated) below the relative sub-directory sub.
// The query is passed as an argument via -e; no shell is involved.
func Run(ctx context.Context, repoDir, sub, query string, opt Options) (*Result, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > MaxQueryLen || strings.ContainsRune(query, 0) {
		return nil, ErrBadQuery
	}
	if sub == "" {
		sub = "."
	}
	args := []string{"--json", "--no-config", "--smart-case", "--max-columns", maxColumns, "--max-columns-preview",
		"--max-count", perFileMax, "--max-filesize", maxFilesize}
	if !opt.Regex {
		args = append(args, "--fixed-strings")
	}
	args = append(args, "-e", query, "--", sub)

	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = repoDir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	res := &Result{Matches: []Match{}}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var ev struct {
			Type string `json:"type"`
			Data struct {
				Path       struct{ Text string } `json:"path"`
				Lines      struct{ Text string } `json:"lines"`
				LineNumber int                   `json:"line_number"`
			} `json:"data"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Type != "match" || ev.Data.Path.Text == "" {
			continue
		}
		if len(res.Matches) >= MaxMatches {
			res.Truncated = true
			cancel() // enough; stop ripgrep
			break
		}
		res.Matches = append(res.Matches, Match{
			Path: strings.TrimPrefix(ev.Data.Path.Text, "./"),
			Line: ev.Data.LineNumber,
			Text: strings.TrimRight(ev.Data.Lines.Text, "\r\n"),
		})
	}
	werr := cmd.Wait()
	if res.Truncated || ctx.Err() != nil {
		return res, nil
	}
	var ee *exec.ExitError
	if errors.As(werr, &ee) && ee.ExitCode() == 2 && len(res.Matches) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		return nil, errors.Join(ErrBadQuery, errors.New(msg))
	}
	return res, nil
}
