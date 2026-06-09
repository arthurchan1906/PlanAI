package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"
)

// processGeminiHook reads the Gemini CLI hook stdin JSON and saves to discussion_log.
// Called via: aipmc hook-gemini
//
// Strategy: store the COMPLETE raw JSON as metadata so nothing is lost.
// The UI / analysis layer can parse and display it later.
func processGeminiHook() {
	now := time.Now().Format("2006-01-02T15:04:05.000")
	data, _ := io.ReadAll(os.Stdin)

	// Always persist raw hook JSON for debugging.
	dumpRawHook("gemini", now, data)

	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "[aipm-gemini %s] ", now)
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}

	// Catch panics so a bug never crashes the parent process.
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC: %v\n%s", r, string(debug.Stack()))
			os.Exit(0)
		}
	}()

	logf("hook called, stdin=%d bytes", len(data))
	if len(data) < 10 {
		logf("stdin too short, exiting")
		os.Exit(0)
	}

	// Parse just enough to route the event. Everything else goes into metadata as raw JSON.
	var raw struct {
		Event     string          `json:"hook_event_name"`
		SessionID string          `json:"session_id"`
		Prompt    string          `json:"prompt"`
		Response  string          `json:"prompt_response"`
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
		ToolResp  json.RawMessage `json:"tool_response"`
		// CWD and timestamp for context
		CWD       string `json:"cwd"`
		Timestamp string `json:"timestamp"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		logf("JSON parse FAILED: %v — raw(first 200): %s", err, safePrefix(string(data), 200))
		os.Exit(0)
	}
	logf("event=%s tool=%s session=%s", raw.Event, raw.ToolName, raw.SessionID)

	switch raw.Event {
	case "BeforeAgent":
		if raw.Prompt != "" {
			meta := buildFullMeta("before_agent", data)
			if _, err := logDiscussion(raw.SessionID, "user", "gemini-cli", raw.Prompt, meta); err != nil {
				logf("BeforeAgent log FAILED: %v", err)
			} else {
				logf("BeforeAgent logged (%d chars)", len(raw.Prompt))
			}
		}

	case "AfterAgent":
		if raw.Response != "" {
			// Strip the garbled duplicate that Gemini CLI appends.
			// The duplicate begins right after a closing ``` that is NOT
			// followed by \n:  "...``` 重复内容..."  (should be "...```\n...")
			// Valid fences ("```\n" or "```text\n") are left alone.
			clean := raw.Response
			if idx := garbledFenceIndex(clean); idx >= 0 {
				clean = clean[:idx]
			}
			meta := buildFullMeta("after_agent", data)
			if _, err := logDiscussion(raw.SessionID, "assistant", "gemini-cli", clean, meta); err != nil {
				logf("AfterAgent log FAILED: %v", err)
			} else {
				logf("AfterAgent logged (%d/%d chars)", len(clean), len(raw.Response))
			}
		}

	case "BeforeTool":
		// Skip BeforeTool — only record AfterTool to avoid duplicates.
		logf("BeforeTool %s skipped (only AfterTool is recorded)", raw.ToolName)

	case "AfterTool":
		if raw.ToolName == "" {
			break
		}

		// Normalize MCP tool names: strip server prefix
		raw.ToolName = strings.TrimPrefix(raw.ToolName, "mcp__aipm__")
		raw.ToolName = strings.TrimPrefix(raw.ToolName, "mcp_aipm_")

		content := buildToolContent(raw.ToolName, raw.ToolInput, raw.ToolResp)
		meta := buildFullMeta("after_tool", data)

		if content != "" {
			if _, err := logDiscussion(raw.SessionID, "assistant", "gemini-cli", content, meta); err != nil {
				logf("AfterTool %s log FAILED: %v", raw.ToolName, err)
			} else {
				logf("AfterTool %s logged", raw.ToolName)
			}
		} else {
			logf("AfterTool %s — empty content, skipped", raw.ToolName)
		}
	}
}

// garbledFenceIndex finds where the garbled duplicate begins.
//
// Gemini CLI's prompt_response is the clean text followed by a garbled copy
// with random extra spaces. When you strip ALL spaces, the two halves are
// identical. So the midpoint of the space-normalized text IS the boundary.
func garbledFenceIndex(s string) int {
	if len(s) < 200 {
		return -1
	}

	// Build space-normalized version, tracking original positions.
	var mapping []int // normalized-index → original-index
	var norm strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' {
			mapping = append(mapping, i)
			norm.WriteByte(s[i])
		}
	}

	n := norm.Len()
	if n < 80 {
		return -1
	}

	// The clean text and garbled copy are the same (minus spaces).
	// Midpoint of normalized text ≈ boundary in original text.
	// Use 48% to stay slightly conservative (don't cut into clean text).
	mid := n * 48 / 100
	if mid >= len(mapping) {
		return -1
	}
	// Ensure the cut point is at a valid UTF-8 boundary.
	// If mapping[mid] falls inside a multi-byte character, walk back
	// to the start of that character so we never split a rune.
	idx := mapping[mid]
	for idx > 0 && !utf8.RuneStart(s[idx]) {
		idx--
	}
	return idx
}

// buildFullMeta builds a metadata JSON from the complete raw hook input.
// We re-serialize to ensure valid JSON and add a type tag.
// For AfterTool events, also extracts standardized diff fields so the frontend
// can render Gemini data the same as Claude Code data.
func buildFullMeta(eventType string, rawJSON []byte) string {
	var raw map[string]any
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		return fmt.Sprintf(`{"type":"%s","raw":"%s"}`, eventType, escapeJSON(string(rawJSON)))
	}
	raw["_type"] = eventType
	delete(raw, "transcript_path")

	// Enrich AfterTool events with standardized diff metadata
	if eventType == "after_tool" {
		enrichGeminiMeta(raw)
	}

	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(b)
}

// enrichGeminiMeta extracts standardized diff fields from Gemini CLI's
// nested tool_response.returnDisplay format and promotes them to top-level
// metadata keys so the frontend can render diffs using the same code paths
// as Claude Code (DiffPanel + renderUnifiedHunks).
func enrichGeminiMeta(raw map[string]any) {
	toolResp, _ := raw["tool_response"].(map[string]any)
	if toolResp == nil {
		return
	}

	rdRaw := toolResp["returnDisplay"]
	if rdRaw == nil {
		return
	}
	rd, _ := rdRaw.(map[string]any)
	if rd == nil {
		return
	}

	fp, _ := rd["filePath"].(string)
	if fp == "" {
		fp, _ = rd["fileName"].(string)
	}
	if fp == "" {
		if ti, ok := raw["tool_input"].(map[string]any); ok {
			fp, _ = ti["file_path"].(string)
			if fp == "" {
				fp, _ = ti["path"].(string)
			}
		}
	}

	// Case 1: New file
	if isNew, ok := rd["isNewFile"].(bool); ok && isNew {
		raw["type"] = "new_file"
		raw["file_path"] = fp
		return
	}

	// Case 2: Edit with unified diff
	fileDiff, _ := rd["fileDiff"].(string)
	if fileDiff == "" {
		return
	}

	raw["type"] = "edit"
	raw["file_path"] = fp

	hunks := parseDiffToHunks(fileDiff)
	if len(hunks) > 0 {
		raw["hunks"] = hunks
	}

	if ti, ok := raw["tool_input"].(map[string]any); ok {
		if oldStr, ok := ti["old_string"].(string); ok && oldStr != "" {
			raw["old_string"] = oldStr
		}
		if newStr, ok := ti["new_string"].(string); ok && newStr != "" {
			raw["new_string"] = newStr
		}
	}
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return s
}

// truncateText truncates s to at most maxRunes runes, adding "..." if truncated.
// Unlike truncateStr (which uses byte length), this is safe for multi-byte UTF-8.
func truncateText(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	count := 0
	for i := range s {
		if count >= maxRunes {
			return s[:i] + "..."
		}
		count++
	}
	return s
}

// buildToolContent builds a concise human-readable description for a tool call.
func buildToolContent(toolName string, toolInput, toolResp json.RawMessage) string {
	ti := parseToolInput(toolInput)
	llmText := extractLLMText(toolResp)

	switch {
	case toolName == "run_shell_command" || toolName == "execute_command":
		cmd := ti["command"]
		if cmd == "" {
			cmd = ti["cmd"]
		}
		if cmd == "" {
			cmd = ti["description"]
		}
		if cmd == "" && len(toolInput) > 0 {
			cmd = string(toolInput)
		}
		cmd = truncateText(cmd, 150)
		result := "🔧 " + cmd
		if llmText != "" {
			result += "\n  → " + strings.TrimSpace(truncateText(llmText, 120))
		}
		if ec := extractExitCode(toolResp); ec != 0 {
			result += fmtExitCode(ec)
		}
		return result

	case toolName == "read_file":
		fp := ti["file_path"]
		if fp == "" {
			fp = ti["dir_path"]
		}
		if fp == "" {
			fp = ti["path"]
		}
		if fp != "" {
			return "👁 " + fp
		}
		return "👁 read_file"

	case toolName == "write_file" || toolName == "write":
		fp := ti["file_path"]
		if fp == "" {
			fp = ti["path"]
		}
		if fp != "" {
			if isNewFile(toolResp) {
				return "🆕 " + fp
			}
			return "📝 " + fp
		}
		return "📝 write_file"

	case toolName == "replace" || toolName == "edit_file" || toolName == "edit":
		fp := ti["file_path"]
		if fp == "" {
			fp = ti["path"]
		}
		result := ""
		if fp != "" {
			result = "📝 " + fp
		} else {
			result = "📝 replace"
		}
		oldStr := ti["old_string"]
		if oldStr == "" {
			oldStr = ti["old_str"]
		}
		newStr := ti["new_string"]
		if newStr == "" {
			newStr = ti["new_str"]
		}
		if oldStr != "" {
			result += "\n- " + strings.TrimSpace(oldStr)
		}
		if newStr != "" {
			result += "\n+ " + strings.TrimSpace(newStr)
		}
		return result

	case toolName == "list_directory":
		fp := ti["dir_path"]
		if fp == "" {
			fp = ti["file_path"]
		}
		if fp == "" {
			fp = ti["path"]
		}
		if fp != "" {
			return "📂 " + fp
		}
		return "📂 list_directory"

	case strings.HasPrefix(toolName, "aipm_"):
		result := "📡 " + toolName
		q := ti["query"]
		if q == "" {
			q = ti["q"]
		}
		if q != "" {
			q = truncateText(q, 60)
			result += " \"" + q + "\""
		}
		return result

	case toolName == "update_topic":
		// Gemini CLI sends title, summary, strategic_intent — not always all three.
		// Prefer title (shortest), then summary, then strategic_intent.
		label := ti["title"]
		if label == "" {
			label = ti["summary"]
		}
		if label == "" {
			label = ti["strategic_intent"]
		}
		if label != "" {
			label = truncateText(label, 100)
			return "📌 " + label
		}
		return "📌 update_topic"

	case toolName == "grep_search":
		pattern := ti["pattern"]
		if pattern != "" {
			pattern = truncateText(pattern, 80)
			return "🔍 \"" + pattern + "\""
		}
		return "🔍 grep_search"

	default:
		label := "🛠 " + toolName
		for _, key := range []string{"query", "pattern", "file_path", "path", "url"} {
			if v := ti[key]; v != "" {
				qv := truncateText(v, 80)
				label += " \"" + qv + "\""
				break
			}
		}
		return label
	}
}

// parseToolInput extracts known fields from tool_input into a flat map.
func parseToolInput(raw json.RawMessage) map[string]string {
	result := make(map[string]string)
	if len(raw) == 0 {
		return result
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return result
	}
	for k, v := range m {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result
}

// isNewFile checks if the tool_response.returnDisplay contains isNewFile: true.
func isNewFile(toolResp json.RawMessage) bool {
	if len(toolResp) == 0 {
		return false
	}
	var resp struct {
		ReturnDisplay struct {
			IsNewFile bool `json:"isNewFile"`
		} `json:"returnDisplay"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		return resp.ReturnDisplay.IsNewFile
	}
	var flat struct {
		IsNewFile bool `json:"isNewFile"`
	}
	if json.Unmarshal(toolResp, &flat) == nil {
		return flat.IsNewFile
	}
	return false
}

// extractLLMText extracts readable text from Gemini CLI's tool_response.
// Handles: plain string, array of {text: "..."}, or {llmContent: ..., returnDisplay: ...}.
func extractLLMText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) == nil {
		var parts []string
		for _, item := range arr {
			if t, ok := item["text"].(string); ok {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "")
	}
	var resp struct {
		LLMContent    json.RawMessage `json:"llmContent"`
		ReturnDisplay json.RawMessage `json:"returnDisplay"`
	}
	if json.Unmarshal(raw, &resp) == nil {
		if len(resp.LLMContent) > 0 {
			return extractLLMText(resp.LLMContent)
		}
		if len(resp.ReturnDisplay) > 0 {
			return extractLLMText(resp.ReturnDisplay)
		}
	}
	var summary struct {
		Summary string `json:"summary"`
	}
	if json.Unmarshal(raw, &summary) == nil && summary.Summary != "" {
		return summary.Summary
	}
	return ""
}

// extractExitCode tries to find an exit code in tool_response JSON.
// Returns 0 if no non-zero exit code is found.
func extractExitCode(toolResp json.RawMessage) int {
	if len(toolResp) == 0 {
		return 0
	}
	// Try top-level exitCode
	var flat struct {
		ExitCode int `json:"exitCode"`
	}
	if json.Unmarshal(toolResp, &flat) == nil && flat.ExitCode != 0 {
		return flat.ExitCode
	}
	// Try nested in returnDisplay
	var nested struct {
		ReturnDisplay struct {
			ExitCode int `json:"exitCode"`
		} `json:"returnDisplay"`
	}
	if json.Unmarshal(toolResp, &nested) == nil && nested.ReturnDisplay.ExitCode != 0 {
		return nested.ReturnDisplay.ExitCode
	}
	return 0
}

// ---- Diff parsing ----

type PatchHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

func parseDiffToHunks(diff string) []PatchHunk {
	var hunks []PatchHunk
	lines := strings.Split(diff, "\n")
	var currentHunk *PatchHunk

	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			if currentHunk != nil {
				hunks = append(hunks, *currentHunk)
			}
			parts := strings.Split(line, " ")
			if len(parts) >= 3 {
				oldPart := strings.TrimPrefix(parts[1], "-")
				newPart := strings.TrimPrefix(parts[2], "+")

				oldNums := strings.Split(oldPart, ",")
				newNums := strings.Split(newPart, ",")

				h := PatchHunk{}
				if _, err := fmt.Sscanf(oldNums[0], "%d", &h.OldStart); err != nil {
					// Malformed hunk header — skip this hunk
					currentHunk = nil
					continue
				}
				if len(oldNums) > 1 {
					if _, err := fmt.Sscanf(oldNums[1], "%d", &h.OldLines); err != nil {
						h.OldLines = 1
					}
				} else {
					h.OldLines = 1
				}
				if _, err := fmt.Sscanf(newNums[0], "%d", &h.NewStart); err != nil {
					currentHunk = nil
					continue
				}
				if len(newNums) > 1 {
					if _, err := fmt.Sscanf(newNums[1], "%d", &h.NewLines); err != nil {
						h.NewLines = 1
					}
				} else {
					h.NewLines = 1
				}

				currentHunk = &h
			}
		} else if currentHunk != nil {
			if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ") {
				currentHunk.Lines = append(currentHunk.Lines, line)
			}
		}
	}
	if currentHunk != nil {
		hunks = append(hunks, *currentHunk)
	}
	return hunks
}

// ---- Gemini CLI hook setup ----

// setupGeminiHooks writes Gemini CLI hook configuration to .gemini/settings.json.
func setupGeminiHooks(commandPath string) error {
	runtimeDir, _ := findRuntimeDir()
	projectRoot := filepath.Dir(runtimeDir)
	settingsPath := filepath.Join(projectRoot, ".gemini", "settings.json")

	cfg := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil && len(data) > 0 {
		json.Unmarshal(data, &cfg)
	}

	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	// Only quote the path if it contains spaces — bare quoting "aipmc"
	// breaks command execution on Windows.
	hookCmd := shellQuote(commandPath) + " hook-gemini"
	hookEntry := []any{
		map[string]any{
			"matcher": "",
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": hookCmd,
				},
			},
		},
	}

	hooks["BeforeAgent"] = hookEntry
	hooks["AfterAgent"] = hookEntry
	hooks["AfterTool"] = hookEntry
	cfg["hooks"] = hooks

	os.MkdirAll(filepath.Dir(settingsPath), 0755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	fmt.Printf("  ✅ Gemini hooks configured → %s\n", settingsPath)
	return nil
}
