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
