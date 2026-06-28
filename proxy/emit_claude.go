package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// =============================================================================
// ClaudeEmitter — UnifiedStreamEvent → Anthropic SSE
// =============================================================================

// ClaudeEmitter emits stream events in the Anthropic Messages SSE format.
// It manages the complex block state machine: content_block_start/delta/stop
// with index-based block tracking, thinking↔text switching, and delayed
// tool_use emission.
type ClaudeEmitter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	model   string

	messageStarted bool
	messageID      string
	nextBlockIndex int
	thinkBlockOpen bool
	thinkBlockIdx  int
	textBlockOpen  bool
	textBlockIdx   int
	toolAcc        map[int]*StreamToolCall
	latestUsage    *UnifiedUsage
}

// NewClaudeEmitter creates a ClaudeEmitter ready to emit on w.
func NewClaudeEmitter(w http.ResponseWriter, model string) *ClaudeEmitter {
	flusher, _ := w.(http.Flusher)
	return &ClaudeEmitter{
		w:       w,
		flusher: flusher,
		model:   model,
		toolAcc: map[int]*StreamToolCall{},
	}
}

func (e *ClaudeEmitter) Emit(event UnifiedStreamEvent) {
	switch event.Type {

	case StreamThinking:
		if !e.messageStarted {
			e.startMessage()
		}
		if !e.thinkBlockOpen {
			e.thinkBlockIdx = e.nextBlockIndex
			e.nextBlockIndex++
			e.thinkBlockOpen = true
			e.emitSSE("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": e.thinkBlockIdx,
				"content_block": map[string]any{
					"type":     "thinking",
					"thinking": "",
				},
			})
		}
		e.emitSSE("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": e.thinkBlockIdx,
			"delta": map[string]any{
				"type":     "thinking_delta",
				"thinking": event.Delta,
			},
		})

	case StreamText:
		if !e.messageStarted {
			e.startMessage()
		}
		// Close thinking block if open (Anthropic requires block transitions)
		if e.thinkBlockOpen {
			e.emitSSE("content_block_stop", map[string]any{
				"type": "content_block_stop", "index": e.thinkBlockIdx,
			})
			e.thinkBlockOpen = false
		}
		if !e.textBlockOpen {
			e.textBlockIdx = e.nextBlockIndex
			e.nextBlockIndex++
			e.textBlockOpen = true
			e.emitSSE("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": e.textBlockIdx,
				"content_block": map[string]any{
					"type": "text",
					"text": "",
				},
			})
		}
		e.emitSSE("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": e.textBlockIdx,
			"delta": map[string]any{
				"type": "text_delta",
				"text": event.Delta,
			},
		})

	case StreamToolCallStart:
		if !e.messageStarted {
			e.startMessage()
		}
		// Close any open thinking/text blocks before tool use
		if e.thinkBlockOpen {
			e.emitSSE("content_block_stop", map[string]any{
				"type": "content_block_stop", "index": e.thinkBlockIdx,
			})
			e.thinkBlockOpen = false
		}
		if e.textBlockOpen {
			e.emitSSE("content_block_stop", map[string]any{
				"type": "content_block_stop", "index": e.textBlockIdx,
			})
			e.textBlockOpen = false
		}
		// Accumulate tool call (emitted at Done for Anthropic)
		acc := e.toolAcc[event.ToolIndex]
		if acc == nil {
			acc = &StreamToolCall{}
			e.toolAcc[event.ToolIndex] = acc
		}
		if event.ToolID != "" {
			acc.ID = event.ToolID
		}
		if event.ToolName != "" {
			acc.Name = event.ToolName
		}
		acc.Arguments += event.Delta

	case StreamToolCallDelta:
		acc := e.toolAcc[event.ToolIndex]
		if acc == nil {
			acc = &StreamToolCall{}
			e.toolAcc[event.ToolIndex] = acc
		}
		acc.Arguments += event.Delta

	case StreamDone:
		if event.Usage != nil {
			e.latestUsage = event.Usage
		}

	case StreamError:
		// Errors are handled by the caller
	}
}

func (e *ClaudeEmitter) Done(finishReason string, usage *UnifiedUsage) {
	// Close any open blocks
	if e.textBlockOpen {
		e.emitSSE("content_block_stop", map[string]any{
			"type": "content_block_stop", "index": e.textBlockIdx,
		})
		e.textBlockOpen = false
	}
	if e.thinkBlockOpen {
		e.emitSSE("content_block_stop", map[string]any{
			"type": "content_block_stop", "index": e.thinkBlockIdx,
		})
		e.thinkBlockOpen = false
	}

	// Emit accumulated tool calls as tool_use blocks
	for _, acc := range e.toolAcc {
		blockIdx := e.nextBlockIndex
		e.nextBlockIndex++
		e.emitSSE("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": blockIdx,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    acc.ID,
				"name":  acc.Name,
				"input": json.RawMessage("{}"),
			},
		})
		if acc.Arguments != "" && acc.Arguments[0] == '{' {
			e.emitSSE("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": blockIdx,
				"delta": map[string]any{
					"type":         "input_json_delta",
					"partial_json": acc.Arguments,
				},
			})
		}
		e.emitSSE("content_block_stop", map[string]any{
			"type": "content_block_stop", "index": blockIdx,
		})
	}

	// Usage
	usageJSON := map[string]any{"input_tokens": 0, "output_tokens": 0}
	if e.latestUsage != nil {
		usageJSON = map[string]any{
			"input_tokens":  e.latestUsage.PromptTokens,
			"output_tokens": e.latestUsage.CompletionTokens,
		}
	} else if usage != nil {
		usageJSON = map[string]any{
			"input_tokens":  usage.PromptTokens,
			"output_tokens": usage.CompletionTokens,
		}
	}

	// Stop reason
	stopReason := "end_turn"
	switch finishReason {
	case "length":
		stopReason = "max_tokens"
	case "tool_calls":
		stopReason = "tool_use"
	case "error":
		stopReason = "error"
	}

	e.emitSSE("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": usageJSON,
	})
	e.emitSSE("message_stop", map[string]any{"type": "message_stop"})

	if e.flusher != nil {
		e.flusher.Flush()
	}
}

func (e *ClaudeEmitter) startMessage() {
	e.messageStarted = true
	e.messageID = "msg_proxy"
	e.emitSSE("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":    e.messageID,
			"type":  "message",
			"role":  "assistant",
			"model": e.model,
			"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})
}

func (e *ClaudeEmitter) emitSSE(event string, data any) {
	if event != "" {
		fmt.Fprintf(e.w, "event: %s\n", event)
	}
	b, _ := json.Marshal(data)
	fmt.Fprintf(e.w, "data: %s\n\n", b)
	if e.flusher != nil {
		e.flusher.Flush()
	}
}
