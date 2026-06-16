package store

import (
	"database/sql"
	"fmt"

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

// ---- Collaboration Topics (v1; stored in meeting_rooms) ----

func CreateCollaborationTopic(title, planID, createdBy string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	id := u.Slug("topic")
	now := u.NowISO()
	_, err = db.Exec(
		`INSERT INTO meeting_rooms (id, title, topic, context, status, created_by, created_at, pm_last_visit_at, plan_id)
		 VALUES (?, ?, ?, '', 'active', ?, ?, ?, ?)`,
		id, title, title, createdBy, now, now, planID,
	)
	if err != nil {
		return nil, err
	}
	pmdb.SyncFTS5Entity(db, "meeting", id, title, title)
	return GetCollaborationTopic(id)
}

func GetCollaborationTopic(id string) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	m := map[string]any{}
	var closedAt, pmLastVisit, planID sql.NullString
	row := db.QueryRow(
		`SELECT id, title, topic, context, status, created_by, created_at, closed_at,
		        COALESCE(pm_last_visit_at, ''), COALESCE(plan_id, '')
		 FROM meeting_rooms WHERE id = ?`, id,
	)
	var tid, title, topic, ctx, status, createdBy, createdAt string
	if err := row.Scan(&tid, &title, &topic, &ctx, &status, &createdBy, &createdAt, &closedAt, &pmLastVisit, &planID); err != nil {
		return nil, fmt.Errorf("topic not found: %s", id)
	}
	m["id"] = tid
	m["title"] = title
	m["topic"] = topic
	m["context"] = ctx
	m["status"] = status
	m["created_by"] = createdBy
	m["created_at"] = createdAt
	if closedAt.Valid {
		m["closed_at"] = closedAt.String
	}
	if pmLastVisit.Valid {
		m["pm_last_visit_at"] = pmLastVisit.String
	}
	if planID.Valid {
		m["plan_id"] = planID.String
	}
	return m, nil
}

// TouchPMLastVisit sets pm_last_visit_at to now and returns the previous value.
func TouchPMLastVisit(id string) (previous string, err error) {
	topic, err := GetCollaborationTopic(id)
	if err != nil {
		return "", err
	}
	previous = u.Str(topic["pm_last_visit_at"])
	now := u.NowISO()
	db, err := pmdb.Open()
	if err != nil {
		return "", err
	}
	defer db.Close()
	_, err = db.Exec("UPDATE meeting_rooms SET pm_last_visit_at = ? WHERE id = ?", now, id)
	return previous, err
}

func CloseCollaborationTopic(id string) (map[string]any, error) {
	return CloseMeetingRoom(id)
}

