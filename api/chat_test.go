package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Regression: GET chat/session?id=... was joined into the session file path
// without validation — a traversal id (../../etc/passwd) could read arbitrary
// .json files. Invalid ids must be rejected with 400 before any file I/O.
func TestChatSessionRejectsTraversalSid(t *testing.T) {
	s := New(Deps{})

	for _, id := range []string{"../../etc/passwd", "..%2F..%2Fetc%2Fpasswd", "..", "a/b", "", "C:%5Cwindows%5Cfile"} {
		req := httptest.NewRequest("GET", "/pmai/chat/session?id="+id, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("id %q: status = %d, want 400", id, rec.Code)
		}
	}
}

// A well-formed id must pass validation and reach the session lookup (404
// here because the session file does not exist) — not be rejected as invalid.
func TestChatSessionAcceptsWellFormedSid(t *testing.T) {
	s := New(Deps{})
	req := httptest.NewRequest("GET", "/pmai/chat/session?id=s-20260814-101530-1a2b3c", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("well-formed id: status = %d, want 404 (passed validation)", rec.Code)
	}
}
