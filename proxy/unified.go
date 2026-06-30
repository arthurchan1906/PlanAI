package proxy

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// =============================================================================
// Unified intermediate types — agent- and model-agnostic request/response IR
// =============================================================================

// UnifiedReq is the standard intermediate request representation between
// protocol adapters and the upstream router. Every agent adapter translates
// its native protocol into this single type.
type UnifiedReq struct {
	Model           string
	Messages        []UnifiedMsg
	Stream          bool
	Temperature     *float64
	MaxTokens       *int
	TopP            *float64
	Stop            []string
	Tools           []UnifiedTool
	ToolChoice      any
	ReasoningEffort *string
}

// UnifiedMsg is a single message in the unified conversation history.
// Thinking is kept separate from Content so the normalizer can distinguish
// model reasoning from the actual answer without guessing field semantics.
type UnifiedMsg struct {
	Role       string // system / user / assistant / tool
	Content    string // plain text
	Thinking   string // reasoning / thinking, explicitly separated from Content
	ToolCalls  []UnifiedToolCall
	ToolCallID string
}

// UnifiedTool describes a function available to the model.
type UnifiedTool struct {
	Name        string
	Description string
	Parameters  any // JSON Schema
}

// UnifiedToolCall is a function call issued by the model.
type UnifiedToolCall struct {
	ID        string
	Name      string
	Arguments string // JSON string
}

// UnifiedUsage carries token counts including cache.
type UnifiedUsage struct {
	PromptTokens      int
	CompletionTokens  int
	TotalTokens       int
	CacheHitTokens    int // prompt_cache_hit_tokens (OpenAI) or cache_read_input_tokens (Anthropic)
	CacheCreationTokens int // cache_creation_input_tokens (Anthropic)
}

// =============================================================================
// Streaming events — intermediate representation between SSE parser and emitters
// =============================================================================

// StreamEventType classifies a parsed SSE delta chunk.
type StreamEventType int

const (
	StreamThinking      StreamEventType = iota // reasoning_content / reasoning delta
	StreamText                                // content text delta
	StreamToolCallStart                       // first chunk of a new tool call (has id + name)
	StreamToolCallDelta                       // subsequent tool call arguments fragment
	StreamDone                                // [DONE] signal with finish_reason and usage
	StreamError                               // upstream or parse error
)

// UnifiedStreamEvent is a single event in the stream pipeline.
// It is produced by parseUpstreamSSE, optionally transformed by StreamNormalizer,
// and consumed by protocol-specific emitters.
type UnifiedStreamEvent struct {
	Type StreamEventType

	// StreamThinking / StreamText / StreamToolCallDelta / StreamError
	Delta string

	// StreamToolCallStart / StreamToolCallDelta
	ToolIndex int    // index within the tool_calls array
	ToolID    string // populated on StreamToolCallStart
	ToolName  string // populated on StreamToolCallStart

	// StreamDone
	FinishReason string // "stop" | "length" | "tool_calls"
	Model        string
	Usage        *UnifiedUsage
}

// StreamToolCall is a shared accumulator for emitters to track in-progress tool calls.
type StreamToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// =============================================================================
// Core interfaces
// =============================================================================

// ProtocolAdapter translates an agent's native protocol to/from the unified
// intermediate representation. Each code agent (Gemini, Codex, Claude, etc.)
// has its own adapter implementation.
type ProtocolAdapter interface {
	// ParseRequest reads and parses the agent's native HTTP request body,
	// returning a UnifiedReq in the standard intermediate format.
	ParseRequest(r *http.Request) (*UnifiedReq, error)

	// ConvertResponse converts a normalized OpenAI response back into the
	// agent's native response format. The caller MUST call NormalizeResponse
	// on the OpenAI response before passing it here.
	ConvertResponse(openaiResp *OpenAIResponse, model string) any

	// NewEmitter creates a streaming emitter for this agent's protocol.
	NewEmitter(w http.ResponseWriter, model string) Emitter
}

// Emitter consumes UnifiedStreamEvent values and emits them in an agent's
// native SSE protocol. Each emitter maintains the per-protocol state machine
// (block indices, item lifecycles, etc.) required by its protocol.
type Emitter interface {
	// Emit processes a single stream event and writes the corresponding
	// native SSE data to the underlying http.ResponseWriter.
	Emit(event UnifiedStreamEvent)

	// Done signals stream completion with the final finish reason and usage.
	// The emitter performs protocol-specific cleanup (close open blocks,
	// emit message_delta/message_stop, etc.).
	Done(finishReason string, usage *UnifiedUsage)
}

// =============================================================================
// SSE parser: upstream OpenAI Chat Completions → UnifiedStreamEvent channel
// =============================================================================

// parseUpstreamSSE reads an OpenAI Chat Completions SSE stream and emits
// UnifiedStreamEvent values on the returned channel.
//
// Responsibilities:
//   - Parse "data: {...}" lines as JSON chunks
//   - Extract delta fields: content, reasoning_content/reasoning, tool_calls
//   - Emit StreamToolCallStart when a new tool call index first appears (has id+name)
//   - Emit StreamToolCallDelta for subsequent arguments fragments
//   - Emit StreamDone on [DONE] with the last-known finish_reason and usage
//   - Emit StreamError on JSON parse errors, then close the channel
//
// Field-name compatibility: both "reasoning_content" and "reasoning" are checked
// and unified into StreamThinking events.
func parseUpstreamSSE(r io.Reader) <-chan UnifiedStreamEvent {
	ch := make(chan UnifiedStreamEvent, 16)

	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

		var (
			modelName         string
			latestFinishReason string
			latestUsage       *UnifiedUsage
			seenToolIndices   = map[int]bool{}
		)

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				// Emit final usage info if we tracked any
				usage := latestUsage
				if usage == nil {
					usage = &UnifiedUsage{}
				}
				ch <- UnifiedStreamEvent{
					Type:         StreamDone,
					FinishReason: latestFinishReason,
					Model:        modelName,
					Usage:        usage,
				}
				continue
			}

			var raw map[string]any
			if err := json.Unmarshal([]byte(data), &raw); err != nil {
				ch <- UnifiedStreamEvent{
					Type:  StreamError,
					Delta: "parse error: " + err.Error(),
				}
				return
			}

			// Extract model name from first chunk
			if modelName == "" {
				if m, ok := raw["model"].(string); ok && m != "" {
					modelName = m
				}
			}

			// Track usage (including cache)
			if u, ok := raw["usage"].(map[string]any); ok {
				pt, _ := u["prompt_tokens"].(float64)
				ct, _ := u["completion_tokens"].(float64)
				cht, _ := u["prompt_cache_hit_tokens"].(float64)
				latestUsage = &UnifiedUsage{
					PromptTokens:     int(pt),
					CompletionTokens: int(ct),
					TotalTokens:      int(pt) + int(ct),
					CacheHitTokens:   int(cht),
				}
			}

			choices, _ := raw["choices"].([]any)
			if len(choices) == 0 {
				continue
			}
			choice, ok := choices[0].(map[string]any)
			if !ok {
				continue
			}

			// Track finish_reason
			if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
				latestFinishReason = fr
			}

			delta, _ := choice["delta"].(map[string]any)
			if delta == nil {
				continue
			}

			// ── reasoning_content / reasoning → StreamThinking ──
			// Check both field names; upstream models vary (DeepSeek uses
			// "reasoning_content", some GLM versions use "reasoning").
			reasoning := ""
			for _, k := range []string{"reasoning_content", "reasoning"} {
				if s, ok := delta[k].(string); ok && s != "" {
					reasoning = s
					break
				}
			}
			if reasoning != "" {
				ch <- UnifiedStreamEvent{Type: StreamThinking, Delta: reasoning}
			}

			// ── content → StreamText ──
			if c, ok := delta["content"]; ok && c != nil {
				if s, ok := c.(string); ok && s != "" {
					ch <- UnifiedStreamEvent{Type: StreamText, Delta: s}
				}
			}

			// ── tool_calls → StreamToolCallStart / StreamToolCallDelta ──
			if tcs, ok := delta["tool_calls"].([]any); ok {

				for _, tc := range tcs {
					tcMap, ok := tc.(map[string]any)
					if !ok {
						continue
					}
					rawIdx, ok := tcMap["index"].(float64)
					if !ok {
						continue
					}
					idx := int(rawIdx)

					id, _ := tcMap["id"].(string)
					fn, hasFn := tcMap["function"].(map[string]any)

					if !seenToolIndices[idx] && (id != "" || (hasFn && fn["name"] != nil)) {
						// First appearance — emit StreamToolCallStart with id + name
						seenToolIndices[idx] = true
						name := ""
						if hasFn {
							if n, ok := fn["name"].(string); ok {
								name = n
							}
						}
						args := ""
						if hasFn {
							if a, ok := fn["arguments"].(string); ok {
								args = a
							}
						}
						ch <- UnifiedStreamEvent{
							Type:      StreamToolCallStart,
							ToolIndex: idx,
							ToolID:    id,
							ToolName:  name,
							Delta:     args,
						}
					} else {
						// Subsequent chunk — emit StreamToolCallDelta with arguments fragment
						args := ""
						if hasFn {
							if a, ok := fn["arguments"].(string); ok {
								args = a
							}
						}
						if args != "" {
							ch <- UnifiedStreamEvent{
								Type:      StreamToolCallDelta,
								ToolIndex: idx,
								Delta:     args,
							}
						}
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- UnifiedStreamEvent{
				Type:  StreamError,
				Delta: "scanner error: " + err.Error(),
			}
		}
	}()

	return ch
}
