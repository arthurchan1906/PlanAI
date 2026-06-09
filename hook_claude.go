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
)

// processClaudeHook reads the Claude Code PostToolUse/Stop/UserPromptSubmit hook stdin
// JSON and saves to discussion_log with structuredPatch hunks as metadata.
// Uses Go's encoding/json — 100% reliable, zero shell dependency.
// Called via: aipmc hook-process
func processClaudeHook() {
	now := time.Now().Format("2006-01-02T15:04:05.000")
	data, _ := io.ReadAll(os.Stdin)

	// Always persist raw hook JSON for debugging.
	dumpRawHook("claude", now, data)

	// Catch panics so a bug never crashes the parent process.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[aipm-claude %s] PANIC: %v\n%s\n", now, r, string(debug.Stack()))
			os.Exit(0)
		}
	}()

	if len(data) < 10 {
		os.Exit(0)
	}

	type patchHunk struct {
		OldStart int      `json:"oldStart"`
		OldLines int      `json:"oldLines"`
		NewStart int      `json:"newStart"`
		NewLines int      `json:"newLines"`
		Lines    []string `json:"lines"`
	}

	var raw struct {
		Event                string `json:"hook_event_name"`
		SessionID            string `json:"session_id"`
		Prompt               string `json:"prompt"`
		LastAssistantMessage string `json:"last_assistant_message"`
		ToolName             string `json:"tool_name"`
		ToolInput            struct {
			Command   string `json:"command"`
			FilePath  string `json:"file_path"`
			Content   string `json:"content"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"tool_input"`
		ToolResponse struct {
			OriginalFile    string      `json:"originalFile"`
			FilePath        string      `json:"filePath"`
			Stdout          string      `json:"stdout"`
			Stderr          string      `json:"stderr"`
			ExitCode        int         `json:"exitCode"`
			Content         string      `json:"content"`
			LinesCount      int         `json:"linesCount"`
			StructuredPatch []patchHunk `json:"structuredPatch"`
		} `json:"tool_response"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "[aipm-claude %s] JSON parse FAILED: %v — raw(first 200): %s\n", now, err, safePrefix(string(data), 200))
		os.Exit(0)
	}

	switch raw.Event {
	case "UserPromptSubmit":
		if raw.Prompt != "" {
			if _, err := logDiscussion(raw.SessionID, "user", "claude-code", raw.Prompt, ""); err != nil {
				fmt.Fprintf(os.Stderr, "[aipm-claude %s] UserPromptSubmit log FAILED: %v\n", now, err)
			}
		}

	case "Stop", "StopFailure":
		if raw.LastAssistantMessage != "" {
			if _, err := logDiscussion(raw.SessionID, "assistant", "claude-code", raw.LastAssistantMessage, ""); err != nil {
				fmt.Fprintf(os.Stderr, "[aipm-claude %s] Stop log FAILED: %v\n", now, err)
			}
		}

	case "PostToolUse":
		desc := raw.ToolName
		ti := raw.ToolInput
		tr := raw.ToolResponse
		var metadataJSON string

		switch raw.ToolName {
		case "Write":
			if ti.FilePath != "" {
				isNewFile := tr.OriginalFile == ""
				if isNewFile {
					desc = "🆕 " + ti.FilePath
					type newFileMeta struct {
						Type     string `json:"type"`
						FilePath string `json:"file_path"`
					}
					meta := newFileMeta{Type: "new_file", FilePath: ti.FilePath}
					if b, err := json.Marshal(meta); err == nil {
						metadataJSON = string(b)
					}
				} else {
					desc = "📝 " + ti.FilePath
					type editMeta struct {
						Type     string      `json:"type"`
						FilePath string      `json:"file_path"`
						Hunks    []patchHunk `json:"hunks"`
					}
					meta := editMeta{Type: "edit", FilePath: ti.FilePath, Hunks: raw.ToolResponse.StructuredPatch}
					if b, err := json.Marshal(meta); err == nil {
						metadataJSON = string(b)
					}
				}
			}
		case "Edit":
			if ti.FilePath != "" {
				desc = "📝 " + ti.FilePath
				if ti.OldString != "" {
					desc += "\n- " + strings.TrimSpace(ti.OldString)
				}
				if ti.NewString != "" {
					desc += "\n+ " + strings.TrimSpace(ti.NewString)
				}
				type editMeta struct {
					Type      string      `json:"type"`
					FilePath  string      `json:"file_path"`
					Hunks     []patchHunk `json:"hunks,omitempty"`
					OldString string      `json:"old_string,omitempty"`
					NewString string      `json:"new_string,omitempty"`
				}
				meta := editMeta{
					Type:      "edit",
					FilePath:  ti.FilePath,
					Hunks:     raw.ToolResponse.StructuredPatch,
					OldString: ti.OldString,
					NewString: ti.NewString,
				}
				if b, err := json.Marshal(meta); err == nil {
					metadataJSON = string(b)
				}
			}
		case "Bash":
			if ti.Command != "" {
				// Truncate long commands for readability
				cmdPreview := ti.Command
				if len(cmdPreview) > 150 {
					cmdPreview = cmdPreview[:150] + "..."
				}
				desc = "🔧 " + cmdPreview

				// Capture stdout/stderr in metadata
				type bashMeta struct {
					Type     string `json:"type"`
					Command  string `json:"command"`
					ExitCode int    `json:"exit_code"`
					Stdout   string `json:"stdout,omitempty"`
					Stderr   string `json:"stderr,omitempty"`
				}
				stdout := truncateStr(tr.Stdout, 2000)
				stderr := truncateStr(tr.Stderr, 500)
				meta := bashMeta{
					Type:     "bash",
					Command:  ti.Command,
					ExitCode: tr.ExitCode,
					Stdout:   stdout,
					Stderr:   stderr,
				}
				if b, err := json.Marshal(meta); err == nil {
					metadataJSON = string(b)
				}

				// Append a preview of the output to the description
				if stdout != "" {
					outputPreview := truncateStr(stdout, 120)
					desc += "\n  → " + strings.TrimSpace(outputPreview)
				} else if stderr != "" {
					errPreview := truncateStr(stderr, 120)
					desc += "\n  ⚠ " + strings.TrimSpace(errPreview)
				}
				if tr.ExitCode != 0 {
					desc += fmtExitCode(tr.ExitCode)
				}
			}
		case "Read":
			if ti.FilePath != "" {
				desc = "👁 " + ti.FilePath
				if tr.LinesCount > 0 {
					desc += " (" + itoa(tr.LinesCount) + " lines)"
				}
				// Store content preview in metadata
				if tr.Content != "" || tr.LinesCount > 0 {
					type readMeta struct {
						Type       string `json:"type"`
						FilePath   string `json:"file_path"`
						LinesCount int    `json:"lines_count"`
						Preview    string `json:"preview,omitempty"`
					}
					meta := readMeta{
						Type:       "read",
						FilePath:   ti.FilePath,
						LinesCount: tr.LinesCount,
						Preview:    truncateStr(tr.Content, 150),
					}
					if b, err := json.Marshal(meta); err == nil {
						metadataJSON = string(b)
					}
				}
			}
		case "Grep":
			if ti.Command != "" {
				desc = "🔍 " + ti.Command
			}
		default:
			desc = "🛠 " + raw.ToolName
		}

		if desc != "" {
			if _, err := logDiscussion(raw.SessionID, "assistant", "claude-code", desc, metadataJSON); err != nil {
				fmt.Fprintf(os.Stderr, "[aipm-claude %s] PostToolUse %s log FAILED: %v\n", now, raw.ToolName, err)
			}
		}
	}
	os.Exit(0)
}

// truncateStr returns s truncated to maxLen characters, adding "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// fmtExitCode returns a human-readable exit code suffix.
func fmtExitCode(code int) string {
	if code == 0 {
		return ""
	}
	return " [exit:" + itoa(code) + "]"
}

// setupClaudeHooks writes Claude Code hook configuration to settings.local.json.
// Configures Stop, StopFailure, UserPromptSubmit, and PostToolUse hooks
// that all call "aipmc hook-process" (processClaudeHook).
func setupClaudeHooks(commandPath string) error {
	runtimeDir, err := findRuntimeDir()
	if err != nil {
		return fmt.Errorf("find runtime dir: %w", err)
	}
	projectRoot := filepath.Dir(runtimeDir)
	settingsPath := filepath.Join(projectRoot, ".claude", "settings.local.json")

	cfg := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil && len(data) > 0 {
		json.Unmarshal(data, &cfg)
	}

	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	// All hooks use the same command: aipmc hook-process
	// processClaudeHook() dispatches on hook_event_name
	hookEntry := []any{
		map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": commandPath,
					"args":    []string{"hook-process"},
				},
			},
		},
	}

	hooks["Stop"] = hookEntry
	hooks["StopFailure"] = hookEntry
	hooks["UserPromptSubmit"] = hookEntry
	hooks["PostToolUse"] = hookEntry
	cfg["hooks"] = hooks

	os.MkdirAll(filepath.Dir(settingsPath), 0755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	fmt.Printf("  ✅ Hooks configured → %s\n", settingsPath)
	return nil
}
