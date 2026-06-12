package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	pmdb "aipmc/db"
	"aipmc/store"
)

// ProcessOpencodeHook reads the OpenCode hook stdin JSON and saves to discussion_log.
// Called via: aipmc hook-opencode
//
// Events captured:
//   - message.updated   → user/assistant messages
//   - tool.execute.after → tool usage (Bash, Read, Write, Edit, Grep, etc.)
//   - session.idle      → session end marker
func ProcessOpencodeHook() {
	now := time.Now().Format("2006-01-02T15:04:05.000")
	data, _ := io.ReadAll(os.Stdin)

	if os.Getenv("AIPM_DEBUG_HOOK") != "" {
		dumpRawHook("opencode", now, data)
	}

	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "[aipm-opencode %s] ", now)
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}

	// Catch panics so a bug never crashes the parent process.
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC: %v\n%s", r, string(debug.Stack()))
			os.Exit(0)
		}
	}()

	// logf("hook called, stdin=%d bytes", len(data))
	if len(data) < 10 {
		os.Exit(0)
	}

	// Dedup: OpenCode fires multiple message.updated events for the same
	// message (start + complete). Track recent message IDs to skip duplicates.
	seenMsgIDs := map[string]bool{}
	isDuplicateMessage := func(id string) bool {
		if id == "" {
			return false
		}
		if seenMsgIDs[id] {
			return true
		}
		seenMsgIDs[id] = true
		// Keep map bounded
		if len(seenMsgIDs) > 200 {
			for k := range seenMsgIDs {
				delete(seenMsgIDs, k)
				break
			}
		}
		return false
	}

	// Parse common fields. OpenCode hooks follow a format similar to
	// Claude Code hooks but with opencode-specific event names and field names.
	// We try multiple candidate field names for robustness.
	var raw struct {
		Event     string `json:"hook_event_name"`
		SessionID string `json:"session_id"`

		// message.updated — try multiple field shapes
		// Format A (flat): role + content at top level
		Role    string `json:"role"`
		Content string `json:"content"`
		// Format B (nested): message object
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Text    string `json:"text"`
		} `json:"message"`

		// tool.execute.after — tool info
		ToolName     string          `json:"tool_name"`
		ToolInput    json.RawMessage `json:"tool_input"`
		ToolResponse json.RawMessage `json:"tool_response"`
		ToolOutput   json.RawMessage `json:"tool_output"`
		ToolResult   json.RawMessage `json:"tool_result"`

		// Additional fields from OpenCode's event payload
		TurnID string `json:"turn_id"`
		Model  string `json:"model"`

		// Raw event for passthrough
		RawInput  json.RawMessage `json:"_raw_input"`
		RawOutput json.RawMessage `json:"_raw_output"`
		RawEvent  json.RawMessage `json:"_raw"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		logf("JSON parse FAILED: %v", err)
		os.Exit(0)
	}

	// Resolve session_id: top-level session_id is set by our plugin bridge,
	// but may also be found in _raw_input.sessionID (opencode uses camelCase).
	sid := resolveSessionID(raw.SessionID, raw.RawInput, raw.RawEvent)

	// logf("event=%s session=%s", raw.Event, sid)

	switch raw.Event {
	case "message.part.updated", "message.updated":
		// Resolve role and content from multiple possible field locations.
		role := firstNonEmpty(raw.Role, raw.Message.Role)
		content := firstNonEmpty(raw.Content, raw.Message.Content, raw.Message.Text)

		// If top-level role/content are empty, try parsing from _raw nested structure
		if role == "" || content == "" {
			if len(raw.RawEvent) > 0 {
				var oe struct {
					Properties struct {
						Part struct {
							Type    string `json:"type"`
							Text    string `json:"text"`
							Role    string `json:"role"`
							Content string `json:"content"`
						} `json:"part"`
						Info struct {
							Role string `json:"role"`
						} `json:"info"`
						SessionID string `json:"sessionID"`
					} `json:"properties"`
				}
				if json.Unmarshal(raw.RawEvent, &oe) == nil {
					if role == "" {
						role = firstNonEmpty(oe.Properties.Part.Role, oe.Properties.Info.Role)
					}
					if content == "" && oe.Properties.Part.Type == "text" {
						content = firstNonEmpty(oe.Properties.Part.Text, oe.Properties.Part.Content)
					}
				}
			}
		}

		// Skip empty content — OpenCode fires message.updated on LLM start
		// (before content is available) and on completion, causing duplicates.
		if content == "" {
			break
		}

		// Extract message ID for dedup. OpenCode sends multiple message.updated
		// events with the same info.id (start + complete events).
		msgID := extractOpencodeMessageID(raw.RawEvent)
		if isDuplicateMessage(msgID) {
			logf("message %s skipped (duplicate)", raw.Event)
			break
		}

		// Default role to assistant for text messages
		if role == "" {
			role = "assistant"
		}

		role = normalizeRole(role)
		if role == "" {
			break
		}

		eventType := "message_updated"
		if raw.Event == "message.part.updated" {
			eventType = "message_part_updated"
		}
		meta := buildFullMeta(eventType, data)
		if _, err := store.LogDiscussion(sid, role, "opencode", content, meta); err != nil {
			logf("%s log FAILED (role=%s): %v", raw.Event, role, err)
		} else {
			logf("%s logged (role=%s, %d chars)", raw.Event, role, len(content))
		}

	case "tool.execute.after":
		if raw.ToolName == "" {
			logf("tool.execute.after — empty tool_name, skipped")
			break
		}

		// Normalize tool name (strip prefixes)
		normalizedName := raw.ToolName
		normalizedName = strings.TrimPrefix(normalizedName, "mcp__")
		normalizedName = strings.TrimPrefix(normalizedName, "aipm__")

		// Use tool_response or tool_output or tool_result — whichever has data
		toolResp := raw.ToolResponse
		if len(toolResp) == 0 {
			toolResp = raw.ToolOutput
		}
		if len(toolResp) == 0 {
			toolResp = raw.ToolResult
		}

		content := buildOpencodeToolContent(normalizedName, raw.ToolInput, toolResp)
		meta := buildFullMeta("tool_execute_after", data)

		if content != "" {
			if _, err := store.LogDiscussion(sid, "assistant", "opencode", content, meta); err != nil {
				logf("tool.execute.after %s log FAILED: %v", raw.ToolName, err)
			} else {
				logf("tool.execute.after %s logged", raw.ToolName)
			}
		} else {
			logf("tool.execute.after %s — empty content, skipped", raw.ToolName)
		}

	case "session.idle":
		// Session idle — log a lightweight marker so the UI knows the
		// session boundary. Not strictly required but helpful for resumption.
		meta := buildFullMeta("session_idle", data)
		if _, err := store.LogDiscussion(sid, "assistant", "opencode", "⏸ Session idle", meta); err != nil {
			logf("session.idle log FAILED: %v", err)
		} else {
			logf("session.idle logged")
		}

	default:
		logf("unhandled event=%s, ignored", raw.Event)
	}
}

// normalizeRole maps opencode role strings to standard user/assistant.
func normalizeRole(role string) string {
	switch strings.ToLower(role) {
	case "user", "human":
		return "user"
	case "assistant", "ai", "agent", "model":
		return "assistant"
	case "tool":
		return "assistant" // tool usage is assistant action
	default:
		return ""
	}
}

// buildOpencodeToolContent builds a human-readable description for an OpenCode tool call.
func buildOpencodeToolContent(toolName string, toolInput, toolResp json.RawMessage) string {
	ti := parseToolInput(toolInput)

	switch {
	case toolName == "bash" || toolName == "Bash" || toolName == "BashTool":
		cmd := ti["command"]
		if cmd == "" && len(toolInput) > 0 {
			var rawCmd string
			if json.Unmarshal(toolInput, &rawCmd) == nil && rawCmd != "" {
				cmd = rawCmd
			}
		}
		if cmd == "" {
			cmd = string(toolInput)
		}
		cmdPreview := truncateText(cmd, 150)
		result := "🔧 " + cmdPreview

		// Try to extract output from tool_response
		output := extractBashOutput(toolResp)
		if output != "" {
			result += "\n  → " + strings.TrimSpace(truncateText(output, 120))
		}
		if ec := extractExitCode(toolResp); ec != 0 {
			result += fmtExitCode(ec)
		}
		return result

	case toolName == "read" || toolName == "Read" || toolName == "ReadTool":
		fp := ti["file_path"]
		if fp == "" {
			fp = ti["filePath"]
		}
		if fp == "" {
			fp = ti["path"]
		}
		if fp != "" {
			result := "👁 " + fp
			lc := extractLinesCount(toolResp)
			if lc > 0 {
				result += " (" + uitoa(lc) + " lines)"
			}
			return result
		}
		return "👁 read"

	case toolName == "write" || toolName == "Write" || toolName == "WriteTool":
		fp := ti["file_path"]
		if fp == "" {
			fp = ti["filePath"]
		}
		if fp == "" {
			fp = ti["path"]
		}
		if fp != "" {
			if isNewFileFromContent(ti["content"], toolResp) {
				return "🆕 " + fp
			}
			return "📝 " + fp
		}
		return "📝 write"

	case toolName == "edit" || toolName == "Edit" || toolName == "EditTool":
		fp := ti["file_path"]
		if fp == "" {
			fp = ti["filePath"]
		}
		if fp == "" {
			fp = ti["path"]
		}
		result := ""
		if fp != "" {
			result = "📝 " + fp
		} else {
			result = "📝 edit"
		}
		oldStr := firstNonEmpty(ti["old_string"], ti["oldString"], ti["old_str"])
		newStr := firstNonEmpty(ti["new_string"], ti["newString"], ti["new_str"])
		if oldStr != "" {
			result += "\n- " + strings.TrimSpace(oldStr)
		}
		if newStr != "" {
			result += "\n+ " + strings.TrimSpace(newStr)
		}
		return result

	case toolName == "grep" || toolName == "Grep" || toolName == "GrepTool" ||
		toolName == "glob" || toolName == "Glob" || toolName == "GlobTool":
		pattern := ti["pattern"]
		if pattern == "" {
			pattern = ti["query"]
		}
		if pattern != "" {
			pattern = truncateText(pattern, 80)
			return "🔍 \"" + pattern + "\""
		}
		if toolName == "glob" || toolName == "Glob" || toolName == "GlobTool" {
			g := ti["pattern"]
			if g == "" {
				g = ti["glob"]
			}
			if g != "" {
				return "🔍 glob \"" + truncateText(g, 80) + "\""
			}
		}
		return "🔍 " + toolName

	case toolName == "ls" || toolName == "LS" ||
		strings.HasSuffix(toolName, "_ls") || strings.HasSuffix(toolName, "_list_directory"):
		fp := ti["dir_path"]
		if fp == "" {
			fp = ti["path"]
		}
		if fp == "" {
			fp = ti["file_path"]
		}
		if fp != "" {
			return "📂 " + fp
		}
		return "📂 " + toolName

	case toolName == "web_search" || toolName == "WebSearch" || toolName == "webfetch" || toolName == "WebFetch":
		q := ti["query"]
		if q == "" {
			q = ti["q"]
		}
		if q != "" {
			q = truncateText(q, 80)
			return "🌐 \"" + q + "\""
		}
		if url := ti["url"]; url != "" {
			return "🌐 " + truncateText(url, 80)
		}
		return "🌐 " + toolName

	case toolName == "task" || toolName == "Task":
		desc := ti["description"]
		if desc == "" {
			desc = ti["prompt"]
		}
		if desc != "" {
			return "🤖 Task: " + truncateText(desc, 100)
		}
		return "🤖 Task"

	case toolName == "question" || toolName == "Question":
		q := ti["question"]
		if q == "" {
			q = ti["questions"]
		}
		if q != "" {
			return "❓ " + truncateText(q, 100)
		}
		return "❓ Question"

	case toolName == "todowrite" || toolName == "TodoWrite":
		return "📋 Todo updated"

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

	default:
		// Generic display for unknown tools
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

// extractBashOutput extracts stdout/stderr from a bash tool response.
func extractBashOutput(toolResp json.RawMessage) string {
	if len(toolResp) == 0 {
		return ""
	}
	var resp struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
		Output string `json:"output"`
		Result string `json:"result"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		if resp.Stdout != "" {
			return resp.Stdout
		}
		if resp.Stderr != "" {
			return resp.Stderr
		}
		if resp.Output != "" {
			return resp.Output
		}
		if resp.Result != "" {
			return resp.Result
		}
	}
	// Try plain string
	var s string
	if json.Unmarshal(toolResp, &s) == nil {
		return s
	}
	return ""
}

// extractLinesCount extracts a line count from a read tool response.
func extractLinesCount(toolResp json.RawMessage) int {
	if len(toolResp) == 0 {
		return 0
	}
	var resp struct {
		LinesCount int `json:"linesCount"`
		LineCount  int `json:"line_count"`
		Lines      int `json:"lines"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		if resp.LinesCount > 0 {
			return resp.LinesCount
		}
		if resp.LineCount > 0 {
			return resp.LineCount
		}
		if resp.Lines > 0 {
			return resp.Lines
		}
	}
	// Count newlines in string content
	var s string
	if json.Unmarshal(toolResp, &s) == nil && s != "" {
		return strings.Count(s, "\n") + 1
	}
	return 0
}

// isNewFileFromContent checks if the file is new (no prior content).
// For opencode, a Write to a file where tool_response indicates "created"
// or the originalFile is empty means it's a new file.
func isNewFileFromContent(content string, toolResp json.RawMessage) bool {
	if len(toolResp) == 0 && content == "" {
		return false
	}
	// Try structured response
	var resp struct {
		OriginalFile string `json:"originalFile"`
		Created      bool   `json:"created"`
		IsNew        bool   `json:"isNew"`
		IsNewFile    bool   `json:"isNewFile"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		if resp.Created || resp.IsNew || resp.IsNewFile {
			return true
		}
		if resp.OriginalFile == "" && content != "" {
			return true
		}
	}
	return false
}

// uitoa converts int to string without importing strconv (uses fmt).
func uitoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// extractOpencodeMessageID extracts the message ID from an OpenCode _raw event.
// Returns "" if no identifiable message ID is found.
func extractOpencodeMessageID(rawEvent json.RawMessage) string {
	if len(rawEvent) == 0 {
		return ""
	}
	var re struct {
		Properties struct {
			Info struct {
				ID string `json:"id"`
			} `json:"info"`
			Part struct {
				MessageID string `json:"messageID"`
			} `json:"part"`
		} `json:"properties"`
	}
	if json.Unmarshal(rawEvent, &re) == nil {
		if re.Properties.Info.ID != "" {
			return re.Properties.Info.ID
		}
		if re.Properties.Part.MessageID != "" {
			return re.Properties.Part.MessageID
		}
	}
	return ""
}

// resolveSessionID extracts the session ID from the hook payload.
func resolveSessionID(topLevelSessionID string, rawInput, rawEvent json.RawMessage) string {
	if topLevelSessionID != "" {
		return topLevelSessionID
	}
	if len(rawInput) > 0 {
		var ri struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(rawInput, &ri) == nil && ri.SessionID != "" {
			return ri.SessionID
		}
	}
	if len(rawEvent) > 0 {
		// Try properties.sessionID (opencode's message event structure)
		var rp struct {
			Properties struct {
				SessionID string `json:"sessionID"`
			} `json:"properties"`
		}
		if json.Unmarshal(rawEvent, &rp) == nil && rp.Properties.SessionID != "" {
			return rp.Properties.SessionID
		}
		// Try top-level sessionID or id
		var re struct {
			SessionID string `json:"sessionID"`
			ID        string `json:"id"`
		}
		if json.Unmarshal(rawEvent, &re) == nil {
			if re.SessionID != "" {
				return re.SessionID
			}
			if re.ID != "" {
				return re.ID
			}
		}
	}
	return ""
}

// ---- OpenCode hook setup ----

// SetupOpencodeHooks writes OpenCode hook configuration to .opencode/hooks.json.
func SetupOpencodeHooks(commandPath string) error {
	runtimeDir, _ := pmdb.RuntimeDir()
	projectRoot := filepath.Dir(runtimeDir)
	opencodeDir := filepath.Join(projectRoot, ".opencode")
	hooksPath := filepath.Join(opencodeDir, "hooks.json")

	cfg := map[string]any{}
	if data, err := os.ReadFile(hooksPath); err == nil && len(data) > 0 {
		json.Unmarshal(data, &cfg)
	}

	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	hookCmd := shellQuote(commandPath) + " hook-opencode"
	makeEntry := func() []any {
		return []any{
			map[string]any{
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": hookCmd,
					},
				},
			},
		}
	}

	hooks["message.updated"] = makeEntry()
	hooks["tool.execute.after"] = makeEntry()
	hooks["session.idle"] = makeEntry()
	cfg["hooks"] = hooks

	os.MkdirAll(opencodeDir, 0755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(hooksPath, data, 0644); err != nil {
		return fmt.Errorf("write hooks.json: %w", err)
	}
	fmt.Printf("  ✅ OpenCode hooks configured → %s\n", hooksPath)
	return nil
}
