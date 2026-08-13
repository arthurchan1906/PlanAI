package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	pmdb "aipmc/db"
)

func newRecentFilesDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".pmai", "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	d, err := sql.Open("sqlite", filepath.Join(dir, ".pmai", "data", "pmai.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := pmdb.EnsureSchema(d); err != nil {
		t.Fatal(err)
	}
	return dir, d
}

func seedRecentFiles(t *testing.T, d *sql.DB) {
	t.Helper()
	disc := []struct {
		id, sid, role, src, content, metadata, created string
	}{
		{"d1", "s-claude", "tool", "claude-code", "📝 /abs/x.swift",
			`{"type":"edit","file_path":"/abs/x.swift","rel_path":"EncryptDrive/x.swift"}`,
			"2026-08-12T10:00:00"},
		{"d2", "s-codex", "assistant", "codex-cli", "📝 EncryptDrive/a.go",
			`{"_type":"post_tool","type":"edit","file_path":"EncryptDrive/a.go","rel_path":"EncryptDrive/a.go","rel_paths":["EncryptDrive/a.go","EncryptDrive/b.go"]}`,
			"2026-08-12T11:00:00"},
		{"d3", "s-codex", "assistant", "codex-cli", "🔧 ls",
			`{"_type":"post_tool","type":"bash","command":"ls -la"}`,
			"2026-08-12T11:30:00"},
		{"d4", "s-dirty", "tool", "claude-code", "🛠 broken",
			"not-json-at-all",
			"2026-08-12T12:00:00"},
		{"d5", "s-late", "assistant", "codex-cli", "📝 EncryptDrive/x.swift",
			`{"_type":"post_tool","type":"edit","rel_path":"EncryptDrive/x.swift"}`,
			"2026-08-13T09:00:00"},
	}
	for _, r := range disc {
		if _, err := d.Exec(
			`INSERT INTO discussion_log (id, session_id, role, source, content, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.id, r.sid, r.role, r.src, r.content, r.metadata, r.created); err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
	}

	commits := []struct {
		id, hash, files, created string
	}{
		{"c1", "aaaa1111", `["EncryptDrive/a.go","z/w.swift"]`, "2026-08-12T11:05:00"},
		{"c2", "bbbb2222", `["EncryptDrive/a.go2"]`, "2026-08-12T11:50:00"},
		{"c3", "cccc3333", `[]`, "2026-08-12T12:00:00"},
	}
	for _, c := range commits {
		if _, err := d.Exec(
			`INSERT INTO commits (id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at)
			 VALUES (?, '', '', '', '', 'main', ?, '', '', 'committed', 'auto', 'auto', ?, ?, ?)`,
			c.id, c.hash, c.files, c.created, c.created); err != nil {
			t.Fatalf("insert commit %s: %v", c.id, err)
		}
	}
}

func TestGetRecentFileSessionsFor(t *testing.T) {
	dir, d := newRecentFilesDB(t)
	seedRecentFiles(t, d)

	// Primary rel_path match (codex single-file apply_patch).
	got, err := GetRecentFileSessionsFor(dir, "EncryptDrive/a.go", "2026-08-01T00:00:00", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("a.go touches = %d, want 1", len(got))
	}
	if got[0].ID != "d2" || got[0].Op != "edit" {
		t.Fatalf("touch = %+v", got[0])
	}
	if len(got[0].Commits) != 1 || got[0].Commits[0].ID != "c1" {
		t.Fatalf("commits = %+v", got[0].Commits)
	}

	// Array match: codex apply_patch touched b.go too.
	got, err = GetRecentFileSessionsFor(dir, "EncryptDrive/b.go", "2026-08-01T00:00:00", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "d2" {
		t.Fatalf("b.go touches = %+v", got)
	}

	// Exact match: a.go2 must not match a.go.
	got, err = GetRecentFileSessionsFor(dir, "EncryptDrive/a.go2", "2026-08-01T00:00:00", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a.go2 touches = %+v, want 0", got)
	}

	// Dirty metadata tolerated (d4 garbage) and bash-only row (d3) excluded.
	got, err = GetRecentFileSessionsFor(dir, "EncryptDrive/x.swift", "2026-08-01T00:00:00", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("x.swift touches = %+v, want 2 (claude d1 + codex d5)", got)
	}

	// since filter.
	got, err = GetRecentFileSessionsFor(dir, "EncryptDrive/x.swift", "2026-08-13T00:00:00", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "d5" {
		t.Fatalf("since-filtered touches = %+v", got)
	}

	// limit.
	got, err = GetRecentFileSessionsFor(dir, "EncryptDrive/x.swift", "2026-08-01T00:00:00", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "d5" {
		t.Fatalf("limit touches = %+v", got)
	}
}
