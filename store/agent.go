package store

import (
	"database/sql"
	"strings"

	pmdb "aipmc/db"
	"aipmc/u"
)

// ---- Agent Assignments ----

func CreateAssignment(agentID, taskID, role, scope, assignedBy string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("asgn")
	now := u.NowISO()
	var tid any
	if taskID != "" {
		tid = taskID
	}
	_, err = db.Exec("INSERT INTO agent_assignments (id, agent_id, task_id, role, scope, status, assigned_by, assigned_at) VALUES (?, ?, ?, ?, ?, 'assigned', ?, ?)", id, agentID, tid, role, scope, assignedBy, now)
	if err != nil {
		return nil, err
	}
	return GetAssignment(id)
}

func GetAssignment(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	a := map[string]any{}
	var taskID, claimedAt, completedAt sql.NullString
	row := db.QueryRow("SELECT id, agent_id, task_id, role, scope, status, assigned_by, assigned_at, claimed_at, completed_at FROM agent_assignments WHERE id = ?", id)
	var ga_id, ga_aid, ga_role, ga_scope, ga_status, ga_by, ga_at string; row.Scan(&ga_id, &ga_aid, &taskID, &ga_role, &ga_scope, &ga_status, &ga_by, &ga_at, &claimedAt, &completedAt); a["id"]=ga_id; a["agent_id"]=ga_aid; a["role"]=ga_role; a["scope"]=ga_scope; a["status"]=ga_status; a["assigned_by"]=ga_by; a["assigned_at"]=ga_at; err = nil
	if err != nil {
		return nil, err
	}
	if taskID.Valid {
		a["task_id"] = taskID.String
	}
	if claimedAt.Valid {
		a["claimed_at"] = claimedAt.String
	}
	if completedAt.Valid {
		a["completed_at"] = completedAt.String
	}
	return a, nil
}

func ListAssignments(agentID, status string) ([]map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT id, agent_id, task_id, role, scope, status, assigned_by, assigned_at, claimed_at, completed_at FROM agent_assignments WHERE 1=1"
	var args []any
	if agentID != "" {
		q += " AND agent_id = ?"
		args = append(args, agentID)
	}
	if status != "" {
		q += " AND status = ?"
		args = append(args, status)
	}
	q += " ORDER BY assigned_at DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		a := map[string]any{}
		var taskID, claimedAt, completedAt sql.NullString
		var la_id, la_aid, la_role, la_scope, la_status, la_by, la_at string; rows.Scan(&la_id, &la_aid, &taskID, &la_role, &la_scope, &la_status, &la_by, &la_at, &claimedAt, &completedAt); a["id"]=la_id; a["agent_id"]=la_aid; a["role"]=la_role; a["scope"]=la_scope; a["status"]=la_status; a["assigned_by"]=la_by; a["assigned_at"]=la_at
		if taskID.Valid {
			a["task_id"] = taskID.String
		}
		if claimedAt.Valid {
			a["claimed_at"] = claimedAt.String
		}
		if completedAt.Valid {
			a["completed_at"] = completedAt.String
		}
		result = append(result, a)
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}

func UpdateAssignment(id string, payload map[string]any) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	setParts := []string{}
	args := []any{}
	for k, v := range payload {
		switch k {
		case "status", "role", "scope":
			setParts = append(setParts, k+" = ?")
			args = append(args, v)
		case "claimed":
			setParts = append(setParts, "claimed_at = ?")
			args = append(args, u.NowISO())
		case "completed":
			setParts = append(setParts, "completed_at = ?")
			args = append(args, u.NowISO())
		}
	}
	if len(setParts) == 0 {
		return GetAssignment(id)
	}
	args = append(args, id)
	_, err = db.Exec("UPDATE agent_assignments SET "+strings.Join(setParts, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return nil, err
	}
	return GetAssignment(id)
}
