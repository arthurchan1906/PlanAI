package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"aipmc/session"
	"aipmc/store"
	"aipmc/u"
)

const (
	maxInjectChars = 800        // hard cap to prevent context explosion
	sessionTTL     = 48 * time.Hour // ignore sessions older than this
)

type injectState struct {
	lastAt      time.Time
	contentHash string
}

var (
	injectTracker sync.Map // map[agent]injectState
	sessionCache  struct {
		mu          sync.RWMutex
		goals       []string
		warnings    []string
		contentHash string
		updatedAt   time.Time
		ttl         time.Duration
	}
)

func init() {
	sessionCache.ttl = 5 * time.Minute
}

// InjectSessionContext prepends recent session goals into the system message
// of a proxy request. One injection per agent per unique content (content-hash
// based deduplication). Content is capped at maxInjectChars.
func InjectSessionContext(body []byte, agent string) []byte {
	goals, warnings, blockHash := getCachedContext()
	if len(goals) == 0 {
		u.LogShared("INJECT", "skip agent=%s reason=no_summary_data", agent)
		return body
	}

	block := buildContextBlock(goals, warnings)

	// Content-hash based dedup: only inject if content changed since last injection
	if !shouldInject(agent, blockHash) {
		return body
	}

	result := injectIntoPrompt(body, block, agent)
	injectTracker.Store(agent, injectState{lastAt: time.Now(), contentHash: blockHash})
	u.LogShared("INJECT", "agent=%s goals=%d warnings=%d chars=%d hash=%s", agent, len(goals), len(warnings), len(block), blockHash[:8])
	return result
}

func shouldInject(agent, contentHash string) bool {
	v, ok := injectTracker.Load(agent)
	if !ok {
		return true
	}
	st := v.(injectState)
	if st.contentHash == contentHash {
		u.LogShared("INJECT", "skip agent=%s reason=same_content hash=%s", agent, contentHash[:8])
		return false
	}
	return true
}

func getCachedContext() (goals, warnings []string, hash string) {
	sessionCache.mu.RLock()
	if time.Since(sessionCache.updatedAt) < sessionCache.ttl && len(sessionCache.goals) > 0 {
		defer sessionCache.mu.RUnlock()
		return sessionCache.goals, sessionCache.warnings, sessionCache.contentHash
	}
	sessionCache.mu.RUnlock()

	sessionCache.mu.Lock()
	defer sessionCache.mu.Unlock()

	cutoff := time.Now().Add(-sessionTTL).Format("2006-01-02T15:04:05")
	rows, err := store.ListSessionSummariesWithSummary(cutoff, 3)
	if err != nil || len(rows) == 0 {
		sessionCache.goals = nil
		sessionCache.contentHash = ""
		return nil, nil, ""
	}
	goals = make([]string, 0, len(rows))
	for _, r := range rows {
		var l2 session.SessionL2Summary
		if json.Unmarshal([]byte(r.Summary), &l2) == nil && l2.Goal != "" {
			sid := u.Prefix(r.SessionID, 8)
			goals = append(goals, fmt.Sprintf("[%s] %s", sid, l2.Goal))
		}
		// Extract blind_edit_loop findings from review_json
		if r.ReviewJSON != "" {
			var review map[string]any
			if json.Unmarshal([]byte(r.ReviewJSON), &review) == nil {
				findings, _ := review["findings"].([]any)
				for _, fi := range findings {
					f, _ := fi.(map[string]any)
					tag, _ := f["tag"].(string)
					if tag == "blind_edit_loop" {
						ev, _ := f["evidence"].(string)
						if ev != "" {
							warnings = append(warnings, fmt.Sprintf("[%s] \u26a0\ufe0f %s", u.Prefix(r.SessionID, 8), ev))
						}
					}
				}
			}
		}
	}

	// Merge user negative feedback from recent discussion_log
	if fb := detectUserFrustration(); len(fb) > 0 {
		warnings = append(warnings, fb...)
	}

	sessionCache.goals = goals
	sessionCache.warnings = warnings
	sessionCache.contentHash = hashString(fmt.Sprintf("%v%v", goals, warnings))
	sessionCache.updatedAt = time.Now()
	return goals, warnings, sessionCache.contentHash
}

func buildContextBlock(goals, warnings []string) string {
	var buf bytes.Buffer
	buf.WriteString("\n[AIPM Context]\n")
	written := 0
	suppressed := 0

	// Warnings first (high priority)
	for _, w := range warnings {
		line := w + "\n"
		if written+len(line) > maxInjectChars-50 {
			suppressed++
			continue
		}
		buf.WriteString(line)
		written += len(line)
	}

	if len(goals) > 0 {
		buf.WriteString("最近的 session:\n")
		for _, g := range goals {
			line := "- " + g + "\n"
			if written+len(line) > maxInjectChars-50 {
				suppressed++
				continue
			}
			buf.WriteString(line)
			written += len(line)
		}
	}

	if suppressed > 0 {
		u.LogShared("INJECT", "suppressed=%d reason=char_limit cap=%d", suppressed, maxInjectChars)
	}
	return buf.String()
}

// detectUserFrustration checks recent discussion_log for user frustration signals.
// Returns warnings if explicit negative feedback is found.
func detectUserFrustration() []string {
	negativeKW := []string{
		"没有变化", "还是不行", "没有效果", "还是不对", "完全没用",
		"你的方式很垃圾", "你在干什么",
	}
	messages, err := store.RecentUserMessages(5)
	if err != nil || len(messages) == 0 {
		return nil
	}
	var warnings []string
	for _, m := range messages {
		content := strings.ToLower(u.Str(m["content"]))
		for _, kw := range negativeKW {
			if strings.Contains(content, kw) {
				preview := u.TruncateStr(u.Str(m["content"]), 80)
				warnings = append(warnings, fmt.Sprintf("\u26a0\ufe0f \u7528\u6237\u53cd\u9988: %s", preview))
				break
			}
		}
	}
	return warnings
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ── Protocol-specific injectors (unchanged) ──────────────────────────

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
