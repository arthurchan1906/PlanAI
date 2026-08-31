package hook

import (
	"encoding/json"
	"testing"
)

func TestToolResponseObjectShape(t *testing.T) {
	var tr toolResponse
	if err := json.Unmarshal([]byte(`{"originalFile":"","filePath":"a.go","stdout":"ok","exitCode":0}`), &tr); err != nil {
		t.Fatalf("object shape: %v", err)
	}
	if tr.FilePath != "a.go" || tr.Stdout != "ok" {
		t.Fatalf("got %+v", tr)
	}
}

func TestToolResponseArrayShape(t *testing.T) {
	// Regression: Claude PostToolUse emits an array for multi-result tools;
	// previously json.Unmarshal failed and the hook event was dropped.
	var tr toolResponse
	in := `[{"filePath":"a.go","stdout":"first"},{"filePath":"b.go","stdout":"second"}]`
	if err := json.Unmarshal([]byte(in), &tr); err != nil {
		t.Fatalf("array shape: %v", err)
	}
	if tr.FilePath != "a.go" || tr.Stdout != "first" {
		t.Fatalf("array shape should keep first element, got %+v", tr)
	}
}

func TestToolResponseEmptyArray(t *testing.T) {
	var tr toolResponse
	if err := json.Unmarshal([]byte(`[]`), &tr); err != nil {
		t.Fatalf("empty array: %v", err)
	}
}

func TestToolResponseArrayKeepsAllFiles(t *testing.T) {
	// M1 (8/13): multi-file Write response must not drop files beyond arr[0].
	in := `[
		{"filePath":"/p/a.swift","originalFile":"","success":true},
		{"filePath":"/p/b.swift","originalFile":"/p/b.swift","success":true}
	]`
	var tr toolResponse
	if err := json.Unmarshal([]byte(in), &tr); err != nil {
		t.Fatalf("multi-file array: %v", err)
	}
	if len(tr.MultiResults) != 1 {
		t.Fatalf("MultiResults = %d, want 1 (arr[1:] — primary excluded)", len(tr.MultiResults))
	}
	if tr.FilePath != "/p/a.swift" {
		t.Fatalf("primary FilePath = %q", tr.FilePath)
	}
	if tr.MultiResults[0].FilePath != "/p/b.swift" {
		t.Fatalf("second file lost: %+v", tr.MultiResults[0])
	}
}

func TestCollectWriteFilesMergesInputAndResponse(t *testing.T) {
	var tr toolResponse
	_ = json.Unmarshal([]byte(`[
		{"filePath":"/p/a.swift","originalFile":"","success":true},
		{"filePath":"/p/b.swift","originalFile":"/p/b.swift","success":true}
	]`), &tr)

	// tool_input.file_path + response files merged, deduplicated.
	files := collectWriteFiles("/p/a.swift", tr)
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2 (a+b deduped): %+v", len(files), files)
	}
	if files[0].OriginalFile != "" || files[1].OriginalFile == "" {
		t.Fatalf("per-element new/overwrite must be preserved: %+v", files)
	}

	// response-only path (tool_input empty) no longer lost.
	files = collectWriteFiles("", tr)
	if len(files) != 2 {
		t.Fatalf("response-only files = %d, want 2", len(files))
	}
}

func TestToolInputRawSurvivesParse(t *testing.T) {
	// 修复（8/31）：原实现 ToolInputRaw 与内联 ToolInput struct 共用
	// json:"tool_input" tag，Go encoding/json 会忽略该键使两者皆空（T3b
	// 空 metadata 修复与结构化 desc 实际未生效）。现 ToolInputRaw 独占 tag。
	in := `{"tool_name":"Edit","tool_input":{"file_path":"/p/a.go","old_string":"a","new_string":"b"},"tool_response":{"success":true}}`
	var raw struct {
		ToolName     string          `json:"tool_name"`
		ToolInputRaw json.RawMessage `json:"tool_input"`
		ToolResponse toolResponse    `json:"tool_response"`
	}
	if err := json.Unmarshal([]byte(in), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(raw.ToolInputRaw) == 0 {
		t.Fatal("ToolInputRaw empty — tool_input was dropped by duplicate-tag conflict")
	}
	// 结构化字段从 ToolInputRaw 二次解析，不再依赖丢弃的结构体字段。
	ti := parseClaudeToolInput(raw.ToolInputRaw)
	if ti.FilePath != "/p/a.go" || ti.OldString != "a" || ti.NewString != "b" {
		t.Fatalf("structured parse got %+v", ti)
	}
}

func TestParseClaudeToolInput(t *testing.T) {
	// Command（Grep/Bash）解析。
	ti := parseClaudeToolInput(json.RawMessage(`{"command":"grep x","file_path":"/f"}`))
	if ti.Command != "grep x" || ti.FilePath != "/f" {
		t.Fatalf("got %+v", ti)
	}
	// 空/非法 JSON 应返回零值（调用方以 ti.X == "" 兜底）。
	if zero := parseClaudeToolInput(nil); zero.Command != "" || zero.FilePath != "" {
		t.Fatalf("nil raw should yield zero value, got %+v", zero)
	}
	if bad := parseClaudeToolInput(json.RawMessage(`not json`)); bad.Command != "" {
		t.Fatalf("invalid raw should yield zero value, got %+v", bad)
	}
}
