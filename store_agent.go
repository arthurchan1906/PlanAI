package main

import (
	"database/sql"
	"strings"
)

// ---- Meeting Rooms ----

func createMeetingRoom(title, topic, context, createdBy string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("meeting")
	now := nowISO()
	_, err = db.Exec("INSERT INTO meeting_rooms (id, title, topic, context, status, created_by, created_at) VALUES (?, ?, ?, ?, 'active', ?, ?)", id, title, topic, context, createdBy, now)
	if err != nil {
		return nil, err
	}
	syncFTS5Entity(db, "meeting", id, title, title+" "+topic)
	return getMeetingRoom(id)
}

func getMeetingRoom(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	m := map[string]any{}
	var closedAt sql.NullString
	row := db.QueryRow("SELECT id, title, topic, context, status, created_by, created_at, closed_at FROM meeting_rooms WHERE id = ?", id)
	var gmr_id, gmr_title, gmr_topic, gmr_ctx, gmr_status, gmr_by, gmr_at string; row.Scan(&gmr_id, &gmr_title, &gmr_topic, &gmr_ctx, &gmr_status, &gmr_by, &gmr_at, &closedAt); m["id"]=gmr_id; m["title"]=gmr_title; m["topic"]=gmr_topic; m["context"]=gmr_ctx; m["status"]=gmr_status; m["created_by"]=gmr_by; m["created_at"]=gmr_at; err = nil
	if err != nil {
		return nil, err
	}
	if closedAt.Valid {
		m["closed_at"] = closedAt.String
	}
	turns, _ := listMeetingTurns(id)
	m["turns"] = turns
	parts, _ := listMeetingParticipants(id)
	m["participants"] = parts
	return m, nil
}

func listMeetingRooms(status string) ([]map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	q := "SELECT id, title, topic, context, status, created_by, created_at, closed_at FROM meeting_rooms"
	var args []any
	if status != "" {
		q += " WHERE status = ?"
		args = append(args, status)
	}
	q += " ORDER BY created_at DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		m := map[string]any{}
		var closedAt sql.NullString
		var mid, mtitle, mtopic, mctx, mstatus, mcreatedBy, mcreatedAt string; rows.Scan(&mid, &mtitle, &mtopic, &mctx, &mstatus, &mcreatedBy, &mcreatedAt, &closedAt); m["id"]=mid; m["title"]=mtitle; m["topic"]=mtopic; m["context"]=mctx; m["status"]=mstatus; m["created_by"]=mcreatedBy; m["created_at"]=mcreatedAt
		if closedAt.Valid {
			m["closed_at"] = closedAt.String
		}
		result = append(result, m)
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}

func closeMeetingRoom(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.Exec("UPDATE meeting_rooms SET status = 'closed', closed_at = ? WHERE id = ?", nowISO(), id)
	return getMeetingRoom(id)
}

// ---- Meeting Turns ----

func createMeetingTurn(roomID string, turnNumber int, speakerType, speakerID, question string) (map[string]any, error) {
	return createMeetingTurnEx(roomID, turnNumber, speakerType, speakerID, question, "", "")
}

func createMeetingTurnEx(roomID string, turnNumber int, speakerType, speakerID, question, replyTo, addressTo string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("turn")
	now := nowISO()
	tn := turnNumber
	_, err = db.Exec("INSERT INTO meeting_turns (id, room_id, turn_number, speaker_type, speaker_id, question, response, status, reply_to, address_to, created_at) VALUES (?, ?, ?, ?, ?, ?, '', 'waiting', ?, ?, ?)", id, roomID, tn, speakerType, speakerID, question, replyTo, addressTo, now)
	if err != nil {
		return nil, err
	}
	return getMeetingTurn(id)
}

func getMeetingTurn(id string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	t := map[string]any{}
	var gt_id, gt_rid, gt_tn, gt_st, gt_sid, gt_q, gt_r, gt_s, gt_rt, gt_at, gt_ca string
	row := db.QueryRow("SELECT id, room_id, turn_number, speaker_type, speaker_id, question, response, status, reply_to, address_to, created_at FROM meeting_turns WHERE id = ?", id)
	row.Scan(&gt_id, &gt_rid, &gt_tn, &gt_st, &gt_sid, &gt_q, &gt_r, &gt_s, &gt_rt, &gt_at, &gt_ca)
	t["id"] = gt_id; t["room_id"] = gt_rid; t["turn_number"] = gt_tn; t["speaker_type"] = gt_st; t["speaker_id"] = gt_sid; t["question"] = gt_q; t["response"] = gt_r; t["status"] = gt_s; t["reply_to"] = gt_rt; t["address_to"] = gt_at; t["created_at"] = gt_ca
	return t, nil
}

func listMeetingTurns(roomID string) ([]map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT id, room_id, turn_number, speaker_type, speaker_id, question, response, status, reply_to, address_to, created_at FROM meeting_turns WHERE room_id = ? ORDER BY turn_number", roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		t := map[string]any{}
		var lt_id, lt_rid, lt_tn, lt_st, lt_sid, lt_q, lt_r, lt_s, lt_rt, lt_at, lt_ca string
		rows.Scan(&lt_id, &lt_rid, &lt_tn, &lt_st, &lt_sid, &lt_q, &lt_r, &lt_s, &lt_rt, &lt_at, &lt_ca)
		t["id"] = lt_id; t["room_id"] = lt_rid; t["turn_number"] = lt_tn; t["speaker_type"] = lt_st; t["speaker_id"] = lt_sid; t["question"] = lt_q; t["response"] = lt_r; t["status"] = lt_s; t["reply_to"] = lt_rt; t["address_to"] = lt_at; t["created_at"] = lt_ca
		result = append(result, t)
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}

func respondMeetingTurn(id, response string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.Exec("UPDATE meeting_turns SET response = ?, status = 'responded' WHERE id = ?", response, id)
	return getMeetingTurn(id)
}

// ---- Meeting Participants ----

func confirmMeetingAttendance(meetingID, agentID string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	now := nowISO()
	db.Exec("INSERT OR REPLACE INTO meeting_participants (meeting_id, agent_id, status, confirmed_at) VALUES (?, ?, 'ready', ?)", meetingID, agentID, now)
	return map[string]any{"meeting_id": meetingID, "agent_id": agentID, "status": "ready"}, nil
}

func listMeetingParticipants(meetingID string) ([]map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT mp.meeting_id, mp.agent_id, mp.status, mp.confirmed_at, ap.name, ap.role FROM meeting_participants mp JOIN agent_profiles ap ON mp.agent_id = ap.id WHERE mp.meeting_id = ?", meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		p := map[string]any{}
		var lp_mid, lp_aid, lp_st, lp_ca, lp_n, lp_r string; rows.Scan(&lp_mid, &lp_aid, &lp_st, &lp_ca, &lp_n, &lp_r); p["meeting_id"]=lp_mid; p["agent_id"]=lp_aid; p["status"]=lp_st; p["confirmed_at"]=lp_ca; p["name"]=lp_n; p["role"]=lp_r
		result = append(result, p)
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}

// ---- Agent Assignments ----

func createAssignment(agentID, taskID, role, scope, assignedBy string) (map[string]any, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := slug("asgn")
	now := nowISO()
	var tid any
	if taskID != "" {
		tid = taskID
	}
	_, err = db.Exec("INSERT INTO agent_assignments (id, agent_id, task_id, role, scope, status, assigned_by, assigned_at) VALUES (?, ?, ?, ?, ?, 'assigned', ?, ?)", id, agentID, tid, role, scope, assignedBy, now)
	if err != nil {
		return nil, err
	}
	return getAssignment(id)
}

func getAssignment(id string) (map[string]any, error) {
	db, err := openDB()
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

func listAssignments(agentID, status string) ([]map[string]any, error) {
	db, err := openDB()
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

func updateAssignment(id string, payload map[string]any) (map[string]any, error) {
	db, err := openDB()
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
			args = append(args, nowISO())
		case "completed":
			setParts = append(setParts, "completed_at = ?")
			args = append(args, nowISO())
		}
	}
	if len(setParts) == 0 {
		return getAssignment(id)
	}
	args = append(args, id)
	_, err = db.Exec("UPDATE agent_assignments SET "+strings.Join(setParts, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return nil, err
	}
	return getAssignment(id)
}

func updateLastSeenTurn(meetingID, agentID string, turnNumber int) {
	db, err := openDB()
	if err != nil { return }
	defer db.Close()
	db.Exec("UPDATE meeting_participants SET last_seen_turn = ? WHERE meeting_id = ? AND agent_id = ?", turnNumber, meetingID, agentID)
}

func markTurnProcessing(turnID string) {
	db, err := openDB()
	if err != nil { return }
	defer db.Close()
	db.Exec("UPDATE meeting_turns SET status = 'processing' WHERE id = ?", turnID)
}
