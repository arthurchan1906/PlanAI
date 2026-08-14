package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aipmc/ai"
	pmdb "aipmc/db"
	"aipmc/store"
)

// ChatService runs the coding agent for web and CLI entry points.
type ChatService struct {
	LLM     *ai.Client
	WorkDir string
	Source  string
}

// NewChatService creates a chat service rooted at workDir.
func NewChatService(llm *ai.Client, workDir, source string) *ChatService {
	return &ChatService{LLM: llm, WorkDir: workDir, Source: source}
}

// ProjectWorkDir returns the repository root (parent of .pmai/).
func ProjectWorkDir() string {
	runtimeDir, err := pmdb.RuntimeDir()
	if err != nil || runtimeDir == "" {
		return "."
	}
	return filepath.Dir(runtimeDir)
}

// SendResult is the outcome of one chat turn.
type SendResult struct {
	SessionID string
	Response  string
	Events    []Event
	Traces    []TraceTurn
}

// Send runs one user message against an existing or new session.
func (c *ChatService) Send(sessionID, message string) (*SendResult, error) {
	return c.send(sessionID, message, nil)
}

// StreamEmit sends one SSE event: event name + JSON payload.
type StreamEmit func(event string, data map[string]any)

// SendStream runs the agent with streaming callbacks for web SSE.
func (c *ChatService) SendStream(sessionID, message string, emit StreamEmit) (*SendResult, error) {
	var cb *StreamCallbacks
	if emit != nil {
		cb = &StreamCallbacks{
			OnToken: func(token string) {
				emit("token", map[string]any{"content": token})
			},
			OnToolStart: func(id, name string, args map[string]any) {
				emit("tool_start", map[string]any{"id": id, "name": name, "args": args})
			},
			OnToolResult: func(id, name, result string) {
				preview := result
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				emit("tool_result", map[string]any{"id": id, "name": name, "result": preview})
			},
		}
	}
	return c.send(sessionID, message, cb)
}

func (c *ChatService) send(sessionID, message string, cb *StreamCallbacks) (*SendResult, error) {
	if c.LLM == nil || !c.LLM.Enabled() {
		return nil, fmt.Errorf("AI 未配置。请设置 AI_ENDPOINT 环境变量。")
	}
	if message == "" {
		return nil, fmt.Errorf("缺少 message")
	}
	workDir := c.WorkDir
	if workDir == "" {
		workDir = ProjectWorkDir()
	}

	a := New(c.LLM, workDir)
	a.Source = c.Source
	a.CaptureTraces = true
	a.OnEvent = func(sessionID, role, source, content, metadataJSON string) {
		store.LogDiscussion(sessionID, role, source, content, metadataJSON)
	}

	sessionDir := SessionDir(workDir)
	var sess *Session
	if sessionID != "" {
		if IsValidSessionID(sessionID) {
			if loaded, err := LoadSession(filepath.Join(sessionDir, sessionID+".json")); err == nil {
				sess = loaded
			}
		}
	}
	if sess == nil {
		sess = NewSession()
	}

	var response string
	var err error
	if cb != nil {
		response, err = a.RunStream(sess, message, cb)
	} else {
		response, err = a.Run(sess, message)
	}
	if err != nil {
		return nil, err
	}
	if err := sess.Save(filepath.Join(sessionDir, sess.ID+".json")); err != nil {
		return nil, err
	}
	return &SendResult{
		SessionID: sess.ID,
		Response:  response,
		Events:    sess.Events,
		Traces:    sess.Traces,
	}, nil
}

// SessionSummary is a lightweight session listing entry.
type SessionSummary struct {
	ID        string `json:"id"`
	UpdatedAt string `json:"updated_at"`
	Events    int    `json:"events"`
}

// ListSessions returns chat sessions for a project.
func ListSessions(workDir string) ([]SessionSummary, error) {
	if workDir == "" {
		workDir = ProjectWorkDir()
	}
	sessionDir := SessionDir(workDir)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionSummary{}, nil
		}
		return nil, err
	}
	var sessions []SessionSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		info, _ := e.Info()
		var modTime string
		var eventCount int
		if info != nil {
			modTime = info.ModTime().Format("2006-01-02 15:04")
		}
		if sess, err := LoadSession(filepath.Join(sessionDir, e.Name())); err == nil {
			eventCount = len(sess.Events)
		}
		sessions = append(sessions, SessionSummary{ID: id, UpdatedAt: modTime, Events: eventCount})
	}
	if sessions == nil {
		sessions = []SessionSummary{}
	}
	return sessions, nil
}

// LoadLatestSession returns the most recently modified session, or nil.
func LoadLatestSession(workDir string) *Session {
	if workDir == "" {
		workDir = ProjectWorkDir()
	}
	dir := SessionDir(workDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var latest string
	var latestTime int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Unix() > latestTime {
			latestTime = info.ModTime().Unix()
			latest = e.Name()
		}
	}
	if latest == "" {
		return nil
	}
	s, err := LoadSession(filepath.Join(dir, latest))
	if err != nil {
		return nil
	}
	return s
}
