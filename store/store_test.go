package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aipmc/db"
	"aipmc/u"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`CREATE TABLE commits (id TEXT PRIMARY KEY, commit_hash TEXT, task_id TEXT, status TEXT, review_status TEXT, test_status TEXT, created_at TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return d
}

func insertCommit(t *testing.T, d *sql.DB, id, hash string) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO commits (id, commit_hash) VALUES (?, ?)`, id, hash); err != nil {
		t.Fatalf("insert commit %s: %v", id, err)
	}
}

// Regression: the dedup pre-check must NOT match empty/NULL commit_hash rows.
// A bare `? LIKE commit_hash || '%'` turns `'' || '%'` into `'%'`, matching
// every row and silently merging new hook-recorded commits into old rows.
func TestFindExistingCommitByHashIgnoresEmptyHash(t *testing.T) {
	d := openTestDB(t)
	insertCommit(t, d, "commit-empty-1", "")
	insertCommit(t, d, "commit-null-1", "")

	if got := findExistingCommitByHash(d, "9c533e20a525"); got != "" {
		t.Fatalf("empty-hash rows must not match, got %q", got)
	}
}

func TestFindExistingCommitByHashPrefixMatch(t *testing.T) {
	d := openTestDB(t)
	insertCommit(t, d, "commit-empty-1", "")
	insertCommit(t, d, "commit-short", "76276cf")
	insertCommit(t, d, "commit-other", "deadbeef")

	if got := findExistingCommitByHash(d, "76276cfaabbccdd"); got != "commit-short" {
		t.Fatalf("prefix match on real hash: got %q want commit-short", got)
	}
	if got := findExistingCommitByHash(d, "9c533e20a525"); got != "" {
		t.Fatalf("unknown hash must not match, got %q", got)
	}
}

// Regression: aipm_get_commit 允许按完整/短 git SHA 查（8/28 mcp_error：
// agent 从 git log 拿短 SHA e65ca3c 直接调用却按 id 查不到）。
func TestResolveCommitLookupID(t *testing.T) {
	d := openTestDB(t)
	// 表带 created_at，插入需列齐全；这里直接复用 openTestDB 的最小 schema
	// 不够（resolveCommitLookupID 的 SELECT 只读 commit_hash，不依赖 created_at）。
	if _, err := d.Exec(`INSERT INTO commits (id, commit_hash) VALUES (?, ?)`, "commit-1", "e65ca3c1234abcdef"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO commits (id, commit_hash) VALUES (?, ?)`, "commit-2", "deadbeef000011112222"); err != nil {
		t.Fatal(err)
	}

	// 短 SHA 前缀 → 解析为完整 SHA
	got, err := resolveCommitLookupID(d, "e65ca3c")
	if err != nil || got != "e65ca3c1234abcdef" {
		t.Fatalf("short hash prefix: got %q err=%v want e65ca3c1234abcdef", got, err)
	}
	// 完整 SHA 精确匹配
	got, err = resolveCommitLookupID(d, "deadbeef000011112222")
	if err != nil || got != "deadbeef000011112222" {
		t.Fatalf("full hash: got %q err=%v want deadbeef000011112222", got, err)
	}
	// PM id 原样返回
	if got, err = resolveCommitLookupID(d, "commit-1"); err != nil || got != "commit-1" {
		t.Fatalf("pm id: got %q err=%v want commit-1", got, err)
	}
	// 未知 short hash → 原 id（最终查询 no rows）
	if got, err = resolveCommitLookupID(d, "nope123"); err != nil || got != "nope123" {
		t.Fatalf("unknown: got %q err=%v want nope123", got, err)
	}
	// 空 id 防御：直接返回，不做 LIKE '%' 全表匹配
	if got, err = resolveCommitLookupID(d, ""); err != nil || got != "" {
		t.Fatalf("empty: got %q err=%v want ''", got, err)
	}
}

// Regression: done-gate must reject commits with empty/NULL commit_hash —
// they can't be traced to a real git object (StoreGitCommit bug aftermath:
// MCP-recorded commits without hash previously passed the gate).
func TestCountVerifiedCommitsRejectsEmptyHash(t *testing.T) {
	d := openTestDB(t)
	seed := "INSERT INTO commits (id, task_id, status, review_status, test_status, commit_hash) VALUES (?, ?, 'committed', 'approved', 'passed', ?)"
	for _, row := range [][3]string{
		{"commit-empty", "task-x", ""},
		{"commit-null", "task-x", ""},
		{"commit-good", "task-x", "9c533e20a5259860069385b066b9a1c24566af12"},
	} {
		if _, err := d.Exec(seed, row[0], row[1], row[2]); err != nil {
			t.Fatalf("seed %s: %v", row[0], err)
		}
	}

	if got := countVerifiedCommits(d, "task-x"); got != 1 {
		t.Fatalf("empty-hash commits must not count, got %d want 1", got)
	}
	if got := countVerifiedCommits(d, "task-none"); got != 0 {
		t.Fatalf("unknown task must have 0 verified commits, got %d", got)
	}
}

// Regression: the faucet — commit records must always carry a real git hash
// (ED: 538/547 empty-hash rows were MCP-recorded without commit_hash).
func TestCreateCommitRequiresHash(t *testing.T) {
	_, err := CreateCommit("/tmp/nowhere", "t", "", "", "", "", "", "task-x", "", "committed", "not_run", "pending", nil)
	if err == nil || !strings.Contains(err.Error(), "commit_hash") {
		t.Fatalf("empty commit_hash must be rejected, got %v", err)
	}
}

func TestBatchCreateCommitsRejectsEmptyHash(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".pmai", "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	d, err := sql.Open("sqlite", filepath.Join(dir, ".pmai", "data", "pmai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := db.EnsureSchema(d); err != nil {
		t.Fatal(err)
	}
	now := u.NowISO()
	if _, err := d.Exec(`INSERT INTO tasks (id, title, status, priority, phase, acceptance_json, related_docs_json, related_decisions_json, last_note, updated_at, created_at) VALUES ('task-x', 't', 'todo', 'P1', 'general', '[]', '[]', '[]', '', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}

	result, err := BatchCreateCommits(dir, "task-x", "main", "committed", "not_run", "pending", []BatchCommitItem{
		{Title: "no-hash", CommitHash: ""},
		{Title: "with-hash", CommitHash: "deadbeef1234"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success != 1 || result.Failed != 1 {
		t.Fatalf("want 1 success/1 failed, got %d/%d", result.Success, result.Failed)
	}
	if !strings.Contains(result.Details[0].Error, "commit_hash") {
		t.Fatalf("failed item must mention commit_hash, got %q", result.Details[0].Error)
	}
}

// Regression: a hook-recorded commit (task_id='') must be bindable when the
// agent later calls record_commit with the same hash — previously the dedup
// branch returned "already exists" and the orphan could never be linked.
func TestBackfillCommitTask(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".pmai", "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	d, err := sql.Open("sqlite", filepath.Join(dir, ".pmai", "data", "pmai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := db.EnsureSchema(d); err != nil {
		t.Fatal(err)
	}
	now := u.NowISO()
	// Simulate the git post-commit hook: no task context, full hash, files known.
	hookRow := "INSERT INTO commits (id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at) VALUES ('commit-hook-1', 'hook title', '', '', '', 'main', 'aa65155a4225d891e21d844eb391cc5adcaf4748', '', '', 'committed', 'auto', 'auto', '[]', ?, ?)"
	if _, err := d.Exec(hookRow, now, now); err != nil {
		t.Fatal(err)
	}

	// Agent calls record_commit for the same hash — must bind the orphan.
	outcome, err := BackfillCommitTask(dir, "commit-hook-1", "task-x", "hook title", []string{"a.go", "b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != BackfillBound {
		t.Fatalf("first backfill must bind (BackfillBound), got %d", outcome)
	}
	var taskID, filesJSON string
	if err := d.QueryRow("SELECT task_id, files_json FROM commits WHERE id = 'commit-hook-1'").Scan(&taskID, &filesJSON); err != nil {
		t.Fatal(err)
	}
	if taskID != "task-x" {
		t.Fatalf("task_id must be backfilled, got %q", taskID)
	}
	if filesJSON != `["a.go","b.go"]` {
		t.Fatalf("files must be backfilled, got %q", filesJSON)
	}

	// Second call with the same task is a no-op.
	outcome, err = BackfillCommitTask(dir, "commit-hook-1", "task-x", "hook title", nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != BackfillNoop {
		t.Fatalf("repeat backfill must be no-op, got %d", outcome)
	}

	// Rebinding to a different task is rejected.
	if _, err := BackfillCommitTask(dir, "commit-hook-1", "task-other", "hook title", nil); err != ErrCommitTaskConflict {
		t.Fatalf("conflicting task must return ErrCommitTaskConflict, got %v", err)
	}
}

// #24: ListBugs pagination — limit/offset 翻页必须生效（大结果集不再一次性全量）。
func TestListBugsPagination(t *testing.T) {
	setupDailyDB(t)
	if _, err := ListBugs("", "", "", 5, 0); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	for i := 0; i < 3; i++ {
		title := fmt.Sprintf("分页测试 bug %d", i)
		if _, err := CreateBug("", title, "desc", "major", "open", "", "", "", "", "", ""); err != nil {
			t.Fatalf("create bug %d: %v", i, err)
		}
	}
	page1, err := ListBugs("", "", "", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	page2, err := ListBugs("", "", "", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(page2))
	}
	if page1[1]["id"] == page2[0]["id"] {
		t.Error("offset page must not overlap page1")
	}
}
