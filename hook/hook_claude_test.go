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
	if len(tr.MultiResults) != 2 {
		t.Fatalf("MultiResults = %d, want 2", len(tr.MultiResults))
	}
	if tr.FilePath != "/p/a.swift" {
		t.Fatalf("primary FilePath = %q", tr.FilePath)
	}
	if tr.MultiResults[1].FilePath != "/p/b.swift" {
		t.Fatalf("second file lost: %+v", tr.MultiResults[1])
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
