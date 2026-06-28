package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// =============================================================================
// GeminiEmitter — UnifiedStreamEvent → Gemini SSE
// =============================================================================

// GeminiEmitter emits stream events in the Gemini protocol SSE format.
// It maintains minimal state — chunks are mostly self-contained.
type GeminiEmitter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	model   string

	toolAcc       map[int]*StreamToolCall
	pendingFinish string
	totalTokens   int
}

// NewGeminiEmitter creates a GeminiEmitter ready to emit on w.
func NewGeminiEmitter(w http.ResponseWriter, model string) *GeminiEmitter {
	flusher, _ := w.(http.Flusher)
	return &GeminiEmitter{
		w:       w,
		flusher: flusher,
		model:   model,
		toolAcc: map[int]*StreamToolCall{},
	}
}

func (e *GeminiEmitter) Emit(event UnifiedStreamEvent) {
	switch event.Type {
	case StreamThinking:
		e.emitChunk(&GeminiResponse{
			ModelVersion: e.model,
			Candidates: []GeminiCandidate{{
				Content: &GeminiContent{Role: "model", Parts: []GeminiPart{{Thought: event.Delta}}},
				Index:   0,
			}},
		})

	case StreamText:
		e.emitChunk(&GeminiResponse{
			ModelVersion: e.model,
			Candidates: []GeminiCandidate{{
				Content: &GeminiContent{Role: "model", Parts: []GeminiPart{{Text: event.Delta}}},
				Index:   0,
			}},
		})

	case StreamToolCallStart:
		acc := e.toolAcc[event.ToolIndex]
		if acc == nil {
			acc = &StreamToolCall{}
			e.toolAcc[event.ToolIndex] = acc
		}
		acc.ID = event.ToolID
		acc.Name = event.ToolName
		acc.Arguments += event.Delta

	case StreamToolCallDelta:
		acc := e.toolAcc[event.ToolIndex]
		if acc == nil {
			acc = &StreamToolCall{}
			e.toolAcc[event.ToolIndex] = acc
		}
		acc.Arguments += event.Delta

	case StreamDone:
		// Accumulated tool calls are flushed in Done()
		e.pendingFinish = event.FinishReason
		if event.Usage != nil {
			e.totalTokens = event.Usage.TotalTokens
		}

	case StreamError:
		log.Printf("[GEMINI_EMIT] stream error: %s", event.Delta)
	}
}

func (e *GeminiEmitter) Done(finishReason string, usage *UnifiedUsage) {
	// Flush accumulated tool calls
	if len(e.toolAcc) > 0 {
		var parts []GeminiPart
		for _, acc := range e.toolAcc {
			var args map[string]any
			json.Unmarshal([]byte(acc.Arguments), &args)
			parts = append(parts, GeminiPart{
				FunctionCall: &GeminiFuncCall{
					ID:   acc.ID,
					Name: acc.Name,
					Args: args,
				},
			})
		}
		if len(parts) > 0 {
			fr := e.pendingFinish
			if fr == "" {
				fr = finishReason
			}
			if fr == "" {
				fr = "TOOL_CALLS"
			}
			e.emitChunk(&GeminiResponse{
				ModelVersion: e.model,
				Candidates: []GeminiCandidate{{
					Content:      &GeminiContent{Role: "model", Parts: parts},
					FinishReason: fr,
					Index:        0,
				}},
			})
		}
	}

	// Final chunk with usage
	reason := e.pendingFinish
	if reason == "" {
		reason = finishReason
	}
	if reason == "" {
		reason = "STOP"
	}
	finalChunk := map[string]any{
		"candidates": []map[string]any{{
			"content":      map[string]any{"role": "model", "parts": []any{}},
			"finishReason": reason,
			"index":        0,
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount":     0,
			"candidatesTokenCount": e.totalTokens,
			"totalTokenCount":      e.totalTokens,
		},
	}
	e.emitRaw(finalChunk)

	if e.flusher != nil {
		e.flusher.Flush()
	}
}

func (e *GeminiEmitter) emitChunk(gc *GeminiResponse) {
	e.emitRaw(gc)
	if e.flusher != nil {
		e.flusher.Flush()
	}
}

func (e *GeminiEmitter) emitRaw(data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(e.w, "data: %s\n\n", b)
}
