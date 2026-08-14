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

// processClaudeHook reads the Claude Code PostToolUse/Stop/UserPromptSubmit hook stdin
// JSON and saves to discussion_log with structuredPatch hunks as metadata.
// Uses Go's encoding/json — 100% reliable, zero shell dependency.
// Called via: aipmc hook-process
func ProcessClaudeHook() {
	now := time.Now().Format("2006-01-02T15:04:05.000")
	data, _ := io.ReadAll(os.Stdin)

	if os.Getenv("AIPM_DEBUG_HOOK") != "" {
		dumpRawHook("claude", now, data)
	}

	// Catch panics so a bug never crashes the parent process.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[aipm-claude %s] PANIC: %v\n%s\n", now, r, string(debug.Stack()))
			u.LogShared("HOOK", "panic src=claude err=%v", r)
			os.Exit(0)
		}
	}()

	if len(data) < 10 {
		os.Exit(0)
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
		ToolResponse toolResponse `json:"tool_response"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "[aipm-claude %s] JSON parse FAILED: %v — raw(first 200): %s\n", now, err, u.SafePrefix(string(data), 200))
		u.LogShared("HOOK", "json_parse_err src=claude err=%v", err)
		os.Exit(0)
	}

	// Capture assistant text from PostToolUse and Stop events.
	// Both carry last_assistant_message; dedup via DB check.
	if raw.LastAssistantMessage != "" && (raw.Event == "PostToolUse" || raw.Event == "Stop" || raw.Event == "StopFailure") {
		if _, err := store.LogDiscussion(raw.SessionID, "assistant", "claude-code", raw.LastAssistantMessage, ""); err != nil {
			fmt.Fprintf(os.Stderr, "[aipm-claude %s] assistant log FAILED: %v\n", now, err)
			u.LogShared("HOOK", "write_err src=claude role=assistant err=%v", err)
		}
	}

	switch raw.Event {
	case "UserPromptSubmit":
		if raw.Prompt != "" {
			if _, err := store.LogDiscussion(raw.SessionID, "user", "claude-code", raw.Prompt, ""); err != nil {
				fmt.Fprintf(os.Stderr, "[aipm-claude %s] UserPromptSubmit log FAILED: %v\n", now, err)
				u.LogShared("HOOK", "write_err src=claude role=user err=%v", err)
			}
		}

	case "Stop", "StopFailure":

	case "PostToolUse":
		desc := raw.ToolName
		ti := raw.ToolInput
		tr := raw.ToolResponse
		var metadataJSON string

		switch raw.ToolName {
		case "Write":
			files := collectWriteFiles(ti.FilePath, tr)
			if len(files) > 0 {
				allNew := true
				for _, f := range files {
					if f.OriginalFile != "" {
						allNew = false
						break
					}
				}
				if allNew {
					desc = "🆕 " + files[0].File
					type newFileMeta struct {
						Type     string   `json:"type"`
						FilePath string   `json:"file_path"`
						RelPath  string   `json:"rel_path,omitempty"`
						RelPaths []string `json:"rel_paths,omitempty"`
						Source   string   `json:"source,omitempty"`
					}
					meta := newFileMeta{Type: "new_file", FilePath: files[0].File, RelPath: ToRelPath(files[0].File), Source: "structured"}
					meta.RelPaths = relPathsOf(files)
					if b, err := json.Marshal(meta); err == nil {
						metadataJSON = string(b)
					}
				} else {
					desc = "📝 " + files[0].File
					type editMeta struct {
						Type     string      `json:"type"`
						FilePath string      `json:"file_path"`
						RelPath  string      `json:"rel_path,omitempty"`
						RelPaths []string    `json:"rel_paths,omitempty"`
						Source   string      `json:"source,omitempty"`
						Hunks    []patchHunk `json:"hunks"`
					}
					meta := editMeta{Type: "edit", FilePath: files[0].File, RelPath: ToRelPath(files[0].File), Source: "structured", Hunks: raw.ToolResponse.StructuredPatch}
					meta.RelPaths = relPathsOf(files)
					if b, err := json.Marshal(meta); err == nil {
						metadataJSON = string(b)
					}
				}
			}
		case "Edit":
			editPath := ti.FilePath
			if editPath == "" {
				editPath = tr.FilePath
			}
			if editPath != "" {
				desc = "📝 " + editPath
				if ti.OldString != "" {
					desc += "\n- " + strings.TrimSpace(ti.OldString)
				}
				if ti.NewString != "" {
					desc += "\n+ " + strings.TrimSpace(ti.NewString)
				}
				type editMeta struct {
					Type      string      `json:"type"`
					FilePath  string      `json:"file_path"`
					RelPath   string      `json:"rel_path,omitempty"`
					Source    string      `json:"source,omitempty"`
					Hunks     []patchHunk `json:"hunks,omitempty"`
					OldString string      `json:"old_string,omitempty"`
					NewString string      `json:"new_string,omitempty"`
				}
				meta := editMeta{
					Type:      "edit",
					FilePath:  editPath,
					RelPath:   ToRelPath(editPath),
					Source:    "structured",
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
				cmdPreview := u.TruncateStr(ti.Command, 150)
				desc = "🔧 " + cmdPreview

				// Capture stdout/stderr in metadata
				type bashMeta struct {
					Type     string   `json:"type"`
					Command  string   `json:"command"`
					ExitCode int      `json:"exit_code"`
					Stdout   string   `json:"stdout,omitempty"`
					Stderr   string   `json:"stderr,omitempty"`
					RelPath  string   `json:"rel_path,omitempty"`
					RelPaths []string `json:"rel_paths,omitempty"`
					Source   string   `json:"source,omitempty"`
				}
				stdout := u.TruncateStr(tr.Stdout, 2000)
				stderr := u.TruncateStr(tr.Stderr, 500)
				meta := bashMeta{
					Type:     "bash",
					Command:  ti.Command,
					ExitCode: tr.ExitCode,
					Stdout:   stdout,
					Stderr:   stderr,
				}
				if ops := extractBashFileOps(ti.Command); len(ops) > 0 {
					meta.Source = "bash_heuristic"
					if tr.ExitCode != 0 {
						meta.Source = "bash_heuristic_unverified"
					}
					var rels []string
					for _, o := range ops {
						if r := ToRelPath(o.File); r != "" {
							rels = append(rels, r)
						}
					}
					if len(rels) > 0 {
						meta.RelPath = rels[0]
						if len(rels) > 1 {
							meta.RelPaths = rels
						}
					}
				}
				if b, err := json.Marshal(meta); err == nil {
					metadataJSON = string(b)
				}

				// Append a preview of the output to the description
				if stdout != "" {
					outputPreview := u.TruncateStr(stdout, 120)
					desc += "\n  → " + strings.TrimSpace(outputPreview)
				} else if stderr != "" {
					errPreview := u.TruncateStr(stderr, 120)
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
					desc += " (" + u.Itoa(tr.LinesCount) + " lines)"
				}
				// Read metadata: rel_path is the part the query layer needs, so
				// write it whenever a file path exists (content preview optional).
				type readMeta struct {
					Type       string `json:"type"`
					FilePath   string `json:"file_path"`
					RelPath    string `json:"rel_path,omitempty"`
					Source     string `json:"source,omitempty"`
					LinesCount int    `json:"lines_count"`
					Preview    string `json:"preview,omitempty"`
				}
				meta := readMeta{
					Type:       "read",
					FilePath:   ti.FilePath,
					RelPath:    ToRelPath(ti.FilePath),
					Source:     "structured",
					LinesCount: tr.LinesCount,
					Preview:    u.TruncateStr(tr.Content, 150),
				}
				if b, err := json.Marshal(meta); err == nil {
					metadataJSON = string(b)
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
			if _, err := store.LogDiscussion(raw.SessionID, "tool", "claude-code", desc, metadataJSON); err != nil {
				fmt.Fprintf(os.Stderr, "[aipm-claude %s] PostToolUse %s log FAILED: %v\n", now, raw.ToolName, err)
				u.LogShared("HOOK", "write_err src=claude role=tool tool=%s err=%v", raw.ToolName, err)
			}
		}
	}
	os.Exit(0)
}

// fmtExitCode returns a human-readable exit code suffix.
func fmtExitCode(code int) string {
	if code == 0 {
		return ""
	}
	return " [exit:" + u.Itoa(code) + "]"
}

// claudeWriteFile is one target of a Write tool call (input + response side).
type claudeWriteFile struct {
	File         string
	OriginalFile string
}

// collectWriteFiles merges Write targets from tool_input.file_path and every
// tool_response element (single object or array), deduplicated. Files present
// only in the response (tool_response.filePath, M1 fix) are no longer lost.
func collectWriteFiles(inputPath string, tr toolResponse) []claudeWriteFile {
	seen := map[string]bool{}
	var out []claudeWriteFile
	add := func(fp, orig string) {
		if fp == "" || seen[fp] {
			return
		}
		seen[fp] = true
		out = append(out, claudeWriteFile{File: fp, OriginalFile: orig})
	}
	add(inputPath, tr.OriginalFile)
	add(tr.FilePath, tr.OriginalFile)
	for _, m := range tr.MultiResults {
		add(m.FilePath, m.OriginalFile)
	}
	return out
}

// relPathsOf returns repo-relative paths for every collected file, dropping
// project-external targets. Only set when there is more than one target.
func relPathsOf(files []claudeWriteFile) []string {
	var rels []string
	for _, f := range files {
		if r := ToRelPath(f.File); r != "" {
			rels = append(rels, r)
		}
	}
	if len(rels) <= 1 {
		return nil
	}
	return rels
}

// setupClaudeHooks writes Claude Code hook configuration to settings.local.json.
// Configures Stop, StopFailure, UserPromptSubmit, and PostToolUse hooks
// that all call "aipmc hook-process" (processClaudeHook).
func SetupClaudeHooks(commandPath string) error {
	runtimeDir, err := pmdb.RuntimeDir()
	if err != nil {
		return fmt.Errorf("find runtime dir: %w", err)
	}
	projectRoot := filepath.Dir(runtimeDir)
	settingsPath := filepath.Join(projectRoot, ".claude", "settings.local.json")

	cfg := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parse existing %s (refusing to overwrite): %w", settingsPath, err)
		}
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

// patchHunk mirrors Claude Code's structuredPatch hunk shape.
type patchHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

// toolResponse matches Claude Code's PostToolUse tool_response. It is
// usually a single object, but Claude emits an ARRAY when a tool yields
// multiple results (e.g. multi-file Write) — previously that shape failed
// json.Unmarshal and the whole hook event was silently dropped.
type toolResponse struct {
	OriginalFile    string      `json:"originalFile"`
	FilePath        string      `json:"filePath"`
	Stdout          string      `json:"stdout"`
	Stderr          string      `json:"stderr"`
	ExitCode        int         `json:"exitCode"`
	Content         string      `json:"content"`
	LinesCount      int         `json:"linesCount"`
	StructuredPatch []patchHunk `json:"structuredPatch"`

	// MultiResults holds every element when tool_response is an array
	// (e.g. multi-file Write). Previously the array was collapsed to
	// arr[0], silently dropping the other files (M1, 8/13 review).
	MultiResults []toolResponse `json:"-"`
}

// UnmarshalJSON accepts both a single object and an array of objects,
// keeping the first element as the primary result and the rest in
// MultiResults so no files are lost.
func (t *toolResponse) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '[' {
		var arr []toolResponse
		if err := json.Unmarshal(b, &arr); err != nil {
			return err
		}
		if len(arr) > 0 {
			*t = arr[0]
			if len(arr) > 1 {
				t.MultiResults = arr[1:]
			}
		}
		return nil
	}
	type plain toolResponse
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*t = toolResponse(p)
	return nil
}
