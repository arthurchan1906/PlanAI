package proxy

import (
	"log"
	"strings"
)

// =============================================================================
// Stream normalizer — model-specific output normalization for streaming
// =============================================================================

// StreamNormalizer processes a stream of UnifiedStreamEvent values and
// normalizes model-specific behaviors:
//
//  1. Field-name compatibility: already handled by parseUpstreamSSE
//  2. Think-tag stripping: removes <think>...</think> and <|channel>...</channel|>
//  3. Reasoning→content promotion: if the model only emitted reasoning but no
//     regular content, promotes the reasoning text as visible output
//  4. Gemma inline tool-call parsing: detects <|tool_call|> blocks in text
//     and converts them to StreamToolCallStart events
//
// The normalizer is stateful across events: it accumulates reasoning text and
// detects whether any regular content arrived; the promotion decision is made
// only when StreamDone arrives.
type StreamNormalizer struct {
	ReasoningBuf strings.Builder
	HasContent   bool
	ThinkBuf     strings.Builder // text buffer for Gemma tool-call detection
}

// Process applies normalization to a single event and returns zero or more
// normalized events. A single input event may produce multiple output events
// (e.g., when Gemma tool calls are parsed from text, or when reasoning is
// promoted to content at stream end).
func (n *StreamNormalizer) Process(event UnifiedStreamEvent) []UnifiedStreamEvent {
	switch event.Type {
	case StreamThinking:
		n.ReasoningBuf.WriteString(event.Delta)
		return []UnifiedStreamEvent{event}

	case StreamText:
		n.HasContent = true
		return n.processTextDelta(event.Delta)

	case StreamToolCallStart, StreamToolCallDelta:
		return []UnifiedStreamEvent{event}

	case StreamDone:
		return n.finalize(event)

	case StreamError:
		return []UnifiedStreamEvent{event}
	}
	return nil
}

func (n *StreamNormalizer) processTextDelta(delta string) []UnifiedStreamEvent {
	n.ThinkBuf.WriteString(delta)
	raw := n.ThinkBuf.String()

	// ── Gemma 4 inline tool-call detection ──
	hasToolOpen := strings.Contains(raw, "<|tool_call>")
	hasToolClose := strings.Contains(raw, "<tool_call|>")
	hasThinkClose := strings.Contains(raw, "<channel|>") || strings.Contains(raw, "</think>")

	if hasToolOpen && hasToolClose {
		// Complete tool-call block found — parse and emit
		cleaned, toolCalls := parseGemmaToolCalls(raw)
		n.ThinkBuf.Reset()
		var out []UnifiedStreamEvent
		if cleaned != "" {
			out = append(out, UnifiedStreamEvent{Type: StreamText, Delta: cleaned})
		}
		for i, tc := range toolCalls {
			out = append(out, UnifiedStreamEvent{
				Type:      StreamToolCallStart,
				ToolIndex: i,
				ToolID:    tc.ID,
				ToolName:  tc.Name,
				Delta:     tc.Arguments,
			})
		}
		return out
	}

	if hasThinkClose {
		// Thinking block closed — strip tags, emit remaining text
		clean := stripThinkTags(raw)
		n.ThinkBuf.Reset()
		if clean != "" {
			return []UnifiedStreamEvent{{Type: StreamText, Delta: clean}}
		}
		return nil
	}

	// Still inside a potential tool-call or think block — keep buffering.
	// Guard against unbounded buffering: if the buffer is large and doesn't
	// start with '<', it's just regular text.
	if len(raw) > 0 && raw[0] != '<' {
		n.ThinkBuf.Reset()
		return []UnifiedStreamEvent{{Type: StreamText, Delta: raw}}
	}
	if len(raw) > 3000 {
		// Safety valve: flush buffer as-is after 3KB
		n.ThinkBuf.Reset()
		return []UnifiedStreamEvent{{Type: StreamText, Delta: raw}}
	}

	return nil // keep buffering
}

func (n *StreamNormalizer) finalize(event UnifiedStreamEvent) []UnifiedStreamEvent {
	var out []UnifiedStreamEvent

	// Flush any remaining text in ThinkBuf
	if n.ThinkBuf.Len() > 0 {
		raw := n.ThinkBuf.String()
		cleaned, toolCalls := parseGemmaToolCalls(raw)
		if cleaned != "" {
			out = append(out, UnifiedStreamEvent{Type: StreamText, Delta: cleaned})
		}
		for i, tc := range toolCalls {
			out = append(out, UnifiedStreamEvent{
				Type:      StreamToolCallStart,
				ToolIndex: i,
				ToolID:    tc.ID,
				ToolName:  tc.Name,
				Delta:     tc.Arguments,
			})
		}
		n.ThinkBuf.Reset()
	}

	// ── Reasoning → Content promotion (DeepSeek behavior) ──
	if n.ReasoningBuf.Len() > 0 && !n.HasContent {
		promoted := promoteReasoningToContent(n.ReasoningBuf.String())
		if promoted != "" {
			log.Printf("[NORMALIZE] promoting reasoning→content (%d bytes reasoning, no content)",
				n.ReasoningBuf.Len())
			out = append(out, UnifiedStreamEvent{Type: StreamText, Delta: promoted})
		}
	}

	out = append(out, event) // StreamDone last
	return out
}

// =============================================================================
// Non-streaming response normalization
// =============================================================================

// NormalizeResponse applies model-specific normalization to a non-streaming
// OpenAI response in-place. After this call, the response is ready for
// protocol-specific conversion via Adapter.ConvertResponse.
//
// The caller MUST call this BEFORE passing the response to ConvertResponse.
func NormalizeResponse(resp *OpenAIResponse) {
	for i := range resp.Choices {
		msg := resp.Choices[i].Message
		if msg == nil {
			continue
		}

		// ── Reasoning → Content promotion ──
		hasContent := msg.Content != nil && msg.Content != ""
		if msg.ReasoningContent != "" && !hasContent {
			promoted := promoteReasoningToContent(msg.ReasoningContent)
			msg.Content = promoted
			log.Printf("[NORMALIZE] non-stream promote reasoning→content (%d bytes)",
				len(msg.ReasoningContent))
		}

		// ── Think-tag stripping on content ──
		if s, ok := msg.Content.(string); ok && s != "" {
			cleaned := stripThinkTags(s)
			if cleaned != s {
				msg.Content = cleaned
			}
		}
	}
}

// =============================================================================
// Content normalization functions — shared by stream and non-stream paths
// =============================================================================

// stripThinkTags removes thinking markup from text.
// Supported formats:
//   - <think>...</think>   (DeepSeek-R1)
//   - <|channel>thought...<channel|>  (Google/DeepMind)
func stripThinkTags(s string) string {
	// <think>...</think>
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
	// <|channel>thought...<channel|>
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

// promoteReasoningToContent extracts visible answer text from model reasoning.
// 4-tier fallback:
//  1. Strip <think> tags — if content remains after stripping, use it
//  2. Match "Final Answer:" / "The answer is:" markers
//  3. Take last paragraph (text after final double-newline, if short enough)
//  4. Take last 200 characters of reasoning
func promoteReasoningToContent(raw string) string {
	// Tier 1: strip thinking tags
	cleaned := stripThinkTags(raw)
	if cleaned != "" {
		return cleaned
	}

	// Tier 2: match explicit answer markers
	markers := []string{
		"\n\nFinal Answer:",
		"\n\n**Final Answer:**",
		"\n\nThe answer is:",
		"\n\n5. **Final",
	}
	for _, m := range markers {
		if idx := strings.LastIndex(raw, m); idx >= 0 {
			result := strings.TrimSpace(raw[idx+len(m):])
			if result != "" {
				return result
			}
		}
	}

	// Tier 3: last paragraph (after final double-newline)
	if idx := strings.LastIndex(raw, "\n\n"); idx >= 0 {
		last := raw[idx+2:]
		if len(last) < 500 {
			return strings.TrimSpace(last)
		}
	}

	// Tier 4: trailing 200 characters
	if len(raw) > 200 {
		return "..." + raw[len(raw)-200:]
	}
	return raw
}

// =============================================================================
// Gemma 4 inline tool-call parser
// =============================================================================

// parseGemmaToolCalls scans text for inline <|tool_call|> blocks and returns
// cleaned text (with tool-call blocks removed) and parsed tool calls.
//
// Format: <|tool_call>call:functionName{"arg":"val"}<tool_call|>
func parseGemmaToolCalls(text string) (cleaned string, toolCalls []UnifiedToolCall) {
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

		// Text before the tool call block
		cleaned += text[:start]

		// Extract inner content
		raw := text[start+12 : end]
		raw = strings.TrimPrefix(raw, "call:")

		bracePos := strings.Index(raw, "{")
		if bracePos >= 0 {
			name := strings.TrimSpace(raw[:bracePos])
			// Strip any "namespace:" prefix (e.g. "shell:run_command" → "run_command")
			if idx := strings.LastIndex(name, ":"); idx >= 0 && idx < len(name)-1 {
				name = name[idx+1:]
			}
			args := parseGemmaArgs(raw[bracePos:])
			argsJSON := gemmaArgsToJSON(args)
			toolCalls = append(toolCalls, UnifiedToolCall{
				Name:      name,
				Arguments: argsJSON,
			})
		}

		text = text[end+12:] // skip past <tool_call|>
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

func gemmaArgsToJSON(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	var parts []string
	for k, v := range args {
		parts = append(parts, `"`+k+`":"`+strings.ReplaceAll(v.(string), `"`, `\"`)+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
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
