package ai

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBuildArgsRestrictsTools(t *testing.T) {
	c := &ClaudeCLI{Model: "sonnet"}
	args, err := c.BuildArgs(Request{Question: "q", Location: Location{Repo: "r", Path: "p"}})
	if err != nil {
		t.Fatal(err)
	}
	has := func(flag, val string) bool {
		i := slices.Index(args, flag)
		return i >= 0 && i+1 < len(args) && args[i+1] == val
	}
	for _, kv := range [][2]string{{"--tools", "Read,Grep,Glob"}, {"--permission-mode", "dontAsk"}, {"--setting-sources", ""}, {"--model", "sonnet"}} {
		if !has(kv[0], kv[1]) {
			t.Errorf("missing %s %q in %q", kv[0], kv[1], args)
		}
	}
	for _, bad := range []string{"--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "--add-dir"} {
		if slices.Contains(args, bad) {
			t.Errorf("must not pass %s", bad)
		}
	}
	if _, err := c.BuildArgs(Request{Question: "q", Session: "--evil"}); err == nil {
		t.Error("non-UUID session must be rejected")
	}
	args, err = c.BuildArgs(Request{Question: "q", Session: "5afe7aa3-64ae-5367-b822-102fcae54ec9"})
	if err != nil || !has("--resume", "5afe7aa3-64ae-5367-b822-102fcae54ec9") {
		t.Errorf("resume: %v %q", err, args)
	}
}

func TestChildEnvStripsAPIKeys(t *testing.T) {
	got := childEnv([]string{"PATH=/bin", "ANTHROPIC_API_KEY=x", "HOME=/h", "ANTHROPIC_AUTH_TOKEN=y"})
	if strings.Join(got, ",") != "PATH=/bin,HOME=/h" {
		t.Errorf("%v", got)
	}
}

func TestPrompt(t *testing.T) {
	p := BuildPrompt(Request{Question: "何してる？", Snippet: Snippet(118, []string{"a", "b"}, 1000),
		Location: Location{Repo: "demo", Path: "src/x.rs", StartLine: 120, EndLine: 168, TotalLines: 300}})
	for _, want := range []string{"demo", "src/x.rs", "120-168", "118| a", "119| b", "質問: 何してる？"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if s := Snippet(1, []string{strings.Repeat("x", 50), "y"}, 30); !strings.Contains(s, "省略") {
		t.Errorf("snippet not capped: %q", s)
	}
}

func fakeClaude(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func collect(t *testing.T, ch <-chan Event) []Event {
	var evs []Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return evs
			}
			evs = append(evs, e)
		case <-timeout:
			t.Fatal("timeout")
		}
	}
}

func TestAskStreams(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "must-not-leak")
	dir := t.TempDir()
	bin := fakeClaude(t, `
echo '{"type":"system","subtype":"init"}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"cwd="}}}'
echo "{\"type\":\"stream_event\",\"event\":{\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"$(pwd) key=${ANTHROPIC_API_KEY:-none}\"}}}"
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"foo"}}]}}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"dup"}]}}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"x","session_id":"5afe7aa3-64ae-5367-b822-102fcae54ec9"}'
`)
	c := &ClaudeCLI{Bin: bin}
	ch, err := c.Ask(context.Background(), Request{RepoDir: dir, Question: "q"})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	var text string
	var tool, done bool
	for _, e := range evs {
		switch e.Type {
		case EventText:
			text += e.Text
		case EventTool:
			tool = e.Text == "Grep foo"
		case EventDone:
			done = e.Session == "5afe7aa3-64ae-5367-b822-102fcae54ec9"
		}
	}
	real, _ := filepath.EvalSymlinks(dir)
	if !strings.Contains(text, "cwd=") || !strings.Contains(text, real) || !strings.Contains(text, "key=none") || strings.Contains(text, "dup") {
		t.Errorf("text=%q (want cwd %s, no key, no dup)", text, real)
	}
	if !tool || !done {
		t.Errorf("tool=%v done=%v events=%+v", tool, done, evs)
	}
}

func TestAskErrorAndTimeout(t *testing.T) {
	c := &ClaudeCLI{Bin: fakeClaude(t, "echo 'not logged in' >&2; exit 1\n")}
	ch, _ := c.Ask(context.Background(), Request{RepoDir: t.TempDir(), Question: "q"})
	evs := collect(t, ch)
	if len(evs) != 1 || evs[0].Type != EventError || !strings.Contains(evs[0].Text, "not logged in") {
		t.Errorf("%+v", evs)
	}

	c = &ClaudeCLI{Bin: fakeClaude(t, "sleep 30\n"), Timeout: 300 * time.Millisecond}
	ch, _ = c.Ask(context.Background(), Request{RepoDir: t.TempDir(), Question: "q"})
	evs = collect(t, ch)
	if len(evs) != 1 || evs[0].Type != EventError || !strings.Contains(evs[0].Text, "タイムアウト") {
		t.Errorf("timeout: %+v", evs)
	}
}

func TestAskCancelKills(t *testing.T) {
	c := &ClaudeCLI{Bin: fakeClaude(t, "sleep 30\n")}
	ctx, cancel := context.WithCancel(context.Background())
	ch, _ := c.Ask(ctx, Request{RepoDir: t.TempDir(), Question: "q"})
	cancel()
	evs := collect(t, ch) // must return promptly and close the channel
	for _, e := range evs {
		if e.Type == EventDone {
			t.Errorf("unexpected done")
		}
	}
}
