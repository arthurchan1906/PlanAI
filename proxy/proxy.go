// Package proxy translates between AI agent API protocols and OpenAI Chat Completions.
// Supported agents: Claude Code (Anthropic Messages), Gemini CLI (Google Gemini),
// Codex CLI (OpenAI Responses), Cursor (OpenAI passthrough).
package proxy

import (
	"bufio"
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
var startTime time.Time

// Options configures the proxy server.
type Options struct {
	Port        int
	UpstreamURL string
	UpstreamKey string
	Model       string
	LogDir      string
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
	startTime = time.Now()

	port := fmt.Sprintf("%d", opts.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/__proxy/status", handleProxyStatus)
	mux.HandleFunc("/__proxy/traffic", handleProxyTraffic)
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
		handleGenerateContent(rw, r)
	case strings.Contains(path, ":streamGenerateContent"):
		handleStreamGenerateContent(rw, r)
	case strings.Contains(path, ":countTokens"):
		handleCountTokens(rw, r)
	case path == "/v1/messages":
		handleAnthropicMessages(rw, r)
	case path == "/v1/responses" || path == "/responses":
		if r.Method == "GET" {
			if strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
				http.Error(rw, "websocket not supported", http.StatusBadRequest)
				return
			}
			writeJSON(rw, http.StatusOK, map[string]any{"object": "list", "data": []any{}})
			return
		}
		handleResponsesCreate(rw, r)
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

func geminiToOpenAI(gemini *GeminiRequest, model string) *OpenAIRequest {
	openai := &OpenAIRequest{
		Model:  effectiveModel(model),
		Stream: false,
	}

	var messages []OpenAIMessage

	// 1. System instruction
	sysInstr := getSystemInstruction(gemini)
	if sysInstr != nil {
		text := extractText(sysInstr)
		if text != "" {
			messages = append(messages, OpenAIMessage{Role: "system", Content: text})
		}
	}

	// 2. Conversation contents
	for _, c := range gemini.Contents {
		msgs := convertContent(c)
		messages = append(messages, msgs...)
	}

	openai.Messages = messages

	// 3. Generation config
	gc := getGenerationConfig(gemini)
	if gc != nil {
		openai.Temperature = gc.Temperature
		openai.MaxTokens = gc.MaxOutputTokens
		openai.TopP = gc.TopP
		openai.Stop = gc.StopSequences
	}
	if openai.MaxTokens == nil {
		defaultMaxTokens := 4096
		openai.MaxTokens = &defaultMaxTokens
	}

	// 4. Tools
	tools := getTools(gemini)
	for _, t := range tools {
		for _, fd := range t.FunctionDeclarations {
			openai.Tools = append(openai.Tools, OpenAITool{
				Type: "function",
				Function: OpenAIFuncDecl{
					Name:        fd.Name,
					Description: fd.Description,
					Parameters:  fd.Parameters,
				},
			})
		}
	}

	return openai
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

func convertContent(c GeminiContent) []OpenAIMessage {
	var toolCalls []OpenAIToolCall
	var toolResponses []GeminiFuncResp
	var texts []string

	for _, p := range c.Parts {
		if p.FunctionCall != nil {
			argsJSON, _ := json.Marshal(p.FunctionCall.Args)
			toolCalls = append(toolCalls, OpenAIToolCall{
				ID:   p.FunctionCall.ID,
				Type: "function",
				Function: OpenAIToolCallFunction{
					Name:      p.FunctionCall.Name,
					Arguments: string(argsJSON),
				},
			})
		} else if p.FunctionResponse != nil {
			toolResponses = append(toolResponses, *p.FunctionResponse)
		} else if p.Text != "" {
			texts = append(texts, p.Text)
		}
	}

	if len(toolResponses) > 0 {
		var out []OpenAIMessage
		for _, fr := range toolResponses {
			respJSON, _ := json.Marshal(fr.Response)
			out = append(out, OpenAIMessage{
				Role:       "tool",
				ToolCallID: fr.ID,
				Content:    string(respJSON),
			})
		}
		return out
	}

	if len(toolCalls) > 0 {
		return []OpenAIMessage{{
			Role:      "assistant",
			Content:   nil,
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
	return []OpenAIMessage{{
		Role:    role,
		Content: strings.Join(texts, ""),
	}}
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

func openaiToGemini(openai *OpenAIResponse, model string) *GeminiResponse {
	gemini := &GeminiResponse{
		ModelVersion: model,
	}

	for _, choice := range openai.Choices {
		msg := choice.Message
		if msg == nil {
			msg = choice.Delta
		}
		if msg == nil {
			continue
		}

		var parts []GeminiPart

		if msg.ReasoningContent != "" {
			parts = append(parts, GeminiPart{Thought: msg.ReasoningContent})
		}

		if msg.Content != nil {
			switch v := msg.Content.(type) {
			case string:
				if v != "" {
					clean := stripThinkTags(v)
					if clean != "" {
						parts = append(parts, GeminiPart{Text: clean})
					}
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

		hasText := false
		for _, p := range parts {
			if p.Text != "" {
				hasText = true
				break
			}
		}
		if !hasText && msg.ReasoningContent != "" {
			parts = append(parts, GeminiPart{Text: extractFinalAnswer(msg.ReasoningContent)})
		}

		// Parse Gemma tool calls from text
		var newParts []GeminiPart
		for _, p := range parts {
			if p.Text != "" {
				cleaned, toolParts := parseGemmaToolCalls(p.Text)
				if cleaned != "" {
					newParts = append(newParts, GeminiPart{Text: cleaned})
				}
				newParts = append(newParts, toolParts...)
			} else {
				newParts = append(newParts, p)
			}
		}
		parts = newParts

		for _, tc := range msg.ToolCalls {
			var args map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
			log.Printf("[FUNCTION_CALL←MODEL] name=%s args=%s id=%s", tc.Function.Name, tc.Function.Arguments, tc.ID)
			parts = append(parts, GeminiPart{
				FunctionCall: &GeminiFuncCall{
					ID:   tc.ID,
					Name: tc.Function.Name,
					Args: args,
				},
			})
		}

		finishReason := ""
		if choice.FinishReason != nil {
			finishReason = mapFinishReason(*choice.FinishReason)
		}

		gemini.Candidates = append(gemini.Candidates, GeminiCandidate{
			Content:      &GeminiContent{Role: "model", Parts: parts},
			FinishReason: finishReason,
			Index:        choice.Index,
		})
	}

	if openai.Usage != nil {
		gemini.UsageMetadata = &GeminiUsage{
			PromptTokenCount:     openai.Usage.PromptTokens,
			CandidatesTokenCount: openai.Usage.CompletionTokens,
			TotalTokenCount:      openai.Usage.TotalTokens,
		}
	}

	return gemini
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

func handleGenerateContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	model := extractModel(r.URL.Path)
	effModel := effectiveModel(model)
	log.Printf("→ generateContent  model=%s", effModel)

	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var geminiReq GeminiRequest
	if err := json.Unmarshal(body, &geminiReq); err != nil {
		log.Printf("ERROR parsing request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	apiKey := extractAPIKey(r)
	openaiReq := geminiToOpenAI(&geminiReq, model)
	resp, err := forwardToUpstream("chat/completions", openaiReq, apiKey)
	if err != nil {
		log.Printf("ERROR upstream: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	var openaiResp OpenAIResponse
	if err := json.Unmarshal(resp, &openaiResp); err != nil {
		log.Printf("ERROR parsing upstream response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	geminiResp := openaiToGemini(&openaiResp, model)
	writeJSON(w, http.StatusOK, geminiResp)
	log.Printf("← generateContent  model=%s  tokens=%d", effModel,
		geminiResp.UsageMetadata.TotalTokenCount)
}

func handleStreamGenerateContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	model := extractModel(r.URL.Path)
	effModel := effectiveModel(model)
	log.Printf("→ streamGenerateContent  model=%s", effModel)

	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var geminiReq GeminiRequest
	if err := json.Unmarshal(body, &geminiReq); err != nil {
		log.Printf("ERROR parsing request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	apiKey := extractAPIKey(r)
	openaiReq := geminiToOpenAI(&geminiReq, model)
	openaiReq.Stream = true

	respBody, err := forwardToUpstreamStream("chat/completions", openaiReq, apiKey)
	if err != nil {
		log.Printf("ERROR upstream stream: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer respBody.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	scanner := bufio.NewScanner(respBody)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	totalTokens := 0
	pendingFinish := ""
	thinkBuf := ""
	toolCallAcc := map[int]*streamToolCall{}

	emitChunk := func(gc *GeminiResponse) {
		b, _ := json.Marshal(gc)
		fmt.Fprintf(w, "data: %s\n\n", b)
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
			if thinkBuf != "" {
				clean, toolParts := parseGemmaToolCalls(thinkBuf)
				if clean != "" {
					emitChunk(&GeminiResponse{
						ModelVersion: model,
						Candidates:   []GeminiCandidate{{Content: &GeminiContent{Role: "model", Parts: []GeminiPart{{Text: clean}}}, Index: 0}},
					})
				}
				for _, tp := range toolParts {
					emitChunk(&GeminiResponse{
						ModelVersion: model,
						Candidates:   []GeminiCandidate{{Content: &GeminiContent{Role: "model", Parts: []GeminiPart{tp}}, Index: 0}},
					})
				}
				thinkBuf = ""
			}
			if len(toolCallAcc) > 0 {
				final := buildAccumulatedChunk(toolCallAcc, pendingFinish)
				emitChunk(final)
				toolCallAcc = map[int]*streamToolCall{}
			}
			finalChunk := map[string]any{
				"candidates": []map[string]any{{
					"content":      map[string]any{"role": "model", "parts": []any{}},
					"finishReason": pendingFinish,
					"index":        0,
				}},
				"usageMetadata": map[string]any{
					"promptTokenCount":     0,
					"candidatesTokenCount": totalTokens,
					"totalTokenCount":      totalTokens,
				},
			}
			if pendingFinish == "" {
				finalChunk["candidates"].([]map[string]any)[0]["finishReason"] = "STOP"
			}
			b, _ := json.Marshal(finalChunk)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
			continue
		}

		var rawChunk map[string]any
		if err := json.Unmarshal([]byte(data), &rawChunk); err != nil {
			continue
		}

		var textDelta string
		var thoughtDelta string
		if choices, ok := rawChunk["choices"].([]any); ok && len(choices) > 0 {
			choice := choices[0].(map[string]any)
			if delta, ok := choice["delta"].(map[string]any); ok {
				if c, ok := delta["content"]; ok && c != nil {
					if s, ok := c.(string); ok {
						textDelta = s
					}
				}
				if rc, ok := delta["reasoning_content"]; ok {
					if s, ok := rc.(string); ok {
						thoughtDelta = s
					}
				}
				if tcs, ok := delta["tool_calls"].([]any); ok {
					for _, tc := range tcs {
						tcMap := tc.(map[string]any)
						idx := int(tcMap["index"].(float64))
						if _, exists := toolCallAcc[idx]; !exists {
							toolCallAcc[idx] = &streamToolCall{}
						}
						acc := toolCallAcc[idx]
						if id, ok := tcMap["id"]; ok {
							acc.ID = id.(string)
						}
						if fn, ok := tcMap["function"].(map[string]any); ok {
							if name, ok := fn["name"]; ok {
								if s, ok := name.(string); ok && s != "" {
									acc.Name = s
								}
							}
							if args, ok := fn["arguments"]; ok {
								if s, ok := args.(string); ok {
									acc.Arguments += s
								}
							}
						}
					}
				}
				if fr, ok := choice["finish_reason"]; ok && fr != nil {
					if s, ok := fr.(string); ok {
						pendingFinish = s
					}
				}
			}
			if usage, ok := rawChunk["usage"].(map[string]any); ok {
				if tt, ok := usage["total_tokens"]; ok {
					if n, ok := tt.(float64); ok {
						totalTokens = int(n)
					}
				}
			}
		}

		geminiChunk := &GeminiResponse{ModelVersion: model}
		var parts []GeminiPart
		if thoughtDelta != "" {
			parts = append(parts, GeminiPart{Thought: thoughtDelta})
		}
		if textDelta != "" {
			thinkBuf += textDelta
			hasToolOpen := strings.Contains(thinkBuf, "<|tool_call>")
			hasToolClose := strings.Contains(thinkBuf, "<tool_call|>")
			hasThinkClose := strings.Contains(thinkBuf, "<channel|>") || strings.Contains(thinkBuf, "</think>")
			if hasToolOpen && hasToolClose {
				cleaned, toolParts := parseGemmaToolCalls(thinkBuf)
				if cleaned != "" {
					parts = append(parts, GeminiPart{Text: cleaned})
				}
				parts = append(parts, toolParts...)
				thinkBuf = ""
			} else if hasThinkClose {
				clean := stripThinkTags(thinkBuf)
				thinkBuf = ""
				if clean != "" {
					parts = append(parts, GeminiPart{Text: clean})
				}
			} else if hasToolOpen || (len(thinkBuf) > 0 && thinkBuf[0] == '<' && len(thinkBuf) < 3000) {
				// keep buffering
			} else {
				parts = append(parts, GeminiPart{Text: thinkBuf})
				thinkBuf = ""
			}
		}

		if pendingFinish == "tool_calls" && len(toolCallAcc) > 0 {
			for _, acc := range toolCallAcc {
				var args map[string]any
				json.Unmarshal([]byte(acc.Arguments), &args)
				log.Printf("[FUNCTION_CALL←MODEL STREAM] name=%s args=%s id=%s", acc.Name, acc.Arguments, acc.ID)
				parts = append(parts, GeminiPart{
					FunctionCall: &GeminiFuncCall{
						ID:   acc.ID,
						Name: acc.Name,
						Args: args,
					},
				})
			}
			geminiChunk.Candidates = []GeminiCandidate{{
				Content:      &GeminiContent{Role: "model", Parts: parts},
				FinishReason: "TOOL_CALLS",
				Index:        0,
			}}
			emitChunk(geminiChunk)
			toolCallAcc = map[int]*streamToolCall{}
			pendingFinish = ""
			continue
		}

		if len(toolCallAcc) > 0 && textDelta == "" && thoughtDelta == "" {
			continue
		}

		var finalParts []GeminiPart
		for _, p := range parts {
			if p.Text != "" {
				cleaned, toolParts := parseGemmaToolCalls(p.Text)
				if cleaned != "" {
					finalParts = append(finalParts, GeminiPart{Text: cleaned})
				}
				finalParts = append(finalParts, toolParts...)
			} else {
				finalParts = append(finalParts, p)
			}
		}

		geminiChunk.Candidates = []GeminiCandidate{{
			Content: &GeminiContent{Role: "model", Parts: finalParts},
			Index:   0,
		}}
		emitChunk(geminiChunk)
	}

	log.Printf("← streamGenerateContent  model=%s  tokens=%d", effModel, totalTokens)
}

type streamToolCall struct {
	ID        string
	Name      string
	Arguments string
	itemID    string
	outputIdx int
}

func buildAccumulatedChunk(acc map[int]*streamToolCall, finishReason string) *GeminiResponse {
	var parts []GeminiPart
	for i := 0; i < len(acc); i++ {
		if tc, ok := acc[i]; ok {
			var args map[string]any
			json.Unmarshal([]byte(tc.Arguments), &args)
			parts = append(parts, GeminiPart{
				FunctionCall: &GeminiFuncCall{
					ID:   tc.ID,
					Name: tc.Name,
					Args: args,
				},
			})
		}
	}
	fr := mapFinishReason(finishReason)
	return &GeminiResponse{
		Candidates: []GeminiCandidate{{
			Content:      &GeminiContent{Role: "model", Parts: parts},
			FinishReason: fr,
			Index:        0,
		}},
	}
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
	type codexModel struct {
		Slug                     string `json:"slug"`
		DisplayName              string `json:"display_name"`
		Description              string `json:"description"`
		ShellType                string `json:"shell_type"`
		Visibility               string `json:"visibility"`
		SupportedInAPI           bool   `json:"supported_in_api"`
		Priority                 int    `json:"priority"`
		BaseInstructions         string `json:"base_instructions"`
		SupportsReasoningSummary bool   `json:"supports_reasoning_summaries"`
		DefaultReasoningSummary  string `json:"default_reasoning_summary"`
		SupportVerbosity         bool   `json:"support_verbosity"`
		TruncationPolicy         struct {
			Mode  string `json:"mode"`
			Limit int    `json:"limit"`
		} `json:"truncation_policy"`
		SupportsParallelToolCalls bool `json:"supports_parallel_tool_calls"`
		WebSearchToolType         string `json:"web_search_tool_type"`
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
			SupportVerbosity:         false,
			TruncationPolicy: struct {
				Mode  string `json:"mode"`
				Limit int    `json:"limit"`
			}{Mode: "tokens", Limit: 131072},
			SupportsParallelToolCalls: true,
			WebSearchToolType:         "text",
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

// modelIDToSlug converts a llama.cpp model path to a short slug.
func modelIDToSlug(id string) string {
	// e.g. "/Users/dazsec/llms/Qwen3.5-9B-Q4_K_M.gguf" → "qwen3.5-9b"
	name := id
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.TrimSuffix(name, ".gguf")
	// Take first two segments (model name + variant)
	parts := strings.Split(name, "-")
	if len(parts) >= 2 {
		name = strings.ToLower(parts[0] + "-" + parts[1])
	}
	return strings.ToLower(name)
}

// =============================================================================
// Gemma 4 tool call parser
// =============================================================================

func parseGemmaToolCalls(text string) (cleaned string, toolParts []GeminiPart) {
	for {
		start := strings.Index(text, "<|tool_call>")
		if start < 0 {
			break
		}
		end := strings.Index(text[start:], "<tool_call|>")
		if end < 0 {
			break
		}
		end += start
		raw := text[start+12:]
		cleaned = text[:start]
		text = text[end+12:]

		raw = strings.TrimPrefix(raw, "call:")
		bracePos := strings.Index(raw, "{")
		if bracePos < 0 {
			continue
		}
		name := strings.TrimSpace(raw[:bracePos])
		if idx := strings.LastIndex(name, ":"); idx >= 0 && idx < len(name)-1 {
			name = name[idx+1:]
		}
		argsStr := raw[bracePos:]
		args := parseGemmaArgs(argsStr)
		toolParts = append(toolParts, GeminiPart{
			FunctionCall: &GeminiFuncCall{
				Name: name,
				Args: args,
			},
		})
	}
	cleaned += text
	return
}

func parseGemmaArgs(s string) map[string]any {
	args := map[string]any{}
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		return args
	}
	s = s[1:]
	if strings.HasSuffix(s, "}") {
		s = s[:len(s)-1]
	}
	for _, part := range splitArgs(s) {
		colon := strings.Index(part, ":")
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(part[:colon])
		val := strings.Trim(strings.TrimSpace(part[colon+1:]), "\"")
		args[key] = val
	}
	return args
}

func splitArgs(s string) []string {
	var parts []string
	depth := 0
	inQuote := false
	start := 0
	for i, ch := range s {
		switch ch {
		case '"':
			inQuote = !inQuote
		case '{':
			if !inQuote {
				depth++
			}
		case '}':
			if !inQuote {
				depth--
			}
		case ',':
			if depth == 0 && !inQuote {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
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

func extractFinalAnswer(reasoning string) string {
	markers := []string{"\n\nFinal Answer:", "\n\n**Final Answer:**", "\n\nThe answer is:", "\n\n5. **Final"}
	for _, m := range markers {
		if idx := strings.LastIndex(reasoning, m); idx >= 0 {
			return strings.TrimSpace(reasoning[idx+len(m):])
		}
	}
	if idx := strings.LastIndex(reasoning, "\n\n"); idx >= 0 {
		last := reasoning[idx+2:]
		if len(last) < 500 {
			return strings.TrimSpace(last)
		}
	}
	if len(reasoning) > 200 {
		return "..." + reasoning[len(reasoning)-200:]
	}
	return reasoning
}

func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start < 0 {
			break
		}
		end := strings.Index(s, "</think>")
		if end < 0 {
			return strings.TrimSpace(s[:start])
		}
		s = s[:start] + s[end+8:]
	}
	for {
		start := strings.Index(s, "<|channel>thought")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], "<channel|>")
		if end < 0 {
			return strings.TrimSpace(s[:start])
		}
		end += start
		s = s[:start] + s[end+10:]
	}
	return strings.TrimSpace(s)
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

func forwardToUpstream(endpoint string, body any, apiKey string) ([]byte, error) {
	bodyJSON, _ := json.Marshal(body)
	url := upstreamURL + "/" + endpoint

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

func forwardToUpstreamStream(endpoint string, body any, apiKey string) (io.ReadCloser, error) {
	bodyJSON, _ := json.Marshal(body)
	url := upstreamURL + "/" + endpoint

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

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
