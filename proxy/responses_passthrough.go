package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"aipmc/u"
)

// buildResponsesPassthrough 构造透传目标 URL（{baseURL}/responses）并替换 body 的 model 字段。
func buildResponsesPassthrough(baseURL, realModel string, body []byte) (string, []byte) {
	return strings.TrimRight(baseURL, "/") + "/responses", replaceModelInBody(body, realModel)
}

// handleResponsesPassthrough 把 Codex 的原生 Responses body 直接转发到
// provider 的 {ResponsesURL}/responses 端点，字节级 SSE 透传，不做协议翻译。
func handleResponsesPassthrough(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	peekedModel := peekModel(body)
	if cm := loadCurrentModel("codex"); cm != "" {
		peekedModel = cm
		body = replaceModelInBody(body, cm)
	}

	var route *Route
	if router := loadCfg().router; router != nil && router.IsActive() {
		if peekedModel != "" {
			route = router.Resolve(peekedModel, "responses")
		}
	}
	if route != nil {
		body = replaceModelInBody(body, route.RealModel)
	} else if loadCfg().proxyModel != "" {
		body = replaceModelInBody(body, loadCfg().proxyModel)
	}

	effectiveModelName := route.RealModel
	if effectiveModelName == "" {
		effectiveModelName = loadCfg().proxyModel
	}
	if effectiveModelName == "" {
		effectiveModelName = peekedModel
	}

	agent := "codex"
	capID := startCapture(agent, r.Method, r.URL.Path, effectiveModelName, body, copyHeaders(r), nil)
	startTime := time.Now()

	targetURL := loadCfg().upstreamURL + "/responses"
	apiKey := ""
	if route != nil {
		targetURL = strings.TrimRight(route.BaseURL, "/") + "/responses"
		if route.APIKey != "" {
			apiKey = route.APIKey
		}
	}

	// 支持 streaming：优先字节级透传（上游 SSE → 下游 Codex 原样）
	if isStreamingResponses(body) {
		handleResponsesPassthroughStream(w, r, body, targetURL, apiKey, capID, effectiveModelName, startTime, agent)
		return
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		finishCapture(capID, http.StatusInternalServerError, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	proxyReq.Header = r.Header.Clone()
	if apiKey != "" {
		proxyReq.Header.Del("X-Api-Key")
		proxyReq.Header.Del("Authorization")
		proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		finishCapture(capID, http.StatusBadGateway, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		finishCapture(capID, resp.StatusCode, time.Since(startTime), nil, string(respBody), "")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
	finishCapture(capID, resp.StatusCode, time.Since(startTime), nil, string(respBody), "")

	if in, out := extractResponsesStreamUsage(string(respBody)); in > 0 || out > 0 {
		RecordTokenUsage(TokenUsageRecord{
			Agent:            agent,
			Model:            effectiveModelName,
			PromptTokens:     in,
			CompletionTokens: out,
		})
		SetCaptureTokens(capID, in, out)
		u.LogShared("LLM", "agent=codex model=%s in_tok=%d out_tok=%d lat=%.1fs", effectiveModelName, in, out, time.Since(startTime).Seconds())
	}
}

// isStreamingResponses 从 body 判断是否 streaming。
func isStreamingResponses(body []byte) bool {
	var peek struct {
		Stream bool `json:"stream"`
	}
	if json.Unmarshal(body, &peek) == nil {
		return peek.Stream
	}
	return false
}

// handleResponsesPassthroughStream 字节级 SSE 透传 streaming 响应。
func handleResponsesPassthroughStream(w http.ResponseWriter, r *http.Request, body []byte, targetURL, apiKey, capID string, model string, startTime time.Time, agent string) {
	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		finishCapture(capID, http.StatusInternalServerError, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	proxyReq.Header = r.Header.Clone()
	if apiKey != "" {
		proxyReq.Header.Del("X-Api-Key")
		proxyReq.Header.Del("Authorization")
		proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		finishCapture(capID, http.StatusBadGateway, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	var captureBuf bytes.Buffer
	tee := io.TeeReader(resp.Body, &captureBuf)
	buf := make([]byte, 4096)
	for {
		n, err := tee.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				// Client went away — stop reading the upstream stream.
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
	finishCapture(capID, resp.StatusCode, time.Since(startTime), nil, captureBuf.String(), "")
	if in, out := extractResponsesStreamUsage(captureBuf.String()); in > 0 || out > 0 {
		RecordTokenUsage(TokenUsageRecord{
			Agent:            agent,
			Model:            model,
			PromptTokens:     in,
			CompletionTokens: out,
		})
		SetCaptureTokens(capID, in, out)
		u.LogShared("LLM", "agent=codex model=%s in_tok=%d out_tok=%d lat=%.1fs", model, in, out, time.Since(startTime).Seconds())
	}
}
