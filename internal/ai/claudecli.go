package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ClaudeCLI answers questions by running `claude -p` (non-interactive mode).
// It relies on the Claude Code login of the user the service runs as (subscription);
// API keys are deliberately stripped from the child environment.
type ClaudeCLI struct {
	Bin     string        // default "claude"
	Model   string        // optional --model
	Timeout time.Duration // per-question limit
}

func (c *ClaudeCLI) Name() string { return "claude-cli" }

// BuildArgs returns the argument vector (no shell involved).
//
//	--tools Read,Grep,Glob      only read-only built-in tools exist (no Bash/Write/Edit/Web)
//	--permission-mode dontAsk   anything not pre-approved is denied instead of prompting;
//	                            reads outside the working directory are denied
//	--setting-sources ""        ignore user/project/local settings (a viewed repo could ship
//	                            its own permission rules)
//	--strict-mcp-config         no MCP servers; --disable-slash-commands: no skills
func (c *ClaudeCLI) BuildArgs(r Request) ([]string, error) {
	args := []string{
		"-p", BuildPrompt(r),
		"--output-format", "stream-json", "--include-partial-messages", "--verbose",
		"--tools", "Read,Grep,Glob",
		"--permission-mode", "dontAsk",
		"--setting-sources", "",
		"--strict-mcp-config", "--disable-slash-commands",
		"--append-system-prompt", SystemPrompt,
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	if r.Session != "" {
		if !uuidRe.MatchString(r.Session) {
			return nil, errors.New("invalid session id")
		}
		args = append(args, "--resume", r.Session)
	}
	return args, nil
}

// childEnv drops credentials that would switch claude from the subscription to API billing.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY":
			continue
		}
		out = append(out, kv)
	}
	return out
}

func (c *ClaudeCLI) Ask(ctx context.Context, r Request) (<-chan Event, error) {
	args, err := c.BuildArgs(r)
	if err != nil {
		return nil, err
	}
	bin := c.Bin
	if bin == "" {
		bin = "claude"
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = r.RepoDir
	cmd.Env = childEnv(os.Environ())
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) } // kill node children too
	cmd.WaitDelay = 3 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("cannot start %q: %w", bin, err)
	}

	out := make(chan Event, 16)
	go func() {
		defer close(out)
		defer cancel()
		send := func(e Event) bool {
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}
		finished, sawError := parseStream(stdout, send)
		werr := cmd.Wait()
		switch {
		case finished || sawError:
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			out <- Event{Type: EventError, Text: "タイムアウトしました"}
		case ctx.Err() != nil:
			// client went away; nobody is listening
		default:
			msg := strings.TrimSpace(stderr.String())
			if msg == "" && werr != nil {
				msg = werr.Error()
			}
			if msg == "" {
				msg = "claude が結果を返さずに終了しました"
			}
			out <- Event{Type: EventError, Text: msg}
		}
	}()
	return out, nil
}

// parseStream converts claude's stream-json lines into Events.
func parseStream(r io.Reader, send func(Event) bool) (finished, sawError bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 256<<10), 16<<20)
	streamedText := false
	for sc.Scan() {
		var e struct {
			Type  string `json:"type"`
			Event struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			} `json:"event"`
			Message struct {
				Content []struct {
					Type  string         `json:"type"`
					Name  string         `json:"name"`
					Text  string         `json:"text"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			} `json:"message"`
			Subtype   string `json:"subtype"`
			IsError   bool   `json:"is_error"`
			Result    string `json:"result"`
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e.Type {
		case "stream_event":
			if e.Event.Type == "content_block_delta" && e.Event.Delta.Type == "text_delta" && e.Event.Delta.Text != "" {
				streamedText = true
				if !send(Event{Type: EventText, Text: e.Event.Delta.Text}) {
					return
				}
			}
		case "assistant":
			for _, c := range e.Message.Content {
				switch c.Type {
				case "tool_use":
					if !send(Event{Type: EventTool, Text: describeTool(c.Name, c.Input)}) {
						return
					}
				case "text":
					// Only used when no partial (streamed) text was seen at all.
					if !streamedText && c.Text != "" {
						if !send(Event{Type: EventText, Text: c.Text}) {
							return
						}
					}
				}
			}
		case "result":
			if e.IsError || strings.HasPrefix(e.Subtype, "error") {
				msg := e.Result
				if msg == "" {
					msg = "AI がエラーを返しました (" + e.Subtype + ")"
				}
				send(Event{Type: EventError, Text: msg})
				return false, true
			}
			send(Event{Type: EventDone, Session: e.SessionID})
			return true, false
		}
	}
	return
}

func describeTool(name string, in map[string]any) string {
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := in[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	arg := pick("file_path", "pattern", "path")
	if len(arg) > 80 {
		arg = arg[:80] + "…"
	}
	if arg == "" {
		return name
	}
	return name + " " + arg
}

// limitedBuffer keeps the first 4 KiB written to it.
type limitedBuffer struct{ b []byte }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := 4096 - len(l.b); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		l.b = append(l.b, p[:room]...)
	}
	return len(p), nil
}

func (l *limitedBuffer) String() string { return string(l.b) }
