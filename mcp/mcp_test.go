package mcp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	pmdb "aipmc/db"
)

func TestClassifyMCPErr(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"done-gate reject", "task cannot be marked done without at least one verified approved commit", "business_reject"},
		{"already bound commit", "Commit 已存在且已绑定 task t-1，与传入 c-2 冲突", "idempotent"},
		{"system fault", "更新失败: SQLITE_BUSY", "system_fault"},
		{"empty text", "", "system_fault"},
		{"unrelated text", "some random error", "system_fault"},
	}
	for _, c := range cases {
		if got := classifyMCPErr(c.text); got != c.want {
			t.Errorf("%s: classifyMCPErr(%q) = %q, want %q", c.name, c.text, got, c.want)
		}
	}
}

func TestFormatDecisionText(t *testing.T) {
	cases := []struct {
		name string
		d    map[string]any
		want string
	}{
		{
			name: "scan style keys (decision_text mapped to decision)",
			d: map[string]any{
				"title":      "Agent 行为分析以数据反馈闭环为准绳",
				"status":     "accepted",
				"date":       "2026-08-14",
				"background": "8/14 三方讨论实证",
				"decision":   "每个改动绑定观测指标",
			},
			want: "Decision: Agent 行为分析以数据反馈闭环为准绳\n状态: accepted | 日期: 2026-08-14\n背景: 8/14 三方讨论实证\n决策: 每个改动绑定观测指标",
		},
		{
			name: "missing decision key must not render %!s(<nil>)",
			d: map[string]any{
				"title":      "t",
				"status":     "proposed",
				"date":       "2026-06-24",
				"background": "b",
			},
			want: "Decision: t\n状态: proposed | 日期: 2026-06-24\n背景: b\n决策: ",
		},
		{
			name: "empty map",
			d:    map[string]any{},
			want: "Decision: \n状态:  | 日期: \n背景: \n决策: ",
		},
	}
	for _, c := range cases {
		got := formatDecisionText(c.d)
		if got != c.want {
			t.Errorf("%s: formatDecisionText = %q, want %q", c.name, got, c.want)
		}
		if strings.Contains(got, "%!s") {
			t.Errorf("%s: output contains Go fmt placeholder %q", c.name, got)
		}
	}
}

func TestHasFixKeyword(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"fix: migrate 建表顺序", true},
		{"修复 #19/#20 决策渲染", true},
		{"resolved: StoreGitCommit 空 hash", true},
		{"closed the issue", true},
		{"feat(metrics): E5 显式率指标", false},
		{"docs(audit): 数据审计", false},
		{"chore: gofmt 收敛", false},
	}
	for _, c := range cases {
		if got := hasFixKeyword(c.in); got != c.want {
			t.Errorf("hasFixKeyword(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestTruncArgRuneSafe(t *testing.T) {
	args := map[string]interface{}{"title": "iOS UI 对齐 Android 策略：视觉差异优先、改动最小"}
	got := truncArg(args, "title", 30)
	if !utf8.ValidString(got) {
		t.Errorf("truncArg output invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncArg should append ..., got %q", got)
	}
	// 中文单字 3 字节：回退后长度 ≤ max+3，且不出现替换符
	if len(got) > 33 {
		t.Errorf("truncArg result too long: len=%d %q", len(got), got)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("truncArg output contains replacement char: %q", got)
	}
}

func TestHandleLinePanicIncludesRequestID(t *testing.T) {
	var buf bytes.Buffer
	s := &mcpServer{
		writer: &buf,
		handlers: map[string]mcpToolHandler{
			"panic_tool": func(args map[string]interface{}) mcpToolResult {
				panic("boom")
			},
		},
	}
	s.handleLine(`{"jsonrpc":"2.0","id":"req-123","method":"tools/call","params":{"name":"panic_tool","arguments":{}}}`)

	out := buf.String()
	if !strings.Contains(out, `"id":"req-123"`) {
		t.Errorf("panic response must echo the request id, got: %s", out)
	}
	if !strings.Contains(out, "Internal error") {
		t.Errorf("panic response must be an error, got: %s", out)
	}
}

// Regression: MCP tool calls used to be logged with a hardcoded source
// "claude-code", so codex/gemini/cursor calls were misattributed. The source
// must come from the initialize clientInfo (normalized by mcpClientName).
func TestMCPSourceAttribution(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "data", "pmai.db"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PMAI_HOME", home)
	d, err := pmdb.Open()
	if err != nil {
		t.Fatal(err)
	}
	d.Close()

	var buf bytes.Buffer
	s := &mcpServer{
		writer: &buf,
		handlers: map[string]mcpToolHandler{
			"test_tool": func(args map[string]interface{}) mcpToolResult {
				return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "ok"}}}
			},
		},
	}
	s.handleLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"codex","version":"1.0"}}}`)
	s.handleLine(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"test_tool","arguments":{}}}`)

	d, err = pmdb.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var source string
	if err := d.QueryRow("SELECT source FROM discussion_log WHERE role='assistant' ORDER BY created_at DESC LIMIT 1").Scan(&source); err != nil {
		t.Fatalf("read discussion_log: %v", err)
	}
	if source != "codex-cli" {
		t.Errorf("MCP tool call source = %q, want %q (must not be hardcoded claude-code)", source, "codex-cli")
	}
}

// B3 default window (Claude review 8/17): keyword discussion search must get
// a bounded default so agents don't silently scan full history.
func TestDefaultSearchWindow(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.Local)
	got := defaultSearchWindow(now)
	want := "2026-07-18T12:00:00"
	if got != want {
		t.Errorf("defaultSearchWindow(%v) = %q, want %q", now, got, want)
	}
	if _, err := time.Parse("2006-01-02T15:04:05", got); err != nil {
		t.Errorf("defaultSearchWindow output must be ISO parseable: %v", err)
	}
}

// 8/17 补录：read/search 日志需包含 cursor/last_n/since，供去重方案
// 观测真实重复率（Claude 讨论共识第一步）。
func TestMCPLogSummaryDiscussionFields(t *testing.T) {
	// read: 未传 cursor → cursor=-；last_n 生效值由 handler 写回 args。
	readArgs := map[string]interface{}{"source": "claude-code", "last_n": 15, "since": ""}
	got := mcpLogSummary("aipm_read_discussions", readArgs)
	for _, want := range []string{"src=claude-code", "last_n=15", "cursor=-"} {
		if !strings.Contains(got, want) {
			t.Errorf("read summary %q must contain %q", got, want)
		}
	}
	// read: 传 cursor → 显示截断后的 cursor。
	got2 := mcpLogSummary("aipm_read_discussions", map[string]interface{}{"cursor": "disc-20260817-130000-abcdef"})
	if !strings.Contains(got2, "cursor=disc-2026081...") {
		t.Errorf("cursor must be shown truncated, got %q", got2)
	}
	// search: 默认窗口由 handler 写回 args → since 显示生效值。
	searchArgs := map[string]interface{}{"query": "行为分析", "since": "2026-07-18T12:00:00"}
	got3 := mcpLogSummary("aipm_search_discussions", searchArgs)
	if !strings.Contains(got3, "since=2026-07-18T12:00:00") {
		t.Errorf("search summary must show effective since, got %q", got3)
	}
}
