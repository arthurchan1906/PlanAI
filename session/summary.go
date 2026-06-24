package session

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"aipmc/ai"
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

const maxExtractedBytes = 25000 // ~6-7K tokens for small model budget
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
	raw, err := summarizer.Summarize(text, instruction)
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

	instruction = `You are a project knowledge extractor. Analyze an AI coding agent's session record.
Answer the following 7 questions in Chinese. Output ONLY a valid JSON object.

Questions:
1. goal: What was the user's primary goal? (1-2 sentences)
2. root_causes: What were the root causes of the problems? (array of strings)
3. fixes: What solutions were applied? (array of strings)
4. files: What files or modules were involved? (array of paths)
5. entities: Which AIPM entity IDs (task-*, bug-*, commit-*, decision-*, plan-*) are related? (array of IDs)
6. corrections: Did the user correct or redirect the agent? How? (array of strings)
7. patterns: What lessons should future agents learn? (array of strings)

Output format:
{"goal":"...","root_causes":["..."],"fixes":["..."],"files":["..."],"entities":["..."],"corrections":["..."],"patterns":["..."]}`

	text = ctx.String() + "\n\nSession messages:\n\n" + extracted
	return
}

// parseL2Response validates and normalizes the AI JSON response.
// Falls back gracefully on malformed output.
func parseL2Response(raw string) string {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var l2 SessionL2Summary
	if err := json.Unmarshal([]byte(cleaned), &l2); err != nil {
		// Fallback: use raw text as goal
		fallback := SessionL2Summary{Goal: u.TruncateStr(raw, 200)}
		b, _ := json.Marshal(fallback)
		return string(b)
	}

	// Normalize nil slices to empty
	if l2.RootCauses == nil {
		l2.RootCauses = []string{}
	}
	if l2.Fixes == nil {
		l2.Fixes = []string{}
	}
	if l2.Files == nil {
		l2.Files = []string{}
	}
	if l2.Entities == nil {
		l2.Entities = []string{}
	}
	if l2.Corrections == nil {
		l2.Corrections = []string{}
	}
	if l2.Patterns == nil {
		l2.Patterns = []string{}
	}

	// Validate required field
	if utf8.RuneCountInString(l2.Goal) < minGoalRunes {
		fallback := SessionL2Summary{Goal: u.TruncateStr(raw, 200)}
		b, _ := json.Marshal(fallback)
		return string(b)
	}

	// Validate entity IDs: discard fake/hallucinated IDs
	if len(l2.Entities) > 0 {
		valid := make([]string, 0, len(l2.Entities))
		for _, eid := range l2.Entities {
			// entity IDs have format: type-YYYYMMDD-HHMMSS-xxxxxx
			parts := strings.SplitN(eid, "-", 4)
			if len(parts) < 4 {
				continue // clearly not a valid entity ID
			}
			if store.EntityExists(parts[0], eid) {
				valid = append(valid, eid)
			}
			// Silently discard hallucinated IDs
		}
		l2.Entities = valid
	}

	b, _ := json.Marshal(l2)
	return string(b)
}
