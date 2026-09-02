package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func openDBT(t *testing.T) *sql.DB {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pmai.db")
	d, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func mustExecT(t *testing.T, d *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// 全新空库：EnsureSchema + migrate 必须成功，migrate 负责的表/列齐全。
func TestMigrateFreshDB(t *testing.T) {
	d := openDBT(t)
	if err := EnsureSchema(d); err != nil {
		t.Fatalf("EnsureSchema fresh: %v", err)
	}
	for _, tbl := range []string{"audit_log", "meeting_rooms", "meeting_turns", "discussion_log", "agent_profiles", "meeting_participants", "fts5_index", "session_summaries", "agent_status", "verification_log"} {
		if !tableOrVTableExists(d, tbl) {
			t.Errorf("table %s missing after fresh schema", tbl)
		}
	}
	for _, c := range [][2]string{
		{"discussion_log", "metadata"}, {"discussion_log", "thread_id"}, {"discussion_log", "source"},
		{"meeting_rooms", "agent_roles_context"}, {"meeting_rooms", "pm_typing"},
		{"meeting_turns", "reply_to"}, {"meeting_participants", "last_seen_turn"},
		{"bugs", "task_id"},
	} {
		if !ColumnExists(d, c[0], c[1]) {
			t.Errorf("column %s.%s missing after fresh schema", c[0], c[1])
		}
	}
}

// 旧库缺列：旧结构表已存在，EnsureSchema 不得重建，migrate 必须补列成功。
func TestMigrateLegacyDB(t *testing.T) {
	d := openDBT(t)
	mustExecT(t, d, `CREATE TABLE ideas (id TEXT PRIMARY KEY, title TEXT NOT NULL, summary TEXT NOT NULL, created_at TEXT NOT NULL)`)
	mustExecT(t, d, `CREATE TABLE discussion_log (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL)`)
	mustExecT(t, d, `CREATE TABLE meeting_rooms (id TEXT PRIMARY KEY, title TEXT NOT NULL, topic TEXT NOT NULL, context TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', created_by TEXT NOT NULL, created_at TEXT NOT NULL, closed_at TEXT)`)
	mustExecT(t, d, `CREATE TABLE meeting_turns (id TEXT PRIMARY KEY, room_id TEXT NOT NULL, turn_number INTEGER NOT NULL, speaker_type TEXT NOT NULL, speaker_id TEXT NOT NULL, question TEXT NOT NULL, response TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'waiting', created_at TEXT NOT NULL)`)
	mustExecT(t, d, `CREATE TABLE meeting_participants (meeting_id TEXT NOT NULL, agent_id TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', confirmed_at TEXT NOT NULL, PRIMARY KEY (meeting_id, agent_id))`)
	// 旧数据行必须在 migrate 前存在，backfill 才覆盖得到
	mustExecT(t, d, `INSERT INTO ideas (id, title, summary, created_at) VALUES ('i1', 't', 's1', '2026-01-01')`)
	if err := EnsureSchema(d); err != nil {
		t.Fatalf("EnsureSchema legacy: %v", err)
	}
	for _, c := range [][2]string{
		{"ideas", "current_summary"}, {"ideas", "updated_at"},
		{"discussion_log", "metadata"}, {"discussion_log", "source"}, {"discussion_log", "thread_id"},
		{"meeting_rooms", "agent_roles_context"}, {"meeting_rooms", "plan_id"},
		{"meeting_turns", "reply_to"}, {"meeting_participants", "last_seen_turn"},
	} {
		if !ColumnExists(d, c[0], c[1]) {
			t.Errorf("column %s.%s missing after legacy migrate", c[0], c[1])
		}
	}
	// ideas 回填：旧行 summary 应回填到 current_summary
	var cs, ua string
	if err := d.QueryRow(`SELECT current_summary, updated_at FROM ideas WHERE id='i1'`).Scan(&cs, &ua); err != nil {
		t.Fatalf("query ideas: %v", err)
	}
	if cs != "s1" || ua != "2026-01-01" {
		t.Errorf("ideas backfill wrong: current_summary=%q updated_at=%q", cs, ua)
	}
}

// P0④b 数据地基：老库缺 bugs.task_id 列 + 无 verification_log 表。
// EnsureSchema 不得重建，migrate 必须补列 + 补表。
func TestMigrateLegacyBugsTaskIDAndVerificationLog(t *testing.T) {
	d := openDBT(t)
	mustExecT(t, d, `CREATE TABLE bugs (id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT NOT NULL, severity TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL)`)
	mustExecT(t, d, `INSERT INTO bugs (id, title, description, severity, status, created_at) VALUES ('b1', 't', 'd', 'major', 'open', '2026-01-01')`)
	if err := EnsureSchema(d); err != nil {
		t.Fatalf("EnsureSchema legacy: %v", err)
	}
	if !ColumnExists(d, "bugs", "task_id") {
		t.Errorf("bugs.task_id missing after legacy migrate (SCHEMA_VERSION=%d)", SCHEMA_VERSION)
	}
	if !tableOrVTableExists(d, "verification_log") {
		t.Errorf("verification_log table missing after legacy migrate (SCHEMA_VERSION=%d)", SCHEMA_VERSION)
	}
	// 老行保底：task_id 默认 NULL，不因补列丢失数据
	var tid sql.NullString
	if err := d.QueryRow(`SELECT task_id FROM bugs WHERE id='b1'`).Scan(&tid); err != nil {
		t.Fatalf("query bugs.task_id: %v", err)
	}
	if tid.Valid {
		t.Errorf("legacy bug row task_id should be NULL, got %q", tid.String)
	}
}

// 错误透出：用 view 冒充 discussion_log 使 ALTER 必然失败，
// migrate 必须返回错误而不是静默吞掉。
func TestMigrateErrorPropagated(t *testing.T) {
	d := openDBT(t)
	mustExecT(t, d, `CREATE VIEW discussion_log AS SELECT 1 AS id`)
	if err := EnsureSchema(d); err == nil {
		t.Fatal("EnsureSchema with view-colliding discussion_log: expected error, got nil")
	} else if !strings.Contains(err.Error(), "discussion_log") {
		t.Errorf("error should name the failing migration, got: %v", err)
	}
}
