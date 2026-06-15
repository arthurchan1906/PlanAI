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

// ListRecentDiscussions returns the most recent N discussion entries, optionally filtered.
func ListRecentDiscussions(source, typeFilter string, lastN int) ([]map[string]any, error) {
	if lastN <= 0 {
		lastN = 10
	}
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	where := "WHERE 1=1"
	var args []any
	if source != "" {
		where += " AND source = ?"
		args = append(args, source)
	}
	if typeFilter != "" {
		switch typeFilter {
		case "user":
			where += " AND role = 'user'"
		case "assistant":
			where += " AND role = 'assistant'"
		case "tool":
			where += " AND role = 'tool'"
		}
	}

	q := "SELECT id, session_id, role, source, content, metadata, created_at FROM discussion_log " + where + " ORDER BY created_at DESC, rowid DESC LIMIT ?"
	args = append(args, lastN)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, sid, role, src, content, metadata, createdAt string
		rows.Scan(&id, &sid, &role, &src, &content, &metadata, &createdAt)
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

// GetSessionMessages returns all messages for a given session, ordered by time.
func GetSessionMessages(sessionID string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(
		"SELECT id, session_id, role, source, content, metadata, created_at FROM discussion_log WHERE session_id = ? ORDER BY created_at ASC, rowid ASC",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, sid, role, src, content, metadata, createdAt string
		rows.Scan(&id, &sid, &role, &src, &content, &metadata, &createdAt)
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

// ReadDiscussionsOpts controls aipm_read_discussions / topic catchup queries.
type ReadDiscussionsOpts struct {
	Source  string
	LastN   int
	Since   string
	Full    bool
	TopicID string
}

// ReadDiscussions returns substantive discussion rows (user + non-tool assistant).
func ReadDiscussions(opts ReadDiscussionsOpts) ([]map[string]any, error) {
	since := opts.Since
	var closedAt string
	if opts.TopicID != "" {
		topic, err := GetCollaborationTopic(opts.TopicID)
		if err != nil {
			return nil, err
		}
		started := u.Str(topic["created_at"])
		if since == "" || since < started {
			since = started
		}
		closedAt = u.Str(topic["closed_at"])
	}

	where := "WHERE " + substantiveDiscussionSQL()
	var args []any
	if opts.Source != "" {
		where += " AND source = ?"
		args = append(args, opts.Source)
	}
	if since != "" {
		where += " AND created_at >= ?"
		args = append(args, since)
	}
	if closedAt != "" {
		where += " AND created_at <= ?"
		args = append(args, closedAt)
	}

	limit := opts.LastN
	if limit <= 0 {
		limit = 500
	}

	// Fetch newest-first, then trim to last_n and reverse to chronological.
	q := "SELECT id, session_id, role, source, content, metadata, created_at FROM discussion_log " +
		where + " ORDER BY created_at DESC, rowid DESC LIMIT ?"
	args = append(args, limit)

	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, sid, role, src, content, metadata, createdAt string
		rows.Scan(&id, &sid, &role, &src, &content, &metadata, &createdAt)
		results = append(results, map[string]any{
			"id": id, "session_id": sid, "role": role, "source": src,
			"content": content, "metadata": metadata, "created_at": createdAt,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}

	// Reverse to chronological order (oldest first).
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	return results, nil
}

func substantiveDiscussionSQL() string {
	toolEmojis := []string{"🔧", "📝", "👁", "🔍", "🆕", "🛠", "📡", "💭", "🗑", "📂", "🌐", "❓", "🤖", "📋"}
	notTool := ""
	for _, e := range toolEmojis {
		if notTool != "" {
			notTool += " AND "
		}
		notTool += "content NOT LIKE '" + e + "%'"
	}
	return "(role = 'user' OR (role = 'assistant' AND " + notTool + "))"
}

// GetDiscussionsByIDs returns discussion rows for the given IDs, in request order.
func GetDiscussionsByIDs(ids []string) ([]map[string]any, error) {
	if len(ids) == 0 {
		return []map[string]any{}, nil
	}
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	byID := map[string]map[string]any{}
	for _, id := range ids {
		var rid, sid, role, src, content, metadata, createdAt string
		row := db.QueryRow(
			"SELECT id, session_id, role, source, content, metadata, created_at FROM discussion_log WHERE id = ?",
			id,
		)
		if err := row.Scan(&rid, &sid, &role, &src, &content, &metadata, &createdAt); err != nil {
			continue
		}
		byID[id] = map[string]any{
			"id": rid, "session_id": sid, "role": role, "source": src,
			"content": content, "metadata": metadata, "created_at": createdAt,
		}
	}
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
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
