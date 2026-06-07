package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// processClaudeHook reads the Claude Code PostToolUse/Stop/UserPromptSubmit hook stdin
// JSON and saves to discussion_log with structuredPatch hunks as metadata.
// Uses Go's encoding/json — 100% reliable, zero shell dependency.
// Called via: aipmc hook-process
func processClaudeHook() {
	data, _ := io.ReadAll(os.Stdin)
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
			OriginalFile string `json:"originalFile"`
			FilePath     string `json:"filePath"`
		} `json:"tool_response"`
		StructuredPatch []patchHunk `json:"structuredPatch"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		os.Exit(0)
	}

	switch raw.Event {
	case "UserPromptSubmit":
		if raw.Prompt != "" {
			logDiscussion(raw.SessionID, "user", "claude-code", raw.Prompt, "")
		}

	case "Stop", "StopFailure":
		if raw.LastAssistantMessage != "" {
			logDiscussion(raw.SessionID, "assistant", "claude-code", raw.LastAssistantMessage, "")
		}

	case "PostToolUse":
		desc := raw.ToolName
		ti := raw.ToolInput
		var metadataJSON string

		switch raw.ToolName {
		case "Write":
			if ti.FilePath != "" {
				isNewFile := raw.ToolResponse.OriginalFile == ""
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
					meta := editMeta{Type: "edit", FilePath: ti.FilePath, Hunks: raw.StructuredPatch}
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
					Type     string      `json:"type"`
					FilePath string      `json:"file_path"`
					Hunks    []patchHunk `json:"hunks"`
				}
				meta := editMeta{Type: "edit", FilePath: ti.FilePath, Hunks: raw.StructuredPatch}
				if b, err := json.Marshal(meta); err == nil {
					metadataJSON = string(b)
				}
			}
		case "Bash":
			if ti.Command != "" { desc = "🔧 " + ti.Command }
		case "Read":
			if ti.FilePath != "" { desc = "👁 " + ti.FilePath }
		case "Grep":
			if ti.Command != "" { desc = "🔍 " + ti.Command }
		default:
			desc = "🛠 " + raw.ToolName
		}

		// Debug log
		if raw.ToolName == "Write" || raw.ToolName == "Edit" {
			f, _ := os.OpenFile(filepath.Join(os.TempDir(), "aipm-hook-write-full.log"),
				os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if f != nil {
				f.WriteString("\n=== " + raw.ToolName + " " + ti.FilePath + " ===\n")
				f.Write(data)
				f.WriteString("\n=== END ===\n\n")
				f.Close()
			}
		}

		if desc != "" {
			logDiscussion(raw.SessionID, "assistant", "claude-code", desc, metadataJSON)
		}
	}
	os.Exit(0)
}
