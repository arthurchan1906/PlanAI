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
// Unified 閳?OpenAI Chat Completions conversion
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
	// GLM/ZhipuAI requires thinking:{"type":"enabled"} for reasoning.
	// Send it whenever reasoning is requested (DeepSeek ignores unknown fields).
	if req.ReasoningEffort != nil {
		openai.Thinking = &OpenAIThinking{Type: "enabled"}
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
		defaultMax := 65536
		openai.MaxTokens = &defaultMax
	}

	return openai
}

// =============================================================================
// HTTP transport 閳?send requests to upstream LLM
// =============================================================================

// resolveVirtualRoute applies virtual model routing to the request URL, body, and API key.
// It modifies url, bodyJSON, and apiKey in-place. When virtual routing is inactive
// or the model is unknown, the parameters are left unchanged.
func resolveVirtualRoute(virtualModel, endpoint, agent string, bodyJSON []byte, apiKey *string) (url string, body []byte) {
	url = loadCfg().upstreamURL + "/" + endpoint
	body = bodyJSON

	// currentModel override: when a user has selected a model via Web UI or CLI,
	// force all requests for this agent to use that model.
	// Empty ("") means Auto mode 鈥?passthrough, no override.
	if cm := loadCurrentModel(agent); cm != "" {
		log.Printf("[RESOLVE] agent=%q override %q 鈫?%q", agent, virtualModel, cm)
		virtualModel = cm
		body = replaceModelInBody(bodyJSON, cm)
	}
	if virtualModel == "" {
		return
	}
	router := loadCfg().router
	if router == nil || !router.IsActive() {
		return
	}
	route := router.Resolve(virtualModel, "openai")
	if route == nil {
		return
	}
	url = strings.TrimRight(route.BaseURL, "/") + "/" + endpoint
	body = replaceModelInBody(bodyJSON, route.RealModel)
	if route.APIKey != "" {
		*apiKey = route.APIKey
	}
	return
}

// forwardToUpstream sends a non-streaming POST request to the upstream endpoint.
func forwardToUpstream(endpoint string, body any, apiKey string, virtualModel, agent string) ([]byte, error) {
	bodyJSON, _ := json.Marshal(body)
	url, bodyJSON := resolveVirtualRoute(virtualModel, endpoint, agent, bodyJSON, &apiKey)

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
func forwardToUpstreamStream(endpoint string, body any, apiKey string, virtualModel, agent string) (io.ReadCloser, error) {
	bodyJSON, _ := json.Marshal(body)
	url, bodyJSON := resolveVirtualRoute(virtualModel, endpoint, agent, bodyJSON, &apiKey)

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

