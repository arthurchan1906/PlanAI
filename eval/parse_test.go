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
	// codex mcp 工具名含 "read" 不得误捕为 read（mcp 最优先）
	if r := ParseToolRecord("codex-cli", `{"_type":"post_tool","tool_name":"mcp__aipm__aipm_read_discussions","tool_input":{}}`); r.Tool != "mcp" {
		t.Errorf("mcp tool = %q, want mcp", r.Tool)
	}
	// 无已知关键字 → 保留原名
	if r := ParseToolRecord("codex-cli", `{"_type":"post_tool","tool_name":"update_plan","tool_input":{}}`); r.Tool != "update_plan" {
		t.Errorf("update_plan tool = %q, want 保留原名", r.Tool)
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
	}
	for _, s := range samples {
		if r := ParseToolRecord(s.source, s.md); r.Tool == "unknown" {
			t.Errorf("%s parse failed: %q", s.source, s.md)
		}
	}
}
