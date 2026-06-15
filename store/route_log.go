package store

import (
	pmdb "aipmc/db"
	"aipmc/u"
)

// RouteLogEntry is one PM → agent routing action from topic prompt.
type RouteLogEntry struct {
	TopicID        string
	ToSource       string
	Refs           string
	PMSay          string
	PromptSnapshot string
}

// LogRoute records a topic prompt routing event.
func LogRoute(e RouteLogEntry) (map[string]any, error) {
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	id := u.Slug("route")
	now := u.NowISO()
	_, err = db.Exec(
		`INSERT INTO route_log (id, topic_id, to_source, refs, pm_say, prompt_snapshot, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, e.TopicID, e.ToSource, e.Refs, e.PMSay, e.PromptSnapshot, now,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "topic_id": e.TopicID, "to_source": e.ToSource,
		"refs": e.Refs, "pm_say": e.PMSay, "created_at": now,
	}, nil
}

// LastRouteSince returns created_at of the most recent route for a topic, or "".
func LastRouteSince(topicID string) (string, error) {
	db, err := pmdb.Open()
	if err != nil {
		return "", err
	}
	defer db.Close()

	var ts string
	err = db.QueryRow(
		`SELECT created_at FROM route_log WHERE topic_id = ? ORDER BY created_at DESC LIMIT 1`,
		topicID,
	).Scan(&ts)
	if err != nil {
		return "", nil
	}
	return ts, nil
}

// ListRoutes returns routing history for a topic, newest first.
func ListRoutes(topicID string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	db, err := pmdb.Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(
		`SELECT id, topic_id, to_source, refs, pm_say, prompt_snapshot, created_at
		 FROM route_log WHERE topic_id = ? ORDER BY created_at DESC LIMIT ?`,
		topicID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var id, tid, to, refs, say, snap, createdAt string
		rows.Scan(&id, &tid, &to, &refs, &say, &snap, &createdAt)
		out = append(out, map[string]any{
			"id": id, "topic_id": tid, "to_source": to, "refs": refs,
			"pm_say": say, "prompt_snapshot": snap, "created_at": createdAt,
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}
