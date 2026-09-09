package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func mustExecT(t *testing.T, d *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// TestCollectEventStatsStateAware 锁定 9/9 根因修复：D2 的可行动计数只统计
// 「当前仍待处理」的事件。commit_orphan 事件若对应 commit 已绑定 task（不再
// 是孤儿），必须排除出可行动分母，否则 D2 处理率被历史事件虚增稀释。
func TestCollectEventStatsStateAware(t *testing.T) {
	// 用文件库而非 :memory:——循环内嵌套 db.QueryRow 时，:memory: 会落到另一个
	// 空连接导致 orphan 查询失败；文件库跨连接共享同一份数据。
	d, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "pmai.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	mustExecT(t, d, `CREATE TABLE events (id TEXT PRIMARY KEY, type TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, summary TEXT NOT NULL, created_at TEXT NOT NULL, consumed_by_agent INTEGER NOT NULL DEFAULT 0, processed_by_agent INTEGER NOT NULL DEFAULT 0)`)
	mustExecT(t, d, `CREATE TABLE commits (id TEXT PRIMARY KEY, task_id TEXT)`)

	// c1 = 已绑定（不再是孤儿）→ 其 commit_orphan 事件应为 stale，不计入可行动。
	mustExecT(t, d, `INSERT INTO commits (id, task_id) VALUES ('c1','task-1'),('c2',''),('c3','')`)

	mustExecT(t, d, `INSERT INTO events (id,type,entity_type,entity_id,summary,created_at,processed_by_agent) VALUES
		('ev1','commit_orphan','commit','c1','bound','2026-09-01',0),
		('ev2','commit_orphan','commit','c2','still orphan','2026-09-01',0),
		('ev3','commit_orphan','commit','c3','still orphan processed','2026-09-01',1),
		('ev4','mcp_error','tool','aipm_record_bug','err','2026-09-01',0),
		('ev5','tentative_link','commit','c2','free','2026-09-01',0)`)

	es, err := collectEventStats(d, "")
	if err != nil {
		t.Fatalf("collectEventStats: %v", err)
	}

	// 可行动 = ev2(c2 孤儿) + ev3(c3 孤儿已处理) + ev4(mcp_error)。ev1 为 stale 排除。
	if got, want := es.action, 3; got != want {
		t.Fatalf("action = %d, want %d", got, want)
	}
	if got, want := es.actionProc, 1; got != want {
		t.Fatalf("actionProc = %d, want %d", got, want)
	}
	if got, want := es.actionRate, 1.0/3.0; got != want {
		t.Fatalf("actionRate = %v, want %v", got, want)
	}
	// free 只含 tentative_link。
	if got, want := es.free, 1; got != want {
		t.Fatalf("free = %d, want %d", got, want)
	}
	if es.actionProc == es.action {
		// 不可能是 100% 处理：仍有 ev2/ev4 未处理。
		t.Fatalf("expected some unprocessed action events, got all processed")
	}
}
