package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// =============================================================================
// Responses API request structures
// =============================================================================

type ResponsesRequest struct {
	Model           string              `json:"model"`
	Input           any                 `json:"input,omitempty"`
	Instructions    any                 `json:"instructions,omitempty"`
	Tools           []ResponsesTool     `json:"tools,omitempty"`
	Temperature     *float64            `json:"temperature,omitempty"`
	TopP            *float64            `json:"top_p,omitempty"`
	MaxOutputTokens *int                `json:"max_output_tokens,omitempty"`
	ClientMetadata  map[string]string    `json:"client_metadata,omitempty"`
	Stream          bool                `json:"stream,omitempty"`
	ToolChoice      any                 `json:"tool_choice,omitempty"`
	Reasoning       *ResponsesReasoning `json:"reasoning,omitempty"`
	Truncation      string              `json:"truncation,omitempty"`
}

type ResponsesInputItem struct {
	Type             string `json:"type,omitempty"`
	Role             string `json:"role,omitempty"`
	Content          any    `json:"content,omitempty"`
	CallID           string `json:"call_id,omitempty"`
	Name             string `json:"name,omitempty"`
	Arguments        string `json:"arguments,omitempty"`
	Output           any    `json:"output,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type ResponsesContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL any    `json:"image_url,omitempty"`
}

type ResponsesTool struct {
	Type        string                  `json:"type"`
	Name        string                  `json:"name,omitempty"`
	Function    *ResponsesToolFunction  `json:"function,omitempty"`
	Strict      *bool                   `json:"strict,omitempty"`
	Parameters  any                     `json:"parameters,omitempty"`
	Description string                  `json:"description,omitempty"`
	Tools       []ResponsesNamespaceTool `json:"tools,omitempty"`
}

type ResponsesNamespaceTool struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name,omitempty"`
	Function    *ResponsesToolFunction `json:"function,omitempty"`
	Strict      *bool                  `json:"strict,omitempty"`
	Parameters  any                    `json:"parameters,omitempty"`
	Description string                 `json:"description,omitempty"`
}

type ResponsesToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      *bool  `json:"strict,omitempty"`
}

type ResponsesReasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// =============================================================================
// Responses API response structures
// =============================================================================

type ResponsesOutputItem struct {
	ID               string                 `json:"id"`
	Type             string                 `json:"type"`
	Status           string                 `json:"status,omitempty"`
	Role             string                 `json:"role,omitempty"`
	Content          []ResponsesContentPart `json:"content,omitempty"`
	CallID           string                 `json:"call_id,omitempty"`
	Name             string                 `json:"name,omitempty"`
	Arguments        string                 `json:"arguments,omitempty"`
	Summary          []ResponsesContentPart `json:"summary,omitempty"`
	ReasoningContent string                 `json:"reasoning_content,omitempty"`
}

type ResponsesResponseWrapper struct {
	ID                string                 `json:"id"`
	Object            string                 `json:"object"`
	Status            string                 `json:"status"`
	Model             string                 `json:"model"`
	Output            []ResponsesOutputItem  `json:"output"`
	Usage             any                    `json:"usage,omitempty"`
	IncompleteDetails any                    `json:"incomplete_details,omitempty"`
}

// =============================================================================
// Request: Responses API → Chat Completions
// =============================================================================

func responsesToChat(req *ResponsesRequest) *OpenAIRequest {
	chat := &OpenAIRequest{
		Model:  effectiveModel(req.Model),
		Stream: req.Stream,
	}

	var messages []OpenAIMessage

	instr := instructionText(req.Instructions)
	if instr != "" {
		messages = append(messages, OpenAIMessage{Role: "system", Content: instr})
	} else {
		log.Printf("[RESPONSES] WARNING: empty instructions — injecting default system prompt")
		messages = append([]OpenAIMessage{{
			Role:    "system",
			Content: "You are an AI coding assistant. Use available tools to help the user with their software engineering tasks.",
		}}, messages...)
	}

	switch v := req.Input.(type) {
	case string:
		messages = append(messages, OpenAIMessage{Role: "user", Content: v})
	case []any:
		appendInputItemsAsMessages(v, &messages)
	}

	messages = collapseSystemMessages(messages)

	chat.Messages = messages
	chat.Temperature = req.Temperature
	chat.TopP = req.TopP
	if req.MaxOutputTokens != nil {
		chat.MaxTokens = req.MaxOutputTokens
	} else {
		// Codex doesn't set max_output_tokens; llama.cpp rejects null.
		// Default to a generous limit so the model can generate freely.
		defaultMaxTokens := 4096
		chat.MaxTokens = &defaultMaxTokens
	}

	for _, t := range req.Tools {
		switch t.Type {
		case "", "function":
			chat.Tools = append(chat.Tools, responsesToolToOpenAI(t))
		case "namespace":
			chat.Tools = append(chat.Tools, explodeNamespaceTools(t)...)
		default:
			log.Printf("[RESPONSES] WARNING: skipping unsupported tool type=%q name=%q", t.Type, t.Name)
		}
	}

	// Preserve tool_choice from the original request
	if req.ToolChoice != nil {
		chat.ToolChoice = req.ToolChoice
	}

	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		chat.ReasoningEffort = &req.Reasoning.Effort
	}

	// Log AFTER tool expansion so tools count is accurate (OpenCode P2 fix)
	{
		roles := make([]string, len(chat.Messages))
		for i, m := range chat.Messages {
			roles[i] = m.Role
		}
		var toolNames []string
		for _, t := range chat.Tools {
			toolNames = append(toolNames, t.Function.Name)
		}
		log.Printf("[RESPONSES] → upstream: %d messages roles=%v tools=%d names=%v",
			len(chat.Messages), roles, len(chat.Tools), firstN(toolNames, 15))
	}

	return chat
}

func instructionText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"]; ok {
					parts = append(parts, fmt.Sprint(t))
				}
			}
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}

func appendInputItemsAsMessages(items []any, messages *[]OpenAIMessage) {
	var pendingToolCalls []OpenAIToolCall

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
			pendingToolCalls = append(pendingToolCalls, OpenAIToolCall{
				ID:   callID,
				Type: "function",
				Function: OpenAIToolCallFunction{
					Name:      name,
					Arguments: args,
				},
			})

		case "function_call_output":
			if len(pendingToolCalls) > 0 {
				*messages = append(*messages, OpenAIMessage{
					Role:      "assistant",
					Content:   nil,
					ToolCalls: pendingToolCalls,
				})
				pendingToolCalls = nil
			}
			callID, _ := m["call_id"].(string)
			output := canonicalizeOutput(m["output"])
			*messages = append(*messages, OpenAIMessage{
				Role:       "tool",
				ToolCallID: callID,
				Content:    output,
			})

		case "message", "":
			if len(pendingToolCalls) > 0 {
				*messages = append(*messages, OpenAIMessage{
					Role:      "assistant",
					Content:   nil,
					ToolCalls: pendingToolCalls,
				})
				pendingToolCalls = nil
			}
			role, _ := m["role"].(string)
			if role == "" {
				role = "user"
			}
			content := responsesContentToChatContent(m["content"])
			msg := OpenAIMessage{Role: responsesRoleToChat(role), Content: content}
			if rc, _ := m["reasoning_content"].(string); rc != "" {
				msg.ReasoningContent = rc
			}
			*messages = append(*messages, msg)

		default:
			if role, ok := m["role"].(string); ok {
				content := responsesContentToChatContent(m["content"])
				*messages = append(*messages, OpenAIMessage{Role: responsesRoleToChat(role), Content: content})
			} else {
				log.Printf("[RESPONSES] WARNING: unknown input item type=%q, converting to user message", itemType)
				desc := fmt.Sprintf("[unknown item: type=%s]", itemType)
				*messages = append(*messages, OpenAIMessage{Role: "user", Content: desc})
			}
		}
	}

	if len(pendingToolCalls) > 0 {
		*messages = append(*messages, OpenAIMessage{
			Role:      "assistant",
			Content:   nil,
			ToolCalls: pendingToolCalls,
		})
	}
}

func responsesRoleToChat(role string) string {
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

func responsesContentToChatContent(content any) any {
	switch v := content.(type) {
	case string, nil:
		return v
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
				return content
			}
		}
		if len(texts) == 0 {
			return ""
		}
		if len(texts) == 1 {
			return texts[0]
		}
		return strings.Join(texts, "\n")
	}
	return content
}

func canonicalizeOutput(output any) string {
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

func explodeNamespaceTools(t ResponsesTool) []OpenAITool {
	log.Printf("[RESPONSES] exploding namespace tool %q with %d nested tools", t.Name, len(t.Tools))
	var out []OpenAITool
	for _, nt := range t.Tools {
		if nt.Type != "" && nt.Type != "function" {
			log.Printf("[RESPONSES] WARNING: skipping nested tool type=%q name=%q in namespace %q", nt.Type, nt.Name, t.Name)
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
		out = append(out, OpenAITool{
			Type: "function",
			Function: OpenAIFuncDecl{
				Name:        name,
				Description: desc,
				Parameters:  params,
			},
		})
	}
	return out
}

func responsesToolToOpenAI(tool ResponsesTool) OpenAITool {
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

	return OpenAITool{
		Type: "function",
		Function: OpenAIFuncDecl{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
	}
}

// =============================================================================
// Response: Chat Completions → Responses API
// =============================================================================

func chatCompletionToResponse(chatResp *OpenAIResponse, model string) *ResponsesResponseWrapper {
	var output []ResponsesOutputItem
	responseID := "resp_" + strings.TrimPrefix(chatResp.ID, "chatcmpl-")

	if len(chatResp.Choices) == 0 {
		return &ResponsesResponseWrapper{
			ID:     responseID,
			Object: "response",
			Status: "completed",
			Model:  model,
			Output: output,
			Usage:  chatUsageToResponsesUsage(chatResp.Usage),
		}
	}

	choice := chatResp.Choices[0]
	msg := choice.Message
	if msg == nil {
		msg = &OpenAIMessage{}
	}

	hasContent := msg.Content != nil && msg.Content != ""

	if msg.ReasoningContent != "" {
		output = append(output, ResponsesOutputItem{
			ID:      "rs_" + responseID,
			Type:    "reasoning",
			Status:  "completed",
			Summary: []ResponsesContentPart{{Type: "summary_text", Text: msg.ReasoningContent}},
		})
		// If the model only returned reasoning_content (DeepSeek behavior),
		// promote it to a message item so Codex sees the actual answer.
		if !hasContent {
			text := stripThinkBlock(msg.ReasoningContent)
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
		output = append(output, chatMessageToResponseItem(msg, responseID)...)
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

	return &ResponsesResponseWrapper{
		ID:     responseID,
		Object: "response",
		Status: responseStatusFromFinishReason(choice.FinishReason),
		Model:  model,
		Output: output,
		Usage:  chatUsageToResponsesUsage(chatResp.Usage),
	}
}

func chatMessageToResponseItem(msg *OpenAIMessage, responseID string) []ResponsesOutputItem {
	var content []ResponsesContentPart
	switch v := msg.Content.(type) {
	case string:
		if v != "" {
			text := stripThinkBlock(v)
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

func stripThinkBlock(text string) string {
	if strings.HasPrefix(strings.TrimSpace(text), "<think>") {
		endIdx := strings.Index(text, "</think>")
		if endIdx > 0 {
			return strings.TrimSpace(text[endIdx+8:])
		}
	}
	return text
}

func responseStatusFromFinishReason(fr *string) string {
	if fr == nil {
		return "completed"
	}
	switch *fr {
	case "length":
		return "incomplete"
	default:
		return "completed"
	}
}

func chatUsageToResponsesUsage(usage *OpenAIUsage) any {
	if usage == nil {
		return map[string]any{
			"input_tokens": 0, "output_tokens": 0, "total_tokens": 0,
		}
	}
	return map[string]any{
		"input_tokens":  usage.PromptTokens,
		"output_tokens": usage.CompletionTokens,
		"total_tokens":  usage.TotalTokens,
	}
}

func sseJSON(data any) string {
	b, _ := json.Marshal(data)
	return string(b)
}

func collapseSystemMessages(msgs []OpenAIMessage) []OpenAIMessage {
	var systemParts []string
	var rest []OpenAIMessage
	for _, m := range msgs {
		if m.Role == "system" {
			if s, ok := m.Content.(string); ok && strings.TrimSpace(s) != "" {
				systemParts = append(systemParts, s)
			}
			continue
		}
		rest = append(rest, m)
	}
	if len(systemParts) == 0 {
		return rest
	}
	out := make([]OpenAIMessage, 0, len(rest)+1)
	out = append(out, OpenAIMessage{Role: "system", Content: strings.Join(systemParts, "\n\n")})
	out = append(out, rest...)
	return out
}
