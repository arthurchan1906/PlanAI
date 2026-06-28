package proxy

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// =============================================================================
// ClaudeAdapter — translates Anthropic Messages protocol ↔ UnifiedReq
// =============================================================================

// ClaudeAdapter implements ProtocolAdapter for Claude Code (Anthropic Messages API).
// Types (AnthropicRequest, AnthropicResponse, etc.) are defined in anthropic.go.
type ClaudeAdapter struct{}

func (a *ClaudeAdapter) ParseRequest(r *http.Request) (*UnifiedReq, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	var req AnthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("[CLAUDE] ERROR parsing request: %v", err)
		return nil, err
	}

	model := req.Model
	log.Printf("[CLAUDE] → messages  model=%s stream=%v", model, req.Stream)

	return a.toUnified(&req), nil
}

func (a *ClaudeAdapter) toUnified(req *AnthropicRequest) *UnifiedReq {
	chat := &UnifiedReq{
		Model:    effectiveModel(req.Model),
		Stream:   req.Stream,
		MaxTokens: &req.MaxTokens,
	}

	var messages []UnifiedMsg

	// System instruction (string or array of content blocks)
	switch s := req.System.(type) {
	case string:
		if strings.TrimSpace(s) != "" {
			messages = append(messages, UnifiedMsg{Role: "system", Content: s})
		}
	case []any:
		for _, block := range s {
			if m, ok := block.(map[string]any); ok {
				if text, ok := m["text"].(string); ok && text != "" {
					messages = append(messages, UnifiedMsg{Role: "system", Content: text})
				}
			}
		}
	}

	// Messages
	for _, am := range req.Messages {
		msgs := convertClaudeMessage(am)
		messages = append(messages, msgs...)
	}

	// Collapse multiple system messages into one
	messages = collapseUnifiedSystemMessages(messages)
	chat.Messages = messages

	chat.Temperature = req.Temperature
	chat.TopP = req.TopP
	if len(req.StopSequences) > 0 {
		chat.Stop = req.StopSequences
	}

	// Tools
	for _, t := range req.Tools {
		chat.Tools = append(chat.Tools, UnifiedTool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		})
	}

	// Tool choice
	chat.ToolChoice = mapAnthropicToolChoiceUnified(req.ToolChoice)

	// Thinking → reasoning_effort
	if req.Thinking != nil && req.Thinking.Type == "enabled" {
		effort := "medium"
		if req.Thinking.BudgetTokens != nil && *req.Thinking.BudgetTokens >= 16000 {
			effort = "high"
		}
		chat.ReasoningEffort = &effort
	}

	return chat
}

// ConvertResponse converts a normalized OpenAI response to an Anthropic response.
func (a *ClaudeAdapter) ConvertResponse(openaiResp *OpenAIResponse, model string) any {
	ant := &AnthropicResponse{
		ID:    "msg_" + strings.TrimPrefix(openaiResp.ID, "chatcmpl-"),
		Type:  "message",
		Role:  "assistant",
		Model: model,
	}

	if len(openaiResp.Choices) == 0 {
		return ant
	}

	msg := openaiResp.Choices[0].Message
	if msg == nil {
		msg = &OpenAIMessage{}
	}

	var content []AnthropicContentBlock

	// Reasoning → thinking block
	if msg.ReasoningContent != "" {
		content = append(content, AnthropicContentBlock{
			Type:     "thinking",
			Thinking: msg.ReasoningContent,
		})
	}

	// Content → text block (already normalized by NormalizeResponse)
	if msg.Content != nil && msg.Content != "" {
		switch v := msg.Content.(type) {
		case string:
			if v != "" {
				content = append(content, AnthropicContentBlock{Type: "text", Text: v})
			}
		case []any:
			for _, part := range v {
				if m, ok := part.(map[string]any); ok {
					if t, ok := m["text"].(string); ok && t != "" {
						content = append(content, AnthropicContentBlock{Type: "text", Text: t})
					}
				}
			}
		}
	}

	// Tool calls → tool_use blocks
	for _, tc := range msg.ToolCalls {
		var input json.RawMessage
		if tc.Function.Arguments == "" {
			input = json.RawMessage("{}")
		} else if s := strings.TrimSpace(tc.Function.Arguments); len(s) > 0 && s[0] == '{' {
			input = json.RawMessage(tc.Function.Arguments)
		} else {
			input = json.RawMessage("{}")
		}
		content = append(content, AnthropicContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	ant.Content = content

	fr := ""
	for _, c := range openaiResp.Choices {
		if c.FinishReason != nil {
			fr = *c.FinishReason
			break
		}
	}
	ant.StopReason = mapAnthropicStopReason(fr)

	if openaiResp.Usage != nil {
		ant.Usage = &AnthropicUsage{
			InputTokens:  openaiResp.Usage.PromptTokens,
			OutputTokens: openaiResp.Usage.CompletionTokens,
		}
	}

	return ant
}

func (a *ClaudeAdapter) NewEmitter(w http.ResponseWriter, model string) Emitter {
	return NewClaudeEmitter(w, model)
}

// =============================================================================
// Helpers for Claude → Unified conversion
// =============================================================================

func convertClaudeMessage(am AnthropicMessage) []UnifiedMsg {
	var out []UnifiedMsg
	role := am.Role
	if role == "" {
		role = "user"
	}

	switch c := am.Content.(type) {
	case string:
		out = append(out, UnifiedMsg{Role: role, Content: c})

	case []any:
		var texts []string
			var thinking string
		var toolCalls []UnifiedToolCall
		var toolResults []struct {
			ID      string
			Content string
		}

		for _, block := range c {
			m, ok := block.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := m["type"].(string)
			switch typ {
			case "text":
				if t, ok := m["text"].(string); ok && t != "" {
					texts = append(texts, t)
				}
			case "thinking":
				if th, ok := m["thinking"].(string); ok && th != "" {
					thinking = th
				}
			case "tool_use":
				id, _ := m["id"].(string)
				name, _ := m["name"].(string)
				input := m["input"]
				var args string
				switch v := input.(type) {
				case string:
					args = v
				case map[string]any:
					b, _ := json.Marshal(v)
					args = string(b)
				default:
					args = "{}"
				}
				toolCalls = append(toolCalls, UnifiedToolCall{
					ID:        id,
					Name:      name,
					Arguments: args,
				})
			case "tool_result":
				toolUseID, _ := m["tool_use_id"].(string)
				content := extractToolResultContentUnified(m["content"])
				toolResults = append(toolResults, struct {
					ID      string
					Content string
				}{toolUseID, content})
			}
		}

		if len(toolCalls) > 0 {
			out = append(out, UnifiedMsg{
				Role:      "assistant",
				Content:   stringOrNilUnified(strings.Join(texts, "")),
				Thinking:  thinking,
				ToolCalls: toolCalls,
			})
			for _, tr := range toolResults {
				out = append(out, UnifiedMsg{
					Role:       "tool",
					ToolCallID: tr.ID,
					Content:    tr.Content,
				})
			}
		} else if len(toolResults) > 0 {
			for _, tr := range toolResults {
				out = append(out, UnifiedMsg{Role: "tool", ToolCallID: tr.ID, Content: tr.Content})
			}
		} else if len(texts) > 0 {
			out = append(out, UnifiedMsg{Role: role, Content: strings.Join(texts, ""), Thinking: thinking})
		}
	}

	return out
}

func extractToolResultContentUnified(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, block := range v {
			if m, ok := block.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func stringOrNilUnified(s string) string {
	if s == "" {
		return ""
	}
	return s
}

func mapAnthropicToolChoiceUnified(tc any) any {
	if tc == nil {
		return nil
	}
	switch v := tc.(type) {
	case string:
		switch v {
		case "auto":
			return "auto"
		case "any", "tool":
			return "required"
		default:
			return nil
		}
	case map[string]any:
		typ, _ := v["type"].(string)
		switch typ {
		case "any":
			return "required"
		case "tool":
			name, _ := v["name"].(string)
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": name,
				},
			}
		}
	}
	return nil
}
