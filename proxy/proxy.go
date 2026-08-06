// Package proxy translates between AI agent API protocols and OpenAI Chat Completions.
// Supported agents: Claude Code (Anthropic Messages), Gemini CLI (Google Gemini),
// Codex CLI (OpenAI Responses), Cursor (OpenAI passthrough).
package proxy

import (
	"bytes"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pmdb "aipmc/db"
	"aipmc/u"
)

type ctxKey int

const ctxInjected ctxKey = iota

// proxyCfg holds all proxy configuration. It is stored in an atomic.Value
// so that handlers can read it without locking, and /__proxy/reload can swap
// it atomically.
type proxyCfg struct {
	upstreamURL          string
	proxyModel           string
	upstreamAnthropicURL string
	startTime            time.Time
	router               *ModelRouter
}

var cfg atomic.Value // stores *proxyCfg

func init() {
	cfg.Store(&proxyCfg{})
}

func loadCfg() *proxyCfg { return cfg.Load().(*proxyCfg) }

func storeCfg(c *proxyCfg) { cfg.Store(c) }

// Options configures the proxy server.
type Options struct {
	Port         int
	BindAddr     string
	UpstreamURL  string
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

// NewHandler creates an http.Handler for the proxy without starting a listener.
// This allows the proxy to be embedded in another HTTP server (e.g., the web UI).
// The returned handler serves agent request routing + /__proxy/* inspection endpoints.
func NewHandler(opts Options) http.Handler {
	u := strings.TrimRight(opts.UpstreamURL, "/")
	if u == "" {
		u = "http://localhost:8080/v1"
	}
	storeCfg(&proxyCfg{
		upstreamURL:          u,
		
		proxyModel:           opts.Model,
		upstreamAnthropicURL: strings.TrimRight(opts.AnthropicURL, "/"),
		startTime:            time.Now(),
			router:               NewModelRouter(),
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/__proxy/status", handleProxyStatus)
	mux.HandleFunc("/__proxy/traffic", handleProxyTraffic)
	mux.HandleFunc("/__proxy/capture", handleCaptureList)
	mux.HandleFunc("/__proxy/capture/clear", handleCaptureClear)
	mux.HandleFunc("/__proxy/inspect", handleInspectPage)
	mux.HandleFunc("/__proxy/reload", handleProxyReload)
		mux.HandleFunc("/__proxy/models/reload", handleModelsReload)
	mux.HandleFunc("/__proxy/tokens", handleTokenUsage)
	mux.HandleFunc("/", handler)
	return mux
}

// Run starts the proxy HTTP server on its own port. It blocks until the server
// exits. For embedding the proxy in another server, use NewHandler.
func Run(opts Options) error {
	h := NewHandler(opts)

	bindAddr := opts.BindAddr
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", bindAddr, opts.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("proxy listen %s: %w", addr, err)
	}

	// Write PID file after successful Listen
	pidPath := pidFilePath(opts.Port)
	os.MkdirAll(filepath.Dir(pidPath), 0755)
	os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644)
	defer os.Remove(pidPath)

	log.Printf("[PROXY] listening on %s, upstream=%s", addr, loadCfg().upstreamURL)
	return http.Serve(ln, h)
}

func pidFilePath(port int) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aipmc", fmt.Sprintf("proxy-%d.pid", port))
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
	case strings.Contains(path, "/chat/completions"):
		return "opencode"
	case strings.Contains(path, "/v1/messages"):
		return "claude"
	default:
		return "cursor"
	}
}

func handleTokenUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		tokenUsageMu.Lock()
		tokenUsageLog = nil
		tokenUsageAgg = TokenUsageAggregate{}
		tokenUsageMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	entries, agg := TokenUsageSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"records":   entries,
		"aggregate": agg,
	})
}

func handleProxyStatus(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	count := reqCount
	errs := errCount
	mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"running":      true,
		"uptime":       time.Since(loadCfg().startTime).String(),
		"requests":     count,
		"errors":       errs,
		"upstream":     loadCfg().upstreamURL,
		"port":         strings.TrimPrefix(r.Host, ":"),
		"model_override": loadCfg().proxyModel,
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

func handleProxyReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Read GlobalConfig from disk and atomically swap proxy configuration.
	// This allows the web UI to change upstream/model without restarting the proxy.
	gcfg := pmdb.LoadGlobalConfig()
	u := strings.TrimRight(gcfg.UpstreamURL, "/")
	if u == "" {
		u = "http://localhost:8080/v1"
	}
	storeCfg(&proxyCfg{
		upstreamURL:          u,
		
		proxyModel:           gcfg.ProxyModel,
		upstreamAnthropicURL: strings.TrimRight(gcfg.AnthropicURL, "/"),
		startTime:            loadCfg().startTime, 
			router:               NewModelRouter(),
	})
	log.Printf("[PROXY] config reloaded upstream=%s", loadCfg().upstreamURL)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleModelsReload re-reads models.json and swaps the registry atomically.
func handleModelsReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	router := loadCfg().router
	if router == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "router not initialized"})
		return
	}
	if err := router.Reload(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	log.Printf("[PROXY] models.json reloaded")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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

// Flush implements http.Flusher by delegating to the embedded writer when it
// supports flushing. Without it, the passthrough handlers' `w.(http.Flusher)`
// assertion always fails in production (handler() passes rw everywhere), so
// the SSE Flush() becomes a dead no-op and streaming events go out in ~4KB
// net/http buffer bursts instead of one write per event.
func (rw *responseWrapper) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// effectiveModel returns the model to send upstream.
// The [1m] suffix is NOT stripped — DeepSeek (and other providers
// accessed via Anthropic-compatible APIs) use it as part of the model
// identifier, not as an Anthropic-specific context-window hint.
func effectiveModel(agentModel string) string {
	if pm := loadCfg().proxyModel; pm != "" {
		return pm
	}
	return agentModel
}

func handler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	agent := detectAgent(path)

	rw := &responseWrapper{ResponseWriter: w, status: 200}


	// Model command interception (&aipmc-model switch/auto/current/list)
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	var intercepted bool
	if bytes.Contains(body, []byte("&aipmc-model")) {
		intercepted = tryModelCommand(rw, r, agent, body, r.URL.Path)
	}

	if !intercepted {
		origLen := len(body)
		body = InjectSessionContext(body, agent)
		if len(body) != origLen {
			r = r.WithContext(context.WithValue(r.Context(), ctxInjected, true))
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

	switch {
	case strings.Contains(path, ":generateContent") && !strings.Contains(path, "stream"):
		handleUnifiedNonStream(rw, r, &GeminiAdapter{})
	case strings.Contains(path, ":streamGenerateContent"):
		handleUnifiedStream(rw, r, &GeminiAdapter{})
	case strings.Contains(path, ":countTokens"):
		handleCountTokens(rw, r)
	case path == "/v1/messages":
		router := loadCfg().router
		if router != nil && router.IsActive() {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				log.Printf("[PROXY] ERROR reading /v1/messages body: %v", err)
				http.Error(rw, "failed to read request body", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			model := peekModel(body)
			if model != "" && router.ShouldPassthrough(model) {
				handleAnthropicPassthrough(rw, r)
			} else {
				handleClaudeUnified(rw, r)
			}
		} else if loadCfg().upstreamAnthropicURL != "" {
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
		router := loadCfg().router
		model := peekModel(body)
		// currentModel override governs passthrough selection: when the user has
		// selected a model via Web UI / &aipmc-model, honor it over the body's
		// model so switching to a responses-capable model actually takes effect
		// even if the codex client still sends the default model in the body.
		if cm := loadCurrentModel("codex"); cm != "" {
			model = cm
		}
		if model != "" && router.ShouldPassthroughResponses(model) {
			handleResponsesPassthrough(rw, r)
		} else {
			handleCodexUnified(rw, r)
		}
	case path == "/v1/chat/completions":
		handleOpenAIChatPassthrough(rw, r)
	case path == "/v1/models" || path == "/models" || strings.HasPrefix(path, "/v1/"):
		handlePassthrough(rw, r)
	default:
		http.Error(rw, "not found", http.StatusNotFound)
	}

	} // end if !intercepted

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
	Thinking        *OpenAIThinking `json:"thinking,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxTokens       *int            `json:"max_tokens,omitempty"`
	TopP            *float64        `json:"top_p,omitempty"`
	ReasoningEffort *string         `json:"reasoning_effort,omitempty"`
	Stop            []string        `json:"stop,omitempty"`
	Stream          bool            `json:"stream"`
	Tools           []OpenAITool    `json:"tools,omitempty"`
	ToolChoice      any             `json:"tool_choice,omitempty"`
}

// OpenAIThinking maps to GLM/ZhipuAI's thinking control ({"type": "enabled"}).
type OpenAIThinking struct {
	Type string `json:"type"`
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
	CacheHitTokens   int `json:"prompt_cache_hit_tokens"`
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
	url := loadCfg().upstreamURL + path
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequest(r.Method, url, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	proxyReq.Header = r.Header
	if apiKey := extractAPIKey(r); apiKey != "" && proxyReq.Header.Get("Authorization") == "" {
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
	// When virtual model routing is active, return the virtual model list
	// instead of forwarding to the upstream.
	if router := loadCfg().router; router != nil && router.IsActive() {
		result := router.ListOpenAIModels()
		result["models"] = router.ListCodexModels()
		writeJSON(w, http.StatusOK, result)
		return
	}

	url := loadCfg().upstreamURL + "/models"
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}

	proxyReq, _ := http.NewRequest("GET", url, nil)
	proxyReq.Header = r.Header
	if apiKey := extractAPIKey(r); apiKey != "" && proxyReq.Header.Get("Authorization") == "" {
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

	// Peek at stream flag and model
	var peek struct {
		Stream bool   `json:"stream"`
		Model  string `json:"model"`
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
	agent := detectAgent(r.URL.Path)
	// Use per-agent override for capture display (same logic as resolveVirtualRoute).
	model := req.Model
	if cm := loadCurrentModel(agent); cm != "" {
		model = cm
	}

	// Convert to OpenAI format and send upstream
	openaiReq := unifiedToOpenAI(req)

	capID := startCapture(agent, r.Method, r.URL.Path, model, rawBody, copyHeaders(r), req)
	startTime := time.Now()

	respBody, err := forwardToUpstream("chat/completions", openaiReq, apiKey, req.VirtualModel, agent, r)
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

	// ── Yield detection (observation only) ──────────────────────────
	if len(openaiResp.Choices) > 0 && openaiResp.Choices[0].Message != nil {
		if text := extractMessageText(openaiResp.Choices[0].Message); text != "" {
			detectYieldSignal(text, agent)
		}
	}

	nativeResp := adapter.ConvertResponse(&openaiResp, model)
	responseJSON, _ := json.Marshal(nativeResp)
	writeJSON(w, http.StatusOK, nativeResp)

	finishCapture(capID, http.StatusOK, time.Since(startTime), nil, string(responseJSON), "")

	// Record token usage on both ring buffer and capture entry
	inTok, outTok := 0, 0
	if openaiResp.Usage != nil {
		inTok = openaiResp.Usage.PromptTokens
		outTok = openaiResp.Usage.CompletionTokens
		RecordTokenUsage(TokenUsageRecord{
			Agent:            agent,
			Model:            model,
			PromptTokens:     openaiResp.Usage.PromptTokens,
			CompletionTokens: openaiResp.Usage.CompletionTokens,
			CacheHitTokens:   openaiResp.Usage.CacheHitTokens,
		})
		SetCaptureTokens(capID, openaiResp.Usage.PromptTokens, openaiResp.Usage.CompletionTokens)
		SetCaptureCacheTokens(capID, openaiResp.Usage.CacheHitTokens, 0)
	}
	// Log LLM request/response to shared log (always, 0 when usage is nil)
	u.LogShared("LLM", "agent=%s model=%s in_tok=%d out_tok=%d injected=%s lat=%.1fs", agent, model, inTok, outTok, injectedFlag(r), time.Since(startTime).Seconds())

}
// handleUnifiedStream handles a streaming request using the unified pipeline:
//
//	Adapter.ParseRequest → unifiedToOpenAI → forwardToUpstreamStream →
//	parseUpstreamSSE → StreamNormalizer.Process → Emitter.Emit → Emitter.Done

// detectYieldSignal inspects the final assistant response for yield/finish
// keywords and logs a [YIELD] observation. Observation-only; no injection yet.
func detectYieldSignal(text, agent string) {
	if text == "" {
		return
	}
	lower := strings.ToLower(text)
	signals := []string{}
	kw := map[string][]string{
		"done":   {"done", "完成", "修复完毕", "修复完成", "已修复", "已解决", "已部署", "已提交", "已实现", "ready", "fixed", "resolved"},
		"review": {"审核", "review", "audit", "检查"},
		"plan":   {"plan", "方案", "建议", "实施", "下一步"},
		"sum":    {"总结", "汇总", "summary", "recap"},
	}
	for sig, words := range kw {
		for _, w := range words {
			if strings.Contains(lower, w) {
				signals = append(signals, sig)
				break
			}
		}
	}
	if len(signals) > 0 {
		u.LogShared("YIELD", "agent=%s signals=%v text_preview=%s",
			agent, signals, u.TruncateStr(strings.ReplaceAll(text, "\n", " "), 80))
	}
}

// extractMessageText extracts plain text from an OpenAI message Content field.
func extractMessageText(msg *OpenAIMessage) string {
	switch v := msg.Content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"]; ok {
					if s, ok := t.(string); ok {
						parts = append(parts, s)
					}
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

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
	agent := detectAgent(r.URL.Path)
	// Use per-agent override for capture display (same logic as resolveVirtualRoute).
	model := req.Model
	if cm := loadCurrentModel(agent); cm != "" {
		model = cm
	}


	openaiReq := unifiedToOpenAI(req)

	capID := startCapture(agent, r.Method, r.URL.Path, model, rawBody, copyHeaders(r), req)
	startTime := time.Now()
	cap := newStreamCapture()

	respBody, err := forwardToUpstreamStream("chat/completions", openaiReq, apiKey, req.VirtualModel, agent, r)
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
	var streamPromptTokens int
	var streamCompletionTokens int
	var streamCacheHitTokens int
	var streamCacheCreationTokens int
	status := http.StatusOK
	doneCalled := false

	for event := range parseUpstreamSSE(respBody) {
		cap.addEvent(event)

		if event.Type == StreamDone {
			if event.Usage != nil {
				totalTokens = event.Usage.TotalTokens
				streamPromptTokens = event.Usage.PromptTokens
				streamCompletionTokens = event.Usage.CompletionTokens
				streamCacheHitTokens = event.Usage.CacheHitTokens
				streamCacheCreationTokens = event.Usage.CacheCreationTokens
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

	// ── Yield detection for streaming responses ──────────────────
	if streamText := cap.responseText(); streamText != "" {
		detectYieldSignal(streamText, agent)
	}

	// Record token usage on both ring buffer and capture entry
	if streamPromptTokens > 0 || streamCompletionTokens > 0 {
		RecordTokenUsage(TokenUsageRecord{
			Agent:              agent,
			Model:              model,
			PromptTokens:       streamPromptTokens,
			CompletionTokens:   streamCompletionTokens,
			CacheHitTokens:     streamCacheHitTokens,
			CacheCreationTokens: streamCacheCreationTokens,
		})
		SetCaptureTokens(capID, streamPromptTokens, streamCompletionTokens)
		SetCaptureCacheTokens(capID, streamCacheHitTokens, streamCacheCreationTokens)
	}
	// Log LLM request/response to shared log
	u.LogShared("LLM", "agent=%s model=%s in_tok=%d out_tok=%d cache_hit=%d injected=%s lat=%.1fs", agent, model, streamPromptTokens, streamCompletionTokens, streamCacheHitTokens, injectedFlag(r), time.Since(startTime).Seconds())

}

func injectedFlag(r *http.Request) string {
	if v, _ := r.Context().Value(ctxInjected).(bool); v {
		return "Y"
	}
	return "N"
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

// handleOpenAIChatPassthrough forwards /v1/chat/completions requests to upstream
// with token-usage tracking.  Handles both non-streaming and streaming (SSE).
//
// Non-streaming:  forwardToUpstream → parse usage → write response
// Streaming:      forwardToUpstreamStream → scan SSE lines → extract usage from
//                 the last data chunk before [DONE] → record after stream ends
func handleOpenAIChatPassthrough(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	r.Body.Close()

	var reqBody map[string]any
	if err := json.Unmarshal(rawBody, &reqBody); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	apiKey := extractAPIKey(r)
	model, _ := reqBody["model"].(string)
	stream, _ := reqBody["stream"].(bool)
	agent := detectAgent(r.URL.Path)

	// ── Capture (for Proxy Inspector) ──
	capID := startCapture(agent, r.Method, r.URL.Path, model, rawBody, copyHeaders(r), nil)
	startTime := time.Now()

	if !stream {
		// ── Non-streaming ──
		respBytes, err := forwardToUpstream("chat/completions", reqBody, apiKey, "", agent, r)
		if err != nil {
			log.Printf("[OPENAICHAT] ERROR upstream: %v", err)
			finishCapture(capID, http.StatusBadGateway, time.Since(startTime), nil, "", "")
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		var oai OpenAIResponse
		// Count tokens and log (always, 0 when usage is nil)
		oaiInTok, oaiOutTok := 0, 0
		if json.Unmarshal(respBytes, &oai) == nil && oai.Usage != nil {
			oaiInTok = oai.Usage.PromptTokens
			oaiOutTok = oai.Usage.CompletionTokens
			RecordTokenUsage(TokenUsageRecord{
				Agent:            agent,
				Model:            model,
				PromptTokens:     oai.Usage.PromptTokens,
				CompletionTokens: oai.Usage.CompletionTokens,
				CacheHitTokens:   oai.Usage.CacheHitTokens,
			})
			SetCaptureTokens(capID, oai.Usage.PromptTokens, oai.Usage.CompletionTokens)
			SetCaptureCacheTokens(capID, oai.Usage.CacheHitTokens, 0)
		}
		u.LogShared("LLM", "agent=%s model=%s in_tok=%d out_tok=%d injected=%s lat=%.1fs", agent, model, oaiInTok, oaiOutTok, injectedFlag(r), time.Since(startTime).Seconds())

		finishCapture(capID, http.StatusOK, time.Since(startTime), nil, string(respBytes), "")

		w.Header().Set("Content-Type", "application/json")
		w.Write(respBytes)
		return
	}

	// ── Streaming (SSE) ──
	respBody, err := forwardToUpstreamStream("chat/completions", reqBody, apiKey, "", agent, r)
	if err != nil {
		log.Printf("[OPENAICHAT] ERROR upstream stream: %v", err)
		finishCapture(capID, http.StatusBadGateway, time.Since(startTime), nil, "", "")
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer respBody.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)

	var lastUsage *OpenAIUsage
	var sseBuf strings.Builder
	scanner := bufio.NewScanner(respBody)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(w, line)
		sseBuf.WriteString(line + "\n")
		if flusher != nil {
			flusher.Flush()
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data != "[DONE]" {
				var chunk struct {
					Usage *OpenAIUsage `json:"usage,omitempty"`
				}
				if json.Unmarshal([]byte(data), &chunk) == nil && chunk.Usage != nil {
					lastUsage = chunk.Usage
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[OPENAICHAT] SSE scan error: %v", err)
	}

		inTok, outTok := 0, 0
		if lastUsage != nil {
			inTok = lastUsage.PromptTokens
			outTok = lastUsage.CompletionTokens
			RecordTokenUsage(TokenUsageRecord{
				Agent:            agent,
				Model:            model,
				PromptTokens:     lastUsage.PromptTokens,
				CompletionTokens: lastUsage.CompletionTokens,
				CacheHitTokens:   lastUsage.CacheHitTokens,
			})
			SetCaptureTokens(capID, lastUsage.PromptTokens, lastUsage.CompletionTokens)
			SetCaptureCacheTokens(capID, lastUsage.CacheHitTokens, 0)
		}
		// Log LLM request/response to shared log (always, 0 when usage is nil)
		u.LogShared("LLM", "agent=%s model=%s in_tok=%d out_tok=%d injected=%s lat=%.1fs", agent, model, inTok, outTok, injectedFlag(r), time.Since(startTime).Seconds())

	finishCapture(capID, http.StatusOK, time.Since(startTime), nil, sseBuf.String(), sseBuf.String())
}

