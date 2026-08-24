package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aipmc/app"
)

// Round-trip: POST /pmai/config {vision_model} must persist to
// ~/.aipmc/config.json and GET /pmai/config must return it.
// Clearing with "" restores the auto-selection state.
func TestVisionModelConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PMAI_HOME", filepath.Join(home, "project"))
	for _, d := range []string{filepath.Join(home, "project"), filepath.Join(home, "cwd")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join(home, "cwd"))

	s := New(Deps{App: app.New()})

	// Initial state: not set.
	body := getConfig(t, s)
	if v, _ := body["vision_model"].(string); v != "" {
		t.Fatalf("initial vision_model = %q, want empty", v)
	}

	// Set.
	if !postConfig(t, s, `{"vision_model":"ds-vl"}`) {
		t.Fatal("POST set failed")
	}
	body = getConfig(t, s)
	if v, _ := body["vision_model"].(string); v != "ds-vl" {
		t.Fatalf("vision_model after set = %q, want ds-vl", v)
	}

	// Persisted to disk (~/.aipmc/config.json).
	data, err := os.ReadFile(filepath.Join(home, ".aipmc", "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if !strings.Contains(string(data), `"vision_model":"ds-vl"`) {
		t.Fatalf("config.json missing vision_model: %s", data)
	}

	// Clear (empty string restores auto).
	if !postConfig(t, s, `{"vision_model":""}`) {
		t.Fatal("POST clear failed")
	}
	body = getConfig(t, s)
	if v, _ := body["vision_model"].(string); v != "" {
		t.Fatalf("vision_model after clear = %q, want empty", v)
	}
}

func getConfig(t *testing.T, s *Server) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/pmai/config", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /pmai/config status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func postConfig(t *testing.T, s *Server, payload string) bool {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/pmai/config", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /pmai/config status = %d, body: %s", rec.Code, rec.Body.String())
	}
	return strings.Contains(rec.Body.String(), `"ok":true`)
}
