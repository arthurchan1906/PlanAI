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
	"aipmc/collab"
	"aipmc/store"
)

// processCodexHook reads the Codex CLI hook stdin JSON and saves to discussion_log.
// Called via: aipmc hook-codex
//
// Codex hook events captured:
//   - UserPromptSubmit → user message (like Gemini's BeforeAgent)
//   - PostToolUse      → assistant tool use (like Gemini's AfterTool)
//   - Stop             → assistant response (like Gemini's AfterAgent)
func ProcessCodexHook() {
	now := time.Now().Format("2006-01-02T15:04:05.000")
	data, _ := io.ReadAll(os.Stdin)

	if os.Getenv("AIPM_DEBUG_HOOK") != "" {
		dumpRawHook("codex", now, data)
	}

	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "[aipm-codex %s] ", now)
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}

	// Catch panics so a bug never crashes the parent Codex process.
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

	// Parse common fields plus event-specific ones.
	// Codex may use different field names than Claude Code —
	// we try multiple candidates for each logical field.
	var raw struct {
		Event     string `json:"hook_event_name"`
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
		Model     string `json:"model"`
		PermMode  string `json:"permission_mode"`
		TurnID    string `json:"turn_id"`

		// UserPromptSubmit — try all plausible field names
		Prompt  string `json:"prompt"`
		Message string `json:"message"`
		Content string `json:"content"`
		Query   string `json:"query"`

		// PostToolUse
		ToolName    string          `json:"tool_name"`
		ToolUseID   string          `json:"tool_use_id"`
		ToolInput   json.RawMessage `json:"tool_input"`
		ToolResp    json.RawMessage `json:"tool_response"`

		// Stop — multiple possible response fields
		Response             string `json:"response"`
		Output               string `json:"output"`
		Text                 string `json:"text"`
		LastAssistantMessage string `json:"last_assistant_message"`
		AssistantMsg         string `json:"assistant_message"`
		Reply                string `json:"reply"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		logf("JSON parse FAILED: %v — raw(first 200): %s", err, safePrefix(string(data), 200))
		os.Exit(0)
	}

	// Resolve user prompt from whichever field has content.
	userPrompt := firstNonEmpty(raw.Prompt, raw.Message, raw.Content, raw.Query)

	logf("event=%s tool=%s session=%s prompt=%dch", raw.Event, raw.ToolName, raw.SessionID, len(userPrompt))

	switch raw.Event {
	case "UserPromptSubmit":
		if userPrompt != "" {
			meta := buildFullMeta("user_prompt", data)
			if _, err := store.LogDiscussion(raw.SessionID, "user", "codex-cli", userPrompt, meta); err != nil {
				logf("UserPromptSubmit log FAILED: %v", err)
			} else {
				logf("UserPromptSubmit logged (%d chars)", len(userPrompt))
			}
		} else {
			logf("UserPromptSubmit — empty prompt (fields checked: prompt, message, content, query)")
		}

	case "PostToolUse":
		if raw.ToolName == "" {
			logf("PostToolUse — empty tool_name, skipped")
			break
		}

		// Normalize Codex MCP tool names.
		// Codex format: mcp__<server>__<tool>  =>  strip both layers.
		normalizedName := raw.ToolName
		normalizedName = strings.TrimPrefix(normalizedName, "mcp__")
		normalizedName = strings.TrimPrefix(normalizedName, "aipm__")

		// update_plan gets special treatment: full step display + completion detection
		var content string
		if normalizedName == "update_plan" {
			content = buildCodexPlanContent(raw.SessionID, raw.ToolInput)
		} else {
			content = buildCodexToolContent(normalizedName, raw.ToolInput, raw.ToolResp)
		}
		meta := buildFullMeta("post_tool", data)

		if content != "" {
			if _, err := store.LogDiscussion(raw.SessionID, "assistant", "codex-cli", content, meta); err != nil {
				logf("PostToolUse %s log FAILED: %v", raw.ToolName, err)
			} else {
				logf("PostToolUse %s logged", raw.ToolName)
			}
			collab.MaybeAlertFromToolInput(raw.SessionID, "codex-cli", normalizedName, raw.ToolInput)
		} else {
			logf("PostToolUse %s — empty content, skipped", raw.ToolName)
		}

	case "Stop":
		// Try all plausible response fields.
		respText := firstNonEmpty(raw.Response, raw.Output, raw.Text, raw.LastAssistantMessage, raw.AssistantMsg, raw.Reply)
		if respText != "" {
			meta := buildFullMeta("stop", data)
			if _, err := store.LogDiscussion(raw.SessionID, "assistant", "codex-cli", respText, meta); err != nil {
				logf("Stop log FAILED: %v", err)
			} else {
				logf("Stop logged (%d chars)", len(respText))
			}
		} else {
			meta := buildFullMeta("stop", data)
			if _, err := store.LogDiscussion(raw.SessionID, "assistant", "codex-cli", "(turn stopped)", meta); err != nil {
				logf("Stop (no-text) log FAILED: %v", err)
			} else {
				logf("Stop logged (no response text)")
			}
		}

	default:
		logf("unhandled event=%s, ignored", raw.Event)
	}
}

// firstNonEmpty returns the first non-empty string from the candidates.
func firstNonEmpty(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return c
		}
	}
	return ""
}

// safePrefix returns the first n bytes of s, safely truncated.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// dumpRawHook persists the raw hook JSON to .pmai/logs/ for debugging.
// Always writes — no env var gate. This is essential for diagnosing
// field-name mismatches and format changes across Codex versions.
func dumpRawHook(platform, timestamp string, data []byte) {
	// Find the project's .pmai dir for log storage.
	// Fall back to os.TempDir if .pmai is not found.
	logDir := hookLogDir()
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, platform+"-hook.log")

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "=== [%s] len=%d ===\n", timestamp, len(data))
	f.Write(data)
	f.WriteString("\n=== END ===\n\n")
}

// hookLogDir returns the .pmai/logs directory for the current project,
// or os.TempDir() as a fallback.
func hookLogDir() string {
	dir, err := pmdb.RuntimeDir()
	if err == nil && dir != "" {
		return filepath.Join(dir, "logs")
	}
	return filepath.Join(os.TempDir(), "aipm-hooks")
}

// shellQuote wraps s in double quotes only when it contains spaces.
// Bare quoting (e.g. `"aipmc"`) breaks command execution on Windows
// because the shell treats the quotes as part of the executable name.
func shellQuote(s string) string {
	if strings.Contains(s, " ") {
		return "\"" + s + "\""
	}
	return s
}

// buildCodexToolContent builds a human-readable description for a Codex tool call.
// Codex uses different tool names than Gemini CLI:
//   - Bash (not run_shell_command)
//   - apply_patch (not replace/edit_file/write_file)
//   - MCP tools: mcp__server__tool_name
func buildCodexToolContent(toolName string, toolInput, toolResp json.RawMessage) string {
	ti := parseToolInput(toolInput)
	llmText := extractLLMText(toolResp)

	switch {
	case toolName == "Bash":
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

		// Parse the command for file operations so Codex Bash-based
		// file edits get the same display format as Claude Code/Gemini tools.
		//   🆕 path — create    📝 path — modify/append    👁 path — read
		// Codex doesn't provide structured diffs like Claude/Gemini, so we
		// only show the file path — no old/new strings, no command preview.
		fop := parseBashFileOp(cmd)
		if fop != nil {
			return fileOpIcon(fop) + " " + fop.File
		}

		cmdPreview := truncateText(cmd, 150)
		result := "🔧 " + cmdPreview
		if llmText != "" {
			result += "\n  → " + strings.TrimSpace(truncateText(llmText, 120))
		}
		if ec := extractExitCode(toolResp); ec != 0 {
			result += fmtExitCode(ec)
		}
		return result

	case toolName == "apply_patch":
		fp := ti["file_path"]
		if fp == "" {
			fp = ti["path"]
		}
		if fp == "" {
			fp = ti["filePath"]
		}
		result := ""
		if fp != "" {
			result = "📝 " + fp
		} else {
			result = "📝 apply_patch"
		}
		// Show old/new content previews
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

	case toolName == "Read" || strings.HasSuffix(toolName, "_read") || strings.HasSuffix(toolName, "_read_file"):
		fp := ti["file_path"]
		if fp == "" {
			fp = ti["path"]
		}
		if fp == "" {
			fp = ti["filePath"]
		}
		if fp != "" {
			return "👁 " + fp
		}
		return "👁 " + toolName

	case toolName == "Write" || strings.HasSuffix(toolName, "_write") || strings.HasSuffix(toolName, "_write_file"):
		fp := ti["file_path"]
		if fp == "" {
			fp = ti["path"]
		}
		if fp == "" {
			fp = ti["filePath"]
		}
		if fp != "" {
			if isNewFile(toolResp) {
				return "🆕 " + fp
			}
			return "📝 " + fp
		}
		return "📝 " + toolName

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

	case toolName == "Grep" || strings.HasSuffix(toolName, "_grep") || strings.HasSuffix(toolName, "_search"):
		pattern := ti["pattern"]
		if pattern == "" {
			pattern = ti["query"]
		}
		if pattern != "" {
			pattern = truncateText(pattern, 80)
			return "🔍 \"" + pattern + "\""
		}
		return "🔍 " + toolName

	case toolName == "LS" || strings.HasSuffix(toolName, "_ls") || strings.HasSuffix(toolName, "_list_directory"):
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

	case toolName == "WebSearch" || strings.HasSuffix(toolName, "_web_search"):
		q := ti["query"]
		if q == "" {
			q = ti["q"]
		}
		if q != "" {
			q = truncateText(q, 80)
			return "🌐 \"" + q + "\""
		}
		return "🌐 WebSearch"

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

// ---- Bash file operation parser ----
// Codex agents primarily use Bash (PowerShell on Windows) for file operations,
// unlike Claude Code (Write/Edit) or Gemini (replace/write_file) which provide
// structured diff data directly. This parser extracts file paths and operation
// types from raw shell commands so Codex file changes get comparable visibility.

// ParseBashFileOp extracts file operation metadata from a shell command.
func ParseBashFileOp(cmd string) *FileOp {
	f := parseBashFileOp(cmd)
	if f == nil {
		return nil
	}
	return &FileOp{Op: f.Op, File: f.File}
}

type FileOp struct {
	Op   string
	File string
}

type fileOp struct {
	Op   string // "create", "modify", "append", "read"
	File string // extracted file path
}

func makeFileOp(op, path string) *fileOp {
	path = strings.TrimSpace(strings.TrimRight(path, ",;"))
	if !isValidFileOpPath(path) {
		return nil
	}
	return &fileOp{Op: op, File: path}
}

// IsValidFileOpPath reports whether a token looks like a real file path (not e.g. &1).
func IsValidFileOpPath(path string) bool {
	return isValidFileOpPath(path)
}

// isValidFileOpPath rejects shell redirect targets and other non-path tokens
// mis-parsed from commands like `go build 2>&1`.
func isValidFileOpPath(path string) bool {
	path = strings.TrimSpace(strings.TrimRight(path, ",;"))
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "&") {
		return false
	}
	lower := strings.ToLower(path)
	if lower == "/dev/null" || lower == "nul" || lower == "con" {
		return false
	}
	// Require path-like tokens; bare words (Write, edit.json) are not useful.
	if !looksLikeFilePath(path) {
		return false
	}
	return true
}

func looksLikeFilePath(path string) bool {
	if strings.Contains(path, "/") || strings.Contains(path, "\\") {
		return true
	}
	if len(path) >= 2 && path[1] == ':' {
		return true
	}
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") {
		return true
	}
	return false
}

func fileOpIcon(f *fileOp) string {
	switch f.Op {
	case "create":
		return "🆕"
	case "modify":
		return "📝"
	case "append":
		return "📝"
	case "read":
		return "👁"
	default:
		return "🔧"
	}
}

// parseBashFileOp inspects a shell command and extracts file operation metadata.
// Handles PowerShell (Windows) patterns. Returns nil for unrecognized commands.
func parseBashFileOp(cmd string) *fileOp {
	// ----- PowerShell: Set-Content -Path 'file' or pipeline | Set-Content 'file' (create/write) -----
	if strings.Contains(cmd, "Set-Content") {
		path := extractPSPath(cmd, "-Path")
		if path == "" {
			path = extractPSPath(cmd, "-LiteralPath")
		}
		if path == "" {
			// Bare path after Set-Content (common in pipelines)
			path = extractBareArg(cmd, "Set-Content")
			// Reject variable references like $path or ${path}
			if strings.HasPrefix(path, "$") {
				path = ""
			}
		}
		if path == "" {
			// Variable path: $var = 'file.txt'; ... | Set-Content $var
			// Extract the first quoted literal path from the command.
			path = extractFirstQuoted(cmd)
			if path == "" || (!strings.Contains(path, ".") && !strings.Contains(path, "/") && !strings.Contains(path, "\\")) {
				path = ""
			}
		}
			if path != "" {
				// Set-Content always writes/overwrites — 📝 like Claude Write / Gemini write_file.
				// Only New-Item gets 🆕 because we know it explicitly creates.
				return makeFileOp("modify", path)
			}
	}

	// ----- PowerShell: Add-Content -Path 'file' (append) -----
	if strings.Contains(cmd, "Add-Content") {
		path := extractPSPath(cmd, "-Path")
		if path != "" {
			return makeFileOp("append", path)
		}
	}

	// ----- PowerShell: New-Item -Path 'file' (create) -----
	if strings.Contains(cmd, "New-Item") {
		path := extractPSPath(cmd, "-Path")
		if path != "" {
			return makeFileOp("create", path)
		}
	}

	// ----- PowerShell: Remove-Item 'file' (delete = modify) -----
	if strings.Contains(cmd, "Remove-Item") {
		path := extractPSPath(cmd, "-Path")
		if path == "" {
			path = extractBareArg(cmd, "Remove-Item")
		}
		if path != "" {
			return makeFileOp("modify", path)
		}
	}

	// ----- PowerShell: Get-Content 'file' (read, standalone only) -----
	if strings.Contains(cmd, "Get-Content") && !strings.Contains(cmd, "|") && !strings.Contains(cmd, "-replace") {
		path := extractPSPath(cmd, "-Path")
		if path == "" {
			path = extractPSPath(cmd, "-LiteralPath")
		}
		if path == "" {
			path = extractBareArg(cmd, "Get-Content")
		}
		if path != "" {
			return makeFileOp("read", path)
		}
	}

	// ----- PowerShell: Out-File -FilePath 'file' -----
	if strings.Contains(cmd, "Out-File") {
		path := extractPSPath(cmd, "-FilePath")
		if path != "" {
			return makeFileOp("modify", path)
		}
	}

	// ----- PowerShell: Join-Path ... 'file' (used in variable assignments) -----
	if strings.Contains(cmd, "Join-Path") {
		// Extract the last quoted string after Join-Path
		if path := extractLastQuoted(cmd); path != "" {
			// Only if it looks like a file path
			if strings.Contains(path, ".") || strings.Contains(path, "/") || strings.Contains(path, "\\") {
			return makeFileOp("modify", path)
			}
		}
	}

	// ----- PowerShell: variable assignment with file path -----
	// $path = 'hook-scratch/alpha.txt'; ... | Set-Content $path
	if strings.Contains(cmd, "Set-Content") && strings.Contains(cmd, "$") {
		// Try to find a quoted path earlier in the command (from variable assignment)
		path := extractFirstQuoted(cmd)
		if path != "" && (strings.Contains(path, ".") || strings.Contains(path, "/") || strings.Contains(path, "\\")) {
			if strings.Contains(cmd, "-replace") {
			return makeFileOp("modify", path)
			}
		return makeFileOp("modify", path)
		}
	}

	// ----- Unix: > file, >> file -----
	if idx := strings.Index(cmd, ">>"); idx >= 0 {
		rest := strings.TrimSpace(cmd[idx+2:])
		if path := extractBareToken(rest); path != "" && !strings.Contains(path, "/dev/") {
			return makeFileOp("append", path)
		}
	}
	if idx := strings.LastIndex(cmd, ">"); idx >= 0 && (idx == 0 || cmd[idx-1] != '>') {
		rest := strings.TrimSpace(cmd[idx+1:])
		if path := extractBareToken(rest); path != "" && !strings.Contains(path, "/dev/") {
		return makeFileOp("modify", path)
		}
	}

	// ----- Unix: sed -i 's/old/new/' file -----
	if strings.Contains(cmd, "sed ") {
		if path := extractLastBare(cmd); path != "" {
		return makeFileOp("modify", path)
		}
	}

	return nil
}

// extractPSPath extracts the value of a PowerShell -ParamName from the command.
func extractPSPath(cmd, paramName string) string {
	idx := strings.Index(cmd, paramName)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(cmd[idx+len(paramName):])
	return extractQuotedOrBare(rest)
}

// extractBareArg extracts the first positional arg after a command name.
func extractBareArg(cmd, cmdlet string) string {
	idx := strings.Index(cmd, cmdlet)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(cmd[idx+len(cmdlet):])
	// Skip -ParamName flags
	for strings.HasPrefix(rest, "-") {
		// Skip -Flag and its value if present
		spaceIdx := strings.Index(rest, " ")
		if spaceIdx < 0 {
			return ""
		}
		rest = strings.TrimSpace(rest[spaceIdx:])
		// If next token is a value (doesn't start with -), skip it too
		if !strings.HasPrefix(rest, "-") {
			spaceIdx = strings.Index(rest, " ")
			if spaceIdx >= 0 {
				rest = strings.TrimSpace(rest[spaceIdx:])
			} else {
				return ""
			}
		}
	}
	return extractQuotedOrBare(rest)
}

// extractQuotedOrBare extracts a single-quoted, double-quoted, or bare token.
func extractQuotedOrBare(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return ""
	}
	if s[0] == '\'' {
		end := strings.IndexByte(s[1:], '\'')
		if end >= 0 {
			return s[1 : end+1]
		}
		return s[1:]
	}
	if s[0] == '"' {
		end := strings.IndexByte(s[1:], '"')
		if end >= 0 {
			return s[1 : end+1]
		}
		return s[1:]
	}
	return extractBareToken(s)
}

// extractBareToken reads a bare token until whitespace/special char.
func extractBareToken(s string) string {
	for i, c := range s {
		if c == ' ' || c == ';' || c == '|' || c == '>' || c == '<' || c == '\'' || c == '"' || c == '\r' || c == '\n' {
			return s[:i]
		}
	}
	return s
}

// extractFirstQuoted returns the first quoted string found in the command.
func extractFirstQuoted(cmd string) string {
	for i, c := range cmd {
		if c == '\'' || c == '"' {
			quote := byte(c)
			end := strings.IndexByte(cmd[i+1:], quote)
			if end >= 0 {
				return cmd[i+1 : i+1+end]
			}
		}
	}
	return ""
}

// extractLastQuoted returns the last quoted string found in the command.
func extractLastQuoted(cmd string) string {
	var last string
	for i := 0; i < len(cmd); i++ {
		if cmd[i] == '\'' || cmd[i] == '"' {
			quote := cmd[i]
			end := strings.IndexByte(cmd[i+1:], quote)
			if end >= 0 {
				last = cmd[i+1 : i+1+end]
				i = i + 1 + end
			}
		}
	}
	return last
}

// extractLastBare returns the last bare token (not inside quotes).
func extractLastBare(cmd string) string {
	// Split by space and return last meaningful token
	parts := strings.Fields(cmd)
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.Trim(parts[i], `'";`)
		if p != "" && !strings.HasPrefix(p, "-") && !strings.Contains(p, "=") {
			return p
		}
	}
	return ""
}

// ---- Codex CLI hook setup ----

// setupCodexHooks writes Codex CLI hook configuration to .codex/hooks.json.
// Hooks are enabled by default in Codex CLI — no feature flag needed.
func SetupCodexHooks(commandPath string) error {
	runtimeDir, _ := pmdb.RuntimeDir()
	projectRoot := filepath.Dir(runtimeDir)
	codexDir := filepath.Join(projectRoot, ".codex")
	hooksPath := filepath.Join(codexDir, "hooks.json")

	cfg := map[string]any{}
	if data, err := os.ReadFile(hooksPath); err == nil && len(data) > 0 {
		json.Unmarshal(data, &cfg)
	}

	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	// Build hook entries — no matcher = match all events.
	// Only quote the path if it contains spaces; bare quoting on Windows
	// causes "hook exited with code 1" because the shell can't resolve
	// a quoted bare command name like `"aipmc"`.
	hookCmd := shellQuote(commandPath) + " hook-codex"
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

	hooks["UserPromptSubmit"] = makeEntry()
	hooks["PostToolUse"] = makeEntry()
	hooks["Stop"] = makeEntry()
	cfg["hooks"] = hooks

	os.MkdirAll(codexDir, 0755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(hooksPath, data, 0644); err != nil {
		return fmt.Errorf("write hooks.json: %w", err)
	}
	fmt.Printf("  ✅ Codex hooks configured → %s\n", hooksPath)
	return nil
}

func planStatusIcon(status string) string {
	switch status {
	case "completed":
		return "✅"
	case "in_progress":
		return "🔄"
	case "pending":
		return "⬜"
	default:
		return "•"
	}
}

// ---- Codex plan tracking ----

// codexPlanStep represents a single step in a Codex plan.
type codexPlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

// codexPlanInput matches the update_plan tool_input JSON shape.
type codexPlanInput struct {
	Explanation string          `json:"explanation"`
	Plan        []codexPlanStep `json:"plan"`
}

// buildCodexPlanContent builds a rich display for update_plan tool calls.
// Shows all steps with status icons and marks which steps just completed
// (by comparing with the previous plan state for this session).
func buildCodexPlanContent(sessionID string, toolInput json.RawMessage) string {
	var current codexPlanInput
	if json.Unmarshal(toolInput, &current) == nil && len(current.Plan) > 0 {
		// Get previous plan state to detect completions
		prev := getPreviousPlan(sessionID)
		prevMap := make(map[string]string) // step → previous status
		for _, s := range prev {
			prevMap[s.Step] = s.Status
		}

		var lines []string
		// Title line
		if current.Explanation != "" {
			lines = append(lines, "📌 "+truncateText(current.Explanation, 120))
		} else {
			lines = append(lines, "📌 Plan updated")
		}

		// Each step
		for _, s := range current.Plan {
			icon := planStatusIcon(s.Status)
			line := "   " + icon + " " + s.Step
			// Mark newly completed steps
			if s.Status == "completed" && prevMap[s.Step] != "" && prevMap[s.Step] != "completed" {
				line += " ⬅"
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}
	return "📌 update_plan"
}

// getPreviousPlan retrieves the plan steps from the most recent update_plan
// discussion_log entry in the given session. Returns nil if none found.
func getPreviousPlan(sessionID string) []codexPlanStep {
	db, err := pmdb.Open()
	if err != nil {
		return nil
	}
	defer db.Close()

	// Find the most recent update_plan entry in this session
	row := db.QueryRow(
		`SELECT metadata FROM discussion_log
		 WHERE session_id = ? AND source = 'codex-cli'
		 AND content LIKE '📌%'
		 AND metadata LIKE '%"_type":"post_tool"%'
		 ORDER BY created_at DESC LIMIT 1`,
		sessionID)

	var metaJSON string
	if err := row.Scan(&metaJSON); err != nil {
		return nil
	}

	// Parse metadata → tool_input → plan
	var meta map[string]any
	if json.Unmarshal([]byte(metaJSON), &meta) != nil {
		return nil
	}
	ti, _ := meta["tool_input"].(map[string]any)
	if ti == nil {
		return nil
	}
	planRaw, _ := ti["plan"].([]any)
	if planRaw == nil {
		return nil
	}

	var steps []codexPlanStep
	for _, p := range planRaw {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		step, _ := pm["step"].(string)
		status, _ := pm["status"].(string)
		if step != "" {
			steps = append(steps, codexPlanStep{Step: step, Status: status})
		}
	}
	return steps
}
