package mcp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
