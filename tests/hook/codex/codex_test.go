package hook_test

import (
	"testing"

	"aipmc/hook"
)

func TestIsValidFileOpPath(t *testing.T) {
	tests := map[string]bool{
		"&1":                           false,
		"&2":                           false,
		"edit.json,":                   false,
		"Write":                        false,
		`D:\code\AI\PlanAI\foo.go`:     true,
		"test-hook.json":               false,
		"/dev/null":                    false,
	}
	for path, want := range tests {
		if got := hook.IsValidFileOpPath(path); got != want {
			t.Fatalf("IsValidFileOpPath(%q)=%v want %v", path, got, want)
		}
	}
}

func TestParseBashFileOpRejectsRedirect(t *testing.T) {
	cmd := `cd D:\code\AI\PlanAI; go build -o dist\aipmc.exe . 2>&1`
	if fop := hook.ParseBashFileOp(cmd); fop != nil {
		t.Fatalf("expected nil for 2>&1 redirect, got %+v", fop)
	}
}
