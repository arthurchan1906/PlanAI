package main

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func mustExecT(t *testing.T, d *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// TestCollectEventStatsStateAware 锁定 9/9 修复：D2 的可行动计数只统计
// 「当前仍待处理」的事件。commit_orphan 按 commit 是否仍无 task（真状态）；
// mcp_error/hotspot_untracked 按新鲜度（近 eventFreshness 内才算当前），
// 避免历史 stale 事件虚增分母。
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

	// c1 = 已绑定（不再孤儿）；c2/c3 = 仍孤儿。
	mustExecT(t, d, `INSERT INTO commits (id, task_id) VALUES ('c1','task-1'),('c2',''),('c3','')`)

	now := time.Now()
	fresh := now.Add(-time.Hour).Format("2006-01-02T15:04:05")              // 近 7 天内
	stale := now.Add(-8 * 24 * time.Hour).Format("2006-01-02T15:04:05")    // 超过新鲜度窗口

	mustExecT(t, d, `INSERT INTO events (id,type,entity_type,entity_id,summary,created_at,processed_by_agent) VALUES
		('ev1','commit_orphan','commit','c1','bound (stale)','`+stale+`',0),
		('ev2','commit_orphan','commit','c2','still orphan','`+fresh+`',0),
		('ev3','commit_orphan','commit','c3','still orphan processed','`+fresh+`',1),
		('ev4','mcp_error','aipm_get_task','task-9','recent err','`+fresh+`',0),
		('ev5','mcp_error','aipm_record_bug','aipm_record_bug','old err (stale)','`+stale+`',0),
		('ev6','hotspot_untracked','file','docs/x.md','recent hotspot','`+fresh+`',0),
		('ev7','hotspot_untracked','file','old/doc.md','old hotspot (stale)','`+stale+`',0),
		('ev8','tentative_link','commit','c2','free','`+fresh+`',0)`)

	es, err := collectEventStats(d, "")
	if err != nil {
		t.Fatalf("collectEventStats: %v", err)
	}

	// 可行动 = ev2(c2 孤儿) + ev3(c3 孤儿已处理) + ev4(mcp 近期) + ev6(hotspot 近期)。
	// 排除：ev1(已绑定) ev5(mcp stale) ev7(hotspot stale)。
	if got, want := es.action, 4; got != want {
		t.Fatalf("action = %d, want %d (dist=%v)", got, want, es.actionDist)
	}
	if got, want := es.actionProc, 1; got != want {
		t.Fatalf("actionProc = %d, want %d", got, want)
	}
	if got, want := es.actionRate, 1.0/4.0; got != want {
		t.Fatalf("actionRate = %v, want %v", got, want)
	}
	if got, want := es.free, 1; got != want {
		t.Fatalf("free = %d, want %d", got, want)
	}
}
