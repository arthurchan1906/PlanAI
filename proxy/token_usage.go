package proxy

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// TokenUsageRecord captures token consumption for a single LLM API call.
type TokenUsageRecord struct {
	Time              string `json:"time"`
	Agent             string `json:"agent"`
	Model             string `json:"model"`
	PromptTokens      int    `json:"prompt_tokens"`
	CompletionTokens  int    `json:"completion_tokens"`
	CacheHitTokens    int    `json:"cache_hit_tokens"`
	CacheCreationTokens int  `json:"cache_creation_tokens"`
}

// In-memory ring buffer — same pattern as captureLog in inspect.go.
var (
	tokenUsageMu   sync.Mutex
	tokenUsageLog  []TokenUsageRecord
	maxTokenUsage  = 5000
	tokenUsageAgg  TokenUsageAggregate
)

// TokenUsageAggregate holds running totals.
type TokenUsageAggregate struct {
	TotalPromptTokens      int `json:"total_prompt_tokens"`
	TotalCompletionTokens  int `json:"total_completion_tokens"`
	TotalCacheHitTokens    int `json:"total_cache_hit_tokens"`
	TotalCacheCreationTokens int `json:"total_cache_creation_tokens"`
	TotalCalls             int `json:"total_calls"`
}

// RecordTokenUsage appends a token usage record to the in-memory ring buffer.
func RecordTokenUsage(r TokenUsageRecord) {
	if r.PromptTokens == 0 && r.CompletionTokens == 0 {
		return
	}

	tokenUsageMu.Lock()
	defer tokenUsageMu.Unlock()

	r.Time = time.Now().Format("15:04:05.000")
	tokenUsageLog = append(tokenUsageLog, r)
	if len(tokenUsageLog) > maxTokenUsage {
		tokenUsageLog = tokenUsageLog[len(tokenUsageLog)-maxTokenUsage:]
	}

	tokenUsageAgg.TotalPromptTokens += r.PromptTokens
	tokenUsageAgg.TotalCompletionTokens += r.CompletionTokens
	tokenUsageAgg.TotalCacheHitTokens += r.CacheHitTokens
	tokenUsageAgg.TotalCacheCreationTokens += r.CacheCreationTokens
	tokenUsageAgg.TotalCalls++
}

// TokenUsageSnapshot returns a copy of all buffered records + aggregate.
func TokenUsageSnapshot() ([]TokenUsageRecord, TokenUsageAggregate) {
	tokenUsageMu.Lock()
	defer tokenUsageMu.Unlock()

	entries := make([]TokenUsageRecord, len(tokenUsageLog))
	copy(entries, tokenUsageLog)
	agg := tokenUsageAgg
	return entries, agg
}

// extractAnthropicStreamUsage scans an Anthropic Messages API SSE stream body
// and extracts token usage from all events that carry usage information.
//
// Anthropic SSE format:
//	message_start  → usage.input_tokens
//	message_delta  → usage.output_tokens
//	message_stop   → no usage
//
// This scans every data: line and accumulates the highest seen value for each
// token type, so it is robust regardless of which event carries the usage or
// which implementation quirks the upstream has.
func extractAnthropicStreamUsage(body string) (inputTokens, outputTokens, cacheHitTokens, cacheCreationTokens int) {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var event struct {
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		if event.Usage.InputTokens > 0 {
			inputTokens = event.Usage.InputTokens
		}
		if event.Usage.OutputTokens > 0 {
			outputTokens = event.Usage.OutputTokens
		}
		if event.Usage.CacheReadInputTokens > 0 {
			cacheHitTokens = event.Usage.CacheReadInputTokens
		}
		if event.Usage.CacheCreationInputTokens > 0 {
			cacheCreationTokens = event.Usage.CacheCreationInputTokens
		}
	}
	return
}
