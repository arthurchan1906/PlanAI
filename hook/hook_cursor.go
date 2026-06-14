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

// ProcessCursorHook reads the Cursor hook stdin JSON and saves to discussion_log.
// Called via: aipmc hook-cursor
//
// Strategy: store the COMPLETE raw JSON as metadata (like Gemini CLI) so
// nothing is lost. The UI / analysis layer can parse and display it later.
// Additionally enrich tool events with standardized diff fields so the
// frontend DiffPanel can render them the same way as Claude Code data.
//
// Events captured:
//   - beforeSubmitPrompt   → user messages
//   - stop                 → session end marker
//   - postToolUse          → assistant tool usage (Shell, Read, Write, Edit, etc.)
//   - afterFileEdit        → structured file edit data (old_string / new_string)
//   - afterAgentResponse   → assistant text responses
//   - afterAgentThought    → assistant thinking (preview only)
func ProcessCursorHook() {
	now := time.Now().Format("2006-01-02T15:04:05.000")
	data, _ := io.ReadAll(os.Stdin)

	if os.Getenv("AIPM_DEBUG_HOOK") != "" {
		dumpRawHook("cursor", now, data)
	}

	logf := func(format string, args ...any) {
		if os.Getenv("AIPM_DEBUG_HOOK") == "" {
			return
		}
		fmt.Fprintf(os.Stderr, "[aipm-cursor %s] ", now)
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}

	// Catch panics so a bug never crashes the parent Cursor process.
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC: %v\n%s", r, string(debug.Stack()))
			os.Exit(0)
		}
	}()

	if len(data) < 10 {
		os.Exit(0)
	}

	// Parse common fields plus event-specific ones.
	// Cursor uses conversation_id as the stable session identifier,
	// and may also include session_id in some hook payloads.
	var raw struct {
		Event          string `json:"hook_event_name"`
		SessionID      string `json:"session_id"`
		ConversationID string `json:"conversation_id"`
		GenerationID   string `json:"generation_id"`
		Model          string `json:"model"`

		// beforeSubmitPrompt
		Prompt      string `json:"prompt"`
		Attachments []struct {
			Type     string `json:"type"`
			FilePath string `json:"file_path"`
		} `json:"attachments"`

		// stop
		Status    string `json:"status"`
		LoopCount int    `json:"loop_count"`

		// postToolUse
		ToolName    string          `json:"tool_name"`
		ToolUseID   string          `json:"tool_use_id"`
		ToolInput   json.RawMessage `json:"tool_input"`
		ToolResp    json.RawMessage `json:"tool_response"`
		ToolOutput  json.RawMessage `json:"tool_output"`
		Duration    int             `json:"duration"`
		CWD         string          `json:"cwd"`

		// afterFileEdit
		FilePath string          `json:"file_path"`
		Edits    json.RawMessage `json:"edits"`

		// afterAgentResponse / afterAgentThought
		Text       string `json:"text"`
		DurationMs int    `json:"duration_ms"`

		// postToolUseFailure
		ErrorMessage string `json:"error_message"`
		FailureType  string `json:"failure_type"`
		IsInterrupt  bool   `json:"is_interrupt"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		logf("JSON parse FAILED: %v — raw(first 200): %s", err, safePrefix(string(data), 200))
		os.Exit(0)
	}

	// Resolve session ID: prefer session_id, fall back to conversation_id.
	sid := firstNonEmpty(raw.SessionID, raw.ConversationID)

	logf("event=%s session=%s tool=%s", raw.Event, sid, raw.ToolName)

	switch raw.Event {
	case "beforeSubmitPrompt":
		if raw.Prompt != "" {
			meta := buildFullMeta("before_submit_prompt", data)
			if _, err := store.LogDiscussion(sid, "user", "cursor", raw.Prompt, meta); err != nil {
				logf("beforeSubmitPrompt log FAILED: %v", err)
			} else {
				logf("beforeSubmitPrompt logged (%d chars)", len(raw.Prompt))
			}
		} else {
			logf("beforeSubmitPrompt — empty prompt, skipped")
		}
		// Always return continue: true (observer only, never block)
		fmt.Println(`{"continue":true}`)
		os.Exit(0)

	case "stop":
		status := raw.Status
		if status == "" {
			status = "completed"
		}
		content := "⏹ Session ended"
		switch status {
		case "aborted":
			content = "⏹ Session aborted"
		case "error":
			content = "⏹ Session error"
		}
		meta := buildFullMeta("stop", data)
		if _, err := store.LogDiscussion(sid, "assistant", "cursor", content, meta); err != nil {
			logf("stop log FAILED: %v", err)
		} else {
			logf("stop logged (status=%s)", status)
		}
		os.Exit(0)

	case "postToolUse":
		if raw.ToolName == "" {
			logf("postToolUse — empty tool_name, skipped")
			os.Exit(0)
		}

		// Normalize MCP tool names: strip mcp__ prefix if present.
		normalizedName := raw.ToolName
		normalizedName = strings.TrimPrefix(normalizedName, "mcp__")
		normalizedName = strings.TrimPrefix(normalizedName, "MCP__")

		// Resolve tool output — Cursor may use tool_output (JSON string) or tool_response.
		toolResp := raw.ToolResp
		if len(toolResp) == 0 {
			toolResp = raw.ToolOutput
		}

		content := buildCursorToolContent(normalizedName, raw.ToolInput, toolResp)
		// Use buildFullMeta (stores everything) then enrich with standardized diff fields
		meta := buildFullMeta("post_tool", data)
		meta = enrichCursorMeta(meta, normalizedName, raw.ToolInput, toolResp)

		if content != "" {
			if _, err := store.LogDiscussion(sid, "assistant", "cursor", content, meta); err != nil {
				logf("postToolUse %s log FAILED: %v", raw.ToolName, err)
			} else {
				logf("postToolUse %s logged", raw.ToolName)
			}
		} else {
			logf("postToolUse %s — empty content, skipped", raw.ToolName)
		}
		os.Exit(0)

	case "postToolUseFailure":
		if raw.ToolName == "" {
			logf("postToolUseFailure — empty tool_name, skipped")
			os.Exit(0)
		}
		content := "⚠ " + raw.ToolName + " failed"
		if raw.ErrorMessage != "" {
			content += ": " + truncateText(raw.ErrorMessage, 150)
		}
		meta := buildFullMeta("post_tool_failure", data)
		if _, err := store.LogDiscussion(sid, "assistant", "cursor", content, meta); err != nil {
			logf("postToolUseFailure log FAILED: %v", err)
		} else {
			logf("postToolUseFailure %s logged", raw.ToolName)
		}
		os.Exit(0)

	case "afterFileEdit":
		if raw.FilePath != "" && len(raw.Edits) > 0 {
			// Extract old_string / new_string from edits array for diff rendering
			content := "📝 " + raw.FilePath
			meta := buildFullMeta("after_file_edit", data)
			// Enrich with standardized edit metadata so the frontend DiffPanel can render it
			meta = enrichFileEditMeta(meta, raw.FilePath, raw.Edits)

			if _, err := store.LogDiscussion(sid, "assistant", "cursor", content, meta); err != nil {
				logf("afterFileEdit log FAILED: %v", err)
			} else {
				logf("afterFileEdit logged (%s)", raw.FilePath)
			}
		} else {
			logf("afterFileEdit — empty file_path or edits, skipped")
		}
		os.Exit(0)

	case "afterAgentResponse":
		if raw.Text != "" {
			meta := buildFullMeta("after_agent_response", data)
			if _, err := store.LogDiscussion(sid, "assistant", "cursor", raw.Text, meta); err != nil {
				logf("afterAgentResponse log FAILED: %v", err)
			} else {
				logf("afterAgentResponse logged (%d chars)", len(raw.Text))
			}
		} else {
			logf("afterAgentResponse — empty text, skipped")
		}
		os.Exit(0)

	case "afterAgentThought":
		if raw.Text != "" {
			meta := buildFullMeta("after_agent_thought", data)
			// Store a preview — full text is in metadata for the UI to expand
			preview := "💭 " + truncateText(raw.Text, 200)
			if _, err := store.LogDiscussion(sid, "assistant", "cursor", preview, meta); err != nil {
				logf("afterAgentThought log FAILED: %v", err)
			} else {
				logf("afterAgentThought logged (%d chars)", len(raw.Text))
			}
		}
		os.Exit(0)

	default:
		logf("unhandled event=%s, ignored", raw.Event)
		os.Exit(0)
	}
}

// ---- Metadata enrichment (matching Gemini CLI's enrichGeminiMeta pattern) ----

// enrichCursorMeta extracts standardized diff fields from Cursor's postToolUse
// payload and promotes them to top-level metadata keys so the frontend can
// render tool content using the same code paths as Claude Code.
func enrichCursorMeta(metaJSON string, toolName string, toolInput, toolResp json.RawMessage) string {
	var meta map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		return metaJSON
	}

	ti := parseToolInput(toolInput)
	fp := firstNonEmpty(ti["file_path"], ti["filePath"], ti["path"])

	switch toolName {
	case "Edit", "edit":
		enrichCursorEditMeta(meta, fp, toolResp, ti)
	case "Write", "write":
		enrichCursorWriteMeta(meta, fp, toolResp, ti)
	case "Shell", "Bash", "shell", "bash":
		enrichCursorShellMeta(meta, ti)
	case "Read", "read":
		enrichCursorReadMeta(meta, fp, toolResp)
	}

	b, _ := json.Marshal(meta)
	return string(b)
}

// enrichCursorEditMeta extracts edit/diff info from Cursor's Edit tool response.
func enrichCursorEditMeta(meta map[string]any, filePath string, toolResp json.RawMessage, ti map[string]string) {
	meta["type"] = "edit"
	if filePath != "" {
		meta["file_path"] = filePath
	}

	// Try to extract hunks from tool_output (may contain structured diff)
	hunks := extractHunksFromCursorResp(toolResp)
	if len(hunks) > 0 {
		meta["hunks"] = hunks
	}

	// Always capture old/new strings from tool_input as fallback
	oldStr := firstNonEmpty(ti["old_string"], ti["oldString"], ti["old_str"])
	newStr := firstNonEmpty(ti["new_string"], ti["newString"], ti["new_str"])
	if oldStr != "" {
		meta["old_string"] = oldStr
	}
	if newStr != "" {
		meta["new_string"] = newStr
	}
}

// enrichCursorWriteMeta extracts file metadata from Cursor's Write tool response.
func enrichCursorWriteMeta(meta map[string]any, filePath string, toolResp json.RawMessage, ti map[string]string) {
	if filePath != "" {
		meta["file_path"] = filePath
	}

	isNew := false
	// Check tool_response for new-file indicators
	var resp struct {
		Created   bool `json:"created"`
		IsNewFile bool `json:"isNewFile"`
		IsNew     bool `json:"isNew"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		isNew = resp.Created || resp.IsNewFile || resp.IsNew
	}
	// Try string-encoded JSON (Cursor wraps tool_output as JSON string)
	if !isNew {
		var s string
		if json.Unmarshal(toolResp, &s) == nil && s != "" {
			var inner struct {
				Created   bool `json:"created"`
				IsNewFile bool `json:"isNewFile"`
			}
			if json.Unmarshal([]byte(s), &inner) == nil {
				isNew = inner.Created || inner.IsNewFile
			}
		}
	}
	// If content is provided and we can't determine from response, treat as new
	if !isNew && ti["content"] != "" {
		isNew = true
	}

	if isNew {
		meta["type"] = "new_file"
	} else {
		meta["type"] = "edit"
	}
}

// enrichCursorShellMeta adds bash command metadata.
func enrichCursorShellMeta(meta map[string]any, ti map[string]string) {
	meta["type"] = "bash"
	if cmd := ti["command"]; cmd != "" {
		meta["command"] = cmd
	}
}

// enrichCursorReadMeta adds file read metadata.
func enrichCursorReadMeta(meta map[string]any, filePath string, toolResp json.RawMessage) {
	meta["type"] = "read"
	if filePath != "" {
		meta["file_path"] = filePath
	}
	lc := extractCursorLinesCountFromResp(toolResp)
	if lc > 0 {
		meta["lines_count"] = lc
	}
}

// enrichFileEditMeta extracts standardized edit metadata from Cursor's
// afterFileEdit hook payload, matching the format the frontend DiffPanel expects.
func enrichFileEditMeta(metaJSON string, filePath string, editsRaw json.RawMessage) string {
	var meta map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		return metaJSON
	}

	meta["type"] = "edit"
	meta["file_path"] = filePath

	// Parse edits array: [{"old_string": "...", "new_string": "..."}, ...]
	var edits []struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if json.Unmarshal(editsRaw, &edits) == nil && len(edits) > 0 {
		// Use first edit's old/new strings for diff rendering
		if edits[0].OldString != "" {
			meta["old_string"] = edits[0].OldString
		}
		if edits[0].NewString != "" {
			meta["new_string"] = edits[0].NewString
		}
		// If multiple edits, store all of them
		if len(edits) > 1 {
			var allEdits []map[string]string
			for _, e := range edits {
				allEdits = append(allEdits, map[string]string{
					"old_string": e.OldString,
					"new_string": e.NewString,
				})
			}
			meta["all_edits"] = allEdits
		}
	}

	b, _ := json.Marshal(meta)
	return string(b)
}

// extractHunksFromCursorResp tries to extract diff hunks from a Cursor tool response.
// Cursor may provide structured hunks in the tool_output for Edit tools.
func extractHunksFromCursorResp(toolResp json.RawMessage) []PatchHunk {
	if len(toolResp) == 0 {
		return nil
	}
	// Try direct hunks array
	var resp struct {
		StructuredPatch []PatchHunk `json:"structuredPatch"`
		Hunks           []PatchHunk `json:"hunks"`
		Metadata        struct {
			FileDiff struct {
				Patch string `json:"patch"`
			} `json:"filediff"`
			Diff string `json:"diff"`
		} `json:"metadata"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		if len(resp.StructuredPatch) > 0 {
			return resp.StructuredPatch
		}
		if len(resp.Hunks) > 0 {
			return resp.Hunks
		}
		if patch := resp.Metadata.FileDiff.Patch; patch != "" {
			return parseDiffToHunks(patch)
		}
		if diff := resp.Metadata.Diff; diff != "" {
			return parseDiffToHunks(diff)
		}
	}
	// Try string-encoded JSON
	var s string
	if json.Unmarshal(toolResp, &s) == nil && s != "" {
		var inner struct {
			StructuredPatch []PatchHunk `json:"structuredPatch"`
		}
		if json.Unmarshal([]byte(s), &inner) == nil && len(inner.StructuredPatch) > 0 {
			return inner.StructuredPatch
		}
	}
	return nil
}

// ---- Tool content display ----

// buildCursorToolContent builds a human-readable description for a Cursor tool call.
// Cursor tool names are similar to Claude Code: Shell, Read, Write, Edit, Grep, Glob,
// Task, WebSearch, WebFetch, plus MCP:<server>_<tool> for MCP tools.
func buildCursorToolContent(toolName string, toolInput, toolResp json.RawMessage) string {
	ti := parseToolInput(toolInput)

	switch {
	case toolName == "Shell" || toolName == "Bash":
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

		// Parse Bash file operations
		fop := parseBashFileOp(cmd)
		if fop != nil {
			return fileOpIcon(fop) + " " + fop.File
		}

		cmdPreview := truncateText(cmd, 150)
		result := "🔧 " + cmdPreview

		output := extractCursorBashOutput(toolResp)
		if output != "" {
			result += "\n  → " + strings.TrimSpace(truncateText(output, 120))
		}
		if ec := extractExitCode(toolResp); ec != 0 {
			result += fmtExitCode(ec)
		}
		return result

	case toolName == "Read" || strings.HasSuffix(toolName, "_read") || strings.HasSuffix(toolName, "_read_file"):
		fp := firstNonEmpty(ti["file_path"], ti["filePath"], ti["path"])
		if fp != "" {
			result := "👁 " + fp
			lc := extractCursorLinesCountFromResp(toolResp)
			if lc > 0 {
				result += " (" + uitoa(lc) + " lines)"
			}
			return result
		}
		return "👁 " + toolName

	case toolName == "Write" || strings.HasSuffix(toolName, "_write") || strings.HasSuffix(toolName, "_write_file"):
		fp := firstNonEmpty(ti["file_path"], ti["filePath"], ti["path"])
		if fp != "" {
			if isNewFile(toolResp) || isCursorNewFile(toolResp) {
				return "🆕 " + fp
			}
			return "📝 " + fp
		}
		return "📝 " + toolName

	case toolName == "Edit" || strings.HasSuffix(toolName, "_edit") || toolName == "apply_patch":
		fp := firstNonEmpty(ti["file_path"], ti["filePath"], ti["path"])
		result := ""
		if fp != "" {
			result = "📝 " + fp
		} else {
			result = "📝 edit"
		}
		oldStr := firstNonEmpty(ti["old_string"], ti["oldString"], ti["old_str"])
		newStr := firstNonEmpty(ti["new_string"], ti["newString"], ti["new_str"])
		if oldStr != "" {
			result += "\n- " + strings.TrimSpace(truncateText(oldStr, 80))
		}
		if newStr != "" {
			result += "\n+ " + strings.TrimSpace(truncateText(newStr, 80))
		}
		return result

	case toolName == "Grep" || strings.HasSuffix(toolName, "_grep") || strings.HasSuffix(toolName, "_search"):
		pattern := firstNonEmpty(ti["pattern"], ti["query"])
		if pattern != "" {
			return "🔍 \"" + truncateText(pattern, 80) + "\""
		}
		return "🔍 " + toolName

	case toolName == "Glob" || strings.HasSuffix(toolName, "_glob"):
		g := firstNonEmpty(ti["pattern"], ti["glob"])
		if g != "" {
			return "🔍 glob \"" + truncateText(g, 80) + "\""
		}
		return "🔍 " + toolName

	case toolName == "LS" || toolName == "List" || strings.HasSuffix(toolName, "_ls") || strings.HasSuffix(toolName, "_list_directory"):
		fp := firstNonEmpty(ti["dir_path"], ti["path"], ti["file_path"])
		if fp != "" {
			return "📂 " + fp
		}
		return "📂 " + toolName

	case toolName == "WebSearch" || strings.HasSuffix(toolName, "_web_search"):
		q := firstNonEmpty(ti["query"], ti["q"])
		if q != "" {
			return "🌐 \"" + truncateText(q, 80) + "\""
		}
		return "🌐 WebSearch"

	case toolName == "WebFetch" || strings.HasSuffix(toolName, "_web_fetch"):
		if url := ti["url"]; url != "" {
			return "🌐 " + truncateText(url, 80)
		}
		return "🌐 WebFetch"

	case toolName == "Task" || strings.HasSuffix(toolName, "_task"):
		desc := firstNonEmpty(ti["description"], ti["prompt"], ti["task"])
		if desc != "" {
			return "🤖 Task: " + truncateText(desc, 100)
		}
		return "🤖 Task"

	case toolName == "Question" || toolName == "AskUserQuestion":
		q := firstNonEmpty(ti["question"], ti["questions"])
		if q != "" {
			return "❓ " + truncateText(q, 100)
		}
		return "❓ Question"

	case toolName == "TodoWrite" || toolName == "update_plan":
		return "📋 Plan updated"

	case toolName == "Delete" || strings.HasSuffix(toolName, "_delete"):
		fp := firstNonEmpty(ti["target_file"], ti["file_path"], ti["filePath"], ti["path"])
		if fp != "" {
			return "🗑 " + fp
		}
		return "🗑 " + toolName

	case strings.HasPrefix(toolName, "aipm_"):
		result := "📡 " + toolName
		q := firstNonEmpty(ti["query"], ti["q"])
		if q != "" {
			result += " \"" + truncateText(q, 60) + "\""
		}
		return result

	default:
		// Generic display for unknown/MCP tools
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

// ---- Response parsing helpers ----

// extractCursorBashOutput extracts stdout/stderr from a Cursor Shell tool response.
func extractCursorBashOutput(toolResp json.RawMessage) string {
	if len(toolResp) == 0 {
		return ""
	}
	var resp struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		Output   string `json:"output"`
		ExitCode int    `json:"exitCode"`
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
	}
	// Cursor stores tool_output as a JSON string — parse it again
	var s string
	if json.Unmarshal(toolResp, &s) == nil && s != "" {
		var inner struct {
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			Output   string `json:"output"`
			ExitCode int    `json:"exitCode"`
		}
		if json.Unmarshal([]byte(s), &inner) == nil {
			if inner.Stdout != "" {
				return inner.Stdout
			}
			if inner.Stderr != "" {
				return inner.Stderr
			}
			if inner.Output != "" {
				return inner.Output
			}
		}
		return s
	}
	return ""
}

// extractCursorLinesCountFromResp extracts line count from a Cursor Read tool response.
func extractCursorLinesCountFromResp(toolResp json.RawMessage) int {
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
	var s string
	if json.Unmarshal(toolResp, &s) == nil && s != "" {
		return strings.Count(s, "\n") + 1
	}
	return 0
}

// isCursorNewFile checks Cursor-specific tool_output format for new-file indicators.
func isCursorNewFile(toolResp json.RawMessage) bool {
	if len(toolResp) == 0 {
		return false
	}
	var resp struct {
		Created   bool `json:"created"`
		IsNewFile bool `json:"isNewFile"`
		IsNew     bool `json:"isNew"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		if resp.Created || resp.IsNewFile || resp.IsNew {
			return true
		}
	}
	var s string
	if json.Unmarshal(toolResp, &s) == nil && s != "" {
		var inner struct {
			Created   bool `json:"created"`
			IsNewFile bool `json:"isNewFile"`
		}
		if json.Unmarshal([]byte(s), &inner) == nil {
			return inner.Created || inner.IsNewFile
		}
	}
	return false
}

// ---- Cursor hook setup ----

// SetupCursorHooks writes Cursor hook configuration to .cursor/hooks.json.
// Configures all interaction-capture hooks: beforeSubmitPrompt (user messages),
// stop (session end), postToolUse (tool usage), afterFileEdit (file changes),
// and afterAgentResponse (assistant responses).
func SetupCursorHooks(commandPath string) error {
	runtimeDir, err := pmdb.RuntimeDir()
	if err != nil {
		return fmt.Errorf("find runtime dir: %w", err)
	}
	projectRoot := filepath.Dir(runtimeDir)
	cursorDir := filepath.Join(projectRoot, ".cursor")
	hooksPath := filepath.Join(cursorDir, "hooks.json")

	cfg := map[string]any{}
	if data, err := os.ReadFile(hooksPath); err == nil && len(data) > 0 {
		json.Unmarshal(data, &cfg)
	}

	if _, ok := cfg["version"]; !ok {
		cfg["version"] = 1
	}

	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	hookCmd := shellQuote(commandPath) + " hook-cursor"
	makeEntry := func() []any {
		return []any{
			map[string]any{
				"type":    "command",
				"command": hookCmd,
			},
		}
	}

	hooks["beforeSubmitPrompt"] = makeEntry()
	hooks["stop"] = makeEntry()
	hooks["postToolUse"] = makeEntry()
	hooks["postToolUseFailure"] = makeEntry()
	hooks["afterFileEdit"] = makeEntry()
	hooks["afterAgentResponse"] = makeEntry()
	cfg["hooks"] = hooks

	os.MkdirAll(cursorDir, 0755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(hooksPath, data, 0644); err != nil {
		return fmt.Errorf("write hooks.json: %w", err)
	}
	fmt.Printf("  ✅ Cursor hooks configured → %s\n", hooksPath)
	return nil
}
