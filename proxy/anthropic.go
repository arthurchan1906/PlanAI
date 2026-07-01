package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// =============================================================================
// Anthropic request structures
// =============================================================================

type AnthropicRequest struct {
	Model         string             `json:"model"`
	System        any                `json:"system,omitempty"`
	Messages      []AnthropicMessage `json:"messages"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	Tools         []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice    any                `json:"tool_choice,omitempty"`
	Thinking      *AnthropicThinking `json:"thinking,omitempty"`
}

type AnthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type AnthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   any             `json:"content,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
}

type AnthropicTool struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	InputSchema  any    `json:"input_schema"`
	CacheControl any    `json:"cache_control,omitempty"`
}

type AnthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
}

// =============================================================================
// Anthropic response structures
// =============================================================================

type AnthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []AnthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason,omitempty"`
	Usage      *AnthropicUsage         `json:"usage,omitempty"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// =============================================================================
// Request: Anthropic → OpenAI Chat
// =============================================================================

func anthropicToChat(req *AnthropicRequest) *OpenAIRequest {
	chat := &OpenAIRequest{
		Model:     effectiveModel(req.Model),
		Stream:    req.Stream,
		MaxTokens: &req.MaxTokens,
	}

	var messages []OpenAIMessage

	switch s := req.System.(type) {
	case string:
		if strings.TrimSpace(s) != "" {
			messages = append(messages, OpenAIMessage{Role: "system", Content: s})
		}
	case []any:
		for _, block := range s {
			if m, ok := block.(map[string]any); ok {
				if text, ok := m["text"].(string); ok && text != "" {
					messages = append(messages, OpenAIMessage{Role: "system", Content: text})
				}
			}
		}
	}

	for _, am := range req.Messages {
		msgs := convertAnthropicMessage(am)
		messages = append(messages, msgs...)
	}

	normalizeSystemMessages(&messages)
	chat.Messages = messages

	chat.Temperature = req.Temperature
	chat.TopP = req.TopP
	if len(req.StopSequences) > 0 {
		chat.Stop = req.StopSequences
	}

	for _, t := range req.Tools {
		chat.Tools = append(chat.Tools, OpenAITool{
			Type: "function",
			Function: OpenAIFuncDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	chat.ToolChoice = mapAnthropicToolChoice(req.ToolChoice)

	if req.Thinking != nil && req.Thinking.Type == "enabled" {
		effort := "medium"
		if req.Thinking.BudgetTokens != nil && *req.Thinking.BudgetTokens >= 16000 {
			effort = "high"
		}
		chat.ReasoningEffort = &effort
	}

	return chat
}

func mapAnthropicToolChoice(tc any) any {
	if tc == nil {
		return nil
	}
	switch v := tc.(type) {
	case string:
		switch v {
		case "auto":
			return "auto"
		case "any":
			return "required"
		case "tool":
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

func convertAnthropicMessage(am AnthropicMessage) []OpenAIMessage {
	var out []OpenAIMessage
	role := am.Role
	if role == "" {
		role = "user"
	}

	switch c := am.Content.(type) {
	case string:
		out = append(out, OpenAIMessage{Role: role, Content: c})

	case []any:
		var texts []string
		var toolCalls []OpenAIToolCall
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
			case "text", "thinking":
				if t, ok := m["text"].(string); ok && t != "" {
					texts = append(texts, t)
				}
				if th, ok := m["thinking"].(string); ok && th != "" {
					texts = append(texts, th)
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
				toolCalls = append(toolCalls, OpenAIToolCall{
					ID:   id,
					Type: "function",
					Function: OpenAIToolCallFunction{
						Name:      name,
						Arguments: args,
					},
				})
			case "tool_result":
				toolUseID, _ := m["tool_use_id"].(string)
				content := extractToolResultContent(m["content"])
				toolResults = append(toolResults, struct {
					ID      string
					Content string
				}{toolUseID, content})
			}
		}

		if len(toolCalls) > 0 {
			out = append(out, OpenAIMessage{
				Role:      "assistant",
				Content:   stringOrNil(strings.Join(texts, "")),
				ToolCalls: toolCalls,
			})
			for _, tr := range toolResults {
				out = append(out, OpenAIMessage{
					Role:       "tool",
					ToolCallID: tr.ID,
					Content:    tr.Content,
				})
			}
		} else if len(toolResults) > 0 {
			for _, tr := range toolResults {
				out = append(out, OpenAIMessage{Role: "tool", ToolCallID: tr.ID, Content: tr.Content})
			}
		} else if len(texts) > 0 {
			out = append(out, OpenAIMessage{Role: role, Content: strings.Join(texts, "")})
		}
	}

	return out
}

func extractToolResultContent(content any) string {
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

func stringOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func normalizeSystemMessages(msgs *[]OpenAIMessage) {
	var sysParts []string
	var rest []OpenAIMessage
	for _, m := range *msgs {
		if m.Role == "system" {
			if s, ok := m.Content.(string); ok && strings.TrimSpace(s) != "" {
				sysParts = append(sysParts, s)
			}
			continue
		}
		rest = append(rest, m)
	}
	if len(sysParts) == 0 {
		return
	}
	result := make([]OpenAIMessage, 0, len(rest)+1)
	result = append(result, OpenAIMessage{Role: "system", Content: strings.Join(sysParts, "\n\n")})
	result = append(result, rest...)
	*msgs = result
}

// =============================================================================
// Response: OpenAI Chat → Anthropic
// =============================================================================

func chatToAnthropic(chatResp *OpenAIResponse, model string) *AnthropicResponse {
	ant := &AnthropicResponse{
		ID:    "msg_" + strings.TrimPrefix(chatResp.ID, "chatcmpl-"),
		Type:  "message",
		Role:  "assistant",
		Model: model,
	}

	if len(chatResp.Choices) == 0 {
		return ant
	}

	msg := chatResp.Choices[0].Message
	if msg == nil {
		msg = &OpenAIMessage{}
	}

	var content []AnthropicContentBlock

	if msg.ReasoningContent != "" {
		content = append(content, AnthropicContentBlock{
			Type:     "thinking",
			Thinking: msg.ReasoningContent,
		})
	}

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

	for _, tc := range msg.ToolCalls {
		var input json.RawMessage
		if tc.Function.Arguments == "" {
			input = json.RawMessage("{}")
		} else if strings.TrimSpace(tc.Function.Arguments)[0] == '{' {
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
	for _, c := range chatResp.Choices {
		if c.FinishReason != nil {
			fr = *c.FinishReason
			break
		}
	}
	ant.StopReason = mapAnthropicStopReason(fr)

	if chatResp.Usage != nil {
		ant.Usage = &AnthropicUsage{
			InputTokens:  chatResp.Usage.PromptTokens,
			OutputTokens: chatResp.Usage.CompletionTokens,
		}
	}

	return ant
}

func mapAnthropicStopReason(fr string) string {
	switch fr {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}

// =============================================================================
// HTTP handlers
// =============================================================================

func handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var req AnthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("[ANTHROPIC] ERROR parsing request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	model := req.Model
	log.Printf("[ANTHROPIC] → messages  model=%s stream=%v", model, req.Stream)
	apiKey := extractAPIKey(r)

	if req.Stream {
		handleAnthropicStream(w, &req, model, apiKey)
	} else {
		handleAnthropicNonStream(w, &req, model, apiKey)
	}
}

func handleAnthropicNonStream(w http.ResponseWriter, req *AnthropicRequest, model, apiKey string) {
	chatReq := anthropicToChat(req)
	respBody, err := forwardToUpstream("chat/completions", chatReq, apiKey, "")
	if err != nil {
		log.Printf("[ANTHROPIC] ERROR upstream: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	var chatResp OpenAIResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		log.Printf("[ANTHROPIC] ERROR parsing upstream: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	antResp := chatToAnthropic(&chatResp, model)
	writeJSON(w, http.StatusOK, antResp)
	log.Printf("[ANTHROPIC] ← complete  model=%s", model)
}

func handleAnthropicStream(w http.ResponseWriter, req *AnthropicRequest, model, apiKey string) {
	chatReq := anthropicToChat(req)
	chatReq.Stream = true

	respBody, err := forwardToUpstreamStream("chat/completions", chatReq, apiKey, "")
	if err != nil {
		log.Printf("[ANTHROPIC] ERROR upstream stream: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer respBody.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)

	emitSSE := func(event, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	messageStarted := false
	messageID := "msg_proxy"
	modelName := model
	nextBlockIndex := 0
	var textBlockOpen bool
	var textBlockIndex int
	var thinkBlockOpen bool
	var thinkBlockIndex int
	toolAcc := map[int]*streamToolCall{}
	var latestUsage *OpenAIUsage

	scanner := bufio.NewScanner(respBody)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			if textBlockOpen {
				emitSSE("content_block_stop", sseJSON(map[string]any{
					"type": "content_block_stop", "index": textBlockIndex,
				}))
				textBlockOpen = false
			}
			if thinkBlockOpen {
				emitSSE("content_block_stop", sseJSON(map[string]any{
					"type": "content_block_stop", "index": thinkBlockIndex,
				}))
				thinkBlockOpen = false
			}

			for i := 0; i < len(toolAcc); i++ {
				acc, ok := toolAcc[i]
				if !ok {
					continue
				}
				blockIdx := nextBlockIndex
				nextBlockIndex++
				emitSSE("content_block_start", sseJSON(map[string]any{
					"type":  "content_block_start",
					"index": blockIdx,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    acc.ID,
						"name":  acc.Name,
						"input": json.RawMessage("{}"),
					},
				}))
				if acc.Arguments != "" && strings.TrimSpace(acc.Arguments)[0] == '{' {
					emitSSE("content_block_delta", sseJSON(map[string]any{
						"type":  "content_block_delta",
						"index": blockIdx,
						"delta": map[string]any{
							"type":         "input_json_delta",
							"partial_json": acc.Arguments,
						},
					}))
				}
				emitSSE("content_block_stop", sseJSON(map[string]any{
					"type": "content_block_stop", "index": blockIdx,
				}))
			}

			usageJSON := map[string]any{"input_tokens": 0, "output_tokens": 0}
			if latestUsage != nil {
				usageJSON = map[string]any{
					"input_tokens":  latestUsage.PromptTokens,
					"output_tokens": latestUsage.CompletionTokens,
				}
			}
			emitSSE("message_delta", sseJSON(map[string]any{
				"type": "message_delta",
				"delta": map[string]any{
					"stop_reason":   "end_turn",
					"stop_sequence": nil,
				},
				"usage": usageJSON,
			}))
			emitSSE("message_stop", sseJSON(map[string]any{"type": "message_stop"}))
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			continue
		}

		if !messageStarted {
			messageStarted = true
			if id, ok := raw["id"].(string); ok && id != "" {
				messageID = "msg_" + id
			}
			if m, ok := raw["model"].(string); ok && m != "" {
				modelName = m
			}
			emitSSE("message_start", sseJSON(map[string]any{
				"type": "message_start",
				"message": map[string]any{
					"id":    messageID,
					"type":  "message",
					"role":  "assistant",
					"model": modelName,
					"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
				},
			}))
		}

		if u, ok := raw["usage"].(map[string]any); ok {
			pt, _ := u["prompt_tokens"].(float64)
			ct, _ := u["completion_tokens"].(float64)
			latestUsage = &OpenAIUsage{
				PromptTokens:     int(pt),
				CompletionTokens: int(ct),
				TotalTokens:      int(pt) + int(ct),
			}
		}

		choices, _ := raw["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)

		if delta != nil {
			reasoning := ""
			for _, k := range []string{"reasoning_content", "reasoning"} {
				if s, ok := delta[k].(string); ok && s != "" {
					reasoning = s
					break
				}
			}
			if reasoning != "" {
				if !thinkBlockOpen {
					thinkBlockIndex = nextBlockIndex
					nextBlockIndex++
					thinkBlockOpen = true
					emitSSE("content_block_start", sseJSON(map[string]any{
						"type":  "content_block_start",
						"index": thinkBlockIndex,
						"content_block": map[string]any{
							"type":     "thinking",
							"thinking": "",
						},
					}))
				}
				emitSSE("content_block_delta", sseJSON(map[string]any{
					"type":  "content_block_delta",
					"index": thinkBlockIndex,
					"delta": map[string]any{
						"type":     "thinking_delta",
						"thinking": reasoning,
					},
				}))
			}

			textDelta := ""
			if s, ok := delta["content"].(string); ok && s != "" {
				textDelta = s
			}
			if textDelta != "" {
				if thinkBlockOpen {
					emitSSE("content_block_stop", sseJSON(map[string]any{
						"type": "content_block_stop", "index": thinkBlockIndex,
					}))
					thinkBlockOpen = false
				}
				if !textBlockOpen {
					textBlockIndex = nextBlockIndex
					nextBlockIndex++
					textBlockOpen = true
					emitSSE("content_block_start", sseJSON(map[string]any{
						"type":  "content_block_start",
						"index": textBlockIndex,
						"content_block": map[string]any{
							"type": "text",
							"text": "",
						},
					}))
				}
				emitSSE("content_block_delta", sseJSON(map[string]any{
					"type":  "content_block_delta",
					"index": textBlockIndex,
					"delta": map[string]any{
						"type": "text_delta",
						"text": textDelta,
					},
				}))
			}

			if tcs, ok := delta["tool_calls"].([]any); ok {
				if thinkBlockOpen {
					emitSSE("content_block_stop", sseJSON(map[string]any{
						"type": "content_block_stop", "index": thinkBlockIndex,
					}))
					thinkBlockOpen = false
				}
				if textBlockOpen {
					emitSSE("content_block_stop", sseJSON(map[string]any{
						"type": "content_block_stop", "index": textBlockIndex,
					}))
					textBlockOpen = false
				}
				for _, tc := range tcs {
					tcMap := tc.(map[string]any)
					idx := int(tcMap["index"].(float64))
					if _, exists := toolAcc[idx]; !exists {
						toolAcc[idx] = &streamToolCall{}
					}
					acc := toolAcc[idx]
					if id, ok := tcMap["id"].(string); ok && id != "" {
						acc.ID = id
					}
					if fn, ok := tcMap["function"].(map[string]any); ok {
						if name, ok := fn["name"].(string); ok && name != "" {
							acc.Name = name
						}
						if args, ok := fn["arguments"].(string); ok {
							acc.Arguments += args
						}
					}
				}
			}
		}
	}

	log.Printf("[ANTHROPIC] ← stream complete  model=%s", model)
}
