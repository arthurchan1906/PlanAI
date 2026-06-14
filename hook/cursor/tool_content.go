package cursor

import (
	"encoding/json"
	"strings"
)

func toolFilePath(ti map[string]string, toolResp json.RawMessage) string {
	if fp := firstNonEmpty(ti["file_path"], ti["filePath"], ti["path"], ti["target_file"], ti["target"]); fp != "" {
		return fp
	}
	if len(toolResp) == 0 {
		return ""
	}
	var resp struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		return firstNonEmpty(resp.FilePath, resp.Path)
	}
	var s string
	if json.Unmarshal(toolResp, &s) == nil && s != "" {
		_ = json.Unmarshal([]byte(s), &resp)
		return firstNonEmpty(resp.FilePath, resp.Path)
	}
	return ""
}

func metaString(meta map[string]any, key string) string {
	if v, ok := meta[key].(string); ok {
		return v
	}
	return ""
}

func isGenericToolLabel(content string) bool {
	if content == "" {
		return true
	}
	for _, prefix := range []string{iconTool, iconRead, iconGrep, iconDir, iconDelete, iconWeb} {
		if strings.HasPrefix(content, prefix) {
			rest := strings.TrimPrefix(content, prefix)
			if !strings.Contains(rest, "/") && !strings.Contains(rest, "\\") &&
				!strings.Contains(rest, ":") && !strings.Contains(rest, "\"") &&
				len(rest) < 40 {
				return true
			}
		}
	}
	return false
}

// FormatFileEditContent builds discussion content with file path and diff preview.
func FormatFileEditContent(filePath string, meta map[string]any) string {
	icon := iconEdit
	if metaString(meta, "type") == "new_file" {
		icon = iconNewFile
	}
	result := icon + filePath

	oldS := metaString(meta, "old_string")
	newS := metaString(meta, "new_string")
	if oldS != "" {
		result += "\n- " + strings.TrimSpace(truncateText(oldS, 100))
	}
	if newS != "" {
		result += "\n+ " + strings.TrimSpace(truncateText(newS, 100))
	}
	if oldS == "" && newS == "" {
		if all, ok := meta["all_edits"].([]any); ok && len(all) > 1 {
			result += " (" + uitoa(len(all)) + " hunks)"
		}
	}
	return result
}

// FinalizeToolContent ensures file ops include paths and other tools include key args.
func FinalizeToolContent(toolName, content, metaJSON string, toolInput json.RawMessage) string {
	ti := parseToolInput(toolInput)
	var meta map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		meta = map[string]any{}
	}

	fp := firstNonEmpty(metaString(meta, "file_path"), toolFilePath(ti, nil))

	switch metaString(meta, "type") {
	case "new_file":
		if fp != "" {
			return iconNewFile + fp
		}
	case "edit":
		if fp != "" {
			return iconEdit + fp
		}
	case "read":
		if fp != "" {
			return iconRead + fp
		}
	case "bash":
		if cmd := firstNonEmpty(metaString(meta, "command"), ti["command"]); cmd != "" {
			return iconShell + truncateText(cmd, 150)
		}
	}

	if isGenericToolLabel(content) {
		if rebuilt := BuildToolContent(toolName, toolInput, nil); rebuilt != "" && !isGenericToolLabel(rebuilt) {
			return rebuilt
		}
	}

	if fp != "" && isFileTool(toolName) && !strings.Contains(content, fp) {
		return fileToolIcon(toolName, meta) + fp
	}

	return content
}

func isFileTool(toolName string) bool {
	switch toolName {
	case "Write", "write", "Edit", "edit", "StrReplace", "str_replace", "Read", "read", "Delete", "delete", "apply_patch":
		return true
	}
	return strings.HasSuffix(toolName, "_write") || strings.HasSuffix(toolName, "_edit") ||
		strings.HasSuffix(toolName, "_read") || strings.HasSuffix(toolName, "_delete")
}

func fileToolIcon(toolName string, meta map[string]any) string {
	if metaString(meta, "type") == "new_file" {
		return iconNewFile
	}
	switch toolName {
	case "Read", "read":
		return iconRead
	case "Delete", "delete":
		return iconDelete
	default:
		return iconEdit
	}
}

// BuildToolFailureContent formats a failed tool invocation for discussion_log.
func BuildToolFailureContent(toolName string, toolInput json.RawMessage, errMsg string) string {
	base := BuildToolContent(toolName, toolInput, nil)
	if base == "" || isGenericToolLabel(base) {
		base = iconWarn + toolName
	} else {
		for _, pair := range [][2]string{{iconShell, iconWarn}, {iconRead, iconWarn}, {iconGrep, iconWarn}, {iconEdit, iconWarn}, {iconNewFile, iconWarn}} {
			base = strings.Replace(base, pair[0], pair[1], 1)
		}
		if !strings.HasPrefix(base, iconWarn) {
			base = iconWarn + base
		}
	}
	if !strings.Contains(base, "failed") {
		base += " failed"
	}
	if errMsg != "" {
		base += ": " + truncateText(errMsg, 120)
	}
	return base
}
