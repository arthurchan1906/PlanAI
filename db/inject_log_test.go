package db

import (
	"fmt"
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
		t.Errorf("suppressed = %d, want 0 (默认无裁剪)", suppressed)
	}

	// 8/18 修订写策略：char_limit 裁剪的请求已实际注入，写表且 suppressed=1
	if err := InsertInjectLog(InjectLogEntry{
		ID: "inj-test-2", Agent: "codex-cli", SessionID: "sess-B", ReqID: "r1-2",
		TS: "2026-08-18T12:01:00", Hash: "def45678", Source: "",
		SegmentsJSON: `{"fileAssoc":["a.go","b.go"]}`, Chars: 80, Suppressed: 1,
	}); err != nil {
		t.Fatalf("InsertInjectLog(suppressed=1): %v", err)
	}
	var supp int
	if err := d.QueryRow(`SELECT suppressed FROM inject_log WHERE id = ?`, "inj-test-2").Scan(&supp); err != nil {
		t.Fatalf("SELECT suppressed: %v", err)
	}
	if supp != 1 {
		t.Errorf("suppressed = %d, want 1 (修订后如实记录裁剪)", supp)
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

// retryBusy（8/25，C2 审核补测）：busy 错误重试至成功、非 busy 不重试、持续 busy 3 次放弃。
func TestRetryBusyRetriesThenSucceeds(t *testing.T) {
	n := 0
	err := RetryBusy(func() error {
		n++
		if n < 3 {
			return fmt.Errorf("SQLITE_BUSY: database is locked")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryBusy: %v", err)
	}
	if n != 3 {
		t.Errorf("calls = %d, want 3（两次 busy 后成功）", n)
	}
}

func TestRetryBusyNonBusyNoRetry(t *testing.T) {
	n := 0
	err := RetryBusy(func() error {
		n++
		return fmt.Errorf("boom")
	})
	if err == nil {
		t.Fatal("want error")
	}
	if n != 1 {
		t.Errorf("calls = %d, want 1（非 busy 错误不重试）", n)
	}
}

func TestRetryBusyExhausts(t *testing.T) {
	n := 0
	err := RetryBusy(func() error {
		n++
		return fmt.Errorf("database is locked")
	})
	if err == nil {
		t.Fatal("want error")
	}
	if n != 3 {
		t.Errorf("calls = %d, want 3（持续 busy 3 次后放弃）", n)
	}
}
