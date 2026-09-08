package store

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openTraceTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	stmts := []string{
		`CREATE TABLE commits (id TEXT PRIMARY KEY, commit_hash TEXT, title TEXT, status TEXT, review_status TEXT, test_status TEXT, created_at TEXT, task_id TEXT, files_json TEXT)`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, title TEXT, status TEXT)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return d
}

func mustExec(t *testing.T, d *sql.DB, q string, args ...interface{}) {
	t.Helper()
	if _, err := d.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// 反馈 #29: trace_context 增强——按文件路径反查最近改动 commit/task。
// 必须是 json_each 精确匹配(不误命中 store/trace.go.bak、store/other.go),
// LEFT JOIN task 带回任务标题/状态, since 窗口过滤。
func TestTraceFileContext(t *testing.T) {
	d := openTraceTestDB(t)
	mustExec(t, d, `INSERT INTO tasks (id, title, status) VALUES ('task-1','改 trace','done')`)
	mustExec(t, d, `INSERT INTO tasks (id, title, status) VALUES ('task-2','改别的','todo')`)
	mustExec(t, d, `INSERT INTO commits (id, commit_hash, title, status, review_status, test_status, created_at, task_id, files_json) VALUES ('c1','aaa','fix trace','committed','approved','passed','2026-09-07T10:00:00','task-1','["store/trace.go"]')`)
	mustExec(t, d, `INSERT INTO commits (id, commit_hash, title, status, review_status, test_status, created_at, task_id, files_json) VALUES ('c2','bbb','fix other','committed','approved','passed','2026-09-07T09:00:00','task-2','["store/other.go"]')`)
	// 子串/前缀陷阱: json_each 精确值不应命中 "store/trace.go"
	mustExec(t, d, `INSERT INTO commits (id, commit_hash, title, status, review_status, test_status, created_at, task_id, files_json) VALUES ('c3','ccc','fix bak','committed','approved','passed','2026-09-07T08:00:00','','["store/trace.go.bak"]')`)
	// 非法 JSON 应被 json_valid 跳过
	mustExec(t, d, `INSERT INTO commits (id, commit_hash, title, status, review_status, test_status, created_at, task_id, files_json) VALUES ('c4','ddd','bad json','committed','approved','passed','2026-09-07T07:00:00','','{bad')`)

	got, err := traceFileContext(d, "store/trace.go", "", 10)
	if err != nil {
		t.Fatalf("traceFileContext: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly 1 commit, got %d: %+v", len(got), got)
	}
	ref := got[0]
	if ref.CommitID != "c1" {
		t.Fatalf("commit id: got %q want c1", ref.CommitID)
	}
	if ref.TaskID != "task-1" || ref.TaskTitle != "改 trace" || ref.TaskStatus != "done" {
		t.Fatalf("task linkage: got %+v", ref)
	}

	// since 窗口: c1(10:00) 应被 11:00 之后的窗口排除
	got2, err := traceFileContext(d, "store/trace.go", "2026-09-07T11:00:00", 10)
	if err != nil {
		t.Fatalf("traceFileContext since: %v", err)
	}
	if len(got2) != 0 {
		t.Fatalf("since filter: want 0 commits, got %d: %+v", len(got2), got2)
	}

	// 文件不存在 → 空结果而非报错
	got3, err := traceFileContext(d, "nope/absent.go", "", 10)
	if err != nil {
		t.Fatalf("absent file: %v", err)
	}
	if len(got3) != 0 {
		t.Fatalf("absent file: want 0, got %d", len(got3))
	}
}
