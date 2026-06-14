package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunningBinaryPathAbsolute(t *testing.T) {
	p := RunningBinaryPath()
	if p == "" {
		t.Fatal("empty path")
	}
	if !filepath.IsAbs(strings.ReplaceAll(p, "/", string(filepath.Separator))) {
		t.Fatalf("not absolute: %q", p)
	}
}

func TestConfigCommandNotEmpty(t *testing.T) {
	cmd := ConfigCommand()
	if cmd == "" {
		t.Fatal("empty command")
	}
	if cmd != "aipmc" {
		if !filepath.IsAbs(strings.ReplaceAll(cmd, "/", string(filepath.Separator))) {
			t.Fatalf("expected aipmc or absolute path, got %q", cmd)
		}
	}
}

func TestConfigCommandMatchesExecutableWhenNotPortable(t *testing.T) {
	cmd := ConfigCommand()
	if cmd == "aipmc" {
		t.Skip("aipmc on PATH matches running binary")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	want := filepath.ToSlash(exe)
	if cmd != want {
		t.Fatalf("ConfigCommand() = %q, want %q", cmd, want)
	}
}
