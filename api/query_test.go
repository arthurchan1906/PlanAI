package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression: the daily POST/PUT handlers previously discarded the request
// body (hardcoded empty payload), so the frontend's daily note save was
// silently lost. dailyPayload must map the JSON body into the store payload.
func TestDailyPayload(t *testing.T) {
	p := dailyPayload(map[string]any{
		"completed": []any{"任务一", "任务二"},
		"problems":  []any{"阻塞"},
		"risks":     []any{},
		"next":      []any{"下一步", "", "x"},
	})
	if len(p["completed"]) != 2 || p["completed"][0] != "任务一" {
		t.Errorf("completed = %v, want [任务一 任务二]", p["completed"])
	}
	if len(p["problems"]) != 1 || p["problems"][0] != "阻塞" {
		t.Errorf("problems = %v, want [阻塞]", p["problems"])
	}
	if len(p["risks"]) != 0 {
		t.Errorf("risks = %v, want empty", p["risks"])
	}
	if len(p["next"]) != 2 || p["next"][0] != "下一步" {
		t.Errorf("next = %v, want [下一步 x] (empty strings dropped)", p["next"])
	}
}

// Regression: POST/PUT daily used to swallow AppendDailyNote/ReplaceDailyNote
// errors (d, _ := ...) and reply 200 with a null body; failures must surface
// as 500 (same class as the canon/update fix).
func TestDailyRoutesPropagateErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PMAI_HOME", home) // no pmai.db -> UpsertDaily must fail

	s := New(Deps{})
	for _, method := range []string{"POST", "PUT"} {
		req := httptest.NewRequest(method, "/pmai/daily?date=2026-08-14", strings.NewReader(`{"completed":["x"]}`))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s daily: status = %d, want 500 (error must propagate), body: %s", method, rec.Code, rec.Body.String())
		}
	}
}
