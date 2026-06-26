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
	}

	switch v := req.Input.(type) {
	case string:
		messages = append(messages, OpenAIMessage{Role: "user", Content: v})
	case []any:
		appendInputItemsAsMessages(v, &messages)
	}

	messages = collapseSystemMessages(messages)

	roles := make([]string, len(messages))
	for i, m := range messages {
		roles[i] = m.Role
	}
	log.Printf("[RESPONSES] → upstream: %d messages roles=%v tools=%d", len(messages), roles, len(chat.Tools))

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

	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		chat.ReasoningEffort = &req.Reasoning.Effort
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

	if msg.ReasoningContent != "" {
		output = append(output, ResponsesOutputItem{
			ID:      "rs_" + responseID,
			Type:    "reasoning",
			Status:  "completed",
			Summary: []ResponsesContentPart{{Type: "summary_text", Text: msg.ReasoningContent}},
		})
	}

	if msg.Content != nil && msg.Content != "" {
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

// =============================================================================
// HTTP handlers for Responses API
// =============================================================================

func handleResponsesCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var req ResponsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		log.Printf("[RESPONSES] ERROR parsing request: %v — body: %s", err, preview)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	model := req.Model
	var toolNames []string
	for _, t := range req.Tools {
		switch t.Type {
		case "function", "":
			name := t.Name
			if name == "" && t.Function != nil {
				name = t.Function.Name
			}
			toolNames = append(toolNames, name)
		case "namespace":
			for _, nt := range t.Tools {
				name := nt.Name
				if name == "" && nt.Function != nil {
					name = nt.Function.Name
				}
				toolNames = append(toolNames, name)
			}
		default:
			toolNames = append(toolNames, t.Type+":"+t.Name)
		}
	}
	msgCount := 0
	switch v := req.Input.(type) {
	case []any:
		msgCount = len(v)
	case string:
		msgCount = 1
	}
	log.Printf("[RESPONSES] → generate  model=%s stream=%v max_tokens=%v tools=%d msgs=%d names=%v",
		model, req.Stream, req.MaxOutputTokens, len(req.Tools), msgCount, toolNames)

		apiKey := extractAPIKey(r)
	if req.Stream {
		handleResponsesStream(w, &req, model, apiKey)
	} else {
		handleResponsesNonStream(w, &req, model, apiKey)
	}
}

func handleResponsesNonStream(w http.ResponseWriter, req *ResponsesRequest, model, apiKey string) {
	chatReq := responsesToChat(req)
	respBody, err := forwardToUpstream("chat/completions", chatReq, apiKey)
	if err != nil {
		log.Printf("[RESPONSES] ERROR upstream: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	var chatResp OpenAIResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		log.Printf("[RESPONSES] ERROR parsing upstream response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	responsesResp := chatCompletionToResponse(&chatResp, model)
	writeJSON(w, http.StatusOK, responsesResp)
	log.Printf("[RESPONSES] ← complete  model=%s", model)
}

func handleResponsesStream(w http.ResponseWriter, req *ResponsesRequest, model, apiKey string) {
	chatReq := responsesToChat(req)
	chatReq.Stream = true

	respBody, err := forwardToUpstreamStream("chat/completions", chatReq, apiKey)
	if err != nil {
		log.Printf("[RESPONSES] ERROR upstream stream: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer respBody.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)

	responseStarted := false
	responseID := "resp_ccswitch"
	var modelName string
	var totalTokens int
	textAdded := false
	reasoningAdded := false
	var textItemID string
	var reasoningItemID string
	var textOutputIndex int
	var reasoningOutputIndex int
	outputIndex := 0
	toolCallAcc := map[int]*streamToolCall{}
	pendingFinish := ""

	scanner := bufio.NewScanner(respBody)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	emitSSE := func(event, data string) {
		if event != "" {
			fmt.Fprintf(w, "event: %s\n", event)
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			for _, acc := range toolCallAcc {
				if acc.itemID != "" {
					emitSSE("response.output_item.done", sseJSON(map[string]any{
						"type":         "response.output_item.done",
						"output_index": acc.outputIdx,
						"item": map[string]any{
							"id":        acc.itemID,
							"type":      "function_call",
							"status":    "completed",
							"call_id":   acc.ID,
							"name":      acc.Name,
							"arguments": acc.Arguments,
						},
					}))
				}
			}

			status := "completed"
			if pendingFinish == "length" {
				status = "incomplete"
			}
			emitSSE("response.completed", sseJSON(map[string]any{
				"type": "response.completed",
				"response": map[string]any{
					"id":     responseID,
					"object": "response",
					"status": status,
					"model":  modelName,
					"usage":  chatUsageToResponsesUsage(nil),
				},
			}))
			continue
		}

		var rawChunk map[string]any
		if err := json.Unmarshal([]byte(data), &rawChunk); err != nil {
			continue
		}

		if !responseStarted {
			responseStarted = true
			if id, ok := rawChunk["id"].(string); ok && id != "" {
				responseID = "resp_" + id
			}
			if m, ok := rawChunk["model"].(string); ok && m != "" {
				modelName = m
			}
			if modelName == "" {
				modelName = model
			}

			emitSSE("response.created", sseJSON(map[string]any{
				"type": "response.created",
				"response": map[string]any{
					"id":     responseID,
					"object": "response",
					"status": "in_progress",
					"model":  modelName,
				},
			}))
		}

		choices, _ := rawChunk["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)

		if usage, ok := rawChunk["usage"].(map[string]any); ok {
			if tt, ok := usage["total_tokens"].(float64); ok {
				totalTokens = int(tt)
			}
		}

		if delta != nil {
			reasoningText := ""
			for _, key := range []string{"reasoning_content", "reasoning"} {
				if s, ok := delta[key].(string); ok && s != "" {
					reasoningText = s
					break
				}
			}
			if reasoningText != "" {
				if !reasoningAdded {
					reasoningAdded = true
					reasoningOutputIndex = outputIndex
					outputIndex++
					reasoningItemID = "rs_" + responseID
					emitSSE("response.output_item.added", sseJSON(map[string]any{
						"type":         "response.output_item.added",
						"output_index": reasoningOutputIndex,
						"item": map[string]any{
							"id":      reasoningItemID,
							"type":    "reasoning",
							"status":  "in_progress",
							"summary": []any{},
						},
					}))
					emitSSE("response.reasoning_summary_part.added", sseJSON(map[string]any{
						"type":          "response.reasoning_summary_part.added",
						"item_id":       reasoningItemID,
						"output_index":  reasoningOutputIndex,
						"summary_index": 0,
						"part": map[string]any{
							"type": "summary_text",
							"text": "",
						},
					}))
				}
				emitSSE("response.reasoning_summary_text.delta", sseJSON(map[string]any{
					"type":          "response.reasoning_summary_text.delta",
					"item_id":       reasoningItemID,
					"output_index":  reasoningOutputIndex,
					"summary_index": 0,
					"delta":         reasoningText,
				}))
			}

			textDelta := ""
			if c, ok := delta["content"]; ok {
				if s, ok := c.(string); ok && s != "" {
					textDelta = s
				}
			}
			if textDelta != "" {
				if !textAdded {
					textAdded = true
					textOutputIndex = outputIndex
					outputIndex++
					textItemID = responseID + "_msg"
					emitSSE("response.output_item.added", sseJSON(map[string]any{
						"type":         "response.output_item.added",
						"output_index": textOutputIndex,
						"item": map[string]any{
							"id":      textItemID,
							"type":    "message",
							"role":    "assistant",
							"status":  "in_progress",
							"content": []any{},
						},
					}))
					emitSSE("response.content_part.added", sseJSON(map[string]any{
						"type":         "response.content_part.added",
						"item_id":      textItemID,
						"output_index": textOutputIndex,
						"content_index": 0,
						"part": map[string]any{
							"type":        "output_text",
							"text":        "",
							"annotations": []any{},
						},
					}))
				}
				emitSSE("response.output_text.delta", sseJSON(map[string]any{
					"type":          "response.output_text.delta",
					"item_id":       textItemID,
					"output_index":  textOutputIndex,
					"content_index": 0,
					"delta":         textDelta,
				}))
			}

			if tcs, ok := delta["tool_calls"].([]any); ok {
				for _, tc := range tcs {
					tcMap := tc.(map[string]any)
					idx := int(tcMap["index"].(float64))
					newTool := false
					if _, exists := toolCallAcc[idx]; !exists {
						toolCallAcc[idx] = &streamToolCall{}
						newTool = true
					}
					acc := toolCallAcc[idx]
					if id, ok := tcMap["id"].(string); ok && id != "" {
						acc.ID = id
					}
					if fn, ok := tcMap["function"].(map[string]any); ok {
						if name, ok := fn["name"].(string); ok && name != "" {
							acc.Name = name
						}
						if args, ok := fn["arguments"].(string); ok && args != "" {
							acc.Arguments += args
							if newTool {
								oi := outputIndex
								outputIndex++
								itemID := fmt.Sprintf("fc_%s_%d", responseID, oi)
								acc.itemID = itemID
								acc.outputIdx = oi
								emitSSE("response.output_item.added", sseJSON(map[string]any{
									"type":         "response.output_item.added",
									"output_index": oi,
									"item": map[string]any{
										"id":        itemID,
										"type":      "function_call",
										"status":    "in_progress",
										"call_id":   acc.ID,
										"name":      acc.Name,
										"arguments": "",
									},
								}))
							}
							emitSSE("response.function_call_arguments.delta", sseJSON(map[string]any{
								"type":         "response.function_call_arguments.delta",
								"item_id":      acc.itemID,
								"output_index": acc.outputIdx,
								"delta":        args,
							}))
						}
					}
				}
			}
		}

		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			pendingFinish = fr
		}
	}

	log.Printf("[RESPONSES] ← stream complete  model=%s tokens=%d", model, totalTokens)
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
