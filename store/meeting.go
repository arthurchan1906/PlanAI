package store

import (
	"database/sql"

	pmdb "aipmc/db"
	"aipmc/u"
)

// ---- Meeting Rooms ----

func CreateMeetingRoom(title, topic, context, createdBy string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("meeting")
	now := u.NowISO()
	_, err = db.Exec("INSERT INTO meeting_rooms (id, title, topic, context, status, created_by, created_at) VALUES (?, ?, ?, ?, 'active', ?, ?)", id, title, topic, context, createdBy, now)
	if err != nil {
		return nil, err
	}
	pmdb.SyncFTS5Entity(db, "meeting", id, title, title+" "+topic)
	return GetMeetingRoom(id)
}

func GetMeetingRoom(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	m := map[string]any{}
	var closedAt sql.NullString
	row := db.QueryRow("SELECT id, title, topic, context, status, created_by, created_at, closed_at FROM meeting_rooms WHERE id = ?", id)
	var gmr_id, gmr_title, gmr_topic, gmr_ctx, gmr_status, gmr_by, gmr_at string
	row.Scan(&gmr_id, &gmr_title, &gmr_topic, &gmr_ctx, &gmr_status, &gmr_by, &gmr_at, &closedAt)
	m["id"] = gmr_id
	m["title"] = gmr_title
	m["topic"] = gmr_topic
	m["context"] = gmr_ctx
	m["status"] = gmr_status
	m["created_by"] = gmr_by
	m["created_at"] = gmr_at
	if closedAt.Valid {
		m["closed_at"] = closedAt.String
	}
	turns, _ := ListMeetingTurns(id)
	m["turns"] = turns
	parts, _ := ListMeetingParticipants(id)
	m["participants"] = parts
	return m, nil
}

func ListMeetingRooms(status string) ([]map[string]any, error) {
	db, err := pmdb.Open()
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
		var mid, mtitle, mtopic, mctx, mstatus, mcreatedBy, mcreatedAt string
		rows.Scan(&mid, &mtitle, &mtopic, &mctx, &mstatus, &mcreatedBy, &mcreatedAt, &closedAt)
		m["id"] = mid
		m["title"] = mtitle
		m["topic"] = mtopic
		m["context"] = mctx
		m["status"] = mstatus
		m["created_by"] = mcreatedBy
		m["created_at"] = mcreatedAt
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

func CloseMeetingRoom(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.Exec("UPDATE meeting_rooms SET status = 'closed', closed_at = ? WHERE id = ?", u.NowISO(), id)
	return GetMeetingRoom(id)
}

func SetMeetingPMTyping(roomID string, typing bool) error {
	db, err := pmdb.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	v := 0
	if typing {
		v = 1
	}
	_, err = db.Exec("UPDATE meeting_rooms SET pm_typing = ? WHERE id = ?", v, roomID)
	return err
}

// ---- Meeting Turns ----

func NextTurnNumber(roomID string) int {
	turns, _ := ListMeetingTurns(roomID)
	return len(turns) + 1
}

func CreateMeetingTurn(roomID string, turnNumber int, speakerType, speakerID, question string) (map[string]any, error) {
	return CreateMeetingTurnEx(roomID, turnNumber, speakerType, speakerID, question, "", "")
}

func CreateMeetingTurnEx(roomID string, turnNumber int, speakerType, speakerID, question, replyTo, addressTo string) (map[string]any, error) {
	if turnNumber <= 0 {
		turnNumber = NextTurnNumber(roomID)
	}
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("turn")
	now := u.NowISO()
	_, err = db.Exec("INSERT INTO meeting_turns (id, room_id, turn_number, speaker_type, speaker_id, question, response, status, reply_to, address_to, created_at) VALUES (?, ?, ?, ?, ?, ?, '', 'waiting', ?, ?, ?)", id, roomID, turnNumber, speakerType, speakerID, question, replyTo, addressTo, now)
	if err != nil {
		return nil, err
	}
	return GetMeetingTurn(id)
}

func CreateAgentVoluntaryTurn(roomID, agentID, content, replyTo, addressTo string) (map[string]any, error) {
	turnNumber := NextTurnNumber(roomID)
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("turn")
	now := u.NowISO()
	_, err = db.Exec("INSERT INTO meeting_turns (id, room_id, turn_number, speaker_type, speaker_id, question, response, status, reply_to, address_to, created_at) VALUES (?, ?, ?, 'agent', ?, '[主动发言]', ?, 'responded', ?, ?, ?)", id, roomID, turnNumber, agentID, content, replyTo, addressTo, now)
	if err != nil {
		return nil, err
	}
	return GetMeetingTurn(id)
}

func CreateArbitrationTurn(roomID, agentID, question string) (map[string]any, error) {
	return CreateMeetingTurn(roomID, 0, "agent", agentID, question)
}

func GetMeetingTurn(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	t := map[string]any{}
	var gt_id, gt_rid, gt_tn, gt_st, gt_sid, gt_q, gt_r, gt_s, gt_rt, gt_at, gt_ca string
	row := db.QueryRow("SELECT id, room_id, turn_number, speaker_type, speaker_id, question, response, status, reply_to, address_to, created_at FROM meeting_turns WHERE id = ?", id)
	row.Scan(&gt_id, &gt_rid, &gt_tn, &gt_st, &gt_sid, &gt_q, &gt_r, &gt_s, &gt_rt, &gt_at, &gt_ca)
	t["id"] = gt_id
	t["room_id"] = gt_rid
	t["turn_number"] = gt_tn
	t["speaker_type"] = gt_st
	t["speaker_id"] = gt_sid
	t["question"] = gt_q
	t["response"] = gt_r
	t["status"] = gt_s
	t["reply_to"] = gt_rt
	t["address_to"] = gt_at
	t["created_at"] = gt_ca
	return t, nil
}

func ListMeetingTurns(roomID string) ([]map[string]any, error) {
	db, err := pmdb.Open()
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
		t["id"] = lt_id
		t["room_id"] = lt_rid
		t["turn_number"] = lt_tn
		t["speaker_type"] = lt_st
		t["speaker_id"] = lt_sid
		t["question"] = lt_q
		t["response"] = lt_r
		t["status"] = lt_s
		t["reply_to"] = lt_rt
		t["address_to"] = lt_at
		t["created_at"] = lt_ca
		result = append(result, t)
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}

func RespondMeetingTurn(id, response string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.Exec("UPDATE meeting_turns SET response = ?, status = 'responded' WHERE id = ?", response, id)
	return GetMeetingTurn(id)
}

// ---- Meeting Participants ----

func ConfirmMeetingAttendance(meetingID, agentID string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	now := u.NowISO()
	db.Exec("INSERT OR REPLACE INTO meeting_participants (meeting_id, agent_id, status, confirmed_at) VALUES (?, ?, 'ready', ?)", meetingID, agentID, now)
	return map[string]any{"meeting_id": meetingID, "agent_id": agentID, "status": "ready"}, nil
}

func ListMeetingParticipants(meetingID string) ([]map[string]any, error) {
	db, err := pmdb.Open()
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
		var lp_mid, lp_aid, lp_st, lp_ca, lp_n, lp_r string
		rows.Scan(&lp_mid, &lp_aid, &lp_st, &lp_ca, &lp_n, &lp_r)
		p["meeting_id"] = lp_mid
		p["agent_id"] = lp_aid
		p["status"] = lp_st
		p["confirmed_at"] = lp_ca
		p["name"] = lp_n
		p["role"] = lp_r
		result = append(result, p)
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}

func UpdateLastSeenTurn(meetingID, agentID string, turnNumber int) {
	db, err := pmdb.Open()
	if err != nil {
		return
	}
	defer db.Close()
	db.Exec("UPDATE meeting_participants SET last_seen_turn = ? WHERE meeting_id = ? AND agent_id = ?", turnNumber, meetingID, agentID)
}

func MarkTurnProcessing(turnID string) {
	db, err := pmdb.Open()
	if err != nil {
		return
	}
	defer db.Close()
	db.Exec("UPDATE meeting_turns SET status = 'processing' WHERE id = ?", turnID)
}

// FindWaitingTurn returns the earliest waiting turn for an agent in an active room.
func FindWaitingTurn(agentID string) (turnID, roomID, question, roomTitle string, turnNumber int, ok bool) {
	db, err := pmdb.Open()
	if err != nil {
		return
	}
	defer db.Close()
	row := db.QueryRow(`
		SELECT mt.id, mt.room_id, mt.turn_number, mt.question, mr.title
		FROM meeting_turns mt
		JOIN meeting_rooms mr ON mt.room_id = mr.id
		WHERE mt.speaker_id = ? AND mt.status = 'waiting' AND mr.status = 'active'
		ORDER BY mt.created_at
		LIMIT 1`, agentID)
	var tn int
	if err := row.Scan(&turnID, &roomID, &tn, &question, &roomTitle); err != nil {
		return
	}
	return turnID, roomID, question, roomTitle, tn, true
}
