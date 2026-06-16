package store

import (
	"database/sql"
	"fmt"

	pmdb "aipmc/db"
	"aipmc/u"
)

// SessionSummary row in session_summaries.
type SessionSummary struct {
	SessionID    string
	Source       string
	ReviewJSON   string
	Summary      string
	Intent       string
	EntityRefs   string
	QualityScore int
	CreatedAt    string
}

// UpsertSessionSummary inserts or replaces a session review row.
func UpsertSessionSummary(row SessionSummary) error {
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()

	now := u.NowISO()
	if row.CreatedAt == "" {
		row.CreatedAt = now
	}
	_, err = db.Exec(`
		INSERT INTO session_summaries
			(session_id, source, review_json, summary, intent, entity_refs, quality_score, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			source = excluded.source,
			review_json = excluded.review_json,
			summary = excluded.summary,
			intent = excluded.intent,
			entity_refs = excluded.entity_refs,
			quality_score = excluded.quality_score,
			created_at = excluded.created_at`,
		row.SessionID, row.Source, row.ReviewJSON, row.Summary, row.Intent, row.EntityRefs, row.QualityScore, row.CreatedAt,
	)
	return err
}

// ListSessionSummariesSince returns reviewed sessions since the ISO cutoff.
func ListSessionSummariesSince(since string, limit int) ([]SessionSummary, error) {
	if limit <= 0 {
		limit = 100
	}
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	q := `SELECT session_id, source, review_json, summary, intent, entity_refs, quality_score, created_at
		FROM session_summaries`
	var args []any
	if since != "" {
		q += " WHERE created_at >= ?"
		args = append(args, since)
	}
	q += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionSummary
	for rows.Next() {
		var r SessionSummary
		if err := rows.Scan(&r.SessionID, &r.Source, &r.ReviewJSON, &r.Summary, &r.Intent, &r.EntityRefs, &r.QualityScore, &r.CreatedAt); err != nil {
			continue
		}
		out = append(out, r)
	}
	if out == nil {
		out = []SessionSummary{}
	}
	return out, nil
}

// GetSessionSummary returns one row or nil if missing.
func GetSessionSummary(sessionID string) (*SessionSummary, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var r SessionSummary
	err = db.QueryRow(`
		SELECT session_id, source, review_json, summary, intent, entity_refs, quality_score, created_at
		FROM session_summaries WHERE session_id = ?`, sessionID,
	).Scan(&r.SessionID, &r.Source, &r.ReviewJSON, &r.Summary, &r.Intent, &r.EntityRefs, &r.QualityScore, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListOrphanMCPRows returns MCP log rows with no real session_id.
func ListOrphanMCPRows(since string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	q := `SELECT id, session_id, role, source, content, metadata, created_at
		FROM discussion_log
		WHERE (session_id = '' OR session_id = 'unknown')
		  AND content LIKE '📡 aipm_%'`
	var args []any
	if since != "" {
		q += " AND created_at >= ?"
		args = append(args, since)
	}
	q += " ORDER BY created_at ASC, rowid ASC"

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, sid, role, src, content, metadata, createdAt string
		if err := rows.Scan(&id, &sid, &role, &src, &content, &metadata, &createdAt); err != nil {
			continue
		}
		results = append(results, map[string]any{
			"id": id, "session_id": sid, "role": role, "source": src,
			"content": content, "metadata": metadata, "created_at": createdAt,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}
	return results, nil
}

// EnsureSessionSummariesTable is a no-op when schema migration already ran.
func EnsureSessionSummariesTable() error {
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS session_summaries (
			session_id TEXT PRIMARY KEY,
			source TEXT NOT NULL DEFAULT '',
			review_json TEXT NOT NULL DEFAULT '{}',
			summary TEXT NOT NULL DEFAULT '',
			intent TEXT NOT NULL DEFAULT '',
			entity_refs TEXT NOT NULL DEFAULT '[]',
			quality_score INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`)
	if err != nil {
		return fmt.Errorf("session_summaries: %w", err)
	}
	return nil
}
