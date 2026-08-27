package hook

import (
	"encoding/json"
	"strings"
	"testing"
)

// T3b：default 分支 metadata 以 post_tool 格式落地，ParseToolRecord 可分类。
func TestPostToolMetaJSON(t *testing.T) {
	meta := postToolMetaJSON("mcp__aipm__aipm_read_discussions", json.RawMessage(`{"query":"T3b","limit":10}`))
	var m map[string]any
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("invalid JSON: %v — %s", err, meta)
	}
	if m["_type"] != "post_tool" || m["tool_name"] != "mcp__aipm__aipm_read_discussions" {
		t.Fatalf("got %+v", m)
	}
	in, ok := m["tool_input"].(map[string]any)
	if !ok || in["query"] != "T3b" {
		t.Fatalf("tool_input lost: %+v", m)
	}
}

// T3b：超长 tool_input 降级为仅 tool_name，仍为合法 JSON。
func TestPostToolMetaJSONOverlongInput(t *testing.T) {
	bigQuery := `{"query":"` + strings.Repeat("a", 3000) + `"}`
	meta := postToolMetaJSON("WebSearch", json.RawMessage(bigQuery))
	if !json.Valid([]byte(meta)) {
		t.Fatalf("invalid JSON after truncation: %s", meta)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["tool_name"] != "WebSearch" {
		t.Fatalf("tool_name lost: %+v", m)
	}
	if _, has := m["tool_input"]; has {
		t.Fatalf("overlong tool_input must be dropped, got %+v", m)
	}
}

// T3b：非法 tool_input 不污染 metadata（降级为仅 tool_name）。
func TestPostToolMetaJSONInvalidInput(t *testing.T) {
	meta := postToolMetaJSON("kill", json.RawMessage(`{not-json`))
	if !json.Valid([]byte(meta)) {
		t.Fatalf("invalid JSON: %s", meta)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(meta), &m)
	if m["tool_name"] != "kill" {
		t.Fatalf("tool_name lost: %+v", m)
	}
	if _, has := m["tool_input"]; has {
		t.Fatalf("invalid tool_input must be dropped, got %+v", m)
	}
}

// T3b：历史回填——read 行提取 file_path + rel_path。
func TestBackfillMetaForRead(t *testing.T) {
	meta := backfillMetaFor("👁 /Users/dazsec/workspace/aipmc/hook/hook_claude.go (42 lines)")
	var m map[string]string
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("invalid JSON: %v — %s", err, meta)
	}
	if m["type"] != "read" || m["file_path"] != "/Users/dazsec/workspace/aipmc/hook/hook_claude.go" {
		t.Fatalf("got %+v", m)
	}
	if m["rel_path"] != "hook/hook_claude.go" {
		t.Fatalf("rel_path = %q, want hook/hook_claude.go", m["rel_path"])
	}
	if m["source"] != "backfill" {
		t.Fatalf("source = %q, want backfill", m["source"])
	}
}

// T3b：历史回填——default 行提取 tool_name（post_tool 格式）。
func TestBackfillMetaForTool(t *testing.T) {
	meta := backfillMetaFor("🛠 mcp__aipm__aipm_read_discussions")
	var m map[string]string
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("invalid JSON: %v — %s", err, meta)
	}
	if m["_type"] != "post_tool" || m["tool_name"] != "mcp__aipm__aipm_read_discussions" {
		t.Fatalf("got %+v", m)
	}
}

// T3b：无法解析的行跳过（返回空串）。
func TestBackfillMetaForUnknown(t *testing.T) {
	if meta := backfillMetaFor("📡 aipm_create_task"); meta != "" {
		t.Fatalf("unparseable line should be skipped, got %s", meta)
	}
}
