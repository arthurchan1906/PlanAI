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
// GeminiAdapter — translates Gemini API protocol ↔ UnifiedReq
// =============================================================================

// GeminiAdapter implements ProtocolAdapter for the Gemini CLI protocol.
// Types (GeminiRequest, GeminiResponse, etc.) are defined in proxy.go.
type GeminiAdapter struct{}

func (a *GeminiAdapter) ParseRequest(r *http.Request) (*UnifiedReq, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	var geminiReq GeminiRequest
	if err := json.Unmarshal(body, &geminiReq); err != nil {
		return nil, err
	}

	// Extract model name from the URL path (Gemini puts it in /v1/models/{model}:action)
	model := extractModel(r.URL.Path)
	return a.toUnified(&geminiReq, model), nil
}

func (a *GeminiAdapter) toUnified(g *GeminiRequest, urlModel string) *UnifiedReq {
	// Prefer the URL-path model; effectiveModel overrides with proxyModel if configured
	model := urlModel
	if loadCfg().proxyModel != "" {
		model = loadCfg().proxyModel
	}
	req := &UnifiedReq{
		Model:  model,
		Stream: false,
	}

	var messages []UnifiedMsg

	// 1. System instruction
	sysInstr := getSystemInstruction(g)
	if sysInstr != nil {
		text := extractText(sysInstr)
		if text != "" {
			messages = append(messages, UnifiedMsg{Role: "system", Content: text})
		}
	}

	// 2. Conversation contents
	for _, c := range g.Contents {
		msgs := convertGeminiContent(c)
		messages = append(messages, msgs...)
	}

	req.Messages = messages

	// 3. Generation config
	gc := getGenerationConfig(g)
	if gc != nil {
		req.Temperature = gc.Temperature
		req.MaxTokens = gc.MaxOutputTokens
		req.TopP = gc.TopP
		req.Stop = gc.StopSequences
	}
	if req.MaxTokens == nil {
		defaultMax := 4096
		req.MaxTokens = &defaultMax
	}

	// 4. Tools
	tools := getTools(g)
	for _, t := range tools {
		for _, fd := range t.FunctionDeclarations {
			req.Tools = append(req.Tools, UnifiedTool{
				Name:        fd.Name,
				Description: fd.Description,
				Parameters:  fd.Parameters,
			})
		}
	}

	return req
}

// ConvertResponse converts a normalized OpenAI response to a Gemini response.
// The caller MUST have called NormalizeResponse first.
func (a *GeminiAdapter) ConvertResponse(openaiResp *OpenAIResponse, model string) any {
	gemini := &GeminiResponse{
		ModelVersion: model,
	}

	for _, choice := range openaiResp.Choices {
		msg := choice.Message
		if msg == nil {
			msg = &OpenAIMessage{}
		}

		var parts []GeminiPart

		// Reasoning as thought
		if msg.ReasoningContent != "" {
			parts = append(parts, GeminiPart{Thought: msg.ReasoningContent})
		}

		// Content as text
		if msg.Content != nil {
			switch v := msg.Content.(type) {
			case string:
				if v != "" {
					parts = append(parts, GeminiPart{Text: v})
				}
			case []any:
				for _, item := range v {
					if m, ok := item.(map[string]any); ok {
						if t, ok := m["text"]; ok {
							parts = append(parts, GeminiPart{Text: fmt.Sprint(t)})
						}
					}
				}
			}
		}

		// Tool calls
		for _, tc := range msg.ToolCalls {
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
			log.Printf("[FUNCTION_CALL] name=%s args=%s id=%s", tc.Function.Name, tc.Function.Arguments, tc.ID)
			parts = append(parts, GeminiPart{
				FunctionCall: &GeminiFuncCall{
					ID:   tc.ID,
					Name: tc.Function.Name,
					Args: args,
				},
			})
		}

		finishReason := mapFinishReason("")
		if choice.FinishReason != nil {
			finishReason = mapFinishReason(*choice.FinishReason)
		}

		gemini.Candidates = append(gemini.Candidates, GeminiCandidate{
			Content:      &GeminiContent{Role: "model", Parts: parts},
			FinishReason: finishReason,
			Index:        choice.Index,
		})
	}

	if openaiResp.Usage != nil {
		gemini.UsageMetadata = &GeminiUsage{
			PromptTokenCount:     openaiResp.Usage.PromptTokens,
			CandidatesTokenCount: openaiResp.Usage.CompletionTokens,
			TotalTokenCount:      openaiResp.Usage.TotalTokens,
		}
	}

	return gemini
}

// NewEmitter returns a GeminiEmitter for streaming.
func (a *GeminiAdapter) NewEmitter(w http.ResponseWriter, model string) Emitter {
	return NewGeminiEmitter(w, model)
}

// =============================================================================
// Helpers for Gemini → Unified conversion
// =============================================================================

func convertGeminiContent(c GeminiContent) []UnifiedMsg {
	var toolCalls []UnifiedToolCall
	var toolResponses []GeminiFuncResp
	var texts []string

	for _, p := range c.Parts {
		if p.FunctionCall != nil {
			argsJSON, _ := json.Marshal(p.FunctionCall.Args)
			toolCalls = append(toolCalls, UnifiedToolCall{
				ID:        p.FunctionCall.ID,
				Name:      p.FunctionCall.Name,
				Arguments: string(argsJSON),
			})
		} else if p.FunctionResponse != nil {
			toolResponses = append(toolResponses, *p.FunctionResponse)
		} else if p.Text != "" {
			texts = append(texts, p.Text)
		}
	}

	// Tool responses first (they come before assistant tool_calls in Gemini format)
	if len(toolResponses) > 0 {
		var out []UnifiedMsg
		for _, fr := range toolResponses {
			respJSON, _ := json.Marshal(fr.Response)
			out = append(out, UnifiedMsg{
				Role:       "tool",
				ToolCallID: fr.ID,
				Content:    string(respJSON),
			})
		}
		return out
	}

	if len(toolCalls) > 0 {
		return []UnifiedMsg{{
			Role:      "assistant",
			ToolCalls: toolCalls,
		}}
	}

	role := c.Role
	switch role {
	case "model":
		role = "assistant"
	case "":
		role = "user"
	}
	return []UnifiedMsg{{
		Role:    role,
		Content: strings.Join(texts, ""),
	}}
}
