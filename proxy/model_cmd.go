package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	pmdb "aipmc/db"
)

// ModelCommand represents a parsed &aipmc-model command.
type ModelCommand struct {
	Subcommand string // switch | auto | current | list
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

	result := executeModelCommand(cmd, agent)

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
	case "auto", "current", "list":
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

// executeModelCommand runs the command and returns a user-facing result string.
func executeModelCommand(cmd *ModelCommand, agent string) string {
	switch cmd.Subcommand {
	case "switch":
		reg := pmdb.LoadModelRegistry()
		if reg.FindModel(cmd.ModelID) == nil {
			return fmt.Sprintf("✗ Unknown model: %s", cmd.ModelID)
		}
		if err := saveCurrentModel(agent, cmd.ModelID); err != nil {
			return fmt.Sprintf("✗ Failed to switch: %v", err)
		}
		provider := pmdb.CurrentModelProvider(agent)
		return fmt.Sprintf("✓ %s → %s (%s)", agent, cmd.ModelID, provider)

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
		provider := pmdb.CurrentModelProvider(agent)
		return fmt.Sprintf("%s: %s (%s)", agent, cm, provider)

	case "list":
		reg := pmdb.LoadModelRegistry()
		if len(reg.Models) == 0 {
			return "No models configured.\nUse aipmc models add to configure."
		}
		var lines []string
		for _, vm := range reg.Models {
			line := fmt.Sprintf("  %s (%s)", vm.ID, vm.Provider)
			if vm.DisplayName != "" {
				line = fmt.Sprintf("  %s — %s (%s)", vm.ID, vm.DisplayName, vm.Provider)
			}
			lines = append(lines, line)
		}
		return fmt.Sprintf("Available models:\n%s", strings.Join(lines, "\n"))
	}
	return "✗ Unknown command"
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
				"index":   0,
				"message": map[string]any{"role": "assistant", "content": result},
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
	var peek struct{ Stream bool `json:"stream"` }
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
