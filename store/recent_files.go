package store

import (
	"database/sql"

	pmdb "aipmc/db"
)

// CommitEvidence links a file touch to a git commit that changed the same
// repo-relative path — the evidence chain for the orchestration view.
type CommitEvidence struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Hash      string `json:"hash"`
	CreatedAt string `json:"created_at"`
}

// FileTouch is one "who recently touched this file" row: a discussion_log
// entry whose metadata matches rel_path, plus linked commit evidence.
type FileTouch struct {
	ID        string           `json:"id"`
	SessionID string           `json:"session_id"`
	Source    string           `json:"source"`
	Content   string           `json:"content"`
	Metadata  string           `json:"metadata"`
	CreatedAt string           `json:"created_at"`
	Op        string           `json:"op"`
	Commits   []CommitEvidence `json:"commits,omitempty"`
}

// GetRecentFileSessions resolves to the cwd project.
func GetRecentFileSessions(relPath, since string, limit int) ([]FileTouch, error) {
	return GetRecentFileSessionsFor("", relPath, since, limit)
}

// GetRecentFileSessionsFor returns discussion_log entries whose metadata
// matches the repo-relative path exactly, most recent first. It covers both
// metadata shapes from the T3b field-unification work:
//   - claude: {"type":"edit","rel_path":"..."} — single value
//   - codex:  {"_type":"post_tool","type":"edit","rel_path":...,"rel_paths":[...]}
//     (apply_patch may touch several files, so the array is also matched)
//
// Invalid JSON metadata is tolerated (json_valid guard). Every returned row is
// enriched with commit evidence for the same file (hash/title/time).
func GetRecentFileSessionsFor(projectPath, relPath, since string, limit int) ([]FileTouch, error) {
	if limit <= 0 {
		limit = 20
	}
	db, err := pmdb.OpenProject(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT id, session_id, source, content, metadata, created_at,
		       COALESCE(NULLIF(json_extract(metadata,'$.type'), ''), 'tool')
		FROM discussion_log
		WHERE json_valid(metadata)
		  AND (json_extract(metadata,'$.rel_path') = ?
		       OR EXISTS (SELECT 1 FROM json_each(metadata,'$.rel_paths') WHERE json_each.value = ?))
		  AND created_at >= ?
		ORDER BY created_at DESC, rowid DESC
		LIMIT ?`, relPath, relPath, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	touches := []FileTouch{}
	for rows.Next() {
		var ft FileTouch
		if err := rows.Scan(&ft.ID, &ft.SessionID, &ft.Source, &ft.Content, &ft.Metadata, &ft.CreatedAt, &ft.Op); err != nil {
			return nil, err
		}
		touches = append(touches, ft)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if commits, err := recentFileCommits(db, relPath, since, limit); err == nil {
		for i := range touches {
			touches[i].Commits = commits
		}
	}
	return touches, nil
}

// recentFileCommits finds git commits whose files_json contains the exact
// repo-relative path (json_each match — no prefix/substring false positives).
func recentFileCommits(db *sql.DB, relPath, since string, limit int) ([]CommitEvidence, error) {
	rows, err := db.Query(`
		SELECT id, title, commit_hash, created_at FROM commits
		WHERE json_valid(files_json)
		  AND EXISTS (SELECT 1 FROM json_each(commits.files_json) WHERE json_each.value = ?)
		  AND created_at >= ?
		ORDER BY created_at DESC
		LIMIT ?`, relPath, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CommitEvidence{}
	for rows.Next() {
		var c CommitEvidence
		if err := rows.Scan(&c.ID, &c.Title, &c.Hash, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
