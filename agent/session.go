package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Event represents a single turn in the agent conversation.
// Maps to OpenAI's message roles: user, assistant, tool.
type Event struct {
	Role    string `json:"role"` // "user" | "assistant" | "tool"

	// Text content — used by user messages and assistant text responses.
	Content string `json:"content,omitempty"`

	// Tool calls — set when Role="assistant" and the LLM chooses to invoke tools.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// Tool result fields — set when Role="tool".
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolResult string `json:"tool_result,omitempty"`

	Timestamp string `json:"timestamp"`
}

// ToolCall represents a single tool invocation chosen by the LLM.
type ToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// TraceTurn records the raw LLM request and response for one iteration.
type TraceTurn struct {
	Turn     int    `json:"turn"`
	Request  string `json:"request"`  // JSON: [{role, content, tool_calls?...}, ...]
	Response string `json:"response"` // JSON: {content, tool_calls:[...]}
}

// Session holds the full conversation history for an agent session.
type Session struct {
	ID        string      `json:"id"`
	Events    []Event     `json:"events"`
	Traces    []TraceTurn `json:"traces,omitempty"`
	CreatedAt string      `json:"created_at"`
	UpdatedAt string      `json:"updated_at"`
}

// AddTrace appends a trace turn to the session.
func (s *Session) AddTrace(t TraceTurn) {
	s.Traces = append(s.Traces, t)
}

// NewSession creates a session with a unique ID.
func NewSession() *Session {
	now := time.Now()
	return &Session{
		ID:        "s-" + now.Format("20060102-150405") + "-" + randHex(3),
		Events:    nil,
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
	}
}

// Append adds an event to the session and bumps UpdatedAt.
func (s *Session) Append(e Event) {
	e.Timestamp = time.Now().Format(time.RFC3339)
	s.Events = append(s.Events, e)
	s.UpdatedAt = time.Now().Format(time.RFC3339)
}

// Save persists the session as JSON to the given path.
func (s *Session) Save(filePath string) error {
	s.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

// LoadSession reads a session from a JSON file.
func LoadSession(filePath string) (*Session, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("解析会话文件失败: %w", err)
	}
	return &s, nil
}

// SessionDir returns the directory where sessions are stored.
func SessionDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".pmai", "agent", "sessions")
}

// sessionIDRe matches generated session IDs (s-YYYYMMDD-HHMMSS-hex).
// The strict charset keeps IDs safe to embed in file paths: no separators,
// no "." or ".." components, no drive/UNC prefixes.
var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// IsValidSessionID reports whether id can be safely used as a session file
// name. Callers must check before building paths with a user-supplied ID.
func IsValidSessionID(id string) bool {
	return id != "" && sessionIDRe.MatchString(id)
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
