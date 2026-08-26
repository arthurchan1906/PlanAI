package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	pmdb "aipmc/db"
)

var errBusyStub = errors.New("database is locked (5) (SQLITE_BUSY)")

// TestSpoolFallbackThenFlush: 写失败事件落盘 → flush 补写 → spool 清空、
// discussion_log 补齐（P0 捕获缺口兜底，bug-20260826-154305-941881）。
func TestSpoolFallbackThenFlush(t *testing.T) {
	setupDailyDB(t)
	// Bootstrap schema（同 discussion_status_test：首次 Open 建表）。
	if _, err := ReadDiscussions(ReadDiscussionsOpts{LastN: 5}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	path := discussionSpoolPath()
	_ = os.Remove(path)

	if err := spoolDiscussionFallback("sess-spool", "assistant", "codex-cli", "事件A apply_patch 修复 metrics_test.go", `{"type":"post_tool"}`, errBusyStub); err != nil {
		t.Fatalf("spool fallback 1: %v", err)
	}
	if err := spoolDiscussionFallback("sess-spool", "assistant", "codex-cli", "事件B 写测试", "", errBusyStub); err != nil {
		t.Fatalf("spool fallback 2: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spool file missing: %v", err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 2 {
		t.Fatalf("spool lines = %d, want 2", lines)
	}

	if err := FlushDiscussionSpool(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		left, _ := os.ReadFile(path)
		t.Fatalf("spool not cleared: %q", string(left))
	}

	db, err := pmdb.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM discussion_log WHERE session_id='sess-spool'").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("flushed rows = %d, want 2", n)
	}
}

// TestSpoolEntryKeptOnFailedFlush: 补写失败（UNIQUE 冲突模拟）时条目保留在 spool，不丢。
func TestSpoolEntryKeptOnFailedFlush(t *testing.T) {
	setupDailyDB(t)
	if _, err := ReadDiscussions(ReadDiscussionsOpts{LastN: 5}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	path := discussionSpoolPath()
	_ = os.Remove(path)
	if err := spoolDiscussionFallback("sess-dup", "assistant", "codex-cli", "待补写", "", errBusyStub); err != nil {
		t.Fatalf("spool fallback: %v", err)
	}
	// 读 spool 拿 id，然后预插同 id 行制造 UNIQUE 冲突。
	data, _ := os.ReadFile(path)
	var e spoolEntry
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	if sc.Scan() {
		_ = json.Unmarshal(sc.Bytes(), &e)
	}
	db, err := pmdb.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, err = db.Exec("INSERT INTO discussion_log (id, session_id, role, source, content, metadata, created_at) VALUES (?, 'sess-dup', 'assistant', 'codex-cli', '占位', '', ?)", e.ID, e.CreatedAt)
	if err != nil {
		t.Fatalf("pre-insert: %v", err)
	}
	db.Close()

	if err := FlushDiscussionSpool(); err != nil {
		t.Fatalf("flush should not hard-fail: %v", err)
	}
	left, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spool entry lost on failed flush: %v", err)
	}
	if !strings.Contains(string(left), e.ID) {
		t.Fatalf("spool entry %s missing after failed flush: %q", e.ID, string(left))
	}
}
