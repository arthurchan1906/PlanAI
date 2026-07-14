package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	pmdb "aipmc/db"
	"aipmc/session"
	"aipmc/store"
	"aipmc/u"
)

var (
	injectTracker sync.Map // map[string]time.Time
	sessionCache  struct {
		mu        sync.RWMutex
		goals     []string
		updatedAt time.Time
		ttl       time.Duration
	}
)

func init() {
	sessionCache.ttl = 60 * time.Second
}

// InjectSessionContext prepends recent session goals into the system message
// or first user message of a proxy request. Uses a time-based tracker to avoid
// injecting into every request (5-minute cooldown per agent type).
func InjectSessionContext(body []byte, agent string) []byte {
	if !shouldInject(agent) {
		return body
	}

	goals, ids := getCachedSessionGoals()
	if len(goals) == 0 {
		u.LogShared("INJECT", "skip agent=%s reason=no_summary_data", agent)
		return body
	}

	block := buildContextBlock(goals)
	result := injectIntoPrompt(body, block, agent)
	u.LogShared("INJECT", "agent=%s goals=%d chars=%d ids=%v", agent, len(goals), len(block), ids)
	return result
}

func shouldInject(key string) bool {
	last, ok := injectTracker.Load(key)
	if !ok || time.Since(last.(time.Time)) > 5*time.Minute {
		injectTracker.Store(key, time.Now())
		return true
	}
	remaining := 5*time.Minute - time.Since(last.(time.Time))
	u.LogShared("INJECT", "skip agent=%s reason=cooldown remaining=%s", key, remaining.Truncate(time.Second))
	return false
}

func getCachedSessionGoals() (goals, ids []string) {
	sessionCache.mu.RLock()
	if time.Since(sessionCache.updatedAt) < sessionCache.ttl && len(sessionCache.goals) > 0 {
		defer sessionCache.mu.RUnlock()
		// Need to rebuild ids from cached goals since we only cache goals strings
		for _, g := range sessionCache.goals {
			if id := extractIDFromGoal(g); id != "" {
				ids = append(ids, id)
			}
		}
		return sessionCache.goals, ids
	}
	sessionCache.mu.RUnlock()

	sessionCache.mu.Lock()
	defer sessionCache.mu.Unlock()

	rows, err := store.ListSessionSummariesWithSummary("", 3)
	if err != nil || len(rows) == 0 {
		return nil, nil
	}
	goals = make([]string, 0, len(rows))
	ids = make([]string, 0, len(rows))
	for _, r := range rows {
		var l2 session.SessionL2Summary
		if json.Unmarshal([]byte(r.Summary), &l2) == nil && l2.Goal != "" {
			sid := shortID(r.SessionID)
			goals = append(goals, fmt.Sprintf("[%s] %s", sid, l2.Goal))
			ids = append(ids, sid)
		}
	}
	sessionCache.goals = goals
	sessionCache.updatedAt = time.Now()
	return goals, ids
}

func extractIDFromGoal(goal string) string {
	if len(goal) > 10 && goal[0] == '[' {
		end := strings.IndexByte(goal, ']')
		if end > 0 {
			return goal[1:end]
		}
	}
	return ""
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func buildContextBlock(goals []string) string {
	var buf bytes.Buffer
	buf.WriteString("\n[AIPM Context]\n最近 3 个 session:\n")
	for _, g := range goals {
		buf.WriteString("- " + g + "\n")
	}
	if hasVisionModels() {
		buf.WriteString("\n[能力] 你有 aipmc_vision 视觉工具，修改 UI 代码后可截图自查效果（公式：[代码]+[期望]+[问题]）。\n")
	}
	return buf.String()
}

// hasVisionModels checks models.json for any vision-tagged model.
func hasVisionModels() bool {
	reg := pmdb.LoadModelRegistry()
	for _, vm := range reg.Models {
		for _, t := range vm.Tags {
			if t == "vision" {
				return true
			}
		}
	}
	return false
}

func injectIntoPrompt(body []byte, block string, agent string) []byte {
	switch agent {
	case "claude":
		return injectAnthropic(body, block)
	case "codex":
		return injectCodex(body, block)
	case "gemini":
		return injectGemini(body, block)
	default:
		return injectOpenAI(body, block)
	}
}

func injectAnthropic(body []byte, block string) []byte {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	messages, _ := raw["messages"].([]any)
	if len(messages) == 0 {
		return body
	}
	for i, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if msg["role"] == "system" {
			content, _ := msg["content"].(string)
			messages[i] = map[string]any{
				"role":    "system",
				"content": content + block,
			}
			raw["messages"] = messages
			b, _ := json.Marshal(raw)
			return b
		}
	}
	messages = append([]any{map[string]any{"role": "system", "content": block}}, messages...)
	raw["messages"] = messages
	b, _ := json.Marshal(raw)
	return b
}

func injectCodex(body []byte, block string) []byte {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	instructions, _ := raw["instructions"].(string)
	raw["instructions"] = instructions + block
	b, _ := json.Marshal(raw)
	return b
}

func injectGemini(body []byte, block string) []byte {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	si, _ := raw["systemInstruction"].(map[string]any)
	if si == nil {
		raw["systemInstruction"] = map[string]any{
			"parts": []any{map[string]any{"text": block}},
		}
		b, _ := json.Marshal(raw)
		return b
	}
	parts, _ := si["parts"].([]any)
	if len(parts) == 0 {
		si["parts"] = []any{map[string]any{"text": block}}
	} else if p, ok := parts[0].(map[string]any); ok {
		text, _ := p["text"].(string)
		p["text"] = text + block
	}
	b, _ := json.Marshal(raw)
	return b
}

func injectOpenAI(body []byte, block string) []byte {
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	messages, _ := raw["messages"].([]any)
	if len(messages) == 0 {
		return body
	}
	for i, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if msg["role"] == "system" {
			content, _ := msg["content"].(string)
			messages[i] = map[string]any{
				"role":    "system",
				"content": content + block,
			}
			raw["messages"] = messages
			b, _ := json.Marshal(raw)
			return b
		}
	}
	messages = append([]any{map[string]any{"role": "system", "content": block}}, messages...)
	raw["messages"] = messages
	b, _ := json.Marshal(raw)
	return b
}
