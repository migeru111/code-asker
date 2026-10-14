// Package ai defines the Provider abstraction used for code questions.
// Everything that talks to an LLM lives behind this interface so the backend
// can be swapped (Claude CLI today; Gemini API, Codex, ... later).
package ai

import "context"

// Location is what the user is looking at.
type Location struct {
	Repo       string
	Path       string
	StartLine  int // first visible line (1-based)
	EndLine    int // last visible line
	TotalLines int
	Search     bool // the user described the place in words; the AI must find it itself
}

// Request is one question. Snippet is pre-built, line-numbered code around the location.
type Request struct {
	RepoDir  string // absolute repository root; the provider must confine tool access to it
	Location Location
	Snippet  string
	Question string
	Session  string // provider session id to continue a conversation, "" for a new one
}

type EventType string

const (
	EventText  EventType = "text"  // incremental answer text
	EventTool  EventType = "tool"  // the AI is using a read-only tool (Text = short description)
	EventDone  EventType = "done"  // finished; Session holds the id to continue with
	EventError EventType = "error" // failed; Text holds a message
)

type Event struct {
	Type    EventType
	Text    string
	Session string
}

// Provider streams an answer. The channel is closed after a Done or Error event.
// Cancelling ctx must stop all work started for the request.
type Provider interface {
	Name() string
	Ask(ctx context.Context, req Request) (<-chan Event, error)
}
