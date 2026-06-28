// Package proxy translates between AI agent API protocols and OpenAI Chat Completions.
// Supported agents: Claude Code (Anthropic Messages), Gemini CLI (Google Gemini),
// Codex CLI (OpenAI Responses), Cursor (OpenAI passthrough).
package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var upstreamURL string
var upstreamKey string
var proxyModel string
var proxyLogDir string
var upstreamAnthropicURL string
var startTime time.Time

// Options configures the proxy server.
type Options struct {
	Port         int
	UpstreamURL  string
	UpstreamKey  string
	Model        string
	LogDir       string
	AnthropicURL string
}

type trafficEntry struct {
	Time   string `json:"time"`
	Agent  string `json:"agent"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	Size   int    `json:"size"`
}

var (
	mu          sync.Mutex
	reqCount    int
	errCount    int
	trafficLog  []trafficEntry
	maxTraffic  = 500
)

// Run starts the proxy HTTP server.
func Run(opts Options) {
	upstreamURL = strings.TrimRight(opts.UpstreamURL, "/")
	if upstreamURL == "" {
		upstreamURL = "http://localhost:8080/v1"
	}
	upstreamKey = opts.UpstreamKey
	proxyModel = opts.Model
	proxyLogDir = opts.LogDir
	upstreamAnthropicURL = strings.TrimRight(opts.AnthropicURL, "/")
	startTime = time.Now()

	port := fmt.Sprintf("%d", opts.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/__proxy/status", handleProxyStatus)
	mux.HandleFunc("/__proxy/traffic", handleProxyTraffic)
	mux.HandleFunc("/__proxy/capture", handleCaptureList)
	mux.HandleFunc("/__proxy/capture/clear", handleCaptureClear)
	mux.HandleFunc("/__proxy/inspect", handleInspectPage)
	mux.HandleFunc("/", handler)

	addr := ":" + port
	log.Printf("[PROXY] starting on %s, upstream=%s", addr, upstreamURL)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[PROXY] server error: %v", err)
	}
}

func recordTraffic(agent, method, path string, status, size int) {
	mu.Lock()
	defer mu.Unlock()
	reqCount++
	if status >= 400 {
		errCount++
	}
	trafficLog = append(trafficLog, trafficEntry{
		Time:   time.Now().Format("15:04:05"),
		Agent:  agent,
		Method: method,
		Path:   path,
		Status: status,
		Size:   size,
	})
	if len(trafficLog) > maxTraffic {
		trafficLog = trafficLog[len(trafficLog)-maxTraffic:]
	}
}

func detectAgent(path string) string {
	switch {
	case strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent"):
		return "gemini"
	case strings.Contains(path, "/v1/responses"):
		return "codex"
	case strings.Contains(path, "/v1/messages"):
		return "claude"
	default:
		return "cursor"
	}
}

func handleProxyStatus(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	count := reqCount
	errs := errCount
	mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"running":      true,
		"uptime":       time.Since(startTime).String(),
		"requests":     count,
		"errors":       errs,
		"upstream":     upstreamURL,
		"port":         strings.TrimPrefix(r.Host, ":"),
		"model_override": proxyModel,
	})
}

func handleProxyTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		mu.Lock()
		trafficLog = nil
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	mu.Lock()
	entries := make([]trafficEntry, len(trafficLog))
	copy(entries, trafficLog)
	mu.Unlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Time > entries[j].Time })
	writeJSON(w, http.StatusOK, map[string]any{"traffic": entries})
}

type responseWrapper struct {
	http.ResponseWriter
	status int
	size   int
}

func (rw *responseWrapper) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWrapper) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.size += n
	return n, err
}

func effectiveModel(agentModel string) string {
	if proxyModel != "" {
		return proxyModel
	}
	return agentModel
}

func handler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	agent := detectAgent(path)
	log.Printf("→ %s %s", r.Method, path)

	rw := &responseWrapper{ResponseWriter: w, status: 200}

	switch {
	case strings.Contains(path, ":generateContent") && !strings.Contains(path, "stream"):
		handleUnifiedNonStream(rw, r, &GeminiAdapter{})
	case strings.Contains(path, ":streamGenerateContent"):
		handleUnifiedStream(rw, r, &GeminiAdapter{})
	case strings.Contains(path, ":countTokens"):
		handleCountTokens(rw, r)
	case path == "/v1/messages":
		if upstreamAnthropicURL != "" {
			handleAnthropicPassthrough(rw, r)
		} else {
			handleClaudeUnified(rw, r)
		}
	case path == "/v1/responses" || path == "/responses":
		if r.Method == "GET" {
			if strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
				http.Error(rw, "websocket not supported", http.StatusBadRequest)
				return
			}
			writeJSON(rw, http.StatusOK, map[string]any{"object": "list", "data": []any{}})
			return
		}
		handleCodexUnified(rw, r)
	case path == "/v1/models" || path == "/models" || strings.HasPrefix(path, "/v1/"):
		handlePassthrough(rw, r)
	default:
		log.Printf("404: %s %s", r.Method, path)
		http.Error(rw, "not found", http.StatusNotFound)
	}

	recordTraffic(agent, r.Method, path, rw.status, rw.size)
}

// =============================================================================
// Request translation: Gemini → OpenAI
// =============================================================================

type GeminiRequest struct {
	Contents          []GeminiContent `json:"contents,omitempty"`
	Config            *GeminiConfig   `json:"config,omitempty"`
	SystemInstruction *GeminiContent  `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenConfig      `json:"generationConfig,omitempty"`
	Tools             []GeminiTool    `json:"tools,omitempty"`
	SafetySettings    []any           `json:"safetySettings,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts,omitempty"`
}

type GeminiPart struct {
	Text             string          `json:"text,omitempty"`
	FunctionCall     *GeminiFuncCall `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFuncResp `json:"functionResponse,omitempty"`
	InlineData       *GeminiInline   `json:"inlineData,omitempty"`
	FileData         *GeminiFileData `json:"fileData,omitempty"`
	Thought          string          `json:"thought,omitempty"`
}

type GeminiFuncCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type GeminiFuncResp struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type GeminiInline struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type GeminiFileData struct {
	MimeType string `json:"mimeType"`
	FileUri  string `json:"fileUri"`
}

type GeminiConfig struct {
	SystemInstruction *GeminiContent `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenConfig     `json:"generationConfig,omitempty"`
	Tools             []GeminiTool   `json:"tools,omitempty"`
}

type GenConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	TopK            *float64 `json:"topK,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
	CandidateCount  *int     `json:"candidateCount,omitempty"`
	Seed            *int     `json:"seed,omitempty"`
}

type GeminiTool struct {
	FunctionDeclarations []GeminiFuncDecl `json:"functionDeclarations,omitempty"`
	CodeExecution        any              `json:"codeExecution,omitempty"`
	GoogleSearch         any              `json:"googleSearch,omitempty"`
}

type GeminiFuncDecl struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parametersJsonSchema,omitempty"`
}

// OpenAI request structures
type OpenAIRequest struct {
	Model           string          `json:"model"`
	Messages        []OpenAIMessage `json:"messages"`
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxTokens       *int            `json:"max_tokens,omitempty"`
	TopP            *float64        `json:"top_p,omitempty"`
	ReasoningEffort *string         `json:"reasoning_effort,omitempty"`
	Stop            []string        `json:"stop,omitempty"`
	Stream          bool            `json:"stream"`
	Tools           []OpenAITool    `json:"tools,omitempty"`
	ToolChoice      any             `json:"tool_choice,omitempty"`
}

type OpenAIMessage struct {
	Role             string           `json:"role"`
	Content          any              `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}

type OpenAIToolCall struct {
	ID       string                `json:"id"`
	Type     string                `json:"type"`
	Function OpenAIToolCallFunction `json:"function"`
}

type OpenAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type OpenAITool struct {
	Type     string         `json:"type"`
	Function OpenAIFuncDecl `json:"function"`
}

type OpenAIFuncDecl struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

func getSystemInstruction(g *GeminiRequest) *GeminiContent {
	if g.Config != nil && g.Config.SystemInstruction != nil {
		return g.Config.SystemInstruction
	}
	return g.SystemInstruction
}

func getGenerationConfig(g *GeminiRequest) *GenConfig {
	if g.Config != nil && g.Config.GenerationConfig != nil {
		return g.Config.GenerationConfig
	}
	return g.GenerationConfig
}

func getTools(g *GeminiRequest) []GeminiTool {
	if g.Config != nil && g.Config.Tools != nil {
		return g.Config.Tools
	}
	return g.Tools
}

func extractText(c *GeminiContent) string {
	var texts []string
	for _, p := range c.Parts {
		if p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// =============================================================================
// Response translation: OpenAI → Gemini
// =============================================================================

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *OpenAIUsage   `json:"usage,omitempty"`
}

type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      *OpenAIMessage `json:"message,omitempty"`
	Delta        *OpenAIMessage `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason,omitempty"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type GeminiResponse struct {
	Candidates     []GeminiCandidate `json:"candidates,omitempty"`
	UsageMetadata  *GeminiUsage      `json:"usageMetadata,omitempty"`
	ModelVersion   string            `json:"modelVersion,omitempty"`
	ResponseID     string            `json:"responseId,omitempty"`
	PromptFeedback any               `json:"promptFeedback,omitempty"`
}

type GeminiCandidate struct {
	Content      *GeminiContent `json:"content,omitempty"`
	FinishReason string         `json:"finishReason,omitempty"`
	Index        int            `json:"index,omitempty"`
}

type GeminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

func mapFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "STOP"
	case "length":
		return "MAX_TOKENS"
	case "tool_calls":
		return "TOOL_CALLS"
	case "content_filter":
		return "SAFETY"
	default:
		return "STOP"
	}
}

// =============================================================================
// HTTP handlers
// =============================================================================

type streamToolCall struct {
	ID        string
	Name      string
	Arguments string
	itemID    string
	outputIdx int
}

func handleCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	tokenCount := len(body) / 4
	if tokenCount < 1 {
		tokenCount = 1
	}

	resp := map[string]any{
		"totalTokens":           tokenCount,
		"cachedContentTokenCount": 0,
	}
	writeJSON(w, http.StatusOK, resp)
}

func handlePassthrough(w http.ResponseWriter, r *http.Request) {
	if (r.URL.Path == "/v1/models" || r.URL.Path == "/models") && r.Method == "GET" {
		handleModelsList(w, r)
		return
	}

	path := r.URL.Path
	path = strings.TrimPrefix(path, "/v1")
	url := upstreamURL + path
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequest(r.Method, url, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	proxyReq.Header = r.Header
	// When upstreamKey is configured, always use it (ignore incoming auth)
	if upstreamKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+upstreamKey)
	} else if apiKey := extractAPIKey(r); apiKey != "" && proxyReq.Header.Get("Authorization") == "" {
		proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
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
	io.Copy(w, resp.Body)
}

// handleModelsList transforms the upstream model list into a format compatible
// with both standard OpenAI clients and Codex (which requires "slug" fields).
func handleModelsList(w http.ResponseWriter, r *http.Request) {
	url := upstreamURL + "/models"
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}

	proxyReq, _ := http.NewRequest("GET", url, nil)
	proxyReq.Header = r.Header
	// When upstreamKey is configured, always use it (ignore incoming auth)
	if upstreamKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+upstreamKey)
	} else if apiKey := extractAPIKey(r); apiKey != "" && proxyReq.Header.Get("Authorization") == "" {
		proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Parse upstream response (OpenAI standard format or llama.cpp format)
	var upstream struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &upstream); err != nil || len(upstream.Data) == 0 {
		// Fallback: return upstream response as-is
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	// Build Codex-compatible model list from upstream models
	type reasoningLevel struct {
		Effort      string `json:"effort"`
		Description string `json:"description"`
	}
	type codexModel struct {
		Slug                      string           `json:"slug"`
		DisplayName               string           `json:"display_name"`
		Description               string           `json:"description"`
		ShellType                 string           `json:"shell_type"`
		Visibility                string           `json:"visibility"`
		SupportedInAPI            bool             `json:"supported_in_api"`
		Priority                  int              `json:"priority"`
		BaseInstructions          string           `json:"base_instructions"`
		SupportsReasoningSummary  bool             `json:"supports_reasoning_summaries"`
		DefaultReasoningSummary   string           `json:"default_reasoning_summary"`
		DefaultReasoningLevel     *string          `json:"default_reasoning_level"`
		SupportedReasoningLevels  []reasoningLevel `json:"supported_reasoning_levels"`
		SupportVerbosity          bool             `json:"support_verbosity"`
		TruncationPolicy          struct {
			Mode  string `json:"mode"`
			Limit int    `json:"limit"`
		} `json:"truncation_policy"`
		SupportsParallelToolCalls  bool     `json:"supports_parallel_tool_calls"`
		SupportsSearchTool         bool     `json:"supports_search_tool"`
		WebSearchToolType          string   `json:"web_search_tool_type"`
		ExperimentalSupportedTools []string `json:"experimental_supported_tools"`
	}

	var codexModels []codexModel
	for i, m := range upstream.Data {
		slug := modelIDToSlug(m.ID)
		cm := codexModel{
			Slug:                     slug,
			DisplayName:              slug,
			Description:              fmt.Sprintf("Local model (%s)", m.OwnedBy),
			ShellType:                "local",
			Visibility:               "list",
			SupportedInAPI:           true,
			Priority:                 i,
			BaseInstructions:         "",
			SupportsReasoningSummary: false,
			DefaultReasoningSummary:  "none",
			DefaultReasoningLevel:    func() *string { s := "medium"; return &s }(),
			SupportedReasoningLevels: []reasoningLevel{
				{Effort: "low", Description: "Low effort"},
				{Effort: "medium", Description: "Medium effort"},
				{Effort: "high", Description: "High effort"},
			},
			SupportVerbosity: false,
			TruncationPolicy: struct {
				Mode  string `json:"mode"`
				Limit int    `json:"limit"`
			}{Mode: "tokens", Limit: 131072},
			SupportsParallelToolCalls:  true,
			SupportsSearchTool:         false,
			WebSearchToolType:          "text",
			ExperimentalSupportedTools: []string{},
		}
		codexModels = append(codexModels, cm)
	}

	// Return merged response: both Codex format (models) and OpenAI format (data)
	merged := map[string]any{
		"object": upstream.Object,
		"data":   upstream.Data,
		"models": codexModels,
	}
	writeJSON(w, http.StatusOK, merged)
}

// modelIDToSlug converts a model ID to a display slug.
func modelIDToSlug(id string) string {
	// e.g. "/Users/dazsec/llms/Qwen3.5-9B-Q4_K_M.gguf" → "qwen3.5-9b"
	name := id
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.TrimSuffix(name, ".gguf")
	return strings.ToLower(name)
}

// =============================================================================
// Helpers
// =============================================================================

func extractAPIKey(r *http.Request) string {
	if upstreamKey != "" {
		return upstreamKey
	}
	if key := r.Header.Get("x-goog-api-key"); key != "" {
		return key
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func extractModel(path string) string {
	parts := strings.SplitN(path, "/models/", 2)
	if len(parts) < 2 {
		return "unknown"
	}
	modelPart := parts[1]
	if idx := strings.IndexAny(modelPart, ":?"); idx >= 0 {
		return modelPart[:idx]
	}
	return modelPart
}

// handleCodexUnified dispatches Codex requests to stream or non-stream handler
// based on the "stream" field in the request body.
func handleCodexUnified(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	// Peek at stream flag
	var peek struct {
		Stream bool `json:"stream"`
	}
	json.Unmarshal(body, &peek)

	// Reconstruct body for the adapter
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	adapter := &CodexAdapter{}
	if peek.Stream {
		handleUnifiedStream(w, r, adapter)
	} else {
		handleUnifiedNonStream(w, r, adapter)
	}
}

func handleClaudeUnified(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	var peek struct {
		Stream bool `json:"stream"`
	}
	json.Unmarshal(body, &peek)
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	adapter := &ClaudeAdapter{}
	if peek.Stream {
		handleUnifiedStream(w, r, adapter)
	} else {
		handleUnifiedNonStream(w, r, adapter)
	}
}

// forwardToUpstream and forwardToUpstreamStream are now in upstream.go

// =============================================================================
// Unified handler — new architecture (progressively replacing old handlers)
// =============================================================================

// handleUnifiedNonStream handles a non-streaming request using the unified pipeline:
//
//	Adapter.ParseRequest → unifiedToOpenAI → forwardToUpstream → NormalizeResponse → Adapter.ConvertResponse → JSON
func handleUnifiedNonStream(w http.ResponseWriter, r *http.Request, adapter ProtocolAdapter) {
	rawBody, _ := io.ReadAll(r.Body)
	r.Body.Close()
	r.Body = io.NopCloser(strings.NewReader(string(rawBody)))

	req, err := adapter.ParseRequest(r)
	if err != nil {
		log.Printf("[UNIFIED] ERROR parsing request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	apiKey := extractAPIKey(r)
	model := req.Model
	agent := detectAgent(r.URL.Path)
	log.Printf("[UNIFIED] → non-stream  model=%s", model)

	// Convert to OpenAI format and send upstream
	openaiReq := unifiedToOpenAI(req)

	capID := startCapture(agent, r.Method, r.URL.Path, model, rawBody, copyHeaders(r), req)
	startTime := time.Now()

	respBody, err := forwardToUpstream("chat/completions", openaiReq, apiKey)
	if err != nil {
		log.Printf("[UNIFIED] ERROR upstream: %v", err)
		finishCapture(capID, http.StatusBadGateway, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	var openaiResp OpenAIResponse
	if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		log.Printf("[UNIFIED] ERROR parsing upstream response: %v", err)
		finishCapture(capID, http.StatusInternalServerError, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Normalize (model compat) then convert to native protocol
	NormalizeResponse(&openaiResp)
	nativeResp := adapter.ConvertResponse(&openaiResp, model)
	responseJSON, _ := json.Marshal(nativeResp)
	writeJSON(w, http.StatusOK, nativeResp)

	finishCapture(capID, http.StatusOK, time.Since(startTime), nil, string(responseJSON), "")

	log.Printf("[UNIFIED] ← complete  model=%s", model)
}

// handleUnifiedStream handles a streaming request using the unified pipeline:
//
//	Adapter.ParseRequest → unifiedToOpenAI → forwardToUpstreamStream →
//	parseUpstreamSSE → StreamNormalizer.Process → Emitter.Emit → Emitter.Done
func handleUnifiedStream(w http.ResponseWriter, r *http.Request, adapter ProtocolAdapter) {
	rawBody, _ := io.ReadAll(r.Body)
	r.Body.Close()
	r.Body = io.NopCloser(strings.NewReader(string(rawBody)))

	req, err := adapter.ParseRequest(r)
	if err != nil {
		log.Printf("[UNIFIED] ERROR parsing request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req.Stream = true
	apiKey := extractAPIKey(r)
	model := req.Model
	agent := detectAgent(r.URL.Path)
	log.Printf("[UNIFIED] → stream  model=%s", model)

	openaiReq := unifiedToOpenAI(req)

	capID := startCapture(agent, r.Method, r.URL.Path, model, rawBody, copyHeaders(r), req)
	startTime := time.Now()
	cap := newStreamCapture()

	respBody, err := forwardToUpstreamStream("chat/completions", openaiReq, apiKey)
	if err != nil {
		log.Printf("[UNIFIED] ERROR upstream stream: %v", err)
		finishCapture(capID, http.StatusBadGateway, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer respBody.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	emitter := adapter.NewEmitter(w, model)
	normalizer := &StreamNormalizer{}
	var totalTokens int
	var finishReason string
	status := http.StatusOK
	doneCalled := false

	for event := range parseUpstreamSSE(respBody) {
		cap.addEvent(event)

		if event.Type == StreamDone {
			if event.Usage != nil {
				totalTokens = event.Usage.TotalTokens
			}
			finishReason = event.FinishReason
		}

		if event.Type == StreamError {
			log.Printf("[UNIFIED] stream error: %s", event.Delta)
			status = http.StatusBadGateway
			emitter.Emit(event)
			emitter.Done("error", &UnifiedUsage{})
			doneCalled = true
			break
		}

		for _, normalized := range normalizer.Process(event) {
			emitter.Emit(normalized)
		}
	}

	if !doneCalled {
		if finishReason == "" {
			finishReason = "STOP"
		}
		emitter.Done(finishReason, &UnifiedUsage{TotalTokens: totalTokens})
	}

	finishCapture(capID, status, time.Since(startTime), nil, cap.responseText(), cap.eventsJSON())

	log.Printf("[UNIFIED] ← stream complete  model=%s  tokens=%d", model, totalTokens)
}

func firstN[T any](slice []T, n int) []T {
	if len(slice) <= n {
		return slice
	}
	return slice[:n]
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
