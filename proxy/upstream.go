package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// =============================================================================
// Unified → OpenAI Chat Completions conversion
// =============================================================================

// unifiedToOpenAI converts a UnifiedReq to an OpenAI Chat Completions request.
// This is a pure data transformation; no model-specific logic lives here.
func unifiedToOpenAI(req *UnifiedReq) *OpenAIRequest {
	openai := &OpenAIRequest{
		Model:           effectiveModel(req.Model),
		Stream:          req.Stream,
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxTokens,
		TopP:            req.TopP,
		Stop:            req.Stop,
		ReasoningEffort: req.ReasoningEffort,
		ToolChoice:      req.ToolChoice,
	}

	// Convert messages
	for _, m := range req.Messages {
		omsg := OpenAIMessage{
			Role:             m.Role,
			ReasoningContent: m.Thinking,
			ToolCallID:       m.ToolCallID,
		}
		if m.Content != "" {
			omsg.Content = m.Content
		}
		for _, tc := range m.ToolCalls {
			omsg.ToolCalls = append(omsg.ToolCalls, OpenAIToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: OpenAIToolCallFunction{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}
		openai.Messages = append(openai.Messages, omsg)
	}

	// Convert tools
	for _, t := range req.Tools {
		openai.Tools = append(openai.Tools, OpenAITool{
			Type: "function",
			Function: OpenAIFuncDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	// Ensure max_tokens is never null (llama.cpp rejects null)
	if openai.MaxTokens == nil {
		defaultMax := 4096
		openai.MaxTokens = &defaultMax
	}

	return openai
}

// =============================================================================
// HTTP transport — send requests to upstream LLM
// =============================================================================

// forwardToUpstream sends a non-streaming POST request to the upstream endpoint.
func forwardToUpstream(endpoint string, body any, apiKey string) ([]byte, error) {
	bodyJSON, _ := json.Marshal(body)
	url := loadCfg().upstreamURL + "/" + endpoint

	req, _ := http.NewRequest("POST", url, strings.NewReader(string(bodyJSON)))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// forwardToUpstreamStream sends a streaming POST request to the upstream endpoint.
// The caller MUST close the returned body when done.
func forwardToUpstreamStream(endpoint string, body any, apiKey string) (io.ReadCloser, error) {
	bodyJSON, _ := json.Marshal(body)
	url := loadCfg().upstreamURL + "/" + endpoint

	req, _ := http.NewRequest("POST", url, strings.NewReader(string(bodyJSON)))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}
