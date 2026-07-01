package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// handleAnthropicPassthrough forwards Claude Code's Anthropic request directly
// to a DeepSeek Anthropic endpoint, bypassing the OpenAI translation pipeline.
// This preserves thinking/signature, tool_use input structure, budget_tokens
// granularity, and stop_reason semantics.
func handleAnthropicPassthrough(w http.ResponseWriter, r *http.Request) {
	// 1. Read original request body
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	// 2. Resolve virtual model routing, or use proxyModel fallback
	var route *Route
	if router := loadCfg().router; router != nil && router.IsActive() {
		model := peekModel(body)
		if model != "" {
			route = router.Resolve(model, "anthropic")
		}
	}
	if route != nil {
		body = replaceModelField(body, route.RealModel)
	} else if loadCfg().proxyModel != "" {
		body = replaceModelField(body, loadCfg().proxyModel)
	}

	// 3. Start capture recording
	agent := "claude"
	capID := startCapture(agent, r.Method, r.URL.Path, loadCfg().proxyModel, body, copyHeaders(r), nil)
	startTime := time.Now()

	// 4. Build upstream request URL
	targetURL := loadCfg().upstreamAnthropicURL + "/v1/messages"
	apiKey := loadCfg().upstreamKey
	if route != nil {
		targetURL = strings.TrimRight(route.BaseURL, "/") + "/v1/messages"
		if route.APIKey != "" {
			apiKey = route.APIKey
		}
	}
	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		finishCapture(capID, http.StatusInternalServerError, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 5. Copy request headers (keep Claude Code's headers, replace Authorization)
	proxyReq.Header = r.Header.Clone()
	if apiKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// 6. Send request to upstream
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		log.Printf("[ANTHROPIC_PASSTHROUGH] ERROR upstream: %v", err)
		finishCapture(capID, http.StatusBadGateway, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 7. Copy response headers to downstream (Claude Code needs Content-Type: text/event-stream)
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// 8. Stream SSE lines directly to Claude Code, capturing for inspector
	flusher, _ := w.(http.Flusher)
	var captureBuf bytes.Buffer
	tee := io.TeeReader(resp.Body, &captureBuf)
	buf := make([]byte, 4096)
	var totalBytes int
	for {
		n, err := tee.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			totalBytes += n
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}

	// 9. Complete capture with actual response body
	finishCapture(capID, resp.StatusCode, time.Since(startTime), nil, captureBuf.String(), "")

	// Record token usage from Anthropic SSE events
	if inputT, outputT, cacheHit, cacheCreate := extractAnthropicStreamUsage(captureBuf.String()); inputT > 0 || outputT > 0 || cacheHit > 0 {
		totalPrompt := inputT + cacheHit // Anthropic: input_tokens excludes cache reads
		RecordTokenUsage(TokenUsageRecord{
			Agent:              "claude",
			Model:              loadCfg().proxyModel,
			PromptTokens:       totalPrompt,
			CompletionTokens:   outputT,
			CacheHitTokens:     cacheHit,
			CacheCreationTokens: cacheCreate,
		})
		SetCaptureTokens(capID, totalPrompt, outputT)
		SetCaptureCacheTokens(capID, cacheHit, cacheCreate)
	}

	if resp.StatusCode >= 400 {
		log.Printf("[ANTHROPIC_PASSTHROUGH] upstream returned %d", resp.StatusCode)
	} else {
	}
}

// replaceModelField replaces the "model" field value in an Anthropic JSON request body.
// Uses lightweight string scanning — does not fully parse the JSON.
func replaceModelField(body []byte, model string) []byte {
	s := string(body)
	idx := strings.Index(s, `"model"`)
	if idx < 0 {
		return body
	}
	colonPos := strings.Index(s[idx:], ":")
	if colonPos < 0 {
		return body
	}
	colonPos += idx
	// Find the value start and end positions
	valStart := -1
	for i := colonPos + 1; i < len(s); i++ {
		if s[i] == '"' {
			valStart = i
			break
		}
	}
	if valStart < 0 {
		return body
	}
	valEnd := strings.Index(s[valStart+1:], `"`)
	if valEnd < 0 {
		return body
	}
	valEnd += valStart + 1
	return []byte(s[:valStart+1] + model + s[valEnd:])
}
