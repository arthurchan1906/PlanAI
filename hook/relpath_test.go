package hook

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestToRelPath(t *testing.T) {
	root := t.TempDir()
	// Ensure projectRoot resolves under a controlled PMAI_HOME (package cache).
	t.Setenv("PMAI_HOME", filepath.Join(root, ".pmai"))
	projectRootCache = ""

	cases := []struct {
		in   string
		want string
	}{
		{filepath.Join(root, "EncryptDrive", "a.go"), "EncryptDrive/a.go"},
		{filepath.Join(root, "a.go"), "a.go"},
		{filepath.Join(root, "..", "outside.go"), ""},
		{"/Users/other/project/x.go", ""},
		{"EncryptDrive/a.go", "EncryptDrive/a.go"},
		{"./EncryptDrive/a.go", "EncryptDrive/a.go"},
		{"../outside.go", "outside.go"},
		{"", ""},
		{"  ", ""},
	}
	for _, c := range cases {
		if got := ToRelPath(c.in); got != c.want {
			t.Errorf("ToRelPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractPatchFiles(t *testing.T) {
	in := `*** Begin Patch
*** Update File: EncryptDrive/Shared/UI/VaultTheme.swift
@@
    // Level 0 — bottom nav bar background
-old
+new
*** Update File: EncryptDrive/Shared/UI/FilesDecryptTab.swift
@@
-x
+y
*** End Patch`
	got := ExtractPatchFiles(in)
	want := []string{"EncryptDrive/Shared/UI/VaultTheme.swift", "EncryptDrive/Shared/UI/FilesDecryptTab.swift"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractPatchFiles = %v, want %v", got, want)
	}

	// Dedup
	dup := in + "\n*** Update File: EncryptDrive/Shared/UI/VaultTheme.swift\n"
	got = ExtractPatchFiles(dup)
	if len(got) != 2 {
		t.Fatalf("dedup failed: %v", got)
	}
}

func TestExtractPatchFilesNoFence(t *testing.T) {
	// Bash heredoc without Begin/End fence markers (some codex variants).
	in := `cd /Users/dazsec/projects/EncryptDrive && cat > /tmp/p.patch << 'PATCH'
*** Begin Patch
*** Update File: EncryptDrive/a.go
@@
-x
+y
*** End Patch
PATCH
apply_patch < /tmp/p.patch`
	got := ExtractPatchFiles(in)
	if len(got) != 1 || got[0] != "EncryptDrive/a.go" {
		t.Fatalf("heredoc extract = %v, want [EncryptDrive/a.go]", got)
	}
}

func TestExtractPatchFilesIgnoresNoise(t *testing.T) {
	if got := ExtractPatchFiles("echo hello\nls -la\n"); len(got) != 0 {
		t.Fatalf("noise text should yield nothing, got %v", got)
	}
}
