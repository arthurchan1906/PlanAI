package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: the opencode plugin previously interpolated the raw payload into
// a shell command (`echo ${JSON.stringify(payload)} | aipmc hook-opencode`),
// giving LLM-controlled text a command-injection surface. It must ship as
// base64 on stdin, and SetupOpencodeHooks must inject the resolved command path.
func TestOpencodePluginBase64SafeAndCommandPath(t *testing.T) {
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

	if !strings.Contains(js, "base64 -d") {
		t.Error("plugin must pipe base64 to hook-opencode stdin")
	}
	if strings.Contains(js, "JSON.stringify(payload)} | aipmc") {
		t.Error("plugin still interpolates raw payload into the shell command")
	}
	if strings.Contains(js, "AIPMC_CMD") {
		t.Error("AIPMC_CMD placeholder was not replaced")
	}
	if !strings.Contains(js, "/custom/bin/aipmc") {
		t.Error("resolved aipmc command path was not injected")
	}
}
