package app

import (
	"os"
	"path/filepath"
	"testing"

	"aipmc/ai"
	pmdb "aipmc/db"
)

// TestSummarizerFor_PerProject verifies the pipeline builds each project's AI
// summarizer from that project's own config, not the serve instance's home.
func TestSummarizerFor_PerProject(t *testing.T) {
	t.Setenv("AI_ENDPOINT", "")
	t.Setenv("AI_CHAT_MODEL", "")

	root := t.TempDir()
	projA := filepath.Join(root, "projA")
	if err := os.MkdirAll(filepath.Join(projA, ".pmai"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projA, ".pmai", "config.json"),
		[]byte(`{"ai_endpoint":"http://projA:9999/v1","ai_chat_model":"model-a"}`), 0644); err != nil {
		t.Fatal(err)
	}

	a := New()
	s := a.SummarizerFor(projA)
	if s == nil {
		t.Fatal("expected a summarizer for a project with ai_endpoint configured")
	}
	cli, ok := s.(*ai.Client)
	if !ok {
		t.Fatalf("SummarizerFor returned %T, want *ai.Client", s)
	}
	if !cli.Enabled() {
		t.Error("client should be enabled (endpoint non-empty)")
	}

	// Project without an endpoint → nil summarizer (L2 skipped gracefully).
	projNone := filepath.Join(root, "projNone")
	if err := os.MkdirAll(filepath.Join(projNone, ".pmai"), 0755); err != nil {
		t.Fatal(err)
	}
	if s2 := a.SummarizerFor(projNone); s2 != nil {
		t.Errorf("expected nil summarizer for unconfigured project, got %T", s2)
	}
}

// Silence unused-import error if db ends up unused in a future edit.
var _ = pmdb.Config{}
