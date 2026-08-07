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

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`CREATE TABLE commits (id TEXT PRIMARY KEY, commit_hash TEXT, task_id TEXT, status TEXT, review_status TEXT, test_status TEXT)`); err != nil {
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
