package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: sendHook used to swallow every failure with `catch (_) {}`,
// so a broken plugin invocation left no trace. The plugin must surface
// execution errors via console.error while staying fail-open (it still must
// not throw, which would disrupt the opencode session).
func TestOpencodePluginLogsSendHookFailures(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PMAI_HOME", filepath.Join(root, "pmai-home"))

	if err := SetupOpencodeHooks("/custom/bin/aipmc"); err != nil {
		t.Fatalf("SetupOpencodeHooks: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".opencode", "plugins", "hook-recorder.js"))
	if err != nil {
		t.Fatalf("read plugin: %v", err)
	}
	js := string(data)

	if strings.Contains(js, "catch (_) {}") {
		t.Error("sendHook still swallows errors silently (catch (_) {})")
	}
	if !strings.Contains(js, "console.error") {
		t.Error("sendHook must surface execution failures via console.error")
	}
	if !strings.Contains(js, "hook-opencode send failed") {
		t.Error("failure log must identify the aipmc hook")
	}
}
