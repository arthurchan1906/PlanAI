package hook

import (
	"reflect"
	"testing"
)

func TestExtractBashFileOpsGitAdd(t *testing.T) {
	ops := extractBashFileOps("cd /Users/dazsec/projects/EncryptDrive && git add EncryptDrive/a.go EncryptDrive/b.go")
	want := []BashFileOp{
		{Op: "stage", File: "EncryptDrive/a.go"},
		{Op: "stage", File: "EncryptDrive/b.go"},
	}
	if !reflect.DeepEqual(ops, want) {
		t.Fatalf("git add ops = %v, want %v", ops, want)
	}

	// Directory / bare tokens are not staged as files.
	if ops := extractBashFileOps("git add ."); len(ops) != 0 {
		t.Fatalf("git add . should be skipped, got %v", ops)
	}
	if ops := extractBashFileOps("git add -A"); len(ops) != 0 {
		t.Fatalf("git add -A should be skipped, got %v", ops)
	}
	// Bare filename without a path separator is not confidently a file.
	if ops := extractBashFileOps("git add VaultTheme.swift"); len(ops) != 0 {
		t.Fatalf("bare filename should be skipped, got %v", ops)
	}
}

func TestExtractBashFileOpsReads(t *testing.T) {
	ops := extractBashFileOps("cat EncryptDrive/README.md EncryptDrive/CHANGELOG.md")
	want := []BashFileOp{
		{Op: "read", File: "EncryptDrive/README.md"},
		{Op: "read", File: "EncryptDrive/CHANGELOG.md"},
	}
	if !reflect.DeepEqual(ops, want) {
		t.Fatalf("cat ops = %v, want %v", ops, want)
	}

	// wc -l and quoted paths.
	ops = extractBashFileOps(`wc -l "EncryptDrive/My File.swift"`)
	want = []BashFileOp{{Op: "read", File: "EncryptDrive/My File.swift"}}
	if !reflect.DeepEqual(ops, want) {
		t.Fatalf("wc ops = %v, want %v", ops, want)
	}

	// Redirection breaks the argument list; target of > is not a cat arg.
	ops = extractBashFileOps("cat EncryptDrive/a.go > /tmp/out.txt")
	if len(ops) != 1 || ops[0].File != "EncryptDrive/a.go" {
		t.Fatalf("cat with redirect ops = %v", ops)
	}
}

func TestExtractBashFileOpsSedAndFind(t *testing.T) {
	// sed -n reads; sed -i stays with parseBashFileOp (modify).
	ops := extractBashFileOps("sed -n '1,10p' EncryptDrive/a.go")
	want := []BashFileOp{{Op: "read", File: "EncryptDrive/a.go"}}
	if !reflect.DeepEqual(ops, want) {
		t.Fatalf("sed -n ops = %v, want %v", ops, want)
	}
	if ops := extractBashFileOps("sed -i 's/x/y/' EncryptDrive/a.go"); len(ops) != 0 {
		t.Fatalf("sed -i should stay with parseBashFileOp, got %v", ops)
	}

	// find start path (directory traversal, read semantics).
	ops = extractBashFileOps("find EncryptDrive/Features -name '*.swift'")
	want = []BashFileOp{{Op: "read", File: "EncryptDrive/Features"}}
	if !reflect.DeepEqual(ops, want) {
		t.Fatalf("find ops = %v, want %v", ops, want)
	}
	if ops := extractBashFileOps("find . -name '*.go'"); len(ops) != 0 {
		t.Fatalf("find . should be skipped, got %v", ops)
	}
}

func TestExtractBashFileOpsXcodebuild(t *testing.T) {
	ops := extractBashFileOps("xcodebuild -project EncryptDrive.xcodeproj -scheme EncryptDrive build")
	want := []BashFileOp{{Op: "read", File: "EncryptDrive.xcodeproj"}}
	if !reflect.DeepEqual(ops, want) {
		t.Fatalf("xcodebuild ops = %v, want %v", ops, want)
	}
}

func TestExtractBashFileOpsLowConfidence(t *testing.T) {
	for _, cmd := range []string{
		"python3 << 'PYEOF'\nimport os\nprint('x')\nPYEOF",
		"git status",
		"ls -la",
		"echo hello world",
		"xcodebuild -scheme X build", // no -project
	} {
		if ops := extractBashFileOps(cmd); len(ops) != 0 {
			t.Fatalf("low-confidence %q should return nil, got %v", cmd, ops)
		}
	}
}
