package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"aipmc/ai"
	"aipmc/db"
	"aipmc/store"
	"aipmc/u"
)

// SessionL2Summary is the structured JSON output from the AI model for L2 summary.
type SessionL2Summary struct {
	Goal        string   `json:"goal"`         // Main goal, 1-2 sentences (required)
	RootCauses  []string `json:"root_causes"`  // Root causes
	Fixes       []string `json:"fixes"`        // Solutions
	Files       []string `json:"files"`        // Files/modules involved
	Entities    []string `json:"entities"`     // Related entity IDs (Go-validated)
	Corrections []string `json:"corrections"`  // User redirects/corrections
	Patterns    []string `json:"patterns"`     // Lessons for future agents
}

const maxExtractedBytes = 12000 // ~3-4K tokens, fits Gemma 12B prompt processing within timeout
const minGoalRunes = 5

// GenerateL2Summary produces a structured JSON summary of a session using the AI Summarizer.
// Returns "" if summarizer is nil or AI call fails (graceful degradation).
func GenerateL2Summary(messages []map[string]any, review ReviewResult, summarizer ai.Summarizer) string {
	if summarizer == nil {
		return ""
	}

	extracted := extractSessionText(messages)
	if len(extracted) < 100 {
		return "" // too short to summarize
	}

	instruction, text := buildL2Prompt(extracted, review)
	var raw string
	var err error
	if js, ok := summarizer.(interface{ SummarizeJSON(string, string) (string, error) }); ok {
		raw, err = js.SummarizeJSON(text, instruction)
	} else {
		raw, err = summarizer.Summarize(text, instruction)
	}
	if err != nil || raw == "" {
		return "" // model unavailable, degrade gracefully
	}

	return parseL2Response(raw)
}

// extractSessionText filters and truncates messages using density-based strategy.
// S-level: all user messages (goals, corrections, direction)
// A-level: assistant conclusions (>200 runes, anchor keywords), kept reverse-chronological
// B-level: assistant analysis process, kept chronological if space allows
// C-level: emoji-prefixed tool logs, compilation output, hex dumps — discarded
func extractSessionText(messages []map[string]any) string {
	type keptMsg struct {
		role    string
		content string
		level   byte // 'S', 'A', 'B'
	}

	// Phase 1: classify
	var items []keptMsg
	for _, m := range messages {
		role := u.Str(m["role"])
		content := u.Str(m["content"])
		if content == "" {
			continue
		}

		if shouldSkipMessage(role, content) {
			continue
		}

		if role == "user" {
			items = append(items, keptMsg{role: "user", content: content, level: 'S'})
			continue
		}

		if role == "assistant" {
			runes := utf8.RuneCountInString(content)
			if runes < 80 {
				continue // too short, likely tool call prefix or brief ack
			}
			if isConclusion(content) {
				items = append(items, keptMsg{role: "assistant", content: content, level: 'A'})
			} else {
				items = append(items, keptMsg{role: "assistant", content: content, level: 'B'})
			}
		}
	}

	if len(items) == 0 {
		return ""
	}

	// Phase 2: format S-level (all user messages) first
	var buf strings.Builder
	sBudget := maxExtractedBytes
	for _, k := range items {
		if k.level == 'S' {
			buf.WriteString("[user] ")
			buf.WriteString(k.content)
			buf.WriteString("\n\n")
			sBudget -= len(k.content) + 9
		}
	}

	// Phase 3: A-level, reverse chronological (most recent conclusions first)
	var aList []keptMsg
	for _, k := range items {
		if k.level == 'A' {
			aList = append(aList, k)
		}
	}
	// A-level items are already in chronological order from the loop above.
	// We want most recent first, so iterate in reverse.
	for i := len(aList) - 1; i >= 0; i-- {
		k := aList[i]
		size := len(k.content) + 14
		if sBudget-size < 0 {
			break
		}
		buf.WriteString("[assistant] ")
		buf.WriteString(k.content)
		buf.WriteString("\n\n")
		sBudget -= size
	}

	// Phase 4: B-level, chronological, fill remaining budget
	for _, k := range items {
		if k.level != 'B' {
			continue
		}
		size := len(k.content) + 14
		if sBudget-size < 0 {
			buf.WriteString("[... truncated ...]\n")
			break
		}
		buf.WriteString("[assistant] ")
		buf.WriteString(k.content)
		buf.WriteString("\n\n")
		sBudget -= size
	}

	result := buf.String()
	if len(result) > maxExtractedBytes {
		// Truncate on rune boundary to avoid splitting multi-byte characters
		runes := []rune(result)
		byteCount := 0
		cut := 0
		for _, r := range runes {
			rl := utf8.RuneLen(r)
			if byteCount+rl > maxExtractedBytes {
				break
			}
			byteCount += rl
			cut++
		}
		result = string(runes[:cut])
	}
	return result
}

// isConclusion checks if an assistant message looks like a conclusion/finding.
func isConclusion(content string) bool {
	anchors := []string{"根因", "结论", "修复", "总结", "最终", "root cause", "conclusion", "fix"}
	for _, a := range anchors {
		if strings.Contains(strings.ToLower(content), strings.ToLower(a)) {
			return true
		}
	}
	return false
}

// shouldSkipMessage filters out tool logs, MCP internal lines, and noise.
func shouldSkipMessage(role, content string) bool {
	if role == "tool" {
		return true
	}
	if IsMCPLog(content) {
		return true
	}
	if role == "assistant" {
		emojiPrefices := []string{
			"🔧", "📝", "👁", "🔍", "📡", "🛠", "💭",
			"🗑", "📂", "🌐", "❓", "🤖", "📋", "🆕",
		}
		for _, p := range emojiPrefices {
			if strings.HasPrefix(content, p) {
				return true
			}
		}
	}
	return false
}

const defaultL2Prompt = `You are a project knowledge extractor. Analyze an AI coding agent's session record.
Answer the following 7 questions in Chinese. Output ONLY a valid JSON object.

IMPORTANT: Every field must be a string or array of strings. Never nest a JSON object inside a string.
Goal must be plain text like "修复跨平台密友同步问题", never a JSON string like "{\"goal\":\"...\"}".

Entity ID format — look for patterns like task-20260615-172610-abcdef, bug-20260624-114337-813aa9,
commit-20260623-093457-67c970, plan-20260414-092344-56abbf in the session text.
Extract exact IDs into the entities array. Empty array [] if none found.

Questions:
1. goal: What was the primary goal? (1-2 Chinese sentences, plain text)
2. root_causes: What were the root causes? (array of Chinese strings)
3. fixes: What solutions were applied? (array of Chinese strings)
4. files: What files or modules were involved? (array of paths)
5. entities: AIPM entity IDs found in the text? Look for patterns like task-20260615-172610-abcdef, bug-20260624-114337-813aa9, commit-20260623-093457-67c970. Copy EXACT IDs found, or [].
6. corrections: Did the user correct the agent? How? (array of Chinese strings)
7. patterns: What lessons for future agents? (array of Chinese strings)

Output format:
{"goal":"...","root_causes":["..."],"fixes":["..."],"files":["..."],"entities":["..."],"corrections":["..."],"patterns":["..."]}`

// loadL2Prompt reads the L2 prompt from .pmai/config/l2_prompt.txt.
// Falls back to defaultL2Prompt if the file does not exist.
func loadL2Prompt() string {
	pmaiDir, err := db.RuntimeDir()
	if err != nil {
		return defaultL2Prompt
	}
	path := filepath.Join(pmaiDir, "config", "l2_prompt.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultL2Prompt
	}
	return string(data)
}

// buildL2Prompt constructs the system instruction and user text for the AI call.
func buildL2Prompt(extracted string, review ReviewResult) (instruction, text string) {
	var ctx strings.Builder
	ctx.WriteString("B1 review context:\n")
	ctx.WriteString("- Intent: " + review.Intent + "\n")
	ctx.WriteString("- Quality score: " + u.Itoa(review.QualityScoreValue()) + "\n")
	ctx.WriteString("- Tools used: " + strings.Join(review.MCPTools, ", ") + "\n")
	ctx.WriteString("- Entity refs: " + review.EntityRefsJSON() + "\n")
	// Collect file paths from layer0 edges
	var files []string
	for _, e := range review.Layer0Edges {
		if e.Type == "file_overlap" {
			files = append(files, e.To)
		}
	}
	ctx.WriteString("- Files mentioned: " + strings.Join(files, ", ") + "\n")

	if len(review.CommitsInWindow) > 0 {
		ctx.WriteString("- Commits in time window:\n")
		for _, c := range review.CommitsInWindow {
			ctx.WriteString("  " + c.Title + "\n")
		}
	}

	instruction = loadL2Prompt()

	// Few-shot example — shows model the expected output format
	example := `\n\n---\nExample session:\n[user] 修复登录页面在iOS上崩溃的问题\n[assistant] 根因是 NSLayoutConstraint 在 iOS17 上的行为变化...\n\nCorrect JSON output:\n{"goal":"修复登录页面在iOS17上崩溃的问题","root_causes":["NSLayoutConstraint 在 iOS17 上的行为变化导致约束冲突"],"fixes":["替换为 UIStackView 布局"],"files":["LoginViewController.swift","LoginView.xib"],"entities":["bug-20231015-143022-abcdef"],"corrections":["用户指出应优先使用 UIStackView 而非修复旧约束"],"patterns":["iOS 大版本升级后应优先验证 AutoLayout 行为"]}\n---\n\nNow analyze the REAL session below. Output ONLY the JSON:` + "\n\n"

	text = ctx.String() + example + "Session messages:\n\n" + extracted
	return
}

// parseL2Response validates and normalizes the AI JSON response.
// Uses map[string]any to preserve extra fields from customized prompts.
// Falls back gracefully on malformed output.
func parseL2Response(raw string) string {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		fallback := SessionL2Summary{Goal: u.TruncateStr(raw, 200)}
		b, _ := json.Marshal(fallback)
		return string(b)
	}

	// Extract required fields for validation
	goal, _ := parsed["goal"].(string)
	rootCauses := toStringSlice(parsed["root_causes"])

	// Quality gate: goal must be substantive and root_causes non-empty
	if utf8.RuneCountInString(goal) < minGoalRunes || len(rootCauses) == 0 {
		fallback := SessionL2Summary{Goal: u.TruncateStr(raw, 200)}
		b, _ := json.Marshal(fallback)
		return string(b)
	}

	// Normalize known array fields to empty if nil/missing
	for _, field := range []string{"root_causes", "fixes", "files", "entities", "corrections", "patterns"} {
		if parsed[field] == nil {
			parsed[field] = []any{}
		}
	}

	// Validate entity IDs: discard fake/hallucinated IDs
	if entities, ok := parsed["entities"].([]any); ok && len(entities) > 0 {
		valid := make([]any, 0, len(entities))
		for _, eid := range entities {
			eidStr, _ := eid.(string)
			parts := strings.SplitN(eidStr, "-", 4)
			if len(parts) < 4 {
				continue
			}
			if store.EntityExists(parts[0], eidStr) {
				valid = append(valid, eidStr)
			}
		}
		parsed["entities"] = valid
	}

	b, _ := json.Marshal(parsed)
	return string(b)
}

// toStringSlice converts a parsed JSON array to []string.
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
