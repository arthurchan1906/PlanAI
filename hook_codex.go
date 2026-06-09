package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// processCodexHook reads the Codex CLI hook stdin JSON and saves to discussion_log.
// Called via: aipmc hook-codex
//
// Codex hook events captured:
//   - UserPromptSubmit → user message (like Gemini's BeforeAgent)
//   - PostToolUse      → assistant tool use (like Gemini's AfterTool)
//   - Stop             → assistant response (like Gemini's AfterAgent)
func processCodexHook() {
	now := time.Now().Format("15:04:05.000")
	data, _ := io.ReadAll(os.Stdin)

	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "[aipm-codex %s] ", now)
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}

	logf("hook called, stdin=%d bytes", len(data))
	if len(data) < 10 {
		logf("stdin too short, exiting")
		os.Exit(0)
	}

	// Debug dump (only when AIPM_DEBUG_HOOK is set)
	if os.Getenv("AIPM_DEBUG_HOOK") != "" {
		f, _ := os.OpenFile(filepath.Join(os.TempDir(), "aipm-codex-hook-debug.txt"),
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if f != nil {
			fmt.Fprintf(f, "=== [%s] len=%d ===\n", now, len(data))
			f.Write(data)
			f.WriteString("\n=== END ===\n\n")
			f.Close()
		}
	}

	// Parse common fields plus event-specific ones.
	var raw struct {
		Event     string `json:"hook_event_name"`
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
		Model     string `json:"model"`
		PermMode  string `json:"permission_mode"`
		TurnID    string `json:"turn_id"`

		// UserPromptSubmit
		Prompt string `json:"prompt"`

		// PostToolUse
		ToolName    string          `json:"tool_name"`
		ToolUseID   string          `json:"tool_use_id"`
		ToolInput   json.RawMessage `json:"tool_input"`
		ToolResp    json.RawMessage `json:"tool_response"`

		// Stop — try common response fields
		Response string `json:"response"`
		Output   string `json:"output"`
		Text     string `json:"text"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		logf("JSON parse FAILED: %v", err)
		os.Exit(0)
	}
	logf("event=%s tool=%s", raw.Event, raw.ToolName)

	switch raw.Event {
	case "UserPromptSubmit":
		if raw.Prompt != "" {
			meta := buildFullMeta("user_prompt", data)
			logDiscussion(raw.SessionID, "user", "codex-cli", raw.Prompt, meta)
			logf("UserPromptSubmit logged (%d chars)", len(raw.Prompt))
		} else {
			logf("UserPromptSubmit — empty prompt, skipped")
		}

	case "PostToolUse":
		if raw.ToolName == "" {
			logf("PostToolUse — empty tool_name, skipped")
			break
		}

		// Normalize Codex MCP tool names: strip mcp__ prefix
		normalizedName := raw.ToolName
		normalizedName = strings.TrimPrefix(normalizedName, "mcp__")

		content := buildCodexToolContent(normalizedName, raw.ToolInput, raw.ToolResp)
		meta := buildFullMeta("post_tool", data)

		if content != "" {
			logDiscussion(raw.SessionID, "assistant", "codex-cli", content, meta)
			logf("PostToolUse %s logged", raw.ToolName)
		} else {
			logf("PostToolUse %s — empty content, skipped", raw.ToolName)
		}

	case "Stop":
		// Try to find the assistant's final response text.
		respText := raw.Response
		if respText == "" {
			respText = raw.Output
		}
		if respText == "" {
			respText = raw.Text
		}
		if respText != "" {
			meta := buildFullMeta("stop", data)
			logDiscussion(raw.SessionID, "assistant", "codex-cli", respText, meta)
			logf("Stop logged (%d chars)", len(respText))
		} else {
			// Log the raw JSON as metadata even without text
			meta := buildFullMeta("stop", data)
			logDiscussion(raw.SessionID, "assistant", "codex-cli", "(turn stopped)", meta)
			logf("Stop logged (no response text)")
		}

	default:
		logf("unhandled event=%s, ignored", raw.Event)
	}
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
			// If tool_input is a plain string, use it directly
			var rawCmd string
			if json.Unmarshal(toolInput, &rawCmd) == nil && rawCmd != "" {
				cmd = rawCmd
			}
		}
		if cmd == "" {
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

// ---- Codex CLI hook setup ----

// setupCodexHooks writes Codex CLI hook configuration to .codex/hooks.json.
// Hooks are enabled by default in Codex CLI — no feature flag needed.
func setupCodexHooks(commandPath string) error {
	runtimeDir, _ := findRuntimeDir()
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

	// Build hook entries — no matcher needed (match all).
	// Quote the command path in case it contains spaces.
	makeEntry := func() []any {
		return []any{
			map[string]any{
				"matcher": "",
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": "\"" + commandPath + "\" hook-codex",
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
