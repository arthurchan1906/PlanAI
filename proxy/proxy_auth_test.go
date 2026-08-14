package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Regression: state-changing /__proxy/* endpoints must require the proxy token.
// Previously any local process or cross-origin page could POST /__proxy/reload
// or /__proxy/capture/clear without credentials (Claude review).
func TestRequireProxyToken(t *testing.T) {
	old := proxyToken
	proxyToken = "aipmc-test-token"
	defer func() { proxyToken = old }()

	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	h := requireProxyToken(ok)

	// no token -> 401
	req := httptest.NewRequest(http.MethodPost, "/__proxy/capture/clear", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d, want 401", rec.Code)
	}

	// wrong token -> 401
	req = httptest.NewRequest(http.MethodPost, "/__proxy/capture/clear?token=wrong", nil)
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: got %d, want 401", rec.Code)
	}

	// correct query token -> pass
	req = httptest.NewRequest(http.MethodPost, "/__proxy/capture/clear?token="+proxyToken, nil)
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct query token: got %d, want 200", rec.Code)
	}

	// correct Bearer token -> pass
	req = httptest.NewRequest(http.MethodPost, "/__proxy/reload", nil)
	req.Header.Set("Authorization", "Bearer "+proxyToken)
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct bearer token: got %d, want 200", rec.Code)
	}
}

// Regression: captured request headers must not leak credentials into the
// inspect/capture log.
func TestCopyHeadersSanitizesCredentials(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	req.Header.Set("X-Goog-Api-Key", "googkey")
	req.Header.Set("X-Api-Key", "apikey")
	req.Header.Set("Cookie", "session=abc")
	req.Header.Set("X-Custom", "visible")

	h := copyHeaders(req)
	for _, k := range []string{"Authorization", "X-Goog-Api-Key", "X-Api-Key", "Cookie"} {
		if v := h[k]; v != "***" {
			t.Fatalf("%s: got %q, want ***", k, v)
		}
	}
	if v := h["X-Custom"]; v != "visible" {
		t.Fatalf("X-Custom: got %q, want visible", v)
	}
}
