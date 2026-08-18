package db

import (
	"testing"
)

func TestInjectLogSchemaAndRoundTrip(t *testing.T) {
	t.Setenv("PMAI_HOME", t.TempDir())
	if _, err := Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	d, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='inject_log'").Scan(&n); err != nil {
		t.Fatalf("query inject_log table: %v", err)
	}
	if n != 1 {
		t.Fatalf("inject_log table missing (n=%d)", n)
	}

	entry := InjectLogEntry{
		ID: "inj-test-1", Agent: "codex-cli", SessionID: "sess-A", ReqID: "r1-1",
		TS: "2026-08-18T12:00:00", Hash: "abc12345", Source: "guidelines_only",
		SegmentsJSON: `{"goals":["g1"],"guidelines":true}`, Chars: 120,
	}
	if err := InsertInjectLog(entry); err != nil {
		t.Fatalf("InsertInjectLog: %v", err)
	}

	var got InjectLogEntry
	var suppressed int
	err = d.QueryRow(`SELECT id, agent, session_id, req_id, ts, hash, source, segments_json, chars, suppressed FROM inject_log WHERE id = ?`, "inj-test-1").
		Scan(&got.ID, &got.Agent, &got.SessionID, &got.ReqID, &got.TS, &got.Hash, &got.Source, &got.SegmentsJSON, &got.Chars, &suppressed)
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if got.Agent != "codex-cli" || got.SessionID != "sess-A" || got.ReqID != "r1-1" {
		t.Errorf("identity fields mismatch: %+v", got)
	}
	if got.Hash != "abc12345" || got.Source != "guidelines_only" || got.SegmentsJSON == "" {
		t.Errorf("payload fields mismatch: %+v", got)
	}
	if got.Chars != 120 {
		t.Errorf("chars = %d, want 120", got.Chars)
	}
	if suppressed != 0 {
		t.Errorf("suppressed = %d, want 0 (T7: inject_log 不得出现 suppressed=1)", suppressed)
	}
}

func TestInsertInjectLogMissingDBReturnsError(t *testing.T) {
	t.Setenv("PMAI_HOME", t.TempDir())
	// 未 Bootstrap：Open 应报错，InsertInjectLog 返回 error 而非 panic
	err := InsertInjectLog(InjectLogEntry{ID: "inj-x", Agent: "codex-cli"})
	if err == nil {
		t.Fatal("expected error when db missing")
	}
}
