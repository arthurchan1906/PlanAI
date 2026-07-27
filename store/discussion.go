package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	pmdb "aipmc/db"
	"aipmc/u"
)

const discussionLinkSourceType = "discussion"
const discussionLinkRelation = "relates_to"

// entityIDPattern matches AIPM entity IDs (e.g. task-20260615-172610-6ccede).
var entityIDPattern = regexp.MustCompile(`(?i)(task|plan|decision|commit|bug)-\d{8}-\d{6}-[a-f0-9]{6}`)

// EntityRef ties a discussion session to a referenced PM entity.
type EntityRef struct {
	SessionID  string
	TargetType string
	TargetID   string
}

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
	// Dedup: skip if identical to the last message for this session
	if role == "assistant" {
		var lastContent string
		db.QueryRow("SELECT content FROM discussion_log WHERE session_id=? AND role='assistant' ORDER BY created_at DESC LIMIT 1", sid).Scan(&lastContent)
		if lastContent == content {
			return map[string]any{"id": id, "status": "skipped_duplicate"}, nil
		}
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
func ListRecentDiscussions(source, typeFilter, projectPath string, lastN int, cursor string) ([]map[string]any, error) {
	if lastN <= 0 {
		lastN = 10
	}
	db, err := openOrCurrentDB(projectPath)
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
	if cursor != "" {
		where += " AND id > ?"
		args = append(args, cursor)
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

// ReadDiscussionsOpts controls aipm_read_discussions queries.
type ReadDiscussionsOpts struct {
	Source      string
	LastN       int
	Since       string
	Cursor      string
	Full        bool
	ProjectPath string
}

// ReadDiscussions returns substantive discussion rows (user + non-tool assistant).
func ReadDiscussions(opts ReadDiscussionsOpts) ([]map[string]any, error) {
	where := "WHERE " + substantiveDiscussionSQL()
	var args []any
	if opts.Source != "" {
		where += " AND source = ?"
		args = append(args, opts.Source)
	}
	if opts.Since != "" {
		where += " AND created_at >= ?"
		args = append(args, opts.Since)
	}

	limit := opts.LastN
	if limit <= 0 {
		limit = 15
	}

	// Cursor-based incremental read: only fetch rows after the cursor chronologically.
	// id format "disc-YYYYMMDD-HHMMSS-xxxxxx" is lexicographically time-ordered.
	if opts.Cursor != "" {
		where += " AND id > ?"
		args = append(args, opts.Cursor)
	}

	// Fetch newest-first, then trim to last_n and reverse to chronological.
	q := "SELECT id, session_id, role, source, content, metadata, created_at FROM discussion_log " +
		where + " ORDER BY created_at DESC, rowid DESC LIMIT ?"
	args = append(args, limit)

	db, err := openOrCurrentDB(opts.ProjectPath)
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

func toolCallCountSQL() string {
	prefixes := []string{"🔧", "📝", "👁", "🔍", "🆕", "🛠", "📡", "🗑", "📂", "🌐", "❓", "🤖", "📋"}
	var parts []string
	parts = append(parts, "role = 'tool'")
	for _, p := range prefixes {
		parts = append(parts, "(role = 'assistant' AND content LIKE '"+p+"%')")
	}
	return "SUM(CASE WHEN " + strings.Join(parts, " OR ") + " THEN 1 ELSE 0 END)"
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

// AgentSessionSummary describes one agent session for the briefing.
type AgentSessionSummary struct {
	Source           string
	SessionID        string
	UserPromptCount  int
	ToolCallCount    int
	SubstantiveCount int
	FirstSeen        string
	LastSeen         string
	UserPrompts      []string // first few user prompts for context
}

// RecentAgentActivity returns per-session summaries grouped by source,
// limited to sessions with activity since the given ISO timestamp.
func RecentAgentActivity(since string, limit int) ([]AgentSessionSummary, error) {
	if limit <= 0 {
		limit = 10
	}
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Get active sessions since the cutoff. Only include sessions
	// with at least one user message (real conversations).
	rows, err := db.Query(`
		SELECT source, session_id,
			SUM(CASE WHEN role = 'user' THEN 1 ELSE 0 END) AS users,
			SUM(CASE WHEN `+substantiveDiscussionSQL()+` AND role != 'user' THEN 1 ELSE 0 END) AS substantive,
			`+toolCallCountSQL()+` AS tools,
			MIN(created_at) AS first_seen,
			MAX(created_at) AS last_seen
		FROM discussion_log
		WHERE created_at >= ? AND source != ''
		GROUP BY source, session_id
		HAVING users > 0
		ORDER BY last_seen DESC
		LIMIT ?`,
		since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []AgentSessionSummary
	for rows.Next() {
		var s AgentSessionSummary
		if err := rows.Scan(&s.Source, &s.SessionID, &s.UserPromptCount, &s.SubstantiveCount, &s.ToolCallCount, &s.FirstSeen, &s.LastSeen); err != nil {
			continue
		}

		// Fetch up to 3 user prompts for context.
		prompts, _ := recentUserPrompts(s.SessionID, 3)
		s.UserPrompts = prompts

		result = append(result, s)
	}
	if result == nil {
		result = []AgentSessionSummary{}
	}
	return result, nil
}

// recentUserPrompts returns the most recent user prompts for a session.
func recentUserPrompts(sessionID string, limit int) ([]string, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(
		`SELECT content FROM discussion_log
		 WHERE session_id = ? AND role = 'user'
		 ORDER BY created_at DESC LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prompts []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err == nil {
			prompts = append(prompts, content)
		}
	}
	if prompts == nil {
		prompts = []string{}
	}
	return prompts, nil
}

// ExtractEntityRefsFromSessions scans user messages in the given sessions for entity ID references.
func ExtractEntityRefsFromSessions(sessions []AgentSessionSummary) ([]EntityRef, error) {
	seen := map[string]bool{}
	var refs []EntityRef
	for _, s := range sessions {
		if s.SessionID == "" {
			continue
		}
		text, err := sessionLinkableText(s.SessionID)
		if err != nil || text == "" {
			continue
		}
		for _, m := range entityIDPattern.FindAllStringSubmatch(text, -1) {
			if len(m) < 2 {
				continue
			}
			targetType := strings.ToLower(m[1])
			targetID := m[0]
			key := s.SessionID + "|" + targetType + "|" + targetID
			if seen[key] {
				continue
			}
			seen[key] = true
			refs = append(refs, EntityRef{
				SessionID:  s.SessionID,
				TargetType: targetType,
				TargetID:   targetID,
			})
		}
	}
	if refs == nil {
		refs = []EntityRef{}
	}
	return refs, nil
}

// AutoLinkDiscussions creates links from discussion sessions to referenced entities.
// Returns the number of newly created links.
func AutoLinkDiscussions(sessions []AgentSessionSummary) (int, error) {
	refs, err := ExtractEntityRefsFromSessions(sessions)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, ref := range refs {
		if !EntityExists(ref.TargetType, ref.TargetID) {
			continue
		}
		exists, err := discussionLinkExists(ref.SessionID, ref.TargetType, ref.TargetID)
		if err != nil || exists {
			continue
		}
		if _, err := CreateLink("", discussionLinkSourceType, ref.SessionID, discussionLinkRelation, ref.TargetType, ref.TargetID, "auto-linked from discussion"); err != nil {
			continue
		}
		created++
	}
	return created, nil
}

// EntityExists returns true when the entity ID exists in the PM database.
func EntityExists(entityType, entityID string) bool {
	if entityID == "" {
		return false
	}
	db, err := pmdb.Open()
	if err != nil {
		return false
	}
	defer db.Close()

	var q string
	switch strings.ToLower(entityType) {
	case "task":
		q = "SELECT 1 FROM tasks WHERE id = ? LIMIT 1"
	case "plan":
		q = "SELECT 1 FROM plans WHERE id = ? LIMIT 1"
	case "decision":
		q = "SELECT 1 FROM decisions WHERE id = ? LIMIT 1"
	case "commit":
		q = "SELECT 1 FROM commits WHERE id = ? LIMIT 1"
	case "bug":
		q = "SELECT 1 FROM bugs WHERE id = ? LIMIT 1"
	default:
		return false
	}
	var n int
	if err := db.QueryRow(q, entityID).Scan(&n); err != nil {
		return false
	}
	return n == 1
}

// CommitSummary is a lightweight commit record for B1 context injection.
type CommitSummary struct {
	ID      string
	Title   string
	Files   []string
}

// FindCommitsInWindow returns commits whose created_at falls between start and end (+2h margin).
func FindCommitsInWindow(start, end string) ([]CommitSummary, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(
		"SELECT id, title, files_json FROM commits WHERE datetime(created_at) >= datetime(?) AND datetime(created_at) <= datetime(?, '+2 hours') ORDER BY created_at",
		start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CommitSummary
	for rows.Next() {
		var c CommitSummary
		var filesJSON string
		if err := rows.Scan(&c.ID, &c.Title, &filesJSON); err != nil {
			continue
		}
		c.Files = parseFilesJSON(filesJSON)
		result = append(result, c)
	}
	return result, nil
}

func parseFilesJSON(raw string) []string {
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil
	}
	return arr
}

// FindGitCommitsInWindow runs `git log` in the current directory for commits between start and end.
func FindGitCommitsInWindow(start, end string) ([]CommitSummary, error) {
	cmd := exec.Command("git", "log",
		"--since="+start,
		"--until="+end,
		"--format=%H|%s",
		"--name-only",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseGitLog(string(out)), nil
}

func parseGitLog(raw string) []CommitSummary {
	var result []CommitSummary
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	var current *CommitSummary

	hashPattern := regexp.MustCompile(`^[a-f0-9]{40}\|`)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if hashPattern.MatchString(line) {
			if current != nil {
				result = append(result, *current)
			}
			parts := strings.SplitN(line, "|", 2)
			shortHash := parts[0][:7]
			title := ""
			if len(parts) == 2 {
				title = parts[1]
			}
			current = &CommitSummary{ID: shortHash, Title: title}
		} else if current != nil {
			current.Files = append(current.Files, line)
		}
	}
	if current != nil {
		result = append(result, *current)
	}
	return result
}

func discussionLinkExists(sessionID, targetType, targetID string) (bool, error) {
	db, err := pmdb.Open()
	if err != nil {
		return false, err
	}
	defer db.Close()
	var n int
	err = db.QueryRow(
		`SELECT 1 FROM links
		 WHERE source_type = ? AND source_id = ? AND relation = ? AND target_type = ? AND target_id = ?
		 LIMIT 1`,
		discussionLinkSourceType, sessionID, discussionLinkRelation, targetType, targetID,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func sessionLinkableText(sessionID string) (string, error) {
	db, err := pmdb.Open()
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT content FROM discussion_log
		 WHERE session_id = ? AND `+substantiveDiscussionSQL()+`
		 ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err == nil && content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// LinkedEntityIDsForSession returns entity IDs linked from a discussion session.
func LinkedEntityIDsForSession(sessionID string) ([]string, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT target_type, target_id FROM links
		 WHERE source_type = ? AND source_id = ? AND relation = ?
		 ORDER BY created_at ASC`,
		discussionLinkSourceType, sessionID, discussionLinkRelation,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var targetType, targetID string
		if err := rows.Scan(&targetType, &targetID); err == nil && targetID != "" {
			ids = append(ids, targetID)
		}
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

// HasRecentDiscussionLink returns true if an entity was discussed since the given ISO timestamp.
func HasRecentDiscussionLink(targetType, targetID, since string) bool {
	if targetID == "" {
		return false
	}
	db, err := pmdb.Open()
	if err != nil {
		return false
	}
	defer db.Close()
	q := `SELECT 1 FROM links
		WHERE source_type = ? AND relation = ? AND target_type = ? AND target_id = ?`
	args := []any{discussionLinkSourceType, discussionLinkRelation, targetType, targetID}
	if since != "" {
		q += " AND created_at >= ?"
		args = append(args, since)
	}
	q += " LIMIT 1"
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		return false
	}
	return true
}

// LinkedDiscussionSessions returns discussion sessions linked to an entity.
func LinkedDiscussionSessions(targetType, targetID string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 5
	}
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT source_id, note, created_at FROM links
		 WHERE source_type = ? AND relation = ? AND target_type = ? AND target_id = ?
		 ORDER BY created_at DESC LIMIT ?`,
		discussionLinkSourceType, discussionLinkRelation, targetType, targetID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var sessionID, note, createdAt string
		if err := rows.Scan(&sessionID, &note, &createdAt); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"session_id": sessionID,
			"note":       note,
			"created_at": createdAt,
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

// UpdateDiscussionSessionID writes back a resolved session_id for orphan MCP rows.
func UpdateDiscussionSessionID(id, sessionID string) error {
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("UPDATE discussion_log SET session_id=? WHERE id=? AND (session_id='' OR session_id='unknown')", sessionID, id)
	return err
}

// openOrCurrentDB opens the specified project's pmai.db, or the current project's if projectPath is empty.
func openOrCurrentDB(projectPath string) (*sql.DB, error) {
	if projectPath == "" {
		return pmdb.Open()
	}
	dbPath := filepath.Join(projectPath, ".pmai", "data", "pmai.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("project not found at %s", projectPath)
	}
	return sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
}

// RecentUserMessages returns the most recent N user messages from discussion_log.
func RecentUserMessages(limit int) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(
		`SELECT id, session_id, role, source, content, metadata, created_at
		FROM discussion_log WHERE role = 'user'
		ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var id, sid, role, src, content, metadata, createdAt string
		rows.Scan(&id, &sid, &role, &src, &content, &metadata, &createdAt)
		out = append(out, map[string]any{
			"id": id, "session_id": sid, "role": role,
			"source": src, "content": content,
			"metadata": metadata, "created_at": createdAt,
		})
	}
	return out, nil
}
