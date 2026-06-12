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
	"aipmc/u"
)

// ProcessOpenCodeHook reads OpenCode hook stdin JSON and saves to discussion_log.
// OpenCode supports the same hook event names as Claude Code:
// UserPromptSubmit, PostToolUse, Stop.
// Called via: aipmc hook-opencode
func ProcessOpenCodeHook() {
	now := time.Now().Format("2006-01-02T15:04:05.000")
	data, _ := io.ReadAll(os.Stdin)

	if os.Getenv("AIPM_DEBUG_HOOK") != "" {
		dumpRawHook("opencode", now, data)
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[aipm-opencode %s] PANIC: %v\n%s\n", now, r, string(debug.Stack()))
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
		fmt.Fprintf(os.Stderr, "[aipm-opencode %s] JSON parse FAILED: %v — raw(first 200): %s\n", now, err, u.SafePrefix(string(data), 200))
		os.Exit(0)
	}

	switch raw.Event {
	case "UserPromptSubmit":
		if raw.Prompt != "" {
			if _, err := store.LogDiscussion(raw.SessionID, "user", "opencode", raw.Prompt, ""); err != nil {
				fmt.Fprintf(os.Stderr, "[aipm-opencode %s] UserPromptSubmit log FAILED: %v\n", now, err)
			}
		}

	case "Stop", "StopFailure":
		if raw.LastAssistantMessage != "" {
			if _, err := store.LogDiscussion(raw.SessionID, "assistant", "opencode", raw.LastAssistantMessage, ""); err != nil {
				fmt.Fprintf(os.Stderr, "[aipm-opencode %s] Stop log FAILED: %v\n", now, err)
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
					if b, err := json.Marshal(newFileMeta{Type: "new_file", FilePath: ti.FilePath}); err == nil {
						metadataJSON = string(b)
					}
				} else {
					desc = "📝 " + ti.FilePath
					type editMeta struct {
						Type     string      `json:"type"`
						FilePath string      `json:"file_path"`
						Hunks    []patchHunk `json:"hunks"`
					}
					if b, err := json.Marshal(editMeta{Type: "edit", FilePath: ti.FilePath, Hunks: raw.ToolResponse.StructuredPatch}); err == nil {
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
				if b, err := json.Marshal(editMeta{
					Type:      "edit",
					FilePath:  ti.FilePath,
					Hunks:     raw.ToolResponse.StructuredPatch,
					OldString: ti.OldString,
					NewString: ti.NewString,
				}); err == nil {
					metadataJSON = string(b)
				}
			}
		case "Bash":
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
					Stdout   string `json:"stdout,omitempty"`
					Stderr   string `json:"stderr,omitempty"`
				}
				stdout := u.TruncateStr(tr.Stdout, 2000)
				stderr := u.TruncateStr(tr.Stderr, 500)
				if b, err := json.Marshal(bashMeta{
					Type:     "bash",
					Command:  ti.Command,
					ExitCode: tr.ExitCode,
					Stdout:   stdout,
					Stderr:   stderr,
				}); err == nil {
					metadataJSON = string(b)
				}
				if stdout != "" {
					desc += "\n  → " + strings.TrimSpace(u.TruncateStr(stdout, 120))
				} else if stderr != "" {
					desc += "\n  ⚠ " + strings.TrimSpace(u.TruncateStr(stderr, 120))
				}
				if tr.ExitCode != 0 {
					desc += " [exit:" + u.Itoa(tr.ExitCode) + "]"
				}
			}
		case "Read":
			if ti.FilePath != "" {
				desc = "👁 " + ti.FilePath
				if tr.LinesCount > 0 {
					desc += " (" + u.Itoa(tr.LinesCount) + " lines)"
				}
				if tr.Content != "" || tr.LinesCount > 0 {
					type readMeta struct {
						Type       string `json:"type"`
						FilePath   string `json:"file_path"`
						LinesCount int    `json:"lines_count"`
						Preview    string `json:"preview,omitempty"`
					}
					if b, err := json.Marshal(readMeta{
						Type:       "read",
						FilePath:   ti.FilePath,
						LinesCount: tr.LinesCount,
						Preview:    u.TruncateStr(tr.Content, 150),
					}); err == nil {
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
			if _, err := store.LogDiscussion(raw.SessionID, "assistant", "opencode", desc, metadataJSON); err != nil {
				fmt.Fprintf(os.Stderr, "[aipm-opencode %s] PostToolUse %s log FAILED: %v\n", now, raw.ToolName, err)
			}
		}
	}
	os.Exit(0)
}

// SetupOpenCodeHooks writes OpenCode hook configuration to .opencode/hooks.json.
func SetupOpenCodeHooks(commandPath string) error {
	runtimeDir, err := pmdb.RuntimeDir()
	if err != nil {
		return fmt.Errorf("find runtime dir: %w", err)
	}
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

	hookCmd := commandPath + " hook-opencode"
	// Only quote if path contains spaces
	if strings.Contains(commandPath, " ") {
		hookCmd = "\"" + commandPath + "\" hook-opencode"
	}

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

	os.MkdirAll(opencodeDir, 0755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(hooksPath, data, 0644); err != nil {
		return fmt.Errorf("write hooks.json: %w", err)
	}
	fmt.Printf("  ✅ OpenCode hooks configured → %s\n", hooksPath)
	return nil
}
