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

	// Stderr log: Gemini CLI captures stderr for debugging
	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "[aipm-gemini %s] ", now)
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}

	logf("hook called, stdin=%d bytes", len(data))

	if len(data) < 10 {
		logf("stdin too short, exiting")
		os.Exit(0)
	}

	// Dump raw stdin to file for detailed inspection
	f, _ := os.OpenFile(filepath.Join(os.TempDir(), "aipm-gemini-hook-debug.txt"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		fmt.Fprintf(f, "=== [%s] len=%d ===\n", now, len(data))
		f.Write(data)
		f.WriteString("\n=== END ===\n\n")
		f.Close()
	}

	type toolInput struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
		Query    string `json:"query"`
	}

	var raw struct {
		Event          string          `json:"hook_event_name"`
		Prompt         string          `json:"prompt"`
		PromptResponse string          `json:"prompt_response"`
		ToolName       string          `json:"tool_name"`
		ToolInput      json.RawMessage `json:"tool_input"`
		ToolOutput     string          `json:"tool_output"`
		ExitCode       int             `json:"exit_code"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		logf("JSON parse FAILED: %v", err)
		logf("raw stdin: %s", truncateStr(string(data), 200))
		os.Exit(0)
	}

	logf("event=%s", raw.Event)

	switch raw.Event {
	case "BeforeAgent":
		logf("BeforeAgent: prompt=%d chars", len(raw.Prompt))
		if raw.Prompt != "" {
			logDiscussion("", "user", "gemini-cli", raw.Prompt, "")
			logf("logged to discussion (user/gemini-cli)")
		}

	case "AfterTool":
		if raw.ToolName == "" {
			break
		}

		var ti toolInput
		json.Unmarshal(raw.ToolInput, &ti)

		desc := ""
		var metaJSON string

		switch raw.ToolName {
		case "run_shell_command", "RunShellCommand", "execute_command":
			if ti.Command != "" {
				cmdPreview := ti.Command
				if len(cmdPreview) > 150 {
					cmdPreview = cmdPreview[:150] + "..."
				}
				desc = "🔧 " + cmdPreview

				type bashMeta struct {
					Type     string `json:"type"`
					Command  string `json:"command"`
					ExitCode int    `json:"exit_code"`
					Output   string `json:"output,omitempty"`
				}
				stdout := truncateStr(raw.ToolOutput, 2000)
				meta := bashMeta{
					Type:     "bash",
					Command:  ti.Command,
					ExitCode: raw.ExitCode,
					Output:   stdout,
				}
				if b, _ := json.Marshal(meta); b != nil {
					metaJSON = string(b)
				}

				if stdout != "" {
					desc += "\n  → " + strings.TrimSpace(truncateStr(stdout, 120))
				}
				if raw.ExitCode != 0 {
					desc += fmtExitCode(raw.ExitCode)
				}
			}

		case "read_file", "ReadFile", "read":
			if ti.FilePath != "" {
				desc = "👁 " + ti.FilePath
				if raw.ToolOutput != "" {
					lines := strings.Count(raw.ToolOutput, "\n") + 1
					desc += fmt.Sprintf(" (%d lines)", lines)

					type readMeta struct {
						Type     string `json:"type"`
						FilePath string `json:"file_path"`
						Lines    int    `json:"lines"`
						Preview  string `json:"preview,omitempty"`
					}
					meta := readMeta{
						Type:     "read",
						FilePath: ti.FilePath,
						Lines:    lines,
						Preview:  truncateStr(raw.ToolOutput, 150),
					}
					if b, _ := json.Marshal(meta); b != nil {
						metaJSON = string(b)
					}
				}
			}

		case "write_file", "WriteFile", "write", "edit_file", "EditFile", "edit":
			if ti.FilePath != "" {
				isNew := raw.ToolName == "write_file" || raw.ToolName == "WriteFile" || raw.ToolName == "write"
				if isNew {
					desc = "🆕 " + ti.FilePath
					type newFileMeta struct {
						Type     string `json:"type"`
						FilePath string `json:"file_path"`
					}
					if b, _ := json.Marshal(newFileMeta{Type: "new_file", FilePath: ti.FilePath}); b != nil {
						metaJSON = string(b)
					}
				} else {
					desc = "📝 " + ti.FilePath
					if ti.Content != "" {
						preview := truncateStr(ti.Content, 100)
						desc += "\n+ " + strings.TrimSpace(preview)
					}
					type editMeta struct {
						Type     string `json:"type"`
						FilePath string `json:"file_path"`
						Content  string `json:"content,omitempty"`
					}
					if b, _ := json.Marshal(editMeta{Type: "edit", FilePath: ti.FilePath, Content: ti.Content}); b != nil {
						metaJSON = string(b)
					}
				}
			}

		case "search", "grep", "Search", "Grep":
			if ti.Query != "" {
				desc = "🔍 " + ti.Query
			} else if ti.Command != "" {
				desc = "🔍 " + ti.Command
			}

		default:
			desc = "🛠 " + raw.ToolName
			if len(raw.ToolInput) > 0 {
				type unknownMeta struct {
					Type  string          `json:"type"`
					Tool  string          `json:"tool"`
					Input json.RawMessage `json:"input,omitempty"`
				}
				if b, _ := json.Marshal(unknownMeta{Type: "tool", Tool: raw.ToolName, Input: raw.ToolInput}); b != nil {
					metaJSON = string(b)
				}
			}
		}

		if desc != "" {
			logDiscussion("", "assistant", "gemini-cli", desc, metaJSON)
			logf("AfterTool %s logged (desc=%d chars, meta=%d chars)", raw.ToolName, len(desc), len(metaJSON))
		} else {
			logf("AfterTool %s: no description generated", raw.ToolName)
		}

	case "AfterAgent":
		logf("AfterAgent: response=%d chars", len(raw.PromptResponse))
		if raw.PromptResponse != "" {
			logDiscussion("", "assistant", "gemini-cli", raw.PromptResponse, "")
			logf("logged to discussion (assistant/gemini-cli)")
		}

	default:
		logf("unhandled event: %s", raw.Event)
	}
}

// setupGeminiHooks writes Gemini CLI hook configuration to .gemini/settings.json.
// Uses the V2 hook format with matcher (wildcard) and nested hooks array.
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

	// Gemini CLI V2 hook format: [{matcher: "", hooks: [{type, command}]}]
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
