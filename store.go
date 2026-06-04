package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"os"
	"path/filepath"
)

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

func listTasks(status, planID string) ([]Task, error) {
	db, err := openDB()
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
	return scanTasks(db.Query(q, args...))
}

func getTaskSimple(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	task := map[string]any{}
	row := db.QueryRow("SELECT * FROM tasks WHERE id = ?", id)
	if err := scanTaskRow(row, task); err != nil {
		return nil, err
	}
	return task, nil
}

func createTask(title, priority, status, phase, planID string, acceptance []string) (map[string]any, error) {
	if planID == "" {
		return nil, fmt.Errorf("task requires --plan-id. Find a plan: aipmc plan list")
	}
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("task")
	now := nowISO()
	accJSON := "[]"
	if len(acceptance) > 0 {
		accJSON = jsonStr(acceptance)
	}
	// Backfill roadmap_id from the plan
	var roadmapID string
	if err := db.QueryRow("SELECT roadmap_id FROM plans WHERE id = ?", planID).Scan(&roadmapID); err != nil {
		roadmapID = ""
	}
	_, err = db.Exec("INSERT INTO tasks (id, title, status, priority, phase, plan_id, acceptance_json, related_docs_json, related_decisions_json, last_note, updated_at, roadmap_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, title, status, priority, phase, planID, accJSON, "[]", "[]", "", today(), roadmapID, now)
	if err != nil {
		return nil, err
	}
	// Auto-create event
	createEvent("task_created", "task", id, fmt.Sprintf("New task: %s", title))
	var taskIDsJSON string
	if err := db.QueryRow("SELECT task_ids_json FROM plans WHERE id = ?", planID).Scan(&taskIDsJSON); err == nil {
		var ids []string
		json.Unmarshal([]byte(taskIDsJSON), &ids)
		ids = append(ids, id)
		newJSON, _ := json.Marshal(ids)
		db.Exec("UPDATE plans SET task_ids_json = ?, updated_at = ? WHERE id = ?", string(newJSON), nowISO(), planID)
	}
	return getTaskSimple(id)
}

func updateTask(id, status, note string, allowWithoutCommit, appendNote bool) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT * FROM tasks WHERE id = ?", id)
	existing := map[string]any{}
	if err := scanTaskRow(row, existing); err != nil {
		return nil, err
	}
	oldStatus := existing["status"].(string)
	if status == "" {
		status = oldStatus
	}
	if status != oldStatus {
		db.Exec("INSERT INTO task_notes (id, task_id, content, mode, created_at) VALUES (?, ?, ?, ?, ?)", slug("task-note"), id, fmt.Sprintf("Status changed from %s to %s", oldStatus, status), "system", nowISO())
	}
	if status == "done" && oldStatus != "done" && !allowWithoutCommit {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM commits WHERE task_id = ? AND status IN ('committed','merged') AND review_status='approved' AND test_status='passed'", id).Scan(&count)
		if count == 0 {
			return nil, fmt.Errorf("task cannot be marked done without at least one verified approved commit")
		}
	}
	nextNote := existing["last_note"].(string)
	if note != "" {
		if appendNote && nextNote != "" {
			nextNote = strings.TrimRight(nextNote, "\n") + "\n\n" + note
		} else {
			nextNote = note
		}
	}
	_, err = db.Exec("UPDATE tasks SET status = ?, last_note = ?, updated_at = ? WHERE id = ?", status, nextNote, today(), id)
	if err != nil {
		return nil, err
	}
	if note != "" {
		mode := "replace"
		if appendNote {
			mode = "append"
		}
		db.Exec("INSERT INTO task_notes (id, task_id, content, mode, created_at) VALUES (?, ?, ?, ?, ?)", slug("task-note"), id, note, mode, nowISO())
	}
	return getTaskSimple(id)
}

func appendTaskNote(taskID, content string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT * FROM tasks WHERE id = ?", taskID)
	existing := map[string]any{}
	if err := scanTaskRow(row, existing); err != nil {
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
	db.Exec("INSERT INTO task_notes (id, task_id, content, mode, created_at) VALUES (?, ?, ?, ?, ?)", slug("task-note"), taskID, content, "append", nowISO())
	db.Exec("UPDATE tasks SET last_note = ?, updated_at = ? WHERE id = ?", nextNote, today(), taskID)
	return getTask(taskID)
}

func listTaskNotes(taskID string, limit int) ([]map[string]any, error) {
	db, err := openDB()
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

func planTask(taskID string, steps []string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	items := make([]map[string]any, len(steps))
	for i, s := range steps {
		items[i] = map[string]any{"text": s, "done": false}
	}
	db.Exec("UPDATE tasks SET acceptance_json = ?, updated_at = ? WHERE id = ?", jsonStr(items), today(), taskID)
	return getTaskSimple(taskID)
}

func updateTaskCheckpoint(taskID string, index int, done bool) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT * FROM tasks WHERE id = ?", taskID)
	existing := map[string]any{}
	if err := scanTaskRow(row, existing); err != nil {
		return nil, err
	}
	var acc []map[string]any
	json.Unmarshal([]byte(existing["acceptance_json"].(string)), &acc)
	if index < 0 || index >= len(acc) {
		return nil, fmt.Errorf("checkpoint index %d out of range", index)
	}
	acc[index]["done"] = done
	db.Exec("UPDATE tasks SET acceptance_json = ?, updated_at = ? WHERE id = ?", jsonStr(acc), today(), taskID)
	return getTaskSimple(taskID)
}

// ============================================================
// Commits
// ============================================================

func listCommits(status, taskID, decisionID, since string, limit int) ([]map[string]any, error) {
	db, err := openDB()
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
			since = today()
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
	return scanCommitRows(rows)
}

func listCommitsByTask(taskID string) ([]map[string]any, error) {
	return listCommits("", taskID, "", "", 0)
}

func getCommit(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	c := map[string]any{}
	row := db.QueryRow("SELECT id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at FROM commits WHERE id = ?", id)
	if err := scanCommitRow(row, c); err != nil {
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

func createCommit(title, summary, evidenceSummary, reviewNotes, branch, commitHash, taskID, decisionID, status, testStatus, reviewStatus string, files []string) (map[string]any, error) {
	if taskID == "" {
		return nil, fmt.Errorf("commit requires --task-id (or --task-ids for multi-task). Find a task: aipmc task list --status in_progress")
	}
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var _x int
	if err := db.QueryRow("SELECT 1 FROM tasks WHERE id = ?", taskID).Scan(&_x); err != nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}
	id := slug("commit")
	now := nowISO()
	filesJSON := "[]"
	if len(files) > 0 {
		filesJSON = jsonStr(files)
	}
	_, err = db.Exec("INSERT INTO commits (id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, title, summary, evidenceSummary, reviewNotes, branch, commitHash, taskID, decisionID, status, testStatus, reviewStatus, filesJSON, now, now)
	if err != nil {
		return nil, err
	}
	c, _ := getCommit(id)
	return c, nil
}

func updateCommit(id string, payload map[string]any) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRow("SELECT id, title, summary, evidence_summary, review_notes, branch, commit_hash, task_id, decision_id, status, test_status, review_status, files_json, created_at, updated_at FROM commits WHERE id = ?", id)
	existing := map[string]any{}
	if err := scanCommitRow(row, existing); err != nil {
		return nil, err
	}
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := mapKeyToColumn(k)
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
	args = append(args, nowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE commits SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return existing, nil
}

// ============================================================
// Plans
// ============================================================

func listPlans(roadmapID, status string) ([]map[string]any, error) {
	db, err := openDB()
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
	return scanPlanRows(rows)
}

func getPlan(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	p := map[string]any{}
	row := db.QueryRow("SELECT * FROM plans WHERE id = ?", id)
	if err := scanPlanRow(row, p); err != nil {
		return nil, err
	}
	return p, nil
}

func createPlan(title, goal, roadmapID, visionID, priority, status string, scope, risks, assumptions, taskIDs []string) (map[string]any, error) {
	if roadmapID == "" {
		return nil, fmt.Errorf("plan requires --roadmap-id. Find a roadmap: aipmc roadmap list")
	}
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("plan")
	now := nowISO()
	_, err = db.Exec("INSERT INTO plans (id, roadmap_id, vision_id, title, goal, status, priority, scope_json, risks_json, assumptions_json, task_ids_json, source, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, roadmapID, visionID, title, goal, status, priority, jsonStr(scope), jsonStr(risks), jsonStr(assumptions), jsonStr(taskIDs), "manual", now, now)
	if err != nil {
		return nil, err
	}
	// Auto-create event for PM tracking
	createEvent("plan_created", "plan", id, fmt.Sprintf("New plan created: %s", title))
	return getPlan(id)
}

func updatePlan(id string, payload map[string]any) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := mapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return getPlan(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, nowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE plans SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return getPlan(id)
}

// ============================================================
// Bugs
// ============================================================

func listBugs(status, severity, commitID string, limit int) ([]map[string]any, error) {
	db, err := openDB()
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
	return scanBugRows(rows)
}

func getBug(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	b := map[string]any{}
	row := db.QueryRow("SELECT * FROM bugs WHERE id = ?", id)
	if err := scanBugRow(row, b); err != nil {
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

func createBug(title, description, severity, status, commitID, errMsg, files, rootCause, fix, tags string) (map[string]any, error) {
	db, err := openDB()
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
	id := slug("bug")
	now := nowISO()
	_, err = db.Exec("INSERT INTO bugs (id, title, description, severity, status, commit_id, error, files, root_cause, fix, tags, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, title, description, severity, status, commitID, errMsg, files, rootCause, fix, tags, now, now)
	if err != nil {
		return nil, err
	}
	return getBug(id)
}

func updateBug(id string, payload map[string]any) (map[string]any, error) {
	db, err := openDB()
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
		col := mapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return getBug(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, nowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE bugs SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return getBug(id)
}

// ============================================================
// Decisions
// ============================================================

func listDecisions() ([]map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT * FROM decisions ORDER BY date DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDecisionRows(rows)
}

func getDecision(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	d := map[string]any{}
	row := db.QueryRow("SELECT * FROM decisions WHERE id = ?", id)
	if err := scanDecisionRow(row, d); err != nil {
		return nil, err
	}
	return d, nil
}

func createDecision(title, background, decision, status string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("decision")
	_, err = db.Exec("INSERT INTO decisions (id, title, date, status, background, decision_text, impact_json, alternatives_json, related_tasks_json, updates_canon) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, title, today(), status, background, decision, "[]", "[]", "[]", 0)
	if err != nil {
		return nil, err
	}
	return getDecision(id)
}

func updateDecisionStatus(id, status string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.Exec("UPDATE decisions SET status = ? WHERE id = ?", status, id)
	return getDecision(id)
}

// ============================================================
// Ideas
// ============================================================

func listIdeas(status string) ([]map[string]any, error) {
	db, err := openDB()
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
		ideas, err := scanIdeaRows(rows); if err != nil { return nil, err }; for _, idea := range ideas { var cc int; db.QueryRow("SELECT COUNT(*) FROM idea_comments WHERE idea_id = ?", idea["id"]).Scan(&cc); idea["comment_count"] = cc; idea["converted_to"] = nil }; return ideas, nil
}

func getIdea(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	idea := map[string]any{}
	row := db.QueryRow("SELECT * FROM ideas WHERE id = ?", id)
	if err := scanIdeaRow(row, idea); err != nil {
		return nil, err
	}
	return idea, nil
}

func createIdea(title, summary, impact, source string, canonConflict bool, currentSummary, mainQuestion, recommendedNextAction string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("idea")
	now := nowISO()
	cc := 0
	if canonConflict {
		cc = 1
	}
	_, err = db.Exec("INSERT INTO ideas (id, title, summary, impact, source, status, canon_conflict, current_summary, main_question, recommended_next_action, updated_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", id, title, summary, impact, source, "new", cc, currentSummary, mainQuestion, recommendedNextAction, now, now)
	if err != nil {
		return nil, err
	}
	return getIdea(id)
}

func updateIdea(id string, payload map[string]any) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := mapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return getIdea(id)
	}
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE ideas SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return getIdea(id)
}

func reviewIdea(id, status, note string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.Exec("UPDATE ideas SET status = ?, updated_at = ? WHERE id = ?", status, nowISO(), id)
	if note != "" {
		db.Exec("INSERT INTO idea_comments (id, idea_id, author_type, author_name, kind, content, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)", slug("ic"), id, "human", "reviewer", "review", note, nowISO())
	}
	return getIdea(id)
}

func createIdeaComment(ideaID, content, kind, authorType, authorName string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("ic")
	now := nowISO()
	db.Exec("INSERT INTO idea_comments (id, idea_id, author_type, author_name, kind, content, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)", id, ideaID, authorType, authorName, kind, content, now)
	return map[string]any{"id": id, "idea_id": ideaID, "kind": kind, "content": content, "created_at": now}, nil
}

func convertIdeaToTask(ideaID, planID string) (map[string]any, error) {
	if planID == "" {
		return nil, fmt.Errorf("idea convert --to task requires --plan-id. Find a plan: aipmc plan list")
	}
	idea, err := getIdea(ideaID)
	if err != nil {
		return nil, err
	}
	task, err := createTask(idea["title"].(string), "P1", "todo", "general", planID, nil)
	if err != nil {
		return nil, err
	}
	createLink("idea", ideaID, "converted_to", "task", task["id"].(string), "Converted from idea thread")
	updateIdea(ideaID, map[string]any{"status": "under_review", "recommended_next_action": "converted_to_task"})
	return map[string]any{"type": "task", "id": task["id"], "title": task["title"]}, nil
}

func convertIdeaToDecision(ideaID string) (map[string]any, error) {
	idea, err := getIdea(ideaID)
	if err != nil {
		return nil, err
	}
	bg := idea["title"].(string)
	if cs, _ := idea["current_summary"].(string); cs != "" {
		bg = cs
	}
	dec, err := createDecision(idea["title"].(string), bg, idea["title"].(string), "proposed")
	if err != nil {
		return nil, err
	}
	createLink("idea", ideaID, "converted_to", "decision", dec["id"].(string), "Converted from idea thread")
	updateIdea(ideaID, map[string]any{"status": "accepted", "recommended_next_action": "converted_to_decision"})
	return map[string]any{"type": "decision", "id": dec["id"], "title": dec["title"]}, nil
}

// ============================================================
// Roadmaps
// ============================================================

func listRoadmaps(visionID string) ([]map[string]any, error) {
	db, err := openDB()
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
	return scanRoadmapRows(rows)
}

func getRoadmap(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	r := map[string]any{}
	row := db.QueryRow("SELECT * FROM roadmap WHERE id = ?", id)
	if err := scanRoadmapRow(row, r); err != nil {
		return nil, err
	}
	return r, nil
}

func createRoadmap(title, targetDate, visionID, status, priority string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("rdm")
	now := nowISO()
	_, err = db.Exec("INSERT INTO roadmap (id, vision_id, title, target_date, status, priority, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", id, visionID, title, targetDate, status, priority, now, now)
	if err != nil {
		return nil, err
	}
	return getRoadmap(id)
}

func updateRoadmap(id string, payload map[string]any) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := mapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return getRoadmap(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, nowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE roadmap SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return getRoadmap(id)
}

// ============================================================
// Principles
// ============================================================

func listPrinciples(status, kind string) ([]map[string]any, error) {
	db, err := openDB()
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
	return scanPrincipleRows(rows)
}

func getPrinciple(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	p := map[string]any{}
	row := db.QueryRow("SELECT * FROM principles WHERE id = ?", id)
	if err := scanPrincipleRow(row, p); err != nil {
		return nil, err
	}
	return p, nil
}

func createPrinciple(title, summary, kind, status string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("prncpl")
	now := nowISO()
	_, err = db.Exec("INSERT INTO principles (id, title, summary, kind, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", id, title, summary, kind, status, now, now)
	if err != nil {
		return nil, err
	}
	return getPrinciple(id)
}

func updatePrinciple(id string, payload map[string]any) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := mapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return getPrinciple(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, nowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE principles SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return getPrinciple(id)
}

// ============================================================
// Links
// ============================================================

func listLinks(sourceID, targetID, relation string) ([]map[string]any, error) {
	db, err := openDB()
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

func createLink(sourceType, sourceID, relation, targetType, targetID, note string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("link")
	now := nowISO()
	db.Exec("INSERT INTO links (id, source_type, source_id, relation, target_type, target_id, note, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", id, sourceType, sourceID, relation, targetType, targetID, note, now)
	return map[string]any{"id": id, "source_type": sourceType, "source_id": sourceID, "relation": relation, "target_type": targetType, "target_id": targetID}, nil
}

func deleteLink(id string) error {
	db, err := openDB()
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

func listDocRecords(status, layer string) ([]map[string]any, error) {
	db, err := openDB()
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

func updateDocRecord(path string, payload map[string]any) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := mapKeyToColumn(k)
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


func readDocContent(path string) (string, error) {
	dir, err := findRuntimeDir()
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

func getDailyNote(date string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if date == "" {
		date = today()
	}
	row := db.QueryRow("SELECT * FROM daily_notes WHERE note_date = ?", date)
	var noteDate, completedJSON, problemsJSON, risksJSON, nextJSON, updatedAt string
	if err := row.Scan(&noteDate, &completedJSON, &problemsJSON, &risksJSON, &nextJSON, &updatedAt); err != nil {
		return map[string]any{"note_date": date, "completed": []any{}, "problems": []any{}, "risks": []any{}, "next": []any{}}, nil
	}
	return map[string]any{"note_date": noteDate, "completed": parseJSONList(completedJSON), "problems": parseJSONList(problemsJSON), "risks": parseJSONList(risksJSON), "next": parseJSONList(nextJSON), "updated_at": updatedAt}, nil
}

func appendDailyNote(date string, payload map[string][]string) (map[string]any, error) {
	return upsertDaily(date, payload, true)
}
func replaceDailyNote(date string, payload map[string][]string) (map[string]any, error) {
	return upsertDaily(date, payload, false)
}

func upsertDaily(date string, payload map[string][]string, append_ bool) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if date == "" {
		date = today()
	}
	existing, _ := getDailyNote(date)
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
	now := nowISO()
	db.Exec("INSERT OR REPLACE INTO daily_notes (note_date, completed_json, problems_json, risks_json, next_json, updated_at) VALUES (?, ?, ?, ?, ?, ?)", date, jsonStr(merge("completed", payload["completed"])), jsonStr(merge("problems", payload["problems"])), jsonStr(merge("risks", payload["risks"])), jsonStr(merge("next", payload["next"])), now)
	return getDailyNote(date)
}

func listDailyNotes() ([]map[string]any, error) {
	db, err := openDB()
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
		notes = append(notes, map[string]any{"note_date": noteDate, "completed": parseJSONList(completedJSON), "problems": parseJSONList(problemsJSON), "risks": parseJSONList(risksJSON), "next": parseJSONList(nextJSON), "updated_at": updatedAt})
	}
	if notes == nil {
		notes = []map[string]any{}
	}
	return notes, nil
}

// ============================================================
// Visions
// ============================================================

func listVisions() ([]map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT * FROM visions ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVisionRows(rows)
}

func getVision(id string) (map[string]any, error) {
	db, err := openDB()
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

func createVision(title, summary, status, horizon string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("vision")
	now := nowISO()
	db.Exec("INSERT INTO visions (id, title, summary, status, horizon, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", id, title, summary, status, horizon, now, now)
	return getVision(id)
}

func updateVision(id string, payload map[string]any) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		col := mapKeyToColumn(k)
		if col == "" {
			continue
		}
		setParts = append(setParts, col+" = ?")
		args = append(args, v)
	}
	if len(setParts) == 0 {
		return getVision(id)
	}
	setParts = append(setParts, "updated_at = ?")
	args = append(args, nowISO())
	args = append(args, id)
	_, err = db.Exec(fmt.Sprintf("UPDATE visions SET %s WHERE id = ?", strings.Join(setParts, ", ")), args...)
	if err != nil {
		return nil, err
	}
	return getVision(id)
}

// ============================================================
// Canon
// ============================================================

func getCanon() (map[string]any, error) {
	db, err := openDB()
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

func updateCanon(decisionID, productGoal, engFocus, arch string, addScope, addAvoid []string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	now := nowISO()
	db.Exec("INSERT OR REPLACE INTO canon (id, updated_at, product_goal, engineering_focus, architecture) VALUES (1, ?, ?, ?, ?)", now, productGoal, engFocus, arch)
	for i, s := range addScope {
		db.Exec("INSERT OR REPLACE INTO canon_items (item_type, position, value) VALUES (?, ?, ?)", "scope", i, s)
	}
	for i, s := range addAvoid {
		db.Exec("INSERT OR REPLACE INTO canon_items (item_type, position, value) VALUES (?, ?, ?)", "avoid", i, s)
	}
	return getCanon()
}

// ============================================================
// Helpers
// ============================================================

func mapKeyToColumn(k string) string {
	m := map[string]string{
		"title": "title", "summary": "summary", "status": "status", "priority": "priority",
		"phase": "phase", "goal": "goal", "roadmap_id": "roadmap_id", "plan_id": "plan_id",
		"vision_id": "vision_id", "task_id": "task_id", "decision_id": "decision_id",
		"commit_id": "commit_id", "description": "description", "severity": "severity",
		"error": "error", "files": "files", "root_cause": "root_cause", "fix": "fix", "tags": "tags",
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

func scanTasks(rows *sql.Rows, err error) ([]Task, error) {
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
		t.Acceptance = parseJSONList(acceptanceJSON)
		t.RelatedDocs = parseJSONList(docsJSON)
		t.RelatedDecisions = parseJSONList(decsJSON)
		tasks = append(tasks, t)
	}
	if tasks == nil {
		tasks = []Task{}
	}
	return tasks, nil
}

func scanTaskRow(row *sql.Row, m map[string]any) error {
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

func scanCommitRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var commits []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := scanCommitRow(rows, m); err != nil {
			return nil, err
		}
		commits = append(commits, m)
	}
	if commits == nil {
		commits = []map[string]any{}
	}
	return commits, nil
}

func scanCommitRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
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
	m["files"] = parseJSONList(filesJSON)
	m["files_json"] = filesJSON
	m["created_at"] = createdAt
	m["updated_at"] = updatedAt
	return nil
}

func scanPlanRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var plans []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := scanPlanRow(rows, m); err != nil {
			return nil, err
		}
		plans = append(plans, m)
	}
	if plans == nil {
		plans = []map[string]any{}
	}
	return plans, nil
}

func scanPlanRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
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
	m["scope"] = parseJSONList(scopeJSON)
	m["risks"] = parseJSONList(risksJSON)
	m["assumptions"] = parseJSONList(assumptionsJSON)
	m["task_ids"] = parseJSONStrList(taskIDsJSON)
	m["source"] = source
	m["created_at"] = createdAt
	m["updated_at"] = updatedAt
	return nil
}

func scanBugRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var bugs []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := scanBugRow(rows, m); err != nil {
			return nil, err
		}
		bugs = append(bugs, m)
	}
	if bugs == nil {
		bugs = []map[string]any{}
	}
	return bugs, nil
}

func scanBugRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
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

func scanDecisionRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var decisions []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := scanDecisionRow(rows, m); err != nil {
			return nil, err
		}
		decisions = append(decisions, m)
	}
	if decisions == nil {
		decisions = []map[string]any{}
	}
	return decisions, nil
}

func scanDecisionRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
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
	m["impact"] = parseJSONList(impactJSON)
	m["alternatives"] = parseJSONList(altJSON)
	m["related_tasks"] = parseJSONList(relTasksJSON)
	m["updates_canon"] = updatesCanon == 1
	return nil
}

func scanIdeaRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var ideas []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := scanIdeaRow(rows, m); err != nil {
			return nil, err
		}
		ideas = append(ideas, m)
	}
	if ideas == nil {
		ideas = []map[string]any{}
	}
	return ideas, nil
}

func scanIdeaRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
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

func scanRoadmapRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var roadmaps []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := scanRoadmapRow(rows, m); err != nil {
			return nil, err
		}
		roadmaps = append(roadmaps, m)
	}
	if roadmaps == nil {
		roadmaps = []map[string]any{}
	}
	return roadmaps, nil
}

func scanRoadmapRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
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

func scanPrincipleRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var principles []map[string]any
	for rows.Next() {
		m := map[string]any{}
		if err := scanPrincipleRow(rows, m); err != nil {
			return nil, err
		}
		principles = append(principles, m)
	}
	if principles == nil {
		principles = []map[string]any{}
	}
	return principles, nil
}

func scanPrincipleRow(scanner interface{ Scan(...any) error }, m map[string]any) error {
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

func scanVisionRows(rows *sql.Rows) ([]map[string]any, error) {
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

func getTask(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	task := map[string]any{}
	row := db.QueryRow("SELECT * FROM tasks WHERE id = ?", id)
	if err := scanTaskRow(row, task); err != nil {
		return nil, err
	}
	commits, _ := listCommitsByTask(id)
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
	notes, _ := listTaskNotes(id, 20)
	task["note_history"] = notes
	return task, nil
}

// ============================================================
// Delete operations
// ============================================================

func deleteTask(id string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("DELETE FROM tasks WHERE id = ?", id)
	return err
}

func deletePlan(id string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("DELETE FROM plans WHERE id = ?", id)
	return err
}

func deleteBug(id string) error {
	db, err := openDB()
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

func createEvent(typ, entityType, entityID, summary string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("evt")
	now := nowISO()
	_, err = db.Exec("INSERT INTO events (id, type, entity_type, entity_id, summary, created_at, consumed_by_agent) VALUES (?, ?, ?, ?, ?, ?, 0)", id, typ, entityType, entityID, summary, now)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "type": typ, "entity_type": entityType, "entity_id": entityID, "summary": summary, "created_at": now, "consumed_by_agent": false}, nil
}

func listEvents(consumedOnly string) ([]map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT * FROM events"
	if consumedOnly == "unconsumed" {
		q += " WHERE consumed_by_agent = 0"
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
		var consumed int
		rows.Scan(&id, &typ, &entityType, &entityID, &summary, &createdAt, &consumed)
		events = append(events, map[string]any{
			"id": id, "type": typ, "entity_type": entityType, "entity_id": entityID,
			"summary": summary, "created_at": createdAt, "consumed_by_agent": consumed == 1,
		})
	}
	if events == nil {
		events = []map[string]any{}
	}
	return events, nil
}

func markEventsConsumed() error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("UPDATE events SET consumed_by_agent = 1 WHERE consumed_by_agent = 0")
	return err
}

func getUnconsumedEvents() ([]map[string]any, error) {
	return listEvents("unconsumed")
}

// ============================================================
// Feedback — delegated to remote API (feedback.go)
//   Compatible with Python pmai feedback server.
//   CLI: aipmc feedback add --label bug|suggestion --content "..."
//   CLI: aipmc feedback list [--label bug|suggestion]
// ============================================================
