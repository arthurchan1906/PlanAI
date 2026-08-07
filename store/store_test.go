package store

import (
	"database/sql"
	"testing"

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
