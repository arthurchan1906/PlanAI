package hook

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestExtractFileOpMetaApplyPatchDirect(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PMAI_HOME", filepath.Join(root, ".pmai"))
	projectRootCache = ""

	in := `{"patch":"*** Begin Patch\n*** Update File: EncryptDrive/Shared/UI/VaultTheme.swift\n@@\n-old\n+new\n*** End Patch"}`
	meta := extractFileOpMeta("apply_patch", json.RawMessage(in), nil)
	var m map[string]any
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", meta, err)
	}
	if m["type"] != "edit" {
		t.Fatalf("type = %v, want edit", m["type"])
	}
	if m["rel_path"] != "EncryptDrive/Shared/UI/VaultTheme.swift" {
		t.Fatalf("rel_path = %v", m["rel_path"])
	}
	if m["file_path"] != "EncryptDrive/Shared/UI/VaultTheme.swift" {
		t.Fatalf("file_path = %v", m["file_path"])
	}
	if m["source"] != "structured" {
		t.Fatalf("source = %v, want structured", m["source"])
	}
}

func TestExtractFileOpMetaBashHeredocPatch(t *testing.T) {
	in := `{"command":"cd /Users/dazsec/projects/EncryptDrive \u0026\u0026 cat \u003e /tmp/p1.patch \u003c\u003c 'PATCH'\n*** Begin Patch\n*** Update File: EncryptDrive/a.go\n@@\n-x\n+y\n*** Update File: EncryptDrive/b.go\n@@\n-z\n+w\n*** End Patch\nPATCH"}`
	meta := extractFileOpMeta("Bash", json.RawMessage(in), nil)
	var m map[string]any
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", meta, err)
	}
	if m["type"] != "edit" {
		t.Fatalf("type = %v, want edit", m["type"])
	}
	// First file is the primary rel_path; all files land in rel_paths.
	if m["rel_path"] != "EncryptDrive/a.go" {
		t.Fatalf("rel_path = %v", m["rel_path"])
	}
	rels, ok := m["rel_paths"].([]any)
	if !ok || len(rels) != 2 || rels[1] != "EncryptDrive/b.go" {
		t.Fatalf("rel_paths = %v", m["rel_paths"])
	}
	// The /tmp target from the redirection must NOT be picked as the file.
	if m["file_path"] == "/tmp/p1.patch" {
		t.Fatalf("heredoc temp target leaked: %v", m["file_path"])
	}
	if m["source"] != "bash_heuristic" {
		t.Fatalf("source = %v, want bash_heuristic", m["source"])
	}
}

func TestExtractFileOpMetaStructuredTools(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PMAI_HOME", filepath.Join(root, ".pmai"))
	projectRootCache = ""

	if meta := extractFileOpMeta("Read", json.RawMessage(`{"file_path":"/abs/outside.go"}`), nil); meta != "" {
		t.Fatalf("project-external Read should not write meta, got %q", meta)
	}

	abs := filepath.Join(root, "EncryptDrive", "x.swift")
	meta := extractFileOpMeta("Write", json.RawMessage(`{"file_path":"`+abs+`"}`), nil)
	var m map[string]any
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", meta, err)
	}
	if m["type"] != "write" || m["rel_path"] != "EncryptDrive/x.swift" {
		t.Fatalf("write meta = %v", m)
	}
	if m["source"] != "structured" {
		t.Fatalf("source = %v, want structured", m["source"])
	}
}

func TestExtractFileOpMetaBashStageAndUnverified(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PMAI_HOME", filepath.Join(root, ".pmai"))
	projectRootCache = ""

	// git add → stage op, bash_heuristic source.
	in := `{"command":"cd /Users/dazsec/projects/EncryptDrive && git add EncryptDrive/a.go EncryptDrive/b.go"}`
	meta := extractFileOpMeta("Bash", json.RawMessage(in), nil)
	var m map[string]any
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", meta, err)
	}
	if m["type"] != "stage" {
		t.Fatalf("type = %v, want stage", m["type"])
	}
	if m["source"] != "bash_heuristic" {
		t.Fatalf("source = %v", m["source"])
	}
	rels, _ := m["rel_paths"].([]any)
	if len(rels) != 2 {
		t.Fatalf("rel_paths = %v", m["rel_paths"])
	}

	// Non-zero exit code downgrades the source.
	meta = extractFileOpMeta("Bash", json.RawMessage(in), json.RawMessage(`{"exitCode":1}`))
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", meta, err)
	}
	if m["source"] != "bash_heuristic_unverified" {
		t.Fatalf("source = %v, want bash_heuristic_unverified", m["source"])
	}
}
