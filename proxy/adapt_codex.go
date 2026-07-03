package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// =============================================================================
// CodexAdapter — translates OpenAI Responses API protocol ↔ UnifiedReq
// =============================================================================

// CodexAdapter implements ProtocolAdapter for the Codex CLI (OpenAI Responses API).
// Types (ResponsesRequest, ResponsesResponseWrapper, etc.) are defined in responses.go.
type CodexAdapter struct {
	SessionID    string
	namespaceMap map[string]string // short tool name → namespace (for MCP tools)
}

func (a *CodexAdapter) ParseRequest(r *http.Request) (*UnifiedReq, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	var req ResponsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		log.Printf("[CODEX] ERROR parsing request: %v — body: %s", err, preview)
		return nil, err
	}

	return a.toUnified(&req), nil
}

func (a *CodexAdapter) toUnified(req *ResponsesRequest) *UnifiedReq {
	// Extract session_id from request client_metadata
	if req.ClientMetadata != nil {
		if sid, ok := req.ClientMetadata["session_id"]; ok && sid != "" {
			a.SessionID = sid
		}
	}
	chat := &UnifiedReq{
		VirtualModel: req.Model,
		Model:        effectiveModel(req.Model),
		Stream:       req.Stream,
	}

	var messages []UnifiedMsg

	// Instructions → system message
	instr := instructionText(req.Instructions)
	if instr != "" {
		messages = append(messages, UnifiedMsg{Role: "system", Content: instr})
	} else {
		// Mirror old-path behavior: inject a default system prompt so the upstream
		// model knows it's an AI coding assistant (some models need this hint).
		messages = append(messages, UnifiedMsg{
			Role:    "system",
			Content: "You are an AI coding assistant. Use available tools to help the user with their software engineering tasks.",
		})
	}

	// Input items → messages
	switch v := req.Input.(type) {
	case string:
		messages = append(messages, UnifiedMsg{Role: "user", Content: v})
	case []any:
		appendCodexInputItems(v, &messages)
	}

	messages = collapseUnifiedSystemMessages(messages)
	chat.Messages = messages
	chat.Temperature = req.Temperature
	chat.TopP = req.TopP

	if req.MaxOutputTokens != nil {
		chat.MaxTokens = req.MaxOutputTokens
	} else {
		defaultMax := 4096
		chat.MaxTokens = &defaultMax
	}

	for _, t := range req.Tools {
		switch t.Type {
		case "", "function":
			chat.Tools = append(chat.Tools, codexToolToUnified(t))
		case "namespace":
			// Record namespace mapping: short tool name → namespace
			// so the emitter can rebuild full MCP names when the model calls back.
			if a.namespaceMap == nil {
				a.namespaceMap = make(map[string]string)
			}
			for _, nt := range t.Tools {
				shortName := nt.Name
				if shortName == "" && nt.Function != nil {
					shortName = nt.Function.Name
				}
				if shortName != "" {
					a.namespaceMap[shortName] = t.Name
				}
			}
			chat.Tools = append(chat.Tools, explodeNamespaceToUnified(t)...)
		default:
		}
	}

	if req.ToolChoice != nil {
		chat.ToolChoice = req.ToolChoice
	}
	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		chat.ReasoningEffort = &req.Reasoning.Effort
	}

	return chat
}

// ConvertResponse converts a normalized OpenAI response to a Codex Responses API response.
func (a *CodexAdapter) ConvertResponse(openaiResp *OpenAIResponse, model string) any {
	var output []ResponsesOutputItem
	responseID := "resp_" + strings.TrimPrefix(openaiResp.ID, "chatcmpl-")

	if len(openaiResp.Choices) == 0 {
		return &ResponsesResponseWrapper{
			ID:     responseID,
			Object: "response",
			Status: "completed",
			Model:  model,
			Output: output,
			Usage:  chatUsageToResponsesUsage(openaiResp.Usage),
		}
	}

	choice := openaiResp.Choices[0]
	msg := choice.Message
	if msg == nil {
		msg = &OpenAIMessage{}
	}

	hasContent := msg.Content != nil && msg.Content != ""

	// Reasoning item
	if msg.ReasoningContent != "" {
		output = append(output, ResponsesOutputItem{
			ID:      "rs_" + responseID,
			Type:    "reasoning",
			Status:  "completed",
			Summary: []ResponsesContentPart{{Type: "summary_text", Text: msg.ReasoningContent}},
		})
		// If only reasoning (no content), promote it as message
		// (should already be handled by NormalizeResponse, but double-check)
		if !hasContent {
			text := stripThinkBlockLegacy(msg.ReasoningContent)
			output = append(output, ResponsesOutputItem{
				ID:      responseID + "_msg",
				Type:    "message",
				Status:  "completed",
				Role:    "assistant",
				Content: []ResponsesContentPart{{Type: "output_text", Text: text}},
			})
		}
	}

	if hasContent {
		output = append(output, chatMsgToCodexItem(msg, responseID)...)
	}

	for i, tc := range msg.ToolCalls {
		itemID := fmt.Sprintf("fc_%s_%d", responseID, i)
		output = append(output, ResponsesOutputItem{
			ID:        itemID,
			Type:      "function_call",
			Status:    "completed",
			CallID:    tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	status := "completed"
	if choice.FinishReason != nil && *choice.FinishReason == "length" {
		status = "incomplete"
	}

	return &ResponsesResponseWrapper{
		ID:     responseID,
		Object: "response",
		Status: status,
		Model:  model,
		Output: output,
		Usage:  chatUsageToResponsesUsage(openaiResp.Usage),
	}
}

func (a *CodexAdapter) NewEmitter(w http.ResponseWriter, model string) Emitter {
	return NewCodexEmitter(w, model, a.SessionID, a.namespaceMap)
}

// =============================================================================
// Helpers for Codex → Unified conversion
// =============================================================================

func appendCodexInputItems(items []any, messages *[]UnifiedMsg) {
	var pendingToolCalls []UnifiedToolCall

	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := m["type"].(string)

		switch itemType {
		case "function_call":
			name, _ := m["name"].(string)
			callID, _ := m["call_id"].(string)
			if callID == "" {
				id, _ := m["id"].(string)
				callID = id
			}
			args, _ := m["arguments"].(string)
			pendingToolCalls = append(pendingToolCalls, UnifiedToolCall{
				ID:        callID,
				Name:      name,
				Arguments: args,
			})

		case "function_call_output":
			if len(pendingToolCalls) > 0 {
				*messages = append(*messages, UnifiedMsg{
					Role:      "assistant",
					ToolCalls: pendingToolCalls,
				})
				pendingToolCalls = nil
			}
			callID, _ := m["call_id"].(string)
			output := canonicalizeOutputCodex(m["output"])
			*messages = append(*messages, UnifiedMsg{
				Role:       "tool",
				ToolCallID: callID,
				Content:    output,
			})

		case "message", "":
			if len(pendingToolCalls) > 0 {
				*messages = append(*messages, UnifiedMsg{
					Role:      "assistant",
					ToolCalls: pendingToolCalls,
				})
				pendingToolCalls = nil
			}
			role, _ := m["role"].(string)
			if role == "" {
				role = "user"
			}
			content := codexContentToString(m["content"])
			msg := UnifiedMsg{Role: codexRoleToUnified(role), Content: content}
			if rc, _ := m["reasoning_content"].(string); rc != "" {
				msg.Thinking = rc
			}
			*messages = append(*messages, msg)

		case "reasoning":
			// Codex reasoning items — append thinking text to the last assistant message
			// or create a standalone assistant message if none exists yet.
			// Codex UI hides reasoning by default; proxy preserves the text as thinking.
			summary := extractReasoningSummary(m["summary"])
			if summary != "" {
				if len(*messages) > 0 && (*messages)[len(*messages)-1].Role == "assistant" {
					(*messages)[len(*messages)-1].Thinking = summary
				} else {
					*messages = append(*messages, UnifiedMsg{
						Role:    "user",
						Content: "[reasoning] " + summary,
					})
				}
			}

		default:
			if role, ok := m["role"].(string); ok {
				content := codexContentToString(m["content"])
				*messages = append(*messages, UnifiedMsg{Role: codexRoleToUnified(role), Content: content})
			} else {
				desc := fmt.Sprintf("[unknown item: type=%s]", itemType)
				*messages = append(*messages, UnifiedMsg{Role: "user", Content: desc})
			}
		}
	}

	if len(pendingToolCalls) > 0 {
		*messages = append(*messages, UnifiedMsg{
			Role:      "assistant",
			ToolCalls: pendingToolCalls,
		})
	}
}

func extractReasoningSummary(summary any) string {
	if summary == nil {
		return ""
	}
	arr, ok := summary.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		txt, _ := m["text"].(string)
		switch t {
		case "summary_text":
			if txt != "" {
				parts = append(parts, txt)
			}
		}
	}
	return strings.Join(parts, "\n")
}
func codexRoleToUnified(role string) string {
	switch role {
	case "system":
		return "system"
	case "developer":
		return "user"
	case "assistant":
		return "assistant"
	case "tool":
		return "tool"
	default:
		return "user"
	}
}

func codexContentToString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case nil:
		return ""
	case []any:
		var texts []string
		for _, part := range v {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			ptype, _ := m["type"].(string)
			switch ptype {
			case "input_text", "output_text", "text":
				if t, ok := m["text"].(string); ok && t != "" {
					texts = append(texts, t)
				}
			case "input_image":
				b, _ := json.Marshal(v)
				return string(b)
			}
		}
		if len(texts) == 0 {
			return ""
		}
		return strings.Join(texts, "\n")
	}
	return fmt.Sprint(content)
}

func canonicalizeOutputCodex(output any) string {
	switch v := output.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func codexToolToUnified(tool ResponsesTool) UnifiedTool {
	var name, description string
	var parameters any
	if tool.Function != nil {
		name = tool.Function.Name
		description = tool.Function.Description
		parameters = tool.Function.Parameters
	}
	if name == "" {
		name = tool.Name
	}
	if description == "" {
		description = tool.Description
	}
	if parameters == nil {
		parameters = tool.Parameters
	}
	if parameters == nil {
		parameters = map[string]any{"type": "object"}
	}
	return UnifiedTool{
		Name:        name,
		Description: description,
		Parameters:  parameters,
	}
}

func explodeNamespaceToUnified(tool ResponsesTool) []UnifiedTool {
	var out []UnifiedTool
	for _, nt := range tool.Tools {
		if nt.Type != "" && nt.Type != "function" {
			continue
		}
		name := nt.Name
		if name == "" && nt.Function != nil {
			name = nt.Function.Name
		}
		desc := nt.Description
		if desc == "" && nt.Function != nil {
			desc = nt.Function.Description
		}
		params := nt.Parameters
		if params == nil && nt.Function != nil {
			params = nt.Function.Parameters
		}
		if params == nil {
			params = map[string]any{"type": "object"}
		}
		out = append(out, UnifiedTool{
			Name:        name,
			Description: desc,
			Parameters:  params,
		})
	}
	return out
}

func chatMsgToCodexItem(msg *OpenAIMessage, responseID string) []ResponsesOutputItem {
	var content []ResponsesContentPart
	switch v := msg.Content.(type) {
	case string:
		if v != "" {
			text := stripThinkBlockLegacy(v)
			content = append(content, ResponsesContentPart{Type: "output_text", Text: text})
		}
	case nil:
	default:
		b, _ := json.Marshal(v)
		content = append(content, ResponsesContentPart{Type: "output_text", Text: string(b)})
	}

	if len(content) == 0 {
		return nil
	}

	itemID := responseID + "_msg"
	return []ResponsesOutputItem{{
		ID:      itemID,
		Type:    "message",
		Status:  "completed",
		Role:    "assistant",
		Content: content,
	}}
}

// stripThinkBlockLegacy removes <think>...</think> wrapper from text.
func stripThinkBlockLegacy(text string) string {
	if strings.HasPrefix(strings.TrimSpace(text), "<think>") {
		endIdx := strings.Index(text, "</think>")
		if endIdx > 0 {
			return strings.TrimSpace(text[endIdx+8:])
		}
	}
	return text
}

func collapseUnifiedSystemMessages(msgs []UnifiedMsg) []UnifiedMsg {
	var sysParts []string
	var rest []UnifiedMsg
	for _, m := range msgs {
		if m.Role == "system" {
			if strings.TrimSpace(m.Content) != "" {
				sysParts = append(sysParts, m.Content)
			}
			continue
		}
		rest = append(rest, m)
	}
	if len(sysParts) == 0 {
		return rest
	}
	out := make([]UnifiedMsg, 0, len(rest)+1)
	out = append(out, UnifiedMsg{Role: "system", Content: strings.Join(sysParts, "\n\n")})
	out = append(out, rest...)
	return out
}
