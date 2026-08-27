package eval

// EVAL_PIPELINE §4.3：parse 分型 fixture——四格式（实测修正）+ opencode _raw
// + unknown + 乱码容忍。纯代码无 LLM 依赖。

import (
	"strings"
	"testing"
)

func TestParsePostToolCodex(t *testing.T) {
	md := `{"_type":"post_tool","cwd":"/Users/dazsec/workspace/aipmc","hook_event_name":"PostToolUse","model":"deepseek-v4-flash","tool_input":{"command":"go test ./..."}}`
	r := ParseToolRecord("codex-cli", md)
	if r.Tool != "bash" {
		t.Errorf("tool = %q, want bash", r.Tool)
	}
	if r.Command != "go test ./..." {
		t.Errorf("command = %q", r.Command)
	}
	if r.Cwd != "/Users/dazsec/workspace/aipmc" || r.Model != "deepseek-v4-flash" {
		t.Errorf("cwd/model = %q/%q", r.Cwd, r.Model)
	}
	if r.Quality != "ok" {
		t.Errorf("quality = %q, want ok", r.Quality)
	}
}

func TestParsePostToolEditFile(t *testing.T) {
	md := `{"_type":"post_tool","cwd":"/tmp","tool_input":{"file_path":"src/app.go"}}`
	r := ParseToolRecord("codex-cli", md)
	if r.Tool != "edit" {
		t.Errorf("tool = %q, want edit", r.Tool)
	}
	if len(r.Files) != 1 || r.Files[0] != "src/app.go" {
		t.Errorf("files = %v, want [src/app.go]", r.Files)
	}
}

func TestParseLegacyBashClaude(t *testing.T) {
	md := `{"type":"bash","command":"grep -n foo x.go","exit_code":0,"stdout":"12:foo"}`
	r := ParseToolRecord("claude-code", md)
	if r.Tool != "bash" || r.Command != "grep -n foo x.go" {
		t.Errorf("tool/command = %q/%q", r.Tool, r.Command)
	}
	if r.ExitCode == nil || *r.ExitCode != 0 {
		t.Errorf("exit_code = %v, want 0", r.ExitCode)
	}
	if r.Output != "12:foo" {
		t.Errorf("output = %q", r.Output)
	}
}

func TestParseGeminiAfterTool(t *testing.T) {
	md := `{"_type":"after_tool","cwd":"/p","hook_event_name":"AfterTool","tool_name":"read_file","tool_input":{"file_path":"session/summary.go","start_line":1}}`
	r := ParseToolRecord("gemini-cli", md)
	if r.Tool != "read" {
		t.Errorf("tool = %q, want read", r.Tool)
	}
	if len(r.Files) != 1 || r.Files[0] != "session/summary.go" {
		t.Errorf("files = %v", r.Files)
	}
}

func TestParseCursorAssistantMessage(t *testing.T) {
	md := `{"_type":"assistant_message","conversation_id":"abc-123","generation_id":"g1","model":"default","input_tokens":100}`
	r := ParseToolRecord("cursor", md)
	if r.Tool != "llm_message" {
		t.Errorf("tool = %q, want llm_message（cursor 为 LLM 响应记录）", r.Tool)
	}
	if r.Model != "default" {
		t.Errorf("model = %q", r.Model)
	}
}

func TestParseOpencodeRaw(t *testing.T) {
	md := `{"_raw":{"id":"e1","properties":{"info":{"agent":"build","modelID":"deepseek-v4-flash","role":"assistant","path":{"cwd":"/repo"}}}}}`
	r := ParseToolRecord("opencode", md)
	if r.Tool != "llm_message" {
		t.Errorf("tool = %q, want llm_message", r.Tool)
	}
	if r.Model != "deepseek-v4-flash" || r.Cwd != "/repo" {
		t.Errorf("model/cwd = %q/%q", r.Model, r.Cwd)
	}
}

func TestParsePostToolTopLevelFile(t *testing.T) {
	// codex-cli 实测：file_path/rel_path 在顶层，tool_input 只有 command
	md := `{"_type":"post_tool","cwd":"/repo","file_path":"hook/hook_claude.go","hook_event_name":"PostToolUse","model":"m1","rel_path":"hook/hook_claude.go","tool_input":{"command":"rg -n foo hook/hook_claude.go"}}`
	r := ParseToolRecord("codex-cli", md)
	if r.Tool != "bash" || r.HookEventName != "PostToolUse" {
		t.Errorf("tool/event = %q/%q, want bash/PostToolUse", r.Tool, r.HookEventName)
	}
	if len(r.Files) != 1 || r.Files[0] != "hook/hook_claude.go" {
		t.Errorf("files = %v, want [hook/hook_claude.go]", r.Files)
	}
}

func TestParsePostToolCursorEventCase(t *testing.T) {
	// cursor 实测：事件名小写 postToolUse + 顶层 file_path 与 tool_input 内重复（需去重）
	md := `{"_type":"post_tool","conversation_id":"c1","file_path":"/repo/a.go","hook_event_name":"postToolUse","tool_input":{"file_path":"/repo/a.go"}}`
	r := ParseToolRecord("cursor", md)
	if r.HookEventName != "postToolUse" {
		t.Errorf("event = %q, want postToolUse", r.HookEventName)
	}
	if len(r.Files) != 1 || r.Files[0] != "/repo/a.go" {
		t.Errorf("files = %v, want 去重后 [/repo/a.go]", r.Files)
	}
	// 顶层 file_path 且 tool_input 为空 → 文件操作归类 edit
	md2 := `{"_type":"post_tool","file_path":"/repo/b.go","hook_event_name":"PostToolUse","tool_input":{}}`
	r2 := ParseToolRecord("codex-cli", md2)
	if r2.Tool != "edit" || len(r2.Files) != 1 || r2.Files[0] != "/repo/b.go" {
		t.Errorf("tool/files = %q/%v, want edit/[/repo/b.go]", r2.Tool, r2.Files)
	}
}

func TestClassifyToolNamePriority(t *testing.T) {
	// cursor Read + file_path → read（tool_name 优先，不得误判 edit）
	r := ParseToolRecord("cursor", `{"_type":"post_tool","file_path":"/repo/SKILL.md","hook_event_name":"postToolUse","tool_name":"Read","tool_input":{"file_path":"/repo/SKILL.md"}}`)
	if r.Tool != "read" {
		t.Errorf("Read tool = %q, want read", r.Tool)
	}
	if len(r.Files) != 1 || r.Files[0] != "/repo/SKILL.md" {
		t.Errorf("Read files = %v, want [/repo/SKILL.md]", r.Files)
	}
	// cursor Grep → read
	if r := ParseToolRecord("cursor", `{"_type":"post_tool","tool_name":"Grep","tool_input":{}}`); r.Tool != "read" {
		t.Errorf("Grep tool = %q, want read", r.Tool)
	}
	// cursor Shell → bash
	if r := ParseToolRecord("cursor", `{"_type":"post_tool","tool_name":"Shell","tool_input":{"command":"ls"}}`); r.Tool != "bash" || r.Command != "ls" {
		t.Errorf("Shell tool/cmd = %q/%q, want bash/ls", r.Tool, r.Command)
	}
	// cursor Delete → edit
	if r := ParseToolRecord("cursor", `{"_type":"post_tool","tool_name":"Delete","tool_input":{}}`); r.Tool != "edit" {
		t.Errorf("Delete tool = %q, want edit", r.Tool)
	}
	// codex mcp 工具名含 "read" 不得误捕为 read（mcp 最优先，G1：aipm 子类细分）
	if r := ParseToolRecord("codex-cli", `{"_type":"post_tool","tool_name":"mcp__aipm__aipm_read_discussions","tool_input":{}}`); r.Tool != "mcp_aipm_read" {
		t.Errorf("aipm read tool = %q, want mcp_aipm_read", r.Tool)
	}
	// G1：aipm 工具子类细分（search/trace = 历史检索；get/list = 状态读取；read = 例行）
	cases := []struct{ tool, want string }{
		{"mcp__aipm__aipm_search_context", "mcp_aipm_search"},
		{"mcp__aipm__aipm_smart_search", "mcp_aipm_search"},
		{"mcp__aipm__aipm_search_discussions", "mcp_aipm_search"},
		{"mcp__aipm__aipm_trace_context", "mcp_aipm_trace"},
		{"mcp__aipm__aipm_get_task", "mcp_aipm_get"},
		{"mcp__aipm__aipm_get_commit", "mcp_aipm_get"},
		{"mcp__aipm__aipm_list_commits", "mcp_aipm_list"},
		{"mcp__aipm__aipm_list_tasks", "mcp_aipm_list"},
		{"mcp__aipm__aipm_read_discussions", "mcp_aipm_read"},
		{"mcp__aipm__aipm_record_commit", "mcp_aipm_other"},
		{"mcp__other__some_tool", "mcp"},
	}
	for _, c := range cases {
		meta := `{"_type":"post_tool","tool_name":"` + c.tool + `","tool_input":{}}`
		if r := ParseToolRecord("codex-cli", meta); r.Tool != c.want {
			t.Errorf("tool %q = %q, want %q", c.tool, r.Tool, c.want)
		}
	}
	// 无已知关键字 → 保留原名
	if r := ParseToolRecord("codex-cli", `{"_type":"post_tool","tool_name":"update_plan","tool_input":{}}`); r.Tool != "update_plan" {
		t.Errorf("update_plan tool = %q, want 保留原名", r.Tool)
	}
}

func TestParseClaudeFileOp(t *testing.T) {
	// claude-code 实测：type=edit/new_file + 顶层 file_path（8/18 真实库 41 行）
	r := ParseToolRecord("claude-code", `{"type":"edit","file_path":"/repo/a.go","hunks":[]}`)
	if r.Tool != "edit" || len(r.Files) != 1 || r.Files[0] != "/repo/a.go" {
		t.Errorf("edit = %q/%v, want edit/[/repo/a.go]", r.Tool, r.Files)
	}
	if r := ParseToolRecord("claude-code", `{"type":"new_file","file_path":"/repo/b.go"}`); r.Tool != "edit" {
		t.Errorf("new_file = %q, want edit", r.Tool)
	}
	if r := ParseToolRecord("claude-code", `{"type":"read","file_path":"/repo/c.go"}`); r.Tool != "read" {
		t.Errorf("read = %q, want read", r.Tool)
	}
	// 其他 type（如 bash）不归文件操作分型
	if r := ParseToolRecord("claude-code", `{"type":"bash","command":"ls"}`); r.Tool != "bash" {
		t.Errorf("bash = %q, want bash", r.Tool)
	}
}

func TestParseUnknownAndDegraded(t *testing.T) {
	if r := ParseToolRecord("aipmc-vision", `{"id":"x","iteration":1}`); r.Tool != "unknown" {
		t.Errorf("unknown tool = %q, want unknown", r.Tool)
	}
	// 乱码容忍：非法 UTF-8 输出标记 degraded，不丢弃。
	// 注意：不能用 `\xff` JSON 转义（json 会拒绝该转义），需注入原始字节。
	md := `{"type":"bash","command":"c","exit_code":1,"stdout":"` + string([]byte{0xff, 0xfe}) + ` bad"}`
	r := ParseToolRecord("claude-code", md)
	if r.Quality != "degraded" {
		t.Errorf("quality = %q, want degraded", r.Quality)
	}
	if !strings.Contains(r.Output, "bad") {
		t.Errorf("degraded output 应保留可读部分: %q", r.Output)
	}
}

// 冒烟：真实库全量 metadata 的分型覆盖率由 eval smoke 统计（非 CI），此处仅保证
// 已知格式不回归。
func TestParseAllKnownFormats(t *testing.T) {
	samples := []struct{ source, md string }{
		{"codex-cli", `{"_type":"post_tool","tool_input":{"command":"ls"}}`},
		{"claude-code", `{"type":"bash","command":"ls","exit_code":0,"stdout":""}`},
		{"gemini-cli", `{"_type":"after_tool","hook_event_name":"AfterTool","tool_name":"run_bash","tool_input":{"command":"ls"}}`},
		{"cursor", `{"_type":"after_agent_thought","conversation_id":"c1"}`},
		{"opencode", `{"_raw":{"properties":{"info":{"agent":"build","path":{"cwd":"/"}}}}}`},
		{"claude-code", `{"type":"mcp_tool","tool":"aipm_smart_search","reflection":"未找到匹配结果"}`},
	}
	for _, s := range samples {
		if r := ParseToolRecord(s.source, s.md); r.Tool == "unknown" {
			t.Errorf("%s parse failed: %q", s.source, s.md)
		}
	}
}

// TestParseMcpTool 真实 mcp_tool 格式分型（Claude 审核 8/24：测试数据格式与真实验收
// 样本脱节——c0ad2534 38 条 mcp 记录全部是 {"type":"mcp_tool","tool":...}，此前全落 unknown）。
func TestParseMcpTool(t *testing.T) {
	cases := []struct{ tool, want string }{
		{"aipm_smart_search", "mcp_aipm_search"},
		{"aipm_search_context", "mcp_aipm_search"},
		{"aipm_search_discussions", "mcp_aipm_search"},
		{"aipm_trace_context", "mcp_aipm_trace"},
		{"aipm_get_task", "mcp_aipm_get"},
		{"aipm_list_tasks", "mcp_aipm_list"},
		{"aipm_read_discussions", "mcp_aipm_read"},
		{"aipm_record_bug", "mcp_aipm_other"},
	}
	for _, c := range cases {
		md := `{"type":"mcp_tool","tool":"` + c.tool + `","reflection":"r"}`
		r := ParseToolRecord("claude-code", md)
		if r.Tool != c.want {
			t.Errorf("tool %q = %q, want %q", c.tool, r.Tool, c.want)
		}
		if r.Command != c.tool {
			t.Errorf("command = %q, want %q", r.Command, c.tool)
		}
	}
	// reflection 进 Output，供阶段 5 降权/提示词消费
	if r := ParseToolRecord("claude-code", `{"type":"mcp_tool","tool":"aipm_smart_search","reflection":"无结果"}`); r.Output != "无结果" {
		t.Errorf("output = %q, want 无结果", r.Output)
	}
	// 非 mcp_tool type 不误捕
	if r := ParseToolRecord("claude-code", `{"type":"post_tool","tool":"aipm_smart_search"}`); r.Tool != "unknown" {
		t.Errorf("post_tool + tool 字段 = %q, want unknown（无 tool_name 的 post_tool 兜底）", r.Tool)
	}
}

// T3b：hook default 分支修复后，mcp__aipm__* 工具行以 post_tool 格式落地，
// 必须从 unknown 归入 mcp_aipm_*（此前空 metadata → 33% S2-claude 盲区）。
func TestParsePostToolMcpAipm(t *testing.T) {
	md := `{"_type":"post_tool","tool_name":"mcp__aipm__aipm_read_discussions","tool_input":{"query":"T3b","limit":10}}`
	r := ParseToolRecord("claude-code", md)
	if r.Tool != "mcp_aipm_read" {
		t.Errorf("tool = %q, want mcp_aipm_read（read_discussions 为例行读取）", r.Tool)
	}
	md2 := `{"_type":"post_tool","tool_name":"mcp__aipm__aipm_search_context","tool_input":{"query":"T3b"}}`
	r2 := ParseToolRecord("claude-code", md2)
	if r2.Tool != "mcp_aipm_search" {
		t.Errorf("tool = %q, want mcp_aipm_search（历史检索信号）", r2.Tool)
	}
	// 无 tool_input 时（历史回填降级形态）仍可按 tool_name 分类。
	md3 := `{"_type":"post_tool","tool_name":"mcp__aipm__aipm_get_task"}`
	r3 := ParseToolRecord("claude-code", md3)
	if r3.Tool != "mcp_aipm_get" {
		t.Errorf("tool = %q, want mcp_aipm_get", r3.Tool)
	}
	// WebSearch / Agent / kill 等 default 工具：保持原 tool 名（有信号即非盲区）。
	md4 := `{"_type":"post_tool","tool_name":"WebSearch"}`
	r4 := ParseToolRecord("claude-code", md4)
	if r4.Tool != "WebSearch" {
		t.Errorf("tool = %q, want WebSearch", r4.Tool)
	}
}

// T3b：回填 read 行 metadata 可被 parseClaudeFileOp 归入 read（含 file_path）。
func TestParseBackfilledRead(t *testing.T) {
	md := `{"type":"read","file_path":"/Users/dazsec/workspace/aipmc/hook/hook_claude.go","rel_path":"hook/hook_claude.go","source":"backfill"}`
	r := ParseToolRecord("claude-code", md)
	if r.Tool != "read" {
		t.Errorf("tool = %q, want read", r.Tool)
	}
	if len(r.Files) != 1 || r.Files[0] != "/Users/dazsec/workspace/aipmc/hook/hook_claude.go" {
		t.Errorf("files = %v", r.Files)
	}
}
