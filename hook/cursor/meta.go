package cursor

import (
	"encoding/json"
	"time"
)

// EnrichMeta extracts standardized diff fields from Cursor postToolUse payloads.
func EnrichMeta(metaJSON, sessionID, toolName string, toolInput, toolResp json.RawMessage) string {
	var meta map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		return metaJSON
	}

	ti := parseToolInput(toolInput)
	fp := toolFilePath(ti, toolResp)

	switch toolName {
	case "Edit", "edit", "StrReplace", "str_replace":
		enrichEditMeta(meta, sessionID, fp, toolResp, ti)
	case "Write", "write":
		enrichWriteMeta(meta, sessionID, fp, toolResp, ti)
	case "Shell", "Bash", "shell", "bash":
		enrichShellMeta(meta, ti)
	case "Read", "read":
		enrichReadMeta(meta, toolFilePath(ti, toolResp), toolResp)
	case "Grep", "grep":
		if fp := firstNonEmpty(ti["file_path"], ti["path"]); fp != "" {
			meta["file_path"] = fp
		}
		if p := firstNonEmpty(ti["pattern"], ti["query"]); p != "" {
			meta["pattern"] = p
		}
	case "Delete", "delete":
		if fp := toolFilePath(ti, toolResp); fp != "" {
			meta["file_path"] = fp
			meta["type"] = "delete"
		}
	}

	b, _ := json.Marshal(meta)
	return string(b)
}

// RefreshFileToolContent rebuilds display text from enriched metadata.
func RefreshFileToolContent(content, metaJSON string) string {
	var meta map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		return FinalizeToolContent("", content, metaJSON, nil)
	}
	fp := metaString(meta, "file_path")
	if fp == "" {
		return FinalizeToolContent("", content, metaJSON, nil)
	}
	return FormatFileEditContent(fp, meta)
}

func enrichEditMeta(meta map[string]any, sessionID, filePath string, toolResp json.RawMessage, ti map[string]string) {
	if cached := takeCursorFileEditWithRetry(sessionID, filePath, 3*time.Second); cached != nil {
		ApplyEditsToMeta(meta, filePath, cached.Edits, "")
		return
	}

	meta["type"] = "edit"
	if filePath != "" {
		meta["file_path"] = filePath
	}

	if hunks := extractHunksFromCursorResp(toolResp); len(hunks) > 0 {
		meta["hunks"] = hunks
	}

	oldStr := firstNonEmpty(ti["old_string"], ti["oldString"], ti["old_str"])
	newStr := firstNonEmpty(ti["new_string"], ti["newString"], ti["new_str"])
	if oldStr != "" {
		meta["old_string"] = oldStr
	}
	if newStr != "" {
		meta["new_string"] = newStr
	}
}

func enrichWriteMeta(meta map[string]any, sessionID, filePath string, toolResp json.RawMessage, ti map[string]string) {
	writeContent := ti["content"]
	if cached := takeCursorFileEditWithRetry(sessionID, filePath, 3*time.Second); cached != nil {
		ApplyEditsToMeta(meta, filePath, cached.Edits, writeContent)
		return
	}

	if filePath != "" {
		meta["file_path"] = filePath
	}

	if isCursorNewFileFromToolResp(toolResp) {
		meta["type"] = "new_file"
		return
	}

	meta["type"] = "edit"
	oldStr := firstNonEmpty(ti["old_string"], ti["oldString"], ti["old_str"])
	newStr := firstNonEmpty(ti["new_string"], ti["newString"], ti["new_str"])
	if oldStr != "" {
		meta["old_string"] = oldStr
	}
	if newStr != "" {
		meta["new_string"] = newStr
	}
	if oldStr == "" && newStr == "" && writeContent != "" {
		meta["new_string"] = writeContent
	}
	if hunks := extractHunksFromCursorResp(toolResp); len(hunks) > 0 {
		meta["hunks"] = hunks
	}
}

func isCursorNewFileFromToolResp(toolResp json.RawMessage) bool {
	if len(toolResp) == 0 {
		return false
	}
	var resp struct {
		Created   bool `json:"created"`
		IsNewFile bool `json:"isNewFile"`
		IsNew     bool `json:"isNew"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		return resp.Created || resp.IsNewFile || resp.IsNew
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

func enrichShellMeta(meta map[string]any, ti map[string]string) {
	meta["type"] = "bash"
	if cmd := ti["command"]; cmd != "" {
		meta["command"] = cmd
	}
}

func enrichReadMeta(meta map[string]any, filePath string, toolResp json.RawMessage) {
	meta["type"] = "read"
	if filePath != "" {
		meta["file_path"] = filePath
	}
	if lc := extractCursorLinesCountFromResp(toolResp); lc > 0 {
		meta["lines_count"] = lc
	}
}

func extractHunksFromCursorResp(toolResp json.RawMessage) []PatchHunk {
	if len(toolResp) == 0 {
		return nil
	}
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
