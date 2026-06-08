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

// processGeminiHook reads the Gemini CLI hook stdin JSON and saves to discussion_log.
// Called via: aipmc hook-gemini
func processGeminiHook() {
	now := time.Now().Format("15:04:05.000")
	data, _ := io.ReadAll(os.Stdin)

	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "[aipm-gemini %s] ", now)
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}

	logf("hook called, stdin=%d bytes", len(data))
	if len(data) < 10 {
		logf("stdin too short, exiting")
		os.Exit(0)
	}

	// Debug dump for inspection
	f, _ := os.OpenFile(filepath.Join(os.TempDir(), "aipm-gemini-hook-debug.txt"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		fmt.Fprintf(f, "=== [%s] len=%d ===\n", now, len(data))
		f.Write(data)
		f.WriteString("\n=== END ===\n\n")
		f.Close()
	}

	type toolInput struct {
		Command   string `json:"command"`
		FilePath  string `json:"file_path"`
		DirPath   string `json:"dir_path"`
		Content   string `json:"content"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
		Query     string `json:"query"`
	}

	var raw struct {
		Event     string `json:"hook_event_name"`
		SessionID string `json:"session_id"`
		Prompt    string `json:"prompt"`
		Response  string `json:"prompt_response"`
		ToolName  string `json:"tool_name"`
		ToolInput json.RawMessage
		ToolResp  struct {
			LLMContent json.RawMessage `json:"llmContent"`
			ExitCode   int             `json:"exit_code"`
		} `json:"tool_response"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		logf("JSON parse FAILED: %v", err)
		os.Exit(0)
	}
	logf("event=%s", raw.Event)

	switch raw.Event {
	case "BeforeAgent":
		if raw.Prompt != "" {
			logDiscussion(raw.SessionID, "user", "gemini-cli", raw.Prompt, "")
			logf("BeforeAgent logged (%d chars)", len(raw.Prompt))
		}

	case "AfterAgent":
		if raw.Response != "" {
			logDiscussion(raw.SessionID, "assistant", "gemini-cli", raw.Response, "")
			logf("AfterAgent logged (%d chars)", len(raw.Response))
		}

	case "BeforeTool", "AfterTool":
		if raw.ToolName == "" {
			break
		}
		raw.ToolName = strings.TrimPrefix(raw.ToolName, "mcp__aipm__")

		var ti toolInput
		json.Unmarshal(raw.ToolInput, &ti)
		llmText := extractLLMText(raw.ToolResp.LLMContent)

		desc := ""
		var metaJSON string

		switch {
		// Shell commands
		case raw.ToolName == "run_shell_command" || raw.ToolName == "execute_command":
			cmd := ti.Command
			if cmd == "" {
				cmd = fmt.Sprintf("%v", raw.ToolInput)
			}
			if len(cmd) > 150 {
				cmd = cmd[:150] + "..."
			}
			desc = "🔧 " + cmd

			type bashMeta struct {
				Type     string `json:"type"`
				Command  string `json:"command"`
				ExitCode int    `json:"exit_code"`
				Output   string `json:"output,omitempty"`
			}
			m := bashMeta{Type: "bash", Command: ti.Command, ExitCode: raw.ToolResp.ExitCode, Output: truncateStr(llmText, 2000)}
			if b, _ := json.Marshal(m); b != nil {
				metaJSON = string(b)
			}
			if llmText != "" {
				desc += "\n  → " + strings.TrimSpace(truncateStr(llmText, 120))
			}
			if raw.ToolResp.ExitCode != 0 {
				desc += fmtExitCode(raw.ToolResp.ExitCode)
			}

		// File reading
		case raw.ToolName == "read_file":
			fp := ti.FilePath
			if fp == "" {
				fp = ti.DirPath
			}
			if fp != "" {
				desc = "👁 " + fp
				if llmText != "" {
					lines := strings.Count(llmText, "\n") + 1
					desc += fmt.Sprintf(" (%d lines)", lines)
					type readMeta struct {
						Type     string `json:"type"`
						FilePath string `json:"file_path"`
						Lines    int    `json:"lines"`
						Preview  string `json:"preview,omitempty"`
					}
					m := readMeta{Type: "read", FilePath: fp, Lines: lines, Preview: truncateStr(llmText, 150)}
					if b, _ := json.Marshal(m); b != nil {
						metaJSON = string(b)
					}
				}
			}

		// Directory listing
		case raw.ToolName == "list_directory":
			fp := ti.DirPath
			if fp == "" {
				fp = ti.FilePath
			}
			if fp != "" {
				desc = "📂 " + fp
				if llmText != "" {
					desc += "\n  → " + truncateStr(llmText, 120)
				}
			}

		// File writing (new file)
		case raw.ToolName == "write_file" || raw.ToolName == "write":
			if ti.FilePath != "" {
				desc = "🆕 " + ti.FilePath
				type newFileMeta struct {
					Type     string `json:"type"`
					FilePath string `json:"file_path"`
				}
				if b, _ := json.Marshal(newFileMeta{Type: "new_file", FilePath: ti.FilePath}); b != nil {
					metaJSON = string(b)
				}
			}

		// File editing
		case raw.ToolName == "replace" || raw.ToolName == "edit_file" || raw.ToolName == "edit":
			if ti.FilePath != "" {
				desc = "📝 " + ti.FilePath
				if ti.OldString != "" {
					desc += "\n- " + strings.TrimSpace(ti.OldString)
				}
				if ti.NewString != "" {
					desc += "\n+ " + strings.TrimSpace(ti.NewString)
				}
				type editMeta struct {
					Type      string `json:"type"`
					FilePath  string `json:"file_path"`
					OldString string `json:"old_string,omitempty"`
					NewString string `json:"new_string,omitempty"`
				}
				m := editMeta{Type: "edit", FilePath: ti.FilePath, OldString: ti.OldString, NewString: ti.NewString}
				if b, _ := json.Marshal(m); b != nil {
					metaJSON = string(b)
				}
			}

		// MCP tools (mcp__aipm__ prefix already stripped)
		case strings.HasPrefix(raw.ToolName, "aipm_"):
			name := "📡 " + raw.ToolName
			desc = name
			if ti.Query != "" {
				q := ti.Query
				if len(q) > 60 { q = q[:60] + "..." }
				desc += " \"" + q + "\""
			}

		default:
			desc = "🛠 " + raw.ToolName
		}

		if desc != "" {
			logDiscussion(raw.SessionID, "assistant", "gemini-cli", desc, metaJSON)
			logf("%s %s logged", raw.Event, raw.ToolName)
		}
	}
}

// extractLLMText extracts readable text from Gemini CLI's llmContent field.
// llmContent can be: a plain string, an array of {text: "..."} objects, or null.
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
		return strings.Join(parts, "\n")
	}
	return ""
}

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

	hookEntry := []any{
		map[string]any{
			"matcher": "",
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": commandPath + " hook-gemini",
				},
			},
		},
	}

	hooks["BeforeAgent"] = hookEntry
	hooks["AfterAgent"] = hookEntry
	hooks["BeforeTool"] = hookEntry
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
