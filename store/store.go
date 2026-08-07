package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	pmdb "aipmc/db"
	"aipmc/u"
)

// retryOnBusy wraps a function with automatic retry on SQLITE_BUSY errors.
// Uses exponential backoff: 100ms, 200ms, 400ms.
func retryOnBusy(fn func() error) error {
	var err error
	for i := 0; i < 3; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "SQLITE_BUSY") && !strings.Contains(err.Error(), "database is locked") {
			return err
		}
		time.Sleep(time.Duration(1<<uint(i)) * 100 * time.Millisecond)
	}
	return fmt.Errorf("still busy after 3 retries: %w", err)
}

// ============================================================
// Tasks
// ============================================================

type Task struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Status           string `json:"status"`
	Priority         string `json:"priority"`
	Phase            string `json:"phase"`
	RoadmapID        string `json:"roadmap_id"`
	PlanID           string `json:"plan_id"`
	Acceptance       []any  `json:"acceptance"`
	Progress         int    `json:"progress"`
	RelatedDocs      []any  `json:"related_docs"`
	RelatedDecisions []any  `json:"related_decisions"`
	LastNote         string `json:"last_note"`
	UpdatedAt        string `json:"updated_at"`
	CreatedAt        string `json:"created_at"`
}

func ListTasks(status, planID string) ([]Task, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT * FROM tasks"
	var args []any
	var clauses []string
	if status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
	}
	if planID != "" {
		clauses = append(clauses, "plan_id = ?")
		args = append(args, planID)
	}
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY CASE status WHEN 'in_progress' THEN 0 WHEN 'todo' THEN 1 WHEN 'blocked' THEN 2 ELSE 3 END, priority, updated_at DESC"
	return ScanTasks(db.Query(q, args...))
}

func GetTaskSimple(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	task := map[string]any{}
	row := db.QueryRow("SELECT * FROM tasks WHERE id = ?", id)
	if err := ScanTaskRow(row, task); err != nil {
		return nil, err
	}
	return task, nil
}

func CreateTask(projectPath string, title, priority, status, phase, planID string, acceptance []string) (map[string]any, error) {
	if planID == "" {
		return nil, fmt.Errorf("task requires --plan-id. Find a plan: aipmc plan list")
	}
	db, err := pmdb.OpenProject(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("task")
	now := u.NowISO()
	accJSON := "[]"
	if len(acceptance) > 0 {
		accJSON = u.JsonStr(acceptance)
	}
	// Validate plan exists and backfill roadmap_id
	var roadmapID string
	if err := db.QueryRow("SELECT roadmap_id FROM plans WHERE id = ?", planID).Scan(&roadmapID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("plan '%s' not found: task requires a valid plan_id", planID)
		}
		// Surface real failures (lock etc.) instead of misreporting as
		// "plan not found" (bug-20260805-134225-085427).
		return nil, fmt.Errorf("plan '%s' lookup failed: %w", planID, err)
	}
	_, err = db.Exec("INSERT INTO tasks (id, title, status, priority, phase, plan_id, acceptance_json, related_docs_json, related_decisions_json, last_note, updated_at, roadmap_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, title, status, priority, phase, planID, accJSON, "[]", "[]", "", u.Today(), roadmapID, now)
	if err != nil {
		return nil, err
	}
	planIDContent := title
	if planID != "" {
		planIDContent += " " + planID
	}
	pmdb.SyncFTS5Entity(db, "task", id, title, planIDContent)

	// Auto-create event
	CreateEvent("task_created", "task", id, fmt.Sprintf("New task: %s", title))
	var taskIDsJSON string
	if err := db.QueryRow("SELECT task_ids_json FROM plans WHERE id = ?", planID).Scan(&taskIDsJSON); err == nil {
		var ids []string
		json.Unmarshal([]byte(taskIDsJSON), &ids)
		ids = append(ids, id)
		newJSON, _ := json.Marshal(ids)
		db.Exec("UPDATE plans SET task_ids_json = ?, updated_at = ? WHERE id = ?", string(newJSON), u.NowISO(), planID)
	}
	return GetTaskSimple(id)
}

func UpdateTask(projectPath string, id, status, note string, allowWithoutCommit, appendNote bool) (map[string]any, error) {
	db, err := pmdb.OpenProject(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT * FROM tasks WHERE id = ?", id)
	existing := map[string]any{}
	if err := ScanTaskRow(row, existing); err != nil {
		return nil, err
	}
	oldStatus, _ := existing["status"].(string)
	if status == "" {
		status = oldStatus
	}
	if status != oldStatus {
		db.Exec("INSERT INTO task_notes (id, task_id, content, mode, created_at) VALUES (?, ?, ?, ?, ?)", u.Slug("task-note"), id, fmt.Sprintf("Status changed from %s to %s", oldStatus, status), "system", u.NowISO())
	}
	if status == "done" && oldStatus != "done" && !allowWithoutCommit {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM commits WHERE task_id = ? AND status IN ('committed','merged') AND review_status IN ('approved','auto') AND test_status IN ('passed','auto')", id).Scan(&count)
		if count > 0 {
			u.LogShared("DONE-GATE", "pass task=%s commits=%d", id[:min(len(id), 12)], count)
		}
		if count == 0 {
			return nil, fmt.Errorf("task cannot be marked done without at least one verified approved commit")
		}
	}
	nextNote, _ := existing["last_note"].(string)
	if note != "" {
		if appendNote && nextNote != "" {
			nextNote = strings.TrimRight(nextNote, "\n") + "\n\n" + note
		} else {
			nextNote = note
		}
	}
	_, err = db.Exec("UPDATE tasks SET status = ?, last_note = ?, updated_at = ? WHERE id = ?", status, nextNote, u.Today(), id)
	if err != nil {
		return nil, err
	}
	if status == "done" {
		// 2.3: a done task resolves its stale-file events (processed vs consumed).
		MarkEventProcessed("task_stale_file", id)
	}
	title := u.Str(existing["title"])
	planID := u.Str(existing["plan_id"])
	content := title + " " + nextNote
	if planID != "" {
		content += " " + planID
	}
	pmdb.SyncFTS5Entity(db, "task", id, title, content)
	if note != "" {
		mode := "replace"
		if appendNote {
			mode = "append"
		}
		db.Exec("INSERT INTO task_notes (id, task_id, content, mode, created_at) VALUES (?, ?, ?, ?, ?)", u.Slug("task-note"), id, note, mode, u.NowISO())
	}
	return GetTaskSimple(id)
}

func AppendTaskNote(projectPath string, taskID, content string) (map[string]any, error) {
	db, err := pmdb.OpenProject(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT * FROM tasks WHERE id = ?", taskID)
	existing := map[string]any{}
	if err := ScanTaskRow(row, existing); err != nil {
		return nil, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("note content cannot be empty")
	}
	nextNote := content
	if ln, _ := existing["last_note"].(string); ln != "" {
		nextNote = strings.TrimRight(ln, "\n") + "\n\n" + content
	}
	db.Exec("INSERT INTO task_notes (id, task_id, content, mode, created_at) VALUES (?, ?, ?, ?, ?)", u.Slug("task-note"), taskID, content, "append", u.NowISO())
	db.Exec("UPDATE tasks SET last_note = ?, updated_at = ? WHERE id = ?", nextNote, u.Today(), taskID)
	return GetTask(taskID)
}

func ListTaskNotes(taskID string, limit int) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT id, task_id, content, mode, created_at FROM task_notes WHERE task_id = ? ORDER BY created_at DESC, id DESC LIMIT ?", taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notes []map[string]any
	for rows.Next() {
		var id, tid, content, mode, createdAt string
		rows.Scan(&id, &tid, &content, &mode, &createdAt)
		notes = append(notes, map[string]any{"id": id, "task_id": tid, "content": content, "mode": mode, "created_at": createdAt})
	}
	if notes == nil {
		notes = []map[string]any{}
	}
	return notes, nil
}

func PlanTask(taskID string, steps []string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	items := make([]map[string]any, len(steps))
	for i, s := range steps {
		items[i] = map[string]any{"text": s, "done": false}
	}
	db.Exec("UPDATE tasks SET acceptance_json = ?, updated_at = ? WHERE id = ?", u.JsonStr(items), u.Today(), taskID)
	return GetTaskSimple(taskID)
}

func UpdateTaskCheckpoint(taskID string, index int, done bool) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT * FROM tasks WHERE id = ?", taskID)
	existing := map[string]any{}
	if err := ScanTaskRow(row, existing); err != nil {
		return nil, err
	}
	var acc []map[string]any
	accJSON, _ := existing["acceptance_json"].(string)
	json.Unmarshal([]byte(accJSON), &acc)
	if index < 0 || index >= len(acc) {
		return nil, fmt.Errorf("checkpoint index %d out of range", index)
	}
	if acc[index] == nil {
		acc[index] = map[string]any{}
	}
	acc[index]["done"] = done
	db.Exec("UPDATE tasks SET acceptance_json = ?, updated_at = ? WHERE id = ?", u.JsonStr(acc), u.Today(), taskID)
	return GetTaskSimple(taskID)
}

// ============================================================
// Commits
// ============================================================

func ListCommits(status, taskID, decisionID, since string, limit int) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at FROM commits"
	var args []any
	var clauses []string
	if status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
	}
	if taskID != "" {
		clauses = append(clauses, "task_id = ?")
		args = append(args, taskID)
	}
	if decisionID != "" {
		clauses = append(clauses, "decision_id = ?")
		args = append(args, decisionID)
	}
	if since != "" {
		if since == "today" {
			since = u.Today()
		}
		clauses = append(clauses, "created_at >= ?")
		args = append(args, since)
	}
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY created_at DESC, id DESC"
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanCommitRows(rows)
}

func ListCommitsByTask(taskID string) ([]map[string]any, error) {
	return ListCommits("", taskID, "", "", 0)
}

// ListCommitsWithOffset extends ListCommits with offset for pagination.
func ListCommitsWithOffset(status, taskID, decisionID, since string, limit, offset int) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at FROM commits"
	var args []any
	var clauses []string
	if status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
	}
	if taskID != "" {
		clauses = append(clauses, "task_id = ?")
		args = append(args, taskID)
	}
	if decisionID != "" {
		clauses = append(clauses, "decision_id = ?")
		args = append(args, decisionID)
	}
	if since != "" {
		if since == "today" {
			since = u.Today()
		}
		clauses = append(clauses, "created_at >= ?")
		args = append(args, since)
	}
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY created_at DESC, id DESC"
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		q += " OFFSET ?"
		args = append(args, offset)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanCommitRows(rows)
}

// ListOrphanCommits returns commits that have no task_id (orphan commits).
func ListOrphanCommits(limit, offset int) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if limit <= 0 {
		limit = 50
	}
	q := "SELECT id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at FROM commits WHERE task_id IS NULL OR task_id = '' ORDER BY created_at DESC, id DESC LIMIT ?"
	args := []any{limit}
	if offset > 0 {
		q += " OFFSET ?"
		args = append(args, offset)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanCommitRows(rows)
}

func GetCommit(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	c := map[string]any{}
	row := db.QueryRow("SELECT id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at FROM commits WHERE id = ?", id)
	if err := ScanCommitRow(row, c); err != nil {
		return nil, err
	}
	if tid, _ := c["task_id"].(string); tid != "" {
		var id2, title, status, priority, phase string
		if err := db.QueryRow("SELECT id, title, status, priority, phase FROM tasks WHERE id = ?", tid).Scan(&id2, &title, &status, &priority, &phase); err == nil {
			c["linked_task"] = map[string]any{"id": id2, "title": title, "status": status, "priority": priority, "phase": phase}
		}
	}
	if did, _ := c["decision_id"].(string); did != "" {
		var id2, title, status, date string
		if err := db.QueryRow("SELECT id, title, status, date FROM decisions WHERE id = ?", did).Scan(&id2, &title, &status, &date); err == nil {
			c["linked_decision"] = map[string]any{"id": id2, "title": title, "status": status, "date": date}
		}
	}
	return c, nil
}

func CreateCommit(projectPath string, title, summary, evidenceSummary, reviewNotes, branch, commitHash, taskID, decisionID, status, testStatus, reviewStatus string, files []string) (map[string]any, error) {
	if taskID == "" {
		return nil, fmt.Errorf("commit requires --task-id (or --task-ids for multi-task). Find a task: aipmc task list --status in_progress")
	}
	db, err := pmdb.OpenProject(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var _x int
	if err := db.QueryRow("SELECT 1 FROM tasks WHERE id = ?", taskID).Scan(&_x); err != nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}
	id := u.Slug("commit")
	now := u.NowISO()
	filesJSON := "[]"
	if len(files) > 0 {
		filesJSON = u.JsonStr(files)
	}
	err = retryOnBusy(func() error {
		_, derr := db.Exec("INSERT INTO commits (id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, title, summary, evidenceSummary, reviewNotes, branch, commitHash, taskID, decisionID, status, testStatus, reviewStatus, filesJSON, now, now)
		return derr
	})
	if err != nil {
		return nil, err
	}
	if taskID != "" {
		// 2.3: a commit recorded with a task resolves any prior orphan event.
		MarkEventProcessed("commit_orphan", id)
	}
	commitContent := title + " " + summary
	if evidenceSummary != "" {
		commitContent += " " + evidenceSummary
	}
	if reviewNotes != "" {
		commitContent += " " + reviewNotes
	}
	if filesJSON != "" && filesJSON != "[]" {
		commitContent += " " + filesJSON
	}
	pmdb.SyncFTS5Entity(db, "commit", id, title, commitContent)
	c, _ := GetCommit(id)
	return c, nil
}

// BatchCreateCommits creates multiple commits in a single transaction.
// Each commit calls SyncFTS5Entity for search indexing.
// Uses "best-effort" strategy: individual failures do not block the batch.
type BatchCommitItem struct {
	Title      string   `json:"title"`
	CommitHash string   `json:"commit_hash"`
	Files      []string `json:"files"`
	Summary    string   `json:"summary"`
}

type BatchRecordResult struct {
	Total   int               `json:"total"`
	Success int               `json:"success"`
	Failed  int               `json:"failed"`
	Details []BatchRecordItem `json:"details"`
}

type BatchRecordItem struct {
	Index   int    `json:"index"`
	Success bool   `json:"success"`
	ID      string `json:"id,omitempty"`
	Error   string `json:"error,omitempty"`
}

func BatchCreateCommits(projectPath, taskID, branch, status, testStatus, reviewStatus string, items []BatchCommitItem) (*BatchRecordResult, error) {
	db, err := pmdb.OpenProject(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Validate task exists
	var _x int
	if err := db.QueryRow("SELECT 1 FROM tasks WHERE id = ?", taskID).Scan(&_x); err != nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	result := &BatchRecordResult{Total: len(items), Details: make([]BatchRecordItem, len(items))}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	now := u.NowISO()
	for i, item := range items {
		id := u.Slug("commit")
		filesJSON := "[]"
		if len(item.Files) > 0 {
			filesJSON = u.JsonStr(item.Files)
		}
		_, err := tx.Exec("INSERT INTO commits (id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			id, item.Title, item.Summary, "", "", branch, item.CommitHash, taskID, "", status, testStatus, reviewStatus, filesJSON, now, now)
		if err != nil {
			result.Failed++
			result.Details[i] = BatchRecordItem{Index: i, Success: false, Error: err.Error()}
			continue
		}
		// FTS5 indexing
		commitContent := item.Title + " " + item.Summary + " " + filesJSON
		pmdb.SyncFTS5Entity(db, "commit", id, item.Title, commitContent)
		result.Success++
		result.Details[i] = BatchRecordItem{Index: i, Success: true, ID: id}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return result, nil
}

// StoreGitCommit creates or updates a commit entry from git data.
// Unlike CreateCommit, it does not require a task_id — suitable for
// git-log-synced commits that haven't been task-associated yet.
func StoreGitCommit(projectPath, title, commitHash, date string, files []string) (map[string]any, error) {
	db, err := pmdb.OpenProject(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Check if commit_hash already exists. Empty/NULL hashes (e.g. commits
	// recorded via MCP without --commit-hash) must NOT match here — a bare
	// `? LIKE commit_hash || '%'` turns `'' || '%'` into `'%'`, which matches
	// every row, silently merging new hook-recorded commits into old rows
	// (data loss: hook reported success but no row was created).
	existingID := findExistingCommitByHash(db, commitHash)

	filesJSON := "[]"
	if len(files) > 0 {
		filesJSON = u.JsonStr(files)
	}

	if existingID != "" {
		// Update existing — backfill files if empty
		var existingFiles string
		db.QueryRow("SELECT files_json FROM commits WHERE id = ?", existingID).Scan(&existingFiles)
		if existingFiles == "" || existingFiles == "[]" || existingFiles == "null" {
			if _, err := db.Exec("UPDATE commits SET files_json = ?, commit_hash = ?, updated_at = ? WHERE id = ?",
				filesJSON, commitHash, u.NowISO(), existingID); err != nil {
				return nil, err
			}
		}
		return GetCommit(existingID)
	}

	// Insert new
	id := u.Slug("commit")
	now := u.NowISO()
	_, err = db.Exec("INSERT INTO commits (id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, title, "", "", "", "", commitHash, "", "", "committed", "auto", "auto", filesJSON, date, now)
	if err != nil {
		return nil, err
	}
	return GetCommit(id)
}

// findExistingCommitByHash returns the id of a commit whose stored hash is a
// prefix of commitHash. Empty/NULL stored hashes never match: `'' || '%'`
// would otherwise match every row and swallow new commits into old rows.
func findExistingCommitByHash(db *sql.DB, commitHash string) string {
	var existingID string
	db.QueryRow("SELECT id FROM commits WHERE commit_hash IS NOT NULL AND commit_hash != '' AND ? LIKE commit_hash || '%' LIMIT 1", commitHash).Scan(&existingID)
	return existingID
}

func UpdateCommit(id string, payload map[string]any) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at FROM commits WHERE id = ?", id)
	existing := map[string]any{}
	if err := ScanCommitRow(row, existing); err != nil {
		return nil, err
	}
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := MapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return existing, nil
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, u.NowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE commits SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	if tid, _ := payload["task_id"].(string); tid != "" {
		// 2.3: binding an orphan commit to a task resolves its orphan event.
		MarkEventProcessed("commit_orphan", id)
	}
	return existing, nil
}

// ============================================================
// Plans
// ============================================================

func ListPlans(roadmapID, status string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT * FROM plans"
	var args []any
	var clauses []string
	if roadmapID != "" {
		clauses = append(clauses, "roadmap_id = ?")
		args = append(args, roadmapID)
	}
	if status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
	}
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'draft' THEN 1 ELSE 2 END, priority, updated_at DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanPlanRows(rows)
}

func GetPlan(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	p := map[string]any{}
	row := db.QueryRow("SELECT * FROM plans WHERE id = ?", id)
	if err := ScanPlanRow(row, p); err != nil {
		return nil, err
	}
	return p, nil
}

func CreatePlan(title, goal, roadmapID, visionID, priority, status string, scope, risks, assumptions, taskIDs []string) (map[string]any, error) {
	if roadmapID == "" {
		return nil, fmt.Errorf("plan requires --roadmap-id. Find a roadmap: aipmc roadmap list")
	}
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("plan")
	now := u.NowISO()
	_, err = db.Exec("INSERT INTO plans (id, roadmap_id, vision_id, title, goal, status, priority, scope_json, risks_json, assumptions_json, task_ids_json, source, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, roadmapID, visionID, title, goal, status, priority, u.JsonStr(scope), u.JsonStr(risks), u.JsonStr(assumptions), u.JsonStr(taskIDs), "manual", now, now)
	if err != nil {
		return nil, err
	}
	// Auto-create event for PM tracking
	pmdb.SyncFTS5Entity(db, "plan", id, title, title+" "+goal)
	CreateEvent("plan_created", "plan", id, fmt.Sprintf("New plan created: %s", title))
	return GetPlan(id)
}

func UpdatePlan(id string, payload map[string]any) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := MapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return GetPlan(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, u.NowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE plans SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return GetPlan(id)
}

// ============================================================
// Bugs
// ============================================================

func ListBugs(status, severity, commitID string, limit int) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT * FROM bugs"
	var args []any
	var clauses []string
	if status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
	}
	if severity != "" {
		clauses = append(clauses, "severity = ?")
		args = append(args, severity)
	}
	if commitID != "" {
		clauses = append(clauses, "commit_id = ?")
		args = append(args, commitID)
	}
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY created_at DESC, id DESC"
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanBugRows(rows)
}

func GetBug(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	b := map[string]any{}
	row := db.QueryRow("SELECT * FROM bugs WHERE id = ?", id)
	if err := ScanBugRow(row, b); err != nil {
		return nil, err
	}
	if cid, _ := b["commit_id"].(string); cid != "" {
		var id2, title, status, chash string
		if err := db.QueryRow("SELECT id, title, status, commit_hash FROM commits WHERE id = ?", cid).Scan(&id2, &title, &status, &chash); err == nil {
			b["linked_commit"] = map[string]any{"id": id2, "title": title, "status": status, "commit_hash": chash}
		}
	}
	return b, nil
}

func CreateBug(projectPath string, title, description, severity, status, commitID, errMsg, files, rootCause, fix, tags string) (map[string]any, error) {
	db, err := pmdb.OpenProject(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if commitID != "" {
		var _x int
		if err := db.QueryRow("SELECT 1 FROM commits WHERE id = ?", commitID).Scan(&_x); err != nil {
			return nil, fmt.Errorf("commit not found: %s", commitID)
		}
	}
	id := u.Slug("bug")
	now := u.NowISO()
	_, err = db.Exec("INSERT INTO bugs (id, title, description, severity, status, commit_id, error, files, root_cause, fix, tags, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, title, description, severity, status, commitID, errMsg, files, rootCause, fix, tags, now, now)
	if err != nil {
		return nil, err
	}
	pmdb.SyncFTS5Entity(db, "bug", id, title, title+" "+description+" "+errMsg)
	return GetBug(id)
}

func UpdateBug(id string, payload map[string]any) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if clear, _ := payload["clear_commit_id"].(bool); clear {
		payload["commit_id"] = nil
	}
	delete(payload, "clear_commit_id")
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := MapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return GetBug(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, u.NowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE bugs SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return GetBug(id)
}

// ============================================================
// Decisions
// ============================================================

func ListDecisions() ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT * FROM decisions ORDER BY date DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanDecisionRows(rows)
}

func GetDecision(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	d := map[string]any{}
	row := db.QueryRow("SELECT * FROM decisions WHERE id = ?", id)
	if err := ScanDecisionRow(row, d); err != nil {
		return nil, err
	}
	return d, nil
}

func CreateDecision(projectPath string, title, background, decision, status string) (map[string]any, error) {
	db, err := pmdb.OpenProject(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("decision")
	_, err = db.Exec("INSERT INTO decisions (id, title, date, status, background, decision_text, impact_json, alternatives_json, related_tasks_json, updates_canon) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, title, u.Today(), status, background, decision, "[]", "[]", "[]", 0)
	if err != nil {
		return nil, err
	}
		pmdb.SyncFTS5Entity(db, "decision", id, title, title+" "+background+" "+decision)
	return GetDecision(id)
}

func UpdateDecisionStatus(id, status string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.Exec("UPDATE decisions SET status = ? WHERE id = ?", status, id)
	return GetDecision(id)
}

// ============================================================
// Ideas
// ============================================================

func ListIdeas(status string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT * FROM ideas"
	var args []any
	if status != "" {
		q += " WHERE status = ?"
		args = append(args, status)
	}
	q += " ORDER BY created_at DESC, id DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
		ideas, err := ScanIdeaRows(rows); if err != nil { return nil, err }; for _, idea := range ideas { var cc int; db.QueryRow("SELECT COUNT(*) FROM idea_comments WHERE idea_id = ?", idea["id"]).Scan(&cc); idea["comment_count"] = cc; idea["converted_to"] = nil }; return ideas, nil
}

func GetIdea(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	idea := map[string]any{}
	row := db.QueryRow("SELECT * FROM ideas WHERE id = ?", id)
	if err := ScanIdeaRow(row, idea); err != nil {
		return nil, err
	}
	return idea, nil
}

func CreateIdea(title, summary, impact, source string, canonConflict bool, currentSummary, mainQuestion, recommendedNextAction string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("idea")
	now := u.NowISO()
	cc := 0
	if canonConflict {
		cc = 1
	}
	_, err = db.Exec("INSERT INTO ideas (id, title, summary, impact, source, status, canon_conflict, current_summary, main_question, recommended_next_action, updated_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, title, summary, impact, source, "new", cc, currentSummary, mainQuestion, recommendedNextAction, now, now)
	if err != nil {
		return nil, err
	}
		pmdb.SyncFTS5Entity(db, "idea", id, title, title+" "+summary)
	return GetIdea(id)
}

func UpdateIdea(id string, payload map[string]any) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := MapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return GetIdea(id)
	}
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE ideas SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return GetIdea(id)
}

func ReviewIdea(id, status, note string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.Exec("UPDATE ideas SET status = ?, updated_at = ? WHERE id = ?", status, u.NowISO(), id)
	if note != "" {
		db.Exec("INSERT INTO idea_comments (id, idea_id, author_type, author_name, kind, content, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)", u.Slug("ic"), id, "human", "reviewer", "review", note, u.NowISO())
	}
	return GetIdea(id)
}

func CreateIdeaComment(ideaID, content, kind, authorType, authorName string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("ic")
	now := u.NowISO()
	db.Exec("INSERT INTO idea_comments (id, idea_id, author_type, author_name, kind, content, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)", id, ideaID, authorType, authorName, kind, content, now)
	return map[string]any{"id": id, "idea_id": ideaID, "kind": kind, "content": content, "created_at": now}, nil
}

func ConvertIdeaToTask(ideaID, planID string) (map[string]any, error) {
	if planID == "" {
		return nil, fmt.Errorf("idea convert --to task requires --plan-id. Find a plan: aipmc plan list")
	}
	idea, err := GetIdea(ideaID)
	if err != nil {
		return nil, err
	}
	title, _ := idea["title"].(string)
	if title == "" {
		return nil, fmt.Errorf("idea %s has no title", ideaID)
	}
	task, err := CreateTask("", title, "P1", "todo", "general", planID, nil)
	if err != nil {
		return nil, err
	}
	taskID, _ := task["id"].(string)
	if taskID == "" {
		return nil, fmt.Errorf("created task for idea %s has no id", ideaID)
	}
	CreateLink("", "idea", ideaID, "converted_to", "task", taskID, "Converted from idea thread")
	UpdateIdea(ideaID, map[string]any{"status": "under_review", "recommended_next_action": "converted_to_task"})
	return map[string]any{"type": "task", "id": taskID, "title": title}, nil
}

func ConvertIdeaToDecision(ideaID string) (map[string]any, error) {
	idea, err := GetIdea(ideaID)
	if err != nil {
		return nil, err
	}
	title, _ := idea["title"].(string)
	if title == "" {
		return nil, fmt.Errorf("idea %s has no title", ideaID)
	}
	bg := title
	if cs, _ := idea["current_summary"].(string); cs != "" {
		bg = cs
	}
	dec, err := CreateDecision("", title, bg, title, "proposed")
	if err != nil {
		return nil, err
	}
	decID, _ := dec["id"].(string)
	if decID == "" {
		return nil, fmt.Errorf("created decision for idea %s has no id", ideaID)
	}
	CreateLink("", "idea", ideaID, "converted_to", "decision", decID, "Converted from idea thread")
	UpdateIdea(ideaID, map[string]any{"status": "accepted", "recommended_next_action": "converted_to_decision"})
	return map[string]any{"type": "decision", "id": decID, "title": title}, nil
}

// ============================================================
// Roadmaps
// ============================================================

func ListRoadmaps(visionID string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT * FROM roadmap"
	var args []any
	if visionID != "" {
		q += " WHERE vision_id = ?"
		args = append(args, visionID)
	}
	q += " ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'planned' THEN 1 ELSE 2 END, target_date ASC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanRoadmapRows(rows)
}

func GetRoadmap(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	r := map[string]any{}
	row := db.QueryRow("SELECT * FROM roadmap WHERE id = ?", id)
	if err := ScanRoadmapRow(row, r); err != nil {
		return nil, err
	}
	return r, nil
}

func CreateRoadmap(title, targetDate, visionID, status, priority string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("rdm")
	now := u.NowISO()
	_, err = db.Exec("INSERT INTO roadmap (id, vision_id, title, target_date, status, priority, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", id, visionID, title, targetDate, status, priority, now, now)
	if err != nil {
		return nil, err
	}
		pmdb.SyncFTS5Entity(db, "roadmap", id, title, title)
	return GetRoadmap(id)
}

func UpdateRoadmap(id string, payload map[string]any) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := MapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return GetRoadmap(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, u.NowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE roadmap SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return GetRoadmap(id)
}

// ============================================================
// Principles
// ============================================================

func ListPrinciples(status, kind string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT * FROM principles"
	var args []any
	var clauses []string
	if status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
	}
	if kind != "" {
		clauses = append(clauses, "kind = ?")
		args = append(args, kind)
	}
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY created_at DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanPrincipleRows(rows)
}

func GetPrinciple(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	p := map[string]any{}
	row := db.QueryRow("SELECT * FROM principles WHERE id = ?", id)
	if err := ScanPrincipleRow(row, p); err != nil {
		return nil, err
	}
	return p, nil
}

func CreatePrinciple(title, summary, kind, status string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("prncpl")
	now := u.NowISO()
	_, err = db.Exec("INSERT INTO principles (id, title, summary, kind, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", id, title, summary, kind, status, now, now)
	if err != nil {
		return nil, err
	}
		pmdb.SyncFTS5Entity(db, "principle", id, title, title+" "+summary)
	return GetPrinciple(id)
}

func UpdatePrinciple(id string, payload map[string]any) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := MapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return GetPrinciple(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, u.NowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE principles SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return GetPrinciple(id)
}

// ============================================================
// Links
// ============================================================

func ListLinks(sourceID, targetID, relation string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT * FROM links"
	var args []any
	var clauses []string
	if sourceID != "" {
		clauses = append(clauses, "source_id = ?")
		args = append(args, sourceID)
	}
	if targetID != "" {
		clauses = append(clauses, "target_id = ?")
		args = append(args, targetID)
	}
	if relation != "" {
		clauses = append(clauses, "relation = ?")
		args = append(args, relation)
	}
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY created_at DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var links []map[string]any
	for rows.Next() {
		var id, sourceType, sid, rel, targetType, tid, note, createdAt string
		rows.Scan(&id, &sourceType, &sid, &rel, &targetType, &tid, &note, &createdAt)
		links = append(links, map[string]any{"id": id, "source_type": sourceType, "source_id": sid, "relation": rel, "target_type": targetType, "target_id": tid, "note": note, "created_at": createdAt})
	}
	if links == nil {
		links = []map[string]any{}
	}
	return links, nil
}

func CreateLink(projectPath string, sourceType, sourceID, relation, targetType, targetID, note string) (map[string]any, error) {
	db, err := pmdb.OpenProject(projectPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Whitelist: only these 6 relations are allowed via links table
	allowedLinks := map[string]bool{
		"relates_to":   true,
		"implements":   true,
		"fixes":        true,
		"blocked_by":   true,
		"depends_on":   true,
		"converted_to": true,
	}
	if !allowedLinks[relation] {
		return nil, fmt.Errorf("relation '%s' is not allowed. Valid options: relates_to, implements, fixes, blocked_by, depends_on, converted_to", relation)
	}

	id := u.Slug("link")
	now := u.NowISO()

	// Wrap transaction in retryOnBusy for concurrent-write resilience
	err = retryOnBusy(func() error {
		tx, txErr := db.Begin()
		if txErr != nil {
			return fmt.Errorf("begin tx: %w", txErr)
		}
		defer tx.Rollback()

		_, txErr = tx.Exec("INSERT OR IGNORE INTO links (id, source_type, source_id, relation, target_type, target_id, note, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", id, sourceType, sourceID, relation, targetType, targetID, note, now)
		if txErr != nil {
			return fmt.Errorf("links write: %w", txErr)
		}

		// Dual-write to graph_edges for trace_context visibility.
		if storeAllowedEdgeTypes[relation] {
			edgeID := u.Slug("gedge")
			weight := relationWeight(relation)
			ev := map[string]any{"via": "link_entities", "link_id": id}
			if note != "" {
				ev["note"] = note
			}
			evJSON := u.JsonStr(ev)
			_, txErr = tx.Exec("INSERT OR IGNORE INTO graph_edges (id, source_type, source_id, edge_type, target_type, target_id, weight, evidence_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
				edgeID, sourceType, sourceID, relation, targetType, targetID, weight, evJSON, now)
			if txErr != nil {
				return fmt.Errorf("graph_edges write: %w", txErr)
			}
		}

		return tx.Commit()
	})
	if err != nil {
		return nil, err
	}

	return map[string]any{"id": id, "source_type": sourceType, "source_id": sourceID, "relation": relation, "target_type": targetType, "target_id": targetID}, nil
}

func DeleteLink(id string) error {
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("DELETE FROM links WHERE id = ?", id)
	return err
}

// ============================================================
// Docs
// ============================================================

func ListDocRecords(status, layer string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT * FROM doc_records"
	var args []any
	var clauses []string
	if status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
	}
	if layer != "" {
		clauses = append(clauses, "layer = ?")
		args = append(args, layer)
	}
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var docs []map[string]any
	for rows.Next() {
		var path, dtype, dstatus, layer, lastReviewed string
		var sourceOfTruth int
		var supersededBy sql.NullString
		rows.Scan(&path, &dtype, &dstatus, &layer, &sourceOfTruth, &lastReviewed, &supersededBy)
		d := map[string]any{"path": path, "type": dtype, "status": dstatus, "layer": layer, "source_of_truth": sourceOfTruth == 1, "last_reviewed": lastReviewed}
		if supersededBy.Valid {
			d["superseded_by"] = supersededBy.String
		}
		docs = append(docs, d)
	}
	if docs == nil {
		docs = []map[string]any{}
	}
	return docs, nil
}

func UpdateDocRecord(path string, payload map[string]any) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := MapKeyToColumn(k)
		if col == "" {
			continue
		}
		if k == "source_of_truth" {
			if b, ok := v.(bool); ok && b {
				v = 1
			} else {
				v = 0
			}
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return nil, fmt.Errorf("nothing to update")
	}
	args = append(args, path)
	_, err = db.Exec(fmt.Sprintf("UPDATE doc_records SET %s WHERE path = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}


func ReadDocContent(path string) (string, error) {
	dir, err := pmdb.RuntimeDir()
	if err != nil {
		return "", err
	}
	projectRoot := filepath.Dir(filepath.Dir(dir))
	fullPath := filepath.Join(projectRoot, path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ============================================================
// Daily Notes
// ============================================================

func GetDailyNote(date string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if date == "" {
		date = u.Today()
	}
	row := db.QueryRow("SELECT * FROM daily_notes WHERE note_date = ?", date)
	var noteDate, completedJSON, problemsJSON, risksJSON, nextJSON, updatedAt string
	if err := row.Scan(&noteDate, &completedJSON, &problemsJSON, &risksJSON, &nextJSON, &updatedAt); err != nil {
		return map[string]any{"note_date": date, "completed": []any{}, "problems": []any{}, "risks": []any{}, "next": []any{}}, nil
	}
	return map[string]any{"note_date": noteDate, "completed": u.ParseJSONList(completedJSON), "problems": u.ParseJSONList(problemsJSON), "risks": u.ParseJSONList(risksJSON), "next": u.ParseJSONList(nextJSON), "updated_at": updatedAt}, nil
}

func AppendDailyNote(date string, payload map[string][]string) (map[string]any, error) {
	return UpsertDaily(date, payload, true)
}
func ReplaceDailyNote(date string, payload map[string][]string) (map[string]any, error) {
	return UpsertDaily(date, payload, false)
}

func UpsertDaily(date string, payload map[string][]string, append_ bool) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if date == "" {
		date = u.Today()
	}
	existing, _ := GetDailyNote(date)
	merge := func(key string, newItems []string) []any {
		var base []any
		if append_ {
			if arr, ok := existing[key].([]any); ok {
				base = arr
			}
		}
		for _, s := range newItems {
			base = append(base, s)
		}
		return base
	}
	now := u.NowISO()
	db.Exec("INSERT OR REPLACE INTO daily_notes (note_date, completed_json, problems_json, risks_json, next_json, updated_at) VALUES (?, ?, ?, ?, ?, ?)", date, u.JsonStr(merge("completed", payload["completed"])), u.JsonStr(merge("problems", payload["problems"])), u.JsonStr(merge("risks", payload["risks"])), u.JsonStr(merge("next", payload["next"])), now)
	return GetDailyNote(date)
}

func ListDailyNotes() ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT * FROM daily_notes ORDER BY note_date DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notes []map[string]any
	for rows.Next() {
		var noteDate, completedJSON, problemsJSON, risksJSON, nextJSON, updatedAt string
		rows.Scan(&noteDate, &completedJSON, &problemsJSON, &risksJSON, &nextJSON, &updatedAt)
		notes = append(notes, map[string]any{"note_date": noteDate, "completed": u.ParseJSONList(completedJSON), "problems": u.ParseJSONList(problemsJSON), "risks": u.ParseJSONList(risksJSON), "next": u.ParseJSONList(nextJSON), "updated_at": updatedAt})
	}
	if notes == nil {
		notes = []map[string]any{}
	}
	return notes, nil
}

// ============================================================
// Visions
// ============================================================

func ListVisions() ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT * FROM visions ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanVisionRows(rows)
}

func GetVision(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	v := map[string]any{}
	row := db.QueryRow("SELECT * FROM visions WHERE id = ?", id)
	var id2, title, summary, status, horizon, createdAt, updatedAt string
	if err := row.Scan(&id2, &title, &summary, &status, &horizon, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	v["id"] = id2
	v["title"] = title
	v["summary"] = summary
	v["status"] = status
	v["horizon"] = horizon
	v["created_at"] = createdAt
	v["updated_at"] = updatedAt
	return v, nil
}

func CreateVision(title, summary, status, horizon string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("vision")
	now := u.NowISO()
	db.Exec("INSERT INTO visions (id, title, summary, status, horizon, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", id, title, summary, status, horizon, now, now)
		pmdb.SyncFTS5Entity(db, "vision", id, title, title+" "+summary)
	return GetVision(id)
}

func UpdateVision(id string, payload map[string]any) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := MapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return GetVision(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, u.NowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE visions SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return GetVision(id)
}

// ============================================================
// Canon
// ============================================================

func GetCanon() (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT * FROM canon WHERE id = 1")
	var id int
	var updatedAt, productGoal, engFocus, arch string
	if err := row.Scan(&id, &updatedAt, &productGoal, &engFocus, &arch); err != nil {
		return map[string]any{"id":"canon-current","product_goal":"","engineering_focus":"","architecture":"","updated_at":"","version_scope":[]any{},"avoid_now":[]any{},"top_tasks":[]any{},"source_docs":[]any{},"related_decisions":[]any{}}, nil
	}
	items := map[string][]string{}
	itemRows, _ := db.Query("SELECT item_type, value FROM canon_items ORDER BY item_type, position")
	if itemRows != nil {
		defer itemRows.Close()
		for itemRows.Next() {
			var it, val string
			itemRows.Scan(&it, &val)
			items[it] = append(items[it], val)
		}
	}
	gi := func(k string) []any {
		if vals, ok := items[k]; ok {
			r := make([]any, len(vals))
			for i, v := range vals { r[i] = v }
			return r
		}
		return []any{}
	}
	return map[string]any{
		"id":"canon-current","product_goal":productGoal,"engineering_focus":engFocus,
		"architecture":arch,"updated_at":updatedAt,
		"version_scope":gi("version_scope"),"avoid_now":gi("avoid_now"),
		"top_tasks":gi("top_tasks"),"source_docs":gi("source_docs"),
		"related_decisions":gi("related_decisions"),
	}, nil
}

func UpdateCanon(decisionID, productGoal, engFocus, arch string, addScope, addAvoid []string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	now := u.NowISO()
	db.Exec("INSERT OR REPLACE INTO canon (id, updated_at, product_goal, engineering_focus, architecture) VALUES (1, ?, ?, ?, ?)", now, productGoal, engFocus, arch)
	for i, s := range addScope {
		db.Exec("INSERT OR REPLACE INTO canon_items (item_type, position, value) VALUES (?, ?, ?)", "scope", i, s)
	}
	for i, s := range addAvoid {
		db.Exec("INSERT OR REPLACE INTO canon_items (item_type, position, value) VALUES (?, ?, ?)", "avoid", i, s)
	}
	return GetCanon()
}

// ============================================================
// Threads — 线索 (retrospective aggregation of related entities)
// ============================================================

type Thread struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Summary   string          `json:"summary"`
	Status    string          `json:"status"`
	Source    string          `json:"source"`
	Items     []ThreadItem    `json:"items"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
}

type ThreadItem struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Note       string `json:"note"`
	AddedAt    string `json:"added_at"`
}

type ThreadSuggestion struct {
	SuggestedTitle string        `json:"suggested_title"`
	Rationale      string        `json:"rationale"`
	SourceEntities []ThreadItem  `json:"source_entities"`
	Score          float64       `json:"score"`
}

func ListThreads(status string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT * FROM threads"
	var args []any
	if status != "" {
		q += " WHERE status = ?"
		args = append(args, status)
	}
	q += " ORDER BY updated_at DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var threads []map[string]any
	for rows.Next() {
		var id, title, summary, status, source, createdAt, updatedAt string
		rows.Scan(&id, &title, &summary, &status, &source, &createdAt, &updatedAt)
		threads = append(threads, map[string]any{
			"id": id, "title": title, "summary": summary, "status": status,
			"source": source, "created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if threads == nil {
		threads = []map[string]any{}
	}
	for _, t := range threads {
		items, _ := ListThreadItems(u.Str(t["id"]))
		t["items"] = items
	}
	return threads, nil
}

func GetThread(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT * FROM threads WHERE id = ?", id)
	var tid, title, summary, status, source, createdAt, updatedAt string
	if err := row.Scan(&tid, &title, &summary, &status, &source, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	items, _ := ListThreadItems(id)
	return map[string]any{
		"id": tid, "title": title, "summary": summary, "status": status,
		"source": source, "items": items, "created_at": createdAt, "updated_at": updatedAt,
	}, nil
}

func CreateThread(title, summary, source string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("thread")
	now := u.NowISO()
	if source == "" {
		source = "manual"
	}
	_, err = db.Exec("INSERT INTO threads (id, title, summary, status, source, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", id, title, summary, "active", source, now, now)
	if err != nil {
		return nil, err
	}
		pmdb.SyncFTS5Entity(db, "thread", id, title, title+" "+summary)
	return GetThread(id)
}

func UpdateThread(id string, payload map[string]any) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := MapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return GetThread(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, u.NowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE threads SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return GetThread(id)
}

func DeleteThread(id string) error {
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	db.Exec("DELETE FROM thread_items WHERE thread_id = ?", id)
	_, err = db.Exec("DELETE FROM threads WHERE id = ?", id)
	return err
}

func ListThreadItems(threadID string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT entity_type, entity_id, note, added_at FROM thread_items WHERE thread_id = ? ORDER BY added_at", threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var etype, eid, note, addedAt string
		rows.Scan(&etype, &eid, &note, &addedAt)
		title, status := ResolveEntityTitleStatus(etype, eid)
		items = append(items, map[string]any{
			"entity_type": etype, "entity_id": eid, "title": title, "status": status,
			"note": note, "added_at": addedAt,
		})
	}
	if items == nil {
		items = []map[string]any{}
	}
	return items, nil
}

func AddToThread(threadID, entityType, entityID, note string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	now := u.NowISO()
	_, err = db.Exec("INSERT OR REPLACE INTO thread_items (thread_id, entity_type, entity_id, added_at, note) VALUES (?, ?, ?, ?, ?)", threadID, entityType, entityID, now, note)
	if err != nil {
		return nil, err
	}
	db.Exec("UPDATE threads SET updated_at = ? WHERE id = ?", now, threadID)
	return GetThread(threadID)
}

func RemoveFromThread(threadID, entityType, entityID string) error {
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("DELETE FROM thread_items WHERE thread_id = ? AND entity_type = ? AND entity_id = ?", threadID, entityType, entityID)
	return err
}

func ResolveEntityTitleStatus(entityType, entityID string) (string, string) {
	switch entityType {
	case "task":
		if t, err := GetTask(entityID); err == nil {
			return u.Str(t["title"]), u.Str(t["status"])
		}
	case "plan":
		if p, err := GetPlan(entityID); err == nil {
			return u.Str(p["title"]), u.Str(p["status"])
		}
	case "commit":
		if c, err := GetCommit(entityID); err == nil {
			return u.Str(c["title"]), u.Str(c["status"])
		}
	case "decision":
		if d, err := GetDecision(entityID); err == nil {
			return u.Str(d["title"]), u.Str(d["status"])
		}
	case "idea":
		if i, err := GetIdea(entityID); err == nil {
			return u.Str(i["title"]), u.Str(i["status"])
		}
	case "bug":
		if b, err := GetBug(entityID); err == nil {
			return u.Str(b["title"]), u.Str(b["status"])
		}
	case "roadmap":
		if r, err := GetRoadmap(entityID); err == nil {
			return u.Str(r["title"]), u.Str(r["status"])
		}
	}
	return "", ""
}

// CommitContext wraps a commit with resolved task/plan/thread context for agent analysis.
type CommitContext struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Files     []string `json:"files"`
	TaskID    string   `json:"task_id"`
	TaskTitle string   `json:"task_title"`
	PlanID    string   `json:"plan_id"`
	PlanTitle string   `json:"plan_title"`
	CreatedAt string   `json:"created_at"`
	InThreads []string `json:"in_threads"`
}

// listRecentCommitsWithContext returns recent commits enriched with task/plan/file/thread
// information, ready for the AI agent to review and organize into threads.
func ListRecentCommitsWithContext(limit int) ([]CommitContext, error) {
	commits, err := ListCommits("", "", "", "", limit)
	if err != nil {
		return nil, err
	}

	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	taskCache := map[string]map[string]any{}
	planCache := map[string]map[string]any{}

	var results []CommitContext
	for _, c := range commits {
		cc := CommitContext{
			ID:        u.Str(c["id"]),
			Title:     u.Str(c["title"]),
			TaskID:    u.Str(c["task_id"]),
			CreatedAt: u.Str(c["created_at"]),
		}

		if rawFiles, ok := c["files"].([]any); ok {
			for _, f := range rawFiles {
				if s, ok := f.(string); ok {
					cc.Files = append(cc.Files, s)
				}
			}
		}

		// Resolve task
		if cc.TaskID != "" {
			if t, ok := taskCache[cc.TaskID]; ok {
				cc.TaskTitle = u.Str(t["title"])
				cc.PlanID = u.Str(t["plan_id"])
			} else {
				if t, err := GetTaskSimple(cc.TaskID); err == nil {
					taskCache[cc.TaskID] = t
					cc.TaskTitle = u.Str(t["title"])
					cc.PlanID = u.Str(t["plan_id"])
				}
			}
		}

		// Resolve plan
		if cc.PlanID != "" {
			if p, ok := planCache[cc.PlanID]; ok {
				cc.PlanTitle = u.Str(p["title"])
			} else {
				if p, err := GetPlan(cc.PlanID); err == nil && p != nil {
					planCache[cc.PlanID] = p
					cc.PlanTitle = u.Str(p["title"])
				}
			}
		}

		// Resolve existing thread memberships
		if tidRows, err := db.Query("SELECT thread_id FROM thread_items WHERE entity_type = 'commit' AND entity_id = ?", cc.ID); err == nil {
			for tidRows.Next() {
				var tid string
				if tidRows.Scan(&tid) == nil {
					cc.InThreads = append(cc.InThreads, tid)
				}
			}
			tidRows.Close()
		}

		if cc.InThreads == nil {
			cc.InThreads = []string{}
		}

		results = append(results, cc)
	}

	return results, nil
}

// ============================================================
// Helpers
// ============================================================

func MapKeyToColumn(k string) string {
	m := map[string]string{
		"title": "title", "summary": "summary", "status": "status", "priority": "priority",
		"phase": "phase", "goal": "goal", "roadmap_id": "roadmap_id", "plan_id": "plan_id",
		"vision_id": "vision_id", "task_id": "task_id", "decision_id": "decision_id",
		"commit_id": "commit_id", "description": "description", "severity": "severity",
		"error": "error", "files": "files_json", "root_cause": "root_cause", "fix": "fix", "tags": "tags",
		"branch": "branch", "commit_hash": "commit_hash", "test_status": "test_status", "review_status": "review_status",
		"evidence_summary": "evidence_summary", "review_notes": "review_notes",
		"kind": "kind", "source": "source", "impact": "impact",
		"current_summary": "current_summary", "main_question": "main_question",
		"recommended_next_action": "recommended_next_action",
		"task_ids": "task_ids_json",
		"target_date":             "target_date", "horizon": "horizon",
		"note": "note", "content": "content",
	}
	if col, ok := m[k]; ok {
		return col
	}
	return ""
}

// ============================================================
// Row scanners
// ============================================================

func ScanTasks(rows *sql.Rows, err error) ([]Task, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []Task
	for rows.Next() {
		var t Task
		var acceptanceJSON, docsJSON, decsJSON string
		var roadmapID, planID sql.NullString
		rows.Scan(&t.ID, &t.Title, &t.Status, &t.Priority, &t.Phase, &acceptanceJSON, &docsJSON, &decsJSON, &t.LastNote, &t.UpdatedAt, &roadmapID, &planID, &t.CreatedAt)
		t.RoadmapID = roadmapID.String
		t.PlanID = planID.String
		t.Acceptance = u.ParseJSONList(acceptanceJSON)
		t.RelatedDocs = u.ParseJSONList(docsJSON)
		t.RelatedDecisions = u.ParseJSONList(decsJSON)
		tasks = append(tasks, t)
	}
	if tasks == nil {
		tasks = []Task{}
	}
	return tasks, nil
}

func ScanTaskRow(row *sql.Row, m map[string]any) error {
	var id, title, status, priority, phase, accJSON, docsJSON, decsJSON, lastNote, updatedAt, createdAt string
	var roadmapID, planID sql.NullString
	if err := row.Scan(&id, &title, &status, &priority, &phase, &accJSON, &docsJSON, &decsJSON, &lastNote, &updatedAt, &roadmapID, &planID, &createdAt); err != nil {
		return err
	}
	m["id"] = id
	m["title"] = title
	m["status"] = status
	m["priority"] = priority
	m["phase"] = phase
	m["acceptance_json"] = accJSON
	m["related_docs_json"] = docsJSON
	m["related_decisions_json"] = decsJSON
	m["last_note"] = lastNote
	m["updated_at"] = updatedAt
	m["roadmap_id"] = roadmapID.String
	m["plan_id"] = planID.String
	m["created_at"] = createdAt
	return nil
}

func ScanCommitRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var commits []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := ScanCommitRow(rows, m); err != nil {
			return nil, err
		}
		commits = append(commits, m)
	}
	if commits == nil {
		commits = []map[string]any{}
	}
	return commits, nil
}

func ScanCommitRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
	var id, title, summary, evSum, revNotes, branch, chash, status, testStatus, reviewStatus, filesJSON, createdAt, updatedAt string
	var taskID, decID sql.NullString
	// Column order MUST match CREATE TABLE commits:
	// id, title, summary, evidence_summary, review_notes, branch, commit_hash,
	// task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at
	if err := scanner.Scan(&id, &title, &summary, &evSum, &revNotes, &branch, &chash, &taskID, &decID, &status, &testStatus, &reviewStatus, &filesJSON, &createdAt, &updatedAt); err != nil {
		return err
	}
	m["id"] = id
	m["title"] = title
	m["summary"] = summary
	m["evidence_summary"] = evSum
	m["review_notes"] = revNotes
	m["branch"] = branch
	m["commit_hash"] = chash
	m["task_id"] = taskID.String
	m["decision_id"] = decID.String
	m["status"] = status
	m["test_status"] = testStatus
	m["review_status"] = reviewStatus
	m["files"] = u.ParseJSONList(filesJSON)
	m["files_json"] = filesJSON
	m["created_at"] = createdAt
	m["updated_at"] = updatedAt
	return nil
}

func ScanPlanRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var plans []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := ScanPlanRow(rows, m); err != nil {
			return nil, err
		}
		plans = append(plans, m)
	}
	if plans == nil {
		plans = []map[string]any{}
	}
	return plans, nil
}

func ScanPlanRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
	var id, title, goal, status, priority, scopeJSON, risksJSON, assumptionsJSON, taskIDsJSON, source, createdAt, updatedAt string
	var roadmapID, visionID sql.NullString
	if err := scanner.Scan(&id, &roadmapID, &visionID, &title, &goal, &status, &priority, &scopeJSON, &risksJSON, &assumptionsJSON, &taskIDsJSON, &source, &createdAt, &updatedAt); err != nil {
		return err
	}
	m["id"] = id
	m["roadmap_id"] = roadmapID.String
	m["vision_id"] = visionID.String
	m["title"] = title
	m["goal"] = goal
	m["status"] = status
	m["priority"] = priority
	m["scope"] = u.ParseJSONList(scopeJSON)
	m["risks"] = u.ParseJSONList(risksJSON)
	m["assumptions"] = u.ParseJSONList(assumptionsJSON)
	m["task_ids"] = u.ParseJSONStrList(taskIDsJSON)
	m["source"] = source
	m["created_at"] = createdAt
	m["updated_at"] = updatedAt
	return nil
}

func ScanBugRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var bugs []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := ScanBugRow(rows, m); err != nil {
			return nil, err
		}
		bugs = append(bugs, m)
	}
	if bugs == nil {
		bugs = []map[string]any{}
	}
	return bugs, nil
}

func ScanBugRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
	var id, title, desc, severity, status, errMsg, files, rootCause, fix, tags, createdAt, updatedAt string
	var commitID sql.NullString
	if err := scanner.Scan(&id, &title, &desc, &severity, &status, &commitID, &errMsg, &files, &rootCause, &fix, &tags, &createdAt, &updatedAt); err != nil {
		return err
	}
	m["id"] = id
	m["title"] = title
	m["description"] = desc
	m["severity"] = severity
	m["status"] = status
	m["commit_id"] = commitID.String
	m["error"] = errMsg
	m["files"] = files
	m["root_cause"] = rootCause
	m["fix"] = fix
	m["tags"] = tags
	m["created_at"] = createdAt
	m["updated_at"] = updatedAt
	return nil
}

func ScanDecisionRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var decisions []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := ScanDecisionRow(rows, m); err != nil {
			return nil, err
		}
		decisions = append(decisions, m)
	}
	if decisions == nil {
		decisions = []map[string]any{}
	}
	return decisions, nil
}

func ScanDecisionRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
	var id, title, date, status, background, decisionText, impactJSON, altJSON, relTasksJSON string
	var updatesCanon int
	if err := scanner.Scan(&id, &title, &date, &status, &background, &decisionText, &impactJSON, &altJSON, &relTasksJSON, &updatesCanon); err != nil {
		return err
	}
	m["id"] = id
	m["title"] = title
	m["date"] = date
	m["status"] = status
	m["background"] = background
	m["decision"] = decisionText
	m["impact"] = u.ParseJSONList(impactJSON)
	m["alternatives"] = u.ParseJSONList(altJSON)
	m["related_tasks"] = u.ParseJSONList(relTasksJSON)
	m["updates_canon"] = updatesCanon == 1
	return nil
}

func ScanIdeaRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var ideas []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := ScanIdeaRow(rows, m); err != nil {
			return nil, err
		}
		ideas = append(ideas, m)
	}
	if ideas == nil {
		ideas = []map[string]any{}
	}
	return ideas, nil
}

func ScanIdeaRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
	var id, title, summary, impact, source, status, currentSummary, mainQuestion, recommendedNextAction, updatedAt, createdAt string
	var canonConflict int
	if err := scanner.Scan(&id, &title, &summary, &impact, &source, &status, &canonConflict, &currentSummary, &mainQuestion, &recommendedNextAction, &updatedAt, &createdAt); err != nil {
		return err
	}
	m["id"] = id
	m["title"] = title
	m["summary"] = summary
	m["impact"] = impact
	m["source"] = source
	m["status"] = status
	m["canon_conflict"] = canonConflict == 1
	m["current_summary"] = currentSummary
	m["main_question"] = mainQuestion
	m["recommended_next_action"] = recommendedNextAction
	m["updated_at"] = updatedAt
	m["created_at"] = createdAt
	return nil
}

func ScanRoadmapRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var roadmaps []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := ScanRoadmapRow(rows, m); err != nil {
			return nil, err
		}
		roadmaps = append(roadmaps, m)
	}
	if roadmaps == nil {
		roadmaps = []map[string]any{}
	}
	return roadmaps, nil
}

func ScanRoadmapRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
	var id, title, targetDate, status, priority, createdAt, updatedAt string
	var visionID sql.NullString
	if err := scanner.Scan(&id, &visionID, &title, &targetDate, &status, &priority, &createdAt, &updatedAt); err != nil {
		return err
	}
	m["id"] = id
	m["vision_id"] = visionID.String
	m["title"] = title
	m["target_date"] = targetDate
	m["status"] = status
	m["priority"] = priority
	m["created_at"] = createdAt
	m["updated_at"] = updatedAt
	return nil
}

func ScanPrincipleRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var principles []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := ScanPrincipleRow(rows, m); err != nil {
			return nil, err
		}
		principles = append(principles, m)
	}
	if principles == nil {
		principles = []map[string]any{}
	}
	return principles, nil
}

func ScanPrincipleRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
	var id, title, summary, kind, status, createdAt, updatedAt string
	if err := scanner.Scan(&id, &title, &summary, &kind, &status, &createdAt, &updatedAt); err != nil {
		return err
	}
	m["id"] = id
	m["title"] = title
	m["summary"] = summary
	m["kind"] = kind
	m["status"] = status
	m["created_at"] = createdAt
	m["updated_at"] = updatedAt
	return nil
}

func ScanVisionRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var visions []map[string]any
	for rows.Next() {
		m := map[string]any{}
		var id, title, summary, status, horizon, createdAt, updatedAt string
		rows.Scan(&id, &title, &summary, &status, &horizon, &createdAt, &updatedAt)
		m["id"] = id
		m["title"] = title
		m["summary"] = summary
		m["status"] = status
		m["horizon"] = horizon
		m["created_at"] = createdAt
		m["updated_at"] = updatedAt
		visions = append(visions, m)
	}
	if visions == nil {
		visions = []map[string]any{}
	}
	return visions, nil
}

func GetTask(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	task := map[string]any{}
	row := db.QueryRow("SELECT * FROM tasks WHERE id = ?", id)
	if err := ScanTaskRow(row, task); err != nil {
		return nil, err
	}
	commits, _ := ListCommitsByTask(id)
	approved, verified := 0, 0
	for _, c := range commits {
		s, _ := c["status"].(string)
		rs, _ := c["review_status"].(string)
		ts, _ := c["test_status"].(string)
		if (s == "committed" || s == "merged") && rs == "approved" {
			approved++
			if ts == "passed" {
				verified++
			}
		}
	}
	task["linked_commits"] = commits
	task["closure"] = map[string]any{"linked_commit_count": len(commits), "approved_commit_count": approved, "verified_approved_commit_count": verified, "can_mark_done": verified > 0}
	notes, _ := ListTaskNotes(id, 20)
	task["note_history"] = notes
	return task, nil
}

// ============================================================
// Delete operations
// ============================================================

func DeleteTask(id string) error {
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("DELETE FROM tasks WHERE id = ?", id)
	return err
}

func DeletePlan(id string) error {
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("DELETE FROM plans WHERE id = ?", id)
	return err
}

func DeleteBug(id string) error {
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("DELETE FROM bugs WHERE id = ?", id)
	return err
}

// ============================================================
// Events — PM intent change tracking (Phase 1)
// ============================================================

func CreateEvent(typ, entityType, entityID, summary string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("evt")
	now := u.NowISO()
	_, err = db.Exec("INSERT INTO events (id, type, entity_type, entity_id, summary, created_at, consumed_by_agent) VALUES (?, ?, ?, ?, ?, ?, 0)", id, typ, entityType, entityID, summary, now)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "type": typ, "entity_type": entityType, "entity_id": entityID, "summary": summary, "created_at": now, "consumed_by_agent": false}, nil
}

// HasUnconsumedEvent checks if an unconsumed event of the given type+entity already exists.
func HasUnconsumedEvent(typ, entityID string) bool {
	db, err := pmdb.Open()
	if err != nil {
		return false
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM events WHERE type=? AND entity_id=? AND consumed_by_agent=0",
		typ, entityID).Scan(&count); err != nil {
		return false
	}
	if count > 0 {
		u.LogShared("EVENT", "dedup skip type=%s entity=%s", typ, entityID[:min(len(entityID), 12)])
	}
	return count > 0
}

// HasEvent checks if an event of the given type+entity already exists in any
// state (including consumed). Prevents duplicate events from being re-created
// after an agent has consumed them.
func HasEvent(typ, entityType, entityID string) bool {
	db, err := pmdb.Open()
	if err != nil {
		return false
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM events WHERE type=? AND entity_type=? AND entity_id=?",
		typ, entityType, entityID).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

// MarkEventProcessed marks matching events as processed (2.3: 已读/已处理分离).
// Returns the number of events marked. Processed means the agent has actually
// resolved the underlying issue (e.g. bound an orphan commit, done a task),
// as opposed to consumed_by_agent which only means the event was shown.
func MarkEventProcessed(typ, entityID string) (int64, error) {
	db, err := pmdb.Open()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	res, err := db.Exec("UPDATE events SET processed_by_agent = 1 WHERE type = ? AND entity_id = ? AND processed_by_agent = 0", typ, entityID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func ListEvents(consumedOnly string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT * FROM events"
	if consumedOnly == "unconsumed" {
		// 2.3: "unconsumed" = 未读 AND 未处理。Agent 已处理的事件（processed_by_agent=1）
		// 不再注入/展示，避免已解决问题反复打扰（D2 已读/已处理分离）。
		q += " WHERE consumed_by_agent = 0 AND processed_by_agent = 0"
	}
	q += " ORDER BY created_at DESC LIMIT 50"
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []map[string]any
	for rows.Next() {
		var id, typ, entityType, entityID, summary, createdAt string
		var consumed, processed int
		rows.Scan(&id, &typ, &entityType, &entityID, &summary, &createdAt, &consumed, &processed)
		events = append(events, map[string]any{
			"id": id, "type": typ, "entity_type": entityType, "entity_id": entityID,
			"summary": summary, "created_at": createdAt, "consumed_by_agent": consumed == 1,
			"processed_by_agent": processed == 1,
		})
	}
	if events == nil {
		events = []map[string]any{}
	}
	return events, nil
}

func MarkEventsConsumed() error {
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("UPDATE events SET consumed_by_agent = 1 WHERE consumed_by_agent = 0")
	if err != nil {
		return err
	}
	writeLastBriefingConsumedAt(u.NowISO())
	return nil
}

// LastBriefingConsumedAt returns the ISO timestamp of the last aipm_mark_consumed call.
func LastBriefingConsumedAt() string {
	dir, err := pmdb.RuntimeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "cache", "last-briefing-consumed-at.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeLastBriefingConsumedAt(iso string) {
	dir, err := pmdb.RuntimeDir()
	if err != nil || iso == "" {
		return
	}
	cacheDir := filepath.Join(dir, "cache")
	_ = os.MkdirAll(cacheDir, 0755)
	_ = os.WriteFile(filepath.Join(cacheDir, "last-briefing-consumed-at.txt"), []byte(iso), 0644)
}

func GetUnconsumedEvents() ([]map[string]any, error) {
	return ListEvents("unconsumed")
}

// ============================================================
// Feedback — delegated to remote API (feedback.go)
//   Compatible with Python pmai feedback server.
//   CLI: aipmc feedback add --label bug|suggestion --content "..."
//   CLI: aipmc feedback list [--label bug|suggestion]
// ============================================================

// ---- Agent Profiles ----

func CreateAgentProfile(name, role, capabilities string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("agent")
	now := u.NowISO()
	if role == "" {
		role = "coder"
	}
	if capabilities == "" {
		capabilities = "[]"
	}
	_, err = db.Exec("INSERT INTO agent_profiles (id, name, role, capabilities, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', ?, ?)", id, name, role, capabilities, now, now)
	if err != nil {
		return nil, err
	}
	pmdb.SyncFTS5Entity(db, "agent", id, name, name+" "+role)
	return GetAgentProfile(id)
}

func GetAgentProfile(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT id, name, role, capabilities, status, created_at, updated_at FROM agent_profiles WHERE id = ?", id)
	a := map[string]any{}
	var caps string
	var aid, aname, arole, astatus, acreatedAt, aupdatedAt string
	row.Scan(&aid, &aname, &arole, &caps, &astatus, &acreatedAt, &aupdatedAt)
	a["id"] = aid; a["name"] = aname; a["role"] = arole; a["status"] = astatus; a["created_at"] = acreatedAt; a["updated_at"] = aupdatedAt
	a["capabilities"] = u.ParseJSONList(caps)
	return a, nil
}

func ListAgentProfiles() ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT id, name, role, capabilities, status, created_at, updated_at FROM agent_profiles ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		a := map[string]any{}
		var caps string
		var id, name, role, status, createdAt, updatedAt string
		rows.Scan(&id, &name, &role, &caps, &status, &createdAt, &updatedAt)
		a["id"] = id; a["name"] = name; a["role"] = role; a["status"] = status; a["created_at"] = createdAt; a["updated_at"] = updatedAt
		a["capabilities"] = u.ParseJSONList(caps)
		result = append(result, a)
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}

func UpdateAgentProfile(id string, payload map[string]any) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT id, name, role, capabilities, status FROM agent_profiles WHERE id = ?", id)
	var existingName, existingRole string
	var caps string
	var existingStatus string
	var existingID string
	row.Scan(&existingID, &existingName, &existingRole, &caps, &existingStatus)
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		switch k {
		case "name", "role", "status":
			setParts = append(setParts, k+" = ?")
			args = append(args, v)
		}
	}
	if len(setParts) == 0 {
		return GetAgentProfile(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, u.NowISO())
	args = append(args, id)
	_, err = db.Exec("UPDATE agent_profiles SET "+strings.Join(setParts, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return nil, err
	}
	a, _ := GetAgentProfile(id)
	pmdb.SyncFTS5Entity(db, "agent", id, u.Str(a["name"]), u.Str(a["name"])+" "+u.Str(a["role"]))
	return a, nil
}

// ============================================================
// Graph Edges
// ============================================================

// Allowed edge types for graph_edges (pipeline-auto-derived + agent-declared relationships).
var allowedEdgeTypes = map[string]bool{
	"file_touch":   true,
	"file_read":    true,
	"mentions":     true,
	"derived_from": true,
	"same_session": true,
	"implements":   true,
	// Agent-declared relations (mirrored from links table via CreateLink dual-write)
	"fixes":      true,
	"blocked_by": true,
	"depends_on": true,
	"relates_to": true,
}

// storeAllowedEdgeTypes mirrors allowedEdgeTypes for use within the store package.
var storeAllowedEdgeTypes = allowedEdgeTypes

// relationWeight maps a link relation type to a graph edge weight.
// Higher weight = stronger relationship.
func relationWeight(relation string) float64 {
	switch relation {
	case "implements", "fixes":
		return 1.0
	case "blocked_by", "depends_on":
		return 0.8
	case "relates_to":
		return 0.5
	default:
		return 0.5
	}
}

func CreateGraphEdge(sourceType, sourceID, edgeType, targetType, targetID string, weight float64, evidence map[string]any) error {
	if !allowedEdgeTypes[edgeType] {
		return fmt.Errorf("edge_type '%s' is not allowed", edgeType)
	}
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	id := u.Slug("gedge")
	now := u.NowISO()
	evJSON := u.JsonStr(evidence)
	_, err = db.Exec("INSERT OR IGNORE INTO graph_edges (id, source_type, source_id, edge_type, target_type, target_id, weight, evidence_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, sourceType, sourceID, edgeType, targetType, targetID, weight, evJSON, now)
	return err
}

func ListGraphEdges(sourceID, targetID, edgeType string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT id, source_type, source_id, edge_type, target_type, target_id, weight, evidence_json, created_at FROM graph_edges WHERE 1=1"
	var args []any
	if sourceID != "" {
		q += " AND source_id = ?"
		args = append(args, sourceID)
	}
	if targetID != "" {
		q += " AND target_id = ?"
		args = append(args, targetID)
	}
	if edgeType != "" {
		q += " AND edge_type = ?"
		args = append(args, edgeType)
	}
	q += " ORDER BY weight DESC LIMIT 200"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var edges []map[string]any
	for rows.Next() {
		var id, st, sid, et, tt, tid, evJSON, ca string
		var w float64
		rows.Scan(&id, &st, &sid, &et, &tt, &tid, &w, &evJSON, &ca)
		edges = append(edges, map[string]any{
			"id": id, "source_type": st, "source_id": sid, "edge_type": et,
			"target_type": tt, "target_id": tid, "weight": w,
			"evidence_json": evJSON, "created_at": ca,
		})
	}
	if edges == nil {
		edges = []map[string]any{}
	}
	return edges, nil
}
