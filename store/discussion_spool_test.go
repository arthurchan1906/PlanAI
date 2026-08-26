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

	if err := spoolDiscussionFallback("sess-spool", "assistant", "codex-cli", "事件A apply_patch 修复 metrics_test.go", `{"type":"post_tool"}`, "2026-08-18T05:40:25Z", errBusyStub); err != nil {
		t.Fatalf("spool fallback 1: %v", err)
	}
	if err := spoolDiscussionFallback("sess-spool", "assistant", "codex-cli", "事件B 写测试", "", "2026-08-18T05:40:26Z", errBusyStub); err != nil {
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
	// created_at 必须保留事件时点（补写不挪出原窗口，P1 修正）。
	var firstAt string
	if err := db.QueryRow("SELECT created_at FROM discussion_log WHERE session_id='sess-spool' ORDER BY created_at LIMIT 1").Scan(&firstAt); err != nil {
		t.Fatalf("read created_at: %v", err)
	}
	if firstAt != "2026-08-18T05:40:25Z" {
		t.Fatalf("created_at = %q, want 事件时点 2026-08-18T05:40:25Z", firstAt)
	}
}

// TestSpoolEntrySkippedOnUniqueConflict: 补写遇 UNIQUE 冲突 = 条目已在库中（并发双写），
// 按「已补写」跳过并从 spool 移除（P1 修正：此前永久楔住 spool 首位，每次 flush 卡死）。
func TestSpoolEntrySkippedOnUniqueConflict(t *testing.T) {
	setupDailyDB(t)
	if _, err := ReadDiscussions(ReadDiscussionsOpts{LastN: 5}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	path := discussionSpoolPath()
	_ = os.Remove(path)
	if err := spoolDiscussionFallback("sess-dup", "assistant", "codex-cli", "待补写", "", "2026-08-18T05:40:25Z", errBusyStub); err != nil {
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
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		left, _ := os.ReadFile(path)
		t.Fatalf("UNIQUE 冲突条目应按已补写跳过并清空 spool，got: %q", string(left))
	}
}

// TestSpoolDropsWhenFull: spool 达 maxSpoolEntries 上限后新事件被丢弃并告警
// （Claude C3：防 flush 持续失败时 JSONL 无界膨胀），文件不增长。
// 必须 setupDailyDB 隔离 PMAI_HOME——否则 discussionSpoolPath 解析到真实
// ~/.aipmc/cache/，测试会删除/写入真实 spool 并污染生产库（S1 实证教训）。
func TestSpoolDropsWhenFull(t *testing.T) {
	setupDailyDB(t)
	path := discussionSpoolPath()
	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open spool: %v", err)
	}
	for i := 0; i < maxSpoolEntries; i++ {
		if _, err := f.WriteString("{\"id\":\"seed\"}\n"); err != nil {
			t.Fatalf("seed spool: %v", err)
		}
	}
	f.Close()
	if err := spoolDiscussionFallback("sess-full", "assistant", "codex-cli", "超限事件", "", "2026-08-26T09:00:00Z", errBusyStub); !errors.Is(err, errSpoolFull) {
		t.Fatalf("超限应返回 errSpoolFull（丢弃+告警）: %v", err)
	}
	n, err := countSpoolEntries(path)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != maxSpoolEntries {
		t.Fatalf("spool entries = %d, want %d（不增长）", n, maxSpoolEntries)
	}
}
