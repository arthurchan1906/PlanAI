package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
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
	// T2：metadata 写入前合法性检查——非法 JSON 落 LogShared（8/10）
	// 空串（对话消息本应无 metadata）不计；非空但非 JSON 才告警。
	if metadataJSON != "" && !json.Valid([]byte(metadataJSON)) {
		u.LogShared("HOOK", "metadata_invalid src=%s role=%s", source, role)
	}
	// 事件时点：在重试/spool 之前捕获（重试期可能长达数秒，落盘时点会挪出原窗口）。
	eventAt := u.NowISO()
	// 先补写历史 spool（P0 捕获缺口兜底，bug-20260826-154305-941881）：
	// 上次 DB 锁竞争期落盘的条目在新写入前重放，保证不静默丢失。
	if err := flushDiscussionSpool(); err != nil {
		u.LogShared("HOOK", "spool_flush_err src=%s err=%v", source, err)
	}
	var lastErr error
	// 快速失败（P1 修正，bug-20260826-163617-217e96）：hook 生命周期有限，
	// 15×busy_timeout(15s) 最坏阻塞 ~227s，事件在落 spool 前就被超时杀死。
	// 收敛为 3 次短重试（专用连接 busy_timeout=2s）→ 最坏 ~6s 即落盘兜底。
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*25) * time.Millisecond)
		}
		var result map[string]any
		var err error
		withDiscussionLogLock(func() {
			result, err = logDiscussionOnce(sessionID, role, source, content, metadataJSON, eventAt)
		})
		if err == nil {
			return result, nil
		}
		if !isSQLiteBusy(err) {
			return nil, err
		}
		lastErr = err
	}
	// 重试耗尽仍 BUSY：落盘 spool，宁可延迟补写也不静默丢事件。
	// 返回成功（spooled），hook 侧不再报 write_err、不会被超时杀死丢数据。
	if serr := spoolDiscussionFallback(sessionID, role, source, content, metadataJSON, eventAt, lastErr); serr != nil {
		return nil, fmt.Errorf("discussion spool fallback failed: %v (cause: %v)", serr, lastErr)
	}
	return map[string]any{"status": "spooled"}, nil
}

// openDiscussionDB 讨论写入专用连接：busy_timeout 收敛到 2s。
// 写路径要「快速失败→spool」，不能用全局 15s busy_timeout（长阻塞会被 hook 超时杀死）。
func openDiscussionDB() (*sql.DB, error) {
	dbPath, err := pmdb.FindPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("PMAI database not found: %s — run aipmc init first", dbPath)
	}
	d, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, err
	}
	if err := pmdb.EnsureSchemaIfNeeded(d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
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

// discussionSpoolPath 落盘兜底文件（JSONL，每行一个待补写条目）。
// 与锁文件同目录（runtimeDir/cache），进程重启后仍可补写。
func discussionSpoolPath() string {
	runtimeDir, err := pmdb.RuntimeDir()
	if err != nil || runtimeDir == "" {
		return filepath.Join(os.TempDir(), "aipm-discussion-spool.jsonl")
	}
	cacheDir := filepath.Join(runtimeDir, "cache")
	_ = os.MkdirAll(cacheDir, 0755)
	return filepath.Join(cacheDir, "discussion-spool.jsonl")
}

// spoolEntry 一条待补写的讨论记录（id/created_at 在落盘时生成，补写时原样保留）。
// 注记：落盘生成的是新 id（非原事件 id）——补写行为一条新行，与原事件行 id 不同；
// 当前无引用该 id 的消费方（全文检索按 content 重建），仅作去重与溯源用。
type spoolEntry struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Source    string `json:"source"`
	Content   string `json:"content"`
	Metadata  string `json:"metadata"`
	CreatedAt string `json:"created_at"`
}

// maxSpoolEntries spool 总量上限（Claude C3）：flush 持续失败时防 JSONL 无界膨胀。
// 超过上限丢弃新条目并告警——宁可丢新事件也不让文件无限增长（需人工介入排查锁竞争）。
const maxSpoolEntries = 10000

// countSpoolEntries 统计 spool 文件现有条目数（按行，块读避免大文件全量载入）。
func countSpoolEntries(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	n := 0
	buf := make([]byte, 32*1024)
	for {
		c, err := f.Read(buf)
		n += bytes.Count(buf[:c], []byte{'\n'})
		if err != nil {
			if err == io.EOF {
				break
			}
			return 0, err
		}
	}
	return n, nil
}

// spoolDiscussionFallback 把写失败的事件追加到 spool 文件（防静默丢失）。
func spoolDiscussionFallback(sessionID, role, source, content, metadataJSON, now string, cause error) error {
	path := discussionSpoolPath()
	if n, err := countSpoolEntries(path); err == nil && n >= maxSpoolEntries {
		u.LogShared("HOOK", "discussion spool full (%d >= %d): dropping event session=%s err=%v", n, maxSpoolEntries, sessionID, cause)
		return nil
	}
	e := spoolEntry{
		ID:        u.Slug("disc"),
		SessionID: sessionID,
		Role:      role,
		Source:    source,
		Content:   content,
		Metadata:  metadataJSON,
		CreatedAt: now,
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	u.LogShared("HOOK", "discussion spooled session=%s role=%s err=%v file=%s", sessionID, role, cause, path)
	return nil
}

// FlushDiscussionSpool 把 spool 中待补写条目写回 discussion_log（幂等，可重入）。
// 守护进程启动时调用；LogDiscussion 每次写前也会顺带 flush。
func FlushDiscussionSpool() error {
	return flushDiscussionSpool()
}

// flushDiscussionSpool 尝试补写 spool 条目；仍失败（如锁竞争持续）则保留在文件中。
// 单次最多处理 200 条，避免长阻塞；剩余条目下次继续。
func flushDiscussionSpool() error {
	path := discussionSpoolPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var entries []spoolEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e spoolEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			entries = append(entries, e)
		}
	}
	if len(entries) == 0 {
		return nil
	}
	// 整个 flush（读→补写→重写 spool）在锁内执行，避免多进程并发读改写互相覆盖。
	// 复用单个连接：此前每条目一次 open（每次 open 全量 DDL），锁竞争期被放大到
	// 分钟级（bug-20260826-164859-0643c5）。BUSY 即停，剩余条目下次再试。
	var pending []spoolEntry
	withDiscussionLogLock(func() {
		db, derr := openDiscussionDB()
		if derr != nil {
			pending = append(pending, entries...)
			return
		}
		defer db.Close()
		for i, e := range entries {
			if i >= 200 {
				pending = append(pending, entries[i:]...)
				break
			}
			_, ierr := db.Exec("INSERT INTO discussion_log (id, session_id, role, source, content, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
				e.ID, e.SessionID, e.Role, e.Source, e.Content, e.Metadata, e.CreatedAt)
			if ierr == nil {
				_ = pmdb.SyncFTS5Entity(db, "discussion", e.ID, "["+e.Role+"]["+e.Source+"] "+previewSpool(e.Content), e.Content)
			} else if isSQLiteBusy(ierr) {
				// 锁竞争持续：保留剩余全部，下次再试
				pending = append(pending, entries[i:]...)
				break
			} else if isUniqueConstraint(ierr) {
				// UNIQUE 冲突 = 该条目已补写成功（并发 flush 双写）——按已写跳过
			} else {
				// 非 BUSY/非冲突错误：保留单条，避免死循环
				pending = append(pending, e)
			}
		}
	})
	return rewriteDiscussionSpool(path, pending)
}

// isUniqueConstraint SQLite UNIQUE 冲突（SQLITE_CONSTRAINT 19）。
func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

// rewriteDiscussionSpool 原子重写 spool 文件（写临时文件再 rename）。
func rewriteDiscussionSpool(path string, pending []spoolEntry) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	for _, e := range pending {
		line, err := json.Marshal(e)
		if err != nil {
			continue
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if len(pending) == 0 {
		return os.Remove(path)
	}
	return os.Rename(tmp, path)
}

func previewSpool(s string) string {
	if len([]rune(s)) > 80 {
		return string([]rune(s)[:80])
	}
	return s
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

func logDiscussionOnce(sessionID, role, source, content, metadataJSON, now string) (map[string]any, error) {
	db, err := openDiscussionDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("disc")
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
	// Auto-register "what this agent is working on": every user message
	// refreshes the session's current status (last prompt wins).
	if role == "user" && sid != "" && sid != "unknown" && isSubstantiveStatusPrompt(content) {
		_ = touchAgentStatus(db, source, sid, content, false)
	}
	preview := content
	if len([]rune(preview)) > 80 {
		preview = string([]rune(preview)[:80])
	}
	_ = pmdb.SyncFTS5Entity(db, "discussion", id, "["+role+"]["+source+"] "+preview, content)
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

// touchAgentStatus upserts the session's current status. Auto updates (from
// user prompts) never overwrite an explicit declaration; explicit updates
// (aipm_update_status) always win.
func touchAgentStatus(db *sql.DB, source, sessionID, status string, explicit bool) error {
	if len([]rune(status)) > 500 {
		status = string([]rune(status)[:500])
	}
	var q string
	if explicit {
		q = `INSERT INTO agent_status (session_id, source, status, updated_at, explicit) VALUES (?, ?, ?, ?, 1)
			ON CONFLICT(session_id) DO UPDATE SET source=excluded.source, status=excluded.status, updated_at=excluded.updated_at, explicit=1`
	} else {
		q = `INSERT INTO agent_status (session_id, source, status, updated_at, explicit) VALUES (?, ?, ?, ?, 0)
			ON CONFLICT(session_id) DO UPDATE SET source=excluded.source, status=excluded.status, updated_at=excluded.updated_at
			WHERE agent_status.explicit = 0`
	}
	_, err := db.Exec(q, sessionID, source, status, u.NowISO())
	return err
}

// isSubstantiveStatusPrompt filters trivial continuation messages ("继续",
// "ok") so they do not clobber a meaningful "what I am working on" status.
func isSubstantiveStatusPrompt(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if len([]rune(s)) < 4 {
		return false
	}
	switch strings.ToLower(s) {
	case "ok", "ok.", "好的", "好", "继续", "继续修", "继续改", "go", "go.", "continue", "y", "yes", "嗯", "恩":
		return false
	}
	return true
}

// UpdateAgentStatus lets an agent explicitly declare what it is working on.
// An empty projectPath resolves to the cwd project. Busy errors are retried
// like LogDiscussion so concurrent hook/MCP processes do not drop updates.
func UpdateAgentStatus(source, sessionID, status, projectPath string) error {
	if sessionID == "" || sessionID == "unknown" {
		return fmt.Errorf("session_id 不能为空")
	}
	if status == "" {
		status = "(空闲)"
	}
	var lastErr error
	for attempt := 0; attempt < 15; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*25) * time.Millisecond)
		}
		withDiscussionLogLock(func() {
			db, err := openOrCurrentDB(projectPath)
			if err != nil {
				lastErr = err
				return
			}
			defer db.Close()
			lastErr = touchAgentStatus(db, source, sessionID, status, true)
		})
		if lastErr == nil {
			return nil
		}
		if !isSQLiteBusy(lastErr) {
			return lastErr
		}
	}
	return lastErr
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
// since (ISO8601) restricts results to created_at >= since.
func ListRecentDiscussions(source, typeFilter, sessionID, projectPath, since string, lastN int, cursor string) ([]map[string]any, error) {
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
	if sessionID != "" {
		where += " AND session_id = ?"
		args = append(args, sessionID)
	}
	if since != "" {
		where += " AND created_at >= ?"
		args = append(args, since)
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
	return GetSessionMessagesFor("", sessionID)
}

// GetSessionMessagesFor reads a session's messages from a specific project's
// database; empty projectPath resolves to the cwd project.
func GetSessionMessagesFor(projectPath, sessionID string) ([]map[string]any, error) {
	db, err := pmdb.OpenProject(projectPath)
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
	SessionID   string
	ID          string // 按消息 ID 展开单条全文（B7：预览中的 disc-xxx 线索）
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
	if opts.SessionID != "" {
		where += " AND session_id = ?"
		args = append(args, opts.SessionID)
	}
	if opts.ID != "" {
		where += " AND id = ?"
		args = append(args, opts.ID)
	}
	if opts.Since != "" {
		where += " AND created_at >= ?"
		args = append(args, opts.Since)
	}

	limit := opts.LastN
	if limit <= 0 {
		limit = 15
	}
	if opts.ID != "" {
		limit = 1
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
	return RecentAgentActivityFor("", since, limit)
}

// RecentAgentActivityFor reads active sessions from a specific project's
// database; empty projectPath resolves to the cwd project.
func RecentAgentActivityFor(projectPath, since string, limit int) ([]AgentSessionSummary, error) {
	if limit <= 0 {
		limit = 10
	}
	db, err := pmdb.OpenProject(projectPath)
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
		prompts, err := recentUserPrompts(projectPath, s.SessionID, 3)
		if err != nil {
			return nil, err
		}
		s.UserPrompts = prompts

		result = append(result, s)
	}
	if result == nil {
		result = []AgentSessionSummary{}
	}
	return result, nil
}

// recentUserPrompts returns the most recent user prompts for a session in the
// given project (empty projectPath resolves to the cwd project).
func recentUserPrompts(projectPath, sessionID string, limit int) ([]string, error) {
	db, err := openOrCurrentDB(projectPath)
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

// AgentStatusRow describes one agent session for the public status board:
// what the agent is working on (latest user prompt or explicit status) plus
// recent activity counts.
type AgentStatusRow struct {
	Source           string
	SessionID        string
	Status           string
	StatusUpdatedAt  string
	Explicit         bool // true = declared via update_status; false = auto-registered prompt
	UserPromptCount  int
	ToolCallCount    int
	SubstantiveCount int
	FirstSeen        string
	LastSeen         string
	UserPrompts      []string
}

// CountExplicitStatuses returns (explicit, total) counts of agent_status
// rows. explicit counts sessions whose status was declared via aipm_update_status
// (never auto-overwritten); total counts all registered sessions, auto or
// explicit. Used by `aipm metrics` to track L1 "agent declares what it is
// doing" adoption. An empty projectPath resolves to the cwd project.
func CountExplicitStatuses(projectPath string) (explicit, total int, err error) {
	db, err := openOrCurrentDB(projectPath)
	if err != nil {
		return 0, 0, err
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(explicit), 0) FROM agent_status`).Scan(&total, &explicit); err != nil {
		return 0, 0, err
	}
	return explicit, total, nil
}

// ListActiveSessions returns sessions with activity since the cutoff, joined
// with their registered current status (agent_status). This is the public
// "who is doing what right now" query that lets an agent tell apart
// same-source peers (e.g. multiple codex processes on one project).
func ListActiveSessions(projectPath, source, since string, limit int) ([]AgentStatusRow, error) {
	if limit <= 0 {
		limit = 10
	}
	db, err := openOrCurrentDB(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	q := `SELECT s.source, s.session_id, s.users, s.substantive, s.tools, s.first_seen, s.last_seen,
		COALESCE(a.status, ''), COALESCE(a.updated_at, ''), COALESCE(a.explicit, 0)
	FROM (
		SELECT source, session_id,
			SUM(CASE WHEN role = 'user' THEN 1 ELSE 0 END) AS users,
			SUM(CASE WHEN ` + substantiveDiscussionSQL() + ` AND role != 'user' THEN 1 ELSE 0 END) AS substantive,
			` + toolCallCountSQL() + ` AS tools,
			MIN(created_at) AS first_seen,
			MAX(created_at) AS last_seen
		FROM discussion_log
		WHERE created_at >= ? AND source != ''
		GROUP BY source, session_id
		HAVING users > 0
	) s
	LEFT JOIN agent_status a ON a.session_id = s.session_id
	WHERE 1=1`
	var args []any
	args = append(args, since)
	if source != "" {
		q += " AND s.source = ?"
		args = append(args, source)
	}
	q += " ORDER BY s.last_seen DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []AgentStatusRow
	for rows.Next() {
		var s AgentStatusRow
		var explicit int
		if err := rows.Scan(&s.Source, &s.SessionID, &s.UserPromptCount, &s.SubstantiveCount,
			&s.ToolCallCount, &s.FirstSeen, &s.LastSeen, &s.Status, &s.StatusUpdatedAt, &explicit); err != nil {
			return nil, err
		}
		s.Explicit = explicit != 0
		prompts, err := recentUserPromptsFor(projectPath, s.SessionID, s.Source, 2)
		if err != nil {
			return nil, err
		}
		s.UserPrompts = prompts
		result = append(result, s)
	}
	if result == nil {
		result = []AgentStatusRow{}
	}
	return result, nil
}

// recentUserPromptsFor returns the most recent user prompts for a session in
// the given project and source (used by ListActiveSessions).
func recentUserPromptsFor(projectPath, sessionID, source string, limit int) ([]string, error) {
	db, err := openOrCurrentDB(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(
		`SELECT content FROM discussion_log
		 WHERE session_id = ? AND source = ? AND role = 'user'
		 ORDER BY created_at DESC LIMIT ?`,
		sessionID, source, limit,
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
	return EntityExistsFor("", entityType, entityID)
}

// EntityExistsFor checks entity existence in a specific project's database;
// empty projectPath resolves to the cwd project.
func EntityExistsFor(projectPath, entityType, entityID string) bool {
	if entityID == "" {
		return false
	}
	db, err := pmdb.OpenProject(projectPath)
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
	ID    string
	Title string
	Files []string
}

// FindCommitsInWindow returns commits whose created_at falls between start and end (+2h margin).
func FindCommitsInWindow(start, end string) ([]CommitSummary, error) {
	return FindCommitsInWindowFor("", start, end)
}

// FindCommitsInWindowFor reads commits from a specific project's database;
// empty projectPath resolves to the cwd project.
func FindCommitsInWindowFor(projectPath, start, end string) ([]CommitSummary, error) {
	db, err := pmdb.OpenProject(projectPath)
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
	return FindGitCommitsInWindowFor("", start, end)
}

// FindGitCommitsInWindowFor runs `git log` inside the given project directory
// (no cwd mutation); empty projectPath uses the cwd.
func FindGitCommitsInWindowFor(projectPath, start, end string) ([]CommitSummary, error) {
	cmd := exec.Command("git", "log",
		"--since="+start,
		"--until="+end,
		"--format=%H|%s",
		"--name-only",
	)
	cmd.Dir = projectPath
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
	return UpdateDiscussionSessionIDFor("", id, sessionID)
}

// UpdateDiscussionSessionIDFor writes back a resolved session_id in a specific
// project's database; empty projectPath resolves to the cwd project.
func UpdateDiscussionSessionIDFor(projectPath, id, sessionID string) error {
	db, err := pmdb.OpenProject(projectPath)
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
	return sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
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
