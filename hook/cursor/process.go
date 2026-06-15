package cursor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"

	"aipmc/store"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// ProcessHook reads Cursor hook stdin JSON and saves to discussion_log.
func ProcessHook() {
	now := time.Now().Format("2006-01-02T15:04:05.000")
	data, _ := io.ReadAll(os.Stdin)

	// Cursor on Chinese Windows may send hook JSON in GBK/CP936 instead of
	// UTF-8. When bytes are not valid UTF-8, decode as GBK. For mojibake
	// (bytes that are valid-but-wrong UTF-8), FixHookText handles it per-field.
	if !utf8.Valid(data) {
		if fixed, err := gbkToUTF8(data); err == nil {
			data = fixed
		}
	}

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
	note := func(format string, args ...any) {
		AppendHookDiagnostic("cursor", now, fmt.Sprintf(format, args...))
	}

	defer func() {
		if r := recover(); r != nil {
			logf("PANIC: %v\n%s", r, string(debug.Stack()))
			os.Exit(0)
		}
	}()

	if len(data) < 2 {
		os.Exit(0)
	}

	raw, data := parseHookData(data)
	sid := firstNonEmpty(raw.SessionID, raw.ConversationID)
	transcriptPath := extractTranscriptPath(data)
	logf("event=%s session=%s tool=%s prompt=%dch text=%dch transcript=%s", raw.Event, sid, raw.ToolName, len(raw.Prompt), len(raw.Text), transcriptPath)

	switch raw.Event {
	case "beforeSubmitPrompt":
		prompt := PreferUserPromptAtSubmit(raw.Prompt)
		genID := raw.GenerationID
		if shouldDeferUserPrompt(raw.Prompt, prompt) {
			cacheDeferredUserPrompt(sid, genID, transcriptPath, raw.Prompt)
			logf("beforeSubmitPrompt deferred (transcript backfill): gen=%s", genID)
		} else if prompt != "" {
			meta := buildFullMeta("user_prompt", data)
			if _, err := store.LogDiscussion(sid, "user", "cursor", prompt, meta); err != nil {
				logf("beforeSubmitPrompt log FAILED: %v", err)
				note("beforeSubmitPrompt log FAILED: %v", err)
			} else {
				logf("beforeSubmitPrompt logged (%d chars)", len(prompt))
			}
		} else {
			cacheDeferredUserPrompt(sid, genID, transcriptPath, raw.Prompt)
			logf("beforeSubmitPrompt: empty prompt, deferred")
		}
		fmt.Println(`{"continue":true}`)
		os.Exit(0)

	case "afterAgentResponse":
		flushDeferredUserPrompt(sid, raw.GenerationID, transcriptPath, data)
		text := preferAssistantText(raw.Text, transcriptPath)
		if text != "" {
			meta := buildFullMeta("assistant_message", data)
			if _, err := store.LogDiscussion(sid, "assistant", "cursor", text, meta); err != nil {
				logf("afterAgentResponse log FAILED: %v", err)
				note("afterAgentResponse log FAILED: %v", err)
			} else {
				logf("afterAgentResponse logged (%d chars)", len(text))
			}
		} else {
			logf("afterAgentResponse: empty text, skipped")
		}
		os.Exit(0)

	case "postToolUse":
		flushDeferredUserPrompt(sid, raw.GenerationID, transcriptPath, data)
		if raw.ToolName == "" {
			logf("postToolUse: empty tool_name, skipped")
			os.Exit(0)
		}

		normalizedName := normalizeToolName(raw.ToolName)
		toolResp := raw.ToolResp
		if len(toolResp) == 0 {
			toolResp = raw.ToolOutput
		}

		ti := parseToolInput(raw.ToolInput)
		fp := toolFilePath(ti, toolResp)
		// Cursor emits afterFileEdit with old/new text pairs but NO line numbers.
		// postToolUse has structuredPatch hunks with line numbers (oldStart/newStart).
		// Cache the hunks here so afterFileEdit can include them in metadata for
		// the frontend to render proper unified-diff blocks.
		if isFileEditTool(normalizedName) && fp != "" {
			if hunks := extractHunksFromCursorResp(toolResp); len(hunks) > 0 {
				cacheCursorHunks(sid, fp, hunks)
				logf("postToolUse %s cached %d hunks for afterFileEdit: %s", raw.ToolName, len(hunks), fp)
			} else {
				logf("postToolUse %s skipped (no hunks, afterFileEdit owns file edits): %s", raw.ToolName, fp)
			}
			os.Exit(0)
		}

		meta := buildFullMeta("post_tool", data)
		meta = EnrichMeta(meta, sid, normalizedName, raw.ToolInput, toolResp)

		content := FinalizeToolContent(
			normalizedName,
			BuildToolContent(normalizedName, raw.ToolInput, toolResp),
			meta,
			raw.ToolInput,
		)
		if IsLowValueToolContent(content) {
			logf("postToolUse %s skipped (low-value): %s", raw.ToolName, truncateText(content, 60))
			os.Exit(0)
		}
		if content == "" {
			content = iconTool + normalizedName
		}

		if _, err := store.LogDiscussion(sid, "assistant", "cursor", content, meta); err != nil {
			logf("postToolUse %s log FAILED: %v", raw.ToolName, err)
			note("postToolUse %s log FAILED: %v", raw.ToolName, err)
		} else {
			logf("postToolUse %s logged", raw.ToolName)
		}
		os.Exit(0)

	case "afterFileEdit":
		flushDeferredUserPrompt(sid, raw.GenerationID, transcriptPath, data)
		edits := parseCursorFileEdits(raw.Edits)
		if raw.FilePath == "" || len(edits) == 0 {
			if raw.FilePath == "" {
				logf("afterFileEdit skipped (no file_path)")
				note("afterFileEdit skipped (no file_path)")
			} else if len(raw.Edits) > 0 {
				// Edits field present but unparseable (JSON corrupted by Chinese chars).
				logf("afterFileEdit skipped (JSON corrupt for edits): %s", raw.FilePath)
				note("afterFileEdit skipped (JSON corrupt, lenient tried): %s", raw.FilePath)
			} else {
				logf("afterFileEdit skipped (edits field empty): %s", raw.FilePath)
				note("afterFileEdit skipped (edits field empty): %s", raw.FilePath)
			}
			os.Exit(0)
		}
		cacheCursorFileEdit(sid, raw.FilePath, edits)

		if wasDuplicateFileEdit(sid, raw.GenerationID, raw.FilePath, edits) {
			logf("afterFileEdit skipped (duplicate): %s", raw.FilePath)
			note("afterFileEdit skipped (duplicate): %s", raw.FilePath)
			os.Exit(0)
		}

		metaJSON := buildAfterFileEditMeta(buildFullMeta("after_file_edit", data), sid, raw.FilePath, edits)
		var meta map[string]any
		if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
			meta = map[string]any{"file_path": raw.FilePath, "type": "edit"}
		}
		content := FormatFileEditContent(raw.FilePath, meta)
		if IsLowValueToolContent(content) {
			logf("afterFileEdit skipped (low-value): %s", raw.FilePath)
			note("afterFileEdit skipped (low-value): %s", raw.FilePath)
			os.Exit(0)
		}

		if _, err := store.LogDiscussion(sid, "assistant", "cursor", content, metaJSON); err != nil {
			logf("afterFileEdit log FAILED: %v", err)
			note("afterFileEdit log FAILED %s: %v", raw.FilePath, err)
		} else {
			markFileEditLogged(sid, raw.GenerationID, raw.FilePath, edits)
			logf("afterFileEdit logged %d edit(s) for %s", len(edits), raw.FilePath)
		}
		os.Exit(0)

	case "postToolUseFailure":
		if raw.ToolName == "" {
			logf("postToolUseFailure: empty tool_name, skipped")
			os.Exit(0)
		}
		failName := normalizeToolName(raw.ToolName)
		// Extract structured failure fields for richer display.
		var failInfo struct {
			FailureType string `json:"failure_type"`
			IsInterrupt bool   `json:"is_interrupt"`
		}
		json.Unmarshal(data, &failInfo)
		content := BuildToolFailureContent(failName, raw.ToolInput, raw.ErrorMessage)
		if failInfo.IsInterrupt {
			content += " [interrupted]"
		} else if failInfo.FailureType == "timeout" {
			content += " [timeout]"
		} else if failInfo.FailureType == "permission_denied" {
			content += " [denied]"
		}
		meta := buildFullMeta("post_tool_failure", data)
		if _, err := store.LogDiscussion(sid, "assistant", "cursor", content, meta); err != nil {
			logf("postToolUseFailure log FAILED: %v", err)
		} else {
			logf("postToolUseFailure %s logged", raw.ToolName)
		}
		os.Exit(0)

	case "afterShellExecution":
		// Cursor fires afterShellExecution for every shell command (alongside
		// postToolUse Shell). Capture the sandbox field not available in postToolUse.
		var shell struct {
			Command  string `json:"command"`
			Output   string `json:"output"`
			Duration int    `json:"duration"`
			Sandbox  bool   `json:"sandbox"`
		}
		if err := json.Unmarshal(data, &shell); err == nil && shell.Command != "" {
			meta := buildFullMeta("after_shell_execution", data)
			ct := iconShell + truncateText(shell.Command, 150)
			if shell.Output != "" {
				ct += "\n  -> " + truncateText(shell.Output, 120)
			}
			if shell.Sandbox {
				ct += " [sandbox]"
			}
			if _, err := store.LogDiscussion(sid, "assistant", "cursor", ct, meta); err != nil {
				logf("afterShellExecution log FAILED: %v", err)
			} else {
				logf("afterShellExecution logged (sandbox=%v)", shell.Sandbox)
			}
		}
		os.Exit(0)

	case "stop":
		// Observer-only: do not log session-end noise to discussion_log.
		// IMPORTANT: do NOT return followup_message -- we are observer-only.
		logf("stop ignored (not recorded)")
		os.Exit(0)

	case "afterAgentThought":
		// Agent thinking/reasoning. Store preview in content, full text in metadata.
		if raw.Text != "" {
			meta := buildFullMeta("after_agent_thought", data)
			preview := iconThought + truncateText(FixHookText(raw.Text), 200)
			if _, err := store.LogDiscussion(sid, "assistant", "cursor", preview, meta); err != nil {
				logf("afterAgentThought log FAILED: %v", err)
			} else {
				logf("afterAgentThought logged (%d chars)", len(raw.Text))
			}
		}
		os.Exit(0)

	default:
		logf("event=%s ignored (not recorded)", raw.Event)
		os.Exit(0)
	}
}

// gbkToUTF8 converts GBK/CP936 encoded data to UTF-8.
// Cursor on Chinese Windows may emit hook JSON in the system encoding.
func gbkToUTF8(data []byte) ([]byte, error) {
	reader := transform.NewReader(bytes.NewReader(data), simplifiedchinese.GBK.NewDecoder())
	return io.ReadAll(reader)
}

func normalizeToolName(name string) string {
	name = strings.TrimPrefix(name, "mcp__")
	name = strings.TrimPrefix(name, "MCP__")
	return name
}

func isFileEditTool(toolName string) bool {
	switch toolName {
	case "Write", "write", "Edit", "edit", "StrReplace", "str_replace":
		return true
	default:
		return false
	}
}

// IsLowValueToolContent filters noise like redirect tokens mis-parsed as file paths.
func IsLowValueToolContent(content string) bool {
	if content == "" {
		return false
	}
	switch content {
	case iconEdit + "&1", iconEdit + "Write", iconEdit + "edit.json,", iconEdit + "edit.json":
		return true
	}
	if strings.HasPrefix(content, iconEdit+"&") {
		return true
	}
	rest := strings.TrimPrefix(content, iconEdit)
	return rest == "Write" || strings.HasSuffix(rest, ",")
}
