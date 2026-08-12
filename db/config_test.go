package db

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigFor verifies that per-project config loading reads each
// project's own .pmai/config.json without depending on the process cwd.
func TestLoadConfigFor(t *testing.T) {
	t.Setenv("AI_ENDPOINT", "")
	t.Setenv("AI_EMBEDDING_ENDPOINT", "")
	t.Setenv("AI_MODEL", "")
	t.Setenv("AI_CHAT_MODEL", "")
	t.Setenv("AI_API_KEY", "")

	root := t.TempDir()
	projA := filepath.Join(root, "projA")
	projB := filepath.Join(root, "projB")
	projNone := filepath.Join(root, "projNone")
	for _, p := range []string{projA, projB, projNone} {
		if err := os.MkdirAll(filepath.Join(p, ".pmai"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(projA, ".pmai", "config.json"),
		[]byte(`{"ai_endpoint":"http://projA:9999/v1","ai_chat_model":"model-a"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projB, ".pmai", "config.json"),
		[]byte(`{"ai_endpoint":"https://projB.example.com","ai_chat_model":"model-b"}`), 0644); err != nil {
		t.Fatal(err)
	}

	a := LoadConfigFor(projA)
	if a.AIEndpoint != "http://projA:9999/v1" || a.AIChatModel != "model-a" {
		t.Fatalf("projA config wrong: endpoint=%q chat_model=%q", a.AIEndpoint, a.AIChatModel)
	}
	b := LoadConfigFor(projB)
	if b.AIEndpoint != "https://projB.example.com" || b.AIChatModel != "model-b" {
		t.Fatalf("projB config wrong: endpoint=%q chat_model=%q", b.AIEndpoint, b.AIChatModel)
	}
	// Project with no config.json → defaults (endpoint empty, no AI).
	none := LoadConfigFor(projNone)
	if none.AIEndpoint != "" {
		t.Fatalf("projNone should have empty endpoint, got %q", none.AIEndpoint)
	}
}

// TestLoadConfigForEmptyUsesRuntimeDir verifies that an empty projectPath falls
// back to RuntimeDir semantics (PMAI_HOME first), preserving the pre-change
// LoadConfig behavior instead of hard-coding the process cwd.
func TestLoadConfigForEmptyUsesRuntimeDir(t *testing.T) {
	t.Setenv("AI_ENDPOINT", "")
	t.Setenv("AI_EMBEDDING_ENDPOINT", "")
	t.Setenv("AI_MODEL", "")
	t.Setenv("AI_CHAT_MODEL", "")
	t.Setenv("AI_API_KEY", "")

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.json"),
		[]byte(`{"ai_endpoint":"http://pmai-home:7777/v1","ai_chat_model":"home-model"}`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PMAI_HOME", home)

	cfg := LoadConfigFor("")
	if cfg.AIEndpoint != "http://pmai-home:7777/v1" || cfg.AIChatModel != "home-model" {
		t.Fatalf("LoadConfigFor(\"\") should read PMAI_HOME config, got endpoint=%q chat_model=%q", cfg.AIEndpoint, cfg.AIChatModel)
	}
}
