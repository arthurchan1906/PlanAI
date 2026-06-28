package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// =============================================================================
// CodexEmitter — UnifiedStreamEvent → Responses API SSE
// =============================================================================

// codexToolAcc tracks an in-progress function_call in the Responses protocol.
type codexToolAcc struct {
	ID        string
	Name      string
	Arguments string
	itemID    string
	outputIdx int
}

// CodexEmitter emits stream events in the OpenAI Responses API SSE format.
// It maintains the protocol-specific state: output_index numbering, item
// lifecycles, and reasoning promotion.
type CodexEmitter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	model   string

	responseID         string
	started            bool
	outputIndex        int
	textItemID         string
	textAdded          bool
	textOutputIdx      int
	reasoningItemID    string
	reasoningAdded     bool
	reasoningOutputIdx int
	pendingToolCalls   map[int]*codexToolAcc
	pendingFinish      string
}

// NewCodexEmitter creates a CodexEmitter ready to emit on w.
func NewCodexEmitter(w http.ResponseWriter, model string) *CodexEmitter {
	flusher, _ := w.(http.Flusher)
	return &CodexEmitter{
		w:               w,
		flusher:         flusher,
		model:           model,
		responseID:      "resp_ccswitch",
		pendingToolCalls: map[int]*codexToolAcc{},
	}
}

func (e *CodexEmitter) Emit(event UnifiedStreamEvent) {
	switch event.Type {

	case StreamThinking:
		if !e.started {
			e.startResponse()
		}
		if !e.reasoningAdded {
			e.reasoningAdded = true
			e.reasoningOutputIdx = e.outputIndex
			e.outputIndex++
			e.reasoningItemID = "rs_" + e.responseID
			e.emitSSE("response.output_item.added", map[string]any{
				"type":         "response.output_item.added",
				"output_index": e.reasoningOutputIdx,
				"item": map[string]any{
					"id":      e.reasoningItemID,
					"type":    "reasoning",
					"status":  "in_progress",
					"summary": []any{},
				},
			})
		}
		// Use reasoning_text.delta instead of reasoning_summary_text.delta.
		// Codex TUI unconditionally displays ReasoningSummaryTextDelta (protocol.rs:80),
		// but gates ReasoningTextDelta on show_raw_agent_reasoning (protocol.rs:83).
		e.emitSSE("response.reasoning_text.delta", map[string]any{
			"type":          "response.reasoning_text.delta",
			"item_id":       e.reasoningItemID,
			"output_index":  e.reasoningOutputIdx,
			"content_index": 0,
			"delta":         event.Delta,
		})

	case StreamText:
		if !e.started {
			e.startResponse()
		}
		if !e.textAdded {
			e.textAdded = true
			e.textOutputIdx = e.outputIndex
			e.outputIndex++
			e.textItemID = e.responseID + "_msg"
			e.emitSSE("response.output_item.added", map[string]any{
				"type":         "response.output_item.added",
				"output_index": e.textOutputIdx,
				"item": map[string]any{
					"id":      e.textItemID,
					"type":    "message",
					"role":    "assistant",
					"status":  "in_progress",
					"content": []any{},
				},
			})
			e.emitSSE("response.content_part.added", map[string]any{
				"type":          "response.content_part.added",
				"item_id":       e.textItemID,
				"output_index":  e.textOutputIdx,
				"content_index": 0,
				"part":          map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
			})
		}
		e.emitSSE("response.output_text.delta", map[string]any{
			"type":          "response.output_text.delta",
			"item_id":       e.textItemID,
			"output_index":  e.textOutputIdx,
			"content_index": 0,
			"delta":         event.Delta,
		})

	case StreamToolCallStart:
		if !e.started {
			e.startResponse()
		}
		acc := e.pendingToolCalls[event.ToolIndex]
		if acc == nil {
			acc = &codexToolAcc{}
			e.pendingToolCalls[event.ToolIndex] = acc
		}
		if event.ToolID != "" {
			acc.ID = event.ToolID
		}
		if event.ToolName != "" {
			acc.Name = event.ToolName
		}
		acc.Arguments += event.Delta

		// Emit output_item.added on first appearance
		if acc.itemID == "" && (acc.ID != "" || acc.Name != "") {
			oi := e.outputIndex
			e.outputIndex++
			acc.itemID = fmt.Sprintf("fc_%s_%d", e.responseID, oi)
			acc.outputIdx = oi
			e.emitSSE("response.output_item.added", map[string]any{
				"type":         "response.output_item.added",
				"output_index": oi,
				"item": map[string]any{
					"id":        acc.itemID,
					"type":      "function_call",
					"status":    "in_progress",
					"call_id":   acc.ID,
					"name":      acc.Name,
					"arguments": "",
				},
			})
			if event.Delta != "" {
				e.emitSSE("response.function_call_arguments.delta", map[string]any{
					"type":         "response.function_call_arguments.delta",
					"item_id":      acc.itemID,
					"output_index": oi,
					"delta":        event.Delta,
				})
			}
		}

	case StreamToolCallDelta:
		acc := e.pendingToolCalls[event.ToolIndex]
		if acc == nil {
			acc = &codexToolAcc{}
			e.pendingToolCalls[event.ToolIndex] = acc
		}
		acc.Arguments += event.Delta
		if acc.itemID != "" && event.Delta != "" {
			e.emitSSE("response.function_call_arguments.delta", map[string]any{
				"type":         "response.function_call_arguments.delta",
				"item_id":      acc.itemID,
				"output_index": acc.outputIdx,
				"delta":        event.Delta,
			})
		}

	case StreamDone:
		e.pendingFinish = event.FinishReason

	case StreamError:
		log.Printf("[CODEX_EMIT] stream error: %s", event.Delta)
	}
}

func (e *CodexEmitter) Done(finishReason string, usage *UnifiedUsage) {
	// Complete in-progress reasoning item
	if e.reasoningAdded {
		e.emitSSE("response.output_item.done", map[string]any{
			"type":         "response.output_item.done",
			"output_index": e.reasoningOutputIdx,
			"item": map[string]any{
				"id":     e.reasoningItemID,
				"type":   "reasoning",
				"status": "completed",
			},
		})
	}

	// Complete in-progress text item
	if e.textAdded {
		e.emitSSE("response.output_item.done", map[string]any{
			"type":         "response.output_item.done",
			"output_index": e.textOutputIdx,
			"item": map[string]any{
				"id":     e.textItemID,
				"type":   "message",
				"status": "completed",
			},
		})
	}

	// Complete tool calls
	for _, acc := range e.pendingToolCalls {
		if acc.itemID != "" {
			args := acc.Arguments
			if args == "" {
				args = "{}"
			}
			e.emitSSE("response.output_item.done", map[string]any{
				"type":         "response.output_item.done",
				"output_index": acc.outputIdx,
				"item": map[string]any{
					"id":        acc.itemID,
					"type":      "function_call",
					"status":    "completed",
					"call_id":   acc.ID,
					"name":      acc.Name,
					"arguments": args,
				},
			})
		}
	}

	// Final response.completed
	status := "completed"
	if finishReason == "length" {
		status = "incomplete"
	} else if finishReason == "error" {
		status = "failed"
	}
	totalTokens := 0
	if usage != nil {
		totalTokens = usage.TotalTokens
	}
	e.emitSSE("response.completed", map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     e.responseID,
			"object": "response",
			"status": status,
			"model":  e.model,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": totalTokens,
				"total_tokens":  totalTokens,
			},
		},
	})

	if e.flusher != nil {
		e.flusher.Flush()
	}
}

func (e *CodexEmitter) startResponse() {
	e.started = true
	e.emitSSE("response.created", map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":     e.responseID,
			"object": "response",
			"status": "in_progress",
			"model":  e.model,
		},
	})
}

func (e *CodexEmitter) emitSSE(event string, data any) {
	if event != "" {
		fmt.Fprintf(e.w, "event: %s\n", event)
	}
	b, _ := json.Marshal(data)
	fmt.Fprintf(e.w, "data: %s\n\n", b)
	if e.flusher != nil {
		e.flusher.Flush()
	}
}
