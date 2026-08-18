package eval

// EVAL_PIPELINE §3.1 阶段 0：discussion_log.metadata 四格式分型解析。
// 8/18 实测（真实库 20k+ 行）修正规格：codex-cli 同样产出 _type=post_tool
// （新通用工具调用格式，非仅 claude）；cursor 记录主要是 assistant_message
// （LLM 响应，无工具字段）；opencode 是 _raw 嵌套结构（规格未覆盖，补充）。
// 归一化 ToolRecord 供阶段 1-3/5 使用：纯代码、确定性、零 LLM 依赖。

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// ToolRecord 归一化工具调用/消息记录。
type ToolRecord struct {
	Source        string   `json:"source"` // 原始来源（claude-code/codex-cli/...）
	Tool          string   `json:"tool"`   // bash/edit/read/write/llm_message/mcp/unknown
	Command       string   `json:"command"` // bash command 或工具输入摘要
	Files         []string `json:"files"`   // 文件路径（tool_input + 顶层 file_path/rel_path，去重）
	ExitCode      *int     `json:"exit_code,omitempty"`
	Output        string   `json:"output,omitempty"` // stdout/工具响应（截断）
	Model         string   `json:"model,omitempty"`
	Cwd           string   `json:"cwd,omitempty"`
	HookEventName string   `json:"hook_event_name,omitempty"` // 原始事件名（PostToolUse/postToolUse 大小写不同，阶段 5 判定写操作）
	Quality       string   `json:"quality"`                    // ok / degraded（乱码容忍，不丢弃降权）
}

// postTool 新通用工具调用格式（codex-cli/cursor 实测；tool_input 为工具入参，
// file_path/rel_path 为工具关联文件，位于顶层而非 tool_input 内）。
type postTool struct {
	Type          string         `json:"_type"`
	Cwd           string         `json:"cwd"`
	HookEventName string         `json:"hook_event_name"`
	Model         string         `json:"model"`
	FilePath      string         `json:"file_path"`
	RelPath       string         `json:"rel_path"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
}

// legacyBash claude-code 旧通用格式。
type legacyBash struct {
	Type     string `json:"type"`
	Command  string `json:"command"`
	ExitCode *int   `json:"exit_code"`
	Stdout   string `json:"stdout"`
}

// geminiTool gemini-cli 工具调用（hook_event_name=AfterTool）。
type geminiTool struct {
	Type          string         `json:"_type"`
	Cwd           string         `json:"cwd"`
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
}

// cursorMsg cursor 记录（assistant_message/after_agent_thought：LLM 响应，无工具字段）。
type cursorMsg struct {
	Type          string `json:"_type"`
	Conversation  string `json:"conversation_id"`
	HookEventName string `json:"hook_event_name"`
	Model         string `json:"model"`
}

// opencodeRaw opencode 嵌套结构（规格未覆盖，8/18 实测补充）。
type opencodeRaw struct {
	Raw struct {
		Properties struct {
			Info struct {
				Agent   string `json:"agent"`
				ModelID string `json:"modelID"`
				Role    string `json:"role"`
				Path    struct {
					Cwd string `json:"cwd"`
				} `json:"path"`
			} `json:"info"`
		} `json:"properties"`
	} `json:"_raw"`
}

// ParseToolRecord 按 metadata 结构分型解析为归一化 ToolRecord。
// 无法识别的结构返回 Tool=unknown、Quality=ok（不丢弃，供阶段 5 降权处理）。
func ParseToolRecord(source, metadata string) ToolRecord {
	rec := ToolRecord{Source: source, Quality: "ok"}
	// 乱码判定必须发生在 JSON 解析前：encoding/json 会把非法 UTF-8 字节
	// 替换为 U+FFFD，解析后无法再识别。原始 metadata 含非法字节即降权。
	if !utf8.ValidString(metadata) {
		rec.Quality = "degraded"
	}
	rec = parsePostTool(source, metadata, rec)
	if rec.Tool != "" {
		return finishParse(rec)
	}
	rec = parseLegacyBash(source, metadata, rec)
	if rec.Tool != "" {
		return finishParse(rec)
	}
	rec = parseGeminiTool(source, metadata, rec)
	if rec.Tool != "" {
		return finishParse(rec)
	}
	rec = parseOpencode(source, metadata, rec)
	if rec.Tool != "" {
		return finishParse(rec)
	}
	rec = parseCursor(source, metadata, rec)
	if rec.Tool != "" {
		return finishParse(rec)
	}
	rec.Tool = "unknown"
	return finishParse(rec)
}

func finishParse(rec ToolRecord) ToolRecord {
	if rec.Output != "" && len(rec.Output) > 2000 {
		rec.Output = rec.Output[:2000] + "…"
	}
	if !utf8.ValidString(rec.Output) {
		rec.Quality = "degraded"
		rec.Output = strings.ToValidUTF8(rec.Output, "?")
	}
	return rec
}

// parsePostTool 新通用工具调用格式：_type=post_tool（codex-cli / 新 claude）。
func parsePostTool(source, metadata string, rec ToolRecord) ToolRecord {
	var pt postTool
	if err := json.Unmarshal([]byte(metadata), &pt); err != nil || pt.Type != "post_tool" {
		return rec
	}
	rec.Cwd = pt.Cwd
	rec.Model = pt.Model
	rec.HookEventName = pt.HookEventName
	tool, cmd, files := classifyToolInput(pt.ToolName, pt.ToolInput)
	if tool == "unknown" && pt.FilePath != "" {
		// 顶层 file_path 且无命令 → 文件操作（与 tool_input.file_path 归类一致）
		tool = "edit"
	}
	rec.Tool = tool
	rec.Command = cmd
	rec.Files = appendFiles(files, pt.FilePath, pt.RelPath)
	return rec
}

// parseLegacyBash claude-code 旧格式：type=bash + command/exit_code/stdout。
func parseLegacyBash(source, metadata string, rec ToolRecord) ToolRecord {
	var lb legacyBash
	if err := json.Unmarshal([]byte(metadata), &lb); err != nil || lb.Type != "bash" {
		return rec
	}
	rec.Tool = "bash"
	rec.Command = lb.Command
	rec.ExitCode = lb.ExitCode
	rec.Output = lb.Stdout
	return rec
}

// parseGeminiTool gemini-cli：hook_event_name=AfterTool + tool_name/tool_input。
func parseGeminiTool(source, metadata string, rec ToolRecord) ToolRecord {
	var gt geminiTool
	if err := json.Unmarshal([]byte(metadata), &gt); err != nil || gt.HookEventName != "AfterTool" {
		return rec
	}
	rec.Cwd = gt.Cwd
	rec.HookEventName = gt.HookEventName
	rec.Tool = normalizeToolName(gt.ToolName)
	rec.Command = gt.ToolName
	rec.Files = extractFilesFromInput(gt.ToolInput)
	return rec
}

// parseOpencode opencode：_raw.properties.info（agent/modelID/path.cwd/role）。
func parseOpencode(source, metadata string, rec ToolRecord) ToolRecord {
	var oc opencodeRaw
	if err := json.Unmarshal([]byte(metadata), &oc); err != nil || oc.Raw.Properties.Info.Agent == "" {
		return rec
	}
	rec.Cwd = oc.Raw.Properties.Info.Path.Cwd
	rec.Model = oc.Raw.Properties.Info.ModelID
	// opencode 事件是 LLM 消息流（role=assistant/user），非工具调用
	rec.Tool = "llm_message"
	rec.Command = "role=" + oc.Raw.Properties.Info.Role
	return rec
}

// parseCursor cursor：conversation_id（assistant_message/after_agent_thought，
// LLM 响应记录，无工具字段）。设计文档判别键 conversation_id 保留。
func parseCursor(source, metadata string, rec ToolRecord) ToolRecord {
	var cm cursorMsg
	if err := json.Unmarshal([]byte(metadata), &cm); err != nil || cm.Conversation == "" {
		return rec
	}
	rec.Model = cm.Model
	rec.HookEventName = cm.HookEventName
	rec.Tool = "llm_message"
	rec.Command = cm.Type
	return rec
}

// appendFiles 追加文件路径并去重（顶层 file_path/rel_path 与 tool_input 内可能重复）。
func appendFiles(files []string, paths ...string) []string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		dup := false
		for _, f := range files {
			if f == p {
				dup = true
				break
			}
		}
		if !dup {
			files = append(files, p)
		}
	}
	return files
}

// classifyToolInput 判定工具类型并提取命令/文件。
// 优先级：tool_name（实测 100% 行存在，权威标识）→ command（bash）→ file_path（无 tool_name 时兜底）。
func classifyToolInput(toolName string, in map[string]any) (tool, cmd string, files []string) {
	if toolName != "" {
		tool = normalizeToolName(toolName)
	} else if in == nil {
		return "unknown", "", nil
	} else if c, ok := in["command"].(string); ok && c != "" {
		tool = "bash"
	} else if _, ok := in["file_path"].(string); ok {
		tool = "edit"
	} else if t, ok := in["tool_name"].(string); ok {
		tool = normalizeToolName(t)
	} else {
		return "unknown", "", nil
	}
	if c, ok := in["command"].(string); ok {
		cmd = c
	}
	files = extractFilesFromInput(in)
	return tool, cmd, files
}

// extractFilesFromInput 从 tool_input 提取文件路径字段（file_path/file_paths/多键）。
func extractFilesFromInput(in map[string]any) []string {
	var files []string
	add := func(v string) {
		if v != "" {
			files = append(files, v)
		}
	}
	if fp, ok := in["file_path"].(string); ok {
		add(fp)
	}
	if f, ok := in["file_paths"].([]any); ok {
		for _, v := range f {
			if s, ok := v.(string); ok {
				add(s)
			}
		}
	}
	if f, ok := in["files"].([]any); ok {
		for _, v := range f {
			if s, ok := v.(string); ok {
				add(s)
			}
		}
	}
	return files
}

// normalizeToolName 工具名 → 归一类（read_file→read 等），大小写不敏感。
// mcp 必须最先判定：mcp__aipm__aipm_read_discussions 含 "read" 不得误捕。
func normalizeToolName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "mcp"):
		return "mcp"
	case strings.Contains(lower, "read") || strings.Contains(lower, "grep"):
		return "read"
	case strings.Contains(lower, "write"):
		return "write"
	case strings.Contains(lower, "edit") || strings.Contains(lower, "delete"):
		return "edit"
	case strings.Contains(lower, "bash") || strings.Contains(lower, "shell") || strings.Contains(lower, "run"):
		return "bash"
	default:
		return name
	}
}
