package cursor

import (
	"encoding/json"
	"strings"
)

// BuildToolContent builds a human-readable description for a Cursor tool call.
func BuildToolContent(toolName string, toolInput, toolResp json.RawMessage) string {
	ti := parseToolInput(toolInput)

	switch {
	case toolName == "Shell" || toolName == "Bash":
		cmd := ti["command"]
		if cmd == "" && len(toolInput) > 0 {
			var rawCmd string
			if json.Unmarshal(toolInput, &rawCmd) == nil && rawCmd != "" {
				cmd = rawCmd
			}
		}
		if cmd == "" {
			cmd = string(toolInput)
		}
		result := iconShell + truncateText(cmd, 150)
		if output := extractCursorBashOutput(toolResp); output != "" {
			result += "\n  -> " + strings.TrimSpace(truncateText(output, 120))
		}
		if ec := extractExitCode(toolResp); ec != 0 {
			result += fmtExitCode(ec)
		}
		return result

	case toolName == "Read" || strings.HasSuffix(toolName, "_read") || strings.HasSuffix(toolName, "_read_file"):
		fp := toolFilePath(ti, toolResp)
		if fp != "" {
			result := iconRead + fp
			if lc := extractCursorLinesCountFromResp(toolResp); lc > 0 {
				result += " (" + uitoa(lc) + " lines)"
			}
			return result
		}
		return iconRead + toolName

	case toolName == "Write" || strings.HasSuffix(toolName, "_write") || strings.HasSuffix(toolName, "_write_file"):
		fp := toolFilePath(ti, toolResp)
		if fp != "" {
			if isNewFile(toolResp) || isCursorNewFile(toolResp) {
				return iconNewFile + fp
			}
			return iconEdit + fp
		}
		return iconTool + toolName

	case toolName == "Edit" || toolName == "StrReplace" || strings.HasSuffix(toolName, "_edit") || toolName == "apply_patch":
		fp := toolFilePath(ti, toolResp)
		result := iconEdit + "edit"
		if fp != "" {
			result = iconEdit + fp
		}
		oldStr := firstNonEmpty(ti["old_string"], ti["oldString"], ti["old_str"])
		newStr := firstNonEmpty(ti["new_string"], ti["newString"], ti["new_str"])
		if oldStr != "" {
			result += "\n- " + strings.TrimSpace(truncateText(oldStr, 80))
		}
		if newStr != "" {
			result += "\n+ " + strings.TrimSpace(truncateText(newStr, 80))
		}
		return result

	case toolName == "Grep" || strings.HasSuffix(toolName, "_grep") || strings.HasSuffix(toolName, "_search"):
		pattern := firstNonEmpty(ti["pattern"], ti["query"])
		fp := firstNonEmpty(ti["file_path"], ti["path"], ti["glob"])
		if pattern != "" && fp != "" {
			return iconGrep + "\"" + truncateText(pattern, 60) + "\" @ " + truncateText(fp, 80)
		}
		if pattern != "" {
			return iconGrep + "\"" + truncateText(pattern, 80) + "\""
		}
		if fp != "" {
			return iconGrep + "@ " + truncateText(fp, 100)
		}
		return iconGrep + toolName

	case toolName == "Glob" || strings.HasSuffix(toolName, "_glob"):
		g := firstNonEmpty(ti["pattern"], ti["glob"])
		if g != "" {
			return iconGrep + "glob \"" + truncateText(g, 80) + "\""
		}
		return iconGrep + toolName

	case toolName == "LS" || toolName == "List" || strings.HasSuffix(toolName, "_ls") || strings.HasSuffix(toolName, "_list_directory"):
		fp := firstNonEmpty(ti["dir_path"], ti["path"], ti["file_path"])
		if fp != "" {
			return iconDir + fp
		}
		return iconDir + toolName

	case toolName == "WebSearch" || strings.HasSuffix(toolName, "_web_search"):
		q := firstNonEmpty(ti["query"], ti["q"])
		if q != "" {
			return iconWeb + "\"" + truncateText(q, 80) + "\""
		}
		return iconWeb + "WebSearch"

	case toolName == "WebFetch" || strings.HasSuffix(toolName, "_web_fetch"):
		if url := ti["url"]; url != "" {
			return iconWeb + truncateText(url, 80)
		}
		return iconWeb + "WebFetch"

	case toolName == "Task" || strings.HasSuffix(toolName, "_task"):
		desc := firstNonEmpty(ti["description"], ti["prompt"], ti["task"])
		if desc != "" {
			return iconTask + "Task: " + truncateText(desc, 100)
		}
		return iconTask + "Task"

	case toolName == "Question" || toolName == "AskUserQuestion":
		q := firstNonEmpty(ti["question"], ti["questions"])
		if q != "" {
			return iconAsk + truncateText(q, 100)
		}
		return iconAsk + "Question"

	case toolName == "TodoWrite" || toolName == "update_plan":
		return iconPlan + "Plan updated"

	case toolName == "Delete" || strings.HasSuffix(toolName, "_delete"):
		fp := firstNonEmpty(ti["target_file"], ti["file_path"], ti["filePath"], ti["path"])
		if fp != "" {
			return iconDelete + fp
		}
		return iconDelete + toolName

	case strings.HasPrefix(toolName, "aipm_"):
		result := iconMCP + toolName
		if q := firstNonEmpty(ti["query"], ti["q"]); q != "" {
			result += " \"" + truncateText(q, 60) + "\""
		}
		return result

	default:
		label := iconTool + toolName
		for _, key := range []string{"query", "pattern", "file_path", "path", "url"} {
			if v := ti[key]; v != "" {
				label += " \"" + truncateText(v, 80) + "\""
				break
			}
		}
		return label
	}
}

func extractCursorBashOutput(toolResp json.RawMessage) string {
	if len(toolResp) == 0 {
		return ""
	}
	var resp struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		Output   string `json:"output"`
		ExitCode int    `json:"exitCode"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		if resp.Stdout != "" {
			return resp.Stdout
		}
		if resp.Stderr != "" {
			return resp.Stderr
		}
		if resp.Output != "" {
			return resp.Output
		}
	}
	var s string
	if json.Unmarshal(toolResp, &s) == nil && s != "" {
		var inner struct {
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			Output   string `json:"output"`
			ExitCode int    `json:"exitCode"`
		}
		if json.Unmarshal([]byte(s), &inner) == nil {
			if inner.Stdout != "" {
				return inner.Stdout
			}
			if inner.Stderr != "" {
				return inner.Stderr
			}
			if inner.Output != "" {
				return inner.Output
			}
		}
	}
	return ""
}

func extractCursorLinesCountFromResp(toolResp json.RawMessage) int {
	if len(toolResp) == 0 {
		return 0
	}
	var resp struct {
		NumLines   int `json:"numLines"`
		LinesCount int `json:"lines_count"`
	}
	if json.Unmarshal(toolResp, &resp) == nil {
		if resp.NumLines > 0 {
			return resp.NumLines
		}
		if resp.LinesCount > 0 {
			return resp.LinesCount
		}
	}
	var s string
	if json.Unmarshal(toolResp, &s) == nil && s != "" {
		return strings.Count(s, "\n") + 1
	}
	return 0
}

func isCursorNewFile(toolResp json.RawMessage) bool {
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
