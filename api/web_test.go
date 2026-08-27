package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// P5 重启（方案 A 落盘+只读）：web/snapshot 在 latest.json 存在时返回快照，
// 不存在时返回明确错误提示（不静默 200 空体）。
func TestWebSnapshotRoute(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PMAI_HOME", home)
	s := New(Deps{})

	// 无快照：必须报错提示先运行 aipmc snapshot。
	req := httptest.NewRequest("GET", "/pmai/web/snapshot", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("no-snapshot status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["ok"] == true {
		t.Errorf("no-snapshot ok=true, want false+error")
	}

	// 有快照：返回 snapshot 内容。
	dir := filepath.Join(home, "data", "snapshots")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), []byte(`{"schema_version":1,"metrics":{"quality":{"summary_coverage":0.392}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/pmai/web/snapshot", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	if body["ok"] != true {
		t.Errorf("with-snapshot ok = %v, want true (body=%s)", body["ok"], rec.Body.String())
	}
	snap, _ := body["snapshot"].(map[string]any)
	if snap == nil || snap["schema_version"] != float64(1) {
		t.Errorf("snapshot payload wrong: %v", body["snapshot"])
	}
}
