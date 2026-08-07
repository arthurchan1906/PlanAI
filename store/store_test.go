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
	if _, err := d.Exec(`CREATE TABLE commits (id TEXT PRIMARY KEY, commit_hash TEXT)`); err != nil {
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
