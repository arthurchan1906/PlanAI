package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression: POST canon/update used to swallow UpdateCanon errors
// (c, _ := ...) and reply 200 with a null body; failures must surface as 500.
func TestCanonUpdatePropagatesError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PMAI_HOME", home) // no pmai.db → UpdateCanon must fail

	s := New(Deps{})
	req := httptest.NewRequest("POST", "/pmai/canon/update", strings.NewReader(`{"add_scope":["a"]}`))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (error must propagate), body: %s", rec.Code, rec.Body.String())
	}
}
