package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	pmdb "aipmc/db"
	"aipmc/store"
	"aipmc/u"
)

// ModelCommand represents a parsed &aipmc-model command.
type ModelCommand struct {
	Subcommand string // switch | auto | current | list | sessions
	ModelID    string // model ID for "switch"
}

// tryModelCommand attempts to intercept a &aipmc-model command from the request body.
// Returns true if the request was a model command and has been fully handled.
func tryModelCommand(w http.ResponseWriter, r *http.Request, agent string, body []byte, path string) bool {
	if !bytes.Contains(body, []byte("&aipmc-model")) {
		return false
	}

	text := getLastUserText(body, agent)
	text = strings.TrimSpace(text)
	// Claude may wrap the user message in <session> or <system-reminder> tags;
	// search for the command substring instead of requiring it at position 0.
	if idx := strings.Index(text, "&aipmc-model"); idx >= 0 {
		text = text[idx:]
	} else {
		return false
	}

	cmd := parseModelCommand(text)
	if cmd == nil {
		return false
	}

	// Pass the caller's own session ID (codex provides it via client_metadata;
	// other agents may not) so the sessions board can mark "current session".
	result := executeModelCommand(cmd, agent, extractSessionID(body, r.Header))

	capID := startCapture(agent, r.Method, r.URL.Path, "", body, copyHeaders(r), nil)
	finishCapture(capID, http.StatusOK, time.Duration(0), nil, result, "")

	handleModelCommandResponse(w, agent, path, body, result)
	return true
}

// getLastUserText extracts the text content of the last user message from a request body.
func getLastUserText(body []byte, agent string) string {
	adapter := adapterForAgent(agent)
	if adapter != nil {
		r, _ := http.NewRequest("POST", "", bytes.NewReader(body))
		req, err := adapter.ParseRequest(r)
		if err != nil {
			return ""
		}
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				return req.Messages[i].Content
			}
		}
		return ""
	}

	// OpenCode / Cursor: parse as OpenAI Chat Completions
	var raw struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return ""
	}
	for i := len(raw.Messages) - 1; i >= 0; i-- {
		if raw.Messages[i].Role == "user" {
			return contentToString(raw.Messages[i].Content)
		}
	}
	return ""
}

func contentToString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		for _, part := range v {
			if m, ok := part.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					return t
				}
			}
		}
	}
	return ""
}

// parseModelCommand parses text like "&aipmc-model switch glm4.7".
func parseModelCommand(text string) *ModelCommand {
	text = strings.TrimSpace(text)
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return nil
	}
	sub := parts[1]
	switch sub {
	case "auto", "current", "list", "sessions":
		return &ModelCommand{Subcommand: sub}
	case "switch":
		if len(parts) >= 3 {
			return &ModelCommand{Subcommand: "switch", ModelID: parts[2]}
		}
		return nil
	default:
		return nil
	}
}

// modelProvidersDisplay returns a display string for a model's provider routes.
func modelProvidersDisplay(vm *pmdb.VirtualModel) string {
	providers := pmdb.LoadModelRegistry().ListModelProviders(vm.ID)
	if len(providers) == 0 {
		return ""
	}
	return strings.Join(providers, ", ")
}

// executeModelCommand runs the command and returns a user-facing result string.
func executeModelCommand(cmd *ModelCommand, agent, sessionID string) string {
	switch cmd.Subcommand {
	case "switch":
		reg := pmdb.LoadModelRegistry()
		vm := reg.FindModel(cmd.ModelID)
		if vm == nil {
			return fmt.Sprintf("✗ Unknown model: %s", cmd.ModelID)
		}
		if err := saveCurrentModel(agent, cmd.ModelID); err != nil {
			return fmt.Sprintf("✗ Failed to switch: %v", err)
		}
		providers := modelProvidersDisplay(vm)
		return fmt.Sprintf("✓ %s → %s (%s)", agent, cmd.ModelID, providers)

	case "auto":
		if err := saveCurrentModel(agent, ""); err != nil {
			return fmt.Sprintf("✗ Failed to reset: %v", err)
		}
		return fmt.Sprintf("✓ %s → Auto (passthrough)", agent)

	case "current":
		cm := loadCurrentModel(agent)
		if cm == "" {
			return fmt.Sprintf("%s: Auto (passthrough)", agent)
		}
		reg := pmdb.LoadModelRegistry()
		vm := reg.FindModel(cm)
		providers := ""
		if vm != nil {
			providers = " (" + modelProvidersDisplay(vm) + ")"
		}
		return fmt.Sprintf("%s: %s%s", agent, cm, providers)

	case "list":
		reg := pmdb.LoadModelRegistry()
		if len(reg.Models) == 0 {
			return "No models configured.\nUse aipmc models add to configure."
		}
		var lines []string
		for _, vm := range reg.Models {
			providers := modelProvidersDisplay(&vm)
			line := fmt.Sprintf("  %s (%s)", vm.ID, providers)
			if vm.DisplayName != "" {
				line = fmt.Sprintf("  %s — %s (%s)", vm.ID, vm.DisplayName, providers)
			}
			lines = append(lines, line)
		}
		return fmt.Sprintf("Available models:\n%s", strings.Join(lines, "\n"))

	case "sessions":
		return executeSessionsCommand(agent, sessionID, "")
	}
	return "✗ Unknown command"
}

// executeSessionsCommand renders the active-agent board for the given project
// ("" = proxy's CWD), marking the caller's own session. It gives the user
// enough distinguishing info (source, short id, what each agent is doing,
// activity window) to point at a specific session.
func executeSessionsCommand(agent, sessionID, projectPath string) string {
	since := time.Now().Add(-24 * time.Hour).Format("2006-01-02T15:04:05")
	rows, err := store.ListActiveSessions(projectPath, "", since, 10)
	if err != nil {
		// Standalone "aipmc proxy" mode does not chdir to a project dir; if the
		// proxy was started outside a project, session lookups would hit the
		// wrong/no DB. Surface that instead of returning misleading data.
		return fmt.Sprintf("✗ 无法读取会话状态板: %v\n   （proxy 需在 aipmc 项目目录下启动，否则 session 库定位会失效）", err)
	}
	if len(rows) == 0 {
		return "No active agent sessions in the last 24h."
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("活跃 Agent 会话（最近 24h · %d 个）:", len(rows)))
	for i, s := range rows {
		marker := ""
		if sessionID != "" && s.SessionID == sessionID {
			marker = "  ← 当前会话（你）"
		} else if sessionID == "" && s.Source == agent {
			marker = "  ← 当前会话（你）"
		}
		lines = append(lines, fmt.Sprintf("[%d] %s %s%s", i+1, s.Source, u.Prefix(s.SessionID, 13), marker))

		status := s.Status
		statusKind := ""
		if status == "" {
			if len(s.UserPrompts) > 0 {
				status = s.UserPrompts[0]
				statusKind = "自动登记"
			} else {
				status = "—"
			}
		} else if s.Explicit {
			statusKind = "显式声明"
		} else {
			statusKind = "自动登记"
		}
		if statusKind != "" {
			status = fmt.Sprintf("%s（%s）", status, statusKind)
		}
		lines = append(lines, "    正在: "+u.TruncateStr(status, 60))

		window := fmt.Sprintf("%s ~ %s", shortClock(s.FirstSeen), shortClock(s.LastSeen))
		lines = append(lines, fmt.Sprintf("    活跃: %s · user %d · tool %d", window, s.UserPromptCount, s.ToolCallCount))

		if len(s.UserPrompts) > 1 {
			lines = append(lines, "    最近: "+u.TruncateStr(s.UserPrompts[1], 60))
		}
	}
	lines = append(lines, "提示: 说「看 [N]」或短 id（如 019fff14-1437）即可指认某个会话")
	return strings.Join(lines, "\n")
}

// shortClock renders a stored ISO timestamp as HH:MM for the activity window.
// created_at is stored as "2006-01-02T15:04:05" (no zone); tolerate RFC3339 too.
func shortClock(ts string) string {
	for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, ts); err == nil {
			return t.Format("15:04")
		}
	}
	return ts
}

// handleModelCommandResponse sends the command result in the agent's native protocol.
func handleModelCommandResponse(w http.ResponseWriter, agent string, path string, body []byte, result string) {
	streaming := isStreaming(path, body)
	adapter := adapterForAgent(agent)
	model := loadCurrentModel(agent)
	if model == "" {
		model = "auto"
	}

	if streaming {
		handleModelCommandStream(w, adapter, model, result)
	} else {
		handleModelCommandNonStream(w, adapter, model, result)
	}
}

func handleModelCommandNonStream(w http.ResponseWriter, adapter ProtocolAdapter, model, result string) {
	if adapter != nil {
		stop := "stop"
		openaiResp := &OpenAIResponse{
			ID:     "model_cmd",
			Object: "chat.completion",
			Choices: []OpenAIChoice{{
				Index:        0,
				Message:      &OpenAIMessage{Role: "assistant", Content: result},
				FinishReason: &stop,
			}},
			Usage: &OpenAIUsage{TotalTokens: 0},
		}
		writeJSON(w, http.StatusOK, adapter.ConvertResponse(openaiResp, model))
	} else {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "model_cmd",
			"object": "chat.completion",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": result},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{
				"prompt_tokens":     0,
				"completion_tokens": 0,
				"total_tokens":      0,
			},
		})
	}
}

func handleModelCommandStream(w http.ResponseWriter, adapter ProtocolAdapter, model, result string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	if adapter != nil {
		emitter := adapter.NewEmitter(w, model)
		emitter.Emit(UnifiedStreamEvent{Type: StreamText, Delta: result})
		emitter.Done("stop", &UnifiedUsage{})
	} else {
		escaped, _ := json.Marshal(result)
		fmt.Fprintf(w, "data: {\"id\":\"model_cmd\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%s}}]}\n\n", escaped)
		fmt.Fprintf(w, "data: {\"id\":\"model_cmd\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// isStreaming detects whether the request expects a streaming (SSE) response.
func isStreaming(path string, body []byte) bool {
	if strings.Contains(path, "streamGenerateContent") {
		return true
	}
	var peek struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &peek) == nil && peek.Stream
}

// adapterForAgent maps an agent string to its ProtocolAdapter.
func adapterForAgent(agent string) ProtocolAdapter {
	switch agent {
	case "claude":
		return &ClaudeAdapter{}
	case "codex":
		return &CodexAdapter{}
	case "gemini":
		return &GeminiAdapter{}
	default:
		return nil
	}
}
