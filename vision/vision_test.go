package vision

import (
	"os"
	"path/filepath"
	"testing"
)

// testModelsJSON mirrors the shape of ~/.aipmc/models.json:
//   qwen-vl  — local vision model (low priority number = higher sort priority),
//   glm-vl   — cloud vision model with priority 0 (would win a pure priority sort),
//   ds-vl    — cloud vision model, no priority,
//   text-model — NOT vision-tagged; must never be selected.
const testModelsJSON = `{
  "version": 1,
  "providers": [
    {"name": "llama-local", "openai_url": "http://localhost:8080/v1"},
    {"name": "glm", "openai_url": "https://open.bigmodel.cn/api/paas/v4"},
    {"name": "deepseek", "openai_url": "https://api.deepseek.com"}
  ],
  "models": [
    {"id": "qwen-vl", "display_name": "Qwen VL", "routes": [{"provider": "llama-local", "model_openai": "qwen3.5-4b"}], "tags": ["vision", "local"], "priority": 1},
    {"id": "glm-vl", "display_name": "GLM VL", "routes": [{"provider": "glm", "model_openai": "glm-4.6v-flash"}], "tags": ["vision"], "priority": 0},
    {"id": "ds-vl", "display_name": "DS VL", "routes": [{"provider": "deepseek", "model_openai": "deepseek-v4-flash-vision-exp"}], "tags": ["vision"]},
    {"id": "text-model", "routes": [{"provider": "deepseek", "model_openai": "deepseek-v4-pro"}], "tags": []}
  ]
}`

// setupVisionTest points HOME at a temp dir containing a fixed models.json
// and an optional config.json, so resolveVisionModel is hermetic.
func setupVisionTest(t *testing.T, configJSON string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".aipmc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(testModelsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if configJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(configJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolveVisionModel_ConfiguredDefaultUsed(t *testing.T) {
	setupVisionTest(t, `{"vision_model": "ds-vl"}`)
	r, err := resolveVisionModel("")
	if err != nil {
		t.Fatalf("resolveVisionModel: %v", err)
	}
	if r.Provider != "deepseek" {
		t.Errorf("Provider = %q, want %q", r.Provider, "deepseek")
	}
	if r.RealModel != "deepseek-v4-flash-vision-exp" {
		t.Errorf("RealModel = %q, want %q", r.RealModel, "deepseek-v4-flash-vision-exp")
	}
	if r.DisplayName != "DS VL" {
		t.Errorf("DisplayName = %q, want %q", r.DisplayName, "DS VL")
	}
}

func TestResolveVisionModel_ExplicitOverridesConfiguredDefault(t *testing.T) {
	setupVisionTest(t, `{"vision_model": "glm-vl"}`)
	r, err := resolveVisionModel("ds-vl")
	if err != nil {
		t.Fatalf("resolveVisionModel: %v", err)
	}
	if r.Provider != "deepseek" {
		t.Errorf("Provider = %q, want %q", r.Provider, "deepseek")
	}
	if r.RealModel != "deepseek-v4-flash-vision-exp" {
		t.Errorf("RealModel = %q, want %q", r.RealModel, "deepseek-v4-flash-vision-exp")
	}
}

func TestResolveVisionModel_InvalidConfiguredDefaultFallsBackToAuto(t *testing.T) {
	setupVisionTest(t, `{"vision_model": "ghost-model"}`)
	r, err := resolveVisionModel("")
	if err != nil {
		t.Fatalf("resolveVisionModel: %v", err)
	}
	if r.Provider != "llama-local" {
		t.Errorf("Provider = %q, want %q (auto must fall back to local-first)", r.Provider, "llama-local")
	}
	if r.RealModel != "qwen3.5-4b" {
		t.Errorf("RealModel = %q, want %q", r.RealModel, "qwen3.5-4b")
	}
}

func TestResolveVisionModel_AutoPrefersLocalOverHigherPriorityCloud(t *testing.T) {
	setupVisionTest(t, "")
	r, err := resolveVisionModel("")
	if err != nil {
		t.Fatalf("resolveVisionModel: %v", err)
	}
	// glm-vl has priority 0 (higher than qwen-vl's 1), but local-first rule wins.
	if r.Provider != "llama-local" {
		t.Errorf("Provider = %q, want %q (local-first auto)", r.Provider, "llama-local")
	}
}

func TestResolveVisionModel_UnknownExplicitReturnsError(t *testing.T) {
	setupVisionTest(t, "")
	if _, err := resolveVisionModel("nope"); err == nil {
		t.Fatal("expected error for unknown explicit model")
	}
}
