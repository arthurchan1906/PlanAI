package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"aipmc/u"
)

// handleAnthropicPassthrough forwards Claude Code's Anthropic request directly
// to a DeepSeek Anthropic endpoint, bypassing the OpenAI translation pipeline.
// This preserves thinking/signature, tool_use input structure, budget_tokens
// granularity, and stop_reason semantics.
func handleAnthropicPassthrough(w http.ResponseWriter, r *http.Request) {
	// 1. Read original request body
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	// 2. currentModel override: when user has selected a model via Web UI/CLI,
	// replace the peeked model and body before routing.
	peekedModel := peekModel(body)
	if cm := loadCurrentModel("claude"); cm != "" {
		peekedModel = cm
		body = replaceModelInBody(body, cm)
	}

	// 3. Resolve virtual model routing, or use proxyModel fallback
	var route *Route
	if router := loadCfg().router; router != nil && router.IsActive() {
		if peekedModel != "" {
			route = router.Resolve(peekedModel, "anthropic")
		}
	}
	if route != nil {
		body = replaceModelInBody(body, route.RealModel)
	} else if loadCfg().proxyModel != "" {
		body = replaceModelInBody(body, loadCfg().proxyModel)
	}

	// 4. Determine effective model name for capture/token recording
	effectiveModelName := route.RealModel
	if effectiveModelName == "" {
		effectiveModelName = loadCfg().proxyModel
	}
	if effectiveModelName == "" {
		effectiveModelName = peekedModel
	}

	// 5. Start capture recording
	agent := "claude"
	capID := startCapture(agent, r.Method, r.URL.Path, effectiveModelName, body, copyHeaders(r), nil)
	startTime := time.Now()

	// 6. Build upstream request URL
	targetURL := loadCfg().upstreamAnthropicURL + "/v1/messages"
	apiKey := ""
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

	// 7. Copy request headers, scrub proxy-internal auth placeholder, set real upstream auth.
	// When the credential store has a key for this provider, use it — strip the old
	// placeholder auth headers and set the real key. When there is no credential-store
	// key, keep the original headers unchanged (the user may have set a real key
	// directly in the agent's environment).
	proxyReq.Header = r.Header.Clone()
	if apiKey != "" {
		proxyReq.Header.Del("X-Api-Key")
		proxyReq.Header.Del("Authorization")
		proxyReq.Header.Set("X-Api-Key", apiKey)
		proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// 8. Send request to upstream
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		finishCapture(capID, http.StatusBadGateway, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 9. Copy response headers to downstream (Claude Code needs Content-Type: text/event-stream)
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// 10. Stream SSE lines directly to Claude Code, capturing for inspector
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

	// 11. Complete capture with actual response body
	finishCapture(capID, resp.StatusCode, time.Since(startTime), nil, captureBuf.String(), "")

	// Record token usage from Anthropic SSE events
	if inputT, outputT, cacheHit, cacheCreate := extractAnthropicStreamUsage(captureBuf.String()); inputT > 0 || outputT > 0 || cacheHit > 0 {
		totalPrompt := inputT + cacheHit // Anthropic: input_tokens excludes cache reads
		RecordTokenUsage(TokenUsageRecord{
			Agent:              "claude",
			Model:              effectiveModelName,
			PromptTokens:       totalPrompt,
			CompletionTokens:   outputT,
			CacheHitTokens:     cacheHit,
			CacheCreationTokens: cacheCreate,
		})
		SetCaptureTokens(capID, totalPrompt, outputT)
		SetCaptureCacheTokens(capID, cacheHit, cacheCreate)
		// Log LLM request/response to shared log
		u.LogShared("LLM", "agent=claude model=%s in_tok=%d out_tok=%d cache_hit=%d cache_create=%d injected=%s lat=%.1fs", effectiveModelName, inputT+cacheHit, outputT, cacheHit, cacheCreate, injectedFlag(r), time.Since(startTime).Seconds())
	}
}
