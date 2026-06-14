package store

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	pmdb "aipmc/db"
	"aipmc/u"
)

// ---- Discussion Log (pure DB, no AI dependency) ----

// LogDiscussion records a discussion entry in the discussion_log table.
func LogDiscussion(sessionID, role, source, content, metadataJSON string) (map[string]any, error) {
	var lastErr error
	for attempt := 0; attempt < 15; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*25) * time.Millisecond)
		}
		var result map[string]any
		var err error
		withDiscussionLogLock(func() {
			result, err = logDiscussionOnce(sessionID, role, source, content, metadataJSON)
		})
		if err == nil {
			return result, nil
		}
		if !isSQLiteBusy(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func discussionLogLockPath() string {
	runtimeDir, err := pmdb.RuntimeDir()
	if err != nil || runtimeDir == "" {
		return filepath.Join(os.TempDir(), "aipm-discussion-log.lock")
	}
	cacheDir := filepath.Join(runtimeDir, "cache")
	_ = os.MkdirAll(cacheDir, 0755)
	return filepath.Join(cacheDir, "discussion-log.lock")
}

// withDiscussionLogLock serializes discussion_log writes across concurrent hook processes.
func withDiscussionLogLock(fn func()) {
	lockPath := discussionLogLockPath()
	var release func()
	for i := 0; i < 300; i++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			release = func() {
				f.Close()
				_ = os.Remove(lockPath)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if release != nil {
		defer release()
	}
	fn()
}

func logDiscussionOnce(sessionID, role, source, content, metadataJSON string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("disc")
	now := u.NowISO()
	sid := sessionID
	if sid == "" {
		sid = "unknown"
	}
	_, err = db.Exec("INSERT INTO discussion_log (id, session_id, role, source, content, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)", id, sid, role, source, content, metadataJSON, now)
	if err != nil {
		return nil, err
	}
	preview := content
	if len([]rune(preview)) > 80 {
		preview = string([]rune(preview)[:80])
	}
	pmdb.SyncFTS5Entity(db, "discussion", id, "["+role+"]["+source+"] "+preview, content)
	return map[string]any{"id": id, "status": "created"}, nil
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "sqlite_busy")
}

// RecentUserPrompts returns the most recent user message contents for a session.
func RecentUserPrompts(sessionID, source string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 10
	}
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT content FROM discussion_log
		 WHERE session_id = ? AND role = 'user' AND source = ?
		 ORDER BY created_at DESC LIMIT ?`,
		sessionID, source, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

// ListDiscussionSources returns distinct source names from discussion_log.
func ListDiscussionSources() ([]string, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT DISTINCT source FROM discussion_log WHERE source != '' ORDER BY source")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		result = append(result, s)
	}
	if result == nil {
		result = []string{}
	}
	return result, nil
}
