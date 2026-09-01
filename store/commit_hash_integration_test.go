package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aipmc/db"
	"aipmc/u"
	_ "modernc.org/sqlite"
)

// TestBatchCreateCommitsRejectsBadHash 验证批量入口的格式校验：
// HEAD / $(cmd) / 非十六进制等非法 commit_hash 必须 Failed，合法 hash 才 Success。
// 这是 CreateCommit/StoreGitCommit 之外的旁路——BatchCreateCommits 若只校验非空，
// 脏 hash 仍可经 MCP aipm_record_commits 入库。
func TestBatchCreateCommitsRejectsBadHash(t *testing.T) {
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
		{Title: "bad-head", CommitHash: "HEAD"},
		{Title: "bad-cmd", CommitHash: "$(git rev-parse HEAD)"},
		{Title: "bad-nonhex", CommitHash: "not-a-hash"},
		{Title: "ok", CommitHash: "deadbeef1234"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success != 1 || result.Failed != 3 {
		t.Fatalf("want 1 success/3 failed, got %d/%d", result.Success, result.Failed)
	}
	for i := 0; i < 3; i++ {
		if !strings.Contains(result.Details[i].Error, "invalid commit_hash") {
			t.Errorf("item %d error must mention invalid commit_hash, got %q", i, result.Details[i].Error)
		}
	}
	if result.Details[3].Error != "" {
		t.Errorf("valid item must have no error, got %q", result.Details[3].Error)
	}
}
